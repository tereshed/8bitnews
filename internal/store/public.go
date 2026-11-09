package store

import (
	"context"
	"errors"
	"fmt"
	"regexp"
)

// sourcesPageStatuses are the releases whose sources page is public: from the
// moderator's approval on. The video's description links to the page, so it
// has to open as soon as the video can go up, not when someone records that it did.
const sourcesPageStatuses = `('approved', 'published')`

// listedStatuses are the releases on the channel's list of videos: published,
// on the moderator's word that the video is up. An approved one may not be up yet.
const listedStatuses = `('published')`

var releaseCode = regexp.MustCompile(`^[0-9a-f]{10}$`)

// PublicRelease returns the release with a public sources page whose id starts with code, the
// first ten hex digits of the id (publish.Code). Should two ever share
// them, the older one keeps the address.
func (s *Store) PublicRelease(ctx context.Context, code string) (Release, error) {
	if !releaseCode.MatchString(code) {
		return Release{}, fmt.Errorf("release code %q: %w", code, ErrNotFound)
	}
	// The ids from code-0000… to code-ffff…: a range the primary key index answers.
	prefix := code[:8] + "-" + code[8:]
	var r Release
	err := scanRelease(s.pool.QueryRow(ctx, `
		SELECT `+releaseColumns+` FROM releases
		WHERE id BETWEEN $1::uuid AND $2::uuid AND status IN `+sourcesPageStatuses+`
		ORDER BY created_at ASC
		LIMIT 1
	`, prefix+"00-0000-0000-000000000000", prefix+"ff-ffff-ffff-ffffffffffff"), &r)
	if err != nil {
		return Release{}, fmt.Errorf("getting public release %s: %w", code, mapNotFound(err))
	}
	return r, nil
}

// PublicReleases returns a channel's n most recent listed releases, newest
// first, for the channel's list of videos: only their id, the script's
// metadata (the title) and the dates, not the whole release.
func (s *Store) PublicReleases(ctx context.Context, channelID string, n int) ([]Release, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, channel_id, status, jsonb_build_object('metadata', script->'metadata'), created_at, moderated_at
		FROM releases
		WHERE channel_id = $1 AND status IN `+listedStatuses+`
		ORDER BY COALESCE(moderated_at, created_at) DESC
		LIMIT $2
	`, channelID, n)
	if err != nil {
		return nil, fmt.Errorf("listing public releases of channel %s: %w", channelID, err)
	}
	defer rows.Close()
	var out []Release
	for rows.Next() {
		var r Release
		if err := rows.Scan(&r.ID, &r.ChannelID, &r.Status, &r.Script, &r.CreatedAt, &r.Moderation.At); err != nil {
			return nil, fmt.Errorf("scanning public release: %w", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating public releases of channel %s: %w", channelID, err)
	}
	return out, nil
}

// SetPublished moves an approved release to "published" (the moderator
// says the video is up), or, with published false, a published one back to
// "approved" (a mistaken press; it leaves the channel's list, its sources page stays). It
// reports false, with the release as it is, if the release wasn't in the
// status to move from. Either way the question of whether the video is up is
// settled: hub no longer changes it on what it sees on the platform.
func (s *Store) SetPublished(ctx context.Context, releaseID string, published bool) (Release, bool, error) {
	from, to := "approved", "published"
	if !published {
		from, to = to, from
	}
	var r Release
	err := scanRelease(s.pool.QueryRow(ctx, `
		WITH settled AS (
			UPDATE release_publications SET live_at = COALESCE(live_at, now())
			WHERE release_id = $1 AND EXISTS (SELECT 1 FROM releases WHERE id = $1 AND status = $2)
		)
		UPDATE releases SET status = $3, updated_at = now()
		WHERE id = $1 AND status = $2
		RETURNING `+releaseColumns, releaseID, from, to), &r)
	if err == nil {
		return r, true, nil
	}
	if !errors.Is(mapNotFound(err), ErrNotFound) {
		return Release{}, false, fmt.Errorf("moving release %s from %s to %s: %w", releaseID, from, to, err)
	}
	r, err = s.GetRelease(ctx, releaseID)
	return r, false, err
}

// WithdrawRelease closes the sources page of an approved or published release
// (a mistaken approval, a claim) by moving it to "rejected", recording who did.
// It reports false, with the release as it is, if the release was in neither status.
func (s *Store) WithdrawRelease(ctx context.Context, releaseID, by string) (Release, bool, error) {
	var r Release
	err := scanRelease(s.pool.QueryRow(ctx, `
		UPDATE releases SET status = 'rejected', moderated_by = $2, moderated_at = now(), updated_at = now()
		WHERE id = $1 AND status IN ('approved', 'published')
		RETURNING `+releaseColumns, releaseID, by), &r)
	if err == nil {
		return r, true, nil
	}
	if !errors.Is(mapNotFound(err), ErrNotFound) {
		return Release{}, false, fmt.Errorf("withdrawing release %s: %w", releaseID, err)
	}
	r, err = s.GetRelease(ctx, releaseID)
	return r, false, err
}
