package publish

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/tereshed/8bitnews/internal/store"
)

// chartMax is how many videos the charts draw; the table lists them all.
const chartMax = 30

// A share of the tall bar too narrow for its figure prints none: the figure
// stays in its tooltip and in the table.
const shareLabelMin = 11.0

// The picture chart's longest bar leaves this much of the row for its figure.
const pictureBarMax = 82.0

// CostsPage is a channel's public page of what its videos cost: dollars where
// the money is known per video, tokens where a subscription hides it.
type CostsPage struct {
	Channel    string
	ChannelURL string
	T          pageWords

	Tiles  []CostTile
	Shares []CostShare
	Legend []CostLegend

	Bars      []CostBar // the charts' videos, newest first
	Ticks     []CostTick
	StepPct   float64
	HasTokens bool
	HasImages bool
	Pictures  []CostBar // Bars with pictures

	Rows        []CostRow
	Total       CostRow
	OtherRuns   string
	OtherBuilds string
	CachedLine  string
	Unmeasured  string
	Updated     string
}

// CostTile is one headline number.
type CostTile struct {
	Label string
	Value string
}

// CostShare is one stage's block on the tall bar.
type CostShare struct {
	Key   string
	Grow  float64
	Label string // empty: too narrow to print
	Title string
}

// CostLegend names a stage.
type CostLegend struct {
	Key  string
	Name string
}

// CostSegment is one stage of a video's bar.
type CostSegment struct {
	Key   string
	Grow  float64
	Title string
}

// CostBar is one video on the charts.
type CostBar struct {
	Title      string
	URL        string
	Value      string
	WidthPct   float64
	Segments   []CostSegment
	PictureBar float64
	Pictures   string
	Builds     string
}

// CostTick is a label on the token chart's axis.
type CostTick struct {
	Label   string
	LeftPct float64
}

// CostRow is one line of the table.
type CostRow struct {
	Title    string
	URL      string
	Date     string
	Tokens   string
	Written  string
	Builds   string
	Pictures string
}

