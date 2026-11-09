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

// ToUnschedule is a video that may stand in YouTube's schedule while its
// release has been closed or held: it goes out at its slot unless someone
// takes it down. URL is empty when the release booked a slot and no link came:
// an upload may have got as far as YouTube, so look in Studio.
type ToUnschedule struct {
	ReleaseID string
	ChannelID string
	Status    string
	Hold      string
	URL       string
	PublishAt time.Time
}

// ReleasesToUnschedule returns the videos that must come off the schedule.
func (s *Store) ReleasesToUnschedule(ctx context.Context) ([]ToUnschedule, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT r.id, r.channel_id, r.status, r.publish_hold, COALESCE(p.url, ''), sl.publish_at
		FROM releases r
		JOIN release_slots sl ON sl.release_id = r.id
		LEFT JOIN release_publications p ON p.release_id = r.id AND p.platform = $1
		WHERE (r.status IN ('rejected', 'changes_requested', 'failed') OR r.publish_hold <> '') AND sl.publish_at > now()
		ORDER BY sl.publish_at ASC
	`, PlatformYouTube)
	if err != nil {
		return nil, fmt.Errorf("listing videos to take off the schedule: %w", err)
	}
	defer rows.Close()
	var out []ToUnschedule
	for rows.Next() {
		var u ToUnschedule
		if err := rows.Scan(&u.ReleaseID, &u.ChannelID, &u.Status, &u.Hold, &u.URL, &u.PublishAt); err != nil {
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
		ON CONFLICT (release_id) DO UPDATE SET publish_at = EXCLUDED.publish_at, created_at = now(),
		    rechecked_at = CASE WHEN release_slots.publish_at = EXCLUDED.publish_at THEN release_slots.rechecked_at END
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

// ForgetPublication drops a release's link on a platform, its slot and hub's
// upload job: the video is off the schedule, and the next upload starts afresh.
func (s *Store) ForgetPublication(ctx context.Context, releaseID, platform string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := forgetPublicationIn(ctx, tx, releaseID, platform); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func forgetPublicationIn(ctx context.Context, tx pgx.Tx, releaseID, platform string) error {
	if _, err := tx.Exec(ctx, `DELETE FROM release_publications WHERE release_id = $1 AND platform = $2`, releaseID, platform); err != nil {
		return fmt.Errorf("forgetting the %s link of release %s: %w", platform, releaseID, err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM release_slots WHERE release_id = $1`, releaseID); err != nil {
		return fmt.Errorf("freeing the slot of release %s: %w", releaseID, err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM publish_jobs WHERE release_id = $1`, releaseID); err != nil {
		return fmt.Errorf("forgetting the upload of release %s: %w", releaseID, err)
	}
	return nil
}

// ForgetIfOutOfPlay forgets a publication only if its release is still closed or
// held, checked under the row's lock, so a hold lifted at this moment can't lose
// the record of an upload that goes on. It reports false if the release is back
// in play, and then changes nothing.
func (s *Store) ForgetIfOutOfPlay(ctx context.Context, releaseID, platform string) (bool, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var status, hold string
	err = tx.QueryRow(ctx, `SELECT status, publish_hold FROM releases WHERE id = $1 FOR UPDATE`, releaseID).Scan(&status, &hold)
	if err != nil {
		return false, fmt.Errorf("reading release %s: %w", releaseID, mapNotFound(err))
	}
	if status == "approved" && hold == "" {
		return false, nil
	}
	if err := forgetPublicationIn(ctx, tx, releaseID, platform); err != nil {
		return false, err
	}
	return true, tx.Commit(ctx)
}

// ForgetRefused forgets an upload YouTube refused, in one step with what follows
// from it. Hub's own link goes, ownURL. If that leaves no link, the release is
// held and its slot freed, since a hold that failed after a forget would have it
// uploaded again. If a link someone else put there remains, its video is what
// goes out: its slot stays, no hold, and it reports false.
func (s *Store) ForgetRefused(ctx context.Context, releaseID, platform, ownURL, reason string) (bool, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `DELETE FROM release_publications WHERE release_id = $1 AND platform = $2 AND url = $3`, releaseID, platform, ownURL); err != nil {
		return false, fmt.Errorf("forgetting the %s link of release %s: %w", platform, releaseID, err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM publish_jobs WHERE release_id = $1`, releaseID); err != nil {
		return false, fmt.Errorf("forgetting the upload of release %s: %w", releaseID, err)
	}
	var another bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM release_publications WHERE release_id = $1 AND platform = $2)`, releaseID, platform).Scan(&another); err != nil {
		return false, fmt.Errorf("reading the %s link of release %s: %w", platform, releaseID, err)
	}
	if another {
		return false, tx.Commit(ctx)
	}
	if err := forgetPublicationIn(ctx, tx, releaseID, platform); err != nil {
		return false, err
	}
	tag, err := tx.Exec(ctx, setHoldSQL, releaseID, reason)
	if err != nil {
		return false, fmt.Errorf("holding release %s: %w", releaseID, err)
	}
	if tag.RowsAffected() == 0 {
		return false, fmt.Errorf("holding release %s: %w", releaseID, ErrNotFound)
	}
	return true, tx.Commit(ctx)
}

const setHoldSQL = `
	UPDATE releases
	SET publish_hold = $2, updated_at = now(),
	    hold_alerted_at = CASE WHEN publish_hold = $2 THEN hold_alerted_at END
	WHERE id = $1
