package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// ReleaseContext is what a moderation card says about a release besides the
// release itself: the story line and the turn it reports.
type ReleaseContext struct {
	StoryTitle         string
	DevelopmentSummary string
}

// ClaimReleaseForCard takes the oldest release that needs a moderation card
// and moves it to "pending_approval" before the card is posted, so two
// pollers never post the same release twice: a "submitted" release, or one
// claimed more than retryAfter ago whose card never got posted (the upload
// failed, or hub stopped half-way). It reports false when there is none.
func (s *Store) ClaimReleaseForCard(ctx context.Context, retryAfter time.Duration) (Release, bool, error) {
	var r Release
	err := scanRelease(s.pool.QueryRow(ctx, `
		UPDATE releases SET status = 'pending_approval', updated_at = now()
		WHERE id = (
			SELECT id FROM releases
			WHERE status = 'submitted'
				OR (status = 'pending_approval' AND moderation_message_id IS NULL
					AND updated_at < now() - make_interval(secs => $1))
			ORDER BY created_at ASC
			LIMIT 1
			FOR UPDATE SKIP LOCKED
		)
		RETURNING `+releaseColumns, retryAfter.Seconds()), &r)
	if errors.Is(mapNotFound(err), ErrNotFound) {
		return Release{}, false, nil
	}
	if err != nil {
		return Release{}, false, fmt.Errorf("claiming a release for a moderation card: %w", err)
	}
	return r, true, nil
}

// SetCardMessage records the card the bot posted for a release.
func (s *Store) SetCardMessage(ctx context.Context, releaseID string, chatID, messageID int64) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE releases SET moderation_chat_id = $2, moderation_message_id = $3, updated_at = now() WHERE id = $1
	`, releaseID, chatID, messageID)
	if err != nil {
		return fmt.Errorf("recording the moderation card of release %s: %w", releaseID, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("recording the moderation card of release %s: %w", releaseID, ErrNotFound)
	}
	return nil
}

// ReleaseContextOf returns the story title and development summary of a
// release.
func (s *Store) ReleaseContextOf(ctx context.Context, releaseID string) (ReleaseContext, error) {
	var c ReleaseContext
	err := s.pool.QueryRow(ctx, `
		SELECT st.title, d.summary
		FROM releases r
		JOIN story_developments d ON d.id = r.development_id
		JOIN stories st ON st.id = d.story_id
		WHERE r.id = $1
	`, releaseID).Scan(&c.StoryTitle, &c.DevelopmentSummary)
	if err != nil {
		return ReleaseContext{}, fmt.Errorf("getting context of release %s: %w", releaseID, mapNotFound(err))
	}
	return c, nil
}

// DecideRelease moves a release that awaits approval to status (approved,
// rejected or changes_requested), recording who decided. It reports false,
// with the release as it is, if the release was not awaiting approval.
func (s *Store) DecideRelease(ctx context.Context, releaseID, status, by string) (Release, bool, error) {
	switch status {
	case "approved", "rejected", "changes_requested":
	default:
		return Release{}, false, fmt.Errorf("deciding release %s: %q is not a moderation decision", releaseID, status)
	}
	var r Release
	err := scanRelease(s.pool.QueryRow(ctx, `
		UPDATE releases
		SET status = $2, moderated_by = $3, moderated_at = now(), updated_at = now()
		WHERE id = $1 AND status = 'pending_approval'
		RETURNING `+releaseColumns, releaseID, status, by), &r)
	if err == nil {
		return r, true, nil
	}
	if !errors.Is(mapNotFound(err), ErrNotFound) {
		return Release{}, false, fmt.Errorf("deciding release %s: %w", releaseID, err)
	}
	r, err = s.GetRelease(ctx, releaseID)
	return r, false, err
}

// SetModerationNote records the moderator's reason for a rejection or a
// request for changes (the latest reply wins); a release in any other status
// takes no reason and reports ErrNotFound.
func (s *Store) SetModerationNote(ctx context.Context, releaseID, note string) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE releases SET moderation_note = $2, updated_at = now()
		WHERE id = $1 AND status IN ('rejected', 'changes_requested')
	`, releaseID, note)
	if err != nil {
		return fmt.Errorf("recording the moderation note of release %s: %w", releaseID, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("recording the moderation note of release %s: %w", releaseID, ErrNotFound)
	}
	return nil
}

// LogEvent appends to the events journal (docs/architecture.md §7).
func (s *Store) LogEvent(ctx context.Context, kind string, data any) error {
	raw, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("encoding %s event: %w", kind, err)
	}
	if _, err := s.pool.Exec(ctx, `INSERT INTO events (kind, data) VALUES ($1, $2)`, kind, raw); err != nil {
		return fmt.Errorf("logging %s event: %w", kind, err)
	}
	return nil
}
