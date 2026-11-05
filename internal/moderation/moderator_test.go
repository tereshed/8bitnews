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
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tereshed/8bitnews/internal/publish"
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
	mu         sync.Mutex
	failAdmins bool
	calls      []call
	nextMsg    int64
}

type call struct {
	method string
	params map[string]any
	video  []byte
	id     int64 // the message id the fake answered with
}

func (f *fakeTelegram) handler(t *testing.T) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
		f.mu.Lock()
		failAdmins := f.failAdmins
		f.mu.Unlock()
		if method == "getChatAdministrators" && failAdmins {
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error_code": 403, "description": "bot was kicked"})
			return
		}
		c := call{method: method, params: map[string]any{}}
		if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/") {
			if err := r.ParseMultipartForm(1 << 20); err != nil {
				t.Errorf("%s: %v", method, err)
			}
			for k, v := range r.MultipartForm.Value {
				c.params[k] = v[0]
			}
			for _, field := range []string{"video", "document"} {
				if fh := r.MultipartForm.File[field]; len(fh) == 1 {
					f, _ := fh[0].Open()
					c.video, _ = io.ReadAll(f)
					c.params["filename"] = fh[0].Filename
				}
			}
		} else {
			_ = json.NewDecoder(r.Body).Decode(&c.params)
		}
		f.mu.Lock()
		f.nextMsg++
		id := f.nextMsg
		c.id = 1000 + id
		f.calls = append(f.calls, c)
		f.mu.Unlock()
		var result any = true
		switch method {
		case "getMe":
			result = User{ID: botID, IsBot: true, FirstName: "8BitNewsBot"}
		case "sendVideo", "sendMessage", "sendDocument":
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
	mu          sync.Mutex
	failChannel bool
	release     store.Release
	carded      int64
	note        string
	events      []map[string]any
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

func (s *fakeStore) GetRelease(_ context.Context, id string) (store.Release, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if id != s.release.ID {
		return store.Release{}, store.ErrNotFound
	}
	return s.release, nil
}

func (s *fakeStore) SetPublished(_ context.Context, id string, published bool) (store.Release, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if id != s.release.ID {
		return store.Release{}, false, store.ErrNotFound
	}
	from, to := "approved", "published"
	if !published {
		from, to = to, from
	}
	if s.release.Status != from {
		return s.release, false, nil
	}
	s.release.Status = to
	return s.release, true, nil
}

func (s *fakeStore) WithdrawRelease(_ context.Context, id, by string) (store.Release, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if id != s.release.ID {
		return store.Release{}, false, store.ErrNotFound
	}
	if s.release.Status != "approved" && s.release.Status != "published" {
		return s.release, false, nil
	}
	s.release.Status = "rejected"
	s.release.Moderation.By = by
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

func (s *fakeStore) GetChannel(context.Context, string) (store.Channel, error) {
	if s.failChannel {
		return store.Channel{}, fmt.Errorf("connection refused")
	}
	md, err := os.ReadFile("../../channels/8bitnews-us/metadata.md")
	return store.Channel{ID: "8bitnews-us", Name: "8BitNewsUS", CountryCC: "US", MetadataMD: string(md),
		Platforms: json.RawMessage(`{"youtube":{"enabled":true},"tiktok":{"enabled":true},"instagram":{"enabled":true}}`)}, err
}

func (s *fakeStore) GetCountry(context.Context, string) (store.Country, error) {
	return store.Country{}, store.ErrNotFound
}

func (s *fakeStore) LogEvent(_ context.Context, _ string, data any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, data.(map[string]any))
	return nil
}

type fakeMedia struct {
	opened string
	all    []string
	size   int64
	err    error
}

func (m *fakeMedia) Open(_ context.Context, key string) (io.ReadCloser, int64, error) {
	m.opened = key
	m.all = append(m.all, key)
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
	pub := publish.Config{PublicURL: "https://hub.example.org", CorrectionsContact: "corrections@example.org"}
	m := New(NewTelegram(srv.URL, "123:SECRET"), st, media, chatID, time.Minute, pub, slog.New(slog.NewTextHandler(io.Discard, nil)))
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
	edits := tg.byMethod("editMessageReplyMarkup")
	if len(edits) != 1 {
		t.Fatalf("button edits: %d", len(edits))
	}
	if kb, _ := json.Marshal(edits[0].params); strings.Contains(string(kb), "mod:approve:") ||
		!strings.Contains(string(kb), "mod:package:"+release) || !strings.Contains(string(kb), "mod:published:"+release) {
		t.Errorf("the card's buttons after approval: %s", kb)
	}
	if msgs := tg.byMethod("sendMessage"); len(msgs) == 0 || !strings.Contains(msgs[0].params["text"].(string), "Одобрено — Kostya (@kteresh).") {
		t.Errorf("confirmation = %v", msgs)
	}
	if len(st.events) != 2 || st.events[0]["action"] != "approved" || st.events[1]["action"] != "package" {
		t.Errorf("events = %v", st.events)
	}
	// The press is answered before the package goes out.
	var order []string
	for _, c := range tg.calls {
		if c.method == "answerCallbackQuery" || c.method == "sendMessage" {
			order = append(order, c.method)
		}
	}
	if len(order) < 2 || order[0] != "answerCallbackQuery" {
		t.Errorf("calls in order: %v", order)
	}
}

func TestThePackageAgainAndPublished(t *testing.T) {
	m, tg, st, _ := newBot(t, "pending_approval")
	ctx := context.Background()
	button := func(action string, from int64) Update {
		u := press(1, from)
		u.CallbackQuery.Data = "mod:" + action + ":" + release
		return u
	}
	m.Handle(ctx, button("package", adminID))
	if a := tg.byMethod("answerCallbackQuery"); len(a) != 1 || !strings.Contains(a[0].params["text"].(string), "только для одобренного") {
		t.Fatalf("package before approval: %v", a)
	}
	m.Handle(ctx, button("published", adminID))
	if st.release.Status != "pending_approval" {
		t.Fatalf("«Выложено» before approval moved the release to %s", st.release.Status)
	}

	m.Handle(ctx, press(1, adminID))
	before := len(tg.byMethod("sendMessage"))
	m.Handle(ctx, button("package", otherID))
	// A press queued while the package went out doesn't send it twice.
	m.Handle(ctx, button("package", adminID))
	if a := tg.byMethod("answerCallbackQuery"); len(tg.byMethod("sendMessage")) != before ||
		!strings.Contains(a[len(a)-1].params["text"].(string), "Пакет только что отправлен") {
		t.Fatalf("a package again right away: %v", a[len(a)-1].params)
	}
	m.packaged[release] = time.Now().Add(-2 * packageCooldown)
	m.Handle(ctx, button("package", adminID))
	msgs := tg.byMethod("sendMessage")
	if len(msgs) <= before || !strings.Contains(msgs[before].params["text"].(string), "📦 Пакет ещё раз — для Kostya") {
		t.Fatalf("the package again: %v", msgs[before:])
	}
	if !strings.Contains(msgs[before].params["text"].(string), "https://hub.example.org/s/7f01440f43 — открыта") {
		t.Errorf("the page is open from the approval on: %s", msgs[before].params["text"])
	}

	m.Handle(ctx, button("published", adminID))
	if st.release.Status != "published" {
		t.Fatalf("status = %s", st.release.Status)
	}
	msgs = tg.byMethod("sendMessage")
	if last := msgs[len(msgs)-1].params["text"].(string); !strings.Contains(last, "🟢 Выложено — Kostya (@kteresh). Ролик появится в списке канала") {
		t.Errorf("confirmation: %s", last)
	}
	edits := tg.byMethod("editMessageReplyMarkup")
	if kb, _ := json.Marshal(edits[len(edits)-1].params); !strings.Contains(string(kb), "mod:package:") ||
		strings.Contains(string(kb), "mod:published:") || !strings.Contains(string(kb), "mod:unpublished:") {
		t.Errorf("buttons after «Выложено»: %s", kb)
	}
	m.Handle(ctx, button("published", adminID))
	if a := tg.byMethod("answerCallbackQuery"); !strings.Contains(a[len(a)-1].params["text"].(string), "Уже записано: релиз опубликован") {
		t.Errorf("a second «Выложено»: %v", a[len(a)-1].params)
	}
	if last := st.events[len(st.events)-1]; last["action"] != "published" || last["by"] != "Kostya (@kteresh)" {
		t.Errorf("events = %v", st.events)
	}

	// A mistaken «Выложено» is taken back.
	m.Handle(ctx, button("unpublished", adminID))
	if st.release.Status != "approved" {
		t.Fatalf("after «Не выложено» the release is %s", st.release.Status)
	}
	msgs = tg.byMethod("sendMessage")
	if last := msgs[len(msgs)-1].params["text"].(string); !strings.Contains(last, "страница источников остаётся открытой") {
		t.Errorf("confirmation: %s", last)
	}
	edits = tg.byMethod("editMessageReplyMarkup")
	if kb, _ := json.Marshal(edits[len(edits)-1].params); !strings.Contains(string(kb), "mod:published:") {
		t.Errorf("buttons after «Не выложено»: %s", kb)
	}

	// The sources page is closed for good, whichever way the release stood.
	m.Handle(ctx, button("withdraw", adminID))
	if st.release.Status != "rejected" {
		t.Fatalf("after «Закрыть страницу» the release is %s", st.release.Status)
	}
	msgs = tg.byMethod("sendMessage")
	if last := msgs[len(msgs)-1].params["text"].(string); !strings.Contains(last, "🚫 Страница источников закрыта — Kostya (@kteresh)") {
		t.Errorf("confirmation: %s", last)
	}
	if last := st.events[len(st.events)-1]; last["action"] != "withdrawn" || last["by"] != "Kostya (@kteresh)" {
		t.Errorf("events = %v", st.events)
	}
	edits = tg.byMethod("editMessageReplyMarkup")
	if kb, _ := json.Marshal(edits[len(edits)-1].params); strings.Contains(string(kb), "mod:") {
		t.Errorf("buttons after «Закрыть страницу»: %s", kb)
	}
	m.Handle(ctx, button("withdraw", adminID))
	if a := tg.byMethod("answerCallbackQuery"); !strings.Contains(a[len(a)-1].params["text"].(string), "Уже решено: релиз отклонён") {
		t.Errorf("a second «Закрыть страницу»: %v", a[len(a)-1].params)
	}
}

func TestTelegramIsAskedAgainWhenItSaysSlowDown(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			_, _ = w.Write([]byte(`{"ok":false,"error_code":429,"description":"Too Many Requests: retry after 1","parameters":{"retry_after":1}}`))
			return
		}
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":5}}`))
	}))
	defer srv.Close()
	msg, err := NewTelegram(srv.URL, "123:x").SendMessage(context.Background(), chatID, "hi", 0, nil)
	if err != nil || msg.MessageID != 5 || calls != 2 {
		t.Fatalf("message %d, err %v, %d calls", msg.MessageID, err, calls)
	}
}

