package publish

import (
	"bytes"
	"context"
	"errors"
	"html/template"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/tereshed/8bitnews/internal/store"
)

// PageStore is what the public pages read (internal/store).
type PageStore interface {
	PublicRelease(ctx context.Context, code string) (store.Release, error)
	PublicReleases(ctx context.Context, channelID string, n int) ([]store.Release, error)
	ListReleaseSources(ctx context.Context, releaseID string) ([]store.ReleaseSource, error)
	GetChannel(ctx context.Context, id string) (store.Channel, error)
	GetCountry(ctx context.Context, cc string) (store.Country, error)
	ChannelCosts(ctx context.Context, channelID string, n int) (store.ChannelCosts, error)
}

// indexMax is how many videos a channel's list shows.
const indexMax = 300

// A page found is kept cacheTTL, so a crawler or a viral video doesn't put
// every view on the database; at most cacheMax pages are kept.
const (
	cacheTTL = time.Minute
	cacheMax = 500
)

type cachedPage struct {
	body []byte
	at   time.Time
}

// Pages serves hub's public pages, with no API key: a video's sources page
// at /s/{code}, from the moderator's approval on; a channel's list of
// videos at /c/{channel_id}, of published releases (the moderator pressed
// «Выложено»); and what those videos cost at /c/{channel_id}/costs.
type Pages struct {
	store PageStore
	cfg   Config
	log   *slog.Logger
	mux   *http.ServeMux

	mu    sync.Mutex
	cache map[string]cachedPage
}

// NewPages builds the public pages.
func NewPages(st PageStore, cfg Config, log *slog.Logger) *Pages {
	p := &Pages{store: st, cfg: cfg, log: log, mux: http.NewServeMux(), cache: map[string]cachedPage{}}
	p.mux.HandleFunc("GET /s/{code}", p.sourcesPage)
	p.mux.HandleFunc("GET /c/{channel_id}", p.channelPage)
	p.mux.HandleFunc("GET /c/{channel_id}/costs", p.costsPage)
	return p
}

func (p *Pages) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p.mu.Lock()
	c, ok := p.cache[r.URL.Path]
	p.mu.Unlock()
	if ok && time.Since(c.at) < cacheTTL {
		write(w, c.body)
		return
	}
	p.mux.ServeHTTP(w, r)
}

func (p *Pages) sourcesPage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	rel, err := p.store.PublicRelease(ctx, r.PathValue("code"))
	if err != nil {
		p.fail(w, err)
		return
	}
	sources, err := p.store.ListReleaseSources(ctx, rel.ID)
	if err != nil {
		p.fail(w, err)
		return
	}
	ch, err := p.store.GetChannel(ctx, rel.ChannelID)
	if err != nil {
		p.fail(w, err)
		return
	}
	country, err := p.store.GetCountry(ctx, ch.CountryCC)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		p.fail(w, err)
		return
	}
	page := BuildPage(Input{Release: rel, Sources: sources, Channel: ch, Country: country, Config: p.cfg})
	p.render(w, r, sourcesTemplate, page)
}

func (p *Pages) channelPage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	ch, err := p.store.GetChannel(ctx, r.PathValue("channel_id"))
	if err != nil {
		p.fail(w, err)
		return
	}
	releases, err := p.store.PublicReleases(ctx, ch.ID, indexMax)
	if err != nil {
		p.fail(w, err)
		return
	}
	p.render(w, r, indexTemplate, BuildIndex(ch, releases, p.cfg))
}

func (p *Pages) costsPage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	ch, err := p.store.GetChannel(ctx, r.PathValue("channel_id"))
	if err != nil {
		p.fail(w, err)
		return
	}
	costs, err := p.store.ChannelCosts(ctx, ch.ID, indexMax)
	if err != nil {
		p.fail(w, err)
		return
	}
	p.render(w, r, costsTemplate, BuildCosts(ch, costs))
}

