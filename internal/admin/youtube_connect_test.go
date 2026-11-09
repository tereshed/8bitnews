package admin

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/tereshed/8bitnews/internal/store"
	"github.com/tereshed/8bitnews/internal/youtube"
)

// The fake store's channels and accounts for the connect page.
func (f *fakeStore) ListChannels(context.Context) ([]store.Channel, error) {
	return []store.Channel{
		{ID: "8bitnews-us", Name: "8BitNewsUS", Platforms: []byte(`{"youtube":{"enabled":true,"account":"@8BitNewsUS"}}`)},
		{ID: "off-channel", Name: "Off", Platforms: []byte(`{"youtube":{"enabled":false,"account":"@off"}}`)},
	}, nil
}

func (f *fakeStore) PlatformAccountOf(_ context.Context, channelID, _ string) (store.PlatformAccount, error) {
	if f.account == nil || f.account.ChannelID != channelID {
		return store.PlatformAccount{}, store.ErrNotFound
	}
	return *f.account, nil
}

func (f *fakeStore) SetPlatformAccount(_ context.Context, a store.PlatformAccount) error {
	f.account = &a
	return nil
}

func (f *fakeStore) DeletePlatformAccount(context.Context, string, string) error {
	f.account = nil
	return nil
}

type fakeConnector struct {
	refresh   string
	missing   []string
	exchangeE error
	channel   youtube.Channel
	channelE  error
	revoked   []string
	revokeE   error
	exchanged []string
}

func (c *fakeConnector) AuthCodeURL(state string) string {
	return "https://accounts.google.com/o/oauth2/v2/auth?state=" + url.QueryEscape(state)
}
func (c *fakeConnector) Exchange(_ context.Context, code string) (youtube.Grant, error) {
	c.exchanged = append(c.exchanged, code)
	return youtube.Grant{Refresh: c.refresh, Missing: c.missing}, c.exchangeE
}
func (c *fakeConnector) MyChannel(context.Context, string) (youtube.Channel, error) {
	return c.channel, c.channelE
}
func (c *fakeConnector) Revoke(_ context.Context, refresh string) error {
	c.revoked = append(c.revoked, refresh)
	return c.revokeE
}

// connectFixture is the page with a channel to connect; the fake GetChannel
// answers with 8bitnews-es, so the store's profile is set for this test.
func connectFixture(t *testing.T) (*fixture, *fakeConnector, *youtube.Sealer) {
	t.Helper()
	f := newFixture("approved")
	f.store.channelPlatforms = `{"youtube":{"enabled":true,"account":"@8BitNewsUS"}}`
	conn := &fakeConnector{refresh: "1//refresh", channel: youtube.Channel{ID: "UC123", Title: "8BitNewsUS", Handle: "@8bitnewsus"}}
	sealer, err := youtube.NewSealer(strings.Repeat("ab", 32))
	if err != nil {
		t.Fatal(err)
	}
	f.h.YouTube, f.h.Sealer = conn, sealer
	return f, conn, sealer
}

func (f *fixture) callback(state, code string) string {
	q := url.Values{"state": {state}}
	if code != "" {
		q.Set("code", code)
	}
	return YouTubeCallbackPath + "?" + q.Encode()
}

func TestTheConnectPageStatesWhatTheConsentCoversBeforeTheButton(t *testing.T) {
	f, _, _ := connectFixture(t)
	rec := f.get("/admin/youtube", true)
	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, body)
	}
	for _, want := range []string{"не спрашивая вас о каждом", "Private", "метка AI", "не для детей", "myaccount.google.com/permissions", "Отключить", "isn't verified"} {
		if !strings.Contains(body, want) {
			t.Errorf("the page doesn't say %q", want)
		}
	}
	consent, button := strings.Index(body, "не спрашивая"), strings.Index(body, ">Подключить<")
	if button < 0 || consent > button {
		t.Errorf("consent at %d, button at %d", consent, button)
	}
	if !strings.Contains(body, "accounts.google.com") {
		t.Error("no link to Google")
	}
	if strings.Contains(body, "off-channel") || strings.Contains(body, ">Off<") {
		t.Error("a channel with YouTube off is listed")
	}
}

func TestTheConnectPageNeedsAModerator(t *testing.T) {
	f, conn, _ := connectFixture(t)
	if rec := f.get("/admin/youtube", false); rec.Code != http.StatusUnauthorized {
		t.Errorf("no session: %d", rec.Code)
	}
	state := f.auth.YouTubeState(f.me.UserID, "8bitnews-us")
	if rec := f.get(f.callback(state, "code"), false); rec.Code != http.StatusUnauthorized || len(conn.exchanged) != 0 {
		t.Errorf("callback without a session: %d, exchanged %v", rec.Code, conn.exchanged)
	}
}

