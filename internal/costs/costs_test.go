package costs

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/tereshed/8bitnews/internal/store"
)

func discard() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

const devID = "06017b34-2976-4583-86a6-4d4b85e1d793"

func packageArchive(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, body := range files {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestImagesCost(t *testing.T) {
	for _, tc := range []struct {
		name  string
		files map[string]string
		want  float64
		bad   bool
	}{
		{"the figure", map[string]string{"video.mp4": "x", "out/package.json": `{"images":{"cost_usd":0.8148}}`}, 0.8148, false},
		{"no generated pictures", map[string]string{"package.json": `{"duration_s":80}`}, 0, false},
		{"no package.json", map[string]string{"video.mp4": "x"}, 0, true},
		{"not json", map[string]string{"package.json": `nope`}, 0, true},
		{"a negative figure", map[string]string{"package.json": `{"images":{"cost_usd":-1}}`}, 0, true},
		{"an implausible figure", map[string]string{"package.json": `{"images":{"cost_usd":5000}}`}, 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := imagesCost(bytes.NewReader(packageArchive(t, tc.files)))
			var bad *badPackageError
			if tc.bad != errors.As(err, &bad) {
				t.Fatalf("error %v, want bad=%v", err, tc.bad)
			}
			if !tc.bad && got != tc.want {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
	var bad *badPackageError
	if _, err := imagesCost(strings.NewReader("not a gzip")); !errors.As(err, &bad) {
		t.Fatalf("a non-gzip file: %v", err)
	}
}

func TestBuildDevelopment(t *testing.T) {
	for key, want := range map[string]bool{
		"releases/" + devID + "/728aaf1e-9e53-435b-a632-fa6abe41088b-package.tar.gz": true,
		"releases/" + devID + "/85d7e9be-571f-4e47-9d29-f7219760ca6f-video.mp4":      false,
		"releases/" + devID + "/sub/x-package.tar.gz":                                false,
		"releases/not-a-uuid/x-package.tar.gz":                                       false,
		"music/" + devID + "/x-package.tar.gz":                                       false,
	} {
		if dev, ok := buildDevelopment(key); ok != want || (ok && dev != devID) {
			t.Errorf("%s: %q, %v; want %v", key, dev, ok, want)
		}
	}
}

func TestDueForMeasuring(t *testing.T) {
	now := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	ago := func(d time.Duration) time.Time { return now.Add(-d) }
	state := store.CostRunState{SourceUpdatedAt: ago(2 * time.Hour), MeasuredAt: ago(time.Hour)}
	for _, tc := range []struct {
		name  string
		run   Run
		state store.CostRunState
		known bool
		want  bool
	}{
		{"never measured", Run{UpdatedAt: ago(40 * 24 * time.Hour), Status: "done"}, store.CostRunState{}, false, true},
		{"updated since", Run{UpdatedAt: ago(time.Minute), Status: "done"}, state, true, true},
		{"still running", Run{UpdatedAt: ago(2 * time.Hour), Status: "awaiting_delegation"}, state, true, true},
		{"finished, checked recently", Run{UpdatedAt: ago(2 * time.Hour), Status: "done"}, state, true, false},
		{"finished, checked a day ago", Run{UpdatedAt: ago(2 * time.Hour), Status: "done"},
			store.CostRunState{SourceUpdatedAt: ago(2 * time.Hour), MeasuredAt: ago(25 * time.Hour)}, true, true},
		{"too old: PolyMaths may have forgotten it", Run{UpdatedAt: ago(26 * 24 * time.Hour), Status: "awaiting_delegation"},
			store.CostRunState{SourceUpdatedAt: ago(27 * 24 * time.Hour), MeasuredAt: ago(26 * 24 * time.Hour)}, true, false},
	} {
		if got := dueForMeasuring(tc.run, tc.state, tc.known, now); got != tc.want {
			t.Errorf("%s: %v, want %v", tc.name, got, tc.want)
		}
	}
}

// fakePolyMaths serves PolyMaths' REST shapes, as read from the live API on 2026-11-05.
func fakePolyMaths(t *testing.T) (*httptest.Server, *[]string) {
	t.Helper()
	var keys []string
	usage := func(input int64, agents map[string]int64) map[string]any {
		byAgent := []map[string]any{}
		for k, n := range agents {
			byAgent = append(byAgent, map[string]any{"key": k, "runs": 1, "measured_runs": 1, "cache_read_tokens": n})
		}
		var cache int64
		for _, n := range agents {
			cache += n
		}
		return map[string]any{
			"totals":   map[string]any{"runs": len(agents), "measured_runs": len(agents), "input_tokens": input, "output_tokens": 10, "cache_creation_tokens": 20, "cache_read_tokens": cache},
			"by_agent": byAgent,
		}
	}
	mux := http.NewServeMux()
	send := func(w http.ResponseWriter, v any) { _ = json.NewEncoder(w).Encode(v) }
	mux.HandleFunc("GET /api/v1/projects/p/tasks", func(w http.ResponseWriter, r *http.Request) {
		keys = append(keys, r.Header.Get("X-API-Key"))
		send(w, map[string]any{"total": 2, "tasks": []map[string]any{
			{"id": "root", "team_id": "news", "status": "done", "created_at": "2026-11-05T06:42:44Z", "updated_at": "2026-11-05T09:04:52Z"},
			{"id": "dev-task", "team_id": "development", "status": "done", "created_at": "2026-11-05T06:42:44Z", "updated_at": "2026-11-05T09:04:52Z"},
		}})
	})
	mux.HandleFunc("GET /api/v1/tasks/root", func(w http.ResponseWriter, _ *http.Request) {
		send(w, map[string]any{"id": "root", "sub_tasks": []map[string]any{{"id": "sub"}, {"id": "root"}}})
	})
	mux.HandleFunc("GET /api/v1/tasks/sub", func(w http.ResponseWriter, _ *http.Request) {
		send(w, map[string]any{"id": "sub", "sub_tasks": []map[string]any{}})
	})
	mux.HandleFunc("GET /api/v1/tasks/root/artifacts", func(w http.ResponseWriter, _ *http.Request) {
		send(w, map[string]any{"items": []map[string]any{{"id": "a1", "kind": "stage_report"}, {"id": "a2", "kind": "story_brief"}}})
	})
	mux.HandleFunc("GET /api/v1/tasks/root/artifacts/a2", func(w http.ResponseWriter, _ *http.Request) {
		send(w, map[string]any{"content": map[string]any{"channel_id": "8bitnews-us", "development": map[string]any{"hub_development_id": devID}}})
	})
	mux.HandleFunc("GET /api/v1/projects/p/token-usage", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("from") != "2026-11-04" {
			t.Errorf("from = %q", r.URL.Query().Get("from"))
		}
		switch r.URL.Query().Get("task_id") {
		case "root":
			send(w, usage(1, map[string]int64{"editor": 100, "brand_new_agent": 7}))
		case "sub":
			send(w, usage(2, map[string]int64{"analyst": 1000}))
		default:
			http.NotFound(w, r)
		}
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, &keys
}

type fakeStore struct {
	states   map[string]store.CostRunState
	runs     []store.CostRun
	builds   map[string]float64
	buildDev map[string]string
	noDev    bool
}

func (f *fakeStore) CostRunStates(context.Context) (map[string]store.CostRunState, error) {
	return f.states, nil
}
func (f *fakeStore) SaveCostRun(_ context.Context, r store.CostRun) error {
	f.runs = append(f.runs, r)
	return nil
}
func (f *fakeStore) DevelopmentExists(context.Context, string) (bool, error) { return !f.noDev, nil }
func (f *fakeStore) CostBuildKeys(context.Context) (map[string]bool, error) {
	out := map[string]bool{}
	for k := range f.builds {
		out[k] = true
	}
	return out, nil
}
func (f *fakeStore) SaveCostBuild(_ context.Context, key, dev string, usd float64) error {
	if f.builds == nil {
		f.builds, f.buildDev = map[string]float64{}, map[string]string{}
	}
	f.builds[key], f.buildDev[key] = usd, dev
	return nil
}
func (f *fakeStore) GetChannel(_ context.Context, id string) (store.Channel, error) {
	if id != "8bitnews-us" {
		return store.Channel{}, store.ErrNotFound
	}
	return store.Channel{ID: id}, nil
}

type fakeMedia struct {
	objects map[string][]byte
	opens   int
}

func (f *fakeMedia) List(context.Context, string) ([]string, error) {
	var keys []string
	for k := range f.objects {
		keys = append(keys, k)
	}
	return keys, nil
}
func (f *fakeMedia) Open(_ context.Context, key string) (io.ReadCloser, int64, error) {
	f.opens++
	return io.NopCloser(bytes.NewReader(f.objects[key])), int64(len(f.objects[key])), nil
}

func TestSyncAddsUpARunAndItsSubtasks(t *testing.T) {
	srv, keys := fakePolyMaths(t)
	st := &fakeStore{states: map[string]store.CostRunState{}}
	s := NewSyncer(NewClient(srv.URL+"/api/v1", "read-key", "p", "news"), st, &fakeMedia{}, time.Hour, discard())
	s.Once(context.Background())

	if len(st.runs) != 1 {
		t.Fatalf("saved %+v, want the one Newsroom run", st.runs)
	}
	r := st.runs[0]
	if r.TaskID != "root" || r.ChannelID != "8bitnews-us" || r.DevelopmentID != devID {
		t.Errorf("%+v", r)
	}
	if r.AgentRuns != 3 || r.MeasuredRuns != 3 {
		t.Errorf("agent runs %d/%d: the subtask's should be added, and root listed as its own subtask only once", r.AgentRuns, r.MeasuredRuns)
	}
	if want := (store.TokenCounts{Input: 3, Output: 20, CacheWrite: 40, CacheRead: 1107}); r.Tokens != want {
		t.Errorf("tokens %+v, want %+v", r.Tokens, want)
	}
	if r.Stages[store.StageEditor] != 100 || r.Stages[store.StageResearch] != 1000 || r.Stages[store.StageOther] != 7 {
		t.Errorf("stages %v: an unlisted agent's tokens must stay in the total, under other", r.Stages)
	}
	if len(*keys) == 0 || (*keys)[0] != "read-key" {
		t.Errorf("the key wasn't sent: %v", *keys)
	}
}

func TestAFailedUsageReadSavesNothing(t *testing.T) {
	st := &fakeStore{states: map[string]store.CostRunState{}}
	s := NewSyncer(partialFailure{}, st, &fakeMedia{}, time.Hour, discard())
	s.Once(context.Background())
	if len(st.runs) != 0 {
		t.Fatalf("saved %+v: a partial sum would read as a cheaper video", st.runs)
	}
}

type partialFailure struct{}

func (partialFailure) Runs(context.Context) ([]Run, error) {
	return []Run{{ID: "root", Status: "done"}}, nil
}
func (partialFailure) Subtasks(context.Context, string) ([]string, error) {
	return []string{"sub"}, nil
}
func (partialFailure) Usage(_ context.Context, id string, _ time.Time) (Usage, error) {
	if id == "sub" {
		return Usage{}, errors.New("boom")
	}
	return Usage{AgentRuns: 1, MeasuredRuns: 1}, nil
}
func (partialFailure) Brief(context.Context, string) (Brief, bool, error) {
	return Brief{ChannelID: "8bitnews-us"}, true, nil
}

func TestBriefChecksWhatAnAgentWrote(t *testing.T) {
	for content, want := range map[string]Brief{
		`{"channel_id":"8bitnews-us","development":{"hub_development_id":"` + devID + `"}}`: {ChannelID: "8bitnews-us", DevelopmentID: devID},
		`{"channel_id":"8bitnews-us","story":{"hub_story_id":null}}`:                        {ChannelID: "8bitnews-us"},
		`{"channel_id":"8bitnews-us","development":{"hub_development_id":"../../etc"}}`:     {ChannelID: "8bitnews-us"},
	} {
		if got, ok := parseBrief(json.RawMessage(content)); !ok || got != want {
			t.Errorf("%s: %+v, %v", content, got, ok)
		}
	}
	if _, ok := parseBrief(json.RawMessage(`{"story":{}}`)); ok {
		t.Error("a brief with no channel was accepted")
	}
}

func TestClientKeepsItsKeyOnARedirect(t *testing.T) {
	leaked := false
	other := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		leaked = leaked || r.Header.Get("X-API-Key") != ""
	}))
	defer other.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL, http.StatusFound)
	}))
	defer srv.Close()
	if _, err := NewClient(srv.URL, "read-key", "p", "news").Runs(context.Background()); err == nil {
		t.Error("a redirect was accepted as an answer")
	}
	if leaked {
		t.Error("the key went to another address")
	}
}

