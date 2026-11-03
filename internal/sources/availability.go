package sources

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// AvailabilityClient looks for an *existing* snapshot of a URL in the Internet
// Archive, independent of whether Save Page Now can archive it right now.
// Policy (countries/us/policy.md "Источники в ролике") treats a page the hub
// cannot reach today (dead, soft 404, bot-protected, unreachable) as still
// citable if an earlier snapshot shows the page existed with HTTP 200 — Save
// Page Now archiving a dead page's 404 response is not that proof.
//
// It asks the CDX API (https://web.archive.org/cdx/search/cdx), not the
// Availability JSON API (archive.org/wayback/available): that one answers 429
// to an escaped url parameter whatever the rate, misses captures made minutes
// ago, and only reports the closest capture, even when that one is an error
// page and an earlier one isn't (trial 3, 2026-11-02). A capture's recorded
// status isn't enough either: a challenge page or a soft 404 is recorded with
// 200 too, so the client reads the capture before vouching for it.
type AvailabilityClient struct {
	baseURL    string
	http       *http.Client
	retryPause time.Duration
}

// NewAvailabilityClient builds a client against baseURL (e.g.
// "https://web.archive.org"). allowPrivate lifts the SSRF protection for tests
// pointed at an httptest server on a loopback address; production must
// always pass false.
func NewAvailabilityClient(baseURL string, timeout time.Duration, allowPrivate bool) *AvailabilityClient {
	return &AvailabilityClient{
		baseURL:    strings.TrimRight(baseURL, "/"),
		http:       newSafeHTTPClient(timeout, allowPrivate),
		retryPause: 2 * time.Second,
	}
}

// snapshotsToRead bounds how many captures one lookup reads, newest first. Each
// read can take seconds, and the release's source check has one deadline.
const snapshotsToRead = 3

// Closest looks up the latest snapshot that shows targetURL itself. found is
// false when the archive has none: no capture of exactly this URL recorded
// with HTTP 200, or only captures of a challenge page or a soft 404. An error
// means the archive couldn't answer, so whether a copy exists is unknown.
func (a *AvailabilityClient) Closest(ctx context.Context, targetURL string) (archiveURL string, found bool, err error) {
	query := url.Values{
		"url":       {targetURL},
		"matchType": {"exact"}, // without it a trailing "*" is a prefix search
		"filter":    {"statuscode:200"},
		"collapse":  {"digest"},
		"fl":        {"timestamp,original"},
		"limit":     {"-" + strconv.Itoa(snapshotsToRead)}, // the last ones
		"output":    {"json"},
	}
	body, err := a.get(ctx, a.baseURL+"/cdx/search/cdx?"+query.Encode())
	if err != nil {
		return "", false, fmt.Errorf("looking up a snapshot of %s: %w", targetURL, err)
	}

	// A header row, then the captures, oldest first:
	// [["timestamp","original"],["20261102155217","https://…"]]; [] when none.
	var rows [][]string
	if err := json.Unmarshal(body, &rows); err != nil {
		return "", false, fmt.Errorf("parsing the snapshot lookup for %s: %w", targetURL, err)
	}
	var unread error
	for i := len(rows) - 1; i >= 1; i-- {
		if len(rows[i]) != 2 || rows[i][0] == "" || rows[i][1] == "" {
			return "", false, fmt.Errorf("parsing the snapshot lookup for %s: unexpected row %q", targetURL, rows[i])
		}
		timestamp, original := rows[i][0], rows[i][1]
		if !samePage(targetURL, original) {
			continue
		}
		// id_ asks for the page as captured, without the archive's toolbar.
		page, err := a.get(ctx, a.baseURL+"/web/"+timestamp+"id_/"+original)
		if err != nil {
			unread = fmt.Errorf("reading snapshot %s of %s: %w", timestamp, targetURL, err)
			continue
		}
		if !looksLikeChallenge(page) && !looksLikeSoft404(page) {
			return a.baseURL + "/web/" + timestamp + "/" + original, true, nil
		}
	}
	return "", false, unread
}

// ShowsThePage reports whether a snapshot Save Page Now just made shows the
// page, not a challenge page or a soft 404. A snapshot it can't read yet counts:
// a fresh capture may take a while to replay, and Save Page Now did capture it.
func (a *AvailabilityClient) ShowsThePage(ctx context.Context, snapshotURL string) bool {
	page, err := a.get(ctx, snapshotPrefix.ReplaceAllString(snapshotURL, "${1}id_/"))
	if err != nil {
		return true
	}
	return !looksLikeChallenge(page) && !looksLikeSoft404(page)
}

var snapshotPrefix = regexp.MustCompile(`^(.*/web/\d{14})/`)

// samePage reports whether a capture's URL is the page asked for. The CDX API
// matches loosely: it ignores the scheme, www and the port, which is fine, and
// the case of the path and query, which isn't (watch?v=dQw4 is not watch?v=DQW4).
func samePage(target, captured string) bool {
	t, err1 := url.Parse(target)
	c, err2 := url.Parse(captured)
	if err1 != nil || err2 != nil {
		return false
	}
	return strings.TrimRight(t.Path, "/") == strings.TrimRight(c.Path, "/") && t.Query().Encode() == c.Query().Encode()
}

// get fetches rawURL, once more after a pause when the archive is busy (429,
// 5xx): lookups run in parallel, and a refusal here refuses the release.
func (a *AvailabilityClient) get(ctx context.Context, rawURL string) ([]byte, error) {
	for attempt := 1; ; attempt++ {
		body, status, err := a.fetch(ctx, rawURL)
		switch {
		case err != nil:
			return nil, err
		case status < 400:
			return body, nil
		case attempt == 2 || (status != http.StatusTooManyRequests && status < 500):
			return nil, fmt.Errorf("HTTP %d", status)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(a.retryPause):
		}
	}
}

func (a *AvailabilityClient) fetch(ctx context.Context, rawURL string) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("User-Agent", "8bitnews-sources-bot/1.0 (+https://github.com/tereshed/8bitnews)")

	resp, err := a.http.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil {
		return nil, 0, err
	}
	return body, resp.StatusCode, nil
}
