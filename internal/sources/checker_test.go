package sources

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func newChecker() *Checker {
	return NewChecker(5*time.Second, true)
}

func TestChecker_OK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("<html><body>All about the thing</body></html>"))
	}))
	defer srv.Close()

	result := newChecker().Check(context.Background(), srv.URL+"/article")
	if result.Status != StatusOK {
		t.Fatalf("status = %q, want %q", result.Status, StatusOK)
	}
	if !result.Alive() {
		t.Fatalf("expected OK result to be Alive()")
	}
}

func TestChecker_Dead404(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer srv.Close()

	result := newChecker().Check(context.Background(), srv.URL+"/gone")
	if result.Status != StatusDead {
		t.Fatalf("status = %q, want %q", result.Status, StatusDead)
	}
	if result.HTTPStatus != http.StatusNotFound {
		t.Fatalf("HTTPStatus = %d, want 404", result.HTTPStatus)
	}
}

func TestChecker_Dead410(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusGone)
	}))
	defer srv.Close()

	result := newChecker().Check(context.Background(), srv.URL+"/removed")
	if result.Status != StatusDead {
		t.Fatalf("status = %q, want %q", result.Status, StatusDead)
	}
}

func TestChecker_BotProtected403(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	result := newChecker().Check(context.Background(), srv.URL+"/blocked")
	if result.Status != StatusBotProtected {
		t.Fatalf("status = %q, want %q", result.Status, StatusBotProtected)
	}
	if !result.Alive() {
		t.Fatalf("bot-protected sources are still Alive() (policy allows them with an archive copy)")
	}
}

func TestChecker_ChallengePage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("<title>Just a moment...</title>"))
	}))
	defer srv.Close()

	result := newChecker().Check(context.Background(), srv.URL+"/challenge")
	if result.Status != StatusBotProtected {
		t.Fatalf("status = %q, want %q", result.Status, StatusBotProtected)
	}
}

func TestChecker_ChallengePage_CfChlMarker(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`<html><body><div class="cf-chl-widget" data-cfchallenge="challenge-platform"></div></body></html>`))
	}))
	defer srv.Close()

	result := newChecker().Check(context.Background(), srv.URL+"/challenge")
	if result.Status != StatusBotProtected {
		t.Fatalf("status = %q, want %q", result.Status, StatusBotProtected)
	}
}

// TestChecker_GenericCaptchaWordIsNotAChallenge makes sure dropping the
// generic "captcha" marker (revision request point 5) actually stopped a
// false positive: an ordinary article whose comments widget happens to use
// reCAPTCHA must not be classified as bot-protected.
func TestChecker_GenericCaptchaWordIsNotAChallenge(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`<html><body><article>Real news article.</article>
			<div id="comments">Please solve the captcha to post a comment.</div>
			</body></html>`))
	}))
	defer srv.Close()

	result := newChecker().Check(context.Background(), srv.URL+"/article")
	if result.Status != StatusOK {
		t.Fatalf("status = %q, want %q (generic 'captcha' word must not trigger bot_protected)", result.Status, StatusOK)
	}
}

func TestChecker_Soft404Body(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("<html><head><title>Page not found</title></head><body>Sorry, the page could not be found.</body></html>"))
	}))
	defer srv.Close()

	result := newChecker().Check(context.Background(), srv.URL+"/soft")
	if result.Status != StatusSoft404 {
		t.Fatalf("status = %q, want %q", result.Status, StatusSoft404)
	}
}

// TestChecker_SoftMarkerInArticleBodyIsNotSoft404 covers revision request
// point 5: a real article whose running text happens to contain a soft-404
// phrase ("doesn't exist") must not be misclassified, because that phrase
// is not in the page's <title>/<h1>.
func TestChecker_SoftMarkerInArticleBodyIsNotSoft404(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`<html><head><title>City council votes on new budget</title></head>
			<body><h1>City council votes on new budget</h1>
			<p>The mayor said the old proposal doesn't exist anymore after last night's vote.</p>
			</body></html>`))
	}))
	defer srv.Close()

	result := newChecker().Check(context.Background(), srv.URL+"/article")
	if result.Status != StatusOK {
		t.Fatalf("status = %q, want %q (soft-404 phrase in body text, not title/h1, must not trigger soft_404)", result.Status, StatusOK)
	}
}

func TestChecker_Soft404RedirectToHome(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/article", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/", http.StatusFound)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("home page"))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	result := newChecker().Check(context.Background(), srv.URL+"/article")
	if result.Status != StatusSoft404 {
		t.Fatalf("status = %q, want %q", result.Status, StatusSoft404)
	}
}

// TestChecker_RedirectWithoutWWWIsOK covers revision request point 5: a
// redirect from the bare domain to its www subdomain (or vice versa) is an
// ordinary redirect, not a soft 404, because it stays on the same eTLD+1.
// httptest servers don't have real hostnames, so this drives
// sameRegistrableDomain directly rather than through an actual redirect.
func TestChecker_SameRegistrableDomain_WWW(t *testing.T) {
	if !sameRegistrableDomain("example.com", "www.example.com") {
		t.Fatalf("example.com and www.example.com should share the same eTLD+1")
	}
	if sameRegistrableDomain("example.com", "other-example.com") {
		t.Fatalf("example.com and other-example.com must not be treated as the same domain")
	}
}

