package publish

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/tereshed/8bitnews/internal/store"
)

func videoCost(id, title string, tokens, cached int64, stages map[string]int64, builds int, usd float64) store.VideoCost {
	script, _ := json.Marshal(map[string]any{"metadata": map[string]any{"youtube": map[string]string{"title": title}}})
	at := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
	v := store.VideoCost{
		Tokens: store.TokenCounts{Output: tokens - cached, CacheRead: cached}, Stages: stages, AgentRuns: 20, MeasuredRuns: 20, Builds: builds, ImagesUSD: usd,
	}
	v.Release = store.Release{ID: id, ChannelID: "8bitnews-us", Status: "published", Script: script, CreatedAt: at}
	return v
}

func sampleCosts() store.ChannelCosts {
	tokens := time.Date(2026, 11, 5, 10, 0, 0, 0, time.UTC)
	pictures := time.Date(2026, 11, 3, 10, 0, 0, 0, time.UTC)
	return store.ChannelCosts{
		Videos: []store.VideoCost{
			videoCost("77a54dc9-6f0c-4826-9e1b-cf59cc3be8a2", `<script>alert("x")</script> Cook`, 60_000_000, 57_000_000,
				map[string]int64{store.StageResearch: 30_000_000, store.StageScript: 15_000_000, store.StageLegal: 12_000_000, store.StageEditor: 2_900_000, store.StageVideo: 100_000}, 1, 0.8148),
			videoCost("0408ab3b-0000-0000-0000-000000000000", "Canada", 40_000_000, 38_000_000,
				map[string]int64{store.StageResearch: 40_000_000}, 3, 2.25),
		},
		OtherRuns: 2, OtherTokens: store.TokenCounts{CacheRead: 5_500_000}, OtherBuilds: 4, OtherImagesUSD: 3.2,
		AgentRuns: 44, MeasuredRuns: 42, IncompleteRuns: 1, UnreadableBuilds: 2, TokensUpdatedAt: &tokens, PicturesUpdatedAt: &pictures,
	}
}

func TestBuildCostsNumbers(t *testing.T) {
	page := BuildCosts(store.Channel{ID: "8bitnews-us", Name: "8BitNewsUS", Language: "en-US", Timezone: "America/New_York"}, sampleCosts())

	want := []CostTile{{"Videos counted", "2"}, {"Tokens per video, median", "50M"}, {"Pictures per video, median", "$1.53"}, {"Pictures, all videos", "$3.06"}}
	if len(page.Tiles) != len(want) {
		t.Fatalf("tiles %+v", page.Tiles)
	}
	for i := range want {
		if page.Tiles[i] != want[i] {
			t.Errorf("tile %d: %+v, want %+v", i, page.Tiles[i], want[i])
		}
	}
	if page.Total.Tokens != "100M" || page.Total.Written != "5M" || page.Total.Builds != "4" || page.Total.Pictures != "$3.06" {
		t.Errorf("total %+v", page.Total)
	}
	if page.Bars[0].WidthPct != 100 || page.Bars[1].WidthPct < 66 || page.Bars[1].WidthPct > 67 {
		t.Errorf("widths %v %v on a 60M axis", page.Bars[0].WidthPct, page.Bars[1].WidthPct)
	}
	if page.Pictures[1].PictureBar != pictureBarMax {
		t.Errorf("the dearest picture bar is %v wide", page.Pictures[1].PictureBar)
	}
	if !strings.Contains(page.CachedLine, "95%") || !strings.Contains(page.CachedLine, "5%") {
		t.Errorf("cached line %q", page.CachedLine)
	}
	if !strings.Contains(page.OtherRuns, ": 2 (5.5M tokens)") || !strings.Contains(page.OtherBuilds, ": 4 ($3.20 in pictures)") {
		t.Errorf("%q / %q", page.OtherRuns, page.OtherBuilds)
	}
	if !strings.HasPrefix(page.Unmeasured, "2 of 44 ") {
		t.Errorf("unmeasured %q", page.Unmeasured)
	}
	if len(page.Updated) != 2 || !strings.Contains(page.Updated[0], "Nov 5, 2026") || !strings.Contains(page.Updated[1], "Nov 3, 2026") {
		t.Errorf("updated %q: each source keeps its own date", page.Updated)
	}
	if !strings.HasPrefix(page.Incomplete, "Figures are incomplete for 1 ") || !strings.HasPrefix(page.Unreadable, "The packages of 2 ") {
		t.Errorf("%q / %q", page.Incomplete, page.Unreadable)
	}
	// A stage with no tokens draws no mark.
	if n := len(page.Bars[1].Segments); n != 1 {
		t.Errorf("a video with one stage has %d segments", n)
	}
}

