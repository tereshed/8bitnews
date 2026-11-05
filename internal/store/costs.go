package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// The pipeline's stages, as a video's tokens are split on the costs page, in
// the order the page shows them.
const (
	StageResearch = "research"
	StageScript   = "script"
	StageLegal    = "legal"
	StageEditor   = "editor"
	StageVideo    = "video"
	StageOther    = "other"
)

// CostStages lists the stages in that order.
var CostStages = []string{StageResearch, StageScript, StageLegal, StageEditor, StageVideo, StageOther}

// The sources the costs sync reads, for SetCostSyncOK.
const (
	CostSourceRuns   = "runs"
	CostSourceBuilds = "builds"
)

// TokenCounts is what a run's agents read and wrote.
type TokenCounts struct {
	Input      int64 // read for the first time, outside the cache
	Output     int64 // written by the model
	CacheWrite int64 // read for the first time, and stored in the cache
	CacheRead  int64 // read again from the cache
}

// Total is every counter together.
func (c TokenCounts) Total() int64 { return c.Input + c.Output + c.CacheWrite + c.CacheRead }

func (c *TokenCounts) add(o TokenCounts) {
	c.Input += o.Input
	c.Output += o.Output
	c.CacheWrite += o.CacheWrite
	c.CacheRead += o.CacheRead
}

// CostRun is one Newsroom run's token usage (migrations/00005_costs.sql).
type CostRun struct {
	TaskID          string
	ChannelID       string
	DevelopmentID   string // empty: the run never got as far as a development
	StartedAt       time.Time
	AgentRuns       int
	MeasuredRuns    int
	Tokens          TokenCounts
	Stages          map[string]int64 // total tokens by pipeline stage
	Complete        bool             // false: the figures are known to fall short
	SourceUpdatedAt time.Time
}

// CostRunState is what the sync needs to know to decide whether to measure a run again.
type CostRunState struct {
	Complete        bool
	SourceUpdatedAt time.Time
	MeasuredAt      time.Time
}

// CostRunStates returns every stored run's state, by task id.
func (s *Store) CostRunStates(ctx context.Context) (map[string]CostRunState, error) {
	rows, err := s.pool.Query(ctx, `SELECT task_id, complete, source_updated_at, measured_at FROM cost_runs`)
	if err != nil {
		return nil, fmt.Errorf("listing cost runs: %w", err)
	}
	defer rows.Close()
	out := map[string]CostRunState{}
	for rows.Next() {
		var id string
		var st CostRunState
		if err := rows.Scan(&id, &st.Complete, &st.SourceUpdatedAt, &st.MeasuredAt); err != nil {
			return nil, fmt.Errorf("scanning a cost run: %w", err)
		}
		out[id] = st
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating cost runs: %w", err)
	}
	return out, nil
}

