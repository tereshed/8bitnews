package admin

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/tereshed/8bitnews/internal/publish"
	"github.com/tereshed/8bitnews/internal/store"
)

const (
	rel1 = "7f01440f-4314-4406-9667-7b9c49d4d973"
	host = "hub.example.org"
)

type fakeStore struct {
	release  store.Release
	rows     []store.AdminRelease
	progress []store.InProgress
	link     string
	setBy    string
	epoch    int
	story    string
	account  *store.PlatformAccount
	// channelPlatforms replaces the channel's platforms; empty: YouTube on.
	channelPlatforms string
}

func (f *fakeStore) AdminSessionEpoch(context.Context, int64) (int, error) { return f.epoch, nil }
func (f *fakeStore) RaiseAdminSessionEpoch(context.Context, int64) error {
	f.epoch++
	return nil
}

func (f *fakeStore) AdminReleases(context.Context, int) ([]store.AdminRelease, error) {
	return f.rows, nil
}

func (f *fakeStore) DevelopmentsInProgress(context.Context, time.Duration) ([]store.InProgress, error) {
	return f.progress, nil
}

func (f *fakeStore) GetRelease(_ context.Context, id string) (store.Release, error) {
	if _, err := uuid.Parse(id); err != nil {
		return store.Release{}, errors.New("ERROR: invalid input syntax for type uuid") // what Postgres does
	}
	if id != f.release.ID {
		return store.Release{}, store.ErrNotFound
	}
	return f.release, nil
}

func (f *fakeStore) ReleaseContextOf(context.Context, string) (store.ReleaseContext, error) {
	title := f.story
	if title == "" {
		title = "The story"
	}
	return store.ReleaseContext{StoryTitle: title, DevelopmentSummary: "What is new"}, nil
}

func (f *fakeStore) ListReleaseSources(context.Context, string) ([]store.ReleaseSource, error) {
	return []store.ReleaseSource{{URL: "https://example.com/a", Outlet: "Example", ArchiveURL: "https://web.archive.org/a"}, {URL: "https://example.com/b", Outlet: "Other"}}, nil
}

func (f *fakeStore) GetChannel(_ context.Context, id string) (store.Channel, error) {
	if f.channelPlatforms != "" {
		return store.Channel{ID: id, Name: "8BitNewsUS", Platforms: json.RawMessage(f.channelPlatforms)}, nil
	}
	return store.Channel{ID: "8bitnews-es", MetadataMD: "## YouTube Shorts\n\n```\n{summary}\n\n{sources_block}\n\n{hashtags}\n```\n\n**Настройки:**\n- категория News\n",
		Platforms: json.RawMessage(`{"youtube":{"enabled":true}}`)}, nil
}

func (f *fakeStore) GetCountry(context.Context, string) (store.Country, error) {
	return store.Country{}, store.ErrNotFound
}

func (f *fakeStore) PublicationURL(context.Context, string, string) (string, error) {
	return f.link, nil
}

func (f *fakeStore) SetPublicationURL(_ context.Context, _, _, link, by string) error {
	f.link, f.setBy = link, by
	return nil
}

type actCall struct {
	release, action, who, note string
	userID                     int64
}

type fakeModerator struct {
	moderators map[int64]bool
	unchanged  bool
	calls      []actCall
	channel    string // of the release Act answers with
}

func (f *fakeModerator) IsModerator(_ context.Context, id int64) bool { return f.moderators[id] }

func (f *fakeModerator) Act(_ context.Context, release, action, who string, id int64, note string) (store.Release, bool, error) {
	f.calls = append(f.calls, actCall{release, action, who, note, id})
	return store.Release{ID: release, ChannelID: f.channel}, !f.unchanged, nil
}

type fakeMedia struct{}

func (fakeMedia) PresignGet(_ context.Context, key string) (string, error) {
	return "https://bucket.r2.example/" + key + "?sig=secret", nil
}

type fakeVideos struct {
	state     string
	forgotten []string
}

func (f *fakeVideos) State(context.Context, string) string { return f.state }
func (f *fakeVideos) Forget(l string)                      { f.forgotten = append(f.forgotten, l) }

