package api_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/tereshed/8bitnews/internal/api"
	"github.com/tereshed/8bitnews/internal/config"
	"github.com/tereshed/8bitnews/internal/mcpapi"
	"github.com/tereshed/8bitnews/internal/observability"
	"github.com/tereshed/8bitnews/internal/service"
	"github.com/tereshed/8bitnews/internal/sources"
	"github.com/tereshed/8bitnews/internal/storage"
	"github.com/tereshed/8bitnews/internal/store"
)

// These integration tests drive the hub's actual MCP transport end to end
// (real HTTP, real Bearer auth, the official go-sdk client) rather than
// calling internal/service directly. They need a real Postgres and a real
// S3-compatible object store; see internal/service's helpers_test.go for
// the rationale. They default to the addresses deploy/docker-compose.yml
// publishes and skip with an explanatory message if nothing is reachable.

func mcpTestDSN() string {
	return envOrDefault("HUB_TEST_DATABASE_URL", "postgres://8bitnews:8bitnews@localhost:5432/8bitnews?sslmode=disable")
}

func mcpTestS3Config() config.S3Config {
	return config.S3Config{
		Endpoint:        envOrDefault("HUB_TEST_S3_ENDPOINT", "http://localhost:9000"),
		Region:          envOrDefault("HUB_TEST_S3_REGION", "us-east-1"),
		Bucket:          envOrDefault("HUB_TEST_S3_BUCKET", "8bitnews-media"),
		AccessKeyID:     envOrDefault("HUB_TEST_S3_ACCESS_KEY", "8bitnews"),
		SecretAccessKey: envOrDefault("HUB_TEST_S3_SECRET_KEY", "8bitnews-secret"),
		UsePathStyle:    true,
		PresignExpiry:   15 * time.Minute,
	}
}

func envOrDefault(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}

func newMCPTestStore(t *testing.T) *store.Store {
	t.Helper()
	dsn := mcpTestDSN()

	if err := store.MigrateUp(dsn); err != nil {
		t.Skipf("skipping integration test: migrating test database %s: %v", dsn, err)
	}

	st, err := store.Open(context.Background(), dsn)
	if err != nil {
		t.Skipf("skipping integration test: connecting to test database: %v", err)
	}
	t.Cleanup(st.Close)

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
	return st
}

func newMCPTestMedia(t *testing.T) *storage.Client {
	t.Helper()
	media, err := storage.New(context.Background(), mcpTestS3Config())
	if err != nil {
		t.Fatalf("building S3 client: %v", err)
	}
	if _, err := media.Exists(context.Background(), "healthcheck-probe-object"); err != nil {
		t.Skipf("skipping integration test: S3 not reachable: %v", err)
	}
	return media
}

