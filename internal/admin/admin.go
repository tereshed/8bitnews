// Package admin is the moderator's page in hub: every video of every channel
// in one list, with what it needs next and the buttons to do it. It does what
// the moderation card's buttons do, through the same bot, so the card and the
// group stay up to date, and it connects channels for hub's own uploads to
// YouTube (youtube_connect.go). Access is
// the group's administrators, by a login link from the bot (auth.go).
package admin

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/tereshed/8bitnews/internal/moderation"
	"github.com/tereshed/8bitnews/internal/publish"
	"github.com/tereshed/8bitnews/internal/store"
)

const (
	cookieName   = "hub_admin"
	listMax      = 200
	progressDays = 3
	timezone     = "Europe/Madrid"
)

// Store is what the page reads and writes (internal/store).
type Store interface {
	AdminReleases(ctx context.Context, n int) ([]store.AdminRelease, error)
	DevelopmentsInProgress(ctx context.Context, since time.Duration) ([]store.InProgress, error)
	GetRelease(ctx context.Context, id string) (store.Release, error)
	ReleaseContextOf(ctx context.Context, releaseID string) (store.ReleaseContext, error)
	ListReleaseSources(ctx context.Context, releaseID string) ([]store.ReleaseSource, error)
	GetChannel(ctx context.Context, id string) (store.Channel, error)
	GetCountry(ctx context.Context, cc string) (store.Country, error)
	PublicationURL(ctx context.Context, releaseID, platform string) (string, error)
	SetPublicationURL(ctx context.Context, releaseID, platform, url, by string) error
	AdminSessionEpoch(ctx context.Context, userID int64) (int, error)
	RaiseAdminSessionEpoch(ctx context.Context, userID int64) error
	ListChannels(ctx context.Context) ([]store.Channel, error)
	PlatformAccountOf(ctx context.Context, channelID, platform string) (store.PlatformAccount, error)
	SetPlatformAccount(ctx context.Context, a store.PlatformAccount) error
	DeletePlatformAccount(ctx context.Context, channelID, platform string) error
}

// Moderator is the bot: who may moderate, and decisions that reach the group.
type Moderator interface {
	IsModerator(ctx context.Context, userID int64) bool
	Act(ctx context.Context, releaseID, action, who string, userID int64, note string) (store.Release, bool, error)
}

// Media presigns a video or a cover for the browser (internal/storage).
type Media interface {
	PresignGet(ctx context.Context, key string) (string, error)
}

// Videos says whether a link works on the platform.
type Videos interface {
	State(ctx context.Context, link string) string
	Forget(link string)
}

// Deps is what the page needs.
type Deps struct {
	Store     Store
	Moderator Moderator
	Media     Media
	Auth      *Auth
	Videos    Videos
	Publish   publish.Config
	// MediaOrigin is the object store's address, the one other place the
	// page loads images and video from.
	MediaOrigin string
	Log         *slog.Logger
	// YouTube and Sealer connect channels so hub can upload to them itself.
	// Nil: hub doesn't, and /admin/youtube says so.
	YouTube Connector
	Sealer  Sealer
}

// Handler serves /admin.
type Handler struct {
	Deps
	mux      *http.ServeMux
	mediaCSP string
	loc      *time.Location
	// now is time.Now; tests set it.
	now func() time.Time
}

// New builds the page.
func New(d Deps) *Handler {
	h := &Handler{Deps: d, mux: http.NewServeMux(), mediaCSP: mediaSources(d.MediaOrigin), loc: time.UTC, now: time.Now}
	if loc, err := time.LoadLocation(timezone); err == nil {
		h.loc = loc
	}
	h.mux.HandleFunc("GET /admin", h.gate(h.list))
	h.mux.HandleFunc("GET /admin/login", h.loginForm)
	h.mux.HandleFunc("POST /admin/login", h.sameOrigin(h.login))
	h.mux.HandleFunc("POST /admin/logout", h.sameOrigin(h.logout))
	h.mux.HandleFunc("GET /admin/r/{id}", h.gate(h.detail))
	h.mux.HandleFunc("POST /admin/r/{id}/act", h.sameOrigin(h.gate(h.act)))
	h.mux.HandleFunc("POST /admin/r/{id}/link", h.sameOrigin(h.gate(h.setLink)))
	h.mux.HandleFunc("GET /admin/media/{id}/{kind}", h.gate(h.media))
	h.mux.HandleFunc("GET /admin/youtube", h.gate(h.youtubeList))
	h.mux.HandleFunc("GET "+YouTubeCallbackPath, h.gate(h.youtubeCallback))
	h.mux.HandleFunc("POST /admin/youtube/{channel}/disconnect", h.sameOrigin(h.gate(h.youtubeDisconnect)))
	return h
}