type fixture struct {
	h     *Handler
	store *fakeStore
	mod   *fakeModerator
	vids  *fakeVideos
	auth  *Auth
	me    Identity
}

func newFixture(status string) *fixture {
	me := Identity{UserID: 35, Name: "Kostya (@kteresh)"}
	st := &fakeStore{release: store.Release{
		ID: rel1, ChannelID: "8bitnews-es", Status: status, AllowedPlatforms: []string{"youtube"},
		Media:        json.RawMessage(`{"video":"releases/dev/video.mp4","cover":"releases/dev/cover.png"}`),
		VideoPackage: json.RawMessage(`{"media":{"video_no_music":"releases/dev/video_no_music.mp4"}}`),
		Script:       json.RawMessage(`{"metadata":{"youtube":{"title":"Un título","summary":"Resumen <b>sin HTML</b>","hashtags":["#8BitNews"],"tags":["a","b"]}}}`),
	}}
	mod := &fakeModerator{moderators: map[int64]bool{me.UserID: true}}
	vids := &fakeVideos{state: videoVisible}
	auth := NewAuth("123:SECRET")
	h := New(Deps{Store: st, Moderator: mod, Media: fakeMedia{}, Auth: auth, Videos: vids,
		Publish:     publish.Config{PublicURL: "https://" + host},
		MediaOrigin: "https://acc.eu.r2.cloudflarestorage.com",
		Log:         slog.New(slog.NewTextHandler(io.Discard, nil))})
	return &fixture{h: h, store: st, mod: mod, vids: vids, auth: auth, me: me}
}

func (f *fixture) get(path string, signedIn bool) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "https://"+host+path, nil)
	if signedIn {
		req.AddCookie(&http.Cookie{Name: cookieName, Value: f.auth.Session(f.me)})
	}
	rec := httptest.NewRecorder()
	f.h.ServeHTTP(rec, req)
	return rec
}

func (f *fixture) post(path string, form url.Values, signedIn bool, headers map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "https://"+host+path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if headers == nil {
		headers = map[string]string{"Sec-Fetch-Site": "same-origin"}
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	if signedIn {
		req.AddCookie(&http.Cookie{Name: cookieName, Value: f.auth.Session(f.me)})
	}
	rec := httptest.NewRecorder()
	f.h.ServeHTTP(rec, req)
	return rec
}

func TestSignedOutSeesNothing(t *testing.T) {
	f := newFixture("pending_approval")
	f.store.rows = []store.AdminRelease{{ID: rel1, Title: "Secret video title", Status: "pending_approval"}}
	for _, path := range []string{"/admin", "/admin/r/" + rel1, "/admin/media/" + rel1 + "/cover"} {
		rec := f.get(path, false)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s: status %d", path, rec.Code)
		}
		body := rec.Body.String()
		if strings.Contains(body, "Secret video title") || strings.Contains(body, "r2.example") {
			t.Errorf("%s leaks to a signed-out visitor: %s", path, body)
		}
		if !strings.Contains(body, "/admin") {
			t.Errorf("%s doesn't say how to sign in", path)
		}
	}
	rec := f.post("/admin/r/"+rel1+"/act", url.Values{"action": {"approve"}}, false, nil)
	if rec.Code != http.StatusUnauthorized || len(f.mod.calls) != 0 {
		t.Errorf("a signed-out approval: status %d, calls %v", rec.Code, f.mod.calls)
	}
}

func TestForgedOrWrongKindCookieIsRefused(t *testing.T) {
	f := newFixture("pending_approval")
	other := NewAuth("another token")
	login := f.auth.LoginToken(f.me)
	for name, value := range map[string]string{
		"signed with another key": other.Session(f.me),
		"a login token":           login,
		"garbage":                 "abc.def",
		"tampered":                f.auth.Session(f.me) + "x",
	} {
		req := httptest.NewRequest(http.MethodGet, "https://"+host+"/admin", nil)
		req.AddCookie(&http.Cookie{Name: cookieName, Value: value})
		rec := httptest.NewRecorder()
		f.h.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s: status %d", name, rec.Code)
		}
	}
}

