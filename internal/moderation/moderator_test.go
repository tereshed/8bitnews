package moderation

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tereshed/8bitnews/internal/store"
)

const (
	chatID  = int64(-500)
	botID   = int64(7)
	adminID = int64(35)
	otherID = int64(99)
	release = "7f01440f-4314-4406-9667-7b9c49d4d973"
)

// fakeTelegram records every Bot API call and answers like the real one.
type fakeTelegram struct {
	mu      sync.Mutex
	calls   []call
	nextMsg int64
}

type call struct {
	method string
	params map[string]any
	video  []byte
}

func (f *fakeTelegram) handler(t *testing.T) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
		c := call{method: method, params: map[string]any{}}
		if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/") {
			if err := r.ParseMultipartForm(1 << 20); err != nil {
				t.Errorf("%s: %v", method, err)
			}
			for k, v := range r.MultipartForm.Value {
				c.params[k] = v[0]
			}
			if fh := r.MultipartForm.File["video"]; len(fh) == 1 {
				f, _ := fh[0].Open()
				c.video, _ = io.ReadAll(f)
			}
		} else {
			_ = json.NewDecoder(r.Body).Decode(&c.params)
		}
		f.mu.Lock()
		f.calls = append(f.calls, c)
		f.nextMsg++
		id := f.nextMsg
		f.mu.Unlock()
		var result any = true
		switch method {
		case "getMe":
			result = User{ID: botID, IsBot: true, FirstName: "8BitNewsBot"}
		case "sendVideo", "sendMessage":
			result = Message{MessageID: 1000 + id, Chat: Chat{ID: chatID}}
		case "getChatAdministrators":
			result = []ChatMember{
				{Status: "creator", User: User{ID: adminID, FirstName: "Kostya", Username: "kteresh"}},
				{Status: "administrator", User: User{ID: botID, IsBot: true}},
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": result})
	})
}

func (f *fakeTelegram) byMethod(method string) []call {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []call
	for _, c := range f.calls {
		if c.method == method {
			out = append(out, c)
		}
	}
	return out
}

// fakeStore keeps one release in memory.
type fakeStore struct {
	mu      sync.Mutex
	release store.Release
	carded  int64
	note    string
	events  []map[string]any
}

func (s *fakeStore) ClaimReleaseForCard(context.Context, time.Duration) (store.Release, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.release.Status != "submitted" { // a claimed release waits out the retry delay
		return store.Release{}, false, nil
	}
	s.release.Status = "pending_approval"
	return s.release, true, nil
}

func (s *fakeStore) ReleaseContextOf(context.Context, string) (store.ReleaseContext, error) {
	return store.ReleaseContext{StoryTitle: "DOJ v. California", DevelopmentSummary: "Judge weighs a special master"}, nil
}

func (s *fakeStore) ListReleaseSources(context.Context, string) ([]store.ReleaseSource, error) {
	return []store.ReleaseSource{{ArchiveURL: "https://web.archive.org/x"}, {}}, nil
}

func (s *fakeStore) SetCardMessage(_ context.Context, _ string, _, messageID int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.carded = messageID
	s.release.Moderation.MessageID = &messageID
	return nil
}

func (s *fakeStore) DecideRelease(_ context.Context, id, status, by string) (store.Release, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if id != s.release.ID {
		return store.Release{}, false, store.ErrNotFound
	}
	if s.release.Status != "pending_approval" {
		return s.release, false, nil
	}
	s.release.Status, s.release.Moderation.By = status, by
	return s.release, true, nil
}

func (s *fakeStore) SetModerationNote(_ context.Context, _, note string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.release.Status != "rejected" && s.release.Status != "changes_requested" {
		return store.ErrNotFound
	}
	s.note = note
	return nil
}

func (s *fakeStore) LogEvent(_ context.Context, _ string, data any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, data.(map[string]any))
	return nil
}

type fakeMedia struct {
	opened string
	size   int64
	err    error
}

func (m *fakeMedia) Open(_ context.Context, key string) (io.ReadCloser, int64, error) {
	m.opened = key
	if m.err != nil {
		return nil, 0, m.err
	}
	size := m.size
	if size == 0 {
		size = 9
	}
	return io.NopCloser(bytes.NewReader([]byte("MP4-BYTES"))), size, nil
}