func (p *Pages) fail(w http.ResponseWriter, err error) {
	if errors.Is(err, store.ErrNotFound) {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}
	p.log.Error("public page failed", "error", err)
	http.Error(w, "Something went wrong", http.StatusInternalServerError)
}

func (p *Pages) render(w http.ResponseWriter, r *http.Request, t *template.Template, data any) {
	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		p.log.Error("rendering a public page failed", "error", err)
		http.Error(w, "Something went wrong", http.StatusInternalServerError)
		return
	}
	p.mu.Lock()
	if len(p.cache) >= cacheMax {
		p.cache = map[string]cachedPage{}
	}
	p.cache[r.URL.Path] = cachedPage{body: buf.Bytes(), at: time.Now()}
	p.mu.Unlock()
	write(w, buf.Bytes())
}

func write(w http.ResponseWriter, body []byte) {
	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	// The page only shows text and links: nothing to run, load or frame.
	h.Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'")
	h.Set("X-Content-Type-Options", "nosniff")
	// Short: archive copies land in the page minutes after release.
	h.Set("Cache-Control", "public, max-age=300")
	_, _ = w.Write(body)
}

const pageHead = `<!doctype html>
<html lang="{{.T.Lang}}">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<style>
:root { --bg: #fbfaf7; --fg: #1d1d1f; --muted: #5f6368; --rule: #e3e0d8; --accent: #1f5fbf; --quote: #f1efe9; }
@media (prefers-color-scheme: dark) {
  :root { --bg: #15161a; --fg: #ececec; --muted: #a0a4ab; --rule: #2c2e34; --accent: #8ab4f8; --quote: #1e2026; }
}
* { box-sizing: border-box; }
body { margin: 0; background: var(--bg); color: var(--fg); font: 17px/1.55 -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, Helvetica, Arial, sans-serif; }
main { max-width: 720px; margin: 0 auto; padding: 24px 16px 48px; }
.brand { font: 600 14px/1 ui-monospace, "SFMono-Regular", Menlo, Consolas, monospace; letter-spacing: .08em; text-transform: uppercase; color: var(--muted); }
.brand a { color: inherit; text-decoration: none; }
h1 { font-size: 26px; line-height: 1.25; margin: 10px 0 6px; }
h2 { font-size: 19px; margin: 36px 0 10px; padding-top: 18px; border-top: 1px solid var(--rule); }
.date { color: var(--muted); font-size: 15px; margin: 0 0 12px; }
.summary { margin: 0 0 8px; }
a { color: var(--accent); overflow-wrap: anywhere; }
blockquote { margin: 22px 0 8px; padding: 10px 14px; background: var(--quote); border-left: 3px solid var(--rule); border-radius: 4px; }
ul { margin: 0; padding-left: 20px; }
li { margin: 6px 0; }
.meta { color: var(--muted); font-size: 15px; }
.mark { color: var(--muted); font-size: 14px; }
p.small { color: var(--muted); font-size: 15px; }
</style>
`