func TestSessionExpires(t *testing.T) {
	f := newFixture("pending_approval")
	session := f.auth.Session(f.me)
	f.auth.now = func() time.Time { return time.Now().Add(sessionTTL + time.Minute) }
	if _, err := f.auth.CheckSession(session); err == nil {
		t.Error("an expired session is accepted")
	}
}

func TestRemovedModeratorLosesAccess(t *testing.T) {
	f := newFixture("pending_approval")
	f.mod.moderators = map[int64]bool{}
	if rec := f.get("/admin", true); rec.Code != http.StatusForbidden {
		t.Errorf("status %d", rec.Code)
	}
	rec := f.post("/admin/r/"+rel1+"/act", url.Values{"action": {"approve"}}, true, nil)
	if rec.Code != http.StatusForbidden || len(f.mod.calls) != 0 {
		t.Errorf("status %d, calls %v", rec.Code, f.mod.calls)
	}
}

func TestLoginLink(t *testing.T) {
	f := newFixture("pending_approval")
	token := f.auth.LoginToken(f.me)

	// Opening the link, as a link preview does, doesn't use it up.
	for i := 0; i < 2; i++ {
		rec := f.get("/admin/login?t="+url.QueryEscape(token), false)
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Войти как Kostya") {
			t.Fatalf("opening the link: %d %s", rec.Code, rec.Body.String())
		}
		if got := rec.Header().Get("Set-Cookie"); got != "" {
			t.Fatalf("opening the link signs in: %s", got)
		}
	}

	rec := f.post("/admin/login", url.Values{"t": {token}}, false, nil)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("login: %d", rec.Code)
	}
	cookie := rec.Result().Cookies()
	if len(cookie) != 1 || !cookie[0].HttpOnly || !cookie[0].Secure || cookie[0].SameSite != http.SameSiteLaxMode || cookie[0].Path != "/admin" {
		t.Fatalf("cookie: %+v", cookie)
	}
	if id, err := f.auth.CheckSession(cookie[0].Value); err != nil || id != f.me {
		t.Fatalf("session: %+v %v", id, err)
	}

	// Once only: the second press, and opening it again, are refused.
	if rec := f.post("/admin/login", url.Values{"t": {token}}, false, nil); rec.Code != http.StatusUnauthorized || len(rec.Result().Cookies()) != 0 {
		t.Errorf("a spent link: %d %v", rec.Code, rec.Result().Cookies())
	}
	if rec := f.get("/admin/login?t="+url.QueryEscape(token), false); rec.Code != http.StatusUnauthorized {
		t.Errorf("opening a spent link: %d", rec.Code)
	}
}

func TestLoginRefusals(t *testing.T) {
	f := newFixture("pending_approval")
	expired := f.auth.LoginToken(f.me)
	f.auth.now = func() time.Time { return time.Now().Add(loginTTL + time.Minute) }
	if rec := f.post("/admin/login", url.Values{"t": {expired}}, false, nil); rec.Code != http.StatusUnauthorized {
		t.Errorf("an expired link: %d", rec.Code)
	}
	f.auth.now = time.Now

	if rec := f.post("/admin/login", url.Values{"t": {f.auth.Session(f.me)}}, false, nil); rec.Code != http.StatusUnauthorized {
		t.Errorf("a session used as a link: %d", rec.Code)
	}
	stranger := f.auth.LoginToken(Identity{UserID: 99, Name: "Stranger"})
	rec := f.post("/admin/login", url.Values{"t": {stranger}}, false, nil)
	if rec.Code != http.StatusForbidden || len(rec.Result().Cookies()) != 0 {
		t.Errorf("not a moderator: %d %v", rec.Code, rec.Result().Cookies())
	}
}

