package youtube

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func newClient(t *testing.T, h http.Handler) (*Client, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return New(Config{ClientID: "id", ClientSecret: "sec", RedirectURL: "https://hub.example/admin/youtube/callback",
		AuthURL: srv.URL + "/auth", TokenURL: srv.URL + "/token", RevokeURL: srv.URL + "/revoke",
		APIBase: srv.URL + "/api", UploadBase: srv.URL + "/upload"}), srv
}

func token(w http.ResponseWriter) {
	_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "acc", "expires_in": 3600})
}

func TestAuthCodeURLAsksForLastingAccessToEveryScope(t *testing.T) {
	c := New(Config{ClientID: "id", RedirectURL: "https://hub.example/cb"})
	u, err := url.Parse(c.AuthCodeURL("st"))
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	if u.Host != "accounts.google.com" || q.Get("access_type") != "offline" || q.Get("prompt") != "consent" ||
		q.Get("state") != "st" || q.Get("redirect_uri") != "https://hub.example/cb" || q.Get("response_type") != "code" {
		t.Errorf("auth url: %s", u)
	}
	for _, s := range []string{"youtube.upload", "youtube.force-ssl", "yt-analytics.readonly"} {
		if !strings.Contains(q.Get("scope"), s) {
			t.Errorf("scope %q lacks %s", q.Get("scope"), s)
		}
	}
}

func TestAccessTokenIsCachedUntilNearItsExpiry(t *testing.T) {
	var calls atomic.Int32
	c, _ := newClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			calls.Add(1)
			token(w)
			return
		}
		if r.Header.Get("Authorization") != "Bearer acc" {
			t.Errorf("authorization: %q", r.Header.Get("Authorization"))
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"items": []any{map[string]any{"id": "UC1", "snippet": map[string]any{"title": "T", "customUrl": "@t"}}}})
	}))
	now := time.Now()
	c.now = func() time.Time { return now }
	for i := 0; i < 2; i++ {
		ch, err := c.MyChannel(context.Background(), "r")
		if err != nil || ch.ID != "UC1" || ch.Handle != "@t" || ch.Title != "T" {
			t.Fatalf("MyChannel = %+v, %v", ch, err)
		}
	}
	if calls.Load() != 1 {
		t.Errorf("token asked for %d times, want once", calls.Load())
	}
	now = now.Add(time.Hour - 30*time.Second)
	if _, err := c.MyChannel(context.Background(), "r"); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 {
		t.Errorf("a nearly expired token was used: %d asks", calls.Load())
	}
}

func TestInvalidGrantMeansTheAccessIsRevoked(t *testing.T) {
	c, _ := newClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_grant","error_description":"Token has been expired or revoked."}`))
	}))
	_, err := c.MyChannel(context.Background(), "r")
	if !errors.Is(err, ErrAuthRevoked) || Retryable(err) {
		t.Errorf("err = %v, retryable %v", err, Retryable(err))
	}
}

func TestErrorKinds(t *testing.T) {
	for name, c := range map[string]struct {
		status int
		reason string
		retry  bool
	}{
		"quota":         {403, "quotaExceeded", true},
		"rate":          {403, "rateLimitExceeded", true},
		"upload limit":  {400, "uploadLimitExceeded", true},
		"server":        {503, "", true},
		"too many":      {429, "", true},
		"token dropped": {401, "authError", true},
		"bad title":     {400, "invalidTitle", false},
		"forbidden":     {403, "forbidden", false},
	} {
		s, _ := newClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/token" {
				token(w)
				return
			}
			w.WriteHeader(c.status)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"message": "no", "errors": []any{map[string]any{"reason": c.reason}}}})
		}))
		_, err := s.GetVideo(context.Background(), "r", "v")
		if err == nil || Retryable(err) != c.retry {
			t.Errorf("%s: err %v, retryable %v, want %v", name, err, Retryable(err), c.retry)
		}
	}
}

