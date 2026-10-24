// Package config loads hub settings from the process environment. It is the
// single place that knows the env var names, so cmd/hub and tests never read
// os.Getenv directly.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config holds every setting the hub binary needs, regardless of the
// subcommand (migrate, sync-profiles, serve).
type Config struct {
	// DatabaseURL is a Postgres connection string understood by pgx.
	DatabaseURL string

	// RepoRoot is the repository checkout that contains channels/ and
	// countries/, read by `sync-profiles`.
	RepoRoot string

	// HTTPAddr is the address `serve` listens on.
	HTTPAddr string

	// APIKeys is the set of bearer tokens accepted by REST and MCP, read
	// from the comma-separated HUB_API_KEYS env var.
	APIKeys map[string]bool

	S3 S3Config

	// WaybackBaseURL is the Save Page Now endpoint; overridden in tests to
	// point at an httptest server instead of the real Wayback Machine.
	WaybackBaseURL string

	// WaybackAvailabilityBaseURL is the Wayback Availability JSON API
	// endpoint (https://archive.org/wayback/available); overridden in
	// tests to point at an httptest server.
	WaybackAvailabilityBaseURL string

	// WaybackAccessKey and WaybackSecretKey are the optional Save Page Now
	// API credentials (sent as "Authorization: LOW <key>:<secret>").
	// Anonymous Save Page Now calls are subject to tight rate limits; the
	// operator's own archive.org account should supply these in
	// production.
	WaybackAccessKey string
	WaybackSecretKey string

	// SourceCheckTimeout bounds each individual outbound HTTP call
	// release_submit's source checking/archiving makes (one link check,
	// one Save Page Now call, one availability lookup).
	SourceCheckTimeout time.Duration

	// SourceAllowPrivate lifts the SSRF protection in internal/sources,
	// letting it dial loopback/private/link-local addresses. It exists
	// only so tests can point the checker/wayback clients at httptest
	// servers; production must always leave this false.
	SourceAllowPrivate bool

	// SourcesConcurrency bounds how many source links release_submit
	// checks/archives at once.
	SourcesConcurrency int

	// SourcesCheckDeadline bounds the whole synchronous source-checking
	// phase of release_submit (parallel Checker.Check calls, plus the
	// synchronous Save-Page-Now/Availability-API calls needed to decide
	// pass/fail for a non-ok source), regardless of how many sources there
	// are.
	SourcesCheckDeadline time.Duration

	// SourcesBackgroundArchiveTimeout bounds the background job that
	// archives "ok" sources via Save Page Now after a release has already
	// been created.
	SourcesBackgroundArchiveTimeout time.Duration

	// MetricsAddr, if set, serves /metrics unauthenticated on this
	// separate address instead of on the main API's authenticated
	// /metrics route — for deployments that scrape Prometheus metrics from
	// a private network interface.
	MetricsAddr string
}

// S3Config holds the settings for the S3-compatible object store used for
// media uploads (presigned PUT) and release_submit's object-key check.
type S3Config struct {
	Endpoint        string
	Region          string
	Bucket          string
	AccessKeyID     string
	SecretAccessKey string
	UsePathStyle    bool
	PresignExpiry   time.Duration
}

// Load reads the configuration from the environment, filling in the same
// defaults as deploy/docker-compose.yml and .env.example so `make up` followed
// by `go run ./cmd/hub ...` works without exporting anything.
func Load() (Config, error) {
	cfg := Config{
		DatabaseURL:                     getEnv("HUB_DATABASE_URL", "postgres://8bitnews:8bitnews@localhost:5432/8bitnews?sslmode=disable"),
		RepoRoot:                        getEnv("HUB_REPO_ROOT", "."),
		HTTPAddr:                        getEnv("HUB_HTTP_ADDR", ":8080"),
		WaybackBaseURL:                  getEnv("HUB_WAYBACK_BASE_URL", "https://web.archive.org"),
		WaybackAvailabilityBaseURL:      getEnv("HUB_WAYBACK_AVAILABILITY_BASE_URL", "https://archive.org"),
		WaybackAccessKey:                getEnv("HUB_WAYBACK_ACCESS_KEY", ""),
		WaybackSecretKey:                getEnv("HUB_WAYBACK_SECRET_KEY", ""),
		SourceCheckTimeout:              getDuration("HUB_SOURCE_CHECK_TIMEOUT", 15*time.Second),
		SourceAllowPrivate:              getBool("HUB_SOURCE_ALLOW_PRIVATE", false),
		SourcesConcurrency:              getInt("HUB_SOURCES_CONCURRENCY", 8),
		SourcesCheckDeadline:            getDuration("HUB_SOURCES_CHECK_DEADLINE", 25*time.Second),
		SourcesBackgroundArchiveTimeout: getDuration("HUB_SOURCES_BACKGROUND_ARCHIVE_TIMEOUT", 5*time.Minute),
		MetricsAddr:                     getEnv("HUB_METRICS_ADDR", ""),
		S3: S3Config{
			Endpoint:        getEnv("HUB_S3_ENDPOINT", "http://localhost:9000"),
			Region:          getEnv("HUB_S3_REGION", "us-east-1"),
			Bucket:          getEnv("HUB_S3_BUCKET", "8bitnews-media"),
			AccessKeyID:     getEnv("HUB_S3_ACCESS_KEY", "8bitnews"),
			SecretAccessKey: getEnv("HUB_S3_SECRET_KEY", "8bitnews-secret"),
			UsePathStyle:    getBool("HUB_S3_USE_PATH_STYLE", true),
			PresignExpiry:   getDuration("HUB_S3_PRESIGN_EXPIRY", 15*time.Minute),
		},
	}

	cfg.APIKeys = parseAPIKeys(os.Getenv("HUB_API_KEYS"))

	if cfg.S3.Bucket == "" {
		return Config{}, fmt.Errorf("HUB_S3_BUCKET must not be empty")
	}

	return cfg, nil
}

func parseAPIKeys(raw string) map[string]bool {
	keys := map[string]bool{}
	for _, k := range strings.Split(raw, ",") {
		k = strings.TrimSpace(k)
		if k != "" {
			keys[k] = true
		}
	}
	return keys
}

func getEnv(name, fallback string) string {
	if v, ok := os.LookupEnv(name); ok && v != "" {
		return v
	}
	return fallback
}

func getBool(name string, fallback bool) bool {
	v, ok := os.LookupEnv(name)
	if !ok || v == "" {
		return fallback
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return fallback
	}
	return b
}

func getDuration(name string, fallback time.Duration) time.Duration {
	v, ok := os.LookupEnv(name)
	if !ok || v == "" {
		return fallback
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return fallback
	}
	return d
}

func getInt(name string, fallback int) int {
	v, ok := os.LookupEnv(name)
	if !ok || v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
}
