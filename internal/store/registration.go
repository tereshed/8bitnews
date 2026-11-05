package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

type Registration struct {
	ChannelID, Title, Summary, DevelopmentSummary, TaskID, EventKey string
	StoryID                                                         *string
	ReportedAt                                                      time.Time
	SeedURLs                                                        json.RawMessage
}

// RegisterDevelopment reserves an event and a task before creating any story.
// A source/task retry cannot leave another storyline or development behind.
func (s *Store) RegisterDevelopment(ctx context.Context, p Registration) (string, string, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return "", "", err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	keys := []string{"event:" + p.EventKey}
	if p.TaskID != "" {
		keys = append(keys, "task:"+p.TaskID)
	}
	// Channel-wide registration lock covers overlapping reservations in one
	// transaction, including two tasks presenting the same event concurrently.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 1))`, p.ChannelID); err != nil {
		return "", "", err
	}
	var matchedStory, matchedDevelopment string
	for _, key := range keys {
		var storyID, devID, eventKey string
		err := tx.QueryRow(ctx, `SELECT d.story_id, d.id, reg.event_key FROM story_registrations reg
		 JOIN story_developments d ON d.id=reg.development_id WHERE reg.channel_id=$1 AND reg.registration_key=$2`, p.ChannelID, key).Scan(&storyID, &devID, &eventKey)
		if err == nil {
			if eventKey != p.EventKey || p.StoryID != nil && *p.StoryID != storyID {
				return "", "", ErrConflict
			}
			matchedStory, matchedDevelopment = storyID, devID
			continue
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return "", "", err
		}
	}
	if matchedDevelopment != "" {
		for _, key := range keys {
			if _, err := tx.Exec(ctx, `INSERT INTO story_registrations(channel_id,registration_key,event_key,development_id)
			 VALUES($1,$2,$3,$4) ON CONFLICT DO NOTHING`, p.ChannelID, key, p.EventKey, matchedDevelopment); err != nil {
				return "", "", err
			}
		}
		return matchedStory, matchedDevelopment, tx.Commit(ctx)
	}
	var storyID string
	if p.StoryID == nil {
		err = tx.QueryRow(ctx, `INSERT INTO stories(channel_id,title,summary) VALUES($1,$2,$3) RETURNING id`, p.ChannelID, p.Title, p.Summary).Scan(&storyID)
	} else {
		storyID = *p.StoryID
		var channel string
		err = tx.QueryRow(ctx, `SELECT channel_id FROM stories WHERE id=$1`, storyID).Scan(&channel)
		if err == nil && channel != p.ChannelID {
			return "", "", ErrConflict
		}
	}
	if err != nil {
		return "", "", mapNotFound(err)
	}
	var devID string
	err = tx.QueryRow(ctx, `INSERT INTO story_developments(story_id,summary,reported_at,seed_urls,polymaths_task_id)
	 VALUES($1,$2,$3,$4,$5) RETURNING id`, storyID, p.DevelopmentSummary, p.ReportedAt, p.SeedURLs, p.TaskID).Scan(&devID)
	if err != nil {
		return "", "", err
	}
	if _, err = tx.Exec(ctx, `UPDATE stories SET updated_at=now() WHERE id=$1`, storyID); err != nil {
		return "", "", err
	}
	for _, key := range keys {
		if _, err = tx.Exec(ctx, `INSERT INTO story_registrations(channel_id,registration_key,event_key,development_id) VALUES($1,$2,$3,$4)`, p.ChannelID, key, p.EventKey, devID); err != nil {
			return "", "", fmt.Errorf("reserving story event: %w", err)
		}
	}
	return storyID, devID, tx.Commit(ctx)
}
