// Package sources implements the hub "sources" component
// (docs/architecture.md §6): collecting the links a video actually relies
// on, checking that they are alive, and archiving them, per the rules in
// countries/us/policy.md "Источники в ролике".
package sources

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// maxBodyBytes caps how much of a response body the soft-404/bot-protection
// heuristics read, so a huge page cannot stall release_submit.
const maxBodyBytes = 64 * 1024

// CheckStatus is the outcome of checking one source link, matching the
// categories in countries/us/policy.md "Источники в ролике" and the
// release_sources.check_status column.
type CheckStatus string

const (
	StatusOK           CheckStatus = "ok"
	StatusDead         CheckStatus = "dead"
	StatusSoft404      CheckStatus = "soft_404"
	StatusBotProtected CheckStatus = "bot_protected"
	StatusUnreachable  CheckStatus = "unreachable"
)

// CheckResult is what Checker.Check found for one URL.
type CheckResult struct {
	Status     CheckStatus
	HTTPStatus int
	FinalURL   string
}

// Alive reports whether the policy treats this result as a live source: ok,
// or bot-protected (policy.md allows that only when the agent already saw
// the page while researching and an archive copy exists — the archive step
// after Check decides that; bot_protected is not "dead" on its own).
func (r CheckResult) Alive() bool {
	return r.Status == StatusOK || r.Status == StatusBotProtected
}

// Checker makes the live-link checks release_submit requires: HTTP status
// after redirects, "soft 404", and bot/challenge protection.
type Checker struct {
	http *http.Client
}

// NewChecker builds a Checker whose requests time out after timeout.
func NewChecker(timeout time.Duration) *Checker {
	return &Checker{http: &http.Client{Timeout: timeout}}
}

// Check fetches rawURL and classifies the result.
func (c *Checker) Check(ctx context.Context, rawURL string) CheckResult {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return CheckResult{Status: StatusUnreachable, FinalURL: rawURL}
	}
	req.Header.Set("User-Agent", "8bitnews-sources-bot/1.0 (+https://github.com/tereshed/8bitnews)")

	resp, err := c.http.Do(req)
	if err != nil {
		return CheckResult{Status: StatusUnreachable, FinalURL: rawURL}
	}
	defer func() { _ = resp.Body.Close() }()

	finalURL := rawURL
	if resp.Request != nil && resp.Request.URL != nil {
		finalURL = resp.Request.URL.String()
	}

	switch {
	case resp.StatusCode == http.StatusUnauthorized,
		resp.StatusCode == http.StatusForbidden,
		resp.StatusCode == http.StatusTooManyRequests,
		resp.StatusCode == 451: // unavailable for legal reasons
		return CheckResult{Status: StatusBotProtected, HTTPStatus: resp.StatusCode, FinalURL: finalURL}
	case resp.StatusCode == http.StatusNotFound, resp.StatusCode == http.StatusGone:
		return CheckResult{Status: StatusDead, HTTPStatus: resp.StatusCode, FinalURL: finalURL}
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		body, _ := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
		if looksLikeChallenge(body) {
			return CheckResult{Status: StatusBotProtected, HTTPStatus: resp.StatusCode, FinalURL: finalURL}
		}
		if redirectedAway(rawURL, finalURL) || looksLikeSoft404(body) {
			return CheckResult{Status: StatusSoft404, HTTPStatus: resp.StatusCode, FinalURL: finalURL}
		}
		return CheckResult{Status: StatusOK, HTTPStatus: resp.StatusCode, FinalURL: finalURL}
	default:
		return CheckResult{Status: StatusDead, HTTPStatus: resp.StatusCode, FinalURL: finalURL}
	}
}

// redirectedAway reports whether the final URL landed on the site's home
// page or a different host than the one requested — the "soft 404" shape
// policy.md calls out explicitly, distinct from a literal 404 status.
func redirectedAway(original, final string) bool {
	ou, err1 := url.Parse(original)
	fu, err2 := url.Parse(final)
	if err1 != nil || err2 != nil {
		return false
	}
	origPath := strings.Trim(ou.Path, "/")
	if origPath == "" {
		// The link itself pointed at a home page; no "redirected to home"
		// signal is meaningful here.
		return false
	}
	if !strings.EqualFold(ou.Host, fu.Host) {
		return true
	}
	finalPath := strings.Trim(fu.Path, "/")
	return finalPath == ""
}

var soft404Markers = []string{
	"page not found",
	"404 not found",
	"we couldn't find",
	"could not be found",
	"content is no longer available",
	"doesn't exist",
	"page no longer exists",
}

func looksLikeSoft404(body []byte) bool {
	text := strings.ToLower(string(body))
	for _, marker := range soft404Markers {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

var challengeMarkers = []string{
	"cf-browser-verification",
	"just a moment",
	"attention required",
	"verify you are human",
	"checking your browser",
	"captcha",
}

func looksLikeChallenge(body []byte) bool {
	text := strings.ToLower(string(body))
	for _, marker := range challengeMarkers {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}