func TestAnApprovalPostsThePublicationPackage(t *testing.T) {
	m, tg, st, media := newBot(t, "pending_approval")
	st.release.ChannelID = "8bitnews-us"
	st.release.AllowedPlatforms = []string{"youtube", "tiktok", "instagram"}
	st.release.Media = json.RawMessage(`{"video":"releases/dev/abc-video.mp4","cover":"releases/dev/c-cover.png"}`)
	st.release.VideoPackage = json.RawMessage(`{"media":{"video_no_music":"releases/dev/n-video_no_music.mp4"}}`)
	meta := `{"title":"Grand Jury Won't Indict Hutchinson","summary":"A grand jury refused to indict <her>.","hashtags":["#8BitNews","#DOJ"]}`
	st.release.Script = json.RawMessage(`{"metadata":{"youtube":{"title":"Grand Jury Won't Indict Hutchinson","summary":"A grand jury refused to indict <her>.",` +
		`"hashtags":["#8BitNews","#DOJ"],"tags":["grand jury","DOJ"]},"tiktok":` + meta + `,"instagram":` + meta + `}}`)
	m.Handle(context.Background(), press(1, adminID))

	msgs := tg.byMethod("sendMessage")
	if len(msgs) != 4 {
		t.Fatalf("%d messages, want the header and one per platform: %v", len(msgs), msgs)
	}
	header := msgs[0].params["text"].(string)
	for _, want := range []string{"Одобрено — Kostya (@kteresh).", "YouTube Shorts, TikTok, Instagram Reels",
		"https://hub.example.org/s/7f01440f43"} {
		if !strings.Contains(header, want) {
			t.Errorf("header lacks %q:\n%s", want, header)
		}
	}
	for _, msg := range msgs {
		if msg.params["parse_mode"] != "HTML" {
			t.Errorf("not HTML: %v", msg.params)
		}
		if r, _ := msg.params["reply_parameters"].(map[string]any); r["message_id"] != float64(1001) {
			t.Errorf("not under the card: %v", msg.params["reply_parameters"])
		}
	}
	yt := msgs[1].params["text"].(string)
	for _, want := range []string{"<b>YouTube Shorts</b>", "версия без музыки", "<b>Заголовок</b>\n<pre>Grand Jury Won&#39;t Indict Hutchinson</pre>",
		"<b>Описание</b>\n<pre>A grand jury refused to indict ‹her›.", "Corrections: corrections@example.org",
		"<b>Теги</b>\n<pre>grand jury, DOJ</pre>", "<b>Настройки</b>\n• категория News &amp; Politics;"} {
		if !strings.Contains(yt, want) {
			t.Errorf("youtube message lacks %q:\n%s", want, yt)
		}
	}
	if tt := msgs[2].params["text"].(string); strings.Contains(tt, "Заголовок") || !strings.Contains(tt, "<b>Подпись</b>\n<pre>Grand Jury Won&#39;t Indict Hutchinson\nA grand jury refused to indict &lt;her&gt;.") ||
		!strings.Contains(tt, "то же, что в карточке выше") {
		t.Errorf("tiktok message:\n%s", tt)
	}

	docs := tg.byMethod("sendDocument")
	if len(docs) != 2 || docs[0].params["filename"] != "n-video_no_music.mp4" || docs[1].params["filename"] != "c-cover.png" {
		t.Fatalf("documents = %v", docs)
	}
	if !strings.Contains(docs[0].params["caption"].(string), "без музыки") ||
		!strings.Contains(docs[0].params["reply_parameters"].(string), fmt.Sprintf(`"message_id":%d`, msgs[1].id)) {
		t.Errorf("the YouTube file: %v", docs[0].params)
	}
	if strings.Join(media.all, ",") != "releases/dev/n-video_no_music.mp4,releases/dev/c-cover.png" {
		t.Errorf("opened %v", media.all)
	}
}

