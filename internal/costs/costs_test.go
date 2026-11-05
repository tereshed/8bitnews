package costs

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
		{"the figure", map[string]string{"package/video.mp4": "x", "package/package.json": `{"images":{"cost_usd":0.8148}}`}, 0.8148, false},
		{"no generated pictures", map[string]string{"package/package.json": `{"duration_s":80}`}, 0, false},
		{"no package.json", map[string]string{"package/video.mp4": "x"}, 0, true},
		{"only a planted one elsewhere", map[string]string{"package/images/package.json": `{"images":{"cost_usd":0}}`}, 0, true},
		{"not json", map[string]string{"package/package.json": `nope`}, 0, true},
		{"images with no cost", map[string]string{"package/package.json": `{"images":{"cost_usd":null}}`}, 0, true},
		{"a negative figure", map[string]string{"package/package.json": `{"images":{"cost_usd":-1}}`}, 0, true},
		{"an implausible figure", map[string]string{"package/package.json": `{"images":{"cost_usd":999}}`}, 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, sum, err := imagesCost(bytes.NewReader(packageArchive(t, tc.files)))
			if tc.bad != (err != nil) {
				t.Fatalf("error %v, want bad=%v", err, tc.bad)
			}
			if tc.bad {
				return
			}
			if got != tc.want || len(sum) != 64 {
				t.Fatalf("got %v, %q; want %v and a sha256", got, sum, tc.want)
			}
		})
	}
	if _, _, err := imagesCost(strings.NewReader("not a gzip")); err == nil {
		t.Fatal("a non-gzip file was accepted")
	}
	// A real package beside a planted one: the real one counts, in any order.
	got, _, err := imagesCost(bytes.NewReader(packageArchive(t, map[string]string{
		"package/images/package.json": `{"images":{"cost_usd":0}}`,
		"package/package.json":        `{"images":{"cost_usd":0.8}}`,
	})))
	if err != nil || got != 0.8 {
		t.Fatalf("got %v, %v", got, err)
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
	state := store.CostRunState{Complete: true, SourceUpdatedAt: ago(2 * time.Hour), MeasuredAt: ago(time.Hour)}
	recent := ago(48 * time.Hour)
	for _, tc := range []struct {
		name  string
		run   Run
		state store.CostRunState
		known bool
		want  bool
	}{
		{"never measured", Run{CreatedAt: ago(40 * 24 * time.Hour), UpdatedAt: ago(40 * 24 * time.Hour), Status: "done"}, store.CostRunState{}, false, true},
		{"updated since", Run{CreatedAt: recent, UpdatedAt: ago(time.Minute), Status: "done"}, state, true, true},
		{"still running", Run{CreatedAt: recent, UpdatedAt: ago(2 * time.Hour), Status: "awaiting_delegation"}, state, true, true},
		{"finished, checked recently", Run{CreatedAt: recent, UpdatedAt: ago(2 * time.Hour), Status: "done"}, state, true, false},
		{"finished, checked a day ago", Run{CreatedAt: recent, UpdatedAt: ago(2 * time.Hour), Status: "done"},
			store.CostRunState{Complete: true, SourceUpdatedAt: ago(2 * time.Hour), MeasuredAt: ago(25 * time.Hour)}, true, true},
		{"couldn't be read last time", Run{CreatedAt: recent, UpdatedAt: ago(2 * time.Hour), Status: "done"},
			store.CostRunState{SourceUpdatedAt: ago(2 * time.Hour), MeasuredAt: ago(time.Minute)}, true, true},
		{"started too long ago, even if touched today", Run{CreatedAt: ago(26 * 24 * time.Hour), UpdatedAt: ago(time.Minute), Status: "awaiting_delegation"},
			store.CostRunState{Complete: true, SourceUpdatedAt: ago(27 * 24 * time.Hour), MeasuredAt: ago(26 * 24 * time.Hour)}, true, false},
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
		// The older brief comes first, or last: the date decides, not the order.
		send(w, map[string]any{"total": 3, "items": []map[string]any{
			{"id": "new", "kind": "story_brief", "created_at": "2026-11-05T07:00:00Z"},
			{"id": "a1", "kind": "stage_report", "created_at": "2026-11-05T08:00:00Z"},
			{"id": "old", "kind": "story_brief", "created_at": "2026-11-05T06:50:00Z"},
		}})
	})
	mux.HandleFunc("GET /api/v1/tasks/root/artifacts/new", func(w http.ResponseWriter, _ *http.Request) {
		send(w, map[string]any{"content": map[string]any{"channel_id": "8bitnews-us", "development": map[string]any{"hub_development_id": devID}}})
	})
	mux.HandleFunc("GET /api/v1/tasks/root/artifacts/old", func(w http.ResponseWriter, _ *http.Request) {
		send(w, map[string]any{"content": map[string]any{"channel_id": "8bitnews-us"}})
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
	states     map[string]store.CostRunState
	runs       []store.CostRun
	unread     []store.CostRun
	builds     map[string]store.CostBuild
	syncs      map[string]int
	noDev      bool
	devChannel string
}

func newFakeStore() *fakeStore {
	return &fakeStore{states: map[string]store.CostRunState{}, builds: map[string]store.CostBuild{}, syncs: map[string]int{}}
}

func (f *fakeStore) CostRunStates(context.Context) (map[string]store.CostRunState, error) {
	return f.states, nil
}
func (f *fakeStore) SaveCostRun(_ context.Context, r store.CostRun) error {
	f.runs = append(f.runs, r)
	return nil
}
func (f *fakeStore) MarkCostRunUnread(_ context.Context, r store.CostRun) error {
	f.unread = append(f.unread, r)
	return nil
}
func (f *fakeStore) DevelopmentChannel(context.Context, string) (string, bool, error) {
	if f.noDev {
		return "", false, nil
	}
	if f.devChannel != "" {
		return f.devChannel, true, nil
	}
	return "8bitnews-us", true, nil
}
func (f *fakeStore) CostBuildKeys(context.Context) (map[string]bool, error) {
	out := map[string]bool{}
	for k := range f.builds {
		out[k] = true
	}
	return out, nil
}
func (f *fakeStore) SaveCostBuild(_ context.Context, b store.CostBuild) error {
	f.builds[b.Key] = b
	return nil
}
func (f *fakeStore) SetCostSyncOK(_ context.Context, source string) error {
	f.syncs[source]++
	return nil
}
func (f *fakeStore) GetChannel(_ context.Context, id string) (store.Channel, error) {
	if id != "8bitnews-us" && id != "8bitnews-es" {
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
	st := newFakeStore()
	s := NewSyncer(NewClient(srv.URL+"/api/v1", "read-key", "p", "news"), st, &fakeMedia{}, time.Hour, discard())
	s.now = func() time.Time { return time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC) }
	s.Once(context.Background())

	if len(st.runs) != 1 {
		t.Fatalf("saved %+v, want the one Newsroom run", st.runs)
	}
	r := st.runs[0]
	if r.TaskID != "root" || r.ChannelID != "8bitnews-us" || r.DevelopmentID != devID {
		t.Errorf("%+v: the newest brief names the development", r)
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
	if !r.Complete {
		t.Error("a run read in time is marked incomplete")
	}
	if st.syncs[store.CostSourceRuns] != 1 || st.syncs[store.CostSourceBuilds] != 1 {
		t.Errorf("sync dates %v", st.syncs)
	}
	if len(*keys) == 0 || (*keys)[0] != "read-key" {
		t.Errorf("the key wasn't sent: %v", *keys)
	}
}

type partialFailure struct{}

func (partialFailure) Runs(context.Context) ([]Run, error) {
	return []Run{{ID: "root", Status: "done", CreatedAt: time.Now(), UpdatedAt: time.Now()}}, nil
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

func TestARunThatCantBeReadIsMarkedNotDropped(t *testing.T) {
	st := newFakeStore()
	s := NewSyncer(partialFailure{}, st, &fakeMedia{}, time.Hour, discard())
	s.Once(context.Background())
	if len(st.runs) != 0 {
		t.Fatalf("saved %+v: a partial sum would read as a cheaper video", st.runs)
	}
	if len(st.unread) != 1 || st.unread[0].TaskID != "root" || st.unread[0].ChannelID != "8bitnews-us" {
		t.Fatalf("unread %+v: the page has to be able to say a run is missing", st.unread)
	}
	if st.syncs[store.CostSourceRuns] != 0 {
		t.Error("a pass with a failed run still vouches for the tokens' date")
	}
	if st.syncs[store.CostSourceBuilds] != 1 {
		t.Error("a failed run held up the builds")
	}
}

type lateRun struct{ partialFailure }

func (lateRun) Runs(context.Context) ([]Run, error) {
	at := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	return []Run{{ID: "root", Status: "done", CreatedAt: at, UpdatedAt: at}}, nil
}
func (lateRun) Subtasks(context.Context, string) ([]string, error) { return nil, nil }

// A run first read after PolyMaths started forgetting it says so.
func TestALateFirstReadIsIncomplete(t *testing.T) {
	st := newFakeStore()
	s := NewSyncer(lateRun{}, st, &fakeMedia{}, time.Hour, discard())
	s.now = func() time.Time { return time.Date(2026, 12, 20, 0, 0, 0, 0, time.UTC) }
	s.Once(context.Background())
	if len(st.runs) != 1 || st.runs[0].Complete {
		t.Fatalf("runs %+v", st.runs)
	}
}

type briefed struct{ brief Brief }

func (b briefed) Runs(context.Context) ([]Run, error) {
	return []Run{{ID: "root", Status: "done", CreatedAt: time.Now(), UpdatedAt: time.Now()}}, nil
}
func (briefed) Subtasks(context.Context, string) ([]string, error) { return nil, nil }
func (briefed) Usage(context.Context, string, time.Time) (Usage, error) {
	return Usage{AgentRuns: 1, MeasuredRuns: 1}, nil
}
func (b briefed) Brief(context.Context, string) (Brief, bool, error) { return b.brief, true, nil }

// A development decides the channel, whatever the brief says.
func TestADevelopmentDecidesTheChannel(t *testing.T) {
	st := newFakeStore()
	st.devChannel = "8bitnews-es"
	s := NewSyncer(briefed{Brief{ChannelID: "8bitnews-us", DevelopmentID: devID}}, st, &fakeMedia{}, time.Hour, discard())
	s.Once(context.Background())
	if len(st.runs) != 1 || st.runs[0].ChannelID != "8bitnews-es" || st.runs[0].DevelopmentID != devID {
		t.Fatalf("runs %+v", st.runs)
	}
}

type noBrief struct {
	partialFailure
	asked int
}

func (p *noBrief) Runs(context.Context) ([]Run, error) {
	at := time.Date(2026, 11, 5, 0, 0, 0, 0, time.UTC)
	return []Run{{ID: "root", Status: "done", CreatedAt: at, UpdatedAt: at}}, nil
}

func (p *noBrief) Brief(context.Context, string) (Brief, bool, error) {
	p.asked++
	return Brief{}, false, nil
}

// A run with nothing usable isn't asked about again on every pass.
func TestARunWithNoBriefIsNotReadEveryPass(t *testing.T) {
	pm := &noBrief{}
	s := NewSyncer(pm, newFakeStore(), &fakeMedia{}, time.Hour, discard())
	s.Once(context.Background())
	s.Once(context.Background())
	if pm.asked != 1 {
		t.Fatalf("asked %d times", pm.asked)
	}
}

// The server may cap a page below what was asked for.
func TestRunsFollowsWhatTheServerReturns(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		offset := 0
		_, _ = fmt.Sscan(r.URL.Query().Get("offset"), &offset)
		var tasks []map[string]any
		for i := offset; i < 250 && i < offset+100; i++ {
			tasks = append(tasks, map[string]any{"id": fmt.Sprint(i), "team_id": "news"})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"total": 250, "tasks": tasks})
	}))
	defer srv.Close()
	runs, err := NewClient(srv.URL, "k", "p", "news").Runs(context.Background())
	if err != nil || len(runs) != 250 {
		t.Fatalf("%d runs, %v", len(runs), err)
	}
}