func TestPostsFromOtherSitesAreRefused(t *testing.T) {
	f := newFixture("pending_approval")
	form := url.Values{"action": {"approve"}}
	for name, headers := range map[string]map[string]string{
		"cross-site":             {"Sec-Fetch-Site": "cross-site", "Origin": "https://evil.example"},
		"same-site subdomain":    {"Sec-Fetch-Site": "same-site"},
		"other origin, no fetch": {"Origin": "https://evil.example"},
		"null origin":            {"Origin": "null"},
		"no headers at all":      {"X-None": "1"},
	} {
		if rec := f.post("/admin/r/"+rel1+"/act", form, true, headers); rec.Code != http.StatusForbidden {
			t.Errorf("%s: status %d", name, rec.Code)
		}
	}
	if len(f.mod.calls) != 0 {
		t.Errorf("acted on a forged post: %v", f.mod.calls)
	}
	if rec := f.post("/admin/r/"+rel1+"/act", form, true, map[string]string{"Origin": "https://" + host}); rec.Code != http.StatusSeeOther {
		t.Errorf("same origin without Sec-Fetch-Site: %d", rec.Code)
	}
}

func TestActGoesThroughTheBotSignedBySession(t *testing.T) {
	f := newFixture("pending_approval")
	rec := f.post("/admin/r/"+rel1+"/act", url.Values{"action": {"approve"}, "back": {"list"}}, true, nil)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/admin?msg=approve" {
		t.Fatalf("%d %s", rec.Code, rec.Header().Get("Location"))
	}
	want := actCall{rel1, "approve", f.me.Name, "", f.me.UserID}
	if len(f.mod.calls) != 1 || f.mod.calls[0] != want {
		t.Errorf("calls: %+v", f.mod.calls)
	}
}

func TestActRefusals(t *testing.T) {
	f := newFixture("pending_approval")
	if rec := f.post("/admin/r/"+rel1+"/act", url.Values{"action": {"delete_everything"}}, true, nil); rec.Code != http.StatusBadRequest {
		t.Errorf("unknown action: %d", rec.Code)
	}
	for _, action := range []string{"changes", "reject"} {
		rec := f.post("/admin/r/"+rel1+"/act", url.Values{"action": {action}, "note": {"   "}}, true, nil)
		if !strings.Contains(rec.Header().Get("Location"), "msg=need_reason") {
			t.Errorf("%s with no reason: %s", action, rec.Header().Get("Location"))
		}
	}
	if len(f.mod.calls) != 0 {
		t.Errorf("acted: %v", f.mod.calls)
	}
	f.mod.unchanged = true
	rec := f.post("/admin/r/"+rel1+"/act", url.Values{"action": {"approve"}}, true, nil)
	if !strings.Contains(rec.Header().Get("Location"), "msg=already") {
		t.Errorf("an already decided release: %s", rec.Header().Get("Location"))
	}
}