// SaveCostRun stores a run's usage. A measurement with fewer tokens than the
// stored one doesn't replace them, only marks the run as checked: a run never
// uses tokens back, so a smaller reading means PolyMaths has forgotten some.
func (s *Store) SaveCostRun(ctx context.Context, r CostRun) error {
	stages, err := json.Marshal(r.Stages)
	if err != nil {
		return fmt.Errorf("encoding the stages of cost run %s: %w", r.TaskID, err)
	}
	_, err = s.pool.Exec(ctx, `
		INSERT INTO cost_runs (task_id, channel_id, development_id, started_at, agent_runs, measured_runs,
		                       input_tokens, output_tokens, cache_write_tokens, cache_read_tokens,
		                       stages, complete, source_updated_at, measured_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, now())
		ON CONFLICT (task_id) DO UPDATE SET
			channel_id = EXCLUDED.channel_id,
			development_id = COALESCE(EXCLUDED.development_id, cost_runs.development_id),
			source_updated_at = EXCLUDED.source_updated_at,
			measured_at = now(),
			complete = CASE WHEN `+newUsageIsWhole+` THEN EXCLUDED.complete ELSE cost_runs.complete END,
			agent_runs = CASE WHEN `+newUsageIsWhole+` THEN EXCLUDED.agent_runs ELSE cost_runs.agent_runs END,
			measured_runs = CASE WHEN `+newUsageIsWhole+` THEN EXCLUDED.measured_runs ELSE cost_runs.measured_runs END,
			input_tokens = CASE WHEN `+newUsageIsWhole+` THEN EXCLUDED.input_tokens ELSE cost_runs.input_tokens END,
			output_tokens = CASE WHEN `+newUsageIsWhole+` THEN EXCLUDED.output_tokens ELSE cost_runs.output_tokens END,
			cache_write_tokens = CASE WHEN `+newUsageIsWhole+` THEN EXCLUDED.cache_write_tokens ELSE cost_runs.cache_write_tokens END,
			cache_read_tokens = CASE WHEN `+newUsageIsWhole+` THEN EXCLUDED.cache_read_tokens ELSE cost_runs.cache_read_tokens END,
			stages = CASE WHEN `+newUsageIsWhole+` THEN EXCLUDED.stages ELSE cost_runs.stages END
	`, r.TaskID, r.ChannelID, nullable(r.DevelopmentID), r.StartedAt, r.AgentRuns, r.MeasuredRuns,
		r.Tokens.Input, r.Tokens.Output, r.Tokens.CacheWrite, r.Tokens.CacheRead,
		stages, r.Complete, r.SourceUpdatedAt)
	if err != nil {
		return fmt.Errorf("saving cost run %s: %w", r.TaskID, err)
	}
	return nil
}

const newUsageIsWhole = `(EXCLUDED.input_tokens + EXCLUDED.output_tokens + EXCLUDED.cache_write_tokens + EXCLUDED.cache_read_tokens)
	>= (cost_runs.input_tokens + cost_runs.output_tokens + cost_runs.cache_write_tokens + cost_runs.cache_read_tokens)`

// MarkCostRunUnread records that a run's usage couldn't be read: the page says
// so, instead of the run dropping out of sight. What was stored stays, and the
// sync tries again on its next pass.
func (s *Store) MarkCostRunUnread(ctx context.Context, r CostRun) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO cost_runs (task_id, channel_id, development_id, started_at, complete, source_updated_at)
		VALUES ($1, $2, $3, $4, false, $5)
		ON CONFLICT (task_id) DO UPDATE SET complete = false
	`, r.TaskID, r.ChannelID, nullable(r.DevelopmentID), r.StartedAt, r.SourceUpdatedAt)
	if err != nil {
		return fmt.Errorf("marking cost run %s unread: %w", r.TaskID, err)
	}
	return nil
}

func nullable(id string) *string {
	if id == "" {
		return nil
	}
	return &id
}

// DevelopmentChannel returns the channel a development belongs to; false if hub
// doesn't know the development.
func (s *Store) DevelopmentChannel(ctx context.Context, id string) (string, bool, error) {
	var channel string
	err := s.pool.QueryRow(ctx, `
		SELECT st.channel_id FROM story_developments d JOIN stories st ON st.id = d.story_id WHERE d.id = $1
	`, id).Scan(&channel)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("looking for development %s: %w", id, err)
	}
	return channel, true, nil
}

// CostBuildKeys returns the package archives already dealt with.
func (s *Store) CostBuildKeys(ctx context.Context) (map[string]bool, error) {
	rows, err := s.pool.Query(ctx, `SELECT object_key FROM cost_builds`)
	if err != nil {
		return nil, fmt.Errorf("listing cost builds: %w", err)
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return nil, fmt.Errorf("scanning a cost build: %w", err)
		}
		out[key] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating cost builds: %w", err)
	}
	return out, nil
}

// CostBuild is one build's package. USD is nil if the package can't be read.
type CostBuild struct {
	Key           string
	DevelopmentID string
	USD           *float64
	PackageSHA256 string
}

// SaveCostBuild records what one build's pictures cost. A build is read once. A
// package identical to one already counted for its development is a retried
// upload of the same build: it's recorded, so it isn't read again, but not counted.
func (s *Store) SaveCostBuild(ctx context.Context, b CostBuild) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO cost_builds (object_key, development_id, images_usd, package_sha256, counted)
		VALUES ($1, $2, $3, $4, $3::numeric IS NOT NULL AND NOT EXISTS (
			SELECT 1 FROM cost_builds WHERE development_id = $2 AND package_sha256 = $4 AND counted
		))
		ON CONFLICT (object_key) DO NOTHING
	`, b.Key, b.DevelopmentID, b.USD, b.PackageSHA256)
	if err != nil {
		return fmt.Errorf("saving cost build %s: %w", b.Key, err)
	}
	return nil
}