`

// SetPublishHold keeps a release out of the queue for a reason; an empty
// reason lets it back in. A new reason is news to the group again.
func (s *Store) SetPublishHold(ctx context.Context, releaseID, reason string) error {
	tag, err := s.pool.Exec(ctx, setHoldSQL, releaseID, reason)
	if err != nil {
		return fmt.Errorf("holding release %s: %w", releaseID, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("holding release %s: %w", releaseID, ErrNotFound)
	}
	return nil
}

// PublicationCheck is a video to look at on the platform.
type PublicationCheck struct {
	ReleaseID      string
	Status         string
	Held           bool
	URL            string
	CardMessageID  *int64
	PublishAt      *time.Time // nil: uploaded without a slot recorded
	OverdueAlerted bool
	ClosedAlerted  bool
}

// PublicationsToCheck returns the videos to look at on the platform, once
// their slot has come: those of approved releases hub hasn't yet seen go up,
// and, for a day after the slot, those that must not go up: their release has
// been closed or held since. The group hasn't been told about these.
func (s *Store) PublicationsToCheck(ctx context.Context) ([]PublicationCheck, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT r.id, r.status, r.publish_hold <> '', p.url, r.moderation_message_id, sl.publish_at,
		       p.overdue_alerted_at IS NOT NULL, p.closed_alerted_at IS NOT NULL
		FROM release_publications p
		JOIN releases r ON r.id = p.release_id
		LEFT JOIN release_slots sl ON sl.release_id = r.id
		WHERE p.platform = $1
		  AND (sl.publish_at IS NULL OR sl.publish_at <= now())
		  AND ((r.status = 'approved' AND p.live_at IS NULL)
		       OR ((r.status IN ('rejected', 'changes_requested', 'failed') OR (r.status = 'approved' AND r.publish_hold <> ''))
		           AND p.closed_alerted_at IS NULL AND sl.publish_at > now() - interval '1 day'))
		ORDER BY r.created_at ASC
	`, PlatformYouTube)
	if err != nil {
		return nil, fmt.Errorf("listing publications to check: %w", err)
	}
	defer rows.Close()
	var out []PublicationCheck
	for rows.Next() {
		var c PublicationCheck
		if err := rows.Scan(&c.ReleaseID, &c.Status, &c.Held, &c.URL, &c.CardMessageID, &c.PublishAt, &c.OverdueAlerted, &c.ClosedAlerted); err != nil {
			return nil, fmt.Errorf("scanning a publication to check: %w", err)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// UploadAlert is what the group must hear about a release in the queue: it is
// held (Hold says why), or it has waited too long for its upload.
type UploadAlert struct {
	ReleaseID     string
	ChannelID     string
	CardMessageID *int64
	ApprovedAt    time.Time
	Hold          string
	// MayBeScheduled: a link or a booked time exists, so the video may stand in
	// YouTube's schedule and go out in spite of the hold.
	MayBeScheduled bool
}

// UploadAlerts returns what the group hasn't been told yet: automatic approvals
// held back, and those not held and with no link since before `before`.
func (s *Store) UploadAlerts(ctx context.Context, before time.Time) ([]UploadAlert, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, channel_id, moderation_message_id, moderated_at, publish_hold,
		       EXISTS (SELECT 1 FROM release_publications p WHERE p.release_id = releases.id AND p.platform = $2)
		       OR EXISTS (SELECT 1 FROM release_slots sl WHERE sl.release_id = releases.id)
		FROM releases
		WHERE status = 'approved' AND moderated_by = $1 AND $2 = ANY (allowed_platforms)
		  AND ((publish_hold <> '' AND hold_alerted_at IS NULL)
		       OR (publish_hold = '' AND upload_alerted_at IS NULL AND moderated_at < $3
		           AND NOT EXISTS (SELECT 1 FROM release_publications p WHERE p.release_id = releases.id AND p.platform = $2)))
		ORDER BY created_at ASC
	`, AutoApprover, PlatformYouTube, before)
	if err != nil {
		return nil, fmt.Errorf("listing upload alerts: %w", err)
	}
	defer rows.Close()
	var out []UploadAlert
	for rows.Next() {
		var u UploadAlert
		if err := rows.Scan(&u.ReleaseID, &u.ChannelID, &u.CardMessageID, &u.ApprovedAt, &u.Hold, &u.MayBeScheduled); err != nil {
			return nil, fmt.Errorf("scanning an upload alert: %w", err)
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// MarkUploadAlerted records that the group has been told a release's upload stalled.
func (s *Store) MarkUploadAlerted(ctx context.Context, releaseID string) error {
	if _, err := s.pool.Exec(ctx, `UPDATE releases SET upload_alerted_at = now() WHERE id = $1`, releaseID); err != nil {
		return fmt.Errorf("marking the upload alert of release %s: %w", releaseID, err)
	}
	return nil
}

// MarkHoldAlerted records that the group has been told a release is held.
func (s *Store) MarkHoldAlerted(ctx context.Context, releaseID string) error {
	if _, err := s.pool.Exec(ctx, `UPDATE releases SET hold_alerted_at = now() WHERE id = $1`, releaseID); err != nil {
		return fmt.Errorf("marking the hold alert of release %s: %w", releaseID, err)
	}
	return nil
}

// SlotOf returns the time a release holds, or nil.
func (s *Store) SlotOf(ctx context.Context, releaseID string) (*time.Time, error) {
	var at time.Time
	err := s.pool.QueryRow(ctx, `SELECT publish_at FROM release_slots WHERE release_id = $1`, releaseID).Scan(&at)
	if errors.Is(mapNotFound(err), ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading the slot of release %s: %w", releaseID, err)
	}
	return &at, nil
}

// PublishHold returns why a release is held back, or "".
func (s *Store) PublishHold(ctx context.Context, releaseID string) (string, error) {
	var reason string
	err := s.pool.QueryRow(ctx, `SELECT publish_hold FROM releases WHERE id = $1`, releaseID).Scan(&reason)
	if err != nil {
		return "", fmt.Errorf("reading the hold of release %s: %w", releaseID, mapNotFound(err))
	}
	return reason, nil
}

// PublicationOwner returns the release a link on a platform is recorded for, or "".
func (s *Store) PublicationOwner(ctx context.Context, platform, url string) (string, error) {
	var id string
	err := s.pool.QueryRow(ctx, `SELECT release_id FROM release_publications WHERE platform = $1 AND url = $2 LIMIT 1`, platform, url).Scan(&id)
	if errors.Is(mapNotFound(err), ErrNotFound) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("looking up the %s link %s: %w", platform, url, err)
	}
	return id, nil
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

// MarkPublicationClosedAlerted records that the group has been told a video
// came out though its release is closed or held, so it is told once.
func (s *Store) MarkPublicationClosedAlerted(ctx context.Context, releaseID, platform string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE release_publications SET closed_alerted_at = now()
		WHERE release_id = $1 AND platform = $2 AND closed_alerted_at IS NULL
	`, releaseID, platform)
	if err != nil {
		return fmt.Errorf("marking the %s video of release %s as out though closed: %w", platform, releaseID, err)
	}
	return nil
}
