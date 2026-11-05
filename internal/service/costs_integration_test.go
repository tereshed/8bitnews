package service_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/tereshed/8bitnews/internal/store"
)

func TestChannelCosts(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	const channel = "costs-us"
	published := seedRelease(t, st, channel)
	if _, _, err := st.ClaimReleaseForCard(ctx, 0); err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.DecideRelease(ctx, published.ID, "approved", "K"); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := st.SetPublished(ctx, published.ID, true); err != nil || !ok {
		t.Fatalf("publishing: %v, %v", ok, err)
	}

	// A second story whose video never went out.
	story, err := st.CreateStory(ctx, channel, "Closed", "closed")
	if err != nil {
		t.Fatal(err)
	}
	dev, err := st.CreateDevelopment(ctx, story.ID, "No video", time.Now(), json.RawMessage(`[]`), "")
	if err != nil {
		t.Fatal(err)
	}

	save := func(id, development string, read int64, stage string) {
		t.Helper()
		err := st.SaveCostRun(ctx, store.CostRun{TaskID: id, ChannelID: channel, DevelopmentID: development, StartedAt: time.Now(),
			AgentRuns: 3, MeasuredRuns: 2, Tokens: store.TokenCounts{Input: 1, Output: 2, CacheWrite: 3, CacheRead: read},
			Stages: map[string]int64{stage: read + 6}, SourceUpdatedAt: time.Now()})
		if err != nil {
			t.Fatalf("saving run %s: %v", id, err)
		}
	}
	save("run-1", published.DevelopmentID, 94, store.StageResearch)
	save("run-1b", published.DevelopmentID, 194, store.StageLegal) // a second run on the same story
	save("run-2", dev.ID, 44, store.StageScript)
	save("run-3", "", 4, store.StageEditor) // never got as far as a development

	for key, build := range map[string]struct {
		development string
		usd         float64
	}{
		"releases/a/1-package.tar.gz": {published.DevelopmentID, 0.5},
		"releases/a/2-package.tar.gz": {published.DevelopmentID, 0.25},
		"releases/b/1-package.tar.gz": {dev.ID, 1.5},
	} {
		if err := st.SaveCostBuild(ctx, key, build.development, build.usd); err != nil {
			t.Fatal(err)
		}
	}

	c, err := st.ChannelCosts(ctx, channel, 300)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Videos) != 1 || c.Videos[0].Release.ID != published.ID {
		t.Fatalf("videos %+v, want only the published one", c.Videos)
	}
	v := c.Videos[0]
	if v.Tokens.Total() != 100+200 || v.Stages[store.StageResearch] != 100 || v.Stages[store.StageLegal] != 200 || v.AgentRuns != 6 {
		t.Errorf("video %+v: both runs of the story should add up", v)
	}
	if v.Builds != 2 || v.ImagesUSD != 0.75 {
		t.Errorf("builds %d, $%v", v.Builds, v.ImagesUSD)
	}
	if c.OtherRuns != 2 || c.OtherTokens.Total() != 60 || c.OtherBuilds != 1 || c.OtherImagesUSD != 1.5 {
		t.Errorf("the rest: %+v", c)
	}
	if c.AgentRuns != 12 || c.MeasuredRuns != 8 || c.UpdatedAt == nil {
		t.Errorf("runs %d/%d, updated %v", c.MeasuredRuns, c.AgentRuns, c.UpdatedAt)
	}

	// PolyMaths forgets a run after 30 days: a smaller reading doesn't replace it.
	save("run-1", published.DevelopmentID, 0, store.StageResearch)
	c, err = st.ChannelCosts(ctx, channel, 300)
	if err != nil {
		t.Fatal(err)
	}
	if v := c.Videos[0]; v.Tokens.Total() != 300 || v.Stages[store.StageResearch] != 100 {
		t.Errorf("a smaller reading replaced a run's usage: %+v", v)
	}
	// A larger one does.
	save("run-1", published.DevelopmentID, 994, store.StageResearch)
	c, err = st.ChannelCosts(ctx, channel, 300)
	if err != nil {
		t.Fatal(err)
	}
	if v := c.Videos[0]; v.Tokens.Total() != 1000+200 || v.Stages[store.StageResearch] != 1000 {
		t.Errorf("a larger reading didn't replace it: %+v", v)
	}

	// Saving a build twice doesn't count it twice.
	if err := st.SaveCostBuild(ctx, "releases/a/1-package.tar.gz", published.DevelopmentID, 9); err != nil {
		t.Fatal(err)
	}
	keys, err := st.CostBuildKeys(ctx)
	if err != nil || len(keys) != 3 {
		t.Fatalf("keys %v, %v", keys, err)
	}
	c, _ = st.ChannelCosts(ctx, channel, 300)
	if c.Videos[0].ImagesUSD != 0.75 {
		t.Errorf("a build read twice changed the sum: %v", c.Videos[0].ImagesUSD)
	}

	states, err := st.CostRunStates(ctx)
	if err != nil || len(states) != 4 {
		t.Fatalf("states %v, %v", states, err)
	}
	if found, err := st.DevelopmentExists(ctx, dev.ID); err != nil || !found {
		t.Errorf("DevelopmentExists: %v, %v", found, err)
	}
	if found, err := st.DevelopmentExists(ctx, "00000000-0000-0000-0000-000000000000"); err != nil || found {
		t.Errorf("DevelopmentExists of nothing: %v, %v", found, err)
	}
}