func newBot(t *testing.T, status string) (*Moderator, *fakeTelegram, *fakeStore, *fakeMedia) {
	t.Helper()
	tg := &fakeTelegram{}
	srv := httptest.NewServer(tg.handler(t))
	t.Cleanup(srv.Close)
	st := &fakeStore{release: store.Release{
		ID: release, Status: status, AllowedPlatforms: []string{"youtube", "tiktok"},
		Media:            json.RawMessage(`{"video":"releases/dev/abc-video.mp4"}`),
		VideoPackage:     json.RawMessage(`{"duration_s":81.8,"template":"nes-desk","anchor":{"id":"w8"}}`),
		ValidationReport: json.RawMessage(`{"verdict":"pass","issues":[{"severity":"minor","scene_id":"S4","finding":"an extra figure"}]}`),
		LegalReview:      json.RawMessage(`{"verdict":"pass","publish_notes":["No paid promotion."]}`),
	}}
	media := &fakeMedia{}
	m := New(NewTelegram(srv.URL, "123:SECRET"), st, media, chatID, time.Minute, slog.New(slog.NewTextHandler(io.Discard, nil)))
	m.botID = botID
	return m, tg, st, media
}

func press(action, from int64) Update {
	return Update{CallbackQuery: &CallbackQuery{
		ID: "cb", From: User{ID: from, FirstName: "Kostya", Username: "kteresh"},
		Message: &Message{MessageID: 1001, Chat: Chat{ID: chatID}},
		Data:    fmt.Sprintf("mod:%s:%s", map[int64]string{1: "approve", 2: "changes", 3: "reject"}[action], release),
	}}
}

func TestACardCarriesTheVideoTheSummaryAndTheButtons(t *testing.T) {
	m, tg, st, media := newBot(t, "submitted")
	m.PostCards(context.Background())

	videos := tg.byMethod("sendVideo")
	if len(videos) != 1 {
		t.Fatalf("sendVideo calls: %d, want 1", len(videos))
	}
	v := videos[0]
	if string(v.video) != "MP4-BYTES" || media.opened != "releases/dev/abc-video.mp4" {
		t.Fatalf("the video sent is %q from %q", v.video, media.opened)
	}
	caption := v.params["caption"].(string)
	for _, want := range []string{"DOJ v. California", "82 с", "ведущий w8", "youtube, tiktok", "QA: pass", "minor 1", "Legal: pass",
		"заметок к публикации 1", "Источников 2, в архиве 1", release} {
		if !strings.Contains(caption, want) {
			t.Errorf("caption lacks %q:\n%s", want, caption)
		}
	}
	if kb := v.params["reply_markup"].(string); !strings.Contains(kb, "mod:approve:"+release) || !strings.Contains(kb, "mod:reject:"+release) {
		t.Errorf("keyboard = %s", kb)
	}
	if st.release.Status != "pending_approval" || st.carded == 0 {
		t.Fatalf("after the card the release is %s (card %d)", st.release.Status, st.carded)
	}
	details := tg.byMethod("sendMessage")
	if len(details) != 1 || !strings.Contains(details[0].params["text"].(string), "No paid promotion.") ||
		!strings.Contains(details[0].params["text"].(string), "an extra figure") {
		t.Fatalf("details = %v", details)
	}

	m.PostCards(context.Background())
	if len(tg.byMethod("sendVideo")) != 1 {
		t.Fatal("a carded release got a second card")
	}
}

func TestAnAdministratorApproves(t *testing.T) {
	m, tg, st, _ := newBot(t, "pending_approval")
	m.Handle(context.Background(), press(1, adminID))

	if st.release.Status != "approved" || st.release.Moderation.By != "Kostya (@kteresh)" {
		t.Fatalf("release = %s by %q", st.release.Status, st.release.Moderation.By)
	}
	if len(tg.byMethod("editMessageReplyMarkup")) != 1 {
		t.Error("the buttons stayed on the card")
	}
	if msgs := tg.byMethod("sendMessage"); len(msgs) != 1 || !strings.Contains(msgs[0].params["text"].(string), "Одобрено — Kostya") {
		t.Errorf("confirmation = %v", msgs)
	}
	if len(st.events) != 1 || st.events[0]["action"] != "approved" {
		t.Errorf("events = %v", st.events)
	}
}

