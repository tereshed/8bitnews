package service_test

import (
	"context"
	"errors"
	"testing"

	"github.com/tereshed/8bitnews/internal/publish"
	"github.com/tereshed/8bitnews/internal/store"
)

func TestSourcesPageOpensOnApprovalAndTheListOnPublished(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	r := seedRelease(t, st, "pub-us")
	code := publish.Code(r.ID)

	if _, err := st.PublicRelease(ctx, code); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("a submitted release is public: %v", err)
	}
	if _, _, err := st.ClaimReleaseForCard(ctx, 0); err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.DecideRelease(ctx, r.ID, "approved", "Kostya"); err != nil {
		t.Fatal(err)
	}
	if got, err := st.PublicRelease(ctx, code); err != nil || got.ID != r.ID {
		t.Fatalf("an approved release has no sources page: %s, %v", got.ID, err)
	}
	if list, err := st.PublicReleases(ctx, "pub-us", 10); err != nil || len(list) != 0 {
		t.Fatalf("an approved release is listed: %v, %v", list, err)
	}
	published, changed, err := st.SetPublished(ctx, r.ID, true)
	if err != nil || !changed || published.Status != "published" {
		t.Fatalf("SetPublished = %s, %v, %v", published.Status, changed, err)
	}
	if again, changed, err := st.SetPublished(ctx, r.ID, true); err != nil || changed || again.Status != "published" {
		t.Fatalf("a second SetPublished = %s, %v, %v", again.Status, changed, err)
	}
	if _, _, err := st.SetPublished(ctx, "00000000-0000-0000-0000-000000000000", true); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("SetPublished of a missing release: %v", err)
	}
	got, err := st.PublicRelease(ctx, code)
	if err != nil || got.ID != r.ID {
		t.Fatalf("PublicRelease(%s) = %s, %v", code, got.ID, err)
	}
	list, err := st.PublicReleases(ctx, "pub-us", 10)
	if err != nil || len(list) != 1 || list[0].ID != r.ID || list[0].Moderation.At == nil || string(list[0].Script) != `{"metadata": null}` {
		t.Fatalf("PublicReleases = %+v, %v", list, err)
	}
	if list, err := st.PublicReleases(ctx, "other", 10); err != nil || len(list) != 0 {
		t.Fatalf("another channel's list = %v, %v", list, err)
	}
	if back, changed, err := st.SetPublished(ctx, r.ID, false); err != nil || !changed || back.Status != "approved" {
		t.Fatalf("taking «Выложено» back = %s, %v, %v", back.Status, changed, err)
	}
	if got, err := st.PublicRelease(ctx, code); err != nil || got.ID != r.ID {
		t.Fatalf("a release taken back lost its sources page: %s, %v", got.ID, err)
	}
	if list, err := st.PublicReleases(ctx, "pub-us", 10); err != nil || len(list) != 0 {
		t.Fatalf("a release taken back is still listed: %v, %v", list, err)
	}
	if _, _, err := st.SetPublished(ctx, r.ID, true); err != nil {
		t.Fatal(err)
	}
	if withdrawn, changed, err := st.WithdrawRelease(ctx, r.ID, "K"); err != nil || !changed || withdrawn.Status != "rejected" {
		t.Fatalf("WithdrawRelease = %s, %v, %v", withdrawn.Status, changed, err)
	}
	if again, changed, err := st.WithdrawRelease(ctx, r.ID, "K"); err != nil || changed || again.Status != "rejected" {
		t.Fatalf("a second WithdrawRelease = %s, %v, %v", again.Status, changed, err)
	}
	if _, err := st.PublicRelease(ctx, code); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("a withdrawn release has a sources page: %v", err)
	}
	if list, err := st.PublicReleases(ctx, "pub-us", 10); err != nil || len(list) != 0 {
		t.Fatalf("a withdrawn release is listed: %v, %v", list, err)
	}
	if _, _, err := st.WithdrawRelease(ctx, "00000000-0000-0000-0000-000000000000", "K"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("WithdrawRelease of a missing release: %v", err)
	}
	for _, bad := range []string{"", code[:9], code + "0", "zzzzzzzzzz", "ffffffffff"} {
		if _, err := st.PublicRelease(ctx, bad); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("PublicRelease(%q): %v; want ErrNotFound", bad, err)
		}
	}
}