func TestBriefChecksWhatAnAgentWrote(t *testing.T) {
	for content, want := range map[string]Brief{
		`{"channel_id":"8bitnews-us","development":{"hub_development_id":"` + devID + `"}}`: {ChannelID: "8bitnews-us", DevelopmentID: devID},
		`{"channel_id":"8bitnews-us","story":{"hub_story_id":null}}`:                        {ChannelID: "8bitnews-us"},
		`{"channel_id":"8bitnews-us","development":{"hub_development_id":"../../etc"}}`:     {ChannelID: "8bitnews-us"},
		`{"channel_id":"8bitnews-us","development":"oops"}`:                                 {ChannelID: "8bitnews-us"},
		`{"channel_id":"8bitnews-us","development":{"hub_development_id":42}}`:              {ChannelID: "8bitnews-us"},
		`"{\"channel_id\":\"8bitnews-us\"}"`:                                                {ChannelID: "8bitnews-us"},
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
		good:                                 packageArchive(t, map[string]string{"package/package.json": `{"images":{"cost_usd":0.5}}`}),
		bad:                                  []byte("not gzip"),
		"releases/" + devID + "/c-video.mp4": []byte("x"),
	}}
	st := newFakeStore()
	s := NewSyncer(nil, st, media, time.Hour, discard())

	s.Once(context.Background())
	if b := st.builds[good]; b.USD == nil || *b.USD != 0.5 || b.DevelopmentID != devID || b.PackageSHA256 == "" {
		t.Fatalf("build %+v", b)
	}
	if media.opens != 2 {
		t.Fatalf("opened %d objects, want the two packages and not the video", media.opens)
	}
	if st.syncs[store.CostSourceBuilds] != 0 {
		t.Error("a pass with a failed build still vouches for the pictures' date")
	}

	// A failure may be a dropped connection: it's tried again, a few times...
	for i := 1; i < maxBuildAttempts; i++ {
		if _, stored := st.builds[bad]; stored {
			t.Fatalf("given up on the bad package after %d tries", i)
		}
		s.Once(context.Background())
	}
	// ...and then recorded as unreadable, which the page reports and a restart keeps.
	b, stored := st.builds[bad]
	if !stored || b.USD != nil {
		t.Fatalf("after %d tries: %+v, %v", maxBuildAttempts, b, stored)
	}
	if want := 1 + maxBuildAttempts; media.opens != want {
		t.Fatalf("opened %d times, want %d", media.opens, want)
	}
	s.Once(context.Background())
	if want := 1 + maxBuildAttempts; media.opens != want {
		t.Errorf("opened %d times after both were settled", media.opens)
	}
	if st.syncs[store.CostSourceBuilds] != 1 {
		t.Errorf("once settled, the pass should vouch for the date: %v", st.syncs)
	}
}

