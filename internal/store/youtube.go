package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// PlatformAccount is a channel's account on a platform and hub's access to it.
type PlatformAccount struct {
	ChannelID  string
	Platform   string
	ExternalID string // on YouTube, the UC… id
	Tokens     AccountTokens
	// AuthAlertedAt: the access stopped working and the group has been told.
	// hub leaves the channel alone until someone connects it again.
	AuthAlertedAt *time.Time
}

// AccountTokens is oauth_tokens. RefreshSealed is encrypted (youtube.Sealer):
// the database and its backups never hold the token itself.
type AccountTokens struct {
	RefreshSealed string    `json:"refresh_sealed"`
	Title         string    `json:"title,omitempty"`
	Handle        string    `json:"handle,omitempty"`
	ConnectedBy   string    `json:"connected_by,omitempty"`
	ConnectedAt   time.Time `json:"connected_at"`
}

const platformAccountColumns = `channel_id, platform, external_account_id, oauth_tokens, auth_alerted_at`

func scanPlatformAccount(row rowScanner) (PlatformAccount, error) {
	var (
		a   PlatformAccount
		raw json.RawMessage
	)
	if err := row.Scan(&a.ChannelID, &a.Platform, &a.ExternalID, &raw, &a.AuthAlertedAt); err != nil {
		return a, err
	}
	if err := json.Unmarshal(raw, &a.Tokens); err != nil {
		return a, fmt.Errorf("decoding the tokens of %s on %s: %w", a.ChannelID, a.Platform, err)
	}
	return a, nil
}

// SetPlatformAccount connects a channel to its account; connecting again
// replaces the access and clears any alert about the old one.
func (s *Store) SetPlatformAccount(ctx context.Context, a PlatformAccount) error {
	raw, err := json.Marshal(a.Tokens)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `
		INSERT INTO platform_accounts (channel_id, platform, external_account_id, oauth_tokens)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (channel_id, platform) DO UPDATE
		SET external_account_id = EXCLUDED.external_account_id, oauth_tokens = EXCLUDED.oauth_tokens, auth_alerted_at = NULL
	`, a.ChannelID, a.Platform, a.ExternalID, raw)
	if err != nil {
		return fmt.Errorf("connecting %s to %s: %w", a.ChannelID, a.Platform, err)
	}
	return nil
}

// PlatformAccountOf returns a channel's account on a platform, or ErrNotFound.
func (s *Store) PlatformAccountOf(ctx context.Context, channelID, platform string) (PlatformAccount, error) {
	a, err := scanPlatformAccount(s.pool.QueryRow(ctx, `
		SELECT `+platformAccountColumns+` FROM platform_accounts WHERE channel_id = $1 AND platform = $2
	`, channelID, platform))
	if err != nil {
		return PlatformAccount{}, fmt.Errorf("reading the %s account of %s: %w", platform, channelID, mapNotFound(err))
	}
	return a, nil
}

