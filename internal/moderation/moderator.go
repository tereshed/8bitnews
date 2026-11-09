// Package moderation is the Telegram moderation bot (docs/architecture.md §6,
// E4): every submitted release goes to the moderation group as a card -- the
// video, a summary, QA's and Legal's notes -- with the buttons
// «Опубликовать» / «Доработать» / «Отклонить». The group's administrators
// decide; a rejection or a request for changes asks them why, an approval
// gets the publication package (package.go) and the card two new buttons:
// the package again, and «Выложено» once the video is up (and back).
package moderation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"path"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/tereshed/8bitnews/internal/publish"
	"github.com/tereshed/8bitnews/internal/store"
)

// Store is what the bot reads and writes (internal/store).
type Store interface {
	ClaimReleaseForCard(ctx context.Context, retryAfter time.Duration) (store.Release, bool, error)
	SetCardMessage(ctx context.Context, releaseID string, chatID, messageID int64) error
	ReleaseContextOf(ctx context.Context, releaseID string) (store.ReleaseContext, error)
	ListReleaseSources(ctx context.Context, releaseID string) ([]store.ReleaseSource, error)
	DecideRelease(ctx context.Context, releaseID, status, by string) (store.Release, bool, error)
	GetRelease(ctx context.Context, id string) (store.Release, error)
	SetPublished(ctx context.Context, releaseID string, published bool) (store.Release, bool, error)
	WithdrawRelease(ctx context.Context, releaseID, by string) (store.Release, bool, error)
	SetModerationNote(ctx context.Context, releaseID, note string) error
	LogEvent(ctx context.Context, kind string, data any) error
	GetChannel(ctx context.Context, id string) (store.Channel, error)
	GetCountry(ctx context.Context, cc string) (store.Country, error)
	ReleasesAwaitingDecision(ctx context.Context) ([]store.Release, error)
	PublicationsToCheck(ctx context.Context) ([]store.PublicationCheck, error)
	MarkPublicationOverdue(ctx context.Context, releaseID, platform string) error
	StalledUploads(ctx context.Context, before time.Time) ([]store.StalledUpload, error)
	MarkUploadAlerted(ctx context.Context, releaseID string) error
}

// Media opens a release's video in the object store (internal/storage).
type Media interface {
	Open(ctx context.Context, key string) (io.ReadCloser, int64, error)
}

const (
	pollTimeout  = 50 * time.Second // getUpdates long poll
	cardsPerTick = 5
	// A release claimed for a card that never got posted (a failed upload, a
	// restart half-way) is tried again after this long; meanwhile newer
	// releases go ahead.
	cardRetry = 15 * time.Minute
	adminsTTL = 5 * time.Minute
	// How old a list of administrators the admin page accepts when Telegram
	// won't give a new one.
	adminsMaxAge          = 15 * time.Minute
	moderatorCheckTimeout = 5 * time.Second
	actFollowUpTimeout    = 15 * time.Second
	// Telegram takes a video of up to 50 MB from a bot; a bigger one gets a
	// text card.
	maxVideoBytes = 49 << 20
	// «📦 Пакет ещё раз» right after a package went out is a press made while
	// it was going out.
	packageCooldown = time.Minute
	eventKind       = "moderation"
)

// The prompts that ask for a reason, and how they end: "(релиз <uuid>)", so a
// reply finds its release again, even after a restart. Only the bot's own
// prompt counts: it starts with one of the prefixes and ends with the id
// (a moderator's name in the middle can't fake it).
var (
	reasonPrompts = []string{"✏️ На доработку — ", "❌ Отклонено — "}
	releaseRef    = regexp.MustCompile(`\(релиз ([0-9a-f-]{36})\)$`)
)

// Moderator runs the bot: Run posts cards and handles the group.
type Moderator struct {
	tg     *Telegram
	store  Store
	media  Media
	chatID int64
	every  time.Duration
	pub    publish.Config
	log    *slog.Logger

	botID int64

	mu       sync.Mutex
	admins   map[int64]bool
	adminsAt time.Time
	packaged map[string]time.Time // when each release's package last finished going out

	adminLogin func(userID int64, who string) string // nil: no admin page

	videoVisible func(ctx context.Context, url string) (visible, known bool) // nil: hub doesn't look
}

// New builds the bot for the group chatID, looking for new releases every
// `every`; pub is where the publication package's sources pages live.
func New(tg *Telegram, st Store, media Media, chatID int64, every time.Duration, pub publish.Config, log *slog.Logger) *Moderator {
	return &Moderator{tg: tg, store: st, media: media, chatID: chatID, every: every, pub: pub, log: log, packaged: map[string]time.Time{}}
}

