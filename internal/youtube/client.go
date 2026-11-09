// Package youtube is hub's way to YouTube: OAuth for a channel, and the Data
// API calls that put a video on the channel's schedule. It speaks HTTP with no
// Google SDK. Limits and fields: platforms/youtube/rules.md.
package youtube

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Scopes: upload, change or read a video of the channel, and its statistics
// for later (platforms/youtube/setup.md).
var Scopes = []string{
	"https://www.googleapis.com/auth/youtube.upload",
	"https://www.googleapis.com/auth/youtube.force-ssl",
	"https://www.googleapis.com/auth/yt-analytics.readonly",
}

const (
	// CategoryNews is News & Politics.
	CategoryNews = "25"
	// MaxThumbnailBytes is YouTube's limit for a custom thumbnail.
	MaxThumbnailBytes = 2 << 20

	apiTimeout = 30 * time.Second
	// A token this close to its expiry isn't used any more.
	tokenMargin = time.Minute
)

// Config is an OAuth client. The addresses default to Google's; tests set them.
type Config struct {
	ClientID, ClientSecret string
	RedirectURL            string

	AuthURL, TokenURL, RevokeURL string
	APIBase, UploadBase          string
}

// Client talks to YouTube for any channel: each call names the channel by its
// refresh token.
type Client struct {
	cfg  Config
	http *http.Client

	mu     sync.Mutex
	tokens map[string]accessToken
	now    func() time.Time
}

type accessToken struct {
	value  string
	expiry time.Time
}

// New builds a client.
func New(cfg Config) *Client {
	defaults := map[*string]string{
		&cfg.AuthURL:    "https://accounts.google.com/o/oauth2/v2/auth",
		&cfg.TokenURL:   "https://oauth2.googleapis.com/token",
		&cfg.RevokeURL:  "https://oauth2.googleapis.com/revoke",
		&cfg.APIBase:    "https://www.googleapis.com/youtube/v3",
		&cfg.UploadBase: "https://www.googleapis.com/upload/youtube/v3",
	}
	for field, value := range defaults {
		if *field == "" {
			*field = value
		}
	}
	// No overall timeout: an upload takes as long as it takes, bounded by its context.
	return &Client{cfg: cfg, http: &http.Client{}, tokens: map[string]accessToken{}, now: time.Now}
}

// AuthCodeURL is where to send a person to connect a channel. access_type and
// prompt make Google give a refresh token every time, also on a second connect.
func (c *Client) AuthCodeURL(state string) string {
	q := url.Values{
		"client_id":     {c.cfg.ClientID},
		"redirect_uri":  {c.cfg.RedirectURL},
		"response_type": {"code"},
		"scope":         {strings.Join(Scopes, " ")},
		"access_type":   {"offline"},
		"prompt":        {"consent"},
		"state":         {state},
	}
	return c.cfg.AuthURL + "?" + q.Encode()
}

// Exchange turns the code from the consent screen into a refresh token. It is
// "" if Google didn't send one.
func (c *Client) Exchange(ctx context.Context, code string) (string, error) {
	tok, err := c.tokenRequest(ctx, url.Values{
		"grant_type":   {"authorization_code"},
		"code":         {code},
		"redirect_uri": {c.cfg.RedirectURL},
	})
	if err != nil {
		return "", err
	}
	if tok.RefreshToken != "" {
		c.remember(tok.RefreshToken, tok)
	}
	return tok.RefreshToken, nil
}

