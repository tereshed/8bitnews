package service_test

import (
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/tereshed/8bitnews/internal/profilesync"
	"github.com/tereshed/8bitnews/internal/service"
)

// TestChannelGet_AfterSyncProfiles exercises AC1's data path end to end:
// sync-profiles mirrors the real channels/8bitnews-us profile into
// Postgres, then channel_get must return it with language en-US and the
// text of format.md.
func TestChannelGet_AfterSyncProfiles(t *testing.T) {
	st := newTestStore(t)
	media := newTestMedia(t)

	if _, err := profilesync.Sync(context.Background(), st, "../.."); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	svc := service.New(service.Deps{Store: st, Media: media, Log: slog.Default()})
	view, err := svc.ChannelGet(context.Background(), service.ChannelGetRequest{ChannelID: "8bitnews-us"})
	if err != nil {
		t.Fatalf("ChannelGet: %v", err)
	}

	if view.Language != "en-US" {
		t.Errorf("Language = %q, want en-US", view.Language)
	}
	if !strings.Contains(view.FormatMD, "8BitNewsUS") {
		t.Errorf("FormatMD does not look like channels/8bitnews-us/format.md: %q", view.FormatMD)
	}
}

func TestChannelGet_NotFound(t *testing.T) {
	st := newTestStore(t)
	media := newTestMedia(t)

	svc := service.New(service.Deps{Store: st, Media: media, Log: slog.Default()})
	_, err := svc.ChannelGet(context.Background(), service.ChannelGetRequest{ChannelID: "does-not-exist"})
	if err == nil {
		t.Fatalf("expected an error for an unknown channel")
	}
}