// syncNewCard fixes the buttons of a card that has just gone out if the release
// was decided on the admin page during the upload: Act found no card to update.
func (m *Moderator) syncNewCard(ctx context.Context, releaseID string, messageID int64) {
	r, err := m.store.GetRelease(ctx, releaseID)
	if err != nil {
		m.log.Warn("moderation: checking a new card's release failed", "release_id", releaseID, "error", err)
		return
	}
	switch r.Status {
	case "pending_approval":
	case "approved", "published":
		m.setKeyboard(ctx, r.ID, messageID, approvedKeyboard(r.ID, r.Status == "published"))
	default:
		m.setKeyboard(ctx, r.ID, messageID, &Keyboard{InlineKeyboard: [][]Button{}})
	}
}

// SetAdminLogin makes the bot answer /admin in a private chat: to a moderator,
// with the login link login returns. Without it the bot ignores /admin.
func (m *Moderator) SetAdminLogin(login func(userID int64, who string) string) {
	m.adminLogin = login
}

// Run posts cards and answers the group until ctx ends.
func (m *Moderator) Run(ctx context.Context) {
	for m.botID == 0 {
		me, err := m.tg.GetMe(ctx)
		if err == nil {
			m.botID = me.ID
			break
		}
		m.log.Error("moderation: getMe failed", "error", err)
		if !sleep(ctx, time.Minute) {
			return
		}
	}
	m.log.Info("moderation bot running", "chat_id", m.chatID, "bot_id", m.botID)
	go func() {
		var checked time.Time
		for {
			m.PostCards(ctx)
			m.AutoApprove(ctx)
			if time.Since(checked) >= publicationCheckEvery {
				m.CheckPublications(ctx)
				m.CheckStalledUploads(ctx)
				checked = time.Now()
			}
			if !sleep(ctx, m.every) {
				return
			}
		}
	}()
	var offset int64
	backoff := 5 * time.Second
	for ctx.Err() == nil {
		updates, err := m.tg.GetUpdates(ctx, offset, pollTimeout)
		if err != nil {
			if ctx.Err() == nil {
				// 409: another process polls this bot (another hub with the same token) or it has a webhook.
				m.log.Error("moderation: getUpdates failed", "error", err, "retry_in", backoff.String())
				sleep(ctx, backoff)
				backoff = min(backoff*2, time.Minute)
			}
			continue
		}
		backoff = 5 * time.Second
		for _, u := range updates {
			offset = u.UpdateID + 1
			m.Handle(ctx, u)
		}
	}
}

