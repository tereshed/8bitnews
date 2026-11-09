package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// AutoApprover signs the approval of a release on a channel that doesn't ask
// for a moderator's (channel.yaml moderation.approval_required: false).
const AutoApprover = "auto"

// ErrSlotTaken: another release of the channel holds that time.
var ErrSlotTaken = errors.New("the channel's slot is taken")

// slotAttempts is how many times ReserveSlot picks again when another
// release takes the same time first.
const slotAttempts = 5

// ReleasesAwaitingDecision returns the releases whose card is out and that
// wait for a decision, oldest first.
func (s *Store) ReleasesAwaitingDecision(ctx context.Context) ([]Release, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+releaseColumns+` FROM releases
		WHERE status = 'pending_approval' AND moderation_message_id IS NOT NULL
		ORDER BY created_at ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("listing releases awaiting a decision: %w", err)
	}
	return collectReleases(rows)
}

// PublishQueue returns what is waiting to go up on YouTube, oldest first:
// releases approved automatically, that Legal let onto YouTube, not held back
// and with no link yet. A release a moderator approved by hand isn't here: it
// may already be up, with its link not recorded.
func (s *Store) PublishQueue(ctx context.Context) ([]Release, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+releaseColumns+` FROM releases
		WHERE status = 'approved' AND moderated_by = $1 AND publish_hold = ''
		  AND $2 = ANY (allowed_platforms)
		  AND NOT EXISTS (SELECT 1 FROM release_publications p WHERE p.release_id = releases.id AND p.platform = $2)
		ORDER BY created_at ASC
	`, AutoApprover, PlatformYouTube)
	if err != nil {
		return nil, fmt.Errorf("listing the publish queue: %w", err)
	}
	return collectReleases(rows)
}

func collectReleases(rows pgx.Rows) ([]Release, error) {
	defer rows.Close()
	var out []Release
	for rows.Next() {
		var r Release
		if err := scanRelease(rows, &r); err != nil {
			return nil, fmt.Errorf("scanning a release: %w", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating releases: %w", err)
	}
	return out, nil
}

// ToUnschedule is a video standing in YouTube's schedule whose release has
// since been closed: it goes out at its slot unless someone takes it down.
type ToUnschedule struct {
	ReleaseID string
	ChannelID string
	Status    string
	URL       string
	PublishAt time.Time
}

// ReleasesToUnschedule returns the videos that must come off the schedule.
func (s *Store) ReleasesToUnschedule(ctx context.Context) ([]ToUnschedule, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT r.id, r.channel_id, r.status, p.url, sl.publish_at
		FROM releases r
		JOIN release_publications p ON p.release_id = r.id AND p.platform = $1
		JOIN release_slots sl ON sl.release_id = r.id
		WHERE r.status IN ('rejected', 'changes_requested', 'failed') AND sl.publish_at > now()
		ORDER BY sl.publish_at ASC
	`, PlatformYouTube)
	if err != nil {
		return nil, fmt.Errorf("listing videos to take off the schedule: %w", err)
	}
	defer rows.Close()
	var out []ToUnschedule
	for rows.Next() {
		var u ToUnschedule
		if err := rows.Scan(&u.ReleaseID, &u.ChannelID, &u.Status, &u.URL, &u.PublishAt); err != nil {
			return nil, fmt.Errorf("scanning a video to take off the schedule: %w", err)
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// ReserveSlot gives a release its time on the channel: the one it already
// holds if that is still after notBefore, otherwise what pick chooses, given
// the channel's times taken from notBefore on.
func (s *Store) ReserveSlot(ctx context.Context, releaseID, channelID string, notBefore time.Time,
	pick func(taken map[int64]bool) (time.Time, error)) (time.Time, error) {
	var at time.Time
	err := s.pool.QueryRow(ctx, `SELECT publish_at FROM release_slots WHERE release_id = $1`, releaseID).Scan(&at)
	if err == nil && at.After(notBefore) {
		return at, nil
	}
	if err != nil && !errors.Is(mapNotFound(err), ErrNotFound) {
		return time.Time{}, fmt.Errorf("reading the slot of release %s: %w", releaseID, err)
	}
	for attempt := 0; attempt < slotAttempts; attempt++ {
		taken, err := s.takenSlots(ctx, channelID, notBefore)
		if err != nil {
			return time.Time{}, err
		}
		at, err := pick(taken)
		if err != nil {
			return time.Time{}, err
		}
		err = s.SetSlot(ctx, releaseID, channelID, at)
		if err == nil {
			return at, nil
		}
		if !errors.Is(err, ErrSlotTaken) {
			return time.Time{}, err
		}
	}
	return time.Time{}, fmt.Errorf("reserving a slot for release %s: %w", releaseID, ErrSlotTaken)
}

func (s *Store) takenSlots(ctx context.Context, channelID string, from time.Time) (map[int64]bool, error) {
	rows, err := s.pool.Query(ctx, `SELECT publish_at FROM release_slots WHERE channel_id = $1 AND publish_at >= $2`, channelID, from)
	if err != nil {
		return nil, fmt.Errorf("listing the slots of channel %s: %w", channelID, err)
	}
	defer rows.Close()
	taken := map[int64]bool{}
	for rows.Next() {
		var at time.Time
		if err := rows.Scan(&at); err != nil {
			return nil, fmt.Errorf("scanning a slot: %w", err)
		}
		taken[at.Unix()] = true
	}
	return taken, rows.Err()
}

// SetSlot records a release's time on its channel, reporting ErrSlotTaken if
// another release of the channel holds it.
func (s *Store) SetSlot(ctx context.Context, releaseID, channelID string, at time.Time) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO release_slots (release_id, channel_id, publish_at) VALUES ($1, $2, $3)
		ON CONFLICT (release_id) DO UPDATE SET publish_at = EXCLUDED.publish_at, created_at = now()
	`, releaseID, channelID, at)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == uniqueViolationCode {
		return ErrSlotTaken
	}
	if err != nil {
		return fmt.Errorf("recording the slot of release %s: %w", releaseID, err)
	}
	return nil
}

// ForgetPublication drops a release's link on a platform and its slot: the
// video is off the schedule.
func (s *Store) ForgetPublication(ctx context.Context, releaseID, platform string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `DELETE FROM release_publications WHERE release_id = $1 AND platform = $2`, releaseID, platform); err != nil {
		return fmt.Errorf("forgetting the %s link of release %s: %w", platform, releaseID, err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM release_slots WHERE release_id = $1`, releaseID); err != nil {
		return fmt.Errorf("freeing the slot of release %s: %w", releaseID, err)
	}
	return tx.Commit(ctx)
}