func TestListGroupsByWhatIsNext(t *testing.T) {
	f := newFixture("approved")
	yt := []string{"youtube"}
	f.store.rows = []store.AdminRelease{
		{ID: "r-pending", ChannelID: "8bitnews-us", Status: "pending_approval", Title: "To decide", CoverKey: "releases/x/c.png", CreatedAt: time.Now(), AllowedPlatforms: yt},
		{ID: "r-upload", ChannelID: "8bitnews-es", Status: "approved", Title: "To upload", CreatedAt: time.Now(), AllowedPlatforms: yt},
		{ID: "r-confirm", ChannelID: "8bitnews-es", Status: "approved", Title: "To confirm", YouTubeURL: shortsPrefix + "fATVaPVaQOI", CreatedAt: time.Now(), AllowedPlatforms: yt},
		{ID: "r-live", ChannelID: "8bitnews-us", Status: "published", Title: "Is live", YouTubeURL: shortsPrefix + "959ILU_-TF4", CreatedAt: time.Now(), AllowedPlatforms: yt},
		{ID: "r-closed", ChannelID: "8bitnews-us", Status: "rejected", Title: "Was rejected", CreatedAt: time.Now(), AllowedPlatforms: yt},
	}
	f.store.progress = []store.InProgress{{ChannelID: "8bitnews-us", StoryTitle: "A story being made", CreatedAt: time.Now()}}
	rec := f.get("/admin", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	body := rec.Body.String()
	order := []string{"Решить · 1", "To decide", "Загрузить на YouTube · 1", "To upload", "Подтвердить выход · 1", "To confirm", "Вышли · 1", "Is live", "В работе · 1", "A story being made", "Закрытые · 1", "Was rejected"}
	at := 0
	for _, want := range order {
		i := strings.Index(body[at:], want)
		if i < 0 {
			t.Fatalf("%q missing or out of order in:\n%s", want, body)
		}
		at += i
	}
	if !strings.Contains(body, `https://www.youtube.com/shorts/fATVaPVaQOI`) || !strings.Contains(body, "виден по ссылке") {
		t.Errorf("no YouTube link or state:\n%s", body)
	}
	if !strings.Contains(body, `/admin/media/r-pending/cover`) {
		t.Errorf("no cover")
	}

	// Filtered to a channel: only its rows.
	body = f.get("/admin?channel=8bitnews-es", true).Body.String()
	if strings.Contains(body, "To decide") || strings.Contains(body, "A story being made") || !strings.Contains(body, "To upload") {
		t.Errorf("the channel filter doesn't filter:\n%s", body)
	}
}

func TestDetailShowsPackageEscaped(t *testing.T) {
	f := newFixture("approved")
	body := f.get("/admin/r/"+rel1, true).Body.String()
	for _, want := range []string{"Un título", "🟢 Вышел", "/admin/media/" + rel1 + "/upload", "Источников 2, в архиве 1", "https://" + host + "/s/7f01440f43", `data-copy="p-desc"`, "a, b"} {
		if !strings.Contains(body, want) {
			t.Errorf("no %q in:\n%s", want, body)
		}
	}
	if strings.Contains(body, "<b>sin HTML</b>") {
		t.Error("a script's text goes into the page unescaped")
	}
	if strings.Contains(body, "sig=secret") {
		t.Error("the page's HTML carries a presigned address")
	}
}

func TestDetailButtonsFollowStatus(t *testing.T) {
	for status, c := range map[string]struct{ has, hasnt []string }{
		"pending_approval": {[]string{"✅ Одобрить", "Вернуть на доработку", "Отклонить"}, []string{"🟢", "Закрыть страницу"}},
		"approved":         {[]string{"🟢", "Закрыть страницу"}, []string{"✅ Одобрить", "↩️"}},
		"published":        {[]string{"↩️", "Закрыть страницу"}, []string{"🟢", "✅ Одобрить"}},
		"rejected":         {nil, []string{"🟢", "✅ Одобрить", "↩️", "Закрыть страницу"}},
	} {
		body := newFixture(status).get("/admin/r/"+rel1, true).Body.String()
		for _, want := range c.has {
			if !strings.Contains(body, want) {
				t.Errorf("%s: no %q", status, want)
			}
		}
		for _, unwanted := range c.hasnt {
			if strings.Contains(body, unwanted) {
				t.Errorf("%s: has %q", status, unwanted)
			}
		}
	}
}

func TestSetLink(t *testing.T) {
	f := newFixture("approved")
	rec := f.post("/admin/r/"+rel1+"/link", url.Values{"url": {"https://youtube.com/shorts/fATVaPVaQOI?feature=share"}}, true, nil)
	if !strings.Contains(rec.Header().Get("Location"), "msg=link_saved") {
		t.Fatalf("%s", rec.Header().Get("Location"))
	}
	if f.store.link != shortsPrefix+"fATVaPVaQOI" || f.store.setBy != f.me.Name {
		t.Errorf("stored %q by %q", f.store.link, f.store.setBy)
	}
	if len(f.vids.forgotten) != 1 {
		t.Errorf("the state cache kept the old answer: %v", f.vids.forgotten)
	}

	rec = f.post("/admin/r/"+rel1+"/link", url.Values{"url": {"https://evil.example/shorts/fATVaPVaQOI"}}, true, nil)
	if !strings.Contains(rec.Header().Get("Location"), "msg=bad_link") || f.store.link != shortsPrefix+"fATVaPVaQOI" {
		t.Errorf("a foreign link: %s, stored %q", rec.Header().Get("Location"), f.store.link)
	}

	f.post("/admin/r/"+rel1+"/link", url.Values{"url": {""}}, true, nil)
	if f.store.link != "" {
		t.Errorf("an empty field doesn't clear the link: %q", f.store.link)
	}

	f = newFixture("pending_approval")
	rec = f.post("/admin/r/"+rel1+"/link", url.Values{"url": {"fATVaPVaQOI"}}, true, nil)
	if !strings.Contains(rec.Header().Get("Location"), "msg=not_approved") || f.store.link != "" {
		t.Errorf("before approval: %s, stored %q", rec.Header().Get("Location"), f.store.link)
	}
}

func TestMedia(t *testing.T) {
	f := newFixture("approved")
	for kind, key := range map[string]string{"cover": "releases/dev/cover.png", "video": "releases/dev/video.mp4", "upload": "releases/dev/video_no_music.mp4"} {
		rec := f.get("/admin/media/"+rel1+"/"+kind, true)
		if rec.Code != http.StatusFound || !strings.Contains(rec.Header().Get("Location"), key) {
			t.Errorf("%s: %d %s", kind, rec.Code, rec.Header().Get("Location"))
		}
	}
	if rec := f.get("/admin/media/"+rel1+"/script", true); rec.Code != http.StatusNotFound {
		t.Errorf("an unknown kind: %d", rec.Code)
	}
	f.store.release.Media = json.RawMessage(`{"cover":"backups/secret.tar"}`)
	if rec := f.get("/admin/media/"+rel1+"/cover", true); rec.Code != http.StatusNotFound {
		t.Errorf("a key outside releases/: %d", rec.Code)
	}
	if rec := f.get("/admin/media/00000000-0000-0000-0000-000000000000/cover", true); rec.Code != http.StatusNotFound {
		t.Errorf("an unknown release: %d", rec.Code)
	}
}

func TestMalformedReleaseIDIsNotFound(t *testing.T) {
	f := newFixture("approved")
	for _, path := range []string{"/admin/r/not-a-uuid", "/admin/media/not-a-uuid/cover"} {
		if rec := f.get(path, true); rec.Code != http.StatusNotFound {
			t.Errorf("GET %s: %d", path, rec.Code)
		}
	}
	for _, path := range []string{"/admin/r/not-a-uuid/act", "/admin/r/not-a-uuid/link"} {
		if rec := f.post(path, url.Values{"action": {"approve"}, "url": {"x"}}, true, nil); rec.Code != http.StatusNotFound {
			t.Errorf("POST %s: %d", path, rec.Code)
		}
	}
	if len(f.mod.calls) != 0 {
		t.Errorf("acted: %v", f.mod.calls)
	}
}

func TestSigningOutEndsEverySession(t *testing.T) {
	f := newFixture("approved")
	req := httptest.NewRequest(http.MethodPost, "https://"+host+"/admin/logout", nil)
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	session := f.auth.Session(f.me)
	req.AddCookie(&http.Cookie{Name: cookieName, Value: session})
	rec := httptest.NewRecorder()
	f.h.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("logout: %d", rec.Code)
	}
	if c := rec.Result().Cookies(); len(c) != 1 || c[0].MaxAge >= 0 {
		t.Errorf("the cookie stays: %+v", c)
	}
	// The same cookie, from a copy elsewhere, no longer works.
	req = httptest.NewRequest(http.MethodGet, "https://"+host+"/admin", nil)
	req.AddCookie(&http.Cookie{Name: cookieName, Value: session})
	rec = httptest.NewRecorder()
	f.h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("a signed-out session still works: %d", rec.Code)
	}
	// A new login does.
	f.me.Epoch = f.store.epoch
	if rec := f.get("/admin", true); rec.Code != http.StatusOK {
		t.Errorf("a new session: %d", rec.Code)
	}
}

