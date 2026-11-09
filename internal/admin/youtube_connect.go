package admin

import (
	"context"
	"encoding/json"
	"errors"
	"html/template"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/tereshed/8bitnews/internal/store"
	"github.com/tereshed/8bitnews/internal/youtube"
)

// YouTubeCallbackPath is where Google sends the owner back to: the redirect URI
// of the OAuth client (platforms/youtube/setup.md).
const YouTubeCallbackPath = "/admin/youtube/callback"

const (
	kindYouTube = "youtube"
	stateTTL    = 10 * time.Minute
)

// Connector is the OAuth side of connecting a channel (internal/youtube).
type Connector interface {
	AuthCodeURL(state string) string
	Exchange(ctx context.Context, code string) (youtube.Grant, error)
	MyChannel(ctx context.Context, refresh string) (youtube.Channel, error)
	Revoke(ctx context.Context, refresh string) error
}

// Sealer encrypts a refresh token for the database (youtube.Sealer).
type Sealer interface {
	Seal(plain, channelID string) (string, error)
	Open(sealed, channelID string) (string, error)
}

// YouTubeState signs what the consent screen carries there and back: which
// channel is being connected, by whom. It lasts 10 minutes.
func (a *Auth) YouTubeState(userID int64, channelID string) string {
	return a.sign(claims{Kind: kindYouTube, UserID: userID, Channel: channelID, Expiry: a.now().Add(stateTTL).Unix()})
}

// CheckYouTubeState returns the channel a state is for, if it is genuine,
// unexpired and the same moderator's.
func (a *Auth) CheckYouTubeState(state string, userID int64) (string, error) {
	c, err := a.verify(state, kindYouTube)
	if err != nil {
		return "", err
	}
	if c.UserID != userID || c.Channel == "" {
		return "", errBadToken
	}
	return c.Channel, nil
}

// youtubeAccount reads what the profile says about the channel on YouTube.
func youtubeAccount(ch store.Channel) (handle string, enabled bool) {
	var platforms struct {
		YouTube struct {
			Enabled bool   `json:"enabled"`
			Account string `json:"account"`
		} `json:"youtube"`
	}
	_ = json.Unmarshal(ch.Platforms, &platforms)
	return platforms.YouTube.Account, platforms.YouTube.Enabled
}

// scopeName says a permission the way the consent screen's reader would.
func scopeName(scope string) string {
	switch path.Base(scope) {
	case "youtube.upload":
		return "загрузка роликов"
	case "youtube.force-ssl":
		return "просмотр и изменение роликов канала"
	case "yt-analytics.readonly":
		return "статистика канала"
	}
	return scope
}

func sameHandle(a, b string) bool {
	a, b = strings.TrimPrefix(strings.TrimSpace(a), "@"), strings.TrimPrefix(strings.TrimSpace(b), "@")
	return a != "" && strings.EqualFold(a, b)
}

// connected says whether hub uploads to this channel by itself.
func (h *Handler) connected(ctx context.Context, channelID string) bool {
	if h.YouTube == nil {
		return false
	}
	a, err := h.Store.PlatformAccountOf(ctx, channelID, store.PlatformYouTube)
	return err == nil && a.AuthAlertedAt == nil
}

type youtubeRow struct {
	ChannelID string
	Name      string
	Handle    string
	Connected bool
	// Lost: it was connected, and the access has stopped working.
	Lost        bool
	AccountName string
	ConnectedBy string
	ConnectedAt string
	ConnectURL  template.URL
}

type youtubePage struct {
	Who      string
	Flash    string
	FlashBad bool
	Off      bool
	Rows     []youtubeRow
}

func (h *Handler) youtubeList(w http.ResponseWriter, r *http.Request, id Identity) {
	flash, bad := flashText(r.URL.Query().Get("msg"))
	h.youtubeRender(w, r, id, http.StatusOK, flash, bad)
}

