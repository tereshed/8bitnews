package service

import (
	"log/slog"
	"time"

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

// defaultSourcesConcurrency, defaultSourcesCheckDeadline and
// defaultSourcesBackgroundArchiveTimeout are the fallbacks Deps uses when a
// caller leaves the corresponding tunable at its zero value (e.g. a test
// that never calls ReleaseSubmit's source-checking path).
const (
	defaultSourcesConcurrency              = 8
	defaultSourcesCheckDeadline            = 25 * time.Second
	defaultSourcesBackgroundArchiveTimeout = 5 * time.Minute
)

// Service implements the 8 hub operations on top of Postgres, an
// S3-compatible object store, and the source-checking/archiving pipeline.
// It is the single place REST and MCP both call into, so their contracts
// cannot drift apart.
type Service struct {
	store        *store.Store
	media        *storage.Client
	checker      *sources.Checker
	wayback      *sources.WaybackClient
	availability *sources.AvailabilityClient
	log          *slog.Logger

	sourcesConcurrency              int
	sourcesCheckDeadline            time.Duration
	sourcesBackgroundArchiveTimeout time.Duration
}

// Deps are Service's dependencies and tunables. Checker, Wayback and
// Availability may be nil for callers (tests) that never exercise
// ReleaseSubmit's source-checking/archiving path.
type Deps struct {
	Store        *store.Store
	Media        *storage.Client
	Checker      *sources.Checker
	Wayback      *sources.WaybackClient
	Availability *sources.AvailabilityClient
	Log          *slog.Logger

	// SourcesConcurrency bounds how many source links ReleaseSubmit
	// checks/archives at once. Zero uses defaultSourcesConcurrency.
	SourcesConcurrency int
	// SourcesCheckDeadline bounds ReleaseSubmit's whole synchronous
	// source-checking phase. Zero uses defaultSourcesCheckDeadline.
	SourcesCheckDeadline time.Duration
	// SourcesBackgroundArchiveTimeout bounds the background job that
	// archives "ok" sources after a release has been created. Zero uses
	// defaultSourcesBackgroundArchiveTimeout.
	SourcesBackgroundArchiveTimeout time.Duration
}

// New builds a Service from deps, filling in defaults for any zero-valued
// tunable.
func New(deps Deps) *Service {
	log := deps.Log
	if log == nil {
		log = slog.Default()
	}
	concurrency := deps.SourcesConcurrency
	if concurrency <= 0 {
		concurrency = defaultSourcesConcurrency
	}
	deadline := deps.SourcesCheckDeadline
	if deadline <= 0 {
		deadline = defaultSourcesCheckDeadline
	}
	archiveTimeout := deps.SourcesBackgroundArchiveTimeout
	if archiveTimeout <= 0 {
		archiveTimeout = defaultSourcesBackgroundArchiveTimeout
	}
	return &Service{
		store:        deps.Store,
		media:        deps.Media,
		checker:      deps.Checker,
		wayback:      deps.Wayback,
		availability: deps.Availability,
		log:          log,

		sourcesConcurrency:              concurrency,
		sourcesCheckDeadline:            deadline,
		sourcesBackgroundArchiveTimeout: archiveTimeout,
	}
}
