package store

import (
	"database/sql"
	"fmt"

	_ "github.com/jackc/pgx/v5/stdlib" // registers the "pgx" database/sql driver
	"github.com/pressly/goose/v3"

	"github.com/tereshed/8bitnews/migrations"
)

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
	if err := run(db, "."); err != nil {
		return fmt.Errorf("running migrations: %w", err)
	}
	return nil
}