// Revoke ends the access behind a refresh token.
func (c *Client) Revoke(ctx context.Context, refresh string) error {
	ctx, cancel := context.WithTimeout(ctx, apiTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.RevokeURL, strings.NewReader(url.Values{"token": {refresh}}.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.http.Do(req)
	if err != nil {
		return &Error{Retry: true, Message: transportError(err)}
	}
	defer drain(resp)
	// 400: the token isn't valid any more, which is what was asked for.
	if resp.StatusCode >= 300 && resp.StatusCode != http.StatusBadRequest {
		return apiError(resp)
	}
	c.mu.Lock()
	delete(c.tokens, refresh)
	c.mu.Unlock()
	return nil
}

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int64  `json:"expires_in"`
	Error        string `json:"error"`
	Description  string `json:"error_description"`
}

func (c *Client) tokenRequest(ctx context.Context, form url.Values) (tokenResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, apiTimeout)
	defer cancel()
	form.Set("client_id", c.cfg.ClientID)
	form.Set("client_secret", c.cfg.ClientSecret)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return tokenResponse{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.http.Do(req)
	if err != nil {
		return tokenResponse{}, &Error{Retry: true, Message: transportError(err)}
	}
	defer drain(resp)
	var tok tokenResponse
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return tok, &Error{Retry: true, Message: err.Error()}
	}
	_ = json.Unmarshal(body, &tok)
	switch {
	case tok.Error == "invalid_grant":
		return tok, ErrAuthRevoked
	case resp.StatusCode >= 500:
		return tok, &Error{Status: resp.StatusCode, Retry: true, Message: "the token endpoint answered " + resp.Status}
	case resp.StatusCode >= 300 || tok.AccessToken == "":
		// Never the body itself: it can carry a token.
		return tok, &Error{Status: resp.StatusCode, Reason: tok.Error, Message: "the token endpoint refused: " + tok.Error + " " + tok.Description}
	}
	return tok, nil
}

func (c *Client) remember(refresh string, tok tokenResponse) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.tokens[refresh] = accessToken{value: tok.AccessToken, expiry: c.now().Add(time.Duration(tok.ExpiresIn) * time.Second)}
}

func (c *Client) access(ctx context.Context, refresh string) (string, error) {
	c.mu.Lock()
	cached, ok := c.tokens[refresh]
	c.mu.Unlock()
	if ok && c.now().Add(tokenMargin).Before(cached.expiry) {
		return cached.value, nil
	}
	tok, err := c.tokenRequest(ctx, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {refresh}})
	if err != nil {
		return "", err
	}
	c.remember(refresh, tok)
	return tok.AccessToken, nil
}

// do sends a request with the channel's access token. A 401 means the token we
// held no longer counts: it is dropped, so the next attempt gets a new one.
func (c *Client) do(ctx context.Context, refresh string, req *http.Request) (*http.Response, error) {
	token, err := c.access(ctx, refresh)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := c.http.Do(req.WithContext(ctx))
	if err != nil {
		return nil, &Error{Retry: true, Message: transportError(err)}
	}
	if resp.StatusCode == http.StatusUnauthorized {
		c.mu.Lock()
		delete(c.tokens, refresh)
		c.mu.Unlock()
	}
	return resp, nil
}

func (c *Client) getJSON(ctx context.Context, refresh, rawURL string, into any) error {
	ctx, cancel := context.WithTimeout(ctx, apiTimeout)
	defer cancel()
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	resp, err := c.do(ctx, refresh, req)
	if err != nil {
		return err
	}
	defer drain(resp)
	if resp.StatusCode >= 300 {
		return apiError(resp)
	}
	if err := json.NewDecoder(resp.Body).Decode(into); err != nil {
		return &Error{Retry: true, Message: "reading YouTube's answer: " + err.Error()}
	}
	return nil
}

// Channel is a YouTube channel as its owner sees it.
type Channel struct {
	ID     string
	Title  string
	Handle string // customUrl: "@8bitnewsus"
}

// MyChannel returns the channel a refresh token speaks for.
func (c *Client) MyChannel(ctx context.Context, refresh string) (Channel, error) {
	var out struct {
		Items []struct {
			ID      string `json:"id"`
			Snippet struct {
				Title     string `json:"title"`
				CustomURL string `json:"customUrl"`
			} `json:"snippet"`
		} `json:"items"`
	}
	if err := c.getJSON(ctx, refresh, c.cfg.APIBase+"/channels?part=id,snippet&mine=true", &out); err != nil {
		return Channel{}, err
	}
	if len(out.Items) == 0 {
		return Channel{}, &Error{Reason: "noChannel", Message: "this Google account has no YouTube channel"}
	}
	it := out.Items[0]
	return Channel{ID: it.ID, Title: it.Snippet.Title, Handle: it.Snippet.CustomURL}, nil
}

