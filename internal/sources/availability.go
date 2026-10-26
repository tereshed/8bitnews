package sources

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// AvailabilityClient queries the Internet Archive's Wayback Availability
// JSON API (https://archive.org/wayback/available?url=...) to find an
// *existing* snapshot of a URL, independent of whether Save Page Now can
// archive it right now. Policy (countries/us/policy.md "Источники в
// ролике") treats a page the hub cannot reach today (dead, soft 404,
// bot-protected, unreachable) as still citable if an earlier snapshot shows
// the page existed with HTTP 200 — Save Page Now archiving a dead page's
// 404 response is not that proof.
type AvailabilityClient struct {
	baseURL string
	http    *http.Client
}

// NewAvailabilityClient builds a client against baseURL (e.g.
// "https://archive.org"). allowPrivate lifts the SSRF protection for tests
// pointed at an httptest server on a loopback address; production must
// always pass false.
func NewAvailabilityClient(baseURL string, timeout time.Duration, allowPrivate bool) *AvailabilityClient {
	return &AvailabilityClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		http:    newSafeHTTPClient(timeout, allowPrivate),
	}
}

type availabilityResponse struct {
	ArchivedSnapshots struct {
		Closest struct {
			Available bool   `json:"available"`
			URL       string `json:"url"`
			Timestamp string `json:"timestamp"`
			Status    string `json:"status"`
		} `json:"closest"`
	} `json:"archived_snapshots"`
}

// Closest looks up the closest existing snapshot of targetURL. found is
// false when the Availability API has no snapshot at all, or when its
// closest snapshot's recorded HTTP status was not 200 (e.g. the archive
// only has a capture of the page's own 404).
func (a *AvailabilityClient) Closest(ctx context.Context, targetURL string) (archiveURL string, found bool, err error) {
	reqURL := a.baseURL + "/wayback/available?url=" + url.QueryEscape(targetURL)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return "", false, fmt.Errorf("building availability request for %s: %w", targetURL, err)
	}
	req.Header.Set("User-Agent", "8bitnews-sources-bot/1.0 (+https://github.com/tereshed/8bitnews)")

	resp, err := a.http.Do(req)
	if err != nil {
		return "", false, fmt.Errorf("calling availability API for %s: %w", targetURL, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 400 {
		return "", false, fmt.Errorf("availability API rejected %s: HTTP %d", targetURL, resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil {
		return "", false, fmt.Errorf("reading availability response for %s: %w", targetURL, err)
	}

	var parsed availabilityResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", false, fmt.Errorf("parsing availability response for %s: %w", targetURL, err)
	}

	closest := parsed.ArchivedSnapshots.Closest
	if !closest.Available || closest.URL == "" || closest.Status != "200" {
		return "", false, nil
	}
	return closest.URL, true, nil
}