func TestLoginCarriesTheCurrentEpoch(t *testing.T) {
	f := newFixture("approved")
	f.store.epoch = 3
	rec := f.post("/admin/login", url.Values{"t": {f.auth.LoginToken(f.me)}}, false, nil)
	id, err := f.auth.CheckSession(rec.Result().Cookies()[0].Value)
	if err != nil || id.Epoch != 3 {
		t.Errorf("%+v %v", id, err)
	}
}

func TestLegalsPlatformListIsRespected(t *testing.T) {
	f := newFixture("approved")
	f.store.release.AllowedPlatforms = []string{"tiktok"}
	f.store.rows = []store.AdminRelease{{ID: rel1, ChannelID: "8bitnews-es", Status: "approved", Title: "Barred", CreatedAt: time.Now(), AllowedPlatforms: []string{"tiktok"}}}
	body := f.get("/admin", true).Body.String()
	if !strings.Contains(body, "Legal не допустил YouTube") || !strings.Contains(body, "Допущены: tiktok") {
		t.Errorf("the list doesn't say YouTube is barred:\n%s", body)
	}
	if strings.Contains(body, `name="url"`) {
		t.Errorf("the list asks for a YouTube link anyway")
	}
	body = f.get("/admin/r/"+rel1, true).Body.String()
	if !strings.Contains(body, "не допустил этот ролик на YouTube") || strings.Contains(body, `name="url"`) || strings.Contains(body, "Пакет для загрузки на YouTube") {
		t.Errorf("the page offers YouTube anyway:\n%s", body)
	}
	for _, want := range []string{"🟢", "Закрыть страницу"} {
		if !strings.Contains(body, want) {
			t.Errorf("a barred video lost %q", want)
		}
	}
	rec := f.post("/admin/r/"+rel1+"/link", url.Values{"url": {"fATVaPVaQOI"}}, true, nil)
	if !strings.Contains(rec.Header().Get("Location"), "msg=barred") || f.store.link != "" {
		t.Errorf("a link was stored: %s, %q", rec.Header().Get("Location"), f.store.link)
	}
}

