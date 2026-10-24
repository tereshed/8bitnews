package service_test

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/tereshed/8bitnews/internal/config"
	"github.com/tereshed/8bitnews/internal/storage"
	"github.com/tereshed/8bitnews/internal/store"
)

// These integration tests need a real Postgres and a real S3-compatible
// object store (task E1: "SQL — only against a live database"). They
// default to the addresses deploy/docker-compose.yml publishes, and can be
// pointed elsewhere (e.g. this sandbox's dind host) via env vars. If
// neither is reachable, every test using newTestStore/newTestMedia skips
// with an explanatory message instead of failing the build.
func testDSN() string {
	return envOr("HUB_TEST_DATABASE_URL", "postgres://8bitnews:8bitnews@localhost:5432/8bitnews?sslmode=disable")
}

func testS3Config() config.S3Config {
	return config.S3Config{
		Endpoint:        envOr("HUB_TEST_S3_ENDPOINT", "http://localhost:9000"),
		Region:          envOr("HUB_TEST_S3_REGION", "us-east-1"),
		Bucket:          envOr("HUB_TEST_S3_BUCKET", "8bitnews-media"),
		AccessKeyID:     envOr("HUB_TEST_S3_ACCESS_KEY", "8bitnews"),
		SecretAccessKey: envOr("HUB_TEST_S3_SECRET_KEY", "8bitnews-secret"),
		UsePathStyle:    true,
		PresignExpiry:   15 * time.Minute,
	}
}

func envOr(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}

// newTestStore runs the embedded migrations against the test database,
// truncates every table, and returns a connected Store. It skips the test
// if the database is not reachable.
func newTestStore(t *testing.T) *store.Store {
	t.Helper()
	dsn := testDSN()

	if err := store.MigrateUp(dsn); err != nil {
		t.Skipf("skipping integration test: migrating test database %s: %v", dsn, err)
	}

	st, err := store.Open(context.Background(), dsn)
	if err != nil {
		t.Skipf("skipping integration test: connecting to test database: %v", err)
	}
	t.Cleanup(st.Close)

	truncateAll(t, dsn)
	return st
}

func truncateAll(t *testing.T, dsn string) {
	t.Helper()
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("opening raw connection for truncate: %v", err)
	}
	defer func() { _ = db.Close() }()

	_, err = db.Exec(`TRUNCATE TABLE
		events, platform_incidents, music_tracks, metric_snapshots, publications,
		release_sources, releases, assignments, experiment_arms, experiments,
		story_developments, stories, platform_accounts, channels, countries
		CASCADE`)
	if err != nil {
		t.Fatalf("truncating test database: %v", err)
	}
}

// newTestMedia builds an S3 client against the test object store and skips
// the test if it is not reachable or the bucket does not exist.
func newTestMedia(t *testing.T) *storage.Client {
	t.Helper()
	media, err := storage.New(context.Background(), testS3Config())
	if err != nil {
		t.Fatalf("building S3 client: %v", err)
	}
	if _, err := media.Exists(context.Background(), "healthcheck-probe-object"); err != nil {
		t.Skipf("skipping integration test: S3 not reachable: %v", err)
	}
	return media
}

// seedChannel inserts a minimal country+channel pair so FK-constrained
// store calls (stories, releases, ...) have something to point at.
func seedChannel(t *testing.T, st *store.Store, channelID string) {
	t.Helper()
	ctx := context.Background()
	if err := st.UpsertCountry(ctx, store.Country{CC: "zz", Name: "Zzedland", Language: "en-US", Data: []byte(`{}`)}); err != nil {
		t.Fatalf("seeding country: %v", err)
	}
	if err := st.UpsertChannel(ctx, store.Channel{
		ID: channelID, CountryCC: "zz", Name: "Test Channel", Language: "en-US", Timezone: "UTC",
		Platforms: []byte(`{"youtube":{"enabled":true},"tiktok":{"enabled":true}}`),
		Format:    []byte(`{}`),
		FormatMD:  "# Test format",
		Data:      []byte(`{}`),
	}); err != nil {
		t.Fatalf("seeding channel: %v", err)
	}
}
