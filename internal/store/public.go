package store

import (
	"context"
	"errors"
	"fmt"
	"regexp"
)

// publicStatuses are the releases whose sources page is public: published,
// on the moderator's word that the video is up. An approved release isn't
// public yet: the moderator may still hold it back.
const publicStatuses = `('published')`

var releaseCode = regexp.MustCompile(`^[0-9a-f]{10}$`)

// PublicRelease returns the public release whose id starts with code, the
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
		WHERE id BETWEEN $1::uuid AND $2::uuid AND status IN `+publicStatuses+`
		ORDER BY created_at ASC
		LIMIT 1
	`, prefix+"00-0000-0000-000000000000", prefix+"ff-ffff-ffff-ffffffffffff"), &r)
	if err != nil {
		return Release{}, fmt.Errorf("getting public release %s: %w", code, mapNotFound(err))
	}
	return r, nil
}

// PublicReleases returns a channel's n most recent public releases, newest
// first, for the channel's list of videos: only their id, the script's
// metadata (the title) and the dates, not the whole release.
func (s *Store) PublicReleases(ctx context.Context, channelID string, n int) ([]Release, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, channel_id, status, jsonb_build_object('metadata', script->'metadata'), created_at, moderated_at
		FROM releases
		WHERE channel_id = $1 AND status IN `+publicStatuses+`
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
// "approved" (a mistaken press; its sources page is hidden again). It
// reports false, with the release as it is, if the release wasn't in the
// status to move from.
func (s *Store) SetPublished(ctx context.Context, releaseID string, published bool) (Release, bool, error) {
	from, to := "approved", "published"
	if !published {
		from, to = to, from
	}
	var r Release
	err := scanRelease(s.pool.QueryRow(ctx, `
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
