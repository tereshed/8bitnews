package service_test

import (
	"context"
	"errors"
	"testing"
	"time"

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