func TestSomeoneElseCannotDecide(t *testing.T) {
	m, tg, st, _ := newBot(t, "pending_approval")
	m.Handle(context.Background(), press(1, otherID))

	if st.release.Status != "pending_approval" {
		t.Fatalf("a non-administrator moved the release to %s", st.release.Status)
	}
	answers := tg.byMethod("answerCallbackQuery")
	if len(answers) != 1 || answers[0].params["show_alert"] != true || !strings.Contains(answers[0].params["text"].(string), "администраторы") {
		t.Fatalf("answer = %v", answers)
	}
}

func TestASecondPressDoesNotOverturnTheDecision(t *testing.T) {
	m, tg, st, _ := newBot(t, "approved")
	m.Handle(context.Background(), press(3, adminID))
	if st.release.Status != "approved" {
		t.Fatalf("the decision was overturned: %s", st.release.Status)
	}
	if a := tg.byMethod("answerCallbackQuery"); len(a) != 1 || !strings.Contains(a[0].params["text"].(string), "Уже решено: релиз одобрен") {
		t.Fatalf("answer = %v", a)
	}
}

func TestARejectionAsksWhyAndRecordsTheReply(t *testing.T) {
	m, tg, st, _ := newBot(t, "pending_approval")
	ctx := context.Background()
	m.Handle(ctx, press(3, adminID))
	if st.release.Status != "rejected" {
		t.Fatalf("status = %s", st.release.Status)
	}
	prompts := tg.byMethod("sendMessage")
	if len(prompts) != 1 {
		t.Fatalf("prompts = %v", prompts)
	}
	prompt := prompts[0].params["text"].(string)
	markup, _ := json.Marshal(prompts[0].params["reply_markup"])
	if !strings.Contains(prompt, "(релиз "+release+")") || !strings.Contains(string(markup), `"force_reply":true`) {
		t.Fatalf("prompt = %q, markup %s", prompt, markup)
	}

	reply := func(from int64, text string) Update {
		return Update{Message: &Message{MessageID: 2000, From: &User{ID: from, FirstName: "x"}, Chat: Chat{ID: chatID}, Text: text,
			ReplyTo: &Message{MessageID: 1002, From: &User{ID: botID, IsBot: true}, Text: prompt}}}
	}
	m.Handle(ctx, reply(otherID, "I don't like it"))
	if st.note != "" {
		t.Fatalf("a non-administrator's reply was recorded: %q", st.note)
	}
	m.Handle(ctx, reply(adminID, "  The cover shows the wrong person.  "))
	if st.note != "The cover shows the wrong person." {
		t.Fatalf("note = %q", st.note)
	}
	if last := tg.byMethod("sendMessage"); !strings.Contains(last[len(last)-1].params["text"].(string), "Записал причину") {
		t.Errorf("no acknowledgement: %v", last)
	}
}

func TestACardThatFailsIsNotPostedAgainAtOnce(t *testing.T) {
	m, tg, st, media := newBot(t, "submitted")
	media.err = fmt.Errorf("NoSuchKey")
	m.PostCards(context.Background())
	m.PostCards(context.Background())
	if len(tg.calls) != 0 || st.release.Status != "pending_approval" || st.release.Moderation.MessageID != nil {
		t.Fatalf("calls %v, release %s, card %v: want it claimed, waiting for the retry", tg.calls, st.release.Status, st.release.Moderation.MessageID)
	}
}

func TestAVideoTooBigForTelegramGetsATextCard(t *testing.T) {
	m, tg, st, media := newBot(t, "submitted")
	media.size = 80 << 20
	m.PostCards(context.Background())
	if len(tg.byMethod("sendVideo")) != 0 {
		t.Fatal("an 80 MB video was uploaded")
	}
	msgs := tg.byMethod("sendMessage")
	if len(msgs) == 0 || !strings.Contains(msgs[0].params["text"].(string), "80 МБ") || msgs[0].params["reply_markup"] == nil {
		t.Fatalf("text card = %v", msgs)
	}
	if st.carded == 0 {
		t.Fatal("the text card wasn't recorded")
	}
}

