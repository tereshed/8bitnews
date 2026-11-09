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

	// Told it stalled, then held: the hold is news of its own, and so is a new reason.
	if err := st.SetPublishHold(ctx, r.ID, "too old"); err != nil {
		t.Fatal(err)
	}
	held, err := st.UploadAlerts(ctx, time.Now().Add(time.Minute))
	if err != nil || len(held) != 1 || held[0].Hold != "too old" || held[0].MayBeScheduled {
		t.Fatalf("a hold after a stall alert: %+v, %v", held, err)
	}
	if err := st.MarkHoldAlerted(ctx, r.ID); err != nil {
		t.Fatal(err)
	}
	if err := st.SetPublishHold(ctx, r.ID, "too old"); err != nil {
		t.Fatal(err)
	}
	if again, err := st.UploadAlerts(ctx, time.Now().Add(time.Minute)); err != nil || len(again) != 0 {
		t.Errorf("the same reason, told again: %+v, %v", again, err)
	}
	if err := st.SetPublishHold(ctx, r.ID, "the vote is out"); err != nil {
		t.Fatal(err)
	}
	if err := st.SetSlot(ctx, r.ID, r.ChannelID, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	held, err = st.UploadAlerts(ctx, time.Now().Add(time.Minute))
	if err != nil || len(held) != 1 || held[0].Hold != "the vote is out" || !held[0].MayBeScheduled {
		t.Errorf("a new reason, with a slot booked: %+v, %v", held, err)
	}
	if err := st.SetPublishHold(ctx, r.ID, ""); err != nil {
		t.Fatal(err)
	}
	if lifted, err := st.UploadAlerts(ctx, time.Now().Add(time.Minute)); err != nil || len(lifted) != 0 {
		t.Errorf("a lifted hold, already told it stalled: %+v, %v", lifted, err)
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

func TestConnectedChannelsLeaveTheQueueForTheUploader(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	r := seedRelease(t, st, "conn-a")
	approve(t, st, r.ID, store.AutoApprover)
	on := service.New(service.Deps{Store: st, Media: newTestMedia(t), UploaderOn: true})
	off := service.New(service.Deps{Store: st, Media: newTestMedia(t)})
	listed := func(svc *service.Service) bool {
		t.Helper()
		q, err := svc.PublishQueue(ctx)
		if err != nil {
			t.Fatal(err)
		}
		return len(q.Upload) == 1 && q.Upload[0].ReleaseID == r.ID
	}
	if !listed(on) {
		t.Error("a channel not connected isn't in the queue")
	}
	sealed := "sealed-not-the-token"
	if err := st.SetPlatformAccount(ctx, store.PlatformAccount{ChannelID: r.ChannelID, Platform: store.PlatformYouTube, ExternalID: "UC1",
		Tokens: store.AccountTokens{RefreshSealed: sealed, Title: "T", ConnectedBy: "Kostya", ConnectedAt: time.Now()}}); err != nil {
		t.Fatal(err)
	}
	if listed(on) {
		t.Error("a connected channel is still in the browser queue")
	}
	if !listed(off) {
		t.Error("with the uploader off, the queue must keep it")
	}
	items, err := on.UploadItems(ctx)
	if err != nil || len(items) != 1 || items[0].ReleaseID != r.ID {
		t.Errorf("the uploader's own list: %+v, %v", items, err)
	}

	// Taking down follows the same line.
	if err := st.SetSlot(ctx, r.ID, r.ChannelID, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.WithdrawRelease(ctx, r.ID, "Kostya"); err != nil {
		t.Fatal(err)
	}
	if q, err := on.PublishQueue(ctx); err != nil || len(q.Unschedule) != 0 {
		t.Errorf("a connected channel's take-down in the browser queue: %+v, %v", q.Unschedule, err)
	}

	// A lost access hands the channel back.
	if err := st.MarkAccountAlerted(ctx, r.ChannelID, store.PlatformYouTube); err != nil {
		t.Fatal(err)
	}
	if q, err := on.PublishQueue(ctx); err != nil || len(q.Unschedule) != 1 {
		t.Errorf("after a lost access: %+v, %v", q.Unschedule, err)
	}
	if accounts, err := st.ConnectedAccounts(ctx, store.PlatformYouTube); err != nil || len(accounts) != 0 {
		t.Errorf("a lost account still counts as connected: %+v, %v", accounts, err)
	}
	// Connecting again clears it.
	if err := st.SetPlatformAccount(ctx, store.PlatformAccount{ChannelID: r.ChannelID, Platform: store.PlatformYouTube, ExternalID: "UC1",
		Tokens: store.AccountTokens{RefreshSealed: sealed}}); err != nil {
		t.Fatal(err)
	}
	accounts, err := st.ConnectedAccounts(ctx, store.PlatformYouTube)
	if err != nil || len(accounts) != 1 || accounts[0].Tokens.RefreshSealed != sealed || accounts[0].ExternalID != "UC1" {
		t.Errorf("after reconnecting: %+v, %v", accounts, err)
	}
	if err := st.DeletePlatformAccount(ctx, r.ChannelID, store.PlatformYouTube); err != nil {
		t.Fatal(err)
	}
	if _, err := st.PlatformAccountOf(ctx, r.ChannelID, store.PlatformYouTube); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("after disconnecting: %v", err)
	}
}

func TestPublishJobsKeepHowFarAnUploadGot(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	r := seedRelease(t, st, "job-a")
	approve(t, st, r.ID, store.AutoApprover)
	blank, err := st.PublishJobOf(ctx, r.ID)
	if err != nil || blank.ReleaseID != r.ID || blank.SessionURI != "" || !blank.NextAttemptAt.IsZero() {
		t.Fatalf("no job yet: %+v, %v", blank, err)
	}
	at := time.Now().Add(time.Hour).Truncate(time.Second)
	job := store.PublishJob{ReleaseID: r.ID, SessionURI: "https://x/session", VideoSize: 123, PublishAt: &at, NextAttemptAt: at, Attempts: 2, LastError: "boom"}
	if err := st.SavePublishJob(ctx, job); err != nil {
		t.Fatal(err)
	}
	job.VideoID, job.ThumbnailSet, job.ThumbnailAttempts = "vid", true, 1
	if err := st.SavePublishJob(ctx, job); err != nil {
		t.Fatal(err)
	}
	got, err := st.PublishJobOf(ctx, r.ID)
	if err != nil || got.SessionURI != "https://x/session" || got.VideoSize != 123 || got.PublishAt == nil || !got.PublishAt.Equal(at) ||
		got.VideoID != "vid" || !got.ThumbnailSet || got.ThumbnailAttempts != 1 || got.Attempts != 2 || got.LastError != "boom" {
		t.Errorf("job: %+v, %v", got, err)
	}
	// Forgetting a publication starts the next upload afresh.
	if err := st.ForgetPublication(ctx, r.ID, store.PlatformYouTube); err != nil {
		t.Fatal(err)
	}
	if got, err := st.PublishJobOf(ctx, r.ID); err != nil || got.VideoID != "" {
		t.Errorf("after forgetting: %+v, %v", got, err)
	}
}

func TestOnlyOneProcessHoldsThePublishersLock(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	first, ok, err := st.LockPublisher(ctx)
	if err != nil || !ok {
		t.Fatalf("the first: %v, %v", ok, err)
	}
	if !first.Alive(ctx) {
		t.Error("a held lock isn't alive")
	}
	if _, ok, err := st.LockPublisher(ctx); err != nil || ok {
		t.Fatalf("the second got it too: %v, %v", ok, err)
	}
	first.Release(ctx)
	second, ok, err := st.LockPublisher(ctx)
	if err != nil || !ok {
		t.Fatalf("after the release: %v, %v", ok, err)
	}
	second.Release(ctx)
	if first.Alive(ctx) {
		t.Error("a released lock is alive")
	}
}

func TestUnannouncedJobsAreOfApprovedUnheldReleases(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	r := seedRelease(t, st, "unann-a")
	approve(t, st, r.ID, store.AutoApprover)
	listed := func() []store.PublishJob {
		t.Helper()
		jobs, err := st.UnannouncedJobs(ctx)
		if err != nil {
			t.Fatal(err)
		}
		return jobs
	}
	job := store.PublishJob{ReleaseID: r.ID, SessionURI: "https://x/s", VideoSize: 5}
	if err := st.SavePublishJob(ctx, job); err != nil {
		t.Fatal(err)
	}
	if len(listed()) != 0 {
		t.Error("no video yet, yet listed")
	}
	job.VideoID = "vid"
	if err := st.SavePublishJob(ctx, job); err != nil {
		t.Fatal(err)
	}
	if got := listed(); len(got) != 1 || got[0].ChannelID != "unann-a" || got[0].VideoID != "vid" || !got[0].Saved || got[0].AnnouncedAt != nil {
		t.Fatalf("listed: %+v", got)
	}
	if err := st.SetPublishHold(ctx, r.ID, "x"); err != nil {
		t.Fatal(err)
	}
	if len(listed()) != 0 {
		t.Error("a held release is listed")
	}
	if err := st.SetPublishHold(ctx, r.ID, ""); err != nil {
		t.Fatal(err)
	}
	at := time.Now()
	job.AnnouncedAt = &at
	if err := st.SavePublishJob(ctx, job); err != nil {
		t.Fatal(err)
	}
	if len(listed()) != 0 {
		t.Error("an announced job is listed")
	}
	got, err := st.PublishJobOf(ctx, r.ID)
	if err != nil || got.AnnouncedAt == nil || !got.Saved {
		t.Errorf("read back: %+v, %v", got, err)
	}
	blank, err := st.PublishJobOf(ctx, seedRelease(t, st, "unann-a").ID)
	if err != nil || blank.Saved {
		t.Errorf("no row: %+v, %v", blank, err)
	}
}

func TestHubsLinkDoesNotReplaceAModerators(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	r := seedRelease(t, st, "ifnone-a")
	approve(t, st, r.ID, store.AutoApprover)
	if err := st.SetPublicationURLIfNone(ctx, r.ID, store.PlatformYouTube, "https://www.youtube.com/shorts/hubs0000001", "hub"); err != nil {
		t.Fatal(err)
	}
	if url, err := st.PublicationURL(ctx, r.ID, store.PlatformYouTube); err != nil || url != "https://www.youtube.com/shorts/hubs0000001" {
		t.Errorf("first: %q, %v", url, err)
	}
	if err := st.SetPublicationURL(ctx, r.ID, store.PlatformYouTube, "https://www.youtube.com/shorts/moderators1", "Kostya"); err != nil {
		t.Fatal(err)
	}
	if err := st.SetPublicationURLIfNone(ctx, r.ID, store.PlatformYouTube, "https://www.youtube.com/shorts/hubs0000001", "hub"); err != nil {
		t.Fatal(err)
	}
	if url, err := st.PublicationURL(ctx, r.ID, store.PlatformYouTube); err != nil || url != "https://www.youtube.com/shorts/moderators1" {
		t.Errorf("after a moderator: %q, %v", url, err)
	}
}

func TestSlotsToRecheckAreNearAndReadOnce(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	r := seedRelease(t, st, "rechk-a")
	approve(t, st, r.ID, store.AutoApprover)
	if err := st.SetPublicationURL(ctx, r.ID, store.PlatformYouTube, "https://www.youtube.com/shorts/abcdefghijk", "hub"); err != nil {
		t.Fatal(err)
	}
	slot := time.Now().Add(20 * time.Minute).Truncate(time.Second)
	if err := st.SetSlot(ctx, r.ID, r.ChannelID, slot); err != nil {
		t.Fatal(err)
	}
	due := func() int {
		t.Helper()
		list, err := st.SlotsToRecheck(ctx, time.Now().Add(30*time.Minute))
		if err != nil {
			t.Fatal(err)
		}
		return len(list)
	}
	if due() != 1 {
		t.Fatal("a slot 20 minutes off isn't due")
	}
	if list, _ := st.SlotsToRecheck(ctx, time.Now().Add(10*time.Minute)); len(list) != 0 {
		t.Error("a slot further off than asked is due")
	}
	if err := st.MarkSlotRechecked(ctx, r.ID); err != nil {
		t.Fatal(err)
	}
	if due() != 0 {
		t.Error("read twice")
	}
	// The same time again keeps the mark; a new time is a new question.
	if err := st.SetSlot(ctx, r.ID, r.ChannelID, slot); err != nil {
		t.Fatal(err)
	}
	if due() != 0 {
		t.Error("the same slot lost its mark")
	}
	if err := st.SetSlot(ctx, r.ID, r.ChannelID, slot.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if due() != 1 {
		t.Error("a moved slot isn't due")
	}
	if err := st.SetPublishHold(ctx, r.ID, "x"); err != nil {
		t.Fatal(err)
	}
	if due() != 0 {
		t.Error("a held release is due")
	}
}

func TestAStopNoteTakesTheSoonestSlotAndBlackoutDatesAreSkipped(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	hour := func(d time.Duration) string { return time.Now().UTC().Add(d).Format("15:04") }
	// Two slots ahead, the later one listed first as the main one.
	soon, later := hour(2*time.Hour), hour(5*time.Hour)
	// Near midnight the two would swap days and prove nothing.
	if later < soon {
		t.Skip("the two slots fall on different days at this hour")
	}
	slots := `["` + later + `", "` + soon + `"]`

	plain := seedRelease(t, st, "stop-a")
	svc := publishingService(t, st, "stop-a", slots)
	approve(t, st, plain.ID, store.AutoApprover)
	got, err := svc.ReserveReleaseSlot(ctx, plain.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.PublishAt.UTC().Format("15:04") != later {
		t.Errorf("no stop note: %s, want the main slot %s", got.PublishAt.UTC().Format("15:04"), later)
	}
	if err := st.ForgetPublication(ctx, plain.ID, store.PlatformYouTube); err != nil {
		t.Fatal(err)
	}

	db, err := sql.Open("pgx", testDSN())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if _, err := db.ExecContext(ctx, `UPDATE releases SET legal_review = '{"publish_notes":["Set flags","СТОП: if the vote has happened, check X"]}' WHERE id = $1`, plain.ID); err != nil {
		t.Fatal(err)
	}
	got, err = svc.ReserveReleaseSlot(ctx, plain.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.PublishAt.UTC().Format("15:04") != soon {
		t.Errorf("a stop note: %s, want the soonest %s", got.PublishAt.UTC().Format("15:04"), soon)
	}

	// Today and tomorrow blacked out: the slot lands the day after.
	other := seedRelease(t, st, "stop-b")
	if err := st.UpsertChannel(ctx, store.Channel{ID: "stop-b", CountryCC: "zz", Name: "T", Language: "en-US", Timezone: "UTC",
		Platforms: []byte(`{}`), Format: []byte(`{}`), FormatMD: "# T",
		Data: []byte(`{"schedule":{"publish_slots":["` + soon + `"],"publish_blackout":["` + time.Now().UTC().Format(time.DateOnly) + `","` + time.Now().UTC().Add(24*time.Hour).Format(time.DateOnly) + `"]}}`)}); err != nil {
		t.Fatal(err)
	}
	approve(t, st, other.ID, store.AutoApprover)
	got, err = svc.ReserveReleaseSlot(ctx, other.ID)
	if err != nil {
		t.Fatal(err)
	}
	if want := time.Now().UTC().Add(48 * time.Hour).Format(time.DateOnly); got.PublishAt.UTC().Format(time.DateOnly) != want {
		t.Errorf("blackout: %s, want %s", got.PublishAt.UTC().Format(time.DateOnly), want)
	}
}

func TestForgettingChecksTheReleaseIsOutOfPlay(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	r := seedRelease(t, st, "oop-a")
	approve(t, st, r.ID, store.AutoApprover)
	link := "https://www.youtube.com/shorts/abcdefghijk"
	seedRecord := func() {
		t.Helper()
		if err := st.SetPublicationURL(ctx, r.ID, store.PlatformYouTube, link, "hub"); err != nil {
			t.Fatal(err)
		}
		if err := st.SetSlot(ctx, r.ID, r.ChannelID, time.Now().Add(time.Hour)); err != nil {
			t.Fatal(err)
		}
		if err := st.SavePublishJob(ctx, store.PublishJob{ReleaseID: r.ID, VideoID: "abcdefghijk"}); err != nil {
			t.Fatal(err)
		}
	}
	kept := func() bool {
		t.Helper()
		url, err := st.PublicationURL(ctx, r.ID, store.PlatformYouTube)
		if err != nil {
			t.Fatal(err)
		}
		slot, err := st.SlotOf(ctx, r.ID)
		if err != nil {
			t.Fatal(err)
		}
		job, err := st.PublishJobOf(ctx, r.ID)
		if err != nil {
			t.Fatal(err)
		}
		if (url != "") != (slot != nil) || (url != "") != job.Saved {
			t.Fatalf("half a record: link %q, slot %v, job %+v", url, slot, job)
		}
		return url != ""
	}
	seedRecord()
	// Approved and free: it's in play, nothing goes.
	if done, err := st.ForgetIfOutOfPlay(ctx, r.ID, store.PlatformYouTube); err != nil || done || !kept() {
		t.Errorf("in play: %v, %v, kept %v", done, err, kept())
	}
	// Held: it goes.
	if err := st.SetPublishHold(ctx, r.ID, "x"); err != nil {
		t.Fatal(err)
	}
	if done, err := st.ForgetIfOutOfPlay(ctx, r.ID, store.PlatformYouTube); err != nil || !done || kept() {
		t.Errorf("held: %v, %v, kept %v", done, err, kept())
	}
	if err := st.SetPublishHold(ctx, r.ID, ""); err != nil {
		t.Fatal(err)
	}

	// Hold and forget as one step.
	seedRecord()
	if err := st.HoldAndForget(ctx, r.ID, store.PlatformYouTube, "rejected"); err != nil {
		t.Fatal(err)
	}
	if hold, err := st.PublishHold(ctx, r.ID); err != nil || hold != "rejected" || kept() {
		t.Errorf("hold %q, %v, kept %v", hold, err, kept())
	}
	if err := st.HoldAndForget(ctx, "00000000-0000-0000-0000-000000000000", store.PlatformYouTube, "x"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("a release that doesn't exist: %v", err)
	}
	// Closed: it goes.
	if err := st.SetPublishHold(ctx, r.ID, ""); err != nil {
		t.Fatal(err)
	}
	seedRecord()
	if _, _, err := st.WithdrawRelease(ctx, r.ID, "Kostya"); err != nil {
		t.Fatal(err)
	}
	if done, err := st.ForgetIfOutOfPlay(ctx, r.ID, store.PlatformYouTube); err != nil || !done || kept() {
		t.Errorf("closed: %v, %v, kept %v", done, err, kept())
	}
	if _, err := st.ForgetIfOutOfPlay(ctx, "00000000-0000-0000-0000-000000000000", store.PlatformYouTube); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("a release that doesn't exist: %v", err)
	}
}
