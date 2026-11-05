package store

import (
	"context"
	"encoding/json"
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
	SourceUpdatedAt time.Time
}

// CostRunState is what the sync needs to know to decide whether to measure a run again.
type CostRunState struct {
	SourceUpdatedAt time.Time
	MeasuredAt      time.Time
}

// CostRunStates returns every stored run's state, by task id.
func (s *Store) CostRunStates(ctx context.Context) (map[string]CostRunState, error) {
	rows, err := s.pool.Query(ctx, `SELECT task_id, source_updated_at, measured_at FROM cost_runs`)
	if err != nil {
		return nil, fmt.Errorf("listing cost runs: %w", err)
	}
	defer rows.Close()
	out := map[string]CostRunState{}
	for rows.Next() {
		var id string
		var st CostRunState
		if err := rows.Scan(&id, &st.SourceUpdatedAt, &st.MeasuredAt); err != nil {
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
// stored one doesn't replace them, only marks the run as checked: PolyMaths
// forgets a run's usage after 30 days, and a run never uses tokens back.
func (s *Store) SaveCostRun(ctx context.Context, r CostRun) error {
	stages, err := json.Marshal(r.Stages)
	if err != nil {
		return fmt.Errorf("encoding the stages of cost run %s: %w", r.TaskID, err)
	}
	var development *string
	if r.DevelopmentID != "" {
		development = &r.DevelopmentID
	}
	_, err = s.pool.Exec(ctx, `
		INSERT INTO cost_runs (task_id, channel_id, development_id, started_at, agent_runs, measured_runs,
		                       input_tokens, output_tokens, cache_write_tokens, cache_read_tokens,
		                       stages, source_updated_at, measured_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, now())
		ON CONFLICT (task_id) DO UPDATE SET
			development_id = COALESCE(EXCLUDED.development_id, cost_runs.development_id),
			source_updated_at = EXCLUDED.source_updated_at,
			measured_at = now(),
			agent_runs = CASE WHEN `+newUsageIsWhole+` THEN EXCLUDED.agent_runs ELSE cost_runs.agent_runs END,
			measured_runs = CASE WHEN `+newUsageIsWhole+` THEN EXCLUDED.measured_runs ELSE cost_runs.measured_runs END,
			input_tokens = CASE WHEN `+newUsageIsWhole+` THEN EXCLUDED.input_tokens ELSE cost_runs.input_tokens END,
			output_tokens = CASE WHEN `+newUsageIsWhole+` THEN EXCLUDED.output_tokens ELSE cost_runs.output_tokens END,
			cache_write_tokens = CASE WHEN `+newUsageIsWhole+` THEN EXCLUDED.cache_write_tokens ELSE cost_runs.cache_write_tokens END,
			cache_read_tokens = CASE WHEN `+newUsageIsWhole+` THEN EXCLUDED.cache_read_tokens ELSE cost_runs.cache_read_tokens END,
			stages = CASE WHEN `+newUsageIsWhole+` THEN EXCLUDED.stages ELSE cost_runs.stages END
	`, r.TaskID, r.ChannelID, development, r.StartedAt, r.AgentRuns, r.MeasuredRuns,
		r.Tokens.Input, r.Tokens.Output, r.Tokens.CacheWrite, r.Tokens.CacheRead,
		stages, r.SourceUpdatedAt)
	if err != nil {
		return fmt.Errorf("saving cost run %s: %w", r.TaskID, err)
	}
	return nil
}

const newUsageIsWhole = `(EXCLUDED.input_tokens + EXCLUDED.output_tokens + EXCLUDED.cache_write_tokens + EXCLUDED.cache_read_tokens)
	>= (cost_runs.input_tokens + cost_runs.output_tokens + cost_runs.cache_write_tokens + cost_runs.cache_read_tokens)`

// DevelopmentExists reports whether hub knows a development.
func (s *Store) DevelopmentExists(ctx context.Context, id string) (bool, error) {
	var found bool
	err := s.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM story_developments WHERE id = $1)`, id).Scan(&found)
	if err != nil {
		return false, fmt.Errorf("looking for development %s: %w", id, err)
	}
	return found, nil
}

// CostBuildKeys returns the package archives already read.
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

// SaveCostBuild records what one build's pictures cost. A build is read once.
func (s *Store) SaveCostBuild(ctx context.Context, key, developmentID string, usd float64) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO cost_builds (object_key, development_id, images_usd) VALUES ($1, $2, $3)
		ON CONFLICT (object_key) DO NOTHING
	`, key, developmentID, usd)
	if err != nil {
		return fmt.Errorf("saving cost build %s: %w", key, err)
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
	UpdatedAt    *time.Time
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
		       input_tokens, output_tokens, cache_write_tokens, cache_read_tokens, stages, measured_at
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
		var measuredAt time.Time
		if err := rows.Scan(&development, &agentRuns, &measuredRuns, &tokens.Input, &tokens.Output,
			&tokens.CacheWrite, &tokens.CacheRead, &stagesJSON, &measuredAt); err != nil {
			return fmt.Errorf("scanning a cost run of channel %s: %w", channelID, err)
		}
		out.AgentRuns += agentRuns
		out.MeasuredRuns += measuredRuns
		out.noteUpdate(measuredAt)
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
		SELECT b.development_id::text, count(*), sum(b.images_usd)::float8, max(b.read_at)
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
		var builds int
		var usd float64
		var readAt time.Time
		if err := rows.Scan(&development, &builds, &usd, &readAt); err != nil {
			return fmt.Errorf("scanning cost builds of channel %s: %w", channelID, err)
		}
		out.noteUpdate(readAt)
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

func (c *ChannelCosts) noteUpdate(t time.Time) {
	if c.UpdatedAt == nil || t.After(*c.UpdatedAt) {
		c.UpdatedAt = &t
	}
}