func seedMCPTestChannel(t *testing.T, st *store.Store, channelID string) {
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

// newMCPFakeSourceServer stands in for the external press site a release's
// sources point to.
func newMCPFakeSourceServer(t *testing.T) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/ok" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("<html><body>A real article about the story.</body></html>"))
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// newMCPFakeWaybackServer stands in for the Wayback Machine's Save Page Now
// endpoint.
func newMCPFakeWaybackServer(t *testing.T) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/save/"):
			target := strings.TrimPrefix(r.URL.Path, "/save/")
			w.Header().Set("Location", "/web/20261024000000/"+target)
			w.WriteHeader(http.StatusFound)
		case strings.HasPrefix(r.URL.Path, "/web/"):
			w.WriteHeader(http.StatusOK)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// bearerTransport injects the hub's API key on every request, the way a
// real MCP client authenticates against internal/api's router.
type bearerTransport struct {
	key string
}

func (t bearerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.Header.Set("Authorization", "Bearer "+t.key)
	return http.DefaultTransport.RoundTrip(req)
}

// newMCPTestClientSession connects a real MCP client, through the hub's
// full router (auth included), to a freshly started httptest server.
func newMCPTestClientSession(t *testing.T, svc *service.Service, apiKey string) *mcp.ClientSession {
	t.Helper()

	mcpHandler := mcpapi.NewHTTPHandler(svc)
	metrics := observability.NewMetrics()
	router := api.NewRouter(svc, mcpHandler, metrics, slog.Default(), map[string]bool{apiKey: true})
	srv := httptest.NewServer(router)
	t.Cleanup(srv.Close)

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0.0.1"}, nil)
	transport := &mcp.StreamableClientTransport{
		Endpoint:   srv.URL + "/mcp",
		HTTPClient: &http.Client{Transport: bearerTransport{key: apiKey}},
	}
	session, err := client.Connect(context.Background(), transport, nil)
	if err != nil {
		t.Fatalf("connecting MCP client: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session
}

func mcpCallTool(t *testing.T, session *mcp.ClientSession, name string, args map[string]any, out any) *mcp.CallToolResult {
	t.Helper()
	res, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("calling tool %q: %v", name, err)
	}
	if res.IsError {
		b, _ := json.Marshal(res.Content)
		t.Fatalf("tool %q returned an error result: %s", name, b)
	}
	if out != nil {
		b, err := json.Marshal(res.StructuredContent)
		if err != nil {
			t.Fatalf("marshaling tool %q structured content: %v", name, err)
		}
		if err := json.Unmarshal(b, out); err != nil {
			t.Fatalf("unmarshaling tool %q structured content into %T: %v", name, out, err)
		}
	}
	return res
}

// mcpUploadFixture gets a presigned URL via the media_upload_url MCP tool
// and PUTs a small fixed body to it, returning the resulting object key.
func mcpUploadFixture(t *testing.T, session *mcp.ClientSession, developmentID, filename string) string {
	t.Helper()
	var out struct {
		UploadURL string `json:"upload_url"`
		ObjectKey string `json:"object_key"`
	}
	mcpCallTool(t, session, "media_upload_url", map[string]any{
		"development_id": developmentID,
		"filename":       filename,
		"content_type":   "application/octet-stream",
	}, &out)

	req, err := http.NewRequest(http.MethodPut, out.UploadURL, bytes.NewReader([]byte("fixture content")))
	if err != nil {
		t.Fatalf("building PUT request: %v", err)
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PUT upload: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode/100 != 2 {
		t.Fatalf("PUT upload status %d: %s", resp.StatusCode, body)
	}
	return out.ObjectKey
}

// TestMCP_ReleaseSubmitAndGet_HappyPath reproduces the exact scenario a
// previous review found broken: driving media_upload_url, release_submit
// and release_get over a real MCP session with a realistic manifest whose
// research_dossier/script/validation_report/legal_review fields are JSON
// objects, not byte arrays. Before the fix, the go-sdk's auto-inferred
// input/output schema for these json.RawMessage fields rejected any
// object-shaped payload for them (jsonschema-go has no special case for
// json.RawMessage and falls back to its generic []byte -> array rule).
func TestMCP_ReleaseSubmitAndGet_HappyPath(t *testing.T) {
	st := newMCPTestStore(t)
	media := newMCPTestMedia(t)
	seedMCPTestChannel(t, st, "test-channel")

	sourceSrv := newMCPFakeSourceServer(t)
	waybackSrv := newMCPFakeWaybackServer(t)
	checker := sources.NewChecker(5 * time.Second)
	wayback := sources.NewWaybackClient(waybackSrv.URL, 5*time.Second)

	svc := service.New(st, media, checker, wayback, slog.Default())
	const apiKey = "test-key"
	session := newMCPTestClientSession(t, svc, apiKey)

	tools, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	wantTools := map[string]bool{
		"channel_get": true, "story_find_similar": true, "story_register": true,
		"experiment_assign": true, "media_upload_url": true, "release_submit": true,
		"release_get": true, "releases_recent": true,
	}
	if len(tools.Tools) != len(wantTools) {
		t.Fatalf("ListTools returned %d tools, want %d", len(tools.Tools), len(wantTools))
	}
	for _, tool := range tools.Tools {
		if !wantTools[tool.Name] {
			t.Errorf("unexpected tool %q", tool.Name)
		}
	}

	var registerOut struct {
		StoryID       string `json:"story_id"`
		DevelopmentID string `json:"development_id"`
	}
	mcpCallTool(t, session, "story_register", map[string]any{
		"channel_id":          "test-channel",
		"story_id":            nil,
		"title":               "MCP release submit happy path",
		"summary":             "A summary that is long enough to pass the story_register validation.",
		"development_summary": "A development summary that is long enough to pass validation checks.",
		"reported_at":         time.Now().UTC().Format(time.RFC3339),
		"seed_urls":           []string{"https://example.com/seed"},
	}, &registerOut)
	devID := registerOut.DevelopmentID

	video := mcpUploadFixture(t, session, devID, "video.mp4")
	subtitles := mcpUploadFixture(t, session, devID, "subtitles.srt")
	cover := mcpUploadFixture(t, session, devID, "cover.png")

	sourceURL := sourceSrv.URL + "/ok"
	researchDossier := map[string]any{
		"facts": []any{
			map[string]any{
				"id":             "F1",
				"primary_source": map[string]any{"url": sourceURL, "outlet": "Example Wire", "published_at": "2026-10-24T00:00:00Z"},
				"sources":        []any{},
			},
		},
	}
	script := map[string]any{"scenes": []any{map[string]any{"text": "hook", "fact_ids": []any{"F1"}}}}
	legalReview := map[string]any{
		"mode": "final", "verdict": "pass",
		"platforms": map[string]any{
			"youtube": map[string]any{"verdict": "pass"},
			"tiktok":  map[string]any{"verdict": "exclude"},
		},
	}

	var submitOut struct {
		ReleaseID        string   `json:"release_id"`
		Status           string   `json:"status"`
		AllowedPlatforms []string `json:"allowed_platforms"`
		Sources          []struct {
			URL         string `json:"url"`
			CheckStatus string `json:"check_status"`
			ArchiveURL  string `json:"archive_url"`
		} `json:"sources"`
	}
	mcpCallTool(t, session, "release_submit", map[string]any{
		"development_id":    devID,
		"channel_id":        "test-channel",
		"media":             map[string]any{"video": video, "subtitles": subtitles, "cover": cover},
		"research_dossier":  researchDossier,
		"script":            script,
		"video_package":     map[string]any{},
		"validation_report": map[string]any{"verdict": "pass"},
		"legal_review":      legalReview,
	}, &submitOut)

	if submitOut.Status != "submitted" {
		t.Errorf("Status = %q, want submitted", submitOut.Status)
	}
	if len(submitOut.AllowedPlatforms) != 1 || submitOut.AllowedPlatforms[0] != "youtube" {
		t.Errorf("AllowedPlatforms = %v, want [youtube]", submitOut.AllowedPlatforms)
	}
	if len(submitOut.Sources) != 1 || submitOut.Sources[0].CheckStatus != "ok" || submitOut.Sources[0].ArchiveURL == "" {
		t.Fatalf("Sources = %+v", submitOut.Sources)
	}

	var getOut struct {
		Status      string          `json:"status"`
		LegalReview json.RawMessage `json:"legal_review"`
		Script      json.RawMessage `json:"script"`
		Sources     []struct {
			URL string `json:"url"`
		} `json:"sources"`
	}
	mcpCallTool(t, session, "release_get", map[string]any{"release_id": submitOut.ReleaseID}, &getOut)
	if getOut.Status != "submitted" {
		t.Errorf("release_get Status = %q, want submitted", getOut.Status)
	}
	if len(getOut.Sources) != 1 {
		t.Fatalf("release_get Sources = %+v", getOut.Sources)
	}
	var gotLegal map[string]any
	if err := json.Unmarshal(getOut.LegalReview, &gotLegal); err != nil {
		t.Fatalf("release_get legal_review is not valid JSON: %v (%s)", err, getOut.LegalReview)
	}
	if gotLegal["verdict"] != "pass" {
		t.Errorf("release_get legal_review.verdict = %v, want pass", gotLegal["verdict"])
	}
}