func TestABuildOfAnUnknownDevelopmentIsSkipped(t *testing.T) {
	key := "releases/" + devID + "/a-package.tar.gz"
	media := &fakeMedia{objects: map[string][]byte{key: packageArchive(t, map[string]string{"package/package.json": `{}`})}}
	st := newFakeStore()
	st.noDev = true
	NewSyncer(nil, st, media, time.Hour, discard()).Once(context.Background())
	if len(st.builds) != 0 || media.opens != 0 {
		t.Errorf("builds %v, opens %d", st.builds, media.opens)
	}
}

type hanging struct{ partialFailure }

func (hanging) Runs(ctx context.Context) ([]Run, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

type savedSignal struct {
	*fakeStore
	saved chan struct{}
}

func (l *savedSignal) SaveCostBuild(context.Context, store.CostBuild) error {
	l.saved <- struct{}{}
	return nil
}
func (l *savedSignal) CostBuildKeys(context.Context) (map[string]bool, error) {
	return map[string]bool{}, nil
}
func (l *savedSignal) SetCostSyncOK(context.Context, string) error { return nil }

// A slow PolyMaths mustn't hold up the pictures: Run keeps the two apart.
func TestRunReadsBuildsWhilePolyMathsHangs(t *testing.T) {
	key := "releases/" + devID + "/a-package.tar.gz"
	media := &fakeMedia{objects: map[string][]byte{key: packageArchive(t, map[string]string{"package/package.json": `{"images":{"cost_usd":0.5}}`})}}
	st := &savedSignal{fakeStore: newFakeStore(), saved: make(chan struct{}, 1)}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go NewSyncer(hanging{}, st, media, time.Hour, discard()).Run(ctx)
	select {
	case <-st.saved:
	case <-time.After(5 * time.Second):
		t.Fatal("no build was read while PolyMaths hung")
	}
}
