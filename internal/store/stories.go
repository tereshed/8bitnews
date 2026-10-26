package store

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// CreateStory inserts a new storyline for a channel.
func (s *Store) CreateStory(ctx context.Context, channelID, title, summary string) (Story, error) {
	var st Story
	err := s.pool.QueryRow(ctx, `
		INSERT INTO stories (channel_id, title, summary)
		VALUES ($1, $2, $3)
		RETURNING id, channel_id, title, summary, created_at, updated_at
	`, channelID, title, summary).Scan(&st.ID, &st.ChannelID, &st.Title, &st.Summary, &st.CreatedAt, &st.UpdatedAt)
	if err != nil {
		return Story{}, fmt.Errorf("creating story: %w", err)
	}
	return st, nil
}

// GetStory returns the storyline by id.
func (s *Store) GetStory(ctx context.Context, id string) (Story, error) {
	var st Story
	err := s.pool.QueryRow(ctx, `
		SELECT id, channel_id, title, summary, created_at, updated_at
		FROM stories WHERE id = $1
	`, id).Scan(&st.ID, &st.ChannelID, &st.Title, &st.Summary, &st.CreatedAt, &st.UpdatedAt)
	if err != nil {
		return Story{}, fmt.Errorf("getting story %s: %w", id, mapNotFound(err))
	}
	return st, nil
}

// TouchStory bumps a story's updated_at when a new development is
// registered for it, so the most actively covered storylines sort first.
func (s *Store) TouchStory(ctx context.Context, id string) error {
	_, err := s.pool.Exec(ctx, `UPDATE stories SET updated_at = now() WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("touching story %s: %w", id, err)
	}
	return nil
}

// CreateDevelopment inserts a new turn of a storyline.
func (s *Store) CreateDevelopment(ctx context.Context, storyID, summary string, reportedAt time.Time, seedURLs json.RawMessage, polymathsTaskID string) (StoryDevelopment, error) {
	var d StoryDevelopment
	err := s.pool.QueryRow(ctx, `
		INSERT INTO story_developments (story_id, summary, reported_at, seed_urls, polymaths_task_id)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, story_id, summary, reported_at, seed_urls, polymaths_task_id, created_at
	`, storyID, summary, reportedAt, nonNilJSON(seedURLs), polymathsTaskID).Scan(
		&d.ID, &d.StoryID, &d.Summary, &d.ReportedAt, &d.SeedURLs, &d.PolymathsTaskID, &d.CreatedAt,
	)
	if err != nil {
		return StoryDevelopment{}, fmt.Errorf("creating development: %w", err)
	}
	return d, nil
}

// GetDevelopment returns a story development by id.
func (s *Store) GetDevelopment(ctx context.Context, id string) (StoryDevelopment, error) {
	var d StoryDevelopment
	err := s.pool.QueryRow(ctx, `
		SELECT id, story_id, summary, reported_at, seed_urls, polymaths_task_id, created_at
		FROM story_developments WHERE id = $1
	`, id).Scan(&d.ID, &d.StoryID, &d.Summary, &d.ReportedAt, &d.SeedURLs, &d.PolymathsTaskID, &d.CreatedAt)
	if err != nil {
		return StoryDevelopment{}, fmt.Errorf("getting development %s: %w", id, mapNotFound(err))
	}
	return d, nil
}

// FindSimilarStories scores every story of channelID by trigram similarity
// of title and summary against the given query, restricted to stories with
// at least one development reported in the last `days` days, and returns the
// top matches each with its full development history and the releases made
// for it (docs/architecture.md §7, hub task E1 story_find_similar).
func (s *Store) FindSimilarStories(ctx context.Context, channelID, title, summary string, days, limit int) ([]SimilarStory, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, channel_id, title, summary, created_at, updated_at, score
		FROM (
			SELECT s.*, (similarity(s.title, $2) * 0.6 + similarity(s.summary, $3) * 0.4) AS score
			FROM stories s
			WHERE s.channel_id = $1
			  AND EXISTS (
				SELECT 1 FROM story_developments d
				WHERE d.story_id = s.id AND d.reported_at >= now() - make_interval(days => $4::int)
			  )
		) scored
		WHERE score > 0.3
		ORDER BY score DESC
		LIMIT $5
	`, channelID, title, summary, days, limit)
	if err != nil {
		return nil, fmt.Errorf("querying similar stories: %w", err)
	}
	defer rows.Close()

	var matches []SimilarStory
	for rows.Next() {
		var m SimilarStory
		if err := rows.Scan(&m.Story.ID, &m.Story.ChannelID, &m.Story.Title, &m.Story.Summary, &m.Story.CreatedAt, &m.Story.UpdatedAt, &m.Score); err != nil {
			return nil, fmt.Errorf("scanning similar story: %w", err)
		}
		matches = append(matches, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating similar stories: %w", err)
	}

	for i := range matches {
		devs, err := s.developmentsWithReleases(ctx, matches[i].Story.ID)
		if err != nil {
			return nil, err
		}
		matches[i].Developments = devs
	}
	return matches, nil
}

// developmentsWithReleases returns every development of storyID, oldest
// first, each paired with the releases made for it.
func (s *Store) developmentsWithReleases(ctx context.Context, storyID string) ([]DevelopmentWithReleases, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, story_id, summary, reported_at, seed_urls, polymaths_task_id, created_at
		FROM story_developments
		WHERE story_id = $1
		ORDER BY reported_at ASC
	`, storyID)
	if err != nil {
		return nil, fmt.Errorf("querying developments of story %s: %w", storyID, err)
	}

	var devs []StoryDevelopment
	for rows.Next() {
		var d StoryDevelopment
		if err := rows.Scan(&d.ID, &d.StoryID, &d.Summary, &d.ReportedAt, &d.SeedURLs, &d.PolymathsTaskID, &d.CreatedAt); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scanning development: %w", err)
		}
		devs = append(devs, d)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating developments of story %s: %w", storyID, err)
	}

	result := make([]DevelopmentWithReleases, 0, len(devs))
	for _, d := range devs {
		releases, err := s.releasesForDevelopment(ctx, d.ID)
		if err != nil {
			return nil, err
		}
		result = append(result, DevelopmentWithReleases{Development: d, Releases: releases})
	}
	return result, nil
}

func (s *Store) releasesForDevelopment(ctx context.Context, developmentID string) ([]ReleaseSummary, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT r.id, r.status,
		       (SELECT MIN(p.published_at) FROM publications p WHERE p.release_id = r.id) AS published_at
		FROM releases r
		WHERE r.development_id = $1
		ORDER BY r.created_at ASC
	`, developmentID)
	if err != nil {
		return nil, fmt.Errorf("querying releases of development %s: %w", developmentID, err)
	}
	defer rows.Close()

	var releases []ReleaseSummary
	for rows.Next() {
		var r ReleaseSummary
		if err := rows.Scan(&r.ID, &r.Status, &r.PublishedAt); err != nil {
			return nil, fmt.Errorf("scanning release summary: %w", err)
		}
		releases = append(releases, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating releases of development %s: %w", developmentID, err)
	}
	return releases, nil
}

// DevelopmentsForStory is the public form of developmentsWithReleases, used
// by the story_register response to report the current state of the
// storyline the caller just added a turn to.
func (s *Store) DevelopmentsForStory(ctx context.Context, storyID string) ([]DevelopmentWithReleases, error) {
	return s.developmentsWithReleases(ctx, storyID)
}