var sourcesTemplate = template.Must(template.New("sources").Parse(pageHead + `<title>{{.T.Sources}}: {{.Title}} | {{.Channel}}</title>
</head>
<body>
<main>
<div class="brand">{{if .ChannelURL}}<a href="{{.ChannelURL}}">{{.Channel}}</a>{{else}}{{.Channel}}{{end}} · {{.T.Sources}}</div>
<h1>{{.Title}}</h1>
<p class="date">{{.Date}}</p>
{{if .Summary}}<p class="summary">{{.Summary}}</p>{{end}}

<h2>{{.T.Says}}</h2>
{{range .Statements}}
<blockquote>{{.Text}}</blockquote>
<ul>
{{range .Sources}}<li><a href="{{.Link}}">{{.Outlet}}{{if .Title}}: {{.Title}}{{end}}</a>{{with .Marks}}<span class="mark">{{.}}</span>{{end}}{{if .Date}} <span class="meta">· {{.Date}}</span>{{end}}{{if .ArchiveURL}}{{if .Gone}} <span class="meta">· {{$.T.ArchivedGone}}</span>{{else}} <span class="meta">· <a href="{{.ArchiveURL}}">{{$.T.Archived}}</a></span>{{end}}{{end}}</li>
{{end}}
</ul>
{{else}}{{if not .Others}}
<p>{{.T.NoSources}}</p>
{{end}}{{end}}
{{with .Others}}
<h2>{{$.T.Others}}</h2>
<ul>
{{range .}}<li><a href="{{.Link}}">{{.Outlet}}{{if .Title}}: {{.Title}}{{end}}</a>{{with .Marks}}<span class="mark">{{.}}</span>{{end}}{{if .Date}} <span class="meta">· {{.Date}}</span>{{end}}{{if .ArchiveURL}}{{if .Gone}} <span class="meta">· {{$.T.ArchivedGone}}</span>{{else}} <span class="meta">· <a href="{{.ArchiveURL}}">{{$.T.Archived}}</a></span>{{end}}{{end}}</li>
{{end}}
</ul>
{{end}}

<h2>{{.T.How}}</h2>
<p>{{.T.HowDossier}}</p>
<p>{{.T.HowAI}}</p>

<h2>{{.T.Corrections}}</h2>
<p>{{.T.NoCorrections}}{{if .Corrections}} {{.T.WriteTo}} {{.Corrections}}.{{end}}</p>
</main>
</body>
</html>
`))

var indexTemplate = template.Must(template.New("index").Parse(pageHead + `<title>{{.Channel}}: {{.T.IndexTitleLower}}</title>
</head>
<body>
<main>
<div class="brand">{{.Channel}}</div>
<h1>{{.T.IndexTitle}}</h1>
<p class="small">{{.T.IndexLead}} <a href="{{.CostsURL}}">{{.T.CostsLink}}</a>.</p>
<ul>
{{range .Entries}}<li><a href="{{.URL}}">{{.Title}}</a> <span class="meta">· {{.Date}}</span></li>
{{else}}<li>{{.T.NoVideos}}</li>
{{end}}
</ul>
{{if .Corrections}}<h2>{{.T.Corrections}}</h2>
<p>{{.T.WriteTo}} {{.Corrections}}.</p>{{end}}
</main>
</body>
</html>
`))