// mediaSources is the object store for the page's CSP: its host, and its
// subdomains for virtual-hosted buckets.
func mediaSources(origin string) string {
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" {
		return "'none'"
	}
	return u.Scheme + "://" + u.Host + " " + u.Scheme + "://*." + u.Host
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// A stray /admin/ lands on the list rather than on a 404.
	if r.URL.Path == "/admin/" {
		http.Redirect(w, r, "/admin", http.StatusSeeOther)
		return
	}
	hdr := w.Header()
	hdr.Set("X-Content-Type-Options", "nosniff")
	hdr.Set("X-Robots-Tag", "noindex, nofollow")
	// same-origin, not no-referrer: with no-referrer a form post's Origin is
	// "null", which sameOrigin can't check in a browser without Sec-Fetch-Site.
	hdr.Set("Referrer-Policy", "same-origin")
	hdr.Set("Cache-Control", "no-store")
	h.mux.ServeHTTP(w, r)
}

type authedHandler func(w http.ResponseWriter, r *http.Request, id Identity)

// gate lets the group's administrators through. A session is signed for 30
// days, but someone removed from the group loses access within minutes.
func (h *Handler) gate(next authedHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(cookieName)
		if err != nil {
			h.page(w, http.StatusUnauthorized, signInTemplate, signInPage{})
			return
		}
		id, err := h.Auth.CheckSession(cookie.Value)
		if err != nil {
			h.page(w, http.StatusUnauthorized, signInTemplate, signInPage{})
			return
		}
		epoch, err := h.Store.AdminSessionEpoch(r.Context(), id.UserID)
		if err != nil {
			h.fail(w, err)
			return
		}
		if id.Epoch != epoch {
			h.page(w, http.StatusUnauthorized, signInTemplate, signInPage{Message: "Вы вышли из админки. Войдите заново."})
			return
		}
		if !h.Moderator.IsModerator(r.Context(), id.UserID) {
			h.page(w, http.StatusForbidden, signInTemplate, signInPage{Message: "Нет подтверждения, что вы администратор группы модерации: вас убрали из администраторов, или Telegram сейчас не отвечает."})
			return
		}
		next(w, r, id)
	}
}

// sameOrigin refuses a form posted from another site. The cookie's SameSite
// already keeps it off such a request; this holds if a browser doesn't honor
// that. A browser sends at least one of these headers with a POST.
func (h *Handler) sameOrigin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		site := r.Header.Get("Sec-Fetch-Site")
		origin := r.Header.Get("Origin")
		ok := site == "same-origin"
		if site == "" && origin != "" {
			if u, err := url.Parse(origin); err == nil {
				ok = u.Host == r.Host
			}
		}
		if !ok {
			http.Error(w, "Запрос с другого сайта отклонён", http.StatusForbidden)
			return
		}
		next(w, r)
	}
}

func (h *Handler) loginForm(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("t")
	id, err := h.Auth.PeekLogin(token)
	if err != nil {
		h.page(w, http.StatusUnauthorized, signInTemplate, signInPage{Message: "Ссылка устарела или уже использована."})
		return
	}
	h.page(w, http.StatusOK, loginTemplate, loginPage{Token: token, Name: id.Name})
}