// SetPublishHold keeps a release out of the queue for a reason; an empty
// reason lets it back in.
func (s *Store) SetPublishHold(ctx context.Context, releaseID, reason string) error {
	tag, err := s.pool.Exec(ctx, `UPDATE releases SET publish_hold = $2, updated_at = now() WHERE id = $1`, releaseID, reason)
	if err != nil {
		return fmt.Errorf("holding release %s: %w", releaseID, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("holding release %s: %w", releaseID, ErrNotFound)
	}
	return nil
}

// PublicationCheck is a video whose going up hub still has to see.
type PublicationCheck struct {
	ReleaseID      string
	URL            string
	CardMessageID  *int64
	PublishAt      *time.Time // nil: uploaded without a slot recorded
	OverdueAlerted bool
}

// PublicationsToCheck returns the approved releases with a YouTube link that
// hub hasn't yet seen go up and whose slot, if any, has come.
func (s *Store) PublicationsToCheck(ctx context.Context) ([]PublicationCheck, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT r.id, p.url, r.moderation_message_id, sl.publish_at, p.overdue_alerted_at IS NOT NULL
		FROM release_publications p
		JOIN releases r ON r.id = p.release_id
		LEFT JOIN release_slots sl ON sl.release_id = r.id
		WHERE p.platform = $1 AND r.status = 'approved' AND p.live_at IS NULL
		  AND (sl.publish_at IS NULL OR sl.publish_at <= now())
		ORDER BY r.created_at ASC
	`, PlatformYouTube)
	if err != nil {
		return nil, fmt.Errorf("listing publications to check: %w", err)
	}
	defer rows.Close()
	var out []PublicationCheck
	for rows.Next() {
		var c PublicationCheck
		if err := rows.Scan(&c.ReleaseID, &c.URL, &c.CardMessageID, &c.PublishAt, &c.OverdueAlerted); err != nil {
			return nil, fmt.Errorf("scanning a publication to check: %w", err)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// MarkPublicationOverdue records that the group has been told a video didn't
// go up at its slot, so it is told once.
func (s *Store) MarkPublicationOverdue(ctx context.Context, releaseID, platform string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE release_publications SET overdue_alerted_at = now()
		WHERE release_id = $1 AND platform = $2 AND overdue_alerted_at IS NULL
	`, releaseID, platform)
	if err != nil {
		return fmt.Errorf("marking the %s upload of release %s overdue: %w", platform, releaseID, err)
	}
	return nil
}