func TestAPackageThatCannotBeBuiltIsReported(t *testing.T) {
	m, tg, st, _ := newBot(t, "pending_approval")
	st.failChannel = true
	m.Handle(context.Background(), press(1, adminID))
	msgs := tg.byMethod("sendMessage")
	if st.release.Status != "approved" || len(msgs) != 1 || !strings.Contains(msgs[0].params["text"].(string), "Пакет для публикации не собрался") {
		t.Fatalf("release %s, messages %v", st.release.Status, msgs)
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
	for _, action := range []string{"approve", "changes", "reject", actionPackage, actionPublished, actionUnpublished, actionWithdraw} {
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

func TestALongDescriptionStillReachesTheModerator(t *testing.T) {
	p := publish.PlatformPackage{Name: "YouTube Shorts", Title: "T", Settings: []string{"категория News & Politics;"}}
	p.Description = strings.Repeat("a", 3000)
	texts, asFile := platformMessages(p, true)
	if len(texts) != 1 || asFile {
		t.Fatalf("3000 characters: %d messages, file %v", len(texts), asFile)
	}
	p.Description = strings.Repeat("a", 4050)
	texts, asFile = platformMessages(p, true)
	if len(texts) != 2 || asFile || !strings.Contains(texts[1], "<pre>"+p.Description+"</pre>") {
		t.Fatalf("4050 characters: %d messages, file %v", len(texts), asFile)
	}
	p.Description = strings.Repeat("a", 4990)
	texts, asFile = platformMessages(p, true)
	if len(texts) != 1 || !asFile || strings.Contains(texts[0], "aaaa") || !strings.Contains(texts[0], "файлом ниже") {
		t.Fatalf("4990 characters: %d messages, file %v", len(texts), asFile)
	}
	for _, text := range texts {
		if n := utf16Len(text); n > messageLimit {
			t.Errorf("a message of %d units", n)
		}
	}
}

func TestActFromAdminPage(t *testing.T) {
	const cardID = int64(555)
	for _, c := range []struct {
		name, status, action, note, wantStatus, wantReply string
		wantButtons                                       []string
	}{
		{"approve", "pending_approval", "approve", "", "approved", "✅ Одобрено — Kostya", []string{"🟢"}},
		{"changes", "pending_approval", "changes", "fix S3", "changes_requested", "Причина: fix S3", nil},
		{"reject", "pending_approval", "reject", "off topic", "rejected", "❌ Отклонено — Kostya", nil},
		{"published", "approved", "published", "", "published", "🟢 Выложено — Kostya", []string{"↩️"}},
		{"unpublished", "published", "unpublished", "", "approved", "↩️ Не выложено — Kostya", []string{"🟢"}},
		{"withdraw", "published", "withdraw", "claim", "rejected", "🚫 Страница источников закрыта", nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			m, tg, st, media := newBot(t, c.status)
			id := cardID
			st.release.Moderation.MessageID = &id
			r, changed, err := m.Act(context.Background(), release, c.action, "Kostya", adminID, c.note)
			if err != nil || !changed || r.Status != c.wantStatus {
				t.Fatalf("%+v %v %v", r.Status, changed, err)
			}
			msgs := tg.byMethod("sendMessage")
			if len(msgs) != 1 || !strings.Contains(msgs[0].params["text"].(string), c.wantReply) {
				t.Fatalf("group message: %+v", msgs)
			}
			if got := msgs[0].params["chat_id"]; fmt.Sprint(got) != fmt.Sprint(chatID) {
				t.Errorf("sent to %v", got)
			}
			if fm := msgs[0].params["reply_parameters"]; fm == nil {
				t.Error("doesn't reply to the card")
			}
			kb := tg.byMethod("editMessageReplyMarkup")
			if len(kb) != 1 {
				t.Fatalf("card's buttons: %+v", kb)
			}
			raw, _ := json.Marshal(kb[0].params["reply_markup"])
			for _, b := range c.wantButtons {
				if !strings.Contains(string(raw), b) {
					t.Errorf("no %q in %s", b, raw)
				}
			}
			if c.wantButtons == nil && strings.Contains(string(raw), "callback_data") {
				t.Errorf("a closed card keeps buttons: %s", raw)
			}
			// No uploads: the admin page shows the package.
			if len(tg.byMethod("sendDocument")) != 0 || len(tg.byMethod("sendVideo")) != 0 || len(media.opened) != 0 {
				t.Errorf("the package went out: docs %d, videos %d, opened %d", len(tg.byMethod("sendDocument")), len(tg.byMethod("sendVideo")), len(media.opened))
			}
			if c.note != "" && c.action != "withdraw" && st.note != c.note {
				t.Errorf("reason not recorded: %q", st.note)
			}
			last := st.events[len(st.events)-1]
			if last["via"] != "admin" || last["by"] != "Kostya" {
				t.Errorf("event: %v", last)
			}
		})
	}
}

func TestActOnAlreadyDecidedChangesNothing(t *testing.T) {
	m, tg, st, _ := newBot(t, "approved")
	r, changed, err := m.Act(context.Background(), release, "reject", "Kostya", adminID, "too late")
	if err != nil || changed || r.Status != "approved" {
		t.Fatalf("%v %v %v", r.Status, changed, err)
	}
	if len(tg.byMethod("sendMessage")) != 0 || len(tg.byMethod("editMessageReplyMarkup")) != 0 || st.note != "" || len(st.events) != 0 {
		t.Errorf("something happened: %+v note %q events %v", tg.calls, st.note, st.events)
	}
}

func TestActWithoutACardStillTellsTheGroup(t *testing.T) {
	m, tg, _, _ := newBot(t, "pending_approval")
	if _, changed, err := m.Act(context.Background(), release, "approve", "Kostya", adminID, ""); err != nil || !changed {
		t.Fatalf("%v %v", changed, err)
	}
	if len(tg.byMethod("editMessageReplyMarkup")) != 0 {
		t.Error("edited a card that doesn't exist")
	}
	if len(tg.byMethod("sendMessage")) != 1 {
		t.Error("the group wasn't told")
	}
}

func TestActUnknownAction(t *testing.T) {
	m, _, st, _ := newBot(t, "pending_approval")
	if _, _, err := m.Act(context.Background(), release, "package", "Kostya", adminID, ""); err == nil {
		t.Error("accepted an action the page doesn't have")
	}
	if st.release.Status != "pending_approval" {
		t.Errorf("status moved to %s", st.release.Status)
	}
}

func TestAdminCommand(t *testing.T) {
	const link = "https://hub.example.org/admin/login?t=TOKEN"
	private := func(from int64, text string) Update {
		return Update{Message: &Message{MessageID: 1, From: &User{ID: from, FirstName: "K"}, Chat: Chat{ID: from, Type: "private"}, Text: text}}
	}
	group := func(chat, from int64, text string) Update {
		return Update{Message: &Message{MessageID: 1, From: &User{ID: from, FirstName: "K"}, Chat: Chat{ID: chat, Type: "group"}, Text: text}}
	}
	for _, c := range []struct {
		name     string
		update   Update
		wantLink bool
		wantText string
		wantNone bool
	}{
		{"moderator, private", private(adminID, "/admin"), true, "", false},
		{"moderator, /start", private(adminID, "/start"), true, "", false},
		{"moderator, addressed", private(adminID, "/admin@EightBitNewsBot"), true, "", false},
		{"stranger, private", private(otherID, "/admin"), false, "", true},
		{"stranger, /start", private(otherID, "/start"), false, "", true},
		{"moderator, group", group(chatID, adminID, "/admin"), false, "в личные сообщения", false},
		{"stranger, group", group(chatID, otherID, "/admin"), false, "", true},
		{"moderator, another group", group(-777, adminID, "/admin"), false, "", true},
		{"/start in a group", group(chatID, adminID, "/start"), false, "", true},
		{"ordinary text, private", private(adminID, "hello"), false, "", true},
	} {
		t.Run(c.name, func(t *testing.T) {
			m, tg, _, _ := newBot(t, "pending_approval")
			m.SetAdminLogin(func(int64, string) string { return link })
			m.Handle(context.Background(), c.update)
			msgs := tg.byMethod("sendMessage")
			if c.wantNone {
				if len(msgs) != 0 {
					t.Fatalf("answered: %+v", msgs)
				}
				return
			}
			if len(msgs) != 1 {
				t.Fatalf("messages: %+v", msgs)
			}
			text := msgs[0].params["text"].(string)
			if got := strings.Contains(text, link); got != c.wantLink {
				t.Errorf("link in %q: %v", text, got)
			}
			if c.wantText != "" && !strings.Contains(text, c.wantText) {
				t.Errorf("%q has no %q", text, c.wantText)
			}
			if fmt.Sprint(msgs[0].params["chat_id"]) != fmt.Sprint(c.update.Message.Chat.ID) {
				t.Errorf("sent to %v", msgs[0].params["chat_id"])
			}
		})
	}
}

func TestAdminCommandOffWithoutLogin(t *testing.T) {
	m, tg, _, _ := newBot(t, "pending_approval")
	m.Handle(context.Background(), Update{Message: &Message{From: &User{ID: adminID}, Chat: Chat{ID: adminID, Type: "private"}, Text: "/admin"}})
	if len(tg.byMethod("sendMessage")) != 0 {
		t.Error("answered /admin with no admin page")
	}
}

func TestIsModeratorDistrustsAnOldList(t *testing.T) {
	m, tg, _, _ := newBot(t, "pending_approval")
	ctx := context.Background()
	if !m.IsModerator(ctx, adminID) {
		t.Fatal("an administrator isn't a moderator")
	}
	if m.IsModerator(ctx, otherID) {
		t.Fatal("a stranger is a moderator")
	}

	// Telegram stops answering: the list still counts for a while...
	tg.mu.Lock()
	tg.failAdmins = true
	tg.mu.Unlock()
	m.mu.Lock()
	m.adminsAt = time.Now().Add(-10 * time.Minute)
	m.mu.Unlock()
	if !m.IsModerator(ctx, adminID) {
		t.Error("a 10-minute-old list isn't trusted")
	}
	// ...but not for ever, for the page. The card's buttons keep working.
	m.mu.Lock()
	m.adminsAt = time.Now().Add(-adminsMaxAge - time.Minute)
	m.mu.Unlock()
	if m.IsModerator(ctx, adminID) {
		t.Error("the page trusts a list Telegram hasn't confirmed for too long")
	}
	if !m.isModerator(ctx, adminID) {
		t.Error("the bot's own check changed")
	}
}

func TestActFinishesAfterTheBrowserLeaves(t *testing.T) {
	m, tg, st, _ := newBot(t, "pending_approval")
	id := int64(555)
	st.release.Moderation.MessageID = &id
	ctx, cancel := context.WithCancel(context.Background())
	// The store's write has gone through when the browser goes away.
	m.store = cancelAfterDecide{fakeStore: st, cancel: cancel}
	if _, changed, err := m.Act(ctx, release, "reject", "Kostya", adminID, "off topic"); err != nil || !changed {
		t.Fatalf("%v %v", changed, err)
	}
	if st.note != "off topic" {
		t.Errorf("the reason was lost: %q", st.note)
	}
	if len(tg.byMethod("sendMessage")) != 1 || len(tg.byMethod("editMessageReplyMarkup")) != 1 {
		t.Errorf("the group or the card wasn't told: %+v", tg.calls)
	}
}

type cancelAfterDecide struct {
	*fakeStore
	cancel context.CancelFunc
}

func (c cancelAfterDecide) DecideRelease(ctx context.Context, id, status, by string) (store.Release, bool, error) {
	r, changed, err := c.fakeStore.DecideRelease(ctx, id, status, by)
	c.cancel()
	return r, changed, err
}

func TestNewCardFollowsADecisionMadeDuringItsUpload(t *testing.T) {
	for _, c := range []struct {
		status string
		want   string // on the keyboard; "" means no buttons
		edits  int
	}{
		{"pending_approval", "", 0},
		{"approved", "🟢", 1},
		{"published", "↩️", 1},
		{"rejected", "", 1},
	} {
		t.Run(c.status, func(t *testing.T) {
			m, tg, st, _ := newBot(t, c.status)
			m.syncNewCard(context.Background(), st.release.ID, 777)
			edits := tg.byMethod("editMessageReplyMarkup")
			if len(edits) != c.edits {
				t.Fatalf("edits: %+v", edits)
			}
			if c.edits == 0 {
				return
			}
			raw, _ := json.Marshal(edits[0].params["reply_markup"])
			if c.want == "" && strings.Contains(string(raw), "callback_data") {
				t.Errorf("buttons left: %s", raw)
			}
			if c.want != "" && !strings.Contains(string(raw), c.want) {
				t.Errorf("no %q in %s", c.want, raw)
			}
		})
	}
}

func TestWithdrawRecordsWho(t *testing.T) {
	m, _, st, _ := newBot(t, "published")
	if _, changed, err := m.Act(context.Background(), release, "withdraw", "Kostya", adminID, ""); err != nil || !changed {
		t.Fatalf("%v %v", changed, err)
	}
	if st.release.Moderation.By != "Kostya" {
		t.Errorf("credited to %q", st.release.Moderation.By)
	}
}
