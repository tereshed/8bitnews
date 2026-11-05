package costs

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/tereshed/8bitnews/internal/store"
)

const (
	// retention is how long PolyMaths keeps a run's usage.
	retention = 30 * 24 * time.Hour
	// measureWindow: a run this old isn't measured again, so that a later reading
	// can't mix what PolyMaths still has with what it has forgotten. The page lists
	// work on a run after this as not counted.
	measureWindow = 25 * 24 * time.Hour
	// recheckAfter catches usage a run's own updated_at doesn't show, such as
	// work on one of its finished subtasks.
	recheckAfter = 24 * time.Hour

	passDeadline = 20 * time.Minute
	readTimeout  = 5 * time.Minute

	buildsPrefix    = "releases/"
	packageSuffix   = "-package.tar.gz"
	packageJSONPath = "package/package.json" // newsroom/tools/video_build.py packs the build under package/

	maxTreeTasks     = 1000
	maxPackageJSON   = 32 << 20
	maxUnpacked      = 2 << 30
	maxBuildUSD      = 50 // far above any real build; pictures have cost about $1
	maxBuildAttempts = 5
)

// PolyMaths is what the sync reads from PolyMaths (Client).
type PolyMaths interface {
	Runs(ctx context.Context) ([]Run, error)
	Subtasks(ctx context.Context, taskID string) ([]string, error)
	Usage(ctx context.Context, taskID string, from time.Time) (Usage, error)
	Brief(ctx context.Context, taskID string) (Brief, bool, error)
}

// Store is where the sync keeps what it read (internal/store).
type Store interface {
	CostRunStates(ctx context.Context) (map[string]store.CostRunState, error)
	SaveCostRun(ctx context.Context, r store.CostRun) error
	MarkCostRunUnread(ctx context.Context, r store.CostRun) error
	DevelopmentChannel(ctx context.Context, id string) (string, bool, error)
	CostBuildKeys(ctx context.Context) (map[string]bool, error)
	SaveCostBuild(ctx context.Context, b store.CostBuild) error
	SetCostSyncOK(ctx context.Context, source string) error
	GetChannel(ctx context.Context, id string) (store.Channel, error)
}

// Media is the object store holding the video builds (internal/storage).
type Media interface {
	List(ctx context.Context, prefix string) ([]string, error)
	Open(ctx context.Context, key string) (io.ReadCloser, int64, error)
}

// Syncer brings hub's copy of the costs up to date.
type Syncer struct {
	pm       PolyMaths // nil: no key, so no tokens; the pictures still count
	store    Store
	media    Media
	interval time.Duration
	log      *slog.Logger
	now      func() time.Time

	// Runs that say nothing usable about where they belong, as of their
	// updated_at: not read again until they change. Only the runs pass uses it.
	unplaced map[string]time.Time
	// What the builds pass has tried: failures per package, and packages of a
	// development hub doesn't know, which can't be stored.
	attempts map[string]int
	orphans  map[string]bool
}

// NewSyncer builds a Syncer; pm may be nil.
func NewSyncer(pm PolyMaths, st Store, media Media, interval time.Duration, log *slog.Logger) *Syncer {
	return &Syncer{pm: pm, store: st, media: media, interval: interval, log: log, now: time.Now,
		unplaced: map[string]time.Time{}, attempts: map[string]int{}, orphans: map[string]bool{}}
}

// Run reads both sources now and then every interval, until ctx ends. They run
// apart, so a slow PolyMaths doesn't hold up the pictures' dollars.
func (s *Syncer) Run(ctx context.Context) {
	if s.pm == nil {
		s.log.Info("costs: HUB_POLYMATHS_API_KEY is not set, the costs page has no token figures")
	} else {
		go s.every(ctx, s.passRuns)
	}
	s.every(ctx, s.passBuilds)
}