// BuildCosts writes a channel's costs page. Nothing internal to the pipeline
// reaches it: no task ids, and only published videos by name.
func BuildCosts(ch store.Channel, c store.ChannelCosts) CostsPage {
	w := wordsFor(ch.Language)
	page := CostsPage{Channel: ch.Name, ChannelURL: "/c/" + ch.ID, T: w}

	var total store.TokenCounts
	stages := map[string]int64{}
	var tokensPerVideo, usdPerVideo []float64
	var totalUSD, maxTokens, maxUSD float64
	totalBuilds := 0
	for _, v := range c.Videos {
		total = addTokens(total, v.Tokens)
		for k, n := range v.Stages {
			stages[k] += n
		}
		if t := v.Tokens.Total(); t > 0 {
			tokensPerVideo = append(tokensPerVideo, float64(t))
			maxTokens = math.Max(maxTokens, float64(t))
		}
		if v.Builds > 0 {
			usdPerVideo = append(usdPerVideo, v.ImagesUSD)
			totalUSD += v.ImagesUSD
			totalBuilds += v.Builds
			maxUSD = math.Max(maxUSD, v.ImagesUSD)
		}
	}
	page.HasTokens = maxTokens > 0
	page.HasImages = maxUSD > 0

	if len(c.Videos) > 0 {
		page.Tiles = []CostTile{{w.CostsTileVideos, fmt.Sprint(len(c.Videos))}}
		if page.HasTokens {
			page.Tiles = append(page.Tiles, CostTile{w.CostsTileTokens, w.compact(int64(median(tokensPerVideo)))})
		}
		if page.HasImages {
			page.Tiles = append(page.Tiles, CostTile{w.CostsTilePictures, w.usd(median(usdPerVideo))}, CostTile{w.CostsTilePicturesAll, w.usd(totalUSD)})
		}
	}

	page.Shares, page.Legend = stageShares(w, stages)

	top, step := niceScale(maxTokens)
	page.StepPct = step / top * 100
	for v := 0.0; v <= top+step/2; v += step {
		page.Ticks = append(page.Ticks, CostTick{Label: w.compact(int64(v)), LeftPct: v / top * 100})
	}

	for i, v := range c.Videos {
		title := videoTitle(v.Release, ch, w)
		url := "/s/" + Code(v.Release.ID)
		row := CostRow{Title: title, URL: url, Date: dateOf(v.Release, ch.Timezone, w), Tokens: "—", Written: "—", Builds: "—", Pictures: "—"}
		bar := CostBar{Title: title, URL: url}
		if t := v.Tokens.Total(); t > 0 {
			row.Tokens, row.Written = w.compact(t), w.compact(v.Tokens.Output)
			bar.Value, bar.WidthPct = w.compact(t), float64(t)/top*100
			bar.Segments = videoSegments(w, v)
		}
		if v.Builds > 0 {
			row.Builds, row.Pictures = fmt.Sprint(v.Builds), w.usd(v.ImagesUSD)
			bar.Pictures = w.usd(v.ImagesUSD)
			bar.Builds = fmt.Sprintf(w.CostsBuildsOf, v.Builds)
			if maxUSD > 0 {
				bar.PictureBar = v.ImagesUSD / maxUSD * pictureBarMax
			}
		}
		page.Rows = append(page.Rows, row)
		if i < chartMax {
			page.Bars = append(page.Bars, bar)
			if v.Builds > 0 {
				page.Pictures = append(page.Pictures, bar)
			}
		}
	}
	page.Total = CostRow{Tokens: w.compact(total.Total()), Written: w.compact(total.Output), Builds: fmt.Sprint(totalBuilds), Pictures: w.usd(totalUSD)}

	if c.OtherRuns > 0 {
		page.OtherRuns = fmt.Sprintf(w.CostsOtherRuns, c.OtherRuns, w.compact(c.OtherTokens.Total()))
	}
	if c.OtherBuilds > 0 {
		page.OtherBuilds = fmt.Sprintf(w.CostsOtherBuilds, c.OtherBuilds, w.usd(c.OtherImagesUSD))
	}
	if t := total.Total(); t > 0 {
		page.CachedLine = fmt.Sprintf(w.CostsCached, w.percent(share(total.CacheRead, t)), w.percent(share(total.Output, t)))
	}
	if c.AgentRuns > c.MeasuredRuns {
		page.Unmeasured = fmt.Sprintf(w.CostsUnmeasured, c.AgentRuns-c.MeasuredRuns, c.AgentRuns)
	}
	if c.UpdatedAt != nil {
		page.Updated = fmt.Sprintf(w.CostsUpdated, w.date(inZone(*c.UpdatedAt, ch.Timezone)))
	}
	return page
}

func addTokens(a, b store.TokenCounts) store.TokenCounts {
	return store.TokenCounts{Input: a.Input + b.Input, Output: a.Output + b.Output, CacheWrite: a.CacheWrite + b.CacheWrite, CacheRead: a.CacheRead + b.CacheRead}
}

func share(part, whole int64) float64 {
	if whole == 0 {
		return 0
	}
	return float64(part) / float64(whole) * 100
}

func inZone(t time.Time, tz string) time.Time {
	if loc, err := time.LoadLocation(tz); err == nil && tz != "" {
		return t.In(loc)
	}
	return t
}

func videoTitle(r store.Release, ch store.Channel, w pageWords) string {
	var script scriptDoc
	_ = json.Unmarshal(r.Script, &script)
	if title, _ := titleOf(script); title != "" {
		return title
	}
	return w.VideoOf + " " + dateOf(r, ch.Timezone, w)
}