func TestNumbers(t *testing.T) {
	for n, want := range map[int64]string{
		950:           "950",
		880_000:       "880\u00a0mil",
		62_400_000:    "62,4\u00a0M",
		1_200_000_000: "1.200\u00a0M",
		999_500:       "1\u00a0M",
	} {
		if got := spanish.compact(n); got != want {
			t.Errorf("%d: %q, want %q", n, got, want)
		}
	}
	if got := spanish.usd(0.8148); got != "0,81\u00a0$" {
		t.Errorf("%q", got)
	}
	if got := spanish.percent(52.4); got != "52\u00a0%" {
		t.Errorf("%q", got)
	}
	for n, want := range map[int64]string{
		950: "950", 880_000: "880K", 62_400_000: "62.4M", 123_456_789: "123M", 1_200_000_000: "1.2B",
		// The unit is chosen after rounding.
		999_500: "1M", 99_950_000: "100M", 999_500_000: "1B",
	} {
		if got := english.compact(n); got != want {
			t.Errorf("%d: %q, want %q", n, got, want)
		}
	}
}

func TestAShareTooSmallToRoundIsNotZero(t *testing.T) {
	if got := english.percent(0.4); got != "<1%" {
		t.Errorf("%q", got)
	}
	if got := spanish.percent(0.4); got != "<1\u00a0%" {
		t.Errorf("%q", got)
	}
	if got := english.percent(0); got != "0%" {
		t.Errorf("%q", got)
	}
}

// An old outlier off the charts doesn't shrink what they draw.
func TestTheAxisFitsWhatIsDrawn(t *testing.T) {
	var c store.ChannelCosts
	for i := 0; i < chartMax; i++ {
		c.Videos = append(c.Videos, videoCost("77a54dc9-6f0c-4826-9e1b-cf59cc3be8a2", "v", 60_000_000, 0, map[string]int64{store.StageResearch: 60_000_000}, 1, 1))
	}
	c.Videos = append(c.Videos, videoCost("77a54dc9-6f0c-4826-9e1b-cf59cc3be8a2", "old", 9_000_000_000, 0, map[string]int64{store.StageResearch: 9_000_000_000}, 1, 49))
	page := BuildCosts(store.Channel{ID: "x", Language: "en-US"}, c)
	if len(page.Bars) != chartMax || page.Bars[0].WidthPct != 100 || page.Pictures[0].PictureBar != pictureBarMax {
		t.Errorf("%d bars, width %v, picture %v", len(page.Bars), page.Bars[0].WidthPct, page.Pictures[0].PictureBar)
	}
	if len(page.Rows) != chartMax+1 {
		t.Errorf("the table has %d rows: it lists them all", len(page.Rows))
	}
}

func TestNiceScale(t *testing.T) {
	for max, want := range map[float64][2]float64{
		60e6:  {60e6, 20e6},
		73e6:  {80e6, 20e6},
		101e6: {125e6, 25e6},
		0:     {1, 1},
		9:     {10, 2},
	} {
		if top, step := niceScale(max); top != want[0] || step != want[1] {
			t.Errorf("%v: top %v step %v, want %v", max, top, step, want)
		}
	}
}

type costsStore struct {
	channel store.Channel
	costs   store.ChannelCosts
}

func (s costsStore) PublicRelease(context.Context, string) (store.Release, error) {
	return store.Release{}, store.ErrNotFound
}
func (s costsStore) PublicReleases(context.Context, string, int) ([]store.Release, error) {
	return nil, nil
}
func (s costsStore) ListReleaseSources(context.Context, string) ([]store.ReleaseSource, error) {
	return nil, nil
}
func (s costsStore) GetChannel(_ context.Context, id string) (store.Channel, error) {
	if id != s.channel.ID {
		return store.Channel{}, store.ErrNotFound
	}
	return s.channel, nil
}
func (s costsStore) GetCountry(context.Context, string) (store.Country, error) {
	return store.Country{}, nil
}
func (s costsStore) ChannelCosts(context.Context, string, int) (store.ChannelCosts, error) {
	return s.costs, nil
}