func TestConnectingStoresTheTokenEncrypted(t *testing.T) {
	f, conn, sealer := connectFixture(t)
	state := f.auth.YouTubeState(f.me.UserID, "8bitnews-us")
	rec := f.get(f.callback(state, "the-code"), true)
	if rec.Code != http.StatusSeeOther || !strings.Contains(rec.Header().Get("Location"), "yt_connected") {
		t.Fatalf("%d, %s", rec.Code, rec.Header().Get("Location"))
	}
	if len(conn.exchanged) != 1 || conn.exchanged[0] != "the-code" {
		t.Errorf("exchanged %v", conn.exchanged)
	}
	a := f.store.account
	if a == nil || a.ChannelID != "8bitnews-us" || a.ExternalID != "UC123" || a.Tokens.Title != "8BitNewsUS" || a.Tokens.ConnectedBy != f.me.Name {
		t.Fatalf("account: %+v", a)
	}
	if strings.Contains(a.Tokens.RefreshSealed, "refresh") {
		t.Errorf("the refresh token is stored in the clear: %q", a.Tokens.RefreshSealed)
	}
	if plain, err := sealer.Open(a.Tokens.RefreshSealed, "8bitnews-us"); err != nil || plain != "1//refresh" {
		t.Errorf("opening it: %q, %v", plain, err)
	}
}

func TestAStateThatIsForgedExpiredOrSomeoneElsesIsRefused(t *testing.T) {
	f, conn, _ := connectFixture(t)
	good := f.auth.YouTubeState(f.me.UserID, "8bitnews-us")
	other := NewAuth("another secret").YouTubeState(f.me.UserID, "8bitnews-us")
	f.auth.now = func() time.Time { return time.Now().Add(-stateTTL - time.Minute) }
	expired := f.auth.YouTubeState(f.me.UserID, "8bitnews-us")
	f.auth.now = time.Now
	session := f.auth.Session(f.me) // a session token of the right signature, wrong kind
	for name, state := range map[string]string{
		"forged":         other,
		"expired":        expired,
		"someone else's": f.auth.YouTubeState(f.me.UserID+1, "8bitnews-us"),
		"another kind":   session,
		"tampered":       good + "x",
		"none":           "",
	} {
		rec := f.get(f.callback(state, "code"), true)
		if rec.Code != http.StatusSeeOther || !strings.Contains(rec.Header().Get("Location"), "yt_bad_state") {
			t.Errorf("%s: %d, %s", name, rec.Code, rec.Header().Get("Location"))
		}
	}
	if len(conn.exchanged) != 0 || f.store.account != nil {
		t.Errorf("went on: exchanged %v, account %+v", conn.exchanged, f.store.account)
	}
}

func TestTheWrongChannelIsRefusedAndNothingStored(t *testing.T) {
	f, conn, _ := connectFixture(t)
	conn.channel = youtube.Channel{ID: "UC999", Title: "Konstantin", Handle: "@konstantin"}
	rec := f.get(f.callback(f.auth.YouTubeState(f.me.UserID, "8bitnews-us"), "code"), true)
	body := rec.Body.String()
	if rec.Code != http.StatusBadRequest || !strings.Contains(body, "Konstantin") || !strings.Contains(body, "@konstantin") || !strings.Contains(body, "Ничего не сохранено") {
		t.Fatalf("%d: %s", rec.Code, body)
	}
	if f.store.account != nil {
		t.Errorf("stored: %+v", f.store.account)
	}
	// Revoking would cut the access of that other channel, which may be connected.
	if len(conn.revoked) != 0 {
		t.Errorf("revoked %v", conn.revoked)
	}
}

func TestNoRefreshTokenStoresNothing(t *testing.T) {
	f, conn, _ := connectFixture(t)
	conn.refresh = ""
	rec := f.get(f.callback(f.auth.YouTubeState(f.me.UserID, "8bitnews-us"), "code"), true)
	if !strings.Contains(rec.Header().Get("Location"), "yt_no_refresh") || f.store.account != nil {
		t.Errorf("%s, %+v", rec.Header().Get("Location"), f.store.account)
	}
}

func TestAPartialConsentIsRefusedAndSaysWhatIsMissing(t *testing.T) {
	f, conn, _ := connectFixture(t)
	conn.missing = []string{"https://www.googleapis.com/auth/youtube.force-ssl"}
	rec := f.get(f.callback(f.auth.YouTubeState(f.me.UserID, "8bitnews-us"), "code"), true)
	body := rec.Body.String()
	if rec.Code != http.StatusBadRequest || !strings.Contains(body, "просмотр и изменение роликов канала") || !strings.Contains(body, "Ничего не сохранено") {
		t.Errorf("%d: %s", rec.Code, body)
	}
	if f.store.account != nil || len(conn.revoked) != 0 {
		t.Errorf("stored %+v, revoked %v", f.store.account, conn.revoked)
	}
}

func TestDisconnectingSaysScheduledVideosStillGoOut(t *testing.T) {
	f, _, _ := connectFixture(t)
	if body := f.get("/admin/youtube", true).Body.String(); !strings.Contains(body, "уже стоящие в расписании, отключение не снимает") {
		t.Errorf("the page promises too much: %s", body)
	}
	if text, _ := flashText("yt_disconnected"); !strings.Contains(text, "в расписании выйдут") {
		t.Errorf("flash: %s", text)
	}
}