func TestAnUnreachableYouTubeIsRetryableAndHidesTheAddress(t *testing.T) {
	c, srv := newClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { token(w) }))
	session := srv.URL + "/upload/session?upload_id=SECRET"
	if _, err := c.access(context.Background(), "r"); err != nil {
		t.Fatal(err)
	}
	srv.Close()
	_, err := c.QueryUpload(context.Background(), "r", session, 10)
	if err == nil || !Retryable(err) {
		t.Fatalf("err = %v", err)
	}
	if strings.Contains(err.Error(), "SECRET") {
		t.Errorf("the session address got into an error: %v", err)
	}
}

func TestStartUploadSendsThePackageExactly(t *testing.T) {
	var body map[string]map[string]any
	var q url.Values
	var size, kind string
	c, srv := newClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			token(w)
			return
		}
		q, size, kind = r.URL.Query(), r.Header.Get("X-Upload-Content-Length"), r.Header.Get("X-Upload-Content-Type")
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Location", "http://"+r.Host+"/session/1")
	}))
	at := time.Date(2026, 11, 9, 23, 0, 0, 0, time.UTC)
	session, err := c.StartUpload(context.Background(), "r", Upload{Title: "T", Description: "D\nmore", Tags: []string{"a", "b"}, Language: "es-ES", PublishAt: at}, 42)
	if err != nil || session != srv.URL+"/session/1" {
		t.Fatalf("session %q, %v", session, err)
	}
	if q.Get("uploadType") != "resumable" || q.Get("part") != "snippet,status" || size != "42" || kind != "video/mp4" {
		t.Errorf("request: %v, %s, %s", q, size, kind)
	}
	sn, st := body["snippet"], body["status"]
	if sn["title"] != "T" || sn["description"] != "D\nmore" || sn["categoryId"] != "25" || sn["defaultLanguage"] != "es-ES" || sn["defaultAudioLanguage"] != "es-ES" {
		t.Errorf("snippet: %+v", sn)
	}
	if st["privacyStatus"] != "private" || st["publishAt"] != "2026-11-09T23:00:00Z" || st["selfDeclaredMadeForKids"] != false ||
		st["containsSyntheticMedia"] != true || st["license"] != "youtube" || st["embeddable"] != true {
		t.Errorf("status: %+v", st)
	}
}

func TestStartUploadWithNoSessionAddressIsRetryable(t *testing.T) {
	c, _ := newClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			token(w)
		}
	}))
	if _, err := c.StartUpload(context.Background(), "r", Upload{}, 1); err == nil || !Retryable(err) {
		t.Errorf("err = %v", err)
	}
}

func TestQueryUpload(t *testing.T) {
	var answer func(w http.ResponseWriter)
	var contentRange string
	c, srv := newClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			token(w)
			return
		}
		contentRange = r.Header.Get("Content-Range")
		answer(w)
	}))
	session := srv.URL + "/session/1"

	answer = func(w http.ResponseWriter) { w.Header().Set("Range", "bytes=0-99"); w.WriteHeader(http.StatusPermanentRedirect) }
	st, err := c.QueryUpload(context.Background(), "r", session, 500)
	if err != nil || st.Offset != 100 || st.VideoID != "" || contentRange != "bytes */500" {
		t.Errorf("partial: %+v, %v, %q", st, err, contentRange)
	}
	answer = func(w http.ResponseWriter) { w.WriteHeader(http.StatusPermanentRedirect) }
	if st, err := c.QueryUpload(context.Background(), "r", session, 500); err != nil || st.Offset != 0 {
		t.Errorf("nothing arrived: %+v, %v", st, err)
	}
	answer = func(w http.ResponseWriter) { w.WriteHeader(http.StatusCreated); _, _ = w.Write([]byte(`{"id":"vid"}`)) }
	if st, err := c.QueryUpload(context.Background(), "r", session, 500); err != nil || st.VideoID != "vid" {
		t.Errorf("finished: %+v, %v", st, err)
	}
	for _, status := range []int{http.StatusNotFound, http.StatusGone} {
		answer = func(w http.ResponseWriter) { w.WriteHeader(status) }
		if _, err := c.QueryUpload(context.Background(), "r", session, 500); !errors.Is(err, ErrSessionGone) {
			t.Errorf("%d: %v", status, err)
		}
	}
	answer = func(w http.ResponseWriter) { w.WriteHeader(http.StatusServiceUnavailable) }
	if _, err := c.QueryUpload(context.Background(), "r", session, 500); err == nil || !Retryable(err) {
		t.Errorf("503: %v", err)
	}
}

