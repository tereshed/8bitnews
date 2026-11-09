package store

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// PlatformYouTube names YouTube in release_publications.
const PlatformYouTube = "youtube"

// AdminRelease is one row of the admin page's list: every release, without
// the big reports.
type AdminRelease struct {
	ID          string
	ChannelID   string
	Status      string
	StoryTitle  string
	Development string
	Title       string // the video's YouTube title; empty if the script has none
	CoverKey    string
	DurationS   float64
	CreatedAt   time.Time
	ModeratedBy string
	ModeratedAt *time.Time
	Note        string
	YouTubeURL  string
	// AllowedPlatforms: where Legal let the video go.
	AllowedPlatforms []string
}

// AdminReleases returns the releases of every channel, newest first: the n most
// recent, and any older one still waiting for someone, so that nothing open
// drops off the page.
func (s *Store) AdminReleases(ctx context.Context, n int) ([]AdminRelease, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT r.id, r.channel_id, r.status, st.title, d.summary,
		       COALESCE(r.script #>> '{metadata,youtube,title}', ''),
		       COALESCE(r.media ->> 'cover', ''),
		       CASE WHEN jsonb_typeof(r.video_package -> 'duration_s') = 'number'
		            THEN (r.video_package ->> 'duration_s')::float8 ELSE 0 END,
		       r.created_at, r.moderated_by, r.moderated_at, r.moderation_note,
		       COALESCE(p.url, ''), r.allowed_platforms
		FROM releases r
		JOIN story_developments d ON d.id = r.development_id
		JOIN stories st ON st.id = d.story_id
		LEFT JOIN release_publications p ON p.release_id = r.id AND p.platform = $2
		WHERE r.status IN ('submitted', 'pending_approval', 'approved')
		   OR r.id IN (SELECT id FROM releases ORDER BY created_at DESC LIMIT $1)
		ORDER BY r.created_at DESC
	`, n, PlatformYouTube)
	if err != nil {
		return nil, fmt.Errorf("listing releases for the admin page: %w", err)
	}
	defer rows.Close()

	var out []AdminRelease
	for rows.Next() {
		var r AdminRelease
		if err := rows.Scan(&r.ID, &r.ChannelID, &r.Status, &r.StoryTitle, &r.Development, &r.Title, &r.CoverKey,
			&r.DurationS, &r.CreatedAt, &r.ModeratedBy, &r.ModeratedAt, &r.Note, &r.YouTubeURL, &r.AllowedPlatforms); err != nil {
			return nil, fmt.Errorf("scanning a release for the admin page: %w", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating releases for the admin page: %w", err)
	}
	return out, nil
}

// InProgress is a story the newsroom picked that has no release yet.
type InProgress struct {
	ChannelID   string
	StoryTitle  string
	Development string
	CreatedAt   time.Time
}

// DevelopmentsInProgress returns the developments registered in the last
// `since` that have no release yet, newest first. hub doesn't see a run's
// stage, only that a story was picked and no video has come back.
func (s *Store) DevelopmentsInProgress(ctx context.Context, since time.Duration) ([]InProgress, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT st.channel_id, st.title, d.summary, d.created_at
		FROM story_developments d
		JOIN stories st ON st.id = d.story_id
		WHERE d.created_at > $1
		  AND NOT EXISTS (SELECT 1 FROM releases r WHERE r.development_id = d.id)
		ORDER BY d.created_at DESC
	`, time.Now().Add(-since))
	if err != nil {
		return nil, fmt.Errorf("listing developments in progress: %w", err)
	}
	defer rows.Close()

	var out []InProgress
	for rows.Next() {
		var p InProgress
		if err := rows.Scan(&p.ChannelID, &p.StoryTitle, &p.Development, &p.CreatedAt); err != nil {
			return nil, fmt.Errorf("scanning a development in progress: %w", err)
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating developments in progress: %w", err)
	}
	return out, nil
}

// SetPublicationURL records where a release went up on a platform, or, with
// an empty url, forgets it.
func (s *Store) SetPublicationURL(ctx context.Context, releaseID, platform, url, by string) error {
	if url == "" {
		if _, err := s.pool.Exec(ctx, `DELETE FROM release_publications WHERE release_id = $1 AND platform = $2`, releaseID, platform); err != nil {
			return fmt.Errorf("forgetting the %s link of release %s: %w", platform, releaseID, err)
		}
		return nil
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO release_publications (release_id, platform, url, set_by)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (release_id, platform) DO UPDATE
		SET url = EXCLUDED.url, set_by = EXCLUDED.set_by, set_at = now(),
		    live_at = CASE WHEN release_publications.url = EXCLUDED.url THEN release_publications.live_at END,
		    overdue_alerted_at = CASE WHEN release_publications.url = EXCLUDED.url THEN release_publications.overdue_alerted_at END
	`, releaseID, platform, url, by)
	if err != nil {
		return fmt.Errorf("recording the %s link of release %s: %w", platform, releaseID, err)
	}
	return nil
}

// AdminSessionEpoch returns the number a moderator's admin sessions must carry
// to count: signing out raises it, which ends them all on every device.
func (s *Store) AdminSessionEpoch(ctx context.Context, userID int64) (int, error) {
	var epoch int
	err := s.pool.QueryRow(ctx, `SELECT epoch FROM admin_session_epochs WHERE telegram_user_id = $1`, userID).Scan(&epoch)
	if err != nil {
		if errors.Is(mapNotFound(err), ErrNotFound) {
			return 0, nil
		}
		return 0, fmt.Errorf("reading the session epoch of user %d: %w", userID, err)
	}
	return epoch, nil
}

// RaiseAdminSessionEpoch ends a moderator's admin sessions.
func (s *Store) RaiseAdminSessionEpoch(ctx context.Context, userID int64) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO admin_session_epochs (telegram_user_id, epoch) VALUES ($1, 1)
		ON CONFLICT (telegram_user_id) DO UPDATE
		SET epoch = admin_session_epochs.epoch + 1, updated_at = now()
	`, userID)
	if err != nil {
		return fmt.Errorf("raising the session epoch of user %d: %w", userID, err)
	}
	return nil
}

// PublicationURL returns where a release went up on a platform, or "".
func (s *Store) PublicationURL(ctx context.Context, releaseID, platform string) (string, error) {
	var url string
	err := s.pool.QueryRow(ctx, `SELECT url FROM release_publications WHERE release_id = $1 AND platform = $2`, releaseID, platform).Scan(&url)
	if err != nil {
		if errors.Is(mapNotFound(err), ErrNotFound) {
			return "", nil
		}
		return "", fmt.Errorf("reading the %s link of release %s: %w", platform, releaseID, err)
	}
	return url, nil
}
