package store

import (
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// ErrNotFound is returned by Get-style methods when no row matches.
var ErrNotFound = errors.New("not found")

// ErrConflict is returned when an insert violates a uniqueness constraint
// the caller should treat as a business-rule conflict rather than an
// unexpected database error (e.g. two concurrent release_submit calls for
// the same development racing past the application-level check).
var ErrConflict = errors.New("conflict")

// uniqueViolationCode is Postgres's SQLSTATE for unique_violation.
const uniqueViolationCode = "23505"

// mapNotFound turns pgx's "no rows" sentinel into the store-level
// ErrNotFound so callers never need to import pgx.
func mapNotFound(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

// mapConflict turns a unique-constraint-violation Postgres error into the
// store-level ErrConflict so callers never need to import pgconn.
func mapConflict(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == uniqueViolationCode {
		return ErrConflict
	}
	return err
}
