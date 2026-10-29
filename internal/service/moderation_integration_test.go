package service_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/tereshed/8bitnews/internal/service"
	"github.com/tereshed/8bitnews/internal/store"
)

// seedRelease inserts a story, a development and a submitted release.
func seedRelease(t *testing.T, st *store.Store, channelID string) store.Release {
	t.Helper()
	ctx := context.Background()
	seedChannel(t, st, channelID)
	story, err := st.CreateStory(ctx, channelID, "DOJ v. California", "noncitizen voter list")
	if err != nil {
		t.Fatalf("creating story: %v", err)
	}
	dev, err := st.CreateDevelopment(ctx, story.ID, "Judge weighs a special master", time.Now(), json.RawMessage(`[]`), "")
	if err != nil {
		t.Fatalf("creating development: %v", err)
	}
	r, err := st.CreateRelease(ctx, store.NewReleaseParams{
		DevelopmentID: dev.ID, ChannelID: channelID, ArmID: "default",
		Media:            json.RawMessage(`{"video":"releases/x/video.mp4","subtitles":"releases/x/s.srt","cover":"releases/x/c.png"}`),
		AllowedPlatforms: []string{"youtube"},
	})
	if err != nil {
		t.Fatalf("creating release: %v", err)
	}
	return r
}

func TestModerationFlow(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	r := seedRelease(t, st, "mod-us")

	claimed, ok, err := st.ClaimReleaseForCard(ctx, time.Hour)
	if err != nil || !ok || claimed.ID != r.ID || claimed.Status != "pending_approval" {
		t.Fatalf("ClaimReleaseForCard = %+v, %v, %v; want the submitted release, claimed", claimed, ok, err)
	}
	if _, ok, err := st.ClaimReleaseForCard(ctx, time.Hour); err != nil || ok {
		t.Fatalf("a second claim = %v, %v; want none: the release is claimed and not yet due a retry", ok, err)
	}
	if again, ok, err := st.ClaimReleaseForCard(ctx, -time.Second); err != nil || !ok || again.ID != r.ID {
		t.Fatalf("a claim past the retry delay = %v, %v; want the uncarded release again", ok, err)
	}
	rc, err := st.ReleaseContextOf(ctx, r.ID)
	if err != nil || rc.StoryTitle != "DOJ v. California" || rc.DevelopmentSummary != "Judge weighs a special master" {
		t.Fatalf("ReleaseContextOf = %+v, %v", rc, err)
	}
	if err := st.SetModerationNote(ctx, r.ID, "too early"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("a reason before any decision: %v; want ErrNotFound", err)
	}

	if err := st.SetCardMessage(ctx, r.ID, -100, 42); err != nil {
		t.Fatalf("SetCardMessage: %v", err)
	}
	if _, ok, _ := st.ClaimReleaseForCard(ctx, -time.Second); ok {
		t.Fatal("a release with its card posted was claimed again")
	}
	got, err := st.GetRelease(ctx, r.ID)
	if err != nil || got.Status != "pending_approval" || got.Moderation.MessageID == nil || *got.Moderation.MessageID != 42 {
		t.Fatalf("after the card: %+v, %v", got, err)
	}

	decided, changed, err := st.DecideRelease(ctx, r.ID, "rejected", "Kostya (@kteresh)")
	if err != nil || !changed || decided.Status != "rejected" || decided.Moderation.By != "Kostya (@kteresh)" || decided.Moderation.At == nil {
		t.Fatalf("DecideRelease = %+v, %v, %v", decided, changed, err)
	}
	again, changed, err := st.DecideRelease(ctx, r.ID, "approved", "someone else")
	if err != nil || changed || again.Status != "rejected" {
		t.Fatalf("a second decision = %s, %v, %v; want the first one kept", again.Status, changed, err)
	}
	if _, _, err := st.DecideRelease(ctx, r.ID, "published", "x"); err == nil {
		t.Fatal("DecideRelease accepted 'published', which is not a moderation decision")
	}
	if _, _, err := st.DecideRelease(ctx, "00000000-0000-0000-0000-000000000000", "approved", "x"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("deciding a missing release: %v; want ErrNotFound", err)
	}

	if err := st.SetModerationNote(ctx, r.ID, "the cover shows the wrong person"); err != nil {
		t.Fatalf("SetModerationNote: %v", err)
	}
	if err := st.LogEvent(ctx, "moderation", map[string]any{"release_id": r.ID, "action": "rejected"}); err != nil {
		t.Fatalf("LogEvent: %v", err)
	}

	svc := service.New(service.Deps{Store: st, Media: newTestMedia(t)})
	view, err := svc.ReleaseGet(ctx, service.ReleaseGetRequest{ReleaseID: r.ID})
	if err != nil {
		t.Fatalf("ReleaseGet: %v", err)
	}
	if view.Status != "rejected" || view.Moderation == nil || view.Moderation.DecidedBy != "Kostya (@kteresh)" ||
		view.Moderation.Note != "the cover shows the wrong person" || view.Moderation.DecidedAt == nil {
		t.Fatalf("release_get moderation = %+v (status %s)", view.Moderation, view.Status)
	}
}
