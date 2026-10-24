// Package store is the hub's Postgres repository layer. It owns every SQL
// statement; internal/service calls it through plain Go methods and never
// sees a *pgxpool.Pool or raw SQL.
package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Store wraps a Postgres connection pool with the queries the hub needs.
type Store struct {
	pool *pgxpool.Pool
}

// Open creates a connection pool for dsn and verifies it with a ping.
func Open(ctx context.Context, dsn string) (*Store, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("creating pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("pinging database: %w", err)
	}
	return &Store{pool: pool}, nil
}

// Close releases the underlying connection pool.
func (s *Store) Close() {
	s.pool.Close()
}
