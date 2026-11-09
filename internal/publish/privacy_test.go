package publish

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestThePrivacyPageSaysWhatGoogleAndTheLawAskFor(t *testing.T) {
	p := NewPages(nil, Config{CorrectionsContact: "corrections@example.org"}, slog.Default())
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/privacy", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /privacy: %d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{
		"YouTube API Services",
		"https://www.youtube.com/t/terms",
		"https://policies.google.com/privacy",
		"https://myaccount.google.com/permissions",
		"Contact: corrections@example.org",
		"Contacto: corrections@example.org",
		"Privacidad",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the page has no %q", want)
		}
	}
	if csp := rec.Header().Get("Content-Security-Policy"); !strings.Contains(csp, "default-src 'none'") {
		t.Errorf("CSP: %q", csp)
	}
}