func TestSendBytesFromAnOffset(t *testing.T) {
	var got []byte
	var contentRange string
	var length int64
	c, srv := newClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			token(w)
			return
		}
		contentRange, length = r.Header.Get("Content-Range"), r.ContentLength
		got, _ = io.ReadAll(r.Body)
		_, _ = w.Write([]byte(`{"id":"vid"}`))
	}))
	st, err := c.SendBytes(context.Background(), "r", srv.URL+"/s", strings.NewReader("world"), 6, 11)
	if err != nil || st.VideoID != "vid" {
		t.Fatalf("%+v, %v", st, err)
	}
	if string(got) != "world" || contentRange != "bytes 6-10/11" || length != 5 {
		t.Errorf("sent %q, range %q, length %d", got, contentRange, length)
	}
	if _, err := c.SendBytes(context.Background(), "r", srv.URL+"/s", strings.NewReader("hello"), 0, 5); err != nil {
		t.Fatal(err)
	}
	if contentRange != "" {
		t.Errorf("a whole upload carries a range: %q", contentRange)
	}
}

func TestSetThumbnail(t *testing.T) {
	var q url.Values
	var kind string
	var got []byte
	c, _ := newClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			token(w)
			return
		}
		if r.URL.Path != "/upload/thumbnails/set" {
			t.Errorf("path %s", r.URL.Path)
		}
		q, kind = r.URL.Query(), r.Header.Get("Content-Type")
		got, _ = io.ReadAll(r.Body)
	}))
	if err := c.SetThumbnail(context.Background(), "r", "vid", strings.NewReader("png"), 3, "image/png"); err != nil {
		t.Fatal(err)
	}
	if q.Get("videoId") != "vid" || q.Get("uploadType") != "media" || kind != "image/png" || string(got) != "png" {
		t.Errorf("request: %v %s %q", q, kind, got)
	}
	err := c.SetThumbnail(context.Background(), "r", "vid", strings.NewReader(""), MaxThumbnailBytes+1, "image/png")
	if err == nil || Retryable(err) {
		t.Errorf("too big: %v", err)
	}
}

func TestGetVideo(t *testing.T) {
	var answer string
	c, _ := newClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			token(w)
			return
		}
		if q := r.URL.Query(); q.Get("id") != "vid" || q.Get("part") != "status,processingDetails" {
			t.Errorf("query %v", q)
		}
		_, _ = w.Write([]byte(answer))
	}))
	answer = `{"items":[{"status":{"uploadStatus":"uploaded","privacyStatus":"private","publishAt":"2026-11-09T23:00:00Z","containsSyntheticMedia":true}}]}`
	v, err := c.GetVideo(context.Background(), "r", "vid")
	if err != nil || !v.Found || v.PrivacyStatus != "private" || v.PublishAt == nil || v.PublishAt.Unix() != time.Date(2026, 11, 9, 23, 0, 0, 0, time.UTC).Unix() ||
		v.SyntheticMedia == nil || !*v.SyntheticMedia || v.Refused() != "" {
		t.Errorf("%+v, %v", v, err)
	}
	answer = `{"items":[]}`
	if v, err := c.GetVideo(context.Background(), "r", "vid"); err != nil || v.Found {
		t.Errorf("gone: %+v, %v", v, err)
	}
	answer = `{"items":[{"status":{"uploadStatus":"rejected","rejectionReason":"duplicate"}}]}`
	if v, err := c.GetVideo(context.Background(), "r", "vid"); err != nil || v.Refused() != "rejected: duplicate" || v.SyntheticMedia != nil {
		t.Errorf("rejected: %+v, %v", v, err)
	}
	answer = `{"items":[{"status":{"uploadStatus":"uploaded"},"processingDetails":{"processingStatus":"failed"}}]}`
	if v, err := c.GetVideo(context.Background(), "r", "vid"); err != nil || v.Refused() == "" {
		t.Errorf("processing failed: %+v, %v", v, err)
	}
}