func (s *Syncer) every(ctx context.Context, pass func(context.Context)) {
	t := time.NewTicker(s.interval)
	defer t.Stop()
	for {
		pass(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// Once does one pass of each source, one after the other.
func (s *Syncer) Once(ctx context.Context) {
	if s.pm != nil {
		s.passRuns(ctx)
	}
	s.passBuilds(ctx)
}

func (s *Syncer) passRuns(ctx context.Context) {
	s.pass(ctx, store.CostSourceRuns, s.syncRuns)
}

func (s *Syncer) passBuilds(ctx context.Context) {
	s.pass(ctx, store.CostSourceBuilds, s.syncBuilds)
}

// pass runs one source under a deadline and records when it was read in full. A
// failure is logged and tried again on the next pass.
func (s *Syncer) pass(ctx context.Context, source string, sync func(context.Context) error) {
	ctx, cancel := context.WithTimeout(ctx, passDeadline)
	defer cancel()
	if err := sync(ctx); err != nil {
		if ctx.Err() == nil || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			s.log.Error("costs: a sync pass failed", "source", source, "error", err)
		}
		return
	}
	if err := s.store.SetCostSyncOK(ctx, source); err != nil {
		s.log.Error("costs: recording a sync pass failed", "source", source, "error", err)
	}
}

func (s *Syncer) syncRuns(ctx context.Context) error {
	runs, err := s.pm.Runs(ctx)
	if err != nil {
		return err
	}
	states, err := s.store.CostRunStates(ctx)
	if err != nil {
		return err
	}
	failed := 0
	for _, run := range runs {
		if asOf, ok := s.unplaced[run.ID]; ok && !run.UpdatedAt.After(asOf) {
			continue
		}
		state, known := states[run.ID]
		if !dueForMeasuring(run, state, known, s.now()) {
			continue
		}
		// One run's failure mustn't hold up the others.
		if err := s.syncRun(ctx, run, known); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			failed++
			s.log.Error("costs: measuring a run failed", "task", run.ID, "error", err)
		}
	}
	if failed > 0 {
		return fmt.Errorf("%d runs failed", failed)
	}
	return nil
}

func dueForMeasuring(run Run, state store.CostRunState, known bool, now time.Time) bool {
	switch {
	case !known:
		return true
	case now.Sub(run.CreatedAt) > measureWindow:
		return false
	case !state.Complete:
		return true
	case run.UpdatedAt.After(state.SourceUpdatedAt):
		return true
	case !finished(run.Status):
		return true
	default:
		return now.Sub(state.MeasuredAt) >= recheckAfter
	}
}

// finished: nothing is running in the task, though it may be sent back to work.
func finished(status string) bool {
	switch status {
	case "done", "cancelled", "failed", "needs_human":
		return true
	}
	return false
}

func (s *Syncer) syncRun(ctx context.Context, run Run, known bool) error {
	brief, ok, err := s.place(ctx, run)
	if err != nil {
		return err
	}
	if !ok {
		s.unplaced[run.ID] = run.UpdatedAt
		return nil
	}
	total := store.CostRun{TaskID: run.ID, ChannelID: brief.ChannelID, DevelopmentID: brief.DevelopmentID,
		StartedAt: run.CreatedAt, SourceUpdatedAt: run.UpdatedAt, Stages: map[string]int64{},
		// First read this late, PolyMaths may have forgotten part of it already.
		Complete: known || s.now().Sub(run.CreatedAt) < retention}
	if err := s.measure(ctx, run, &total); err != nil {
		if ctx.Err() == nil {
			// A partial sum would read as a cheaper video: say it's missing instead.
			if markErr := s.store.MarkCostRunUnread(ctx, total); markErr != nil {
				return errors.Join(err, markErr)
			}
		}
		return err
	}
	return s.store.SaveCostRun(ctx, total)
}

// place works out which channel and story a run belongs to; false if it can't
// tell. A named development decides the channel, as it does for the run's builds.
func (s *Syncer) place(ctx context.Context, run Run) (Brief, bool, error) {
	brief, ok, err := s.pm.Brief(ctx, run.ID)
	if err != nil || !ok {
		return Brief{}, false, err
	}
	if brief.DevelopmentID != "" {
		channel, found, err := s.store.DevelopmentChannel(ctx, brief.DevelopmentID)
		if err != nil {
			return Brief{}, false, err
		}
		switch {
		case !found:
			s.log.Warn("costs: a run names an unknown development", "task", run.ID, "development", brief.DevelopmentID)
			brief.DevelopmentID = ""
		case channel != brief.ChannelID:
			s.log.Warn("costs: a run's channel isn't its development's", "task", run.ID, "brief", brief.ChannelID, "development", channel)
			brief.ChannelID = channel
		}
	}
	if _, err := s.store.GetChannel(ctx, brief.ChannelID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			s.log.Warn("costs: a run names an unknown channel", "task", run.ID, "channel", brief.ChannelID)
			return Brief{}, false, nil
		}
		return Brief{}, false, err
	}
	return brief, true, nil
}

// measure adds up a run and all its subtasks into total.
func (s *Syncer) measure(ctx context.Context, run Run, total *store.CostRun) error {
	from := run.CreatedAt.Add(-24 * time.Hour)
	seen := map[string]bool{run.ID: true}
	queue := []string{run.ID}
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		u, err := s.pm.Usage(ctx, id, from)
		if err != nil {
			return err
		}
		total.AgentRuns += u.AgentRuns
		total.MeasuredRuns += u.MeasuredRuns
		total.Tokens.Input += u.Tokens.Input
		total.Tokens.Output += u.Tokens.Output
		total.Tokens.CacheWrite += u.Tokens.CacheWrite
		total.Tokens.CacheRead += u.Tokens.CacheRead
		for stage, n := range u.Stages {
			total.Stages[stage] += n
		}
		subtasks, err := s.pm.Subtasks(ctx, id)
		if err != nil {
			return err
		}
		for _, sub := range subtasks {
			if !seen[sub] {
				if len(seen) >= maxTreeTasks {
					return fmt.Errorf("more than %d tasks in the run", maxTreeTasks)
				}
				seen[sub] = true
				queue = append(queue, sub)
			}
		}
	}
	return nil
}

