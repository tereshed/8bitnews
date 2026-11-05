package publish

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/tereshed/8bitnews/internal/store"
)

func TestTheTrialsSourcesPage(t *testing.T) {
	in := trial(t)
	for i, s := range in.Sources {
		if strings.Contains(s.URL, "foxnews.com") {
			in.Sources[i].CheckStatus = "dead"
		}
	}
	// A source hub never checked doesn't appear, even if the dossier has it.
	in.Sources = in.Sources[1:]
	dropped := trial(t).Sources[0].URL

	page := BuildPage(in)
	if page.Title != "Grand Jury Won't Indict Cassidy Hutchinson Over Jan. 6 Testimony" || page.Date != "Nov 1, 2026" ||
		page.Channel != "8BitNewsUS" || page.ChannelURL != "https://hub.example.org/c/8bitnews-us" {
		t.Errorf("page: %+v", page)
	}
	// Ten scenes cite something; the outro (S11) cites nothing.
	if len(page.Statements) != 10 {
		t.Fatalf("%d statements", len(page.Statements))
	}
	if !strings.HasPrefix(page.Statements[0].Text, "A grand jury just refused to indict") || len(page.Statements[0].Sources) == 0 {
		t.Errorf("first statement: %+v", page.Statements[0])
	}
	sawGone := false
	for _, st := range page.Statements {
		for _, s := range st.Sources {
			if s.URL == dropped {
				t.Errorf("an unchecked source is listed: %s", s.URL)
			}
			if strings.Contains(s.URL, "foxnews.com") {
				sawGone = true
				if s.Link() != s.ArchiveURL {
					t.Errorf("a dead page links %s, not its archive copy", s.Link())
				}
			}
		}
	}
	if !sawGone {
		t.Error("the Fox News source is missing")
	}
	// The world press scene (S9) lists the clusters' outlets, marked.
	var world string
	for _, st := range page.Statements {
		if strings.HasPrefix(st.Text, "World outlets") {
			for _, s := range st.Sources {
				world += s.Outlet + s.Marks() + "|"
			}
		}
	}
	if !strings.Contains(world, "Al Jazeera (state media)|") || strings.Contains(world, "(state media) (state media)") {
		t.Errorf("world sources: %s", world)
	}
	if len(page.Others) != 0 {
		t.Errorf("every source of the trial is cited by a scene, yet others: %v", page.Others)
	}
}

func TestSourcesNoSceneCitesAreListedToo(t *testing.T) {
	in := trial(t)
	in.Sources = append(in.Sources, store.ReleaseSource{URL: "https://www.justice.gov/opa/statement", Outlet: "DOJ", CheckStatus: "ok"})
	page := BuildPage(in)
	if len(page.Others) != 1 || page.Others[0].Outlet != "DOJ" {
		t.Fatalf("others: %+v", page.Others)
	}
}

// pageStore serves one release, its page public once approved and listed once published, like the store.
type pageStore struct {
	in    Input
	reads *int
}

func (s pageStore) PublicRelease(_ context.Context, code string) (store.Release, error) {
	if s.reads != nil {
		*s.reads++
	}
	if code != Code(s.in.Release.ID) || (s.in.Release.Status != "approved" && s.in.Release.Status != "published") {
		return store.Release{}, store.ErrNotFound
	}
	return s.in.Release, nil
}

func (s pageStore) PublicReleases(_ context.Context, channelID string, _ int) ([]store.Release, error) {
	if channelID != s.in.Channel.ID || s.in.Release.Status != "published" {
		return nil, nil
	}
	return []store.Release{s.in.Release}, nil
}

func (s pageStore) ListReleaseSources(context.Context, string) ([]store.ReleaseSource, error) {
	return s.in.Sources, nil
}

func (s pageStore) GetChannel(_ context.Context, id string) (store.Channel, error) {
	if id != s.in.Channel.ID {
		return store.Channel{}, store.ErrNotFound
	}
	return s.in.Channel, nil
}

func (s pageStore) GetCountry(context.Context, string) (store.Country, error) {
	return s.in.Country, nil
}

func (s pageStore) ChannelCosts(context.Context, string, int) (store.ChannelCosts, error) {
	return store.ChannelCosts{}, nil
}

func get(t *testing.T, h http.Handler, path string) (int, http.Header, string) {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec.Code, rec.Header(), rec.Body.String()
}

func TestThePublicPages(t *testing.T) {
	in := trial(t)
	in.Release.Status = "published"
	for i, s := range in.Sources {
		if strings.Contains(s.URL, "cbsnews.com") {
			in.Sources[i].Title = `<script>alert("x")</script>`
		}
	}
	reads := 0
	pages := NewPages(pageStore{in, &reads}, cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))

	code, h, body := get(t, pages, "/s/f874ebb1a0")
	if code != http.StatusOK || !strings.HasPrefix(h.Get("Content-Type"), "text/html") ||
		!strings.Contains(h.Get("Content-Security-Policy"), "default-src 'none'") {
		t.Fatalf("%d %v", code, h)
	}
	for _, want := range []string{
		"<title>Sources: Grand Jury Won&#39;t Indict Cassidy Hutchinson Over Jan. 6 Testimony | 8BitNewsUS</title>",
		`<a href="https://hub.example.org/c/8bitnews-us">8BitNewsUS</a>`,
		"Nov 1, 2026",
		`href="https://www.nbcnews.com/politics/justice-department/grand-jury-declines-indict-former-trump-aide-cassidy-hutchinson-jan-6-rcna603233"`,
		`<a href="https://web.archive.org/web/2026/https://www.nbcnews.com/`,
		"Al Jazeera (state media)",
		"&lt;script&gt;alert(&#34;x&#34;)&lt;/script&gt;",
		"Write to corrections@example.org.",
		"AI-generated pixel art",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the page lacks %q", want)
		}
	}
	if strings.Contains(body, "<script>") {
		t.Error("a source title got into the page unescaped")
	}
	if _, _, again := get(t, pages, "/s/f874ebb1a0"); again != body || reads != 1 {
		t.Errorf("a second view read the store again (%d reads)", reads)
	}

	code, _, body = get(t, pages, "/c/8bitnews-us")
	if code != http.StatusOK || !strings.Contains(body, `<a href="/s/f874ebb1a0">Grand Jury Won&#39;t Indict`) {
		t.Errorf("channel page %d:\n%s", code, body)
	}

	for _, path := range []string{"/s/0000000000", "/s/f874ebb1a", "/c/nope"} {
		if code, _, _ := get(t, pages, path); code != http.StatusNotFound {
			t.Errorf("%s: %d", path, code)
		}
	}
	for _, status := range []string{"pending_approval", "approved", "rejected"} {
		in.Release.Status = status
		pages = NewPages(pageStore{in, nil}, cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
		want := http.StatusNotFound
		if status == "approved" {
			want = http.StatusOK
		}
		if code, _, _ := get(t, pages, "/s/f874ebb1a0"); code != want {
			t.Errorf("the page of a release %s: %d, want %d", status, code, want)
		}
		if _, _, body := get(t, pages, "/c/8bitnews-us"); !strings.Contains(body, "No videos yet.") {
			t.Errorf("a release %s is listed:\n%s", status, body)
		}
	}
}
