package sources

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// WaybackClient archives pages via the Internet Archive's Save Page Now
// service. baseURL is overridable so tests point it at an httptest server
// instead of the real web.archive.org (docs/architecture.md §6).
type WaybackClient struct {
	baseURL   string
	http      *http.Client
	accessKey string
	secretKey string
}

// NewWaybackClient builds a client against baseURL (e.g.
// "https://web.archive.org"). accessKey/secretKey are the optional Save
// Page Now API credentials (HUB_WAYBACK_ACCESS_KEY/HUB_WAYBACK_SECRET_KEY);
// anonymous Save Page Now calls are subject to tight rate limits, so an
// archive.org account's keys should be configured in production. allowPrivate
// lifts the SSRF protection for tests pointed at an httptest server on a
// loopback address; production must always pass false.
func NewWaybackClient(baseURL string, timeout time.Duration, accessKey, secretKey string, allowPrivate bool) *WaybackClient {
	return &WaybackClient{
		baseURL:   strings.TrimRight(baseURL, "/"),
		http:      newSafeHTTPClient(timeout, allowPrivate),
		accessKey: accessKey,
		secretKey: secretKey,
	}
}

// snapshotPathPattern matches a genuine Save Page Now / Wayback snapshot
// path: /web/<14 digits>/<original url>. Anything else — including the
// bare "/save/<url>" request URL Save Page Now echoes back when it accepts
// the job without having produced a snapshot yet — is not proof of an
// archive copy.
var snapshotPathPattern = regexp.MustCompile(`^/web/\d{14}/`)

// Save asks Save Page Now to archive targetURL and returns the archived
// snapshot's URL. It only succeeds when the response actually redirected to
// a snapshot path; a plain non-error status with no such redirect (which
// Save Page Now returns while a capture is merely queued or in progress) is
// not an archive copy and must not be recorded as one.
func (w *WaybackClient) Save(ctx context.Context, targetURL string) (string, error) {
	saveURL := w.baseURL + "/save/" + targetURL

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, saveURL, nil)
	if err != nil {
		return "", fmt.Errorf("building Save Page Now request for %s: %w", targetURL, err)
	}
	req.Header.Set("User-Agent", "8bitnews-sources-bot/1.0 (+https://github.com/tereshed/8bitnews)")
	if w.accessKey != "" && w.secretKey != "" {
		req.Header.Set("Authorization", fmt.Sprintf("LOW %s:%s", w.accessKey, w.secretKey))
	}

	resp, err := w.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("calling Save Page Now for %s: %w", targetURL, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("Save Page Now rejected %s: HTTP %d", targetURL, resp.StatusCode)
	}

	if resp.Request == nil || resp.Request.URL == nil || !snapshotPathPattern.MatchString(resp.Request.URL.Path) {
		return "", fmt.Errorf("Save Page Now did not return a snapshot for %s (final URL %q)", targetURL, finalURLOf(resp))
	}
	return resp.Request.URL.String(), nil
}

func finalURLOf(resp *http.Response) string {
	if resp.Request != nil && resp.Request.URL != nil {
		return resp.Request.URL.String()
	}
	return ""
}