func TestStoredTextCantRunAsScript(t *testing.T) {
	f := newFixture("approved")
	const attack = `<script>alert(1)</script>`
	f.store.story = attack
	f.store.rows = []store.AdminRelease{{ID: rel1, ChannelID: "8bitnews-es", Status: "pending_approval", Title: attack, StoryTitle: attack, CreatedAt: time.Now()}}
	f.store.progress = []store.InProgress{{ChannelID: "x", StoryTitle: attack, CreatedAt: time.Now()}}
	for _, path := range []string{"/admin", "/admin/r/" + rel1} {
		body := f.get(path, true).Body.String()
		if strings.Contains(body, attack) {
			t.Errorf("%s: stored text runs as HTML", path)
		}
		if !strings.Contains(body, "&lt;script&gt;") {
			t.Errorf("%s: the text isn't on the page at all", path)
		}
	}
}

func TestSecurityHeaders(t *testing.T) {
	f := newFixture("approved")
	for _, rec := range []*httptest.ResponseRecorder{f.get("/admin", true), f.get("/admin", false)} {
		csp := rec.Header().Get("Content-Security-Policy")
		for _, want := range []string{"default-src 'none'", "frame-ancestors 'none'", "script-src 'nonce-", "form-action 'self'", "https://*.acc.eu.r2.cloudflarestorage.com"} {
			if !strings.Contains(csp, want) {
				t.Errorf("CSP %q has no %q", csp, want)
			}
		}
		if _, scripts, _ := strings.Cut(csp, "script-src "); strings.Contains(strings.SplitN(scripts, ";", 2)[0], "unsafe") {
			t.Errorf("CSP lets inline scripts run: %q", csp)
		}
		if rec.Header().Get("Referrer-Policy") != "same-origin" {
			t.Errorf("a browser without Sec-Fetch-Site would post Origin: null: %q", rec.Header().Get("Referrer-Policy"))
		}
		if rec.Header().Get("Cache-Control") != "no-store" || rec.Header().Get("X-Robots-Tag") == "" {
			t.Errorf("headers: %v", rec.Header())
		}
	}
}

func TestParseYouTubeURL(t *testing.T) {
	const id = "fATVaPVaQOI"
	want := shortsPrefix + id
	for _, in := range []string{
		id,
		"https://www.youtube.com/shorts/" + id,
		"https://youtube.com/shorts/" + id + "?feature=share",
		"http://m.youtube.com/watch?v=" + id + "&t=3",
		"youtu.be/" + id,
		"https://studio.youtube.com/video/" + id + "/edit",
		"  https://youtu.be/" + id + "  ",
	} {
		if got, err := ParseYouTubeURL(in); err != nil || got != want {
			t.Errorf("%q: %q %v", in, got, err)
		}
	}
	for _, in := range []string{
		"", "hello", "https://evil.example/shorts/" + id, "https://youtube.com.evil.example/shorts/" + id,
		"https://youtube.com/@8BitNewsES", "https://youtube.com/shorts/tooshort", "javascript:alert(1)",
		"https://www.youtube.com/watch?v=" + id + "x", "ftp://youtube.com/shorts/" + id,
	} {
		if got, err := ParseYouTubeURL(in); err == nil {
			t.Errorf("%q accepted as %q", in, got)
		}
	}
}

