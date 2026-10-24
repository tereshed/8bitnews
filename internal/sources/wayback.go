package sources

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// WaybackClient archives pages via the Internet Archive's Save Page Now
// service. baseURL is overridable so tests point it at an httptest server
// instead of the real archive.org (docs/architecture.md §6).
type WaybackClient struct {
	baseURL string
	http    *http.Client
}

// NewWaybackClient builds a client against baseURL (e.g.
// "https://web.archive.org").
func NewWaybackClient(baseURL string, timeout time.Duration) *WaybackClient {
	return &WaybackClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		http:    &http.Client{Timeout: timeout},
	}
}

// Save asks Save Page Now to archive targetURL and returns the archived
// snapshot's URL.
func (w *WaybackClient) Save(ctx context.Context, targetURL string) (string, error) {
	saveURL := w.baseURL + "/save/" + targetURL

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, saveURL, nil)
	if err != nil {
		return "", fmt.Errorf("building Save Page Now request for %s: %w", targetURL, err)
	}
	req.Header.Set("User-Agent", "8bitnews-sources-bot/1.0 (+https://github.com/tereshed/8bitnews)")

	resp, err := w.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("calling Save Page Now for %s: %w", targetURL, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("Save Page Now rejected %s: HTTP %d", targetURL, resp.StatusCode)
	}

	archiveURL := saveURL
	if resp.Request != nil && resp.Request.URL != nil {
		archiveURL = resp.Request.URL.String()
	}
	if _, err := url.Parse(archiveURL); err != nil {
		return "", fmt.Errorf("Save Page Now returned an invalid archive URL for %s: %w", targetURL, err)
	}
	return archiveURL, nil
}
