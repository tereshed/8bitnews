package service_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tereshed/8bitnews/internal/service"
	"github.com/tereshed/8bitnews/internal/sources"
	"github.com/tereshed/8bitnews/internal/store"
	"github.com/tereshed/8bitnews/internal/testfixture"
)

func TestReleaseWithSourcesRollsBackAndRetriesConcurrently(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	seedChannel(t, st, "atomic")
	story, err := st.CreateStory(ctx, "atomic", "Atomic release", "A fixture story")
	if err != nil {
		t.Fatal(err)
	}
	dev, err := st.CreateDevelopment(ctx, story.ID, "A new event", time.Now(), []byte(`[]`), "")
	if err != nil {
		t.Fatal(err)
	}
	p := store.NewReleaseParams{DevelopmentID: dev.ID, ChannelID: "atomic", ArmID: "default"}
	bad := []store.ReleaseSource{{URL: "invalid\x00source", CheckStatus: "ok", CheckedAt: time.Now()}}
	if _, _, err := st.CreateReleaseWithSources(ctx, p, bad); err == nil {
		t.Fatal("invalid source should fail")
	}
	if _, err := st.FindActiveRelease(ctx, dev.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("partial release survived: %v", err)
	}
	results := make(chan store.Release, 2)
	failures := make(chan error, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Go(func() {
			r, _, err := st.CreateReleaseWithSources(ctx, p, []store.ReleaseSource{{URL: "https://example.com", CheckStatus: "ok", CheckedAt: time.Now()}})
			results <- r
			failures <- err
		})
	}
	wg.Wait()
	a, b := <-results, <-results
	if e := <-failures; e != nil {
		t.Fatal(e)
	}
	if e := <-failures; e != nil {
		t.Fatal(e)
	}
	if a.ID == "" || a.ID != b.ID {
		t.Fatalf("different releases for identical retries: %s %s", a.ID, b.ID)
	}
	sources, err := st.ListReleaseSources(ctx, a.ID)
	if err != nil || len(sources) != 1 {
		t.Fatalf("sources=%+v error=%v", sources, err)
	}
	p.Script = []byte(`{"different":true}`)
	if _, _, err := st.CreateReleaseWithSources(ctx, p, nil); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("changed build was not rejected: %v", err)
	}
}

func TestReleaseRejectsMediaWithAnotherChecksum(t *testing.T) {
	st := newTestStore(t)
	media := newTestMedia(t)
	ctx := context.Background()
	seedChannel(t, st, "checksums")
	server := newFakeSourceServer(t)
	svc := service.New(service.Deps{Store: st, Media: media, Checker: sources.NewChecker(time.Second, true)})
	dev := registerDevelopment(t, svc, "checksums", "Checksum test")
	files := service.MediaManifest{Video: uploadFixture(t, svc, dev, "video.mp4"), Subtitles: uploadFixture(t, svc, dev, "subtitles.srt"), Cover: uploadFixture(t, svc, dev, "cover.png")}
	dossier, script := dossierAndScript(server.URL + "/ok")
	m := testfixture.Complete(service.ReleaseManifest{DevelopmentID: dev, ChannelID: "checksums", Media: files, ResearchDossier: dossier, Script: script})
	var pkg map[string]any
	_ = json.Unmarshal(m.VideoPackage, &pkg)
	pkg["files"].(map[string]any)["video"].(map[string]any)["sha256"] = strings.Repeat("0", 64)
	m.VideoPackage, _ = json.Marshal(pkg)
	if _, err := svc.ReleaseSubmit(ctx, m); err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("wrong media accepted: %v", err)
	}
	if _, err := st.FindActiveRelease(ctx, dev); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("rejected media left a release: %v", err)
	}
}

