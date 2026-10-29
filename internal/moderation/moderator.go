// Package moderation is the Telegram moderation bot (docs/architecture.md §6,
// E4): every submitted release goes to the moderation group as a card -- the
// video, a summary, QA's and Legal's notes -- with the buttons
// «Опубликовать» / «Доработать» / «Отклонить». The group's administrators
// decide; a rejection or a request for changes asks them why.
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

	"github.com/tereshed/8bitnews/internal/store"
)

// Store is what the bot reads and writes (internal/store).
type Store interface {
	ClaimReleaseForCard(ctx context.Context, retryAfter time.Duration) (store.Release, bool, error)
	SetCardMessage(ctx context.Context, releaseID string, chatID, messageID int64) error
	ReleaseContextOf(ctx context.Context, releaseID string) (store.ReleaseContext, error)
	ListReleaseSources(ctx context.Context, releaseID string) ([]store.ReleaseSource, error)
	DecideRelease(ctx context.Context, releaseID, status, by string) (store.Release, bool, error)
	SetModerationNote(ctx context.Context, releaseID, note string) error
	LogEvent(ctx context.Context, kind string, data any) error
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
	// Telegram takes a video of up to 50 MB from a bot; a bigger one gets a
	// text card.
	maxVideoBytes = 49 << 20
	eventKind     = "moderation"
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
	log    *slog.Logger

	botID int64

	mu       sync.Mutex
	admins   map[int64]bool
	adminsAt time.Time
}

// New builds the bot for the group chatID, looking for new releases every
// `every`.
func New(tg *Telegram, st Store, media Media, chatID int64, every time.Duration, log *slog.Logger) *Moderator {
	return &Moderator{tg: tg, store: st, media: media, chatID: chatID, every: every, log: log}
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
		for {
			m.PostCards(ctx)
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
	if err := m.tg.RemoveKeyboard(ctx, m.chatID, q.Message.MessageID); err != nil {
		m.log.Warn("moderation: removing the buttons failed", "release_id", releaseID, "error", err)
	}
	var reply string
	var markup any
	switch r.Status {
	case "approved":
		reply = fmt.Sprintf("✅ Одобрено — %s. Публикация на площадки появится следующим шагом E4.", who)
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
	m.answer(ctx, q.ID, "Записано: релиз "+statusText(r.Status)+".", false)
	m.event(ctx, map[string]any{"release_id": r.ID, "action": r.Status, "by": who, "telegram_user_id": q.From.ID})
}

func (m *Moderator) onMessage(ctx context.Context, msg Message) {
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
