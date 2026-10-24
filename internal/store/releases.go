package store

import (
	"context"
	"encoding/json"
	"fmt"
)

// terminalReleaseStatuses are the statuses release_submit treats as "no
// longer blocking a new release for the same development"
// (docs/architecture.md §7: rejected, changes_requested, failed).
var terminalReleaseStatuses = map[string]bool{
	"rejected":          true,
	"changes_requested": true,
	"failed":            true,
}

// NewReleaseParams is the input to CreateRelease: everything release_submit
// validated and wants stored.
type NewReleaseParams struct {
	DevelopmentID    string
	ChannelID        string
	ExperimentID     *string
	ArmID            string
	Media            json.RawMessage
	ResearchDossier  json.RawMessage
	Script           json.RawMessage
	VideoPackage     json.RawMessage
	ValidationReport json.RawMessage
	LegalReview      json.RawMessage
	AllowedPlatforms []string
}

// CreateRelease inserts a new release in status "submitted".
func (s *Store) CreateRelease(ctx context.Context, p NewReleaseParams) (Release, error) {
	allowed := p.AllowedPlatforms
	if allowed == nil {
		allowed = []string{}
	}
	var r Release
	err := s.pool.QueryRow(ctx, `
		INSERT INTO releases (
			development_id, channel_id, experiment_id, arm_id, status,
			media, research_dossier, script, video_package, validation_report, legal_review,
			allowed_platforms
		)
		VALUES ($1, $2, $3, $4, 'submitted', $5, $6, $7, $8, $9, $10, $11)
		RETURNING id, development_id, channel_id, experiment_id, arm_id, status,
			media, research_dossier, script, video_package, validation_report, legal_review,
			allowed_platforms, created_at, updated_at
	`,
		p.DevelopmentID, p.ChannelID, p.ExperimentID, p.ArmID,
		nonNilJSON(p.Media), nonNilJSON(p.ResearchDossier), nonNilJSON(p.Script),
		nonNilJSON(p.VideoPackage), nonNilJSON(p.ValidationReport), nonNilJSON(p.LegalReview),
		allowed,
	).Scan(
		&r.ID, &r.DevelopmentID, &r.ChannelID, &r.ExperimentID, &r.ArmID, &r.Status,
		&r.Media, &r.ResearchDossier, &r.Script, &r.VideoPackage, &r.ValidationReport, &r.LegalReview,
		&r.AllowedPlatforms, &r.CreatedAt, &r.UpdatedAt,
	)
	if err != nil {
		return Release{}, fmt.Errorf("creating release: %w", err)
	}
	return r, nil
}

// GetRelease returns a release by id.
func (s *Store) GetRelease(ctx context.Context, id string) (Release, error) {
	var r Release
	err := s.pool.QueryRow(ctx, `
		SELECT id, development_id, channel_id, experiment_id, arm_id, status,
			media, research_dossier, script, video_package, validation_report, legal_review,
			allowed_platforms, created_at, updated_at
		FROM releases WHERE id = $1
	`, id).Scan(
		&r.ID, &r.DevelopmentID, &r.ChannelID, &r.ExperimentID, &r.ArmID, &r.Status,
		&r.Media, &r.ResearchDossier, &r.Script, &r.VideoPackage, &r.ValidationReport, &r.LegalReview,
		&r.AllowedPlatforms, &r.CreatedAt, &r.UpdatedAt,
	)
	if err != nil {
		return Release{}, fmt.Errorf("getting release %s: %w", id, mapNotFound(err))
	}
	return r, nil
}