func (h *Handler) login(w http.ResponseWriter, r *http.Request) {
	id, err := h.Auth.SpendLogin(r.PostFormValue("t"))
	if err != nil {
		h.page(w, http.StatusUnauthorized, signInTemplate, signInPage{Message: "Ссылка устарела или уже использована."})
		return
	}
	if !h.Moderator.IsModerator(r.Context(), id.UserID) {
		h.page(w, http.StatusForbidden, signInTemplate, signInPage{Message: "Вход только для администраторов группы модерации."})
		return
	}
	id.Epoch, err = h.Store.AdminSessionEpoch(r.Context(), id.UserID)
	if err != nil {
		h.fail(w, err)
		return
	}
	h.setCookie(w, h.Auth.Session(id), int(sessionTTL/time.Second))
	h.Log.Info("admin: signed in", "by", id.Name, "telegram_user_id", id.UserID)
	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

// logout ends all of the moderator's sessions, not just this browser's: it's
// also what to do about a cookie that may have leaked.
func (h *Handler) logout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(cookieName); err == nil {
		if id, err := h.Auth.CheckSession(cookie.Value); err == nil {
			if err := h.Store.RaiseAdminSessionEpoch(r.Context(), id.UserID); err != nil {
				h.fail(w, err)
				return
			}
		}
	}
	h.setCookie(w, "", -1)
	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

func (h *Handler) setCookie(w http.ResponseWriter, value string, maxAge int) {
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: value, Path: "/admin", MaxAge: maxAge,
		HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode})
}

// row is one release in the list.
type row struct {
	store.AdminRelease
	DisplayTitle string
	When         string
	Duration     string
	StatusText   string
	StatusClass  string
	NeedsLink    bool
	VideoState   string
	Sources      string
	// Barred: Legal didn't allow YouTube for this video.
	Barred  bool
	Allowed string
	// AutoPublishes: hub takes this video to YouTube and sees it out by itself.
	AutoPublishes bool
}

type section struct {
	Title string
	Hint  string
	Rows  []row
}

type listPage struct {
	Who        string
	YouTubeOn  bool
	Flash      string
	FlashBad   bool
	Tabs       []tab
	Sections   []section
	InProgress []progressRow
	Closed     []row
	Empty      bool
}

type tab struct {
	Label  string
	URL    string
	Active bool
}

type progressRow struct {
	store.InProgress
	When string
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request, id Identity) {
	ctx := r.Context()
	releases, err := h.Store.AdminReleases(ctx, listMax)
	if err != nil {
		h.fail(w, err)
		return
	}
	progress, err := h.Store.DevelopmentsInProgress(ctx, progressDays*24*time.Hour)
	if err != nil {
		h.fail(w, err)
		return
	}
	channel := r.URL.Query().Get("channel")
	page := listPage{Who: id.Name, YouTubeOn: h.YouTube != nil, Tabs: h.tabs(releases, progress, channel)}
	page.Flash, page.FlashBad = flashText(r.URL.Query().Get("msg"))

	rows := make([]row, 0, len(releases))
	for _, rel := range releases {
		if channel == "" || rel.ChannelID == channel {
			rows = append(rows, h.rowOf(rel))
		}
	}
	h.checkVideos(ctx, rows)

	var decide, auto, held, upload, confirm, live []row
	for _, x := range rows {
		switch x.Status {
		case "submitted", "pending_approval":
			decide = append(decide, x)
		case "approved":
			if x.PublishHold != "" {
				held = append(held, x)
			} else if x.AutoPublishes {
				auto = append(auto, x)
			} else if x.YouTubeURL == "" {
				upload = append(upload, x) // a barred one says so there
			} else {
				confirm = append(confirm, x)
			}
		case "published":
			live = append(live, x)
		default:
			page.Closed = append(page.Closed, x)
		}
	}
	for _, s := range []section{
		{Title: "Решить", Hint: "Посмотреть ролик и одобрить, вернуть или отклонить.", Rows: decide},
		{Title: "Задержаны", Hint: "Сами не выйдут. Если ролик уже в расписании, его надо снять в Studio.", Rows: held},
		{Title: "Уходят сами", Hint: "Подтверждение на канале выключено: ролик встаёт в расписание и отмечается вышедшим без вас.", Rows: auto},
		{Title: "Загрузить на YouTube", Hint: "Одобрены. После загрузки вставьте ссылку.", Rows: upload},
		{Title: "Подтвердить выход", Hint: "Ссылка есть. Hub сам отметит выход, когда увидит ролик на YouTube; кнопка «Вышел» — чтобы не ждать.", Rows: confirm},
		{Title: "Вышли", Rows: live},
	} {
		if len(s.Rows) > 0 {
			page.Sections = append(page.Sections, s)
		}
	}
	for _, p := range progress {
		if channel == "" || p.ChannelID == channel {
			page.InProgress = append(page.InProgress, progressRow{InProgress: p, When: h.when(p.CreatedAt)})
		}
	}
	page.Empty = len(page.Sections) == 0 && len(page.InProgress) == 0 && len(page.Closed) == 0
	h.page(w, http.StatusOK, listTemplate, page)
}

