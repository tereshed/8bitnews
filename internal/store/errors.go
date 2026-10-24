package store

import (
	"errors"

	"github.com/jackc/pgx/v5"
)

// ErrNotFound is returned by Get-style methods when no row matches.
var ErrNotFound = errors.New("not found")

// mapNotFound turns pgx's "no rows" sentinel into the store-level
// ErrNotFound so callers never need to import pgx.
func mapNotFound(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	return err
}
