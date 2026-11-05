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
		err := st.SaveCostRun(ctx, store.CostRun{TaskID: id, ChannelID: channel, DevelopmentID: development, StartedAt: time.Now(), Complete: true,
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

	build := func(key, development string, usd float64, sum string) {
		t.Helper()
		if err := st.SaveCostBuild(ctx, store.CostBuild{Key: key, DevelopmentID: development, USD: &usd, PackageSHA256: sum}); err != nil {
			t.Fatal(err)
		}
	}
	build("releases/a/1-package.tar.gz", published.DevelopmentID, 0.5, "aaa")
	build("releases/a/2-package.tar.gz", published.DevelopmentID, 0.25, "bbb")
	build("releases/b/1-package.tar.gz", dev.ID, 1.5, "ccc")
	// A retried upload stores the same package again under a new key: it's
	// remembered, so it isn't read again, but counts once.
	build("releases/a/3-package.tar.gz", published.DevelopmentID, 0.5, "aaa")
	// The same package for another story is another build.
	build("releases/b/2-package.tar.gz", dev.ID, 0.5, "aaa")
	// A package that can't be read is remembered too, and reported.
	if err := st.SaveCostBuild(ctx, store.CostBuild{Key: "releases/a/4-package.tar.gz", DevelopmentID: published.DevelopmentID}); err != nil {
		t.Fatal(err)
	}
	if err := st.SetCostSyncOK(ctx, store.CostSourceRuns); err != nil {
		t.Fatal(err)
	}
	if err := st.SetCostSyncOK(ctx, store.CostSourceBuilds); err != nil {
		t.Fatal(err)
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
		t.Errorf("builds %d, $%v: a retried upload counts once, an unreadable package not at all", v.Builds, v.ImagesUSD)
	}
	if c.OtherRuns != 2 || c.OtherTokens.Total() != 60 || c.OtherBuilds != 2 || c.OtherImagesUSD != 2 {
		t.Errorf("the rest: %+v", c)
	}
	if c.AgentRuns != 12 || c.MeasuredRuns != 8 || c.TokensUpdatedAt == nil || c.PicturesUpdatedAt == nil {
		t.Errorf("runs %d/%d, updated %v %v", c.MeasuredRuns, c.AgentRuns, c.TokensUpdatedAt, c.PicturesUpdatedAt)
	}
	if c.UnreadableBuilds != 1 || c.IncompleteRuns != 0 {
		t.Errorf("unreadable %d, incomplete %d", c.UnreadableBuilds, c.IncompleteRuns)
	}

	// A smaller reading means PolyMaths has forgotten some: it doesn't replace a run.
	save("run-1", published.DevelopmentID, 0, store.StageResearch)
	if c, err = st.ChannelCosts(ctx, channel, 300); err != nil {
		t.Fatal(err)
	}
	if v := c.Videos[0]; v.Tokens.Total() != 300 || v.Stages[store.StageResearch] != 100 {
		t.Errorf("a smaller reading replaced a run's usage: %+v", v)
	}
	// A larger one does.
	save("run-1", published.DevelopmentID, 994, store.StageResearch)
	if c, err = st.ChannelCosts(ctx, channel, 300); err != nil {
		t.Fatal(err)
	}
	if v := c.Videos[0]; v.Tokens.Total() != 1000+200 || v.Stages[store.StageResearch] != 1000 {
		t.Errorf("a larger reading didn't replace it: %+v", v)
	}

	// A run that can't be read shows up as incomplete, whether it's new or known,
	// keeps what was stored, and comes back complete once it's read.
	mark := func(id string) {
		t.Helper()
		if err := st.MarkCostRunUnread(ctx, store.CostRun{TaskID: id, ChannelID: channel, StartedAt: time.Now(), SourceUpdatedAt: time.Now()}); err != nil {
			t.Fatal(err)
		}
	}
	mark("run-4")
	mark("run-1")
	if c, err = st.ChannelCosts(ctx, channel, 300); err != nil {
		t.Fatal(err)
	}
	if c.IncompleteRuns != 2 || c.Videos[0].Tokens.Total() != 1200 {
		t.Errorf("incomplete %d, video %+v", c.IncompleteRuns, c.Videos[0].Tokens)
	}
	states, err := st.CostRunStates(ctx)
	if err != nil || len(states) != 5 || states["run-4"].Complete || states["run-1"].Complete || !states["run-2"].Complete {
		t.Fatalf("states %+v, %v", states, err)
	}
	save("run-1", published.DevelopmentID, 994, store.StageResearch)
	saveEmpty := func(id, onChannel string) {
		t.Helper()
		if err := st.SaveCostRun(ctx, store.CostRun{TaskID: id, ChannelID: onChannel, StartedAt: time.Now(), Complete: true,
			Stages: map[string]int64{}, SourceUpdatedAt: time.Now()}); err != nil {
			t.Fatal(err)
		}
	}
	saveEmpty("run-4", channel)
	if c, err = st.ChannelCosts(ctx, channel, 300); err != nil {
		t.Fatal(err)
	}
	if c.IncompleteRuns != 0 || c.OtherRuns != 3 {
		t.Errorf("after being read: incomplete %d, other %d", c.IncompleteRuns, c.OtherRuns)
	}

	// A corrected brief moves a run to the right channel.
	seedChannel(t, st, "costs-es")
	saveEmpty("run-4", "costs-es")
	if c, err = st.ChannelCosts(ctx, channel, 300); err != nil {
		t.Fatal(err)
	}
	if c.OtherRuns != 2 {
		t.Errorf("the run stayed on its old channel: %d other runs", c.OtherRuns)
	}

	// Saving a build twice doesn't count it twice.
	build("releases/a/1-package.tar.gz", published.DevelopmentID, 9, "zzz")
	keys, err := st.CostBuildKeys(ctx)
	if err != nil || len(keys) != 6 {
		t.Fatalf("keys %v, %v", keys, err)
	}
	if c, err = st.ChannelCosts(ctx, channel, 300); err != nil {
		t.Fatal(err)
	}
	if c.Videos[0].ImagesUSD != 0.75 || c.Videos[0].Builds != 2 {
		t.Errorf("a build read twice changed the sum: %+v", c.Videos[0])
	}

	if got, found, err := st.DevelopmentChannel(ctx, dev.ID); err != nil || !found || got != channel {
		t.Errorf("DevelopmentChannel: %q, %v, %v", got, found, err)
	}
	if _, found, err := st.DevelopmentChannel(ctx, "00000000-0000-0000-0000-000000000000"); err != nil || found {
		t.Errorf("DevelopmentChannel of nothing: %v, %v", found, err)
	}
}