func TestDeniedOrFailedConnectsStoreNothing(t *testing.T) {
	f, conn, _ := connectFixture(t)
	state := f.auth.YouTubeState(f.me.UserID, "8bitnews-us")
	rec := f.get(YouTubeCallbackPath+"?error=access_denied&state="+url.QueryEscape(state), true)
	if !strings.Contains(rec.Header().Get("Location"), "yt_denied") {
		t.Errorf("denied: %s", rec.Header().Get("Location"))
	}
	conn.exchangeE = errors.New("google is down")
	rec = f.get(f.callback(state, "code"), true)
	if !strings.Contains(rec.Header().Get("Location"), "yt_failed") {
		t.Errorf("exchange failed: %s", rec.Header().Get("Location"))
	}
	conn.exchangeE, conn.channelE = nil, errors.New("no channel")
	rec = f.get(f.callback(state, "code"), true)
	if !strings.Contains(rec.Header().Get("Location"), "yt_failed") || f.store.account != nil {
		t.Errorf("channel failed: %s, %+v", rec.Header().Get("Location"), f.store.account)
	}
}

func TestDisconnecting(t *testing.T) {
	f, conn, sealer := connectFixture(t)
	sealed, err := sealer.Seal("1//refresh", "8bitnews-us")
	if err != nil {
		t.Fatal(err)
	}
	account := store.PlatformAccount{ChannelID: "8bitnews-us", Tokens: store.AccountTokens{RefreshSealed: sealed, Title: "8BitNewsUS"}}
	f.store.account = &account
	same := map[string]string{"Sec-Fetch-Site": "same-origin"}

	body := f.get("/admin/youtube", true).Body.String()
	if !strings.Contains(body, "подключён") || strings.Contains(body, ">Подключить<") {
		t.Errorf("a connected channel: %s", body)
	}
	// From another site: refused.
	rec := f.post("/admin/youtube/8bitnews-us/disconnect", url.Values{}, true, map[string]string{"Sec-Fetch-Site": "cross-site"})
	if rec.Code != http.StatusForbidden || f.store.account == nil {
		t.Errorf("cross-site: %d, %+v", rec.Code, f.store.account)
	}
	rec = f.post("/admin/youtube/8bitnews-us/disconnect", url.Values{}, true, same)
	if !strings.Contains(rec.Header().Get("Location"), "yt_disconnected") || f.store.account != nil {
		t.Errorf("%s, %+v", rec.Header().Get("Location"), f.store.account)
	}
	if len(conn.revoked) != 1 || conn.revoked[0] != "1//refresh" {
		t.Errorf("revoked %v", conn.revoked)
	}

	// Google not answering: hub still lets go, and says it couldn't confirm.
	f.store.account = &account
	conn.revokeE = errors.New("down")
	rec = f.post("/admin/youtube/8bitnews-us/disconnect", url.Values{}, true, same)
	if !strings.Contains(rec.Header().Get("Location"), "yt_disconnected_keep") || f.store.account != nil {
		t.Errorf("revoke failed: %s, %+v", rec.Header().Get("Location"), f.store.account)
	}
}

func TestALostAccessAsksToConnectAgain(t *testing.T) {
	f, _, _ := connectFixture(t)
	at := time.Now()
	f.store.account = &store.PlatformAccount{ChannelID: "8bitnews-us", AuthAlertedAt: &at}
	body := f.get("/admin/youtube", true).Body.String()
	for _, want := range []string{"доступ потерян", ">Подключить<", "Отключить"} {
		if !strings.Contains(body, want) {
			t.Errorf("no %q in %s", want, body)
		}
	}
}

func TestWithTheUploaderOffThePageSaysSo(t *testing.T) {
	f := newFixture("approved")
	body := f.get("/admin/youtube", true).Body.String()
	if !strings.Contains(body, "не настроена") || strings.Contains(body, ">Подключить<") {
		t.Errorf("%s", body)
	}
	if strings.Contains(f.get("/admin", true).Body.String(), "/admin/youtube") {
		t.Error("the list links to a page that's off")
	}
	c, _, _ := connectFixture(t)
	if !strings.Contains(c.get("/admin", true).Body.String(), "/admin/youtube") {
		t.Error("the list doesn't link to the connect page")
	}
}

func TestWithdrawingSaysWhoTakesTheVideoDown(t *testing.T) {
	f, _, _ := connectFixture(t)
	same := map[string]string{"Sec-Fetch-Site": "same-origin"}
	form := url.Values{"action": {"withdraw"}, "note": {"a claim"}}
	if loc := f.post("/admin/r/"+rel1+"/act", form, true, same).Header().Get("Location"); !strings.HasSuffix(loc, "msg=withdraw") {
		t.Errorf("not connected: %s", loc)
	}
	f.mod.channel = "8bitnews-us"
	f.store.account = &store.PlatformAccount{ChannelID: "8bitnews-us"}
	if loc := f.post("/admin/r/"+rel1+"/act", form, true, same).Header().Get("Location"); !strings.HasSuffix(loc, "msg=withdraw_connected") {
		t.Errorf("connected: %s", loc)
	}
}
