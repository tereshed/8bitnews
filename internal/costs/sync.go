package costs

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"path"
	"strings"
	"time"

	"github.com/tereshed/8bitnews/internal/store"
)

const (
	// measureWindow: a run left alone this long isn't measured again. It has to
	// stay under PolyMaths' 30 days, after which a run's usage reads as nothing.
	measureWindow = 25 * 24 * time.Hour
	// recheckAfter catches usage a run's own updated_at doesn't show, such as
	// work on one of its finished subtasks.
	recheckAfter = 24 * time.Hour

	buildsPrefix  = "releases/"
	packageSuffix = "-package.tar.gz"
	readTimeout   = 5 * time.Minute

	maxTreeTasks   = 1000
	maxPackageJSON = 32 << 20
	maxUnpacked    = 2 << 30
	maxBuildUSD    = 1000
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
	DevelopmentExists(ctx context.Context, id string) (bool, error)
	CostBuildKeys(ctx context.Context) (map[string]bool, error)
	SaveCostBuild(ctx context.Context, key, developmentID string, usd float64) error
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
	// unreadable: packages that can't be read, not asked for again until a restart.
	unreadable map[string]bool
}

// NewSyncer builds a Syncer; pm may be nil.
func NewSyncer(pm PolyMaths, st Store, media Media, interval time.Duration, log *slog.Logger) *Syncer {
	return &Syncer{pm: pm, store: st, media: media, interval: interval, log: log, now: time.Now, unreadable: map[string]bool{}}
}

// Run syncs now and then every interval, until ctx ends.
func (s *Syncer) Run(ctx context.Context) {
	if s.pm == nil {
		s.log.Info("costs: HUB_POLYMATHS_API_KEY is not set, the costs page has no token figures")
	}
	t := time.NewTicker(s.interval)
	defer t.Stop()
	for {
		s.Once(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// Once does one pass. A failure is logged and tried again on the next pass.
func (s *Syncer) Once(ctx context.Context) {
	if s.pm != nil {
		if err := s.syncRuns(ctx); err != nil && ctx.Err() == nil {
			s.log.Error("costs: syncing runs failed", "error", err)
		}
	}
	if err := s.syncBuilds(ctx); err != nil && ctx.Err() == nil {
		s.log.Error("costs: syncing builds failed", "error", err)
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
	for _, run := range runs {
		state, known := states[run.ID]
		if !dueForMeasuring(run, state, known, s.now()) {
			continue
		}
		if err := s.measure(ctx, run); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			// One run's failure mustn't hold up the others.
			s.log.Error("costs: measuring a run failed", "task", run.ID, "error", err)
		}
	}
	return nil
}

func dueForMeasuring(run Run, state store.CostRunState, known bool, now time.Time) bool {
	if !known {
		return true
	}
	switch {
	case now.Sub(run.UpdatedAt) > measureWindow:
		return false
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

func (s *Syncer) measure(ctx context.Context, run Run) error {
	brief, ok, err := s.pm.Brief(ctx, run.ID)
	if err != nil {
		return err
	}
	if !ok {
		return nil // nothing says which channel it was for: not a video's run
	}
	if _, err := s.store.GetChannel(ctx, brief.ChannelID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			s.log.Warn("costs: a run names an unknown channel", "task", run.ID, "channel", brief.ChannelID)
			return nil
		}
		return err
	}
	if brief.DevelopmentID != "" {
		exists, err := s.store.DevelopmentExists(ctx, brief.DevelopmentID)
		if err != nil {
			return err
		}
		if !exists {
			s.log.Warn("costs: a run names an unknown development", "task", run.ID, "development", brief.DevelopmentID)
			brief.DevelopmentID = ""
		}
	}

	total := store.CostRun{TaskID: run.ID, ChannelID: brief.ChannelID, DevelopmentID: brief.DevelopmentID,
		StartedAt: run.CreatedAt, SourceUpdatedAt: run.UpdatedAt, Stages: map[string]int64{}}
	from := run.CreatedAt.Add(-24 * time.Hour)
	seen := map[string]bool{run.ID: true}
	queue := []string{run.ID}
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		u, err := s.pm.Usage(ctx, id, from)
		if err != nil {
			return err // a partial sum would read as a cheaper video
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
	return s.store.SaveCostRun(ctx, total)
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
	for _, key := range keys {
		development, ok := buildDevelopment(key)
		if !ok || known[key] || s.unreadable[key] {
			continue
		}
		if err := s.readBuild(ctx, key, development); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			var bad *badPackageError
			if errors.As(err, &bad) {
				s.unreadable[key] = true
			}
			s.log.Error("costs: reading a build failed", "key", key, "error", err)
		}
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
	exists, err := s.store.DevelopmentExists(ctx, development)
	if err != nil {
		return err
	}
	if !exists {
		return &badPackageError{"its development isn't in hub"}
	}
	ctx, cancel := context.WithTimeout(ctx, readTimeout)
	defer cancel()
	body, _, err := s.media.Open(ctx, key)
	if err != nil {
		return err
	}
	defer func() { _ = body.Close() }()
	usd, err := imagesCost(body)
	if err != nil {
		return err
	}
	return s.store.SaveCostBuild(ctx, key, development, usd)
}

// badPackageError: reading the package again won't help.
type badPackageError struct{ reason string }

func (e *badPackageError) Error() string { return "bad package: " + e.reason }

// imagesCost finds package.json in a package archive and returns what the
// build's pictures cost: what the provider charged, with nothing for a picture
// taken from the build's cache (studio/README.md). A build with no generated
// pictures cost nothing. An agent uploads the package, so its size and its
// figure are both checked.
func imagesCost(archive io.Reader) (float64, error) {
	gz, err := gzip.NewReader(archive)
	if err != nil {
		return 0, &badPackageError{"not gzip: " + err.Error()}
	}
	defer func() { _ = gz.Close() }()
	tr := tar.NewReader(io.LimitReader(gz, maxUnpacked))
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return 0, &badPackageError{"no package.json"}
		}
		if err != nil {
			return 0, fmt.Errorf("reading the package: %w", err)
		}
		if h.Typeflag != tar.TypeReg || path.Base(h.Name) != "package.json" {
			continue
		}
		var pkg struct {
			Images struct {
				CostUSD float64 `json:"cost_usd"`
			} `json:"images"`
		}
		if err := json.NewDecoder(io.LimitReader(tr, maxPackageJSON)).Decode(&pkg); err != nil {
			return 0, &badPackageError{"package.json: " + err.Error()}
		}
		if usd := pkg.Images.CostUSD; usd < 0 || usd > maxBuildUSD {
			return 0, &badPackageError{fmt.Sprintf("implausible images.cost_usd %v", usd)}
		}
		return pkg.Images.CostUSD, nil
	}
}
