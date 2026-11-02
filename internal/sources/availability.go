package sources

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// AvailabilityClient queries the Internet Archive's CDX API
// (https://web.archive.org/cdx/search/cdx) to find an *existing* snapshot of
// a URL, independent of whether Save Page Now can archive it right now.
// Policy (countries/us/policy.md "Источники в ролике") treats a page the hub
// cannot reach today (dead, soft 404, bot-protected, unreachable) as still
// citable if an earlier snapshot shows the page existed with HTTP 200 — Save
// Page Now archiving a dead page's 404 response is not that proof.
//
// Not the Availability JSON API (archive.org/wayback/available): it answers
// 429 to an escaped url parameter whatever the rate, misses captures made
// minutes ago, and only reports the closest capture, even when that one is
// an error page and an earlier one isn't (trial 3, 2026-11-02).
type AvailabilityClient struct {
	baseURL string
	http    *http.Client
}

// NewAvailabilityClient builds a client against baseURL (e.g.
// "https://web.archive.org"). allowPrivate lifts the SSRF protection for tests
// pointed at an httptest server on a loopback address; production must
// always pass false.
func NewAvailabilityClient(baseURL string, timeout time.Duration, allowPrivate bool) *AvailabilityClient {
	return &AvailabilityClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		http:    newSafeHTTPClient(timeout, allowPrivate),
	}
}

// Closest looks up the latest snapshot of targetURL recorded with HTTP 200.
// found is false when the archive has no such snapshot: none at all, or only
// captures of the page's own error response.
func (a *AvailabilityClient) Closest(ctx context.Context, targetURL string) (archiveURL string, found bool, err error) {
	query := url.Values{
		"url":    {targetURL},
		"filter": {"statuscode:200"},
		"fl":     {"timestamp,original"},
		"limit":  {"-1"}, // the last one
		"output": {"json"},
	}
	reqURL := a.baseURL + "/cdx/search/cdx?" + query.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return "", false, fmt.Errorf("building snapshot lookup for %s: %w", targetURL, err)
	}
	req.Header.Set("User-Agent", "8bitnews-sources-bot/1.0 (+https://github.com/tereshed/8bitnews)")

	resp, err := a.http.Do(req)
	if err != nil {
		return "", false, fmt.Errorf("looking up a snapshot of %s: %w", targetURL, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 400 {
		return "", false, fmt.Errorf("snapshot lookup rejected %s: HTTP %d", targetURL, resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil {
		return "", false, fmt.Errorf("reading the snapshot lookup for %s: %w", targetURL, err)
	}
	if len(bytes.TrimSpace(body)) == 0 {
		return "", false, nil
	}

	// A header row, then the captures: [["timestamp","original"],["20261102155217","https://…"]]; [] when none.
	var rows [][]string
	if err := json.Unmarshal(body, &rows); err != nil {
		return "", false, fmt.Errorf("parsing the snapshot lookup for %s: %w", targetURL, err)
	}
	if len(rows) < 2 {
		return "", false, nil
	}
	capture := rows[len(rows)-1]
	if len(capture) != 2 || capture[0] == "" || capture[1] == "" {
		return "", false, fmt.Errorf("parsing the snapshot lookup for %s: unexpected row %q", targetURL, capture)
	}
	return a.baseURL + "/web/" + capture[0] + "/" + capture[1], true, nil
}
