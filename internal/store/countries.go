package store

import (
	"context"
	"encoding/json"
	"fmt"
)

// UpsertCountry inserts or updates a country profile by cc, used by
// `hub sync-profiles` to mirror countries/<cc>/ into Postgres.
func (s *Store) UpsertCountry(ctx context.Context, c Country) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO countries (cc, name, language, policy_md, legal_md, data, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, now())
		ON CONFLICT (cc) DO UPDATE SET
			name = EXCLUDED.name,
			language = EXCLUDED.language,
			policy_md = EXCLUDED.policy_md,
			legal_md = EXCLUDED.legal_md,
			data = EXCLUDED.data,
			updated_at = now()
	`, c.CC, c.Name, c.Language, c.PolicyMD, c.LegalMD, nonNilJSON(c.Data))
	if err != nil {
		return fmt.Errorf("upserting country %s: %w", c.CC, err)
	}
	return nil
}

// GetCountry returns the country profile for cc.
func (s *Store) GetCountry(ctx context.Context, cc string) (Country, error) {
	var c Country
	err := s.pool.QueryRow(ctx, `
		SELECT cc, name, language, policy_md, legal_md, data, updated_at
		FROM countries WHERE cc = $1
	`, cc).Scan(&c.CC, &c.Name, &c.Language, &c.PolicyMD, &c.LegalMD, &c.Data, &c.UpdatedAt)
	if err != nil {
		return Country{}, fmt.Errorf("getting country %s: %w", cc, mapNotFound(err))
	}
	return c, nil
}

func nonNilJSON(raw json.RawMessage) json.RawMessage {
	if raw == nil {
		return json.RawMessage("{}")
	}
	return raw
}