// Upload is what goes up: taken exactly as given, with nothing appended
// (Developer Policies III.C.3). The video is Private with a schedule, labelled
// as AI, and not made for kids.
type Upload struct {
	Title       string
	Description string
	Tags        []string
	Language    string // "en-US"
	PublishAt   time.Time
}

type videoSnippet struct {
	Title                string   `json:"title"`
	Description          string   `json:"description"`
	Tags                 []string `json:"tags,omitempty"`
	CategoryID           string   `json:"categoryId"`
	DefaultLanguage      string   `json:"defaultLanguage,omitempty"`
	DefaultAudioLanguage string   `json:"defaultAudioLanguage,omitempty"`
}

type videoStatus struct {
	PrivacyStatus           string `json:"privacyStatus"`
	PublishAt               string `json:"publishAt,omitempty"`
	SelfDeclaredMadeForKids bool   `json:"selfDeclaredMadeForKids"`
	ContainsSyntheticMedia  bool   `json:"containsSyntheticMedia"`
	License                 string `json:"license"`
	Embeddable              bool   `json:"embeddable"`
}

func privateStatus(publishAt time.Time) videoStatus {
	s := videoStatus{PrivacyStatus: "private", ContainsSyntheticMedia: true, License: "youtube", Embeddable: true}
	if !publishAt.IsZero() {
		s.PublishAt = publishAt.UTC().Format(time.RFC3339)
	}
	return s
}

// StartUpload opens a resumable upload and returns its session URI. Nothing
// exists on YouTube until all the bytes have gone up through it.
func (c *Client) StartUpload(ctx context.Context, refresh string, u Upload, size int64) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, apiTimeout)
	defer cancel()
	body, err := json.Marshal(struct {
		Snippet videoSnippet `json:"snippet"`
		Status  videoStatus  `json:"status"`
	}{videoSnippet{u.Title, u.Description, u.Tags, CategoryNews, u.Language, u.Language}, privateStatus(u.PublishAt)})
	if err != nil {
		return "", err
	}
	req, err := http.NewRequest(http.MethodPost, c.cfg.UploadBase+"/videos?uploadType=resumable&part=snippet,status", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json; charset=UTF-8")
	req.Header.Set("X-Upload-Content-Length", strconv.FormatInt(size, 10))
	req.Header.Set("X-Upload-Content-Type", "video/mp4")
	resp, err := c.do(ctx, refresh, req)
	if err != nil {
		return "", err
	}
	defer drain(resp)
	if resp.StatusCode >= 300 {
		return "", apiError(resp)
	}
	session := resp.Header.Get("Location")
	if session == "" {
		return "", &Error{Retry: true, Message: "YouTube opened an upload and gave no session address"}
	}
	return session, nil
}

// UploadState is where a session stands: Offset bytes have arrived, or, with
// VideoID set, the upload is finished. A session YouTube no longer knows gives
// ErrSessionGone.
type UploadState struct {
	Offset  int64
	VideoID string
}

// QueryUpload asks a session how far it got: it is how a restart learns that
// the video is already up, so it never uploads twice.
func (c *Client) QueryUpload(ctx context.Context, refresh, session string, size int64) (UploadState, error) {
	ctx, cancel := context.WithTimeout(ctx, apiTimeout)
	defer cancel()
	req, err := http.NewRequest(http.MethodPut, session, nil)
	if err != nil {
		return UploadState{}, err
	}
	req.Header.Set("Content-Range", fmt.Sprintf("bytes */%d", size))
	req.ContentLength = 0
	resp, err := c.do(ctx, refresh, req)
	if err != nil {
		return UploadState{}, err
	}
	defer drain(resp)
	return uploadAnswer(resp)
}

// SendBytes sends the video from offset to the end; body must already stand at
// offset. If the connection drops, QueryUpload says where to go on from.
func (c *Client) SendBytes(ctx context.Context, refresh, session string, body io.Reader, offset, size int64) (UploadState, error) {
	req, err := http.NewRequest(http.MethodPut, session, body)
	if err != nil {
		return UploadState{}, err
	}
	req.ContentLength = size - offset
	req.Header.Set("Content-Type", "video/mp4")
	if offset > 0 {
		req.Header.Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", offset, size-1, size))
	}
	resp, err := c.do(ctx, refresh, req)
	if err != nil {
		return UploadState{}, err
	}
	defer drain(resp)
	return uploadAnswer(resp)
}