func TestTheCostsPage(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	us := store.Channel{ID: "8bitnews-us", Name: "8BitNewsUS", Language: "en-US", Timezone: "America/New_York"}
	pages := NewPages(costsStore{us, sampleCosts()}, cfg, logger)

	code, h, body := get(t, pages, "/c/8bitnews-us/costs")
	if code != http.StatusOK || !strings.Contains(h.Get("Content-Security-Policy"), "default-src 'none'") {
		t.Fatalf("%d %v", code, h)
	}
	for _, want := range []string{
		`<html lang="en">`,
		"<title>8BitNewsUS: what each video took</title>",
		`<a href="/c/8bitnews-us">8BitNewsUS</a>`,
		`<a href="/s/77a54dc96f">`,
		"&lt;script&gt;alert(&#34;x&#34;)&lt;/script&gt; Cook",
		`class="seg fill-research"`,
		`title="Research: 30M (50%)"`,
		"Video and QA",
		"Legal risk check",
		"$0.81", "$2.25",
		"Not counted: the subscription itself",
		"builds that failed before reaching storage",
		"Each rebuild draws its pictures again",
		"Figures are incomplete for 1 runs",
		"The packages of 2 builds",
		"Tokens last read in full on Nov 5, 2026.",
		"Pictures last read in full on Nov 3, 2026.",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the page lacks %q", want)
		}
	}
	if strings.Contains(body, "<script>") {
		t.Error("a title got into the page unescaped")
	}
	if strings.Contains(body, "ZgotmplZ") {
		t.Error("the template refused a value: a width or a color came out as ZgotmplZ")
	}
	if strings.Contains(body, "77a54dc9-6f0c") {
		t.Error("a full release id is on the page")
	}

	es := store.Channel{ID: "8bitnews-es", Name: "8BitNewsES", Language: "es-ES", Timezone: "Europe/Madrid"}
	_, _, body = get(t, NewPages(costsStore{es, store.ChannelCosts{OtherRuns: 1, OtherTokens: store.TokenCounts{CacheRead: 9}}}, cfg, logger), "/c/8bitnews-es/costs")
	for _, want := range []string{`<html lang="es">`, "Qué consume cada vídeo", "Todavía no hay vídeos en esta lista.", "fuera de los totales: 1 (9 tokens)"} {
		if !strings.Contains(body, want) {
			t.Errorf("the empty Spanish page lacks %q:\n%s", want, body)
		}
	}
	for _, unwanted := range []string{"<table>", `class="shares"`, `class="tiles"`} {
		if strings.Contains(body, unwanted) {
			t.Errorf("a page with no videos has %q", unwanted)
		}
	}

	if code, _, _ := get(t, pages, "/c/nope/costs"); code != http.StatusNotFound {
		t.Errorf("an unknown channel: %d", code)
	}
	if _, _, body := get(t, pages, "/c/8bitnews-us"); !strings.Contains(body, `<a href="/c/8bitnews-us/costs">What each video took</a>`) {
		t.Errorf("the channel's list doesn't link to its costs:\n%s", body)
	}
}

// A video whose run wasn't measured says so instead of reading as free.
func TestAVideoWithNoRecordShowsADash(t *testing.T) {
	v := videoCost("77a54dc9-6f0c-4826-9e1b-cf59cc3be8a2", "Cook", 0, 0, map[string]int64{}, 0, 0)
	page := BuildCosts(store.Channel{ID: "x", Language: "en-US"}, store.ChannelCosts{Videos: []store.VideoCost{v}})
	if r := page.Rows[0]; r.Tokens != "—" || r.Pictures != "—" || r.Builds != "—" {
		t.Errorf("%+v", r)
	}
	if page.HasTokens || page.HasImages {
		t.Errorf("charts with nothing to draw: %v %v", page.HasTokens, page.HasImages)
	}
	if !page.NoTokens {
		t.Error("the page doesn't say the token figures are missing")
	}
	if tot := page.Total; tot.Tokens != "—" || tot.Written != "—" || tot.Builds != "—" || tot.Pictures != "—" {
		t.Errorf("a total of nothing reads as free: %+v", tot)
	}
}

// A median that stands on some of the videos says how many.
func TestATileSaysHowManyVideosItCounts(t *testing.T) {
	measured := videoCost("77a54dc9-6f0c-4826-9e1b-cf59cc3be8a2", "A", 60_000_000, 0, map[string]int64{store.StageResearch: 60_000_000}, 1, 1)
	missing := videoCost("0408ab3b-0000-0000-0000-000000000000", "B", 0, 0, map[string]int64{}, 0, 0)
	page := BuildCosts(store.Channel{ID: "x", Language: "en-US"}, store.ChannelCosts{Videos: []store.VideoCost{measured, missing}})
	if got := page.Tiles[1].Label; got != "Tokens per video, median (1 of 2 videos)" {
		t.Errorf("%q", got)
	}
}
