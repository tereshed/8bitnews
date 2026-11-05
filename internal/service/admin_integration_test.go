package service_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/tereshed/8bitnews/internal/store"
)

func TestAdminReleasesAndPublications(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	r := seedRelease(t, st, "ch-admin")

	rows, err := st.AdminReleases(ctx, 10)
	if err != nil {
		t.Fatalf("listing: %v", err)
	}
	if len(rows) != 1 || rows[0].ID != r.ID || rows[0].StoryTitle != "DOJ v. California" || rows[0].CoverKey != "releases/x/c.png" || rows[0].YouTubeURL != "" {
		t.Fatalf("rows: %+v", rows)
	}

	// A link: set, replaced, and shown in the list.
	if got, err := st.PublicationURL(ctx, r.ID, store.PlatformYouTube); err != nil || got != "" {
		t.Fatalf("no link yet: %q %v", got, err)
	}
	for _, link := range []string{"https://www.youtube.com/shorts/aaaaaaaaaaa", "https://www.youtube.com/shorts/bbbbbbbbbbb"} {
		if err := st.SetPublicationURL(ctx, r.ID, store.PlatformYouTube, link, "Kostya"); err != nil {
			t.Fatalf("setting: %v", err)
		}
		if got, err := st.PublicationURL(ctx, r.ID, store.PlatformYouTube); err != nil || got != link {
			t.Fatalf("after setting: %q %v", got, err)
		}
	}
	rows, _ = st.AdminReleases(ctx, 10)
	if rows[0].YouTubeURL != "https://www.youtube.com/shorts/bbbbbbbbbbb" {
		t.Errorf("list link: %q", rows[0].YouTubeURL)
	}

	// Another platform's link isn't YouTube's.
	if err := st.SetPublicationURL(ctx, r.ID, "tiktok", "https://www.tiktok.com/@x/video/1", "Kostya"); err != nil {
		t.Fatalf("setting tiktok: %v", err)
	}
	if err := st.SetPublicationURL(ctx, r.ID, store.PlatformYouTube, "", "Kostya"); err != nil {
		t.Fatalf("clearing: %v", err)
	}
	rows, _ = st.AdminReleases(ctx, 10)
	if rows[0].YouTubeURL != "" {
		t.Errorf("after clearing, list shows %q", rows[0].YouTubeURL)
	}

	// A release that doesn't exist takes no link.
	if err := st.SetPublicationURL(ctx, "00000000-0000-0000-0000-000000000000", store.PlatformYouTube, "https://x", "K"); err == nil {
		t.Error("a link was stored for a release that doesn't exist")
	}
}

func TestAdminReleasesOddDuration(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	seedChannel(t, st, "ch-odd")
	story, err := st.CreateStory(ctx, "ch-odd", "Odd", "odd")
	if err != nil {
		t.Fatal(err)
	}
	dev, err := st.CreateDevelopment(ctx, story.ID, "d", time.Now(), json.RawMessage(`[]`), "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateRelease(ctx, store.NewReleaseParams{DevelopmentID: dev.ID, ChannelID: "ch-odd", ArmID: "default",
		VideoPackage: json.RawMessage(`{"duration_s":"not a number"}`), Script: json.RawMessage(`{"metadata":{"youtube":{"title":"T"}}}`)}); err != nil {
		t.Fatal(err)
	}
	rows, err := st.AdminReleases(ctx, 10)
	if err != nil {
		t.Fatalf("a non-numeric duration breaks the list: %v", err)
	}
	if rows[0].DurationS != 0 || rows[0].Title != "T" {
		t.Errorf("%+v", rows[0])
	}
}

func TestDevelopmentsInProgress(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	r := seedRelease(t, st, "ch-progress")
	if got, err := st.DevelopmentsInProgress(ctx, time.Hour); err != nil || len(got) != 0 {
		t.Fatalf("a development with a release counts as in progress: %+v %v", got, err)
	}
	story, err := st.CreateStory(ctx, r.ChannelID, "Being made", "s")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateDevelopment(ctx, story.ID, "new turn", time.Now(), json.RawMessage(`[]`), ""); err != nil {
		t.Fatal(err)
	}
	got, err := st.DevelopmentsInProgress(ctx, time.Hour)
	if err != nil || len(got) != 1 || got[0].StoryTitle != "Being made" || got[0].ChannelID != r.ChannelID {
		t.Fatalf("%+v %v", got, err)
	}
	if got, _ := st.DevelopmentsInProgress(ctx, -time.Hour); len(got) != 0 {
		t.Errorf("an old development counts as in progress: %+v", got)
	}
}

func TestAdminReleasesKeepsOldOpenOnes(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	closed := seedRelease(t, st, "ch-old")
	if _, ok, err := st.ClaimReleaseForCard(ctx, time.Hour); err != nil || !ok {
		t.Fatalf("claiming: %v %v", ok, err)
	}
	if _, ok, err := st.DecideRelease(ctx, closed.ID, "rejected", "K"); err != nil || !ok {
		t.Fatalf("rejecting: %v %v", ok, err)
	}
	open := seedRelease(t, st, "ch-old")
	newest := seedRelease(t, st, "ch-old")

	rows, err := st.AdminReleases(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, r := range rows {
		got[r.ID] = true
	}
	if !got[newest.ID] || !got[open.ID] || got[closed.ID] {
		t.Errorf("newest %v, older open %v, older closed %v (want true, true, false)", got[newest.ID], got[open.ID], got[closed.ID])
	}
	if len(rows) > 0 && (len(rows[0].AllowedPlatforms) != 1 || rows[0].AllowedPlatforms[0] != "youtube") {
		t.Errorf("allowed platforms: %v", rows[0].AllowedPlatforms)
	}
}

func TestAdminSessionEpoch(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	const user = int64(35)
	if got, err := st.AdminSessionEpoch(ctx, user); err != nil || got != 0 {
		t.Fatalf("before any sign-out: %d %v", got, err)
	}
	for want := 1; want <= 2; want++ {
		if err := st.RaiseAdminSessionEpoch(ctx, user); err != nil {
			t.Fatal(err)
		}
		if got, err := st.AdminSessionEpoch(ctx, user); err != nil || got != want {
			t.Errorf("after %d sign-outs: %d %v", want, got, err)
		}
	}
	if got, _ := st.AdminSessionEpoch(ctx, 99); got != 0 {
		t.Errorf("another user's epoch moved: %d", got)
	}
}

func TestWithdrawRecordsWho(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	r := seedRelease(t, st, "ch-withdraw")
	if _, ok, err := st.ClaimReleaseForCard(ctx, time.Hour); err != nil || !ok {
		t.Fatalf("claiming: %v %v", ok, err)
	}
	if _, ok, err := st.DecideRelease(ctx, r.ID, "approved", "Approver"); err != nil || !ok {
		t.Fatalf("approving: %v %v", ok, err)
	}
	got, ok, err := st.WithdrawRelease(ctx, r.ID, "Closer")
	if err != nil || !ok {
		t.Fatalf("withdrawing: %v %v", ok, err)
	}
	if got.Status != "rejected" || got.Moderation.By != "Closer" {
		t.Errorf("%s by %q", got.Status, got.Moderation.By)
	}
}