func (h *Handler) youtubeRender(w http.ResponseWriter, r *http.Request, id Identity, status int, flash string, bad bool) {
	page := youtubePage{Who: id.Name, Flash: flash, FlashBad: bad, Off: h.YouTube == nil}
	if h.YouTube != nil {
		channels, err := h.Store.ListChannels(r.Context())
		if err != nil {
			h.fail(w, err)
			return
		}
		for _, ch := range channels {
			handle, enabled := youtubeAccount(ch)
			if !enabled {
				continue
			}
			row := youtubeRow{ChannelID: ch.ID, Name: ch.Name, Handle: handle}
			a, err := h.Store.PlatformAccountOf(r.Context(), ch.ID, store.PlatformYouTube)
			switch {
			case err == nil:
				row.Connected, row.Lost = a.AuthAlertedAt == nil, a.AuthAlertedAt != nil
				row.AccountName, row.ConnectedBy = a.Tokens.Title, a.Tokens.ConnectedBy
				if !a.Tokens.ConnectedAt.IsZero() {
					row.ConnectedAt = h.when(a.Tokens.ConnectedAt)
				}
			case !errors.Is(err, store.ErrNotFound):
				h.fail(w, err)
				return
			}
			if !row.Connected {
				// A link, not a form: this page's form-action stops a form at
				// this site's own address, redirects included.
				row.ConnectURL = template.URL(h.YouTube.AuthCodeURL(h.Auth.YouTubeState(id.UserID, ch.ID))) //nolint:gosec // our own address for Google, from our config and a signed state
			}
			page.Rows = append(page.Rows, row)
		}
	}
	h.page(w, status, youtubeTemplate, page)
}

// youtubeCallback is where Google sends the owner back. The session cookie is
// Lax, so it comes along on this top-level GET and the gate sees the moderator.
// The state then proves this answer belongs to a connect this moderator began.
func (h *Handler) youtubeCallback(w http.ResponseWriter, r *http.Request, id Identity) {
	const back = "/admin/youtube"
	if h.YouTube == nil {
		redirect(w, r, back, "")
		return
	}
	ctx, q := r.Context(), r.URL.Query()
	channelID, err := h.Auth.CheckYouTubeState(q.Get("state"), id.UserID)
	if err != nil {
		redirect(w, r, back, "yt_bad_state")
		return
	}
	if q.Get("error") != "" || q.Get("code") == "" {
		redirect(w, r, back, "yt_denied")
		return
	}
	ch, err := h.Store.GetChannel(ctx, channelID)
	if err != nil {
		h.fail(w, err)
		return
	}
	grant, err := h.YouTube.Exchange(ctx, q.Get("code"))
	if err != nil {
		h.Log.Error("admin: connecting YouTube: exchanging the code", "channel_id", channelID, "error", err)
		redirect(w, r, back, "yt_failed")
		return
	}
	// Google lets a person untick each permission. Without one of them hub would
	// fail later, with less to say about why.
	if len(grant.Missing) > 0 {
		names := make([]string, len(grant.Missing))
		for i, scope := range grant.Missing {
			names[i] = scopeName(scope)
		}
		h.youtubeRender(w, r, id, http.StatusBadRequest, "Google выдал не все разрешения: не хватает «"+strings.Join(names, "», «")+
			"». Ничего не сохранено. Подключите канал заново и оставьте все галочки.", true)
		return
	}
	refresh := grant.Refresh
	if refresh == "" {
		redirect(w, r, back, "yt_no_refresh")
		return
	}
	mine, err := h.YouTube.MyChannel(ctx, refresh)
	if err != nil {
		h.Log.Error("admin: connecting YouTube: reading the channel", "channel_id", channelID, "error", err)
		redirect(w, r, back, "yt_failed")
		return
	}
	// The access isn't revoked on a wrong pick: Google ties revocation to the
	// account and this client, and that account may be connected as its own channel.
	handle, _ := youtubeAccount(ch)
	if !sameHandle(mine.Handle, handle) {
		h.youtubeRender(w, r, id, http.StatusBadRequest, "На экране Google выбран канал «"+mine.Title+"» ("+mine.Handle+"), а подключался "+
			ch.Name+" ("+handle+"). Ничего не сохранено. Попробуйте ещё раз и выберите нужный канал.", true)
		return
	}
	sealed, err := h.Sealer.Seal(refresh, channelID)
	if err != nil {
		h.fail(w, err)
		return
	}
	if err := h.Store.SetPlatformAccount(ctx, store.PlatformAccount{ChannelID: channelID, Platform: store.PlatformYouTube, ExternalID: mine.ID,
		Tokens: store.AccountTokens{RefreshSealed: sealed, Title: mine.Title, Handle: mine.Handle, ConnectedBy: id.Name, ConnectedAt: h.now()}}); err != nil {
		h.fail(w, err)
		return
	}
	h.Log.Info("admin: YouTube connected", "channel_id", channelID, "youtube_id", mine.ID, "by", id.Name)
	redirect(w, r, back, "yt_connected")
}