// SetCostSyncOK records that a source has just been read in full.
func (s *Store) SetCostSyncOK(ctx context.Context, source string) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO cost_sync (source, ok_at) VALUES ($1, now())
		ON CONFLICT (source) DO UPDATE SET ok_at = now()
	`, source)
	if err != nil {
		return fmt.Errorf("recording the %s sync: %w", source, err)
	}
	return nil
}

// VideoCost is what one published video took, its reruns and rebuilds included.
type VideoCost struct {
	Release      Release // id, channel, the script's metadata and the dates
	Tokens       TokenCounts
	Stages       map[string]int64
	AgentRuns    int
	MeasuredRuns int
	Builds       int
	ImagesUSD    float64
}

// ChannelCosts is everything a channel's costs page shows.
type ChannelCosts struct {
	Videos []VideoCost // newest first
	// The channel's runs and builds that have no published video, so far.
	OtherRuns      int
	OtherTokens    TokenCounts
	OtherBuilds    int
	OtherImagesUSD float64
	// Every agent run behind the channel's figures, and how many have a record.
	AgentRuns    int
	MeasuredRuns int
	// Where the figures are known to fall short.
	IncompleteRuns   int
	UnreadableBuilds int
	// When each source was last read in full.
	TokensUpdatedAt   *time.Time
	PicturesUpdatedAt *time.Time
}

// ChannelCosts adds up a channel's costs: per published video, as on the
// channel's list of videos, and all the rest together.
func (s *Store) ChannelCosts(ctx context.Context, channelID string, n int) (ChannelCosts, error) {
	var out ChannelCosts
	byDevelopment := map[string]int{} // development id -> index in out.Videos

	rows, err := s.pool.Query(ctx, `
		SELECT id, development_id, channel_id, status, jsonb_build_object('metadata', script->'metadata'), created_at, moderated_at
		FROM releases
		WHERE channel_id = $1 AND status IN `+listedStatuses+`
		ORDER BY COALESCE(moderated_at, created_at) DESC
		LIMIT $2
	`, channelID, n)
	if err != nil {
		return out, fmt.Errorf("listing published releases of channel %s: %w", channelID, err)
	}
	if err := scanVideoCosts(rows, &out, byDevelopment); err != nil {
		return out, fmt.Errorf("listing published releases of channel %s: %w", channelID, err)
	}

	if err := s.addRunCosts(ctx, channelID, &out, byDevelopment); err != nil {
		return out, err
	}
	if err := s.addBuildCosts(ctx, channelID, &out, byDevelopment); err != nil {
		return out, err
	}
	if err := s.addCostSync(ctx, &out); err != nil {
		return out, err
	}
	return out, nil
}

func scanVideoCosts(rows pgx.Rows, out *ChannelCosts, byDevelopment map[string]int) error {
	defer rows.Close()
	for rows.Next() {
		var v VideoCost
		r := &v.Release
		if err := rows.Scan(&r.ID, &r.DevelopmentID, &r.ChannelID, &r.Status, &r.Script, &r.CreatedAt, &r.Moderation.At); err != nil {
			return err
		}
		v.Stages = map[string]int64{}
		// A development has one active release at most, so one video.
		byDevelopment[r.DevelopmentID] = len(out.Videos)
		out.Videos = append(out.Videos, v)
	}
	return rows.Err()
}

func (s *Store) addRunCosts(ctx context.Context, channelID string, out *ChannelCosts, byDevelopment map[string]int) error {
	rows, err := s.pool.Query(ctx, `
		SELECT COALESCE(development_id::text, ''), agent_runs, measured_runs,
		       input_tokens, output_tokens, cache_write_tokens, cache_read_tokens, stages, complete
		FROM cost_runs WHERE channel_id = $1
	`, channelID)
	if err != nil {
		return fmt.Errorf("listing cost runs of channel %s: %w", channelID, err)
	}
	defer rows.Close()
	for rows.Next() {
		var development string
		var agentRuns, measuredRuns int
		var tokens TokenCounts
		var stagesJSON []byte
		var complete bool
		if err := rows.Scan(&development, &agentRuns, &measuredRuns, &tokens.Input, &tokens.Output,
			&tokens.CacheWrite, &tokens.CacheRead, &stagesJSON, &complete); err != nil {
			return fmt.Errorf("scanning a cost run of channel %s: %w", channelID, err)
		}
		out.AgentRuns += agentRuns
		out.MeasuredRuns += measuredRuns
		if !complete {
			out.IncompleteRuns++
		}
		i, published := byDevelopment[development]
		if !published {
			out.OtherRuns++
			out.OtherTokens.add(tokens)
			continue
		}
		var stages map[string]int64
		if err := json.Unmarshal(stagesJSON, &stages); err != nil {
			return fmt.Errorf("reading the stages of a cost run of channel %s: %w", channelID, err)
		}
		v := &out.Videos[i]
		v.Tokens.add(tokens)
		v.AgentRuns += agentRuns
		v.MeasuredRuns += measuredRuns
		for stage, n := range stages {
			v.Stages[stage] += n
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterating cost runs of channel %s: %w", channelID, err)
	}
	return nil
}

func (s *Store) addBuildCosts(ctx context.Context, channelID string, out *ChannelCosts, byDevelopment map[string]int) error {
	rows, err := s.pool.Query(ctx, `
		SELECT b.development_id::text,
		       count(*) FILTER (WHERE b.counted),
		       COALESCE(sum(b.images_usd) FILTER (WHERE b.counted), 0)::float8,
		       count(*) FILTER (WHERE b.images_usd IS NULL)
		FROM cost_builds b
		JOIN story_developments d ON d.id = b.development_id
		JOIN stories st ON st.id = d.story_id
		WHERE st.channel_id = $1
		GROUP BY b.development_id
	`, channelID)
	if err != nil {
		return fmt.Errorf("listing cost builds of channel %s: %w", channelID, err)
	}
	defer rows.Close()
	for rows.Next() {
		var development string
		var builds, unreadable int
		var usd float64
		if err := rows.Scan(&development, &builds, &usd, &unreadable); err != nil {
			return fmt.Errorf("scanning cost builds of channel %s: %w", channelID, err)
		}
		out.UnreadableBuilds += unreadable
		if i, published := byDevelopment[development]; published {
			out.Videos[i].Builds = builds
			out.Videos[i].ImagesUSD = usd
			continue
		}
		out.OtherBuilds += builds
		out.OtherImagesUSD += usd
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterating cost builds of channel %s: %w", channelID, err)
	}
	return nil
}

func (s *Store) addCostSync(ctx context.Context, out *ChannelCosts) error {
	rows, err := s.pool.Query(ctx, `SELECT source, ok_at FROM cost_sync`)
	if err != nil {
		return fmt.Errorf("reading the costs sync's dates: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var source string
		var at time.Time
		if err := rows.Scan(&source, &at); err != nil {
			return fmt.Errorf("scanning the costs sync's dates: %w", err)
		}
		switch source {
		case CostSourceRuns:
			out.TokensUpdatedAt = &at
		case CostSourceBuilds:
			out.PicturesUpdatedAt = &at
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterating the costs sync's dates: %w", err)
	}
	return nil
}
