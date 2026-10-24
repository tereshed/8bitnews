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

	// SourceCheckTimeout bounds each outbound link check in release_submit.
	SourceCheckTimeout time.Duration
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
		DatabaseURL:        getEnv("HUB_DATABASE_URL", "postgres://8bitnews:8bitnews@localhost:5432/8bitnews?sslmode=disable"),
		RepoRoot:           getEnv("HUB_REPO_ROOT", "."),
		HTTPAddr:           getEnv("HUB_HTTP_ADDR", ":8080"),
		WaybackBaseURL:     getEnv("HUB_WAYBACK_BASE_URL", "https://web.archive.org"),
		SourceCheckTimeout: getDuration("HUB_SOURCE_CHECK_TIMEOUT", 15*time.Second),
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
