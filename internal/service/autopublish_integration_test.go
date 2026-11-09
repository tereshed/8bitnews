package service_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/tereshed/8bitnews/internal/service"
	"github.com/tereshed/8bitnews/internal/store"
)

// approve carries a seeded release through the card to an approval by `by`.
func approve(t *testing.T, st *store.Store, id, by string) {
	t.Helper()
	ctx := context.Background()
	if _, ok, err := st.ClaimReleaseForCard(ctx, time.Hour); err != nil || !ok {
		t.Fatalf("claim: %v, %v", ok, err)
	}
	if err := st.SetCardMessage(ctx, id, 1, 2); err != nil {
		t.Fatal(err)
	}
	if _, changed, err := st.DecideRelease(ctx, id, "approved", by); err != nil || !changed {
		t.Fatalf("decide: %v, %v", changed, err)
	}
}

func queueHas(t *testing.T, st *store.Store, id string) bool {
	t.Helper()
	q, err := st.PublishQueue(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range q {
		if r.ID == id {
			return true
		}
	}
	return false
}

func TestPublishQueueTakesOnlyAutomaticApprovalsWithNoLink(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	auto := seedRelease(t, st, "q-auto")
	approve(t, st, auto.ID, store.AutoApprover)
	if !queueHas(t, st, auto.ID) {
		t.Fatal("an automatically approved release isn't in the queue")
	}

	// A person's approval may already be up without its link: never upload it again.
	byHand := seedRelease(t, st, "q-hand")
	approve(t, st, byHand.ID, "Kostya")
	if queueHas(t, st, byHand.ID) {
		t.Error("a release approved by hand is in the queue")
	}

	if err := st.SetPublishHold(ctx, auto.ID, "Legal's stop note came true"); err != nil {
		t.Fatal(err)
	}
	if queueHas(t, st, auto.ID) {
		t.Error("a held release is in the queue")
	}
	if err := st.SetPublishHold(ctx, auto.ID, ""); err != nil {
		t.Fatal(err)
	}
	if !queueHas(t, st, auto.ID) {
		t.Error("a release let back in isn't in the queue")
	}

	if err := st.SetPublicationURL(ctx, auto.ID, store.PlatformYouTube, "https://www.youtube.com/shorts/abcdefghijk", "api"); err != nil {
		t.Fatal(err)
	}
	if queueHas(t, st, auto.ID) {
		t.Error("an uploaded release is still in the queue")
	}
	if err := st.SetPublishHold(ctx, "00000000-0000-0000-0000-000000000000", "x"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("holding a release that doesn't exist: %v", err)
	}
}

func TestReserveSlotKeepsATimeAndNeverSharesOne(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	a := seedRelease(t, st, "slot-a")
	if _, ok, err := st.ClaimReleaseForCard(ctx, time.Hour); err != nil || !ok {
		t.Fatalf("claim: %v, %v", ok, err)
	}
	now := time.Now().Truncate(time.Second)
	first, second := now.Add(2*time.Hour), now.Add(4*time.Hour)
	pick := func(taken map[int64]bool) (time.Time, error) {
		if !taken[first.Unix()] {
			return first, nil
		}
		return second, nil
	}
	got, err := st.ReserveSlot(ctx, a.ID, a.ChannelID, now, pick)
	if err != nil || !got.Equal(first) {
		t.Fatalf("first reservation = %v, %v", got, err)
	}
	if again, err := st.ReserveSlot(ctx, a.ID, a.ChannelID, now, pick); err != nil || !again.Equal(first) {
		t.Errorf("asking again = %v, %v; want the same time", again, err)
	}

	b := seedRelease(t, st, a.ChannelID).ID
	if got, err := st.ReserveSlot(ctx, b, a.ChannelID, now, pick); err != nil || !got.Equal(second) {
		t.Errorf("the next release = %v, %v; want the next time", got, err)
	}
	if err := st.SetSlot(ctx, b, a.ChannelID, first); !errors.Is(err, store.ErrSlotTaken) {
		t.Errorf("sharing a time: %v, want ErrSlotTaken", err)
	}

	// A slot about to pass is given up for a later one, and freed.
	later := now.Add(3 * time.Hour)
	if got, err := st.ReserveSlot(ctx, a.ID, a.ChannelID, later, func(taken map[int64]bool) (time.Time, error) {
		if taken[first.Unix()] {
			t.Error("a time before notBefore counted as taken")
		}
		return now.Add(6 * time.Hour), nil
	}); err != nil || !got.Equal(now.Add(6*time.Hour)) {
		t.Errorf("re-reserving = %v, %v", got, err)
	}
	if err := st.SetSlot(ctx, b, a.ChannelID, first); err != nil {
		t.Errorf("the freed time: %v", err)
	}
}

func TestPublicationsToCheckWaitForTheSlotAndStopOnceSettled(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	r := seedRelease(t, st, "check-a")
	approve(t, st, r.ID, store.AutoApprover)
	url := "https://www.youtube.com/shorts/abcdefghijk"
	if err := st.SetPublicationURL(ctx, r.ID, store.PlatformYouTube, url, "api"); err != nil {
		t.Fatal(err)
	}
	if err := st.SetSlot(ctx, r.ID, r.ChannelID, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	has := func() (store.PublicationCheck, bool) {
		t.Helper()
		list, err := st.PublicationsToCheck(ctx)
		if err != nil {
			t.Fatal(err)
		}
		for _, c := range list {
			if c.ReleaseID == r.ID {
				return c, true
			}
		}
		return store.PublicationCheck{}, false
	}
	if _, ok := has(); ok {
		t.Error("checked before its slot")
	}
	if err := st.SetSlot(ctx, r.ID, r.ChannelID, time.Now().Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	c, ok := has()
	if !ok || c.URL != url || c.PublishAt == nil || c.OverdueAlerted || c.CardMessageID == nil {
		t.Fatalf("after its slot: %+v, %v", c, ok)
	}
	if err := st.MarkPublicationOverdue(ctx, r.ID, store.PlatformYouTube); err != nil {
		t.Fatal(err)
	}
	if c, ok := has(); !ok || !c.OverdueAlerted {
		t.Errorf("overdue not recorded: %+v, %v", c, ok)
	}
	if _, changed, err := st.SetPublished(ctx, r.ID, true); err != nil || !changed {
		t.Fatalf("publish: %v, %v", changed, err)
	}
	// The moderator's «↩️ Не выложено» stays: hub must not put it back.
	if _, changed, err := st.SetPublished(ctx, r.ID, false); err != nil || !changed {
		t.Fatalf("unpublish: %v, %v", changed, err)
	}
	if _, ok := has(); ok {
		t.Error("checked again after a moderator took it back")
	}
	// A new link is a new question.
	if err := st.SetPublicationURL(ctx, r.ID, store.PlatformYouTube, "https://www.youtube.com/shorts/ZZZZZZZZZZZ", "api"); err != nil {
		t.Fatal(err)
	}
	if c, ok := has(); !ok || c.OverdueAlerted {
		t.Errorf("a new link isn't checked afresh: %+v, %v", c, ok)
	}
}

func TestReleasesToUnscheduleAreClosedReleasesBeforeTheirSlot(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	r := seedRelease(t, st, "unsched-a")
	approve(t, st, r.ID, store.AutoApprover)
	if err := st.SetPublicationURL(ctx, r.ID, store.PlatformYouTube, "https://www.youtube.com/shorts/abcdefghijk", "api"); err != nil {
		t.Fatal(err)
	}
	if err := st.SetSlot(ctx, r.ID, r.ChannelID, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	has := func() bool {
		t.Helper()
		list, err := st.ReleasesToUnschedule(ctx)
		if err != nil {
			t.Fatal(err)
		}
		for _, u := range list {
			if u.ReleaseID == r.ID {
				return true
			}
		}
		return false
	}
	if has() {
		t.Error("an open release listed")
	}
	if _, changed, err := st.WithdrawRelease(ctx, r.ID, "Kostya"); err != nil || !changed {
		t.Fatalf("withdraw: %v, %v", changed, err)
	}
	if !has() {
		t.Error("a withdrawn release standing in the schedule isn't listed")
	}
	if err := st.ForgetPublication(ctx, r.ID, store.PlatformYouTube); err != nil {
		t.Fatal(err)
	}
	if has() {
		t.Error("still listed after coming off the schedule")
	}
	if url, err := st.PublicationURL(ctx, r.ID, store.PlatformYouTube); err != nil || url != "" {
		t.Errorf("the link stayed: %q, %v", url, err)
	}
}

func TestHeldVideoInTheScheduleIsToBeTakenDown(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	r := seedRelease(t, st, "held-a")
	approve(t, st, r.ID, store.AutoApprover)
	if err := st.SetPublicationURL(ctx, r.ID, store.PlatformYouTube, "https://www.youtube.com/shorts/abcdefghijk", "api"); err != nil {
		t.Fatal(err)
	}
	if err := st.SetSlot(ctx, r.ID, r.ChannelID, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := st.SetPublishHold(ctx, r.ID, "the vote is out"); err != nil {
		t.Fatal(err)
	}
	list, err := st.ReleasesToUnschedule(ctx)
	if err != nil || len(list) != 1 || list[0].ReleaseID != r.ID || list[0].Hold != "the vote is out" {
		t.Fatalf("to unschedule = %+v, %v", list, err)
	}
}

// A slot with no link is the only trace of an upload that crashed before
// recording itself: closing the release mustn't lose it.
func TestAClosedReleaseKeepsItsSlotUntilItIsTakenOff(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	now := time.Now().Truncate(time.Second)
	slot := now.Add(2 * time.Hour)
	first := func(taken map[int64]bool) (time.Time, error) {
		if taken[slot.Unix()] {
			return slot.Add(time.Hour), nil
		}
		return slot, nil
	}
	a := seedRelease(t, st, "keepslot")
	approve(t, st, a.ID, store.AutoApprover)
	if got, err := st.ReserveSlot(ctx, a.ID, a.ChannelID, now, first); err != nil || !got.Equal(slot) {
		t.Fatalf("reserving: %v, %v", got, err)
	}
	if _, _, err := st.WithdrawRelease(ctx, a.ID, "Kostya"); err != nil {
		t.Fatal(err)
	}
	list, err := st.ReleasesToUnschedule(ctx)
	if err != nil || len(list) != 1 || list[0].ReleaseID != a.ID || list[0].URL != "" {
		t.Fatalf("a closed release with a slot and no link: %+v, %v", list, err)
	}
	b := seedRelease(t, st, "keepslot")
	approve(t, st, b.ID, store.AutoApprover)
	if got, err := st.ReserveSlot(ctx, b.ID, b.ChannelID, now, first); err != nil || !got.Equal(slot.Add(time.Hour)) {
		t.Errorf("took the time of a video that may be scheduled: %v, %v", got, err)
	}
	// Looked in Studio, nothing there: the time comes back.
	if err := st.ForgetPublication(ctx, a.ID, store.PlatformYouTube); err != nil {
		t.Fatal(err)
	}
	if list, err := st.ReleasesToUnschedule(ctx); err != nil || len(list) != 0 {
		t.Errorf("still listed: %+v, %v", list, err)
	}
	c := seedRelease(t, st, "keepslot")
	approve(t, st, c.ID, store.AutoApprover)
	if got, err := st.ReserveSlot(ctx, c.ID, c.ChannelID, now, first); err != nil || !got.Equal(slot) {
		t.Errorf("the freed time: %v, %v", got, err)
	}
}

// Told that it hadn't come out, then closed, then it comes out: the group hears that too.
func TestAClosedVideoIsWatchedEvenAfterAnOverdueAlertOrAPress(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	r := seedRelease(t, st, "closedwatch")
	approve(t, st, r.ID, store.AutoApprover)
	if err := st.SetPublicationURL(ctx, r.ID, store.PlatformYouTube, "https://www.youtube.com/shorts/abcdefghijk", "api"); err != nil {
		t.Fatal(err)
	}
	if err := st.SetSlot(ctx, r.ID, r.ChannelID, time.Now().Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := st.MarkPublicationOverdue(ctx, r.ID, store.PlatformYouTube); err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.SetPublished(ctx, r.ID, true); err != nil { // settles live_at
		t.Fatal(err)
	}
	if _, _, err := st.WithdrawRelease(ctx, r.ID, "Kostya"); err != nil {
		t.Fatal(err)
	}
	checks, err := st.PublicationsToCheck(ctx)
	if err != nil || len(checks) != 1 || !checks[0].OverdueAlerted || checks[0].ClosedAlerted {
		t.Fatalf("not watched: %+v, %v", checks, err)
	}
	if err := st.MarkPublicationClosedAlerted(ctx, r.ID, store.PlatformYouTube); err != nil {
		t.Fatal(err)
	}
	if checks, err := st.PublicationsToCheck(ctx); err != nil || len(checks) != 0 {
		t.Errorf("watched after its alert: %+v, %v", checks, err)
	}
}

// A release closed before the slots, as old ones were, raises no alarm on deploy.
func TestAnOldClosedVideoRaisesNoAlarm(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	r := seedRelease(t, st, "oldclosed")
	approve(t, st, r.ID, "Kostya")
	if err := st.SetPublicationURL(ctx, r.ID, store.PlatformYouTube, "https://www.youtube.com/shorts/abcdefghijk", "x"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.WithdrawRelease(ctx, r.ID, "Kostya"); err != nil {
		t.Fatal(err)
	}
	if checks, err := st.PublicationsToCheck(ctx); err != nil || len(checks) != 0 {
		t.Errorf("an old closed video is watched: %+v, %v", checks, err)
	}
	if list, err := st.ReleasesToUnschedule(ctx); err != nil || len(list) != 0 {
		t.Errorf("an old closed video is to be taken down: %+v, %v", list, err)
	}
}

func TestStalledUploadsAndClosedVideosAfterTheirSlot(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	r := seedRelease(t, st, "stall-a")
	approve(t, st, r.ID, store.AutoApprover)
	stalled, err := st.UploadAlerts(ctx, time.Now().Add(time.Minute))
	if err != nil || len(stalled) != 1 || stalled[0].ReleaseID != r.ID {
		t.Fatalf("stalled = %+v, %v", stalled, err)
	}
	if none, err := st.UploadAlerts(ctx, time.Now().Add(-time.Hour)); err != nil || len(none) != 0 {
		t.Errorf("a fresh approval counted as stalled: %+v, %v", none, err)
	}
	if err := st.MarkUploadAlerted(ctx, r.ID); err != nil {
		t.Fatal(err)
	}
	if again, err := st.UploadAlerts(ctx, time.Now().Add(time.Minute)); err != nil || len(again) != 0 {
		t.Errorf("told twice: %+v, %v", again, err)
	}

	// A closed release's video is watched after its slot, and once alerted, no more.
	if err := st.SetPublicationURL(ctx, r.ID, store.PlatformYouTube, "https://www.youtube.com/shorts/abcdefghijk", "api"); err != nil {
		t.Fatal(err)
	}
	if err := st.SetSlot(ctx, r.ID, r.ChannelID, time.Now().Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.WithdrawRelease(ctx, r.ID, "Kostya"); err != nil {
		t.Fatal(err)
	}
	checks, err := st.PublicationsToCheck(ctx)
	if err != nil || len(checks) != 1 || checks[0].Status != "rejected" {
		t.Fatalf("closed video not watched: %+v, %v", checks, err)
	}
	if err := st.MarkPublicationClosedAlerted(ctx, r.ID, store.PlatformYouTube); err != nil {
		t.Fatal(err)
	}
	if checks, err := st.PublicationsToCheck(ctx); err != nil || len(checks) != 0 {
		t.Errorf("watched after the alert: %+v, %v", checks, err)
	}
}

func publishingService(t *testing.T, st *store.Store, channelID string, slots string) *service.Service {
	t.Helper()
	ctx := context.Background()
	if err := st.UpsertChannel(ctx, store.Channel{
		ID: channelID, CountryCC: "zz", Name: "Test Channel", Language: "en-US", Timezone: "UTC",
		Platforms: []byte(`{"youtube":{"enabled":true}}`), Format: []byte(`{}`), FormatMD: "# Test format",
		Data: []byte(`{"schedule":{"publish_slots":` + slots + `}}`),
	}); err != nil {
		t.Fatal(err)
	}
	return service.New(service.Deps{Store: st, Media: newTestMedia(t)})
}

func isKind(err error, kind service.Kind) bool {
	var e *service.Error
	return errors.As(err, &e) && e.Kind == kind
}

func TestSetYouTubePublicationRules(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	a := seedRelease(t, st, "pub-rules")
	svc := publishingService(t, st, "pub-rules", `["18:00"]`)
	soon := time.Now().Add(time.Hour).Truncate(time.Second)

	if err := svc.SetYouTubePublication(ctx, a.ID, service.YouTubePublicationRequest{VideoID: "abcdefghijk"}); !isKind(err, service.KindConflict) {
		t.Errorf("before any decision: %v", err)
	}
	approve(t, st, a.ID, store.AutoApprover)
	if err := svc.SetYouTubePublication(ctx, a.ID, service.YouTubePublicationRequest{VideoID: "too-short"}); !isKind(err, service.KindInvalid) {
		t.Errorf("a bad id: %v", err)
	}
	past := time.Now().Add(-time.Hour)
	if err := svc.SetYouTubePublication(ctx, a.ID, service.YouTubePublicationRequest{VideoID: "abcdefghijk", PublishAt: &past}); !isKind(err, service.KindInvalid) {
		t.Errorf("a time gone by: %v", err)
	}
	if err := svc.SetYouTubePublication(ctx, a.ID, service.YouTubePublicationRequest{VideoID: "abcdefghijk", PublishAt: &soon}); err != nil {
		t.Fatalf("a good one: %v", err)
	}

	b := seedRelease(t, st, "pub-rules")
	publishingService(t, st, "pub-rules", `["18:00"]`)
	approve(t, st, b.ID, store.AutoApprover)
	if err := svc.SetYouTubePublication(ctx, b.ID, service.YouTubePublicationRequest{VideoID: "abcdefghijk"}); !isKind(err, service.KindConflict) {
		t.Errorf("one video on two releases: %v", err)
	}
	if err := svc.SetYouTubePublication(ctx, b.ID, service.YouTubePublicationRequest{VideoID: "bbbbbbbbbbb", PublishAt: &soon}); !isKind(err, service.KindConflict) {
		t.Errorf("one time on two releases: %v", err)
	}

	// Closed while the upload was under way: recorded, so it can be taken down.
	c := seedRelease(t, st, "pub-rules")
	publishingService(t, st, "pub-rules", `["18:00"]`)
	approve(t, st, c.ID, store.AutoApprover)
	if _, _, err := st.WithdrawRelease(ctx, c.ID, "Kostya"); err != nil {
		t.Fatal(err)
	}
	later := soon.Add(time.Hour)
	if err := svc.SetYouTubePublication(ctx, c.ID, service.YouTubePublicationRequest{VideoID: "ccccccccccc", PublishAt: &later}); err != nil {
		t.Fatalf("closed mid-upload: %v", err)
	}
	queue, err := svc.PublishQueue(ctx)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, u := range queue.Unschedule {
		found = found || u.ReleaseID == c.ID
	}
	if !found {
		t.Errorf("a video closed mid-upload isn't to be taken down: %+v", queue.Unschedule)
	}
}

func TestSetYouTubePublicationRespectsLegal(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	seedChannel(t, st, "pub-barred")
	story, err := st.CreateStory(ctx, "pub-barred", "A story", "n")
	if err != nil {
		t.Fatal(err)
	}
	dev, err := st.CreateDevelopment(ctx, story.ID, "A turn", time.Now(), json.RawMessage(`[]`), "")
	if err != nil {
		t.Fatal(err)
	}
	r, err := st.CreateRelease(ctx, store.NewReleaseParams{DevelopmentID: dev.ID, ChannelID: "pub-barred", ArmID: "default",
		Media: []byte(`{"video":"releases/x/video.mp4"}`), AllowedPlatforms: []string{"tiktok"}})
	if err != nil {
		t.Fatal(err)
	}
	approve(t, st, r.ID, store.AutoApprover)
	if queueHas(t, st, r.ID) {
		t.Error("a release barred from YouTube is in the queue")
	}
	svc := publishingService(t, st, "pub-barred", `["18:00"]`)
	if err := svc.SetYouTubePublication(ctx, r.ID, service.YouTubePublicationRequest{VideoID: "abcdefghijk"}); !isKind(err, service.KindConflict) {
		t.Errorf("a link on a barred release: %v", err)
	}
}

func TestReserveReleaseSlotRefusals(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	r := seedRelease(t, st, "slot-rules")
	svc := publishingService(t, st, "slot-rules", `["18:00", "12:00"]`)

	if _, err := svc.ReserveReleaseSlot(ctx, r.ID); !isKind(err, service.KindConflict) {
		t.Errorf("before approval: %v", err)
	}
	approve(t, st, r.ID, store.AutoApprover)
	if err := svc.HoldRelease(ctx, r.ID, "  "); !isKind(err, service.KindInvalid) {
		t.Errorf("a hold with no reason: %v", err)
	}
	if err := svc.HoldRelease(ctx, r.ID, "the vote is out"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ReserveReleaseSlot(ctx, r.ID); !isKind(err, service.KindConflict) {
		t.Errorf("a held release: %v", err)
	}
	if err := svc.ReleaseHold(ctx, r.ID); err != nil {
		t.Fatal(err)
	}
	got, err := svc.ReserveReleaseSlot(ctx, r.ID)
	if err != nil {
		t.Fatalf("a good one: %v", err)
	}
	if got.PublishAt.Before(time.Now().Add(40*time.Minute)) || got.Local == "" || got.TimeZone != "UTC" {
		t.Errorf("slot = %+v", got)
	}

	// Older than the queue allows, with no slot in reach: no slot, and no booking.
	old := seedRelease(t, st, "slot-rules")
	publishingService(t, st, "slot-rules", `["18:00", "12:00"]`)
	approve(t, st, old.ID, store.AutoApprover)
	db, err := sql.Open("pgx", testDSN())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if _, err := db.ExecContext(ctx, `UPDATE releases SET created_at = now() - interval '10 days' WHERE id = $1`, old.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ReserveReleaseSlot(ctx, old.ID); !isKind(err, service.KindConflict) {
		t.Errorf("a stale release: %v", err)
	}
	if at, err := st.SlotOf(ctx, old.ID); err != nil || at != nil {
		t.Errorf("a stale release holds %v, %v", at, err)
	}
	// Out of the queue, and the group hears why.
	if queueHas(t, st, old.ID) {
		t.Error("a stale release is still in the queue")
	}
	alerts, err := st.UploadAlerts(ctx, time.Now().Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	told := false
	for _, a := range alerts {
		told = told || (a.ReleaseID == old.ID && strings.Contains(a.Hold, "too old"))
	}
	if !told {
		t.Errorf("the group won't hear about a stale release: %+v", alerts)
	}
}

func TestPublishingMigrationKeepsAModeratorsTakeBack(t *testing.T) {
	// SetPublished is what settles the question from now on.
	st := newTestStore(t)
	ctx := context.Background()
	r := seedRelease(t, st, "takeback")
	approve(t, st, r.ID, "Kostya")
	if err := st.SetPublicationURL(ctx, r.ID, store.PlatformYouTube, "https://www.youtube.com/shorts/abcdefghijk", "x"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.SetPublished(ctx, r.ID, true); err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.SetPublished(ctx, r.ID, false); err != nil {
		t.Fatal(err)
	}
	checks, err := st.PublicationsToCheck(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range checks {
		if c.ReleaseID == r.ID {
			t.Errorf("hub would publish again what a moderator took back: %+v", c)
		}
	}
}