func TestModeratorReworkIsDurableAndDeduplicated(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	r := seedRelease(t, st, "rework")
	// A historical development may supply the task ID when the release has none.
	dev, err := st.GetDevelopment(ctx, r.DevelopmentID)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok, err := st.ClaimReleaseForCard(ctx, time.Minute); err != nil || !ok {
		t.Fatal(err)
	}
	if _, _, err := st.DecideRelease(ctx, r.ID, "changes_requested", "moderator"); err != nil {
		t.Fatal(err)
	}
	if err := st.SetModerationNote(ctx, r.ID, "Fix the cover"); err != nil {
		t.Fatal(err)
	}
	if err := st.SetModerationNote(ctx, r.ID, "Fix the cover"); err != nil {
		t.Fatal(err)
	}
	// Missing origin is escalated rather than sending feedback to a guessed task.
	if _, ok, err := st.ClaimRework(ctx); err != nil || ok {
		t.Fatalf("unexpected dispatch without source task: %v %v", ok, err)
	}
	d2, err := st.CreateDevelopment(ctx, dev.StoryID, "Another event", time.Now(), []byte(`[]`), "11111111-1111-4111-8111-111111111111")
	if err != nil {
		t.Fatal(err)
	}
	r2, err := st.CreateRelease(ctx, store.NewReleaseParams{DevelopmentID: d2.ID, ChannelID: r.ChannelID, ArmID: "default"})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok, err := st.ClaimReleaseForCard(ctx, time.Minute); err != nil || !ok {
		t.Fatal(err)
	}
	if _, _, err := st.DecideRelease(ctx, r2.ID, "changes_requested", "moderator"); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := st.SetModerationNote(ctx, r2.ID, "Fix the audio"); err != nil {
			t.Fatal(err)
		}
	}
	job, ok, err := st.ClaimRework(ctx)
	if err != nil || !ok || job.TaskID != "11111111-1111-4111-8111-111111111111" {
		t.Fatalf("job=%+v found=%v error=%v", job, ok, err)
	}
	if _, ok, err := st.ClaimRework(ctx); err != nil || ok {
		t.Fatalf("concurrent dispatcher stole an active lease: %v %v", ok, err)
	}
	stale := job
	stale.LeaseToken = "33333333-3333-4333-8333-333333333333"
	if err := st.FinishRework(ctx, stale, "", false); err != nil {
		t.Fatal(err)
	}
	if err := st.FinishRework(ctx, job, "", false); err != nil {
		t.Fatal(err)
	}
	if err := st.SetModerationNote(ctx, r2.ID, "Fix the audio"); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := st.ClaimRework(ctx); err != nil || ok {
		t.Fatalf("delivered note was enqueued twice: %v %v", ok, err)
	}
}

func TestStoryReservationDeduplicatesConcurrentTasks(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	seedChannel(t, st, "reserve")
	p := store.Registration{ChannelID: "reserve", Title: "A new headline", Summary: "A fixture story", DevelopmentSummary: "A new event", ReportedAt: time.Now(), SeedURLs: []byte(`[]`), EventKey: "event-one"}
	ids := make(chan string, 2)
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for _, task := range []string{"11111111-1111-4111-8111-111111111111", "22222222-2222-4222-8222-222222222222"} {
		wg.Go(func() {
			request := p
			request.TaskID = task
			_, dev, err := st.RegisterDevelopment(ctx, request)
			ids <- dev
			errs <- err
		})
	}
	wg.Wait()
	a, b := <-ids, <-ids
	if err := <-errs; err != nil {
		t.Fatal(err)
	}
	if err := <-errs; err != nil {
		t.Fatal(err)
	}
	if a == "" || a != b {
		t.Fatalf("duplicate event: %s %s", a, b)
	}
	// Both tasks must remain reserved, including the one that reused the event.
	p.EventKey = "event-two"
	for _, task := range []string{"11111111-1111-4111-8111-111111111111", "22222222-2222-4222-8222-222222222222"} {
		p.TaskID = task
		if _, _, err := st.RegisterDevelopment(ctx, p); !errors.Is(err, store.ErrConflict) {
			t.Fatalf("task changed its event: %v", err)
		}
	}
}
