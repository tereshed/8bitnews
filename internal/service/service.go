package service

import (
	"log/slog"

	"github.com/tereshed/8bitnews/internal/sources"
	"github.com/tereshed/8bitnews/internal/storage"
	"github.com/tereshed/8bitnews/internal/store"
)

// defaultSimilarDays and defaultRecentN are the fallbacks for
// story_find_similar's days and releases_recent's n when the caller omits
// them, matching the defaults in task E1 §3.
const (
	defaultSimilarDays = 30
	defaultRecentN     = 10
)

// Service implements the 8 hub operations on top of Postgres, an
// S3-compatible object store, and the source-checking/archiving pipeline.
// It is the single place REST and MCP both call into, so their contracts
// cannot drift apart.
type Service struct {
	store   *store.Store
	media   *storage.Client
	checker *sources.Checker
	wayback *sources.WaybackClient
	log     *slog.Logger
}

// New builds a Service from its dependencies.
func New(st *store.Store, media *storage.Client, checker *sources.Checker, wayback *sources.WaybackClient, log *slog.Logger) *Service {
	if log == nil {
		log = slog.Default()
	}
	return &Service{store: st, media: media, checker: checker, wayback: wayback, log: log}
}