func TestVideoChecker(t *testing.T) {
	var asked int
	status := http.StatusOK
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked++
		if r.URL.Query().Get("url") == "" {
			t.Errorf("no url in %s", r.URL)
		}
		w.WriteHeader(status)
	}))
	defer srv.Close()
	c := &VideoChecker{base: srv.URL, client: srv.Client(), cache: map[string]videoState{}}
	ctx := context.Background()
	link := shortsPrefix + "fATVaPVaQOI"

	if got := c.State(ctx, link); got != videoVisible {
		t.Errorf("200: %q", got)
	}
	if c.State(ctx, link); asked != 1 {
		t.Errorf("not cached: asked %d times", asked)
	}
	c.Forget(link)
	status = http.StatusForbidden
	if got := c.State(ctx, link); got != videoHidden {
		t.Errorf("403: %q", got)
	}
	// A hidden video going public shows up soon, without touching the link.
	c.mu.Lock()
	c.cache[link] = videoState{state: videoHidden, at: time.Now().Add(-videoHiddenTTL - time.Second)}
	c.mu.Unlock()
	status = http.StatusOK
	if got := c.State(ctx, link); got != videoVisible {
		t.Errorf("after going public: %q", got)
	}
	c.Forget(link)
	status = http.StatusTooManyRequests
	if got := c.State(ctx, link); got != videoUnknown {
		t.Errorf("429: %q", got)
	}
	asked = 0
	c.State(ctx, link)
	if asked != 1 {
		t.Errorf("an unknown answer was cached")
	}
}

func TestStrayTrailingSlash(t *testing.T) {
	f := newFixture("approved")
	if rec := f.get("/admin/", true); rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/admin" {
		t.Errorf("%d %s", rec.Code, rec.Header().Get("Location"))
	}
}

// After its slot a video that should have gone out is a person's to look at.
func TestAnAutomaticVideoPastItsSlotGoesToTheSectionWithButtons(t *testing.T) {
	f := newFixture("approved")
	past, future := time.Now().Add(-time.Hour), time.Now().Add(time.Hour)
	row := func(id, title string, slot *time.Time) store.AdminRelease {
		return store.AdminRelease{ID: id, ChannelID: "8bitnews-es", Status: "approved", Title: title, CreatedAt: time.Now(),
			ModeratedBy: store.AutoApprover, AllowedPlatforms: []string{"youtube"}, YouTubeURL: "https://www.youtube.com/shorts/abcdefghijk", SlotAt: slot}
	}
	f.store.rows = []store.AdminRelease{
		row(rel1, "Still waiting", &future),
		row("b7f01440-4314-4406-9667-7b9c49d4d973", "Slot gone by", &past),
	}
	body := f.get("/admin", true).Body.String()
	auto, confirm := strings.Index(body, "Уходят сами"), strings.Index(body, "Подтвердить выход")
	if auto < 0 || confirm < auto {
		t.Fatalf("sections: auto %d, confirm %d: %s", auto, confirm, body)
	}
	waiting, gone := strings.Index(body, "Still waiting"), strings.Index(body, "Slot gone by")
	if waiting < auto || waiting > confirm {
		t.Errorf("a video before its slot isn't in «Уходят сами»: %d, %d, %d", auto, waiting, confirm)
	}
	if gone < confirm {
		t.Errorf("a video past its slot isn't in the normal section: %d, %d", gone, confirm)
	}
	if strings.Count(body, "🟢 Вышел") != 1 {
		t.Errorf("buttons: %d", strings.Count(body, "🟢 Вышел"))
	}
}