// The stages' colors are the validated categorical slots of the dataviz method,
// in its fixed order, stepped for each mode: checked against this page's own
// light and dark backgrounds. Three light slots are under 3:1 there, which is
// why every figure is also in the table. A figure on a block takes the ink with
// the better contrast, measured for each slot in both modes. Dollars get the violet slot: pictures
// aren't one of the stages, and a color follows what it stands for. There is no
// script on these pages (see write), so a mark's tooltip is its title attribute.
const costsStyle = `<style>
:root { --s-research: #2a78d6; --s-script: #eb6834; --s-legal: #1baf7a; --s-editor: #eda100; --s-video: #e87ba4; --s-other: #008300; --pictures: #6250d6; }
@media (prefers-color-scheme: dark) {
  :root { --s-research: #3987e5; --s-script: #d95926; --s-legal: #199e70; --s-editor: #c98500; --s-video: #d55181; --s-other: #008300; --pictures: #9085e9; }
}
.fill-research { background: var(--s-research); } .fill-script { background: var(--s-script); } .fill-legal { background: var(--s-legal); }
.fill-editor { background: var(--s-editor); } .fill-video { background: var(--s-video); } .fill-other { background: var(--s-other); }
.lead-sub { color: var(--muted); font-size: 15px; margin: 0 0 12px; }
.tiles { display: grid; grid-template-columns: repeat(auto-fit, minmax(150px, 1fr)); gap: 12px; margin: 20px 0 0; }
.tile { border: 1px solid var(--rule); border-radius: 8px; padding: 12px 14px; }
.tile b { display: block; font-size: 26px; line-height: 1.2; font-weight: 600; }
.tile span { color: var(--muted); font-size: 14px; line-height: 1.3; display: block; margin-top: 2px; }
.shares { display: flex; gap: 2px; height: 28px; }
.share { min-width: 2px; border-radius: 4px; color: #0b0b0b; font-size: 13px; line-height: 28px; padding-left: 6px; white-space: nowrap; }
.share.fill-other { color: #fff; }
.share.empty { padding-left: 0; }
.legend { display: flex; flex-wrap: wrap; gap: 4px 14px; margin: 10px 0 0; padding: 0; list-style: none; font-size: 14px; color: var(--muted); }
.legend li { margin: 0; display: flex; align-items: center; gap: 6px; }
.swatch { width: 10px; height: 10px; border-radius: 2px; flex: none; }
.chart { margin: 14px 0 0; }
.row { margin: 0 0 12px; }
.row-head { display: flex; justify-content: space-between; gap: 12px; font-size: 14px; line-height: 1.3; margin-bottom: 4px; }
.row-head a { min-width: 0; }
.row-head .num { flex: none; color: var(--fg); font-weight: 600; font-variant-numeric: tabular-nums; }
.track { border-right: 1px solid var(--rule); background-image: linear-gradient(to right, var(--rule) 1px, transparent 1px); }
.bar { display: flex; gap: 2px; height: 16px; border-radius: 0 4px 4px 0; overflow: hidden; }
.seg { min-width: 2px; }
.nodata { color: var(--muted); font-size: 14px; }
.axis { position: relative; height: 18px; margin-top: -6px; color: var(--muted); font-size: 12px; font-variant-numeric: tabular-nums; }
.tick { position: absolute; transform: translateX(-50%); }
.tick:first-child { transform: none; }
.tick:last-child { transform: translateX(-100%); }
.pic { display: flex; align-items: center; gap: 6px; }
.pic .bar { background: var(--pictures); flex: none; }
.pic .num { font-size: 14px; font-weight: 600; font-variant-numeric: tabular-nums; }
.row-head .builds { flex: none; color: var(--muted); }
.scroll { overflow-x: auto; }
table { border-collapse: collapse; width: 100%; font-size: 14px; }
th, td { text-align: right; padding: 7px 0 7px 10px; border-bottom: 1px solid var(--rule); font-variant-numeric: tabular-nums; white-space: nowrap; vertical-align: top; }
th { color: var(--muted); font-weight: 500; }
th:first-child, td:first-child { text-align: left; padding-left: 0; white-space: normal; min-width: 120px; font-variant-numeric: normal; }
tfoot td { font-weight: 600; border-bottom: 0; }
td .meta { display: block; font-size: 13px; }
</style>
`