// youtubeDisconnect ends hub's access to a channel, at Google too if it can.
func (h *Handler) youtubeDisconnect(w http.ResponseWriter, r *http.Request, id Identity) {
	const back = "/admin/youtube"
	if h.YouTube == nil {
		redirect(w, r, back, "")
		return
	}
	ctx, channelID := r.Context(), r.PathValue("channel")
	a, err := h.Store.PlatformAccountOf(ctx, channelID, store.PlatformYouTube)
	if errors.Is(err, store.ErrNotFound) {
		redirect(w, r, back, "yt_disconnected")
		return
	}
	if err != nil {
		h.fail(w, err)
		return
	}
	msg := "yt_disconnected"
	refresh, err := h.Sealer.Open(a.Tokens.RefreshSealed, channelID)
	if err == nil {
		err = h.YouTube.Revoke(ctx, refresh)
	}
	if err != nil {
		h.Log.Error("admin: disconnecting YouTube: revoking", "channel_id", channelID, "error", err)
		msg = "yt_disconnected_keep"
	}
	if err := h.Store.DeletePlatformAccount(ctx, channelID, store.PlatformYouTube); err != nil {
		h.fail(w, err)
		return
	}
	h.Log.Info("admin: YouTube disconnected", "channel_id", channelID, "by", id.Name)
	redirect(w, r, back, msg)
}

var youtubeTemplate = template.Must(template.New("youtube").Parse(head + `<title>YouTube · 8BitNews</title>
</head>
<body><main>
{{with .Data}}` + topBar + `
<h2>Загрузка на YouTube</h2>
{{if .Off}}<p>Загрузка через API не настроена: в hub не заданы HUB_YOUTUBE_*. Ролики загружаются через API публикации (docs/automation.md).</p>
{{else}}
<p>Подключая канал, вы разрешаете hub загружать на него каждый готовый ролик, <b>не спрашивая вас о каждом</b>:</p>
<ul>
<li>видимость Private с расписанием на слот канала: в этот час YouTube сделает ролик публичным;</li>
<li>метка AI включена, «не для детей», категория News &amp; Politics;</li>
<li>заголовок, описание и теги — из пакета ролика, без добавлений;</li>
<li>обложка — из пакета ролика;</li>
<li>hub снимает ролик с расписания, если его релиз закрыт или задержан, и сообщает в группу, если не смог.</li>
</ul>
<p>Новые загрузки останавливает кнопка «Отключить» здесь или <a href="https://myaccount.google.com/permissions">страница разрешений Google</a>. <b>Ролики, уже стоящие в расписании, отключение не снимает: они выйдут в свой час</b>, а hub снять их больше не сможет; снимайте их в Studio. Пока канал не подключён, ролики ждут загрузки через API публикации.</p>
<p class="hint">Google покажет предупреждение «This app isn't verified»: это наше приложение 8bitnews publisher. Нажмите Advanced и перейдите к нему. В списке аккаунтов выберите именно этот канал.</p>
{{range .Rows}}<div class="progress"><b>{{.Name}}</b> {{.Handle}}
<div class="meta">{{if .Connected}}подключён{{with .AccountName}}: {{.}}{{end}}{{with .ConnectedBy}} · {{.}}{{end}}{{with .ConnectedAt}} · {{.}}{{end}}
{{else if .Lost}}<span class="warn">доступ потерян: ролики ждут загрузки через API публикации, подключите заново</span>
{{else}}не подключён{{end}}</div>
<div class="row">{{with .ConnectURL}}<a class="go" href="{{.}}" rel="noreferrer">Подключить</a>{{end}}
{{if or .Connected .Lost}}<form class="inline" method="post" action="/admin/youtube/{{.ChannelID}}/disconnect"><button type="submit">Отключить</button></form>{{end}}</div>
</div>{{end}}
{{end}}
{{end}}
</main></body></html>`))