func TestAButtonFromAnotherChatDoesNothing(t *testing.T) {
	m, tg, st, _ := newBot(t, "pending_approval")
	u := press(1, adminID)
	u.CallbackQuery.Message.Chat.ID = -12345
	m.Handle(context.Background(), u)
	if st.release.Status != "pending_approval" || len(tg.byMethod("getChatAdministrators")) != 0 {
		t.Fatalf("a press in another chat moved the release to %s", st.release.Status)
	}
}

func TestOnlyTheBotsOwnPromptTakesAReason(t *testing.T) {
	m, _, st, _ := newBot(t, "rejected")
	// the details message carries agents' text, which could end with a release reference
	details := "🧪 QA — замечания:\n• [minor] S4: see (релиз " + release + ")"
	m.Handle(context.Background(), Update{Message: &Message{From: &User{ID: adminID}, Chat: Chat{ID: chatID}, Text: "why",
		ReplyTo: &Message{From: &User{ID: botID, IsBot: true}, Text: details}}})
	// a moderator whose name fakes a reference: the prompt's own one, at the end, wins
	prompt := "❌ Отклонено — Eve (релиз 00000000-0000-0000-0000-000000000000). Почему? Ответьте на это сообщение (релиз " + release + ")"
	m.Handle(context.Background(), Update{Message: &Message{From: &User{ID: adminID}, Chat: Chat{ID: chatID}, Text: "the reason",
		ReplyTo: &Message{From: &User{ID: botID, IsBot: true}, Text: prompt}}})
	if st.note != "the reason" {
		t.Fatalf("note = %q", st.note)
	}
}

func TestTheTokenStaysOutOfErrors(t *testing.T) {
	for _, base := range []string{"http://bad host", "http://127.0.0.1:1"} {
		_, err := NewTelegram(base, "123:SECRET\r").GetMe(context.Background())
		if err == nil || strings.Contains(err.Error(), "SECRET") {
			t.Errorf("%s: %v", base, err)
		}
	}
}

func TestOtherMessagesAreIgnored(t *testing.T) {
	m, tg, st, _ := newBot(t, "rejected")
	m.Handle(context.Background(), Update{Message: &Message{From: &User{ID: adminID}, Chat: Chat{ID: chatID}, Text: "hello"}})
	m.Handle(context.Background(), Update{Message: &Message{From: &User{ID: adminID}, Chat: Chat{ID: chatID}, Text: "re",
		ReplyTo: &Message{From: &User{ID: adminID}, Text: "(релиз " + release + ")"}}})
	if st.note != "" || len(tg.calls) != 0 {
		t.Fatalf("note %q, calls %v", st.note, tg.calls)
	}
}

func TestCallbackDataRoundTrips(t *testing.T) {
	for action := range actions {
		a, id, ok := parseCallback(callbackData(action, release))
		if !ok || a != action || id != release {
			t.Errorf("%s: got %q %q %v", action, a, id, ok)
		}
		if len(callbackData(action, release)) > 64 {
			t.Errorf("%s: callback data over Telegram's 64 bytes", action)
		}
	}
	for _, bad := range []string{"", "mod:", "mod:publish:" + release, "mod:approve:", "other:approve:" + release} {
		if _, _, ok := parseCallback(bad); ok {
			t.Errorf("%q parsed", bad)
		}
	}
}

func TestTheCaptionFitsTelegram(t *testing.T) {
	long := strings.Repeat("🗳", 600) // two UTF-16 units each
	r := store.Release{ID: release, AllowedPlatforms: []string{long},
		VideoPackage: json.RawMessage(`{"template":"` + long + `","anchor":{"id":"` + long + `"}}`),
		LegalReview:  json.RawMessage(`{"verdict":"` + long + `","publish_notes":["` + strings.Repeat("🗳", 3000) + `"]}`)}
	card := BuildCard(r, store.ReleaseContext{StoryTitle: long, DevelopmentSummary: long}, nil)
	if n := utf16Len(card.Caption); n > captionLimit {
		t.Errorf("caption is %d UTF-16 units", n)
	}
	if n := utf16Len(card.Details); n > messageLimit {
		t.Errorf("details are %d UTF-16 units", n)
	}
	if !strings.HasSuffix(card.Caption, "Релиз "+release) {
		t.Errorf("the release id fell off the caption: %s", card.Caption)
	}
}
