// Package sources implements the hub "sources" component
// (docs/architecture.md §6): collecting the links a video actually relies
// on, checking that they are alive, and archiving them, per the rules in
// countries/us/policy.md "Источники в ролике".
package sources

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"golang.org/x/net/publicsuffix"
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
	// StatusBlocked marks a URL this process refused to even try to reach:
	// a non-http(s) scheme, or an address isBlockedIP considers internal
	// (SSRF protection). Unlike the other statuses, it is never backed by
	// an archive snapshot — the hub never asks a third party to fetch an
	// address it itself refused to dial.
	StatusBlocked CheckStatus = "blocked"
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
// allowPrivate lifts the SSRF protection for tests pointed at httptest
// servers on loopback addresses; production must always pass false.
func NewChecker(timeout time.Duration, allowPrivate bool) *Checker {
	return &Checker{http: newSafeHTTPClient(timeout, allowPrivate)}
}

// Check fetches rawURL and classifies the result.
func (c *Checker) Check(ctx context.Context, rawURL string) CheckResult {
	parsed, err := url.Parse(rawURL)
	if err != nil || !isAllowedScheme(parsed) {
		return CheckResult{Status: StatusBlocked, FinalURL: rawURL}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return CheckResult{Status: StatusUnreachable, FinalURL: rawURL}
	}
	req.Header.Set("User-Agent", "8bitnews-sources-bot/1.0 (+https://github.com/tereshed/8bitnews)")

	resp, err := c.http.Do(req)
	if err != nil {
		if errors.Is(err, ErrBlockedAddress) {
			return CheckResult{Status: StatusBlocked, FinalURL: rawURL}
		}
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
		if looksLikeConsentWall(finalURL) {
			return CheckResult{Status: StatusBotProtected, HTTPStatus: resp.StatusCode, FinalURL: finalURL}
		}
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
// page, or on a different registrable domain (eTLD+1) than the one
// requested — the "soft 404" shape policy.md calls out explicitly, distinct
// from a literal 404 status. Comparing eTLD+1 (via publicsuffix) rather
// than the full host means ordinary redirects such as example.com ->
// www.example.com, or to a regional subdomain on the same domain, are not
// mistaken for a soft 404.
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
	if !sameRegistrableDomain(ou.Hostname(), fu.Hostname()) {
		return true
	}
	finalPath := strings.Trim(fu.Path, "/")
	return finalPath == ""
}

// sameRegistrableDomain reports whether a and b share the same eTLD+1. It
// falls back to an exact (case-insensitive) host comparison when either
// host is not a recognized public-suffix domain (e.g. a bare IP address,
// as httptest servers use in tests).
func sameRegistrableDomain(a, b string) bool {
	ea, errA := publicsuffix.EffectiveTLDPlusOne(a)
	eb, errB := publicsuffix.EffectiveTLDPlusOne(b)
	if errA != nil || errB != nil {
		return strings.EqualFold(a, b)
	}
	return strings.EqualFold(ea, eb)
}

// consentWallHosts are hosts known to serve a cookie/privacy-consent
// interstitial instead of the requested article to visitors the site
// detects as being in the EU/EEA (the hub itself runs in the EU) — e.g.
// Yahoo and AOL redirect to consent.yahoo.com / guce.yahoo.com. These are
// not a "soft 404": the page exists, but policy treats it like
// bot-protection (allowed only with an archive copy), not a dead link.
var consentWallHostPrefixes = []string{"consent.", "guce."}

var consentWallHosts = map[string]bool{
	"consent.yahoo.com": true,
	"guce.yahoo.com":    true,
	"guce.oath.com":     true,
	"consent.aol.com":   true,
	"guce.aol.com":      true,
}

func looksLikeConsentWall(finalURL string) bool {
	u, err := url.Parse(finalURL)
	if err != nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	if consentWallHosts[host] {
		return true
	}
	for _, prefix := range consentWallHostPrefixes {
		if strings.HasPrefix(host, prefix) {
			return true
		}
	}
	return false
}

var titleTagPattern = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)
var h1TagPattern = regexp.MustCompile(`(?is)<h1[^>]*>(.*?)</h1>`)

// headingText extracts the text of every <title> and <h1> element in body.
// Soft-404 markers are only meaningful there: an ordinary article's running
// text can legitimately contain a phrase like "doesn't exist" (e.g. an
// article discussing a product that no longer exists) without the page
// itself being a 404.
func headingText(body []byte) string {
	var b strings.Builder
	for _, m := range titleTagPattern.FindAllSubmatch(body, -1) {
		b.Write(m[1])
		b.WriteByte(' ')
	}
	for _, m := range h1TagPattern.FindAllSubmatch(body, -1) {
		b.Write(m[1])
		b.WriteByte(' ')
	}
	return strings.ToLower(b.String())
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
	text := headingText(body)
	for _, marker := range soft404Markers {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

// challengeMarkers are signatures of bot/anti-automation challenge pages
// (e.g. Cloudflare's "Just a moment..." interstitial), not of ordinary
// pages that merely embed a CAPTCHA widget somewhere (a comments form, a
// login link) — those are real, reachable pages.
var challengeMarkers = []string{
	"cf-browser-verification",
	"just a moment",
	"attention required",
	"verify you are human",
	"checking your browser",
	"cf-chl",
	"challenge-platform",
}

func looksLikeChallenge(body []byte) bool {
	// Unlike soft-404 markers, these are checked against the whole page:
	// they are specific signatures of challenge-page markup/scripts (not
	// generic words like "captcha", which also shows up in an ordinary
	// page's comment-form widget), so scanning the full body does not
	// produce the false positives the generic marker did.
	text := strings.ToLower(string(body))
	for _, marker := range challengeMarkers {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}
