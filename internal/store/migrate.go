package store

import (
	"context"
	"database/sql"
	"fmt"

	_ "github.com/jackc/pgx/v5/stdlib" // registers the "pgx" database/sql driver
	"github.com/pressly/goose/v3"

	"github.com/tereshed/8bitnews/migrations"
)

// migrationLockKey is an arbitrary, stable key for the Postgres advisory
// lock that serializes concurrent migrators: multiple hub replicas can call
// "migrate up" on startup at the same time, and so can concurrent
// integration-test packages pointed at the same test database. The legacy
// goose.Up/Down API this package uses has no locking of its own, so without
// this, concurrent migrators race on creating goose_db_version and the
// migration's own types/tables.
const migrationLockKey = 0x3862_6974 // "8bit" as hex bytes

func withAdvisoryLock(ctx context.Context, db *sql.DB, fn func() error) error {
	conn, err := db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("acquiring migration lock connection: %w", err)
	}
	defer func() { _ = conn.Close() }()

	if _, err := conn.ExecContext(ctx, "SELECT pg_advisory_lock($1)", migrationLockKey); err != nil {
		return fmt.Errorf("acquiring migration lock: %w", err)
	}
	defer func() { _, _ = conn.ExecContext(ctx, "SELECT pg_advisory_unlock($1)", migrationLockKey) }()

	return fn()
}

// MigrateUp applies every embedded migration that has not run yet.
func MigrateUp(dsn string) error {
	return withGoose(dsn, goose.Up)
}

// MigrateDown rolls back the most recently applied migration.
func MigrateDown(dsn string) error {
	return withGoose(dsn, goose.Down)
}

func withGoose(dsn string, run func(*sql.DB, string, ...goose.OptionsFunc) error) error {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer func() { _ = db.Close() }()

	goose.SetBaseFS(migrations.FS)
	if err := goose.SetDialect("postgres"); err != nil {
		return fmt.Errorf("setting goose dialect: %w", err)
	}
	return withAdvisoryLock(context.Background(), db, func() error {
		if err := run(db, "."); err != nil {
			return fmt.Errorf("running migrations: %w", err)
		}
		return nil
	})
}