func stageShares(w pageWords, stages map[string]int64) ([]CostShare, []CostLegend) {
	var sum int64
	for _, n := range stages {
		sum += n
	}
	if sum == 0 {
		return nil, nil
	}
	var shares []CostShare
	var legend []CostLegend
	for _, key := range store.CostStages {
		n := stages[key]
		if n <= 0 {
			continue
		}
		pct := share(n, sum)
		s := CostShare{Key: key, Grow: pct, Title: fmt.Sprintf("%s: %s (%s)", w.stageName(key), w.compact(n), w.percent(pct))}
		if pct >= shareLabelMin {
			s.Label = w.percent(pct)
		}
		shares = append(shares, s)
		legend = append(legend, CostLegend{Key: key, Name: w.stageName(key)})
	}
	return shares, legend
}

func videoSegments(w pageWords, v store.VideoCost) []CostSegment {
	var sum int64
	for _, n := range v.Stages {
		sum += n
	}
	var out []CostSegment
	for _, key := range store.CostStages {
		if n := v.Stages[key]; n > 0 {
			pct := share(n, sum)
			out = append(out, CostSegment{Key: key, Grow: pct, Title: fmt.Sprintf("%s: %s (%s)", w.stageName(key), w.compact(n), w.percent(pct))})
		}
	}
	return out
}

func median(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sorted := append([]float64(nil), values...)
	sort.Float64s(sorted)
	mid := len(sorted) / 2
	if len(sorted)%2 == 1 {
		return sorted[mid]
	}
	return (sorted[mid-1] + sorted[mid]) / 2
}

// niceScale picks the axis: a round top at or above max, in at most five round steps.
func niceScale(max float64) (top, step float64) {
	if max <= 0 {
		return 1, 1
	}
	rough := max / 5
	mag := math.Pow(10, math.Floor(math.Log10(rough)))
	norm := rough / mag
	switch {
	case norm <= 1:
		step = mag
	case norm <= 2:
		step = 2 * mag
	case norm <= 2.5:
		step = 2.5 * mag
	case norm <= 5:
		step = 5 * mag
	default:
		step = 10 * mag
	}
	return math.Ceil(max/step) * step, step
}

// compact writes a token count: 880K, 62.4M, 1.2B in English; 880 mil, 62,4 M,
// 1.200 M in Spanish, where a billón is something else.
func (w pageWords) compact(n int64) string {
	f := float64(n)
	switch {
	case n < 1000:
		return fmt.Sprint(n)
	case n < 1_000_000:
		return w.scaled(f/1e3, w.thousand)
	case n < 1_000_000_000 || w.billion == "":
		return w.scaled(f/1e6, w.million)
	default:
		return w.scaled(f/1e9, w.billion)
	}
}

func (w pageWords) scaled(v float64, unit string) string {
	var s string
	if v >= 100 || v == math.Trunc(v) {
		s = w.grouped(int64(math.Round(v)))
	} else {
		s = strings.Replace(fmt.Sprintf("%.1f", v), ".", w.decimal, 1)
	}
	return s + unit
}

func (w pageWords) grouped(n int64) string {
	s := fmt.Sprint(n)
	if n < 1000 {
		return s
	}
	var out []string
	for len(s) > 3 {
		out = append([]string{s[len(s)-3:]}, out...)
		s = s[:len(s)-3]
	}
	return strings.Join(append([]string{s}, out...), w.group)
}

// usd writes an amount in dollars: $0.81, or 0,81 $ in Spanish.
func (w pageWords) usd(v float64) string {
	return fmt.Sprintf(w.usdFormat, strings.Replace(fmt.Sprintf("%.2f", v), ".", w.decimal, 1))
}

func (w pageWords) percent(v float64) string {
	return fmt.Sprintf(w.percentFormat, math.Round(v))
}

func (w pageWords) stageName(key string) string {
	switch key {
	case store.StageResearch:
		return w.CostsStageResearch
	case store.StageScript:
		return w.CostsStageScript
	case store.StageLegal:
		return w.CostsStageLegal
	case store.StageEditor:
		return w.CostsStageEditor
	case store.StageVideo:
		return w.CostsStageVideo
	}
	return w.CostsStageOther
}