// ConnectedAccounts returns the accounts hub can use on a platform: connected,
// and with no alert that the access has stopped working.
func (s *Store) ConnectedAccounts(ctx context.Context, platform string) ([]PlatformAccount, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+platformAccountColumns+` FROM platform_accounts
		WHERE platform = $1 AND oauth_tokens ->> 'refresh_sealed' <> '' AND auth_alerted_at IS NULL
		ORDER BY channel_id
	`, platform)
	if err != nil {
		return nil, fmt.Errorf("listing the %s accounts: %w", platform, err)
	}
	defer rows.Close()
	var out []PlatformAccount
	for rows.Next() {
		a, err := scanPlatformAccount(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// DeletePlatformAccount disconnects a channel.
func (s *Store) DeletePlatformAccount(ctx context.Context, channelID, platform string) error {
	if _, err := s.pool.Exec(ctx, `DELETE FROM platform_accounts WHERE channel_id = $1 AND platform = $2`, channelID, platform); err != nil {
		return fmt.Errorf("disconnecting %s from %s: %w", channelID, platform, err)
	}
	return nil
}

// MarkAccountAlerted records that the group has been told an account's access
// stopped working, so it is told once, and hub stops using it.
func (s *Store) MarkAccountAlerted(ctx context.Context, channelID, platform string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE platform_accounts SET auth_alerted_at = now()
		WHERE channel_id = $1 AND platform = $2 AND auth_alerted_at IS NULL
	`, channelID, platform)
	if err != nil {
		return fmt.Errorf("marking the %s account of %s: %w", platform, channelID, err)
	}
	return nil
}

// ListChannels returns every channel, in id order.
func (s *Store) ListChannels(ctx context.Context) ([]Channel, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, country_cc, name, language, timezone, platforms, format, format_md, metadata_md, data, updated_at
		FROM channels ORDER BY id
	`)
	if err != nil {
		return nil, fmt.Errorf("listing channels: %w", err)
	}
	defer rows.Close()
	var out []Channel
	for rows.Next() {
		var c Channel
		if err := rows.Scan(&c.ID, &c.CountryCC, &c.Name, &c.Language, &c.Timezone, &c.Platforms, &c.Format, &c.FormatMD, &c.MetadataMD, &c.Data, &c.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scanning a channel: %w", err)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// PublishJob is how far hub's own upload of a release has got. A session URI is
// stored before the first byte goes out and a video id as soon as it is known,
// so a restart resumes instead of uploading twice.
type PublishJob struct {
	ReleaseID         string
	SessionURI        string
	VideoSize         int64
	PublishAt         *time.Time // what the session asked YouTube for
	VideoID           string
	ThumbnailSet      bool
	ThumbnailAttempts int
	Attempts          int
	NextAttemptAt     time.Time
	LastError         string
	// AnnouncedAt: the group has heard how this upload came out.
	AnnouncedAt *time.Time
	// Saved: the row exists. A release with a slot and no row got its slot from
	// somewhere other than hub's uploader.
	Saved bool
	// ChannelID is filled in by UnannouncedJobs.
	ChannelID string
}

const publishJobColumns = `release_id, session_uri, video_size, publish_at, video_id, thumbnail_set, thumbnail_attempts, attempts, next_attempt_at, last_error, announced_at`

func scanPublishJob(row rowScanner) (PublishJob, error) {
	j := PublishJob{Saved: true}
	err := row.Scan(&j.ReleaseID, &j.SessionURI, &j.VideoSize, &j.PublishAt, &j.VideoID, &j.ThumbnailSet, &j.ThumbnailAttempts, &j.Attempts, &j.NextAttemptAt, &j.LastError, &j.AnnouncedAt)
	return j, err
}

// PublishJobOf returns a release's job, or a blank one if it has none yet. The
// blank one is due now, and saving it creates the row.
func (s *Store) PublishJobOf(ctx context.Context, releaseID string) (PublishJob, error) {
	j, err := scanPublishJob(s.pool.QueryRow(ctx, `SELECT `+publishJobColumns+` FROM publish_jobs WHERE release_id = $1`, releaseID))
	if errors.Is(mapNotFound(err), ErrNotFound) {
		return PublishJob{ReleaseID: releaseID}, nil
	}
	if err != nil {
		return PublishJob{}, fmt.Errorf("reading the publish job of release %s: %w", releaseID, err)
	}
	return j, nil
}

// UnannouncedJobs returns the uploads that are up and whose outcome the group
// hasn't heard, of releases still approved and not held.
func (s *Store) UnannouncedJobs(ctx context.Context) ([]PublishJob, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT j.release_id, j.session_uri, j.video_size, j.publish_at, j.video_id, j.thumbnail_set, j.thumbnail_attempts,
		       j.attempts, j.next_attempt_at, j.last_error, j.announced_at, r.channel_id
		FROM publish_jobs j
		JOIN releases r ON r.id = j.release_id
		WHERE j.video_id <> '' AND j.announced_at IS NULL AND r.status = 'approved' AND r.publish_hold = ''
		ORDER BY j.created_at ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("listing unannounced uploads: %w", err)
	}
	defer rows.Close()
	var out []PublishJob
	for rows.Next() {
		j := PublishJob{Saved: true}
		if err := rows.Scan(&j.ReleaseID, &j.SessionURI, &j.VideoSize, &j.PublishAt, &j.VideoID, &j.ThumbnailSet, &j.ThumbnailAttempts,
			&j.Attempts, &j.NextAttemptAt, &j.LastError, &j.AnnouncedAt, &j.ChannelID); err != nil {
			return nil, fmt.Errorf("scanning an unannounced upload: %w", err)
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

// SavePublishJob writes a job as it stands.
func (s *Store) SavePublishJob(ctx context.Context, j PublishJob) error {
	next := j.NextAttemptAt
	if next.IsZero() {
		next = time.Now()
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO publish_jobs (release_id, session_uri, video_size, publish_at, video_id, thumbnail_set, thumbnail_attempts, attempts, next_attempt_at, last_error, announced_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		ON CONFLICT (release_id) DO UPDATE
		SET session_uri = EXCLUDED.session_uri, video_size = EXCLUDED.video_size, publish_at = EXCLUDED.publish_at, video_id = EXCLUDED.video_id,
		    thumbnail_set = EXCLUDED.thumbnail_set, thumbnail_attempts = EXCLUDED.thumbnail_attempts,
		    attempts = EXCLUDED.attempts, next_attempt_at = EXCLUDED.next_attempt_at, last_error = EXCLUDED.last_error,
		    announced_at = EXCLUDED.announced_at, updated_at = now()
	`, j.ReleaseID, j.SessionURI, j.VideoSize, j.PublishAt, j.VideoID, j.ThumbnailSet, j.ThumbnailAttempts, j.Attempts, next, j.LastError, j.AnnouncedAt)
	if err != nil {
		return fmt.Errorf("saving the publish job of release %s: %w", j.ReleaseID, err)
	}
	return nil
}

// SetPublicationURLIfNone records hub's own upload, unless a moderator has
// already put a link there: theirs stays.
func (s *Store) SetPublicationURLIfNone(ctx context.Context, releaseID, platform, url, by string) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO release_publications (release_id, platform, url, set_by) VALUES ($1, $2, $3, $4)
		ON CONFLICT (release_id, platform) DO NOTHING
	`, releaseID, platform, url, by)
	if err != nil {
		return fmt.Errorf("recording the %s link of release %s: %w", platform, releaseID, err)
	}
	return nil
}

// RecheckDue is a video whose slot is near and that hasn't been read back again.
type RecheckDue struct {
	ReleaseID string
	ChannelID string
	URL       string
	PublishAt time.Time
}

// SlotsToRecheck returns the automatically approved, unheld releases with a
// link whose slot comes by `before` and hasn't been reached.
func (s *Store) SlotsToRecheck(ctx context.Context, before time.Time) ([]RecheckDue, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT r.id, r.channel_id, p.url, sl.publish_at
		FROM release_slots sl
		JOIN releases r ON r.id = sl.release_id
		JOIN release_publications p ON p.release_id = r.id AND p.platform = $1
		WHERE sl.rechecked_at IS NULL AND sl.publish_at > now() AND sl.publish_at <= $2
		  AND r.status = 'approved' AND r.publish_hold = '' AND r.moderated_by = $3
		ORDER BY sl.publish_at ASC
	`, PlatformYouTube, before, AutoApprover)
	if err != nil {
		return nil, fmt.Errorf("listing slots to recheck: %w", err)
	}
	defer rows.Close()
	var out []RecheckDue
	for rows.Next() {
		var d RecheckDue
		if err := rows.Scan(&d.ReleaseID, &d.ChannelID, &d.URL, &d.PublishAt); err != nil {
			return nil, fmt.Errorf("scanning a slot to recheck: %w", err)
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// MarkSlotRechecked records the read-back before the slot, so it happens once.
func (s *Store) MarkSlotRechecked(ctx context.Context, releaseID string) error {
	if _, err := s.pool.Exec(ctx, `UPDATE release_slots SET rechecked_at = now() WHERE release_id = $1`, releaseID); err != nil {
		return fmt.Errorf("marking the slot of release %s rechecked: %w", releaseID, err)
	}
	return nil
}

// publisherLockKey is the Postgres advisory lock of hub's uploader.
const publisherLockKey int64 = 0x8B17_0010

// Lock is held on a connection of its own: the lock lasts as long as it does.
type Lock interface {
	Alive(ctx context.Context) bool
	Release(ctx context.Context)
}

type advisoryLock struct {
	conn *pgx.Conn
}

// LockPublisher takes the lock that lets one process upload. It reports false
// if another process holds it.
func (s *Store) LockPublisher(ctx context.Context) (Lock, bool, error) {
	conn, err := pgx.ConnectConfig(ctx, s.pool.Config().ConnConfig)
	if err != nil {
		return nil, false, fmt.Errorf("connecting for the publisher's lock: %w", err)
	}
	var got bool
	if err := conn.QueryRow(ctx, `SELECT pg_try_advisory_lock($1)`, publisherLockKey).Scan(&got); err != nil {
		_ = conn.Close(ctx)
		return nil, false, fmt.Errorf("taking the publisher's lock: %w", err)
	}
	if !got {
		_ = conn.Close(ctx)
		return nil, false, nil
	}
	return &advisoryLock{conn: conn}, true, nil
}

func (l *advisoryLock) Alive(ctx context.Context) bool {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return l.conn.Ping(ctx) == nil
}

// Release closes the connection, and with it the lock.
func (l *advisoryLock) Release(ctx context.Context) {
	_ = l.conn.Close(ctx)
}
