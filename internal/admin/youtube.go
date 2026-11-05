package admin

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"
)

var (
	youtubeID       = regexp.MustCompile(`^[A-Za-z0-9_-]{11}$`)
	errNotAVideoURL = errors.New("not a YouTube video link")
)

const shortsPrefix = "https://www.youtube.com/shorts/"

// ParseYouTubeURL reads what people paste: a Shorts, watch or short link, a
// Studio edit page, or a bare video id. It returns the Shorts link, the one
// form hub stores.
func ParseYouTubeURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if youtubeID.MatchString(raw) {
		return shortsPrefix + raw, nil
	}
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", errNotAVideoURL
	}
	if u.Scheme != "https" && u.Scheme != "http" {
		return "", errNotAVideoURL
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	var id string
	switch strings.ToLower(u.Hostname()) {
	case "youtu.be":
		id = parts[0]
	case "youtube.com", "www.youtube.com", "m.youtube.com":
		switch {
		case len(parts) >= 2 && (parts[0] == "shorts" || parts[0] == "embed" || parts[0] == "live"):
			id = parts[1]
		case parts[0] == "watch":
			id = u.Query().Get("v")
		}
	case "studio.youtube.com":
		if len(parts) >= 2 && parts[0] == "video" {
			id = parts[1]
		}
	}
	if !youtubeID.MatchString(id) {
		return "", errNotAVideoURL
	}
	return shortsPrefix + id, nil
}

// What YouTube says about a link.
const (
	videoVisible = "visible" // anyone with the link can watch: public or unlisted
	videoHidden  = "hidden"  // private or gone
	videoUnknown = ""        // YouTube didn't answer in time
)

const (
	videoVisibleTTL = 5 * time.Minute
	// A hidden video is about to go public, usually: see that sooner.
	videoHiddenTTL = 30 * time.Second
	videoStateWait = 3 * time.Second
)

type videoState struct {
	state string
	at    time.Time
}

// VideoChecker asks YouTube's oEmbed endpoint, which takes no key and answers
// for public and unlisted videos only. Answers are kept a few minutes, so the
// list doesn't ask again on every load.
type VideoChecker struct {
	base   string
	client *http.Client

	mu    sync.Mutex
	cache map[string]videoState
}

// NewVideoChecker asks the real YouTube.
func NewVideoChecker() *VideoChecker {
	return &VideoChecker{base: "https://www.youtube.com/oembed", client: &http.Client{Timeout: videoStateWait}, cache: map[string]videoState{}}
}

// State is videoVisible, videoHidden or videoUnknown. It only ever requests
// the fixed oEmbed address, with a link ParseYouTubeURL has normalized.
func (c *VideoChecker) State(ctx context.Context, link string) string {
	c.mu.Lock()
	cached, ok := c.cache[link]
	c.mu.Unlock()
	ttl := videoVisibleTTL
	if cached.state == videoHidden {
		ttl = videoHiddenTTL
	}
	if ok && time.Since(cached.at) < ttl {
		return cached.state
	}
	q := url.Values{"url": {link}, "format": {"json"}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"?"+q.Encode(), nil)
	if err != nil {
		return videoUnknown
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return videoUnknown
	}
	_ = resp.Body.Close()
	state := videoUnknown
	switch resp.StatusCode {
	case http.StatusOK:
		state = videoVisible
	case http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound, http.StatusBadRequest:
		state = videoHidden
	}
	if state != videoUnknown {
		c.mu.Lock()
		c.cache[link] = videoState{state: state, at: time.Now()}
		c.mu.Unlock()
	}
	return state
}

// Forget drops a link's answer: the moderator just changed it, or wants to
// see the change now.
func (c *VideoChecker) Forget(link string) {
	c.mu.Lock()
	delete(c.cache, link)
	c.mu.Unlock()
}