var costsTemplate = template.Must(template.New("costs").Parse(pageHead + costsStyle + `<title>{{.Channel}}: {{.T.CostsTitleLower}}</title>
</head>
<body>
<main>
<div class="brand"><a href="{{.ChannelURL}}">{{.Channel}}</a></div>
<h1>{{.T.CostsTitle}}</h1>
<p class="small">{{.T.CostsLead}}</p>
{{with .Tiles}}<div class="tiles">
{{range .}}<div class="tile"><b>{{.Value}}</b><span>{{.Label}}</span></div>
{{end}}</div>
{{end}}
{{if not .Rows}}<p>{{.T.CostsNoVideos}}</p>{{end}}
{{with .Shares}}
<h2>{{$.T.CostsSharesTitle}}</h2>
<p class="lead-sub">{{$.T.CostsSharesLead}}</p>
<div class="shares">
{{range .}}<div class="share fill-{{.Key}}{{if not .Label}} empty{{end}}" style="flex: {{.Grow}} 1 0" title="{{.Title}}">{{.Label}}</div>
{{end}}</div>
<ul class="legend">
{{range $.Legend}}<li><span class="swatch fill-{{.Key}}"></span>{{.Name}}</li>
{{end}}</ul>
{{end}}
{{if .HasTokens}}
<h2>{{.T.CostsTokensTitle}}</h2>
<p class="lead-sub">{{.T.CostsTokensLead}}</p>
<ul class="legend">
{{range .Legend}}<li><span class="swatch fill-{{.Key}}"></span>{{.Name}}</li>
{{end}}</ul>
<div class="chart">
{{range .Bars}}<div class="row">
<div class="row-head"><a href="{{.URL}}">{{.Title}}</a>{{if .Value}}<span class="num">{{.Value}}</span>{{end}}</div>
{{if .Segments}}<div class="track" style="background-size: {{$.StepPct}}% 100%"><div class="bar" style="width: {{.WidthPct}}%">
{{range .Segments}}<div class="seg fill-{{.Key}}" style="flex: {{.Grow}} 1 0" title="{{.Title}}"></div>
{{end}}</div></div>{{else}}<div class="nodata">—</div>{{end}}
</div>
{{end}}<div class="axis">{{range .Ticks}}<span class="tick" style="left: {{.LeftPct}}%">{{.Label}}</span>{{end}}</div>
</div>
{{end}}
{{if .HasImages}}
<h2>{{.T.CostsPicturesTitle}}</h2>
<p class="lead-sub">{{.T.CostsPicturesLead}}</p>
<div class="chart">
{{range .Pictures}}<div class="row">
<div class="row-head"><a href="{{.URL}}">{{.Title}}</a><span class="builds">{{.Builds}}</span></div>
<div class="pic"><div class="bar" style="width: {{.PictureBar}}%" title="{{.Pictures}}"></div><span class="num">{{.Pictures}}</span></div>
</div>
{{end}}</div>
{{end}}
{{if .Rows}}
<h2>{{.T.CostsTableTitle}}</h2>
<div class="scroll"><table>
<thead><tr><th>{{.T.CostsColVideo}}</th><th>{{.T.CostsColTokens}}</th><th>{{.T.CostsColWritten}}</th><th>{{.T.CostsColBuilds}}</th><th>{{.T.CostsColPictures}}</th></tr></thead>
<tbody>
{{range .Rows}}<tr><td><a href="{{.URL}}">{{.Title}}</a><span class="meta">{{.Date}}</span></td><td>{{.Tokens}}</td><td>{{.Written}}</td><td>{{.Builds}}</td><td>{{.Pictures}}</td></tr>
{{end}}</tbody>
<tfoot><tr><td>{{.T.CostsColTotal}}</td><td>{{.Total.Tokens}}</td><td>{{.Total.Written}}</td><td>{{.Total.Builds}}</td><td>{{.Total.Pictures}}</td></tr></tfoot>
</table></div>
{{end}}
{{if .OtherRuns}}<p class="small">{{.OtherRuns}}</p>{{end}}
{{if .OtherBuilds}}<p class="small">{{.OtherBuilds}}</p>{{end}}

<h2>{{.T.CostsMethodTitle}}</h2>
<p>{{.T.CostsMethodTokens}}{{if .CachedLine}} {{.CachedLine}}{{end}}</p>
{{if .NoTokens}}<p>{{.T.CostsNoTokens}}</p>{{end}}
{{if .Unmeasured}}<p>{{.Unmeasured}}</p>{{end}}
{{if .Incomplete}}<p>{{.Incomplete}}</p>{{end}}
<p>{{.T.CostsMethodPictures}}</p>
{{if .Unreadable}}<p>{{.Unreadable}}</p>{{end}}
<p>{{.T.CostsMethodFree}}</p>
<p>{{.T.CostsMethodExcluded}}</p>
{{range .Updated}}<p class="small">{{.}}</p>
{{end}}</main>
</body>
</html>
`))
