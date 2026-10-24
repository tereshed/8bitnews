package service_test

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/tereshed/8bitnews/internal/service"
)

// TestStoryRegisterAndFindSimilar reproduces task E1 AC2: registering a
// storyline then searching for it by an almost-identical title must find
// it with one development; registering a second development on the same
// story_id must make it show up with two.
func TestStoryRegisterAndFindSimilar(t *testing.T) {
	st := newTestStore(t)
	media := newTestMedia(t)
	seedChannel(t, st, "test-channel")

	svc := service.New(st, media, nil, nil, slog.Default())
	ctx := context.Background()

	first, err := svc.StoryRegister(ctx, service.StoryRegisterRequest{
		ChannelID:          "test-channel",
		StoryID:            nil,
		Title:              "ICE enforcement surge in Chicago",
		Summary:            "Federal agents have expanded operations across the Chicago metro area.",
		DevelopmentSummary: "A federal court issued a temporary restraining order limiting tactics.",
		ReportedAt:         time.Now().UTC(),
		SeedURLs:           []string{"https://example.com/seed1"},
	})
	if err != nil {
		t.Fatalf("StoryRegister (new story): %v", err)
	}
	if first.StoryID == "" || first.DevelopmentID == "" {
		t.Fatalf("StoryRegister returned empty ids: %+v", first)
	}

	found, err := svc.StoryFindSimilar(ctx, service.StoryFindSimilarRequest{
		ChannelID: "test-channel",
		Title:     "ICE enforcement surge in the Chicago area",
		Summary:   "Federal agents have expanded operations across the Chicago metro area.",
		Days:      30,
	})
	if err != nil {
		t.Fatalf("StoryFindSimilar: %v", err)
	}
	if len(found.Stories) != 1 {
		t.Fatalf("len(Stories) = %d, want 1: %+v", len(found.Stories), found.Stories)
	}
	if found.Stories[0].StoryID != first.StoryID {
		t.Fatalf("StoryID = %q, want %q", found.Stories[0].StoryID, first.StoryID)
	}
	if len(found.Stories[0].Developments) != 1 {
		t.Fatalf("len(Developments) = %d, want 1", len(found.Stories[0].Developments))
	}

	storyID := first.StoryID
	second, err := svc.StoryRegister(ctx, service.StoryRegisterRequest{
		ChannelID:          "test-channel",
		StoryID:            &storyID,
		DevelopmentSummary: "The court order was appealed by the federal government the next day.",
		ReportedAt:         time.Now().UTC(),
		SeedURLs:           []string{"https://example.com/seed2"},
	})
	if err != nil {
		t.Fatalf("StoryRegister (existing story): %v", err)
	}
	if second.StoryID != first.StoryID {
		t.Fatalf("second StoryID = %q, want %q", second.StoryID, first.StoryID)
	}

	found2, err := svc.StoryFindSimilar(ctx, service.StoryFindSimilarRequest{
		ChannelID: "test-channel",
		Title:     "ICE enforcement surge in the Chicago area",
		Summary:   "Federal agents have expanded operations across the Chicago metro area.",
	})
	if err != nil {
		t.Fatalf("StoryFindSimilar (2nd): %v", err)
	}
	if len(found2.Stories) != 1 {
		t.Fatalf("len(Stories) = %d, want 1", len(found2.Stories))
	}
	if len(found2.Stories[0].Developments) != 2 {
		t.Fatalf("len(Developments) = %d, want 2", len(found2.Stories[0].Developments))
	}
}

func TestExperimentAssign_DefaultArm(t *testing.T) {
	st := newTestStore(t)
	media := newTestMedia(t)
	seedChannel(t, st, "test-channel")

	svc := service.New(st, media, nil, nil, slog.Default())
	ctx := context.Background()

	reg, err := svc.StoryRegister(ctx, service.StoryRegisterRequest{
		ChannelID:          "test-channel",
		Title:              "A story",
		Summary:            "A summary that is long enough to pass validation checks.",
		DevelopmentSummary: "A development summary that is long enough to pass validation.",
		ReportedAt:         time.Now().UTC(),
		SeedURLs:           []string{"https://example.com/seed"},
	})
	if err != nil {
		t.Fatalf("StoryRegister: %v", err)
	}

	resp, err := svc.ExperimentAssign(ctx, service.ExperimentAssignRequest{
		ChannelID:     "test-channel",
		DevelopmentID: reg.DevelopmentID,
	})
	if err != nil {
		t.Fatalf("ExperimentAssign: %v", err)
	}
	if resp.ExperimentID != nil {
		t.Errorf("ExperimentID = %v, want nil", resp.ExperimentID)
	}
	if resp.ArmID != "default" {
		t.Errorf("ArmID = %q, want default", resp.ArmID)
	}
}