func sleep(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

// PostCards posts a card for releases that await one, a few per call. A
// release whose card fails stays claimed and is tried again after cardRetry,
// without holding up the others.
func (m *Moderator) PostCards(ctx context.Context) {
	for i := 0; i < cardsPerTick; i++ {
		r, ok, err := m.store.ClaimReleaseForCard(ctx, cardRetry)
		if err != nil {
			m.log.Error("moderation: claiming a release", "error", err)
			return
		}
		if !ok {
			return
		}
		if err := m.postCard(ctx, r); err != nil {
			m.log.Error("moderation: posting a card failed; trying again later", "release_id", r.ID,
				"retry_in", cardRetry.String(), "error", err)
		}
	}
}

func (m *Moderator) postCard(ctx context.Context, r store.Release) error {
	var media struct {
		Video string `json:"video"`
	}
	if err := json.Unmarshal(r.Media, &media); err != nil || media.Video == "" {
		return fmt.Errorf("the release names no video")
	}
	rc, err := m.store.ReleaseContextOf(ctx, r.ID)
	if err != nil {
		return err
	}
	sources, err := m.store.ListReleaseSources(ctx, r.ID)
	if err != nil {
		return err
	}
	card := BuildCard(r, rc, sources)
	video, size, err := m.media.Open(ctx, media.Video)
	if err != nil {
		return err
	}
	defer func() { _ = video.Close() }()
	var msg Message
	if size > maxVideoBytes {
		text := card.Caption + fmt.Sprintf("\n\n⚠️ Видео %d МБ — больше, чем бот может прислать в Telegram (50 МБ).", size>>20)
		msg, err = m.tg.SendMessage(ctx, m.chatID, text, 0, cardKeyboard(r.ID))
	} else {
		msg, err = m.tg.SendVideo(ctx, m.chatID, path.Base(media.Video), video, card.Caption, cardKeyboard(r.ID))
	}
	if err != nil {
		return err
	}
	// The card is out: record it even if hub is stopping, or it would be posted again.
	if err := m.store.SetCardMessage(context.WithoutCancel(ctx), r.ID, m.chatID, msg.MessageID); err != nil {
		return err
	}
	m.syncNewCard(ctx, r.ID, msg.MessageID)
	if card.Details != "" {
		if _, err := m.tg.SendMessage(ctx, m.chatID, card.Details, msg.MessageID, nil); err != nil {
			m.log.Error("moderation: posting the card's details failed", "release_id", r.ID, "error", err)
		}
	}
	m.event(ctx, map[string]any{"release_id": r.ID, "action": "card", "message_id": msg.MessageID})
	return nil
}

// Handle answers one update from the group: a button press, or a reply
// giving the reason for a decision.
func (m *Moderator) Handle(ctx context.Context, u Update) {
	switch {
	case u.CallbackQuery != nil:
		m.onButton(ctx, *u.CallbackQuery)
	case u.Message != nil:
		m.onMessage(ctx, *u.Message)
	}
}

func (m *Moderator) onButton(ctx context.Context, q CallbackQuery) {
	action, releaseID, ok := parseCallback(q.Data)
	if !ok || q.Message == nil || q.Message.Chat.ID != m.chatID {
		m.answer(ctx, q.ID, "", false)
		return
	}
	if !m.isModerator(ctx, q.From.ID) {
		m.answer(ctx, q.ID, "Решают только администраторы группы.", true)
		return
	}
	who := name(q.From)
	switch action {
	case actionPackage:
		m.packageAgain(ctx, q, releaseID, who)
		return
	case actionPublished, actionUnpublished:
		m.setPublished(ctx, q, releaseID, who, action == actionPublished)
		return
	case actionWithdraw:
		m.withdraw(ctx, q, releaseID, who)
		return
	}
	r, changed, err := m.store.DecideRelease(ctx, releaseID, actions[action], who)
	if errors.Is(err, store.ErrNotFound) {
		m.answer(ctx, q.ID, "Такого релиза нет.", true)
		return
	}
	if err != nil {
		m.log.Error("moderation: deciding failed", "release_id", releaseID, "error", err)
		m.answer(ctx, q.ID, "Не получилось записать решение, попробуйте ещё раз.", true)
		return
	}
	if !changed {
		m.answer(ctx, q.ID, "Уже решено: релиз "+statusText(r.Status)+".", true)
		return
	}
	// The decision is recorded: say so at once, before the package's uploads.
	m.answer(ctx, q.ID, "Записано: релиз "+statusText(r.Status)+".", false)
	m.event(ctx, map[string]any{"release_id": r.ID, "action": r.Status, "by": who, "telegram_user_id": q.From.ID})
	if r.Status == "approved" {
		m.setKeyboard(ctx, r.ID, q.Message.MessageID, approvedKeyboard(r.ID, false))
		m.sendPackage(ctx, r, q.Message.MessageID, "✅ Одобрено — "+who+".")
		return
	}
	m.setKeyboard(ctx, r.ID, q.Message.MessageID, &Keyboard{InlineKeyboard: [][]Button{}})
	var reply string
	var markup any
	switch r.Status {
	case "changes_requested":
		reply = fmt.Sprintf("✏️ На доработку — %s. Что доработать? Ответьте на это сообщение (релиз %s)", who, r.ID)
		markup = forceReply{ForceReply: true, InputFieldPlaceholder: "Что доработать"}
	case "rejected":
		reply = fmt.Sprintf("❌ Отклонено — %s. Почему? Ответьте на это сообщение (релиз %s)", who, r.ID)
		markup = forceReply{ForceReply: true, InputFieldPlaceholder: "Почему отклонено"}
	}
	if _, err := m.tg.SendMessage(ctx, m.chatID, reply, q.Message.MessageID, markup); err != nil {
		m.log.Error("moderation: confirming the decision failed", "release_id", releaseID, "error", err)
	}
}

// packageAgain posts an approved release's package once more: a part that
// didn't arrive, or a fresh copy for a later upload.
func (m *Moderator) packageAgain(ctx context.Context, q CallbackQuery, releaseID, who string) {
	r, err := m.store.GetRelease(ctx, releaseID)
	if errors.Is(err, store.ErrNotFound) {
		m.answer(ctx, q.ID, "Такого релиза нет.", true)
		return
	}
	if err != nil {
		m.log.Error("moderation: reading the release failed", "release_id", releaseID, "error", err)
		m.answer(ctx, q.ID, "Не получилось прочитать релиз, попробуйте ещё раз.", true)
		return
	}
	if r.Status != "approved" && r.Status != "published" {
		m.answer(ctx, q.ID, "Пакет — только для одобренного релиза, а этот "+statusText(r.Status)+".", true)
		return
	}
	// Presses wait while a package goes out; one made meanwhile would only
	// send the same package twice.
	m.mu.Lock()
	sent, recent := m.packaged[r.ID]
	m.mu.Unlock()
	if recent && time.Since(sent) < packageCooldown {
		m.answer(ctx, q.ID, "Пакет только что отправлен. Если чего-то не хватает, нажмите ещё раз через минуту.", true)
		return
	}
	m.answer(ctx, q.ID, "Отправляю пакет.", false)
	m.sendPackage(ctx, r, q.Message.MessageID, "📦 Пакет ещё раз — для "+who+".")
}

// setPublished records the moderator's word that the video is up on the
// platforms, which puts it on the channel's list of videos, or takes it back.
func (m *Moderator) setPublished(ctx context.Context, q CallbackQuery, releaseID, who string, published bool) {
	r, changed, err := m.store.SetPublished(ctx, releaseID, published)
	if errors.Is(err, store.ErrNotFound) {
		m.answer(ctx, q.ID, "Такого релиза нет.", true)
		return
	}
	if err != nil {
		m.log.Error("moderation: recording the publication failed", "release_id", releaseID, "error", err)
		m.answer(ctx, q.ID, "Не получилось записать, попробуйте ещё раз.", true)
		return
	}
	if !changed {
		m.answer(ctx, q.ID, "Уже записано: релиз "+statusText(r.Status)+".", true)
		return
	}
	m.answer(ctx, q.ID, "Записано: релиз "+statusText(r.Status)+".", false)
	action := "published"
	if !published {
		action = "unpublished"
	}
	m.event(ctx, map[string]any{"release_id": r.ID, "action": action, "by": who, "telegram_user_id": q.From.ID})
	m.setKeyboard(ctx, r.ID, q.Message.MessageID, approvedKeyboard(r.ID, published))
	if _, err := m.tg.SendMessage(ctx, m.chatID, publishedReply(who, published), q.Message.MessageID, nil); err != nil {
		m.log.Error("moderation: confirming the publication failed", "release_id", releaseID, "error", err)
	}
}

// withdraw closes an approved release's sources page: a mistaken approval, a
// claim. There's no button back: reopening takes a change in the database.
func (m *Moderator) withdraw(ctx context.Context, q CallbackQuery, releaseID, who string) {
	r, changed, err := m.store.WithdrawRelease(ctx, releaseID, who)
	if errors.Is(err, store.ErrNotFound) {
		m.answer(ctx, q.ID, "Такого релиза нет.", true)
		return
	}
	if err != nil {
		m.log.Error("moderation: withdrawing the release failed", "release_id", releaseID, "error", err)
		m.answer(ctx, q.ID, "Не получилось записать, попробуйте ещё раз.", true)
		return
	}
	if !changed {
		m.answer(ctx, q.ID, "Уже решено: релиз "+statusText(r.Status)+".", true)
		return
	}
	m.answer(ctx, q.ID, "Записано: релиз "+statusText(r.Status)+".", false)
	m.event(ctx, map[string]any{"release_id": r.ID, "action": "withdrawn", "by": who, "telegram_user_id": q.From.ID})
	m.setKeyboard(ctx, r.ID, q.Message.MessageID, &Keyboard{InlineKeyboard: [][]Button{}})
	if _, err := m.tg.SendMessage(ctx, m.chatID, withdrawnReply(who), q.Message.MessageID, nil); err != nil {
		m.log.Error("moderation: confirming the withdrawal failed", "release_id", releaseID, "error", err)
	}
}

func publishedReply(who string, published bool) string {
	if published {
		return "🟢 Выложено — " + who + ". Ролик появится в списке канала за несколько минут."
	}
	return "↩️ Не выложено — " + who + ". Ролик уйдёт из списка канала за несколько минут, страница источников остаётся открытой."
}

func withdrawnReply(who string) string {
	return "🚫 Страница источников закрыта — " + who + ". Несколько минут она ещё может открываться из кэша. Сам ролик на площадках снимите вручную; если он стоит в расписании, то до слота, иначе он выйдет."
}

// Act applies a moderator's decision made on the admin page: the same status
// changes as the card's buttons, with the card and the group kept up to date.
// action is one of approve, changes, reject, published, unpublished, withdraw.
// It reports false, with the release as it is, if the release wasn't in the
// status to move from; then nothing else happens. The publication package
// isn't posted: the admin page shows it, and the card's button still sends it.
func (m *Moderator) Act(ctx context.Context, releaseID, action, who string, userID int64, note string) (store.Release, bool, error) {
	var (
		r       store.Release
		changed bool
		err     error
	)
	switch action {
	case "approve", "changes", "reject":
		r, changed, err = m.store.DecideRelease(ctx, releaseID, actions[action], who)
	case actionPublished, actionUnpublished:
		r, changed, err = m.store.SetPublished(ctx, releaseID, action == actionPublished)
	case actionWithdraw:
		r, changed, err = m.store.WithdrawRelease(ctx, releaseID, who)
	default:
		return store.Release{}, false, fmt.Errorf("unknown moderation action %q", action)
	}
	if err != nil || !changed {
		return r, changed, err
	}
	// The status has moved: the rest happens even if the browser has gone, and
	// doesn't hold the request while Telegram asks to slow down.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), actFollowUpTimeout)
	defer cancel()
	if note != "" && (r.Status == "rejected" || r.Status == "changes_requested") {
		if err := m.store.SetModerationNote(ctx, r.ID, note); err != nil {
			m.log.Error("moderation: recording the reason failed", "release_id", r.ID, "error", err)
		} else {
			r.Moderation.Note = note
		}
	}
	event := map[string]any{"release_id": r.ID, "action": r.Status, "by": who, "telegram_user_id": userID, "via": "admin"}
	var (
		reply    string
		keyboard = &Keyboard{InlineKeyboard: [][]Button{}}
	)
	switch action {
	case "approve":
		reply, keyboard = "✅ Одобрено — "+who+". Пакет для загрузки — на странице ролика в админке и под кнопкой на карточке.", approvedKeyboard(r.ID, false)
	case "changes":
		reply = "✏️ На доработку — " + who + "."
	case "reject":
		reply = "❌ Отклонено — " + who + "."
	case actionPublished, actionUnpublished:
		published := action == actionPublished
		reply, keyboard = publishedReply(who, published), approvedKeyboard(r.ID, published)
		if !published {
			event["action"] = "unpublished"
		}
	case actionWithdraw:
		reply = withdrawnReply(who)
		event["action"] = "withdrawn"
	}
	if note != "" {
		reply += "\nПричина: " + note
		event["note"] = note
	}
	m.event(ctx, event)
	var cardID int64
	if r.Moderation.MessageID != nil {
		cardID = *r.Moderation.MessageID
		m.setKeyboard(ctx, r.ID, cardID, keyboard)
	}
	if _, err := m.tg.SendMessage(ctx, m.chatID, truncate(reply, messageLimit), cardID, nil); err != nil {
		m.log.Error("moderation: telling the group about an admin decision failed", "release_id", r.ID, "error", err)
	}
	return r, true, nil
}

// IsModerator: the group's human administrators, the same people who may
// press the card's buttons. Unlike the buttons, whose presses Telegram itself
// vouches for, the admin page must not trust an old list: if Telegram hasn't
// confirmed it for adminsMaxAge, the answer is no.
func (m *Moderator) IsModerator(ctx context.Context, userID int64) bool {
	ctx, cancel := context.WithTimeout(ctx, moderatorCheckTimeout)
	defer cancel()
	known := m.isModerator(ctx, userID)
	m.mu.Lock()
	age := time.Since(m.adminsAt)
	m.mu.Unlock()
	return known && age < adminsMaxAge
}

func (m *Moderator) setKeyboard(ctx context.Context, releaseID string, messageID int64, kb *Keyboard) {
	if err := m.tg.SetKeyboard(ctx, m.chatID, messageID, kb); err != nil {
		m.log.Warn("moderation: changing the card's buttons failed", "release_id", releaseID, "error", err)
	}
}

func (m *Moderator) onMessage(ctx context.Context, msg Message) {
	if m.adminLogin != nil && msg.From != nil && isAdminCommand(msg) {
		m.onAdminCommand(ctx, msg)
		return
	}
	if msg.Chat.ID != m.chatID || msg.From == nil || msg.ReplyTo == nil || msg.ReplyTo.From == nil ||
		msg.ReplyTo.From.ID != m.botID || strings.TrimSpace(msg.Text) == "" {
		return
	}
	ref := releaseRef.FindStringSubmatch(msg.ReplyTo.Text)
	if ref == nil || !isReasonPrompt(msg.ReplyTo.Text) || !m.isModerator(ctx, msg.From.ID) {
		return
	}
	note := strings.TrimSpace(msg.Text)
	if err := m.store.SetModerationNote(ctx, ref[1], note); err != nil {
		m.log.Error("moderation: recording the reason failed", "release_id", ref[1], "error", err)
		return
	}
	if _, err := m.tg.SendMessage(ctx, m.chatID, "Записал причину.", msg.MessageID, nil); err != nil {
		m.log.Warn("moderation: acknowledging the reason failed", "release_id", ref[1], "error", err)
	}
	m.event(ctx, map[string]any{"release_id": ref[1], "action": "note", "by": name(*msg.From), "note": note})
}

// isAdminCommand: /admin anywhere; /start too in a private chat, since that's
// what Telegram's Start button sends the first time someone opens the bot.
func isAdminCommand(msg Message) bool {
	command, _, _ := strings.Cut(strings.TrimSpace(msg.Text), " ")
	command, _, _ = strings.Cut(command, "@")
	return command == "/admin" || (command == "/start" && msg.Chat.Type == "private")
}

// onAdminCommand gives a moderator a login link, in a private chat only: the
// link is personal and the group could see it. Anyone else gets no answer, so
// strangers can't make the bot's polling loop wait on Telegram's rate limit.
func (m *Moderator) onAdminCommand(ctx context.Context, msg Message) {
	private := msg.Chat.Type == "private"
	if (!private && msg.Chat.ID != m.chatID) || !m.isModerator(ctx, msg.From.ID) {
		return
	}
	var reply string
	switch {
	case !private:
		reply = "Напишите мне /admin в личные сообщения: ссылка на вход личная."
	default:
		who := name(*msg.From)
		reply = "Вход в админку: " + m.adminLogin(msg.From.ID, who) + "\nСсылка действует 10 минут, один раз."
		m.event(ctx, map[string]any{"action": "admin_login_link", "by": who, "telegram_user_id": msg.From.ID})
	}
	if _, err := m.tg.SendMessage(ctx, msg.Chat.ID, reply, 0, nil); err != nil {
		m.log.Error("moderation: answering /admin failed", "error", err)
	}
}

func isReasonPrompt(text string) bool {
	for _, p := range reasonPrompts {
		if strings.HasPrefix(text, p) {
			return true
		}
	}
	return false
}

// isModerator: the group's human administrators decide. The list is cached;
// someone not on it makes it refresh, at most once a minute.
func (m *Moderator) isModerator(ctx context.Context, userID int64) bool {
	m.mu.Lock()
	known, fresh := m.admins[userID], time.Since(m.adminsAt) < adminsTTL
	stale := time.Since(m.adminsAt) > time.Minute
	m.mu.Unlock()
	if known && fresh {
		return true
	}
	if fresh && !stale {
		return known
	}
	members, err := m.tg.ChatAdministrators(ctx, m.chatID)
	if err != nil {
		m.log.Error("moderation: listing the group's administrators failed", "error", err)
		return known
	}
	admins := map[int64]bool{}
	for _, a := range members {
		if !a.User.IsBot && (a.Status == "creator" || a.Status == "administrator") {
			admins[a.User.ID] = true
		}
	}
	m.mu.Lock()
	m.admins, m.adminsAt = admins, time.Now()
	m.mu.Unlock()
	return admins[userID]
}

func (m *Moderator) answer(ctx context.Context, callbackID, text string, alert bool) {
	if err := m.tg.AnswerCallback(ctx, callbackID, text, alert); err != nil {
		m.log.Warn("moderation: answering a button press failed", "error", err)
	}
}

func (m *Moderator) event(ctx context.Context, data map[string]any) {
	if err := m.store.LogEvent(ctx, eventKind, data); err != nil {
		m.log.Error("moderation: logging the event failed", "error", err)
	}
}

// name is how a decision is signed: "Kostya (@kteresh)".
func name(u User) string {
	n := strings.TrimSpace(u.FirstName)
	if n == "" {
		n = fmt.Sprintf("id %d", u.ID)
	}
	if u.Username != "" {
		n += " (@" + u.Username + ")"
	}
	return n
}