func TestBuildsAreReadOnce(t *testing.T) {
	good := "releases/" + devID + "/a-package.tar.gz"
	bad := "releases/" + devID + "/b-package.tar.gz"
	media := &fakeMedia{objects: map[string][]byte{
		good:                                 packageArchive(t, map[string]string{"package.json": `{"images":{"cost_usd":0.5}}`}),
		bad:                                  []byte("not gzip"),
		"releases/" + devID + "/c-video.mp4": []byte("x"),
	}}
	st := &fakeStore{states: map[string]store.CostRunState{}}
	s := NewSyncer(nil, st, media, time.Hour, discard())
	s.Once(context.Background())
	if st.builds[good] != 0.5 || st.buildDev[good] != devID || len(st.builds) != 1 {
		t.Fatalf("builds %v", st.builds)
	}
	if media.opens != 2 {
		t.Fatalf("opened %d objects, want the two packages and not the video", media.opens)
	}
	s.Once(context.Background())
	if media.opens != 2 {
		t.Errorf("opened %d objects on the second pass: a read build and a bad one shouldn't be read again", media.opens)
	}
}

func TestABuildOfAnUnknownDevelopmentIsSkipped(t *testing.T) {
	key := "releases/" + devID + "/a-package.tar.gz"
	media := &fakeMedia{objects: map[string][]byte{key: packageArchive(t, map[string]string{"package.json": `{}`})}}
	st := &fakeStore{states: map[string]store.CostRunState{}, noDev: true}
	NewSyncer(nil, st, media, time.Hour, discard()).Once(context.Background())
	if len(st.builds) != 0 || media.opens != 0 {
		t.Errorf("builds %v, opens %d", st.builds, media.opens)
	}
}