func uploadAnswer(resp *http.Response) (UploadState, error) {
	switch {
	case resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusCreated:
		var video struct {
			ID string `json:"id"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&video); err != nil || video.ID == "" {
			return UploadState{}, &Error{Retry: true, Message: "YouTube finished an upload and gave no video id"}
		}
		return UploadState{VideoID: video.ID}, nil
	case resp.StatusCode == http.StatusPermanentRedirect: // 308: "resume incomplete"
		return UploadState{Offset: nextOffset(resp.Header.Get("Range"))}, nil
	case resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone:
		return UploadState{}, ErrSessionGone
	}
	return UploadState{}, apiError(resp)
}

// nextOffset reads "bytes=0-1048575": the next byte to send. No header: nothing
// has arrived.
func nextOffset(rangeHeader string) int64 {
	_, last, ok := strings.Cut(rangeHeader, "-")
	if !ok {
		return 0
	}
	n, err := strconv.ParseInt(strings.TrimSpace(last), 10, 64)
	if err != nil {
		return 0
	}
	return n + 1
}

// SetThumbnail sets a video's poster.
func (c *Client) SetThumbnail(ctx context.Context, refresh, videoID string, body io.Reader, size int64, contentType string) error {
	if size > MaxThumbnailBytes {
		return &Error{Reason: "thumbnailTooBig", Message: fmt.Sprintf("the poster is %d bytes; YouTube takes up to %d", size, MaxThumbnailBytes)}
	}
	ctx, cancel := context.WithTimeout(ctx, 5*apiTimeout)
	defer cancel()
	q := url.Values{"videoId": {videoID}, "uploadType": {"media"}}
	req, err := http.NewRequest(http.MethodPost, c.cfg.UploadBase+"/thumbnails/set?"+q.Encode(), body)
	if err != nil {
		return err
	}
	req.ContentLength = size
	req.Header.Set("Content-Type", contentType)
	resp, err := c.do(ctx, refresh, req)
	if err != nil {
		return err
	}
	defer drain(resp)
	if resp.StatusCode >= 300 {
		return apiError(resp)
	}
	return nil
}

// Video is what YouTube says about a video.
type Video struct {
	Found            bool
	UploadStatus     string // uploaded, processed, failed, rejected, deleted
	FailureReason    string
	RejectionReason  string
	PrivacyStatus    string
	PublishAt        *time.Time
	SyntheticMedia   *bool // nil: YouTube didn't say
	ProcessingStatus string
}

// Refused says why a video won't play, whatever its other fields say.
func (v Video) Refused() string {
	switch v.UploadStatus {
	case "failed":
		return "failed: " + v.FailureReason
	case "rejected":
		return "rejected: " + v.RejectionReason
	case "deleted":
		return "deleted"
	}
	if v.ProcessingStatus == "failed" {
		return "processing failed"
	}
	return ""
}

// GetVideo reads a video of the channel.
func (c *Client) GetVideo(ctx context.Context, refresh, videoID string) (Video, error) {
	var out struct {
		Items []struct {
			Status struct {
				UploadStatus           string `json:"uploadStatus"`
				FailureReason          string `json:"failureReason"`
				RejectionReason        string `json:"rejectionReason"`
				PrivacyStatus          string `json:"privacyStatus"`
				PublishAt              string `json:"publishAt"`
				ContainsSyntheticMedia *bool  `json:"containsSyntheticMedia"`
			} `json:"status"`
			ProcessingDetails struct {
				ProcessingStatus string `json:"processingStatus"`
			} `json:"processingDetails"`
		} `json:"items"`
	}
	q := url.Values{"part": {"status,processingDetails"}, "id": {videoID}}
	if err := c.getJSON(ctx, refresh, c.cfg.APIBase+"/videos?"+q.Encode(), &out); err != nil {
		return Video{}, err
	}
	if len(out.Items) == 0 {
		return Video{}, nil
	}
	s := out.Items[0].Status
	v := Video{Found: true, UploadStatus: s.UploadStatus, FailureReason: s.FailureReason, RejectionReason: s.RejectionReason,
		PrivacyStatus: s.PrivacyStatus, SyntheticMedia: s.ContainsSyntheticMedia, ProcessingStatus: out.Items[0].ProcessingDetails.ProcessingStatus}
	if t, err := time.Parse(time.RFC3339, s.PublishAt); err == nil {
		v.PublishAt = &t
	}
	return v, nil
}

// Unschedule keeps a video from going out: Private, with no time set. A video
// that is no longer there is already off the schedule.
func (c *Client) Unschedule(ctx context.Context, refresh, videoID string) error {
	ctx, cancel := context.WithTimeout(ctx, apiTimeout)
	defer cancel()
	body, err := json.Marshal(struct {
		ID     string      `json:"id"`
		Status videoStatus `json:"status"`
	}{videoID, privateStatus(time.Time{})})
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPut, c.cfg.APIBase+"/videos?part=status", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json; charset=UTF-8")
	resp, err := c.do(ctx, refresh, req)
	if err != nil {
		return err
	}
	defer drain(resp)
	if resp.StatusCode == http.StatusNotFound {
		return nil
	}
	if resp.StatusCode >= 300 {
		return apiError(resp)
	}
	return nil
}

// transportError leaves out the address: a session URI is as good as a key.
func transportError(err error) string {
	var ue *url.Error
	if errors.As(err, &ue) {
		return ue.Err.Error()
	}
	return err.Error()
}

func drain(resp *http.Response) {
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
	_ = resp.Body.Close()
}

// ErrAuthRevoked: Google no longer accepts the refresh token. Someone has to
// connect the channel again.
var ErrAuthRevoked = errors.New("youtube: the channel's access has been revoked")

// ErrSessionGone: YouTube no longer knows an upload session, and the upload
// didn't finish through it. Start another.
var ErrSessionGone = errors.New("youtube: the upload session is gone")

// Error is a refusal from YouTube, or a failure to reach it.
type Error struct {
	Status  int
	Reason  string // YouTube's own word: quotaExceeded
	Message string
	// Retry: worth trying again later, as it stands.
	Retry bool
}

func (e *Error) Error() string {
	if e.Reason != "" {
		return "youtube: " + e.Reason + ": " + e.Message
	}
	return "youtube: " + e.Message
}

// Retryable says whether an error may pass by itself: everything but a clear
// refusal, and a revoked access, which won't.
func Retryable(err error) bool {
	if errors.Is(err, ErrAuthRevoked) {
		return false
	}
	var e *Error
	if errors.As(err, &e) {
		return e.Retry
	}
	return true // a context cancelled, a body that wouldn't read
}

// retryReasons pass with time.
var retryReasons = map[string]bool{
	"quotaExceeded": true, "rateLimitExceeded": true, "userRateLimitExceeded": true, "dailyLimitExceeded": true,
	"uploadLimitExceeded": true, "servingLimitExceeded": true, "backendError": true, "internalError": true,
}

// apiError reads Google's error envelope. An API 401 is retryable: do has
// dropped the access token, and asking for a new one says if the access is gone.
func apiError(resp *http.Response) error {
	var body struct {
		Error struct {
			Message string `json:"message"`
			Errors  []struct {
				Reason string `json:"reason"`
			} `json:"errors"`
		} `json:"error"`
	}
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	_ = json.Unmarshal(raw, &body)
	e := &Error{Status: resp.StatusCode, Message: body.Error.Message}
	if e.Message == "" {
		e.Message = "answered " + resp.Status
	}
	if len(body.Error.Errors) > 0 {
		e.Reason = body.Error.Errors[0].Reason
	}
	e.Retry = resp.StatusCode >= 500 || resp.StatusCode == http.StatusTooManyRequests ||
		resp.StatusCode == http.StatusUnauthorized || retryReasons[e.Reason]
	return e
}