func TestUnscheduleLeavesPrivateWithNoTime(t *testing.T) {
	var body map[string]any
	var status int
	c, _ := newClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			token(w)
			return
		}
		if r.Method != http.MethodPut || r.URL.Query().Get("part") != "status" {
			t.Errorf("%s %s", r.Method, r.URL)
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.WriteHeader(status)
	}))
	status = http.StatusOK
	if err := c.Unschedule(context.Background(), "r", "vid"); err != nil {
		t.Fatal(err)
	}
	st := body["status"].(map[string]any)
	if body["id"] != "vid" || st["privacyStatus"] != "private" || st["selfDeclaredMadeForKids"] != false || st["containsSyntheticMedia"] != true {
		t.Errorf("body: %+v", body)
	}
	if _, set := st["publishAt"]; set {
		t.Errorf("publishAt still sent: %+v", st)
	}
	status = http.StatusNotFound
	if err := c.Unschedule(context.Background(), "r", "vid"); err != nil {
		t.Errorf("a video no longer there: %v", err)
	}
	status = http.StatusForbidden
	if err := c.Unschedule(context.Background(), "r", "vid"); err == nil {
		t.Error("a refusal passed as success")
	}
}

func TestExchangeAndRevoke(t *testing.T) {
	var form url.Values
	var answer string
	c, _ := newClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		form = r.PostForm
		if r.URL.Path == "/revoke" {
			return
		}
		_, _ = w.Write([]byte(answer))
	}))
	answer = `{"access_token":"acc","refresh_token":"ref","expires_in":3600}`
	refresh, err := c.Exchange(context.Background(), "code")
	if err != nil || refresh != "ref" {
		t.Fatalf("%q, %v", refresh, err)
	}
	if form.Get("grant_type") != "authorization_code" || form.Get("code") != "code" || form.Get("client_secret") != "sec" ||
		form.Get("redirect_uri") != "https://hub.example/admin/youtube/callback" {
		t.Errorf("form: %v", form)
	}
	answer = `{"access_token":"acc","expires_in":3600}`
	if refresh, err := c.Exchange(context.Background(), "code"); err != nil || refresh != "" {
		t.Errorf("no refresh token: %q, %v", refresh, err)
	}
	if err := c.Revoke(context.Background(), "ref"); err != nil || form.Get("token") != "ref" {
		t.Errorf("revoke: %v, %v", err, form)
	}
}

func TestATokenErrorNeverCarriesTheBody(t *testing.T) {
	c, _ := newClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_client","refresh_token":"SECRET"}`))
	}))
	_, err := c.Exchange(context.Background(), "code")
	if err == nil || strings.Contains(err.Error(), "SECRET") || !strings.Contains(err.Error(), "invalid_client") {
		t.Errorf("err = %v", err)
	}
}

func TestSealerKeepsTheTokenOutOfTheDatabase(t *testing.T) {
	s, err := NewSealer(strings.Repeat("ab", 32))
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := s.Seal("1//refresh", "8bitnews-us")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(sealed, "refresh") {
		t.Errorf("not encrypted: %q", sealed)
	}
	if again, _ := s.Seal("1//refresh", "8bitnews-us"); again == sealed {
		t.Error("the same token seals to the same text")
	}
	if plain, err := s.Open(sealed, "8bitnews-us"); err != nil || plain != "1//refresh" {
		t.Errorf("Open = %q, %v", plain, err)
	}
	if _, err := s.Open(sealed, "8bitnews-es"); err == nil {
		t.Error("a token opened for another channel")
	}
	other, _ := NewSealer(strings.Repeat("cd", 32))
	if _, err := other.Open(sealed, "8bitnews-us"); err == nil {
		t.Error("a token opened with another key")
	}
	if _, err := s.Open("not base64!", "8bitnews-us"); err == nil {
		t.Error("garbage opened")
	}
	for _, bad := range []string{"", "abcd", strings.Repeat("zz", 32)} {
		if _, err := NewSealer(bad); err == nil {
			t.Errorf("key %q accepted", bad)
		}
	}
}
