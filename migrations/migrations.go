// Package migrations embeds the goose SQL migrations that describe the hub
// Postgres schema (docs/architecture.md §7), so cmd/hub can run `migrate
// up|down` without shipping the .sql files separately from the binary.
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