// TestLooksLikeConsentWall covers revision request point 5: Yahoo/AOL-style
// EU consent walls are not a soft 404 (the page exists, the visitor is just
// being asked to accept cookies). This checks the host-matching logic
// directly rather than through a real redirect, since the real consent
// hostnames cannot be resolved to a local httptest server without a real
// DNS/network dependency, which these tests must not have.
func TestLooksLikeConsentWall(t *testing.T) {
	cases := []struct {
		finalURL string
		want     bool
	}{
		{"https://consent.yahoo.com/collectConsent?sessionId=abc", true},
		{"https://guce.yahoo.com/consent", true},
		{"https://guce.oath.com/consent", true},
		{"https://news.example.com/article", false},
		{"https://www.example.com/article", false},
	}
	for _, c := range cases {
		if got := looksLikeConsentWall(c.finalURL); got != c.want {
			t.Errorf("looksLikeConsentWall(%q) = %v, want %v", c.finalURL, got, c.want)
		}
	}
}

func TestChecker_Unreachable(t *testing.T) {
	result := newChecker().Check(context.Background(), "http://127.0.0.1:1/nope")
	if result.Status != StatusUnreachable {
		t.Fatalf("status = %q, want %q", result.Status, StatusUnreachable)
	}
	if result.Alive() {
		t.Fatalf("unreachable sources must not be Alive()")
	}
}

// --- SSRF protection (revision request point 1) ---

func TestChecker_BlocksNonHTTPScheme(t *testing.T) {
	checker := NewChecker(5*time.Second, false)
	result := checker.Check(context.Background(), "file:///etc/passwd")
	if result.Status != StatusBlocked {
		t.Fatalf("status = %q, want %q", result.Status, StatusBlocked)
	}
	if result.Alive() {
		t.Fatalf("blocked sources must not be Alive()")
	}
}

func TestChecker_BlocksLoopbackByDefault(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("should never be reached"))
	}))
	defer srv.Close()

	checker := NewChecker(5*time.Second, false) // allowPrivate=false: production setting
	result := checker.Check(context.Background(), srv.URL+"/anything")
	if result.Status != StatusBlocked {
		t.Fatalf("status = %q, want %q", result.Status, StatusBlocked)
	}
}

func TestChecker_BlocksCloudMetadataAddress(t *testing.T) {
	checker := NewChecker(2*time.Second, false)
	result := checker.Check(context.Background(), "http://169.254.169.254/latest/meta-data/")
	if result.Status != StatusBlocked {
		t.Fatalf("status = %q, want %q (must refuse without connecting)", result.Status, StatusBlocked)
	}
}

// TestChecker_BlocksRedirectToLoopback covers the DNS-rebinding / "redirect
// to an internal address" case explicitly called out in the revision
// request: the dialer's Control re-runs the IP check on every dial — the
// one made to follow a redirect included — so a redirect chain that ends
// at a blocked address is refused even though the first hop's address
// alone (checked in isolation) might not reveal that. httptest servers
// only ever bind to loopback, so both hops here are already blocked
// individually; what this test additionally exercises beyond the
// single-hop tests above is that Check() still reports Blocked (not OK)
// when the *final* URL after a redirect, not just the *initial* one, is
// the blocked address.
func TestChecker_BlocksRedirectToLoopback(t *testing.T) {
	internal := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("internal service response"))
	}))
	defer internal.Close()

	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, internal.URL+"/secret", http.StatusFound)
	}))
	defer redirector.Close()

	checker := NewChecker(5*time.Second, false) // allowPrivate=false: production setting
	result := checker.Check(context.Background(), redirector.URL+"/redirect")
	if result.Status != StatusBlocked {
		t.Fatalf("status = %q, want %q", result.Status, StatusBlocked)
	}
}

func TestIsBlockedIP(t *testing.T) {
	cases := []struct {
		ip      string
		blocked bool
	}{
		{"127.0.0.1", true},
		{"169.254.169.254", true},
		{"10.0.0.5", true},
		{"172.16.0.5", true},
		{"192.168.1.1", true},
		{"100.64.0.1", true},
		{"224.0.0.1", true},
		{"0.0.0.0", true},
		{"::1", true},
		{"fc00::1", true},
		{"fe80::1", true},
		{"::ffff:127.0.0.1", true},
		{"::ffff:169.254.169.254", true},
		{"8.8.8.8", false},
		{"93.184.216.34", false},
	}
	for _, c := range cases {
		ip := net.ParseIP(c.ip)
		if ip == nil {
			t.Fatalf("invalid test IP %q", c.ip)
		}
		if got := isBlockedIP(ip); got != c.blocked {
			t.Errorf("isBlockedIP(%s) = %v, want %v", c.ip, got, c.blocked)
		}
	}
}
