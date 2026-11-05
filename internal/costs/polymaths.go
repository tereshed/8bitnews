// Package costs keeps hub's copy of what videos cost, for the channel's public
// costs page: a run's tokens, read from PolyMaths, and a build's pictures, read
// from the build's package. PolyMaths drops usage after 30 days, so hub stores it.
package costs

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/tereshed/8bitnews/internal/store"
)

const (
	requestTimeout = 30 * time.Second
	maxResponse    = 16 << 20
	listPage       = 200
)

// Run is one Newsroom root task.
type Run struct {
	ID        string    `json:"id"`
	TeamID    string    `json:"team_id"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Usage is what one task's agents used, without its subtasks'.
type Usage struct {
	AgentRuns    int
	MeasuredRuns int
	Tokens       store.TokenCounts
	Stages       map[string]int64
}

// Brief is what a run's story_brief says about where the run belongs.
type Brief struct {
	ChannelID     string
	DevelopmentID string // empty: the run registered no development
}

// Client reads PolyMaths' REST API with a read-only key.
type Client struct {
	base, key, project, team string
	http                     *http.Client
}

// NewClient builds a Client. It doesn't follow redirects, so the key goes to
// the configured address and nowhere else.
func NewClient(base, key, project, team string) *Client {
	return &Client{base: strings.TrimRight(base, "/"), key: key, project: project, team: team, http: &http.Client{
		Timeout:       requestTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}
}

func (c *Client) get(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+path, nil)
	if err != nil {
		return fmt.Errorf("building a PolyMaths request: %w", err)
	}
	req.Header.Set("X-API-Key", c.key)
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("PolyMaths %s: %w", path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("PolyMaths %s: status %d", path, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponse))
	if err != nil {
		return fmt.Errorf("reading PolyMaths %s: %w", path, err)
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("decoding PolyMaths %s: %w", path, err)
	}
	return nil
}

// Runs returns the Newsroom team's root tasks.
func (c *Client) Runs(ctx context.Context) ([]Run, error) {
	var out []Run
	for offset := 0; ; offset += listPage {
		var page struct {
			Tasks []Run `json:"tasks"`
			Total int   `json:"total"`
		}
		q := url.Values{"root_only": {"true"}, "limit": {strconv.Itoa(listPage)}, "offset": {strconv.Itoa(offset)},
			"order_by": {"created_at"}, "order_dir": {"desc"}}
		if err := c.get(ctx, "/projects/"+url.PathEscape(c.project)+"/tasks?"+q.Encode(), &page); err != nil {
			return nil, err
		}
		for _, t := range page.Tasks {
			if t.TeamID == c.team {
				out = append(out, t)
			}
		}
		if len(page.Tasks) == 0 || offset+len(page.Tasks) >= page.Total {
			return out, nil
		}
	}
}

// Subtasks returns the ids of a task's direct subtasks.
func (c *Client) Subtasks(ctx context.Context, taskID string) ([]string, error) {
	var t struct {
		SubTasks []struct {
			ID string `json:"id"`
		} `json:"sub_tasks"`
	}
	if err := c.get(ctx, "/tasks/"+url.PathEscape(taskID), &t); err != nil {
		return nil, err
	}
	var ids []string
	for _, s := range t.SubTasks {
		ids = append(ids, s.ID)
	}
	return ids, nil
}

type usageCounters struct {
	Runs         int   `json:"runs"`
	MeasuredRuns int   `json:"measured_runs"`
	Input        int64 `json:"input_tokens"`
	Output       int64 `json:"output_tokens"`
	CacheWrite   int64 `json:"cache_creation_tokens"`
	CacheRead    int64 `json:"cache_read_tokens"`
}

func (u usageCounters) tokens() store.TokenCounts {
	return store.TokenCounts{Input: u.Input, Output: u.Output, CacheWrite: u.CacheWrite, CacheRead: u.CacheRead}
}

// Usage returns a task's own usage since from: its subtasks report theirs.
func (c *Client) Usage(ctx context.Context, taskID string, from time.Time) (Usage, error) {
	var report struct {
		Totals  usageCounters `json:"totals"`
		ByAgent []struct {
			Key string `json:"key"`
			usageCounters
		} `json:"by_agent"`
	}
	q := url.Values{"task_id": {taskID}, "from": {from.UTC().Format("2006-01-02")}}
	if err := c.get(ctx, "/projects/"+url.PathEscape(c.project)+"/token-usage?"+q.Encode(), &report); err != nil {
		return Usage{}, err
	}
	out := Usage{AgentRuns: report.Totals.Runs, MeasuredRuns: report.Totals.MeasuredRuns, Tokens: report.Totals.tokens(), Stages: map[string]int64{}}
	for _, a := range report.ByAgent {
		out.Stages[stageOf(a.Key)] += a.tokens().Total()
	}
	return out, nil
}

var agentStages = map[string]string{
	"fact_researcher":     store.StageResearch,
	"local_press_analyst": store.StageResearch,
	"world_press_analyst": store.StageResearch,
	"analyst":             store.StageResearch,
	"fact_checker":        store.StageResearch,
	"scriptwriter":        store.StageScript,
	"script_editor":       store.StageScript,
	"legal_counsel":       store.StageLegal,
	"platform_reviewer":   store.StageLegal,
	"legal_lead":          store.StageLegal,
	"editor":              store.StageEditor,
	"producer":            store.StageEditor,
	"video_producer":      store.StageVideo,
	"qa_validator":        store.StageVideo,
}

// stageOf is the stage an agent works in. An agent added later counts as
// "other" until it's listed: its tokens stay in the total.
func stageOf(agent string) string {
	if s, ok := agentStages[agent]; ok {
		return s
	}
	return store.StageOther
}

var uuidPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// Brief reads a run's latest story_brief; false if the run has none naming a
// channel. An agent writes it, so its ids are checked before anyone trusts them.
func (c *Client) Brief(ctx context.Context, taskID string) (Brief, bool, error) {
	var list struct {
		Items []struct {
			ID   string `json:"id"`
			Kind string `json:"kind"`
		} `json:"items"`
	}
	path := "/tasks/" + url.PathEscape(taskID) + "/artifacts"
	if err := c.get(ctx, path, &list); err != nil {
		return Brief{}, false, err
	}
	for i := len(list.Items) - 1; i >= 0; i-- {
		if list.Items[i].Kind != "story_brief" {
			continue
		}
		var artifact struct {
			Content json.RawMessage `json:"content"`
		}
		if err := c.get(ctx, path+"/"+url.PathEscape(list.Items[i].ID), &artifact); err != nil {
			return Brief{}, false, err
		}
		if b, ok := parseBrief(artifact.Content); ok {
			return b, true, nil
		}
	}
	return Brief{}, false, nil
}

func parseBrief(content json.RawMessage) (Brief, bool) {
	var doc struct {
		ChannelID   string `json:"channel_id"`
		Development struct {
			HubDevelopmentID string `json:"hub_development_id"`
		} `json:"development"`
	}
	if err := json.Unmarshal(content, &doc); err != nil || doc.ChannelID == "" {
		return Brief{}, false
	}
	b := Brief{ChannelID: doc.ChannelID}
	if id := strings.ToLower(doc.Development.HubDevelopmentID); uuidPattern.MatchString(id) {
		b.DevelopmentID = id
	}
	return b, true
}
