package sources

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// newFakeAvailability mimics https://web.archive.org: the CDX API, which
// returns the captures of a page that were recorded with HTTP 200, oldest
// first, and the capture itself at /web/<timestamp>id_/<url>.
//   - /found has one capture of the article;
//   - /challenged has an article, then two captures of a challenge page;
//   - /only-challenged and /only-soft-404 have only a capture of that page;
//   - /other-case has a capture of the same URL in another case;
//   - /flaky has a capture of the article the archive fails to replay;
//   - anything else has none (a page with only a capture of its 404 too: the
//     status filter removes it).
func newFakeAvailability(t *testing.T) *httptest.Server {
	captures := map[string][][2]string{
		"https://example.com/found": {{"20200102000000", "https://example.com/found"}},
		"https://example.com/challenged": {
			{"20200101000000", "https://example.com/challenged"},
			{"20200102000000", "https://example.com/challenged"},
			{"20200103000000", "https://example.com/challenged"},
		},
		"https://example.com/only-challenged": {{"20200103000000", "https://example.com/only-challenged"}},
		"https://example.com/only-soft-404":   {{"20200103000000", "https://example.com/only-soft-404"}},
		"https://example.com/other-case":      {{"20200102000000", "https://example.com/OTHER-case"}},
		"https://example.com/flaky":           {{"20200102000000", "https://example.com/flaky"}},
	}
	article := `<html><head><title>An article</title></head><body><h1>An article</h1></body></html>`
	challenge := `<html><head><title>Just a moment...</title></head><body>Checking your browser</body></html>`
	soft404 := `<html><head><title>Page not found</title></head><body><h1>Page not found</h1></body></html>`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/web/") {
			switch {
			case strings.HasSuffix(r.URL.Path, "flaky"):
				http.Error(w, "replay failed", http.StatusBadGateway)
			case strings.Contains(r.URL.Path, "only-soft-404"):
				_, _ = w.Write([]byte(soft404))
			case strings.Contains(r.URL.Path, "only-challenged"), strings.Contains(r.URL.Path, "20200102000000id_/https://example.com/challenged"),
				strings.Contains(r.URL.Path, "20200103000000id_/https://example.com/challenged"):
				_, _ = w.Write([]byte(challenge))
			default:
				_, _ = w.Write([]byte(article))
			}
			return
		}
		q := r.URL.Query()
		if r.URL.Path != "/cdx/search/cdx" || q.Get("matchType") != "exact" || q.Get("filter") != "statuscode:200" ||
			q.Get("collapse") != "digest" || q.Get("limit") != "-3" || q.Get("fl") != "timestamp,original" || q.Get("output") != "json" {
			http.Error(w, "unexpected request: "+r.URL.String(), http.StatusBadRequest)
			return
		}
		rows := [][]string{{"timestamp", "original"}}
		for _, c := range captures[q.Get("url")] {
			rows = append(rows, []string{c[0], c[1]})
		}
		if len(rows) == 1 {
			rows = nil
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(rows)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestAvailabilityClient_Found200(t *testing.T) {
	srv := newFakeAvailability(t)
	client := NewAvailabilityClient(srv.URL, 5*time.Second, true)

	archiveURL, found, err := client.Closest(context.Background(), "https://example.com/found")
	if err != nil {
		t.Fatalf("Closest: %v", err)
	}
	if !found {
		t.Fatalf("expected a snapshot to be found")
	}
	if want := srv.URL + "/web/20200102000000/https://example.com/found"; archiveURL != want {
		t.Fatalf("archive URL = %q, want %q", archiveURL, want)
	}
}

func TestAvailabilityClient_PageWithOnlyAnErrorCaptureIsNotFound(t *testing.T) {
	srv := newFakeAvailability(t)
	client := NewAvailabilityClient(srv.URL, 5*time.Second, true)

	_, found, err := client.Closest(context.Background(), "https://example.com/found-404")
	if err != nil {
		t.Fatalf("Closest: %v", err)
	}
	if found {
		t.Fatalf("a page with only a capture of its 404 must not count as found")
	}
}

func TestAvailabilityClient_NoSnapshot(t *testing.T) {
	srv := newFakeAvailability(t)
	client := NewAvailabilityClient(srv.URL, 5*time.Second, true)

	_, found, err := client.Closest(context.Background(), "https://example.com/never-archived")
	if err != nil {
		t.Fatalf("Closest: %v", err)
	}
	if found {
		t.Fatalf("expected no snapshot to be found")
	}
}

func TestAvailabilityClient_BlocksPrivateBaseURL(t *testing.T) {
	srv := newFakeAvailability(t)
	client := NewAvailabilityClient(srv.URL, 2*time.Second, false)

	_, _, err := client.Closest(context.Background(), "https://example.com/found")
	if err == nil {
		t.Fatalf("expected Closest against a loopback base URL to fail when allowPrivate=false")
	}
}

func TestAvailabilityClient_EncodesTargetURL(t *testing.T) {
	var gotRawQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotRawQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[]`))
	}))
	t.Cleanup(srv.Close)

	client := NewAvailabilityClient(srv.URL, 5*time.Second, true)
	target := "https://example.com/a?b=c&d=e"
	if _, _, err := client.Closest(context.Background(), target); err != nil {
		t.Fatalf("Closest: %v", err)
	}
	q, err := url.ParseQuery(gotRawQuery)
	if err != nil {
		t.Fatalf("parsing request query: %v", err)
	}
	if q.Get("url") != target {
		t.Fatalf("server saw url=%q, want %q", q.Get("url"), target)
	}
}

func TestAvailabilityClient_ReadsTheCapture(t *testing.T) {
	srv := newFakeAvailability(t)
	client := NewAvailabilityClient(srv.URL, 5*time.Second, true)

	archiveURL, found, err := client.Closest(context.Background(), "https://example.com/challenged")
	if err != nil || !found {
		t.Fatalf("found=%v err=%v", found, err)
	}
	if want := srv.URL + "/web/20200101000000/https://example.com/challenged"; archiveURL != want {
		t.Fatalf("archive URL = %q, want the older capture of the article %q", archiveURL, want)
	}
	for _, target := range []string{"https://example.com/only-challenged", "https://example.com/only-soft-404"} {
		if _, found, err := client.Closest(context.Background(), target); err != nil || found {
			t.Errorf("%s: found=%v err=%v; a challenge page or soft 404 recorded with 200 isn't a copy", target, found, err)
		}
	}
}

func TestAvailabilityClient_CaptureOfAnotherPageIsNotACopy(t *testing.T) {
	srv := newFakeAvailability(t)
	client := NewAvailabilityClient(srv.URL, 5*time.Second, true)

	if _, found, err := client.Closest(context.Background(), "https://example.com/other-case"); err != nil || found {
		t.Fatalf("found=%v err=%v; the CDX API ignores case, the page doesn't", found, err)
	}
}

func TestAvailabilityClient_ShowsThePage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case !strings.Contains(r.URL.Path, "id_/"):
			http.Error(w, "the snapshot must be read as captured: "+r.URL.Path, http.StatusBadRequest)
		case strings.HasSuffix(r.URL.Path, "/wall"):
			_, _ = w.Write([]byte(`<html><title>Just a moment...</title></html>`))
		case strings.HasSuffix(r.URL.Path, "/slow"):
			http.Error(w, "not ready", http.StatusServiceUnavailable)
		default:
			_, _ = w.Write([]byte(`<html><h1>An article</h1></html>`))
		}
	}))
	t.Cleanup(srv.Close)
	client := NewAvailabilityClient(srv.URL, 5*time.Second, true)
	client.retryPause = time.Millisecond

	for page, want := range map[string]bool{"article": true, "wall": false, "slow": true} {
		cited := "https://example.com/" + page
		if got := client.ShowsThePage(context.Background(), cited, srv.URL+"/web/20261102155217/"+cited); got != want {
			t.Errorf("%s: %v, want %v", page, got, want)
		}
	}
	// The replay followed the citation's redirect: the page itself is fine, but it isn't the one cited.
	home := srv.URL + "/web/20261102155217/https://example.com/"
	if client.ShowsThePage(context.Background(), "https://example.com/article", home) {
		t.Error("a snapshot of the home page passed for a cited article")
	}
	if client.ShowsThePage(context.Background(), "https://example.com/article", srv.URL+"/save/https://example.com/article") {
		t.Error("something that isn't a snapshot passed")
	}
}

func TestChallengeWordsCountOnlyInHeadings(t *testing.T) {
	if !looksLikeChallenge([]byte(`<html><title>Are you a robot?</title></html>`)) {
		t.Error("a page titled like a challenge passed")
	}
	if !looksLikeChallenge([]byte(`<html><h1>Pardon Our Interruption</h1></html>`)) {
		t.Error("a page headed like a challenge passed")
	}
	if looksLikeChallenge([]byte(`<html><title>Bots</title><body><h1>Bots</h1><p>The form asks: are you a robot?</p></body></html>`)) {
		t.Error("an article quoting the words was taken for a challenge")
	}
}

func TestSamePage(t *testing.T) {
	for _, c := range []struct {
		target, captured string
		same             bool
	}{
		{"https://example.com/a", "http://www.example.com:80/a", true},
		{"https://example.com/a/", "https://example.com/a", true},
		{"https://example.com/a?b=1&c=2", "https://example.com/a?c=2&b=1", true},
		{"https://example.com/a#part", "https://example.com/a", true},
		{"https://example.com/a", "https://example.com/A", false},
		{"https://example.com/a?v=x", "https://example.com/a?v=X", false},
		{"https://example.com/a", "https://example.com/a?id=1", false},
		{"https://example.com/a", "https://example.com/b", false},
	} {
		if got := samePage(c.target, c.captured); got != c.same {
			t.Errorf("samePage(%q, %q) = %v", c.target, c.captured, got)
		}
	}
}

func TestAvailabilityClient_AsksAgainWhenTheArchiveIsBusy(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		if calls == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, _ = w.Write([]byte(`[]`))
	}))
	t.Cleanup(srv.Close)
	client := NewAvailabilityClient(srv.URL, 5*time.Second, true)
	client.retryPause = time.Millisecond

	if _, found, err := client.Closest(context.Background(), "https://example.com/x"); err != nil || found || calls != 2 {
		t.Fatalf("found=%v err=%v after %d calls", found, err, calls)
	}
}

func TestAvailabilityClient_ReportsAFailedLookup(t *testing.T) {
	for name, handler := range map[string]http.HandlerFunc{
		"rate limited": func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusTooManyRequests) },
		"server error": func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusBadGateway) },
		"refused":      func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusForbidden) },
		"not JSON":     func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`<html>busy</html>`)) },
		"empty body":   func(http.ResponseWriter, *http.Request) {},
		"short row":    func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`[["timestamp"],["2020"]]`)) },
	} {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(handler)
			t.Cleanup(srv.Close)
			client := NewAvailabilityClient(srv.URL, 5*time.Second, true)
			client.retryPause = time.Millisecond

			if _, found, err := client.Closest(context.Background(), "https://example.com/x"); err == nil || found {
				t.Fatalf("found=%v err=%v, want an error", found, err)
			}
		})
	}
}

func TestAvailabilityClient_ACaptureItCannotReadIsNotVouchedFor(t *testing.T) {
	srv := newFakeAvailability(t)
	client := NewAvailabilityClient(srv.URL, 5*time.Second, true)
	client.retryPause = time.Millisecond

	if _, found, err := client.Closest(context.Background(), "https://example.com/flaky"); err == nil || found {
		t.Fatalf("found=%v err=%v, want an error", found, err)
	}
}