// ActiveReleaseExists reports whether developmentID already has a release
// that is not in a terminal status (rejected, changes_requested, failed).
// release_submit must refuse a second active release for the same turn of
// the story.
func (s *Store) ActiveReleaseExists(ctx context.Context, developmentID string) (bool, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT status FROM releases WHERE development_id = $1
	`, developmentID)
	if err != nil {
		return false, fmt.Errorf("checking active release for development %s: %w", developmentID, err)
	}
	defer rows.Close()

	for rows.Next() {
		var status string
		if err := rows.Scan(&status); err != nil {
			return false, fmt.Errorf("scanning release status: %w", err)
		}
		if !terminalReleaseStatuses[status] {
			return true, nil
		}
	}
	if err := rows.Err(); err != nil {
		return false, fmt.Errorf("iterating releases of development %s: %w", developmentID, err)
	}
	return false, nil
}

// InsertReleaseSources batch-inserts the checked, archived sources of a
// release.
func (s *Store) InsertReleaseSources(ctx context.Context, sources []ReleaseSource) error {
	for _, src := range sources {
		factIDs := src.FactIDs
		if factIDs == nil {
			factIDs = []string{}
		}
		_, err := s.pool.Exec(ctx, `
			INSERT INTO release_sources (
				release_id, url, final_url, outlet, title, published_at, fact_ids,
				is_primary, paywall, state_media, check_status, http_status, archive_url,
				text_fingerprint, checked_at
			)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
		`,
			src.ReleaseID, src.URL, src.FinalURL, src.Outlet, src.Title, src.PublishedAt, factIDs,
			src.IsPrimary, src.Paywall, src.StateMedia, src.CheckStatus, src.HTTPStatus, src.ArchiveURL,
			src.TextFingerprint, src.CheckedAt,
		)
		if err != nil {
			return fmt.Errorf("inserting release source %s: %w", src.URL, err)
		}
	}
	return nil
}

// ListReleaseSources returns the sources recorded for a release.
func (s *Store) ListReleaseSources(ctx context.Context, releaseID string) ([]ReleaseSource, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, release_id, url, final_url, outlet, title, published_at, fact_ids,
			is_primary, paywall, state_media, check_status, http_status, archive_url,
			text_fingerprint, checked_at
		FROM release_sources WHERE release_id = $1
		ORDER BY checked_at ASC
	`, releaseID)
	if err != nil {
		return nil, fmt.Errorf("listing sources of release %s: %w", releaseID, err)
	}
	defer rows.Close()

	var sources []ReleaseSource
	for rows.Next() {
		var src ReleaseSource
		if err := rows.Scan(
			&src.ID, &src.ReleaseID, &src.URL, &src.FinalURL, &src.Outlet, &src.Title, &src.PublishedAt, &src.FactIDs,
			&src.IsPrimary, &src.Paywall, &src.StateMedia, &src.CheckStatus, &src.HTTPStatus, &src.ArchiveURL,
			&src.TextFingerprint, &src.CheckedAt,
		); err != nil {
			return nil, fmt.Errorf("scanning release source: %w", err)
		}
		sources = append(sources, src)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating sources of release %s: %w", releaseID, err)
	}
	return sources, nil
}

// ListRecentReleases returns the n most recent releases of a channel, newest
// first, with enough context (story title, development summary, script,
// video package) for Legal to judge whether a new release is too similar
// (docs/architecture.md §6, releases_recent).
func (s *Store) ListRecentReleases(ctx context.Context, channelID string, n int) ([]RecentRelease, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT r.id, r.status, r.created_at, s.title, d.summary, r.script, r.video_package
		FROM releases r
		JOIN story_developments d ON d.id = r.development_id
		JOIN stories s ON s.id = d.story_id
		WHERE r.channel_id = $1
		ORDER BY r.created_at DESC
		LIMIT $2
	`, channelID, n)
	if err != nil {
		return nil, fmt.Errorf("listing recent releases of channel %s: %w", channelID, err)
	}
	defer rows.Close()

	var releases []RecentRelease
	for rows.Next() {
		var r RecentRelease
		if err := rows.Scan(&r.ID, &r.Status, &r.CreatedAt, &r.StoryTitle, &r.DevelopmentSummary, &r.Script, &r.VideoPackage); err != nil {
			return nil, fmt.Errorf("scanning recent release: %w", err)
		}
		releases = append(releases, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating recent releases of channel %s: %w", channelID, err)
	}
	return releases, nil
}