func (s *Syncer) syncBuilds(ctx context.Context) error {
	keys, err := s.media.List(ctx, buildsPrefix)
	if err != nil {
		return err
	}
	known, err := s.store.CostBuildKeys(ctx)
	if err != nil {
		return err
	}
	failed := 0
	for _, key := range keys {
		development, ok := buildDevelopment(key)
		if !ok || known[key] || s.orphans[key] {
			continue
		}
		if err := s.readBuild(ctx, key, development); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			failed++
			s.log.Error("costs: reading a build failed", "key", key, "error", err)
		}
	}
	if failed > 0 {
		return fmt.Errorf("%d builds failed", failed)
	}
	return nil
}

// buildDevelopment says whether key is a video build's package, and whose:
// releases/<development id>/<uuid>-package.tar.gz (service.objectKey).
func buildDevelopment(key string) (string, bool) {
	rest, ok := strings.CutPrefix(key, buildsPrefix)
	if !ok || !strings.HasSuffix(key, packageSuffix) {
		return "", false
	}
	development, file, ok := strings.Cut(rest, "/")
	if !ok || strings.Contains(file, "/") || !uuidPattern.MatchString(development) {
		return "", false
	}
	return development, true
}

func (s *Syncer) readBuild(ctx context.Context, key, development string) error {
	if _, found, err := s.store.DevelopmentChannel(ctx, development); err != nil {
		return err
	} else if !found {
		s.orphans[key] = true
		s.log.Warn("costs: a build's development isn't in hub", "key", key)
		return nil
	}
	usd, sum, err := s.readPackage(ctx, key)
	if err != nil {
		// A dropped connection and a damaged archive look alike from here, so
		// any failure gets a few tries. Then the page counts it as unreadable.
		s.attempts[key]++
		if ctx.Err() != nil || s.attempts[key] < maxBuildAttempts {
			return err
		}
		if saveErr := s.store.SaveCostBuild(ctx, store.CostBuild{Key: key, DevelopmentID: development}); saveErr != nil {
			return errors.Join(err, saveErr)
		}
		return err
	}
	return s.store.SaveCostBuild(ctx, store.CostBuild{Key: key, DevelopmentID: development, USD: &usd, PackageSHA256: sum})
}

func (s *Syncer) readPackage(ctx context.Context, key string) (float64, string, error) {
	ctx, cancel := context.WithTimeout(ctx, readTimeout)
	defer cancel()
	body, _, err := s.media.Open(ctx, key)
	if err != nil {
		return 0, "", err
	}
	defer func() { _ = body.Close() }()
	return imagesCost(body)
}

// imagesCost finds package.json in a package archive and returns what the build's
// pictures cost, as the provider charged it, and the file's hash, which tells a
// retried upload from a real rebuild. A build with no generated pictures has no
// "images" and cost nothing. An agent uploads the package, so its path, size and
// figure are all checked.
func imagesCost(archive io.Reader) (float64, string, error) {
	gz, err := gzip.NewReader(archive)
	if err != nil {
		return 0, "", fmt.Errorf("not a gzip archive: %w", err)
	}
	defer func() { _ = gz.Close() }()
	tr := tar.NewReader(io.LimitReader(gz, maxUnpacked))
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return 0, "", errors.New("no " + packageJSONPath)
		}
		if err != nil {
			return 0, "", fmt.Errorf("reading the package: %w", err)
		}
		if h.Typeflag != tar.TypeReg || h.Name != packageJSONPath {
			continue
		}
		raw, err := io.ReadAll(io.LimitReader(tr, maxPackageJSON))
		if err != nil {
			return 0, "", fmt.Errorf("reading %s: %w", packageJSONPath, err)
		}
		usd, err := parseImagesCost(raw)
		if err != nil {
			return 0, "", err
		}
		sum := sha256.Sum256(raw)
		return usd, hex.EncodeToString(sum[:]), nil
	}
}

func parseImagesCost(raw []byte) (float64, error) {
	var pkg struct {
		Images *struct {
			CostUSD *float64 `json:"cost_usd"`
		} `json:"images"`
	}
	if err := json.Unmarshal(raw, &pkg); err != nil {
		return 0, fmt.Errorf("decoding %s: %w", packageJSONPath, err)
	}
	if pkg.Images == nil {
		return 0, nil
	}
	if pkg.Images.CostUSD == nil {
		return 0, errors.New("images has no cost_usd")
	}
	if usd := *pkg.Images.CostUSD; usd < 0 || usd > maxBuildUSD {
		return 0, fmt.Errorf("implausible images.cost_usd %v", usd)
	}
	return *pkg.Images.CostUSD, nil
}
