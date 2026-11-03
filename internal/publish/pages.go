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
// at /s/{code}, from the moderator's approval on, and a channel's list of
// videos at /c/{channel_id}, of published releases (the moderator pressed «Выложено»).
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
<html lang="en">
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

var sourcesTemplate = template.Must(template.New("sources").Parse(pageHead + `<title>Sources: {{.Title}} | {{.Channel}}</title>
</head>
<body>
<main>
<div class="brand">{{if .ChannelURL}}<a href="{{.ChannelURL}}">{{.Channel}}</a>{{else}}{{.Channel}}{{end}} · Sources</div>
<h1>{{.Title}}</h1>
<p class="date">{{.Date}}</p>
{{if .Summary}}<p class="summary">{{.Summary}}</p>{{end}}

<h2>What the video says, and where it comes from</h2>
{{range .Statements}}
<blockquote>{{.Text}}</blockquote>
<ul>
{{range .Sources}}<li><a href="{{.Link}}">{{.Outlet}}{{if .Title}}: {{.Title}}{{end}}</a>{{with .Marks}}<span class="mark">{{.}}</span>{{end}}{{if .Date}} <span class="meta">· {{.Date}}</span>{{end}}{{if .ArchiveURL}}{{if .Gone}} <span class="meta">· archived copy; the original page is gone</span>{{else}} <span class="meta">· <a href="{{.ArchiveURL}}">archived copy</a></span>{{end}}{{end}}</li>
{{end}}
</ul>
{{else}}{{if not .Others}}
<p>No sources are listed for this video.</p>
{{end}}{{end}}
{{with .Others}}
<h2>Other sources</h2>
<ul>
{{range .}}<li><a href="{{.Link}}">{{.Outlet}}{{if .Title}}: {{.Title}}{{end}}</a>{{with .Marks}}<span class="mark">{{.}}</span>{{end}}{{if .Date}} <span class="meta">· {{.Date}}</span>{{end}}{{if .ArchiveURL}}{{if .Gone}} <span class="meta">· archived copy; the original page is gone</span>{{else}} <span class="meta">· <a href="{{.ArchiveURL}}">archived copy</a></span>{{end}}{{end}}</li>
{{end}}
</ul>
{{end}}

<h2>How we make these videos</h2>
<p>Every video starts from a research dossier: what is new, what is already known, how positions split at home and how the press abroad covers the story. Each line of the video is traced to the sources above, primary sources first. When a video is released, we check every source and save a copy to the Internet Archive's Wayback Machine where it lets us; the archived copy is linked next to the source, and a source whose page was gone when we checked it links to its archived copy.</p>
<p>The narration is a synthetic voice and the pictures are AI-generated pixel art; real people are shown as stylized illustrations, not photographs. A human editor approves every video before it is published.</p>

<h2>Corrections</h2>
<p>No corrections to this video so far.{{if .Corrections}} Spotted an error? Write to {{.Corrections}}.{{end}}</p>
</main>
</body>
</html>
`))

var indexTemplate = template.Must(template.New("index").Parse(pageHead + `<title>{{.Channel}}: videos and their sources</title>
</head>
<body>
<main>
<div class="brand">{{.Channel}}</div>
<h1>Videos and their sources</h1>
<p class="small">Each video has a page with every source behind it, with archived copies where the Wayback Machine has them.</p>
<ul>
{{range .Entries}}<li><a href="{{.URL}}">{{.Title}}</a> <span class="meta">· {{.Date}}</span></li>
{{else}}<li>No videos yet.</li>
{{end}}
</ul>
{{if .Corrections}}<h2>Corrections</h2>
<p>Spotted an error? Write to {{.Corrections}}.</p>{{end}}
</main>
</body>
</html>
`))
