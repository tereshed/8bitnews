package store

import (
	"context"
	"fmt"
)

// UpsertChannel inserts or updates a channel profile by id, used by
// `hub sync-profiles` to mirror channels/<id>/ into Postgres.
func (s *Store) UpsertChannel(ctx context.Context, c Channel) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO channels (id, country_cc, name, language, timezone, platforms, format, format_md, metadata_md, data, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, now())
		ON CONFLICT (id) DO UPDATE SET
			country_cc = EXCLUDED.country_cc,
			name = EXCLUDED.name,
			language = EXCLUDED.language,
			timezone = EXCLUDED.timezone,
			platforms = EXCLUDED.platforms,
			format = EXCLUDED.format,
			format_md = EXCLUDED.format_md,
			metadata_md = EXCLUDED.metadata_md,
			data = EXCLUDED.data,
			updated_at = now()
	`, c.ID, c.CountryCC, c.Name, c.Language, c.Timezone, nonNilJSON(c.Platforms), nonNilJSON(c.Format), c.FormatMD, c.MetadataMD, nonNilJSON(c.Data))
	if err != nil {
		return fmt.Errorf("upserting channel %s: %w", c.ID, err)
	}
	return nil
}

// GetChannel returns the channel profile for id.
func (s *Store) GetChannel(ctx context.Context, id string) (Channel, error) {
	var c Channel
	err := s.pool.QueryRow(ctx, `
		SELECT id, country_cc, name, language, timezone, platforms, format, format_md, metadata_md, data, updated_at
		FROM channels WHERE id = $1
	`, id).Scan(&c.ID, &c.CountryCC, &c.Name, &c.Language, &c.Timezone, &c.Platforms, &c.Format, &c.FormatMD, &c.MetadataMD, &c.Data, &c.UpdatedAt)
	if err != nil {
		return Channel{}, fmt.Errorf("getting channel %s: %w", id, mapNotFound(err))
	}
	return c, nil
}
