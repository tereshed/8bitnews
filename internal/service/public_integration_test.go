package service_test

import (
	"context"
	"errors"
	"testing"

	"github.com/tereshed/8bitnews/internal/publish"
	"github.com/tereshed/8bitnews/internal/store"
)

func TestOnlyPublishedReleasesArePublic(t *testing.T) {
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
	if _, err := st.PublicRelease(ctx, code); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("an approved release, not yet up, is public: %v", err)
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
	if _, err := st.PublicRelease(ctx, code); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("a release taken back is still public: %v", err)
	}
	if _, _, err := st.SetPublished(ctx, r.ID, true); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"", code[:9], code + "0", "zzzzzzzzzz", "ffffffffff"} {
		if _, err := st.PublicRelease(ctx, bad); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("PublicRelease(%q): %v; want ErrNotFound", bad, err)
		}
	}
}