func (h *Handler) tabs(releases []store.AdminRelease, progress []store.InProgress, active string) []tab {
	seen := map[string]bool{}
	for _, r := range releases {
		seen[r.ChannelID] = true
	}
	for _, p := range progress {
		seen[p.ChannelID] = true
	}
	ids := make([]string, 0, len(seen))
	for id := range seen {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	tabs := []tab{{Label: "Все каналы", URL: "/admin", Active: active == ""}}
	for _, id := range ids {
		tabs = append(tabs, tab{Label: id, URL: "/admin?channel=" + url.QueryEscape(id), Active: active == id})
	}
	return tabs
}

func (h *Handler) rowOf(rel store.AdminRelease) row {
	text, class := statusOf(rel.Status)
	x := row{AdminRelease: rel, DisplayTitle: rel.Title, When: h.when(rel.CreatedAt), StatusText: text, StatusClass: class}
	if x.DisplayTitle == "" {
		x.DisplayTitle = rel.StoryTitle
	}
	if rel.DurationS > 0 {
		x.Duration = fmt.Sprintf("%.0f с", rel.DurationS)
	}
	x.Barred = !contains(rel.AllowedPlatforms, store.PlatformYouTube)
	x.Allowed = strings.Join(rel.AllowedPlatforms, ", ")
	x.AutoPublishes = rel.Status == "approved" && rel.ModeratedBy == store.AutoApprover && rel.PublishHold == "" && !x.Barred
	x.NeedsLink = (rel.Status == "approved" || rel.Status == "published") && rel.YouTubeURL == "" && !x.Barred && !x.AutoPublishes
	if rel.Status == "approved" || rel.Status == "published" {
		x.Sources = h.Publish.SourcesPageURL(rel.ID)
	}
	return x
}

// checkVideos asks about every link at once, for videoStateWait at most: a
// slow answer leaves its row without a mark, not the page without rows.
func (h *Handler) checkVideos(ctx context.Context, rows []row) {
	ctx, cancel := context.WithTimeout(ctx, videoStateWait)
	defer cancel()
	limit := make(chan struct{}, 8)
	var wg sync.WaitGroup
	for i := range rows {
		if rows[i].YouTubeURL == "" {
			continue
		}
		wg.Add(1)
		limit <- struct{}{}
		go func(x *row) {
			defer wg.Done()
			defer func() { <-limit }()
			x.VideoState = h.Videos.State(ctx, x.YouTubeURL)
		}(&rows[i])
	}
	wg.Wait()
}

type detailPage struct {
	Who         string
	Flash       string
	FlashBad    bool
	ID          string
	Channel     string
	Title       string
	StoryTitle  string
	Development string
	When        string
	StatusText  string
	StatusClass string
	Status      string
	ModeratedBy string
	ModeratedAt string
	Note        string
	Sources     string
	CanDecide   bool
	CanPublish  bool
	IsPublished bool
	CanLink     bool
	Barred      bool
	Allowed     string
	HasVideo    bool
	HasCover    bool
	HasUpload   bool
	Link        string
	LinkInput   string
	VideoState  string
	Package     *publish.PlatformPackage
	TagsLine    string
	Warnings    []string
	Notes       []string
	Reports     string
	SourceCount int
	Archived    int
	PackageErr  string
}

func (h *Handler) detail(w http.ResponseWriter, r *http.Request, id Identity) {
	ctx := r.Context()
	relID, ok := releaseID(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	rel, err := h.Store.GetRelease(ctx, relID)
	if err != nil {
		h.fail(w, err)
		return
	}
	story, err := h.Store.ReleaseContextOf(ctx, rel.ID)
	if err != nil {
		h.fail(w, err)
		return
	}
	sources, err := h.Store.ListReleaseSources(ctx, rel.ID)
	if err != nil {
		h.fail(w, err)
		return
	}
	link, err := h.Store.PublicationURL(ctx, rel.ID, store.PlatformYouTube)
	if err != nil {
		h.fail(w, err)
		return
	}
	text, class := statusOf(rel.Status)
	page := detailPage{
		Who: id.Name, ID: rel.ID, Channel: rel.ChannelID, StoryTitle: story.StoryTitle, Development: story.DevelopmentSummary,
		When: h.when(rel.CreatedAt), Status: rel.Status, StatusText: text, StatusClass: class, ModeratedBy: rel.Moderation.By, Note: rel.Moderation.Note,
		CanDecide:   rel.Status == "pending_approval",
		CanPublish:  rel.Status == "approved",
		IsPublished: rel.Status == "published",
		CanLink:     (rel.Status == "approved" || rel.Status == "published") && contains(rel.AllowedPlatforms, store.PlatformYouTube),
		Barred:      !contains(rel.AllowedPlatforms, store.PlatformYouTube),
		Allowed:     strings.Join(rel.AllowedPlatforms, ", "),
		Link:        link, SourceCount: len(sources),
		HasCover: mediaKey(rel, "cover") != "", HasVideo: mediaKey(rel, "video") != "", HasUpload: mediaKey(rel, "upload") != "",
	}
	page.Flash, page.FlashBad = flashText(r.URL.Query().Get("msg"))
	if rel.Moderation.At != nil {
		page.ModeratedAt = h.when(*rel.Moderation.At)
	}
	for _, s := range sources {
		if s.ArchiveURL != "" {
			page.Archived++
		}
	}
	if rel.Status == "approved" || rel.Status == "published" {
		page.Sources = h.Publish.SourcesPageURL(rel.ID)
	}
	if link != "" {
		page.VideoState = h.Videos.State(ctx, link)
	}
	page.Reports = moderation.BuildCard(rel, story, sources).Details
	page.Title = story.StoryTitle
	if pkg, err := h.packageOf(ctx, rel, sources); err != nil {
		h.Log.Error("admin: building the upload package failed", "release_id", rel.ID, "error", err)
		page.PackageErr = "Пакет для загрузки не собрался. Подробности в логах hub."
	} else {
		page.Warnings, page.Notes = pkg.Warnings, pkg.Notes
		for i := range pkg.Platforms {
			if p := &pkg.Platforms[i]; p.Platform == store.PlatformYouTube {
				page.Package = p
				page.TagsLine = strings.Join(p.Tags, ", ")
				if p.Title != "" {
					page.Title = p.Title
				}
			}
		}
	}
	h.page(w, http.StatusOK, detailTemplate, page)
}

func (h *Handler) packageOf(ctx context.Context, rel store.Release, sources []store.ReleaseSource) (publish.Package, error) {
	ch, err := h.Store.GetChannel(ctx, rel.ChannelID)
	if err != nil {
		return publish.Package{}, err
	}
	country, err := h.Store.GetCountry(ctx, ch.CountryCC)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return publish.Package{}, err
	}
	return publish.Build(publish.Input{Release: rel, Sources: sources, Channel: ch, Country: country, Config: h.Publish}), nil
}

// actionsAllowed are the page's buttons; the ones needing a reason ask for it.
var actionsAllowed = map[string]bool{"approve": true, "changes": true, "reject": true, "published": true, "unpublished": true, "withdraw": true}

func (h *Handler) act(w http.ResponseWriter, r *http.Request, id Identity) {
	relID, ok := releaseID(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	action := r.PostFormValue("action")
	back := backTo(r, relID)
	if !actionsAllowed[action] {
		http.Error(w, "Неизвестное действие", http.StatusBadRequest)
		return
	}
	note := strings.TrimSpace(r.PostFormValue("note"))
	if (action == "changes" || action == "reject") && note == "" {
		redirect(w, r, back, "need_reason")
		return
	}
	rel, changed, err := h.Moderator.Act(r.Context(), relID, action, id.Name, id.UserID, note)
	if err != nil {
		h.fail(w, err)
		return
	}
	if !changed {
		redirect(w, r, back, "already")
		return
	}
	msg := action
	if action == "withdraw" && h.connected(r.Context(), rel.ChannelID) {
		msg = "withdraw_connected"
	}
	redirect(w, r, back, msg)
}

func (h *Handler) setLink(w http.ResponseWriter, r *http.Request, id Identity) {
	ctx := r.Context()
	relID, ok := releaseID(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	back := backTo(r, relID)
	rel, err := h.Store.GetRelease(ctx, relID)
	if err != nil {
		h.fail(w, err)
		return
	}
	if rel.Status != "approved" && rel.Status != "published" {
		redirect(w, r, back, "not_approved")
		return
	}
	if !contains(rel.AllowedPlatforms, store.PlatformYouTube) {
		redirect(w, r, back, "barred")
		return
	}
	old, err := h.Store.PublicationURL(ctx, rel.ID, store.PlatformYouTube)
	if err != nil {
		h.fail(w, err)
		return
	}
	raw := strings.TrimSpace(r.PostFormValue("url"))
	link := ""
	if raw != "" {
		if link, err = ParseYouTubeURL(raw); err != nil {
			redirect(w, r, back, "bad_link")
			return
		}
	}
	if err := h.Store.SetPublicationURL(ctx, rel.ID, store.PlatformYouTube, link, id.Name); err != nil {
		h.fail(w, err)
		return
	}
	for _, l := range []string{old, link} {
		if l != "" {
			h.Videos.Forget(l)
		}
	}
	redirect(w, r, back, "link_saved")
}

// media sends the browser to a presigned address of the release's file. The
// page's HTML holds no credentials, and its links don't expire.
func (h *Handler) media(w http.ResponseWriter, r *http.Request, _ Identity) {
	ctx := r.Context()
	relID, ok := releaseID(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	rel, err := h.Store.GetRelease(ctx, relID)
	if err != nil {
		h.fail(w, err)
		return
	}
	key := mediaKey(rel, r.PathValue("kind"))
	if key == "" {
		http.NotFound(w, r)
		return
	}
	target, err := h.Media.PresignGet(ctx, key)
	if err != nil {
		h.fail(w, err)
		return
	}
	http.Redirect(w, r, target, http.StatusFound)
}

// mediaKey finds a release's file by what the page calls it: "cover", "video"
// or "upload", the cut to upload to YouTube (without music when there is one).
func mediaKey(rel store.Release, kind string) string {
	var media, packaged struct {
		Cover        string `json:"cover"`
		Video        string `json:"video"`
		VideoNoMusic string `json:"video_no_music"`
	}
	var vp struct {
		Media *json.RawMessage `json:"media"`
	}
	_ = json.Unmarshal(rel.Media, &media)
	_ = json.Unmarshal(rel.VideoPackage, &vp)
	if vp.Media != nil {
		_ = json.Unmarshal(*vp.Media, &packaged)
	}
	var key string
	switch kind {
	case "cover":
		key = media.Cover
	case "video":
		key = media.Video
	case "upload":
		key = packaged.VideoNoMusic
		if key == "" {
			key = media.Video
		}
	}
	if !strings.HasPrefix(key, "releases/") {
		return ""
	}
	return key
}

// backTo returns to the list when the form says so, otherwise to the video.
func backTo(r *http.Request, id string) string {
	if r.PostFormValue("back") == "list" {
		return "/admin"
	}
	return "/admin/r/" + url.PathEscape(id)
}

func redirect(w http.ResponseWriter, r *http.Request, to, msg string) {
	http.Redirect(w, r, to+"?msg="+url.QueryEscape(msg), http.StatusSeeOther)
}

func flashText(code string) (string, bool) {
	switch code {
	case "approve":
		return "Одобрено. Страница источников открыта.", false
	case "changes":
		return "Отправлено на доработку.", false
	case "reject":
		return "Отклонено.", false
	case "published":
		return "Записано: ролик вышел. В списке канала появится за несколько минут.", false
	case "unpublished":
		return "Записано: ролик не вышел. Страница источников остаётся открытой.", false
	case "withdraw":
		return "Страница источников закрыта. Сам ролик на площадках снимите вручную.", false
	case "withdraw_connected":
		return "Страница источников закрыта. Если ролик стоит в расписании YouTube, hub попробует снять его сам и напишет в группу, что вышло.", false
	case "yt_connected":
		return "Канал подключён: новые ролики hub загружает сам.", false
	case "yt_disconnected":
		return "Канал отключён: доступ hub отозван. Новые ролики ждут загрузки через API публикации, а уже стоящие в расписании выйдут, если не снять их в Studio.", false
	case "yt_disconnected_keep":
		return "Канал отключён, но Google не подтвердил отзыв доступа: проверьте страницу разрешений Google. Ролики, уже стоящие в расписании, выйдут, если не снять их в Studio.", true
	case "yt_denied":
		return "Доступ не выдан: ничего не изменилось.", true
	case "yt_bad_state":
		return "Ссылка на подключение устарела или не ваша. Попробуйте ещё раз.", true
	case "yt_no_refresh":
		return "Google не выдал постоянный доступ. Отзовите доступ на странице разрешений Google и подключите заново. Ничего не сохранено.", true
	case "yt_failed":
		return "Не получилось подключить канал, подробности в логах hub. Ничего не сохранено.", true
	case "link_saved":
		return "Ссылка сохранена.", false
	case "need_reason":
		return "Напишите причину.", true
	case "already":
		return "Статус уже изменился, действие не применено. Проверьте статус ниже.", true
	case "not_approved":
		return "Ссылку можно указать только у одобренного ролика.", true
	case "bad_link":
		return "Не похоже на ссылку на ролик YouTube.", true
	case "barred":
		return "Legal не допустил этот ролик на YouTube.", true
	}
	return "", false
}

func contains(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

// releaseID reads the release id from the URL; anything but a UUID is a page
// that doesn't exist, not a database error.
func releaseID(r *http.Request) (string, bool) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		return "", false
	}
	return id.String(), true
}

func statusOf(status string) (text, class string) {
	switch status {
	case "submitted":
		return "ждёт карточки", "wait"
	case "pending_approval":
		return "ждёт решения", "wait"
	case "approved":
		return "одобрен", "ok"
	case "published":
		return "вышел", "live"
	case "rejected":
		return "отклонён", "off"
	case "changes_requested":
		return "на доработке", "off"
	case "scheduled":
		return "в очереди", "ok"
	case "failed":
		return "сбой", "bad"
	}
	return status, "off"
}

func (h *Handler) when(t time.Time) string {
	return t.In(h.loc).Format("02.01 15:04")
}

func (h *Handler) fail(w http.ResponseWriter, err error) {
	if errors.Is(err, store.ErrNotFound) {
		http.Error(w, "Такого ролика нет", http.StatusNotFound)
		return
	}
	h.Log.Error("admin page failed", "error", err)
	http.Error(w, "Что-то пошло не так, подробности в логах hub", http.StatusInternalServerError)
}

// page writes a template under a Content-Security-Policy that allows only
// this page's own script (copy buttons, by nonce) and the object store's files.
func (h *Handler) page(w http.ResponseWriter, status int, t *template.Template, data any) {
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		h.fail(w, err)
		return
	}
	n := base64.RawStdEncoding.EncodeToString(nonce[:])
	var buf bytes.Buffer
	if err := t.Execute(&buf, struct {
		Nonce string
		Data  any
	}{n, data}); err != nil {
		h.fail(w, err)
		return
	}
	hdr := w.Header()
	hdr.Set("Content-Type", "text/html; charset=utf-8")
	hdr.Set("Content-Security-Policy", "default-src 'none'; img-src 'self' "+h.mediaCSP+"; media-src 'self' "+h.mediaCSP+
		"; style-src 'unsafe-inline'; script-src 'nonce-"+n+"'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'")
	w.WriteHeader(status)
	_, _ = w.Write(buf.Bytes())
}
