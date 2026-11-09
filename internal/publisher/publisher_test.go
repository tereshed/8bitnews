package publisher

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tereshed/8bitnews/internal/service"
	"github.com/tereshed/8bitnews/internal/store"
	"github.com/tereshed/8bitnews/internal/youtube"
)

const (
	rel     = "11111111-1111-1111-1111-111111111111"
	channel = "8bitnews-us"
	refresh = "1//refresh"
)

var videoBytes = strings.Repeat("v", 1000)

// fakeYouTube is YouTube and Google's token endpoint, over HTTP.
type fakeYouTube struct {
	mu      sync.Mutex
	base    string
	uploads map[string]*upload // by session
	videos  map[string]*video
	started []map[string]map[string]any
	puts    int
	thumbs  map[string][]byte
	unsched []string
	nextID  int

	revoked     bool
	startErr    func(w http.ResponseWriter) bool
	putErr      func(w http.ResponseWriter, u *upload) bool
	thumbErr    func(w http.ResponseWriter) bool
	unschedErr  func(w http.ResponseWriter) bool
	afterUpload func(v *video)
}

type upload struct {
	received int
	size     int
	videoID  string
}

type video struct {
	UploadStatus, RejectionReason, Privacy, PublishAt string
	Synthetic                                         *bool
}

func yes() *bool { b := true; return &b }

func writeErr(w http.ResponseWriter, status int, reason string) {
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"message": reason + " said YouTube", "errors": []any{map[string]any{"reason": reason}}}})
}

func (f *fakeYouTube) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	path := r.URL.Path
	switch {
	case path == "/token":
		if f.revoked {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "acc", "expires_in": 3600})
	case path == "/upload/videos":
		if f.startErr != nil && f.startErr(w) {
			return
		}
		var meta map[string]map[string]any
		_ = json.NewDecoder(r.Body).Decode(&meta)
		f.started = append(f.started, meta)
		size, _ := strconv.Atoi(r.Header.Get("X-Upload-Content-Length"))
		session := fmt.Sprintf("/session/%d", len(f.started))
		f.uploads[session] = &upload{size: size}
		w.Header().Set("Location", f.base+session)
	case strings.HasPrefix(path, "/session/"):
		f.session(w, r, path)
	case path == "/upload/thumbnails/set":
		if f.thumbErr != nil && f.thumbErr(w) {
			return
		}
		body, _ := io.ReadAll(r.Body)
		f.thumbs[r.URL.Query().Get("videoId")] = body
	case path == "/api/videos" && r.Method == http.MethodGet:
		items := []any{}
		if v, ok := f.videos[r.URL.Query().Get("id")]; ok {
			items = append(items, map[string]any{"status": map[string]any{"uploadStatus": v.UploadStatus, "rejectionReason": v.RejectionReason,
				"privacyStatus": v.Privacy, "publishAt": v.PublishAt, "containsSyntheticMedia": v.Synthetic}})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"items": items})
	case path == "/api/videos" && r.Method == http.MethodPut:
		if f.unschedErr != nil && f.unschedErr(w) {
			return
		}
		var body struct {
			ID string `json:"id"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.unsched = append(f.unsched, body.ID)
		if v, ok := f.videos[body.ID]; ok {
			v.PublishAt = ""
		}
	default:
		http.NotFound(w, r)
	}
}

func (f *fakeYouTube) session(w http.ResponseWriter, r *http.Request, path string) {
	u, ok := f.uploads[path]
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	finished := func() {
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{"id": u.videoID})
	}
	if strings.HasPrefix(r.Header.Get("Content-Range"), "bytes */") {
		if u.videoID != "" {
			finished()
			return
		}
		if u.received > 0 {
			w.Header().Set("Range", fmt.Sprintf("bytes=0-%d", u.received-1))
		}
		w.WriteHeader(http.StatusPermanentRedirect)
		return
	}
	f.puts++
	if f.putErr != nil && f.putErr(w, u) {
		return
	}
	body, _ := io.ReadAll(r.Body)
	u.received += len(body)
	if u.received < u.size {
		w.Header().Set("Range", fmt.Sprintf("bytes=0-%d", u.received-1))
		w.WriteHeader(http.StatusPermanentRedirect)
		return
	}
	f.nextID++
	u.videoID = fmt.Sprintf("video%06d", f.nextID)
	meta := f.started[len(f.started)-1]
	v := &video{UploadStatus: "uploaded", Privacy: "private", PublishAt: fmt.Sprint(meta["status"]["publishAt"]), Synthetic: yes()}
	if f.afterUpload != nil {
		f.afterUpload(v)
	}
	f.videos[u.videoID] = v
	finished()
}

// fakeStore keeps it all in memory.
type fakeStore struct {
	mu       sync.Mutex
	accounts []store.PlatformAccount
	alerted  []string
	release  store.Release
	hold     string
	job      store.PublishJob
	link     string
	linkBy   string
	closed   []store.ToUnschedule
	forgot   []string
	channel  store.Channel
}

func (s *fakeStore) ConnectedAccounts(context.Context, string) ([]store.PlatformAccount, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []store.PlatformAccount
	for _, a := range s.accounts {
		if a.AuthAlertedAt == nil {
			out = append(out, a)
		}
	}
	return out, nil
}

func (s *fakeStore) MarkAccountAlerted(_ context.Context, ch, _ string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.alerted = append(s.alerted, ch)
	now := time.Now()
	for i := range s.accounts {
		if s.accounts[i].ChannelID == ch {
			s.accounts[i].AuthAlertedAt = &now
		}
	}
	return nil
}

func (s *fakeStore) GetRelease(context.Context, string) (store.Release, error) { return s.release, nil }
func (s *fakeStore) PublishHold(context.Context, string) (string, error)       { return s.hold, nil }
func (s *fakeStore) SetPublishHold(_ context.Context, _, reason string) error {
	s.hold = reason
	return nil
}
func (s *fakeStore) GetChannel(context.Context, string) (store.Channel, error) { return s.channel, nil }
func (s *fakeStore) PublishJobOf(context.Context, string) (store.PublishJob, error) {
	return s.job, nil
}
func (s *fakeStore) SavePublishJob(_ context.Context, j store.PublishJob) error {
	s.job = j
	return nil
}
func (s *fakeStore) DeletePublishJob(context.Context, string) error {
	s.job = store.PublishJob{ReleaseID: rel}
	return nil
}
func (s *fakeStore) SetPublicationURL(_ context.Context, _, _, url, by string) error {
	s.link, s.linkBy = url, by
	return nil
}
func (s *fakeStore) ReleasesToUnschedule(context.Context) ([]store.ToUnschedule, error) {
	return s.closed, nil
}
func (s *fakeStore) ForgetPublication(_ context.Context, id, _ string) error {
	s.forgot = append(s.forgot, id)
	s.closed = nil
	s.link = ""
	s.job = store.PublishJob{ReleaseID: rel}
	return nil
}

// fakeQueue is the service's side: one item, and a slot.
type fakeQueue struct {
	items   []service.PublishQueueItem
	slot    time.Time
	refused *service.Error
	booked  int
}

func (q *fakeQueue) UploadItems(context.Context) ([]service.PublishQueueItem, error) {
	return q.items, nil
}
func (q *fakeQueue) ReserveReleaseSlot(context.Context, string) (service.ReleaseSlotResponse, error) {
	q.booked++
	if q.refused != nil {
		return service.ReleaseSlotResponse{}, q.refused
	}
	return service.ReleaseSlotResponse{PublishAt: q.slot}, nil
}

type fakeMedia struct {
	opened []string
	cover  string
}

func (m *fakeMedia) Open(_ context.Context, key string) (io.ReadCloser, int64, error) {
	m.opened = append(m.opened, key)
	if strings.HasSuffix(key, "cover.png") {
		return io.NopCloser(strings.NewReader(m.cover)), int64(len(m.cover)), nil
	}
	return io.NopCloser(strings.NewReader(videoBytes)), int64(len(videoBytes)), nil
}

type fakeNotifier struct {
	said  []string
	cards []*int64
}

func (n *fakeNotifier) Notify(_ context.Context, card *int64, text string) error {
	n.said = append(n.said, text)
	n.cards = append(n.cards, card)
	return nil
}

type plainSealer struct{}

func (plainSealer) Open(sealed, _ string) (string, error) {
	if sealed == "unreadable" {
		return "", errors.New("wrong key")
	}
	return sealed, nil
}

type fixture struct {
	p     *Publisher
	yt    *fakeYouTube
	st    *fakeStore
	q     *fakeQueue
	media *fakeMedia
	say   *fakeNotifier
	now   time.Time
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	yt := &fakeYouTube{uploads: map[string]*upload{}, videos: map[string]*video{}, thumbs: map[string][]byte{}}
	srv := httptest.NewServer(yt)
	t.Cleanup(srv.Close)
	yt.base = srv.URL
	client := youtube.New(youtube.Config{ClientID: "id", ClientSecret: "sec", TokenURL: srv.URL + "/token", APIBase: srv.URL + "/api", UploadBase: srv.URL + "/upload"})
	card := int64(77)
	now := time.Now().Truncate(time.Second)
	f := &fixture{yt: yt, media: &fakeMedia{cover: "PNGDATA"}, say: &fakeNotifier{}, now: now,
		st: &fakeStore{
			accounts: []store.PlatformAccount{{ChannelID: channel, Tokens: store.AccountTokens{RefreshSealed: refresh}}},
			release:  store.Release{ID: rel, ChannelID: channel, Status: "approved", Moderation: store.Moderation{MessageID: &card}},
			job:      store.PublishJob{ReleaseID: rel},
			channel:  store.Channel{ID: channel, Language: "en-US", Timezone: "America/New_York"},
		},
		q: &fakeQueue{slot: now.Add(6 * time.Hour).UTC(), items: []service.PublishQueueItem{{
			ReleaseID: rel, ChannelID: channel, Title: "A title", Description: "A description", Tags: []string{"a", "b"},
			Video: "releases/x/video_no_music.mp4", Cover: "releases/x/cover.png",
		}}},
	}
	f.p = New(f.st, f.q, client, f.media, f.say, plainSealer{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	f.p.now = func() time.Time { return f.now }
	return f
}

func (f *fixture) pass() { f.p.Pass(context.Background()) }

// due moves past any backoff.
func (f *fixture) due() { f.now = f.st.job.NextAttemptAt.Add(time.Second) }

func TestAFullUpload(t *testing.T) {
	f := newFixture(t)
	f.pass()

	if len(f.yt.started) != 1 || f.yt.puts != 1 {
		t.Fatalf("started %d, puts %d", len(f.yt.started), f.yt.puts)
	}
	meta := f.yt.started[0]
	if meta["snippet"]["title"] != "A title" || meta["snippet"]["description"] != "A description" || meta["snippet"]["defaultLanguage"] != "en-US" {
		t.Errorf("metadata: %+v", meta["snippet"])
	}
	if meta["status"]["publishAt"] != f.q.slot.Format(time.RFC3339) || meta["status"]["privacyStatus"] != "private" {
		t.Errorf("status: %+v", meta["status"])
	}
	vid := f.st.job.VideoID
	if vid == "" || f.st.link != "https://www.youtube.com/shorts/"+vid || f.st.linkBy != setByHub {
		t.Errorf("job %+v, link %q by %q", f.st.job, f.st.link, f.st.linkBy)
	}
	if string(f.yt.thumbs[vid]) != "PNGDATA" || !f.st.job.ThumbnailSet {
		t.Errorf("poster: %q, job %+v", f.yt.thumbs[vid], f.st.job)
	}
	if len(f.say.said) != 1 || !strings.Contains(f.say.said[0], vid) || !strings.Contains(f.say.said[0], "America/New_York") ||
		!strings.Contains(f.say.said[0], "UTC") || f.say.cards[0] == nil || *f.say.cards[0] != 77 {
		t.Errorf("told: %q, %v", f.say.said, f.say.cards)
	}
	if f.st.hold != "" {
		t.Errorf("held: %q", f.st.hold)
	}
}

func TestACrashAfterTheUploadNeverUploadsTwice(t *testing.T) {
	f := newFixture(t)
	f.yt.mu.Lock()
	f.yt.nextID = 41
	f.yt.uploads["/session/1"] = &upload{size: len(videoBytes), received: len(videoBytes), videoID: "video000042"}
	f.yt.videos["video000042"] = &video{UploadStatus: "uploaded", Privacy: "private", PublishAt: f.q.slot.Format(time.RFC3339), Synthetic: yes()}
	f.yt.mu.Unlock()
	at := f.q.slot
	// Stored the session, sent every byte, and hub stopped before reading the answer.
	f.st.job = store.PublishJob{ReleaseID: rel, SessionURI: f.yt.base + "/session/1", VideoSize: int64(len(videoBytes)), PublishAt: &at}

	f.pass()

	if f.yt.puts != 0 || len(f.yt.started) != 0 || f.q.booked != 0 {
		t.Errorf("uploaded again: puts %d, started %d, booked %d", f.yt.puts, len(f.yt.started), f.q.booked)
	}
	if f.st.job.VideoID != "video000042" || f.st.link != "https://www.youtube.com/shorts/video000042" {
		t.Errorf("job %+v, link %q", f.st.job, f.st.link)
	}
}

func TestAnInterruptedUploadResumesFromWhereItStopped(t *testing.T) {
	f := newFixture(t)
	f.yt.uploads["/session/1"] = &upload{size: len(videoBytes), received: 400}
	f.yt.started = []map[string]map[string]any{{"status": {"publishAt": f.q.slot.Format(time.RFC3339)}}}
	at := f.q.slot
	f.st.job = store.PublishJob{ReleaseID: rel, SessionURI: f.yt.base + "/session/1", VideoSize: int64(len(videoBytes)), PublishAt: &at}

	f.pass()

	u := f.yt.uploads["/session/1"]
	if u.received != len(videoBytes) || u.videoID == "" || f.yt.puts != 1 || len(f.yt.started) != 1 {
		t.Errorf("upload %+v, puts %d, started %d", u, f.yt.puts, len(f.yt.started))
	}
	if f.st.link == "" {
		t.Error("no link recorded")
	}
}

func TestASessionYouTubeForgotStartsAgain(t *testing.T) {
	f := newFixture(t)
	at := f.q.slot
	f.st.job = store.PublishJob{ReleaseID: rel, SessionURI: f.yt.base + "/session/forgotten", PublishAt: &at}
	f.pass()
	if len(f.yt.started) != 1 || f.st.link == "" || f.q.booked != 1 {
		t.Errorf("started %d, link %q, booked %d", len(f.yt.started), f.st.link, f.q.booked)
	}
}

func TestAnUnfinishedSessionWhoseTimeIsTooCloseGetsANewSlot(t *testing.T) {
	f := newFixture(t)
	f.yt.uploads["/session/1"] = &upload{size: len(videoBytes), received: 400}
	f.yt.started = []map[string]map[string]any{{"status": {}}}
	soon := f.now.Add(time.Minute)
	f.st.job = store.PublishJob{ReleaseID: rel, SessionURI: f.yt.base + "/session/1", VideoSize: int64(len(videoBytes)), PublishAt: &soon}
	f.pass()
	if f.q.booked != 1 || len(f.yt.started) != 2 || f.st.job.PublishAt == nil || !f.st.job.PublishAt.Equal(f.q.slot) {
		t.Errorf("booked %d, started %d, job %+v", f.q.booked, len(f.yt.started), f.st.job)
	}
}

func TestPackageWarningsHoldTheRelease(t *testing.T) {
	f := newFixture(t)
	f.q.items[0].Warnings = []string{"нет контакта для исправлений"}
	f.pass()
	if !strings.Contains(f.st.hold, "нет контакта") || len(f.yt.started) != 0 {
		t.Errorf("hold %q, started %d", f.st.hold, len(f.yt.started))
	}
}

func TestWarningsAboutOtherPlatformsDontStopYouTube(t *testing.T) {
	f := newFixture(t)
	f.q.items[0].Warnings = []string{"tiktok: подпись длинная", "instagram: 40 хэштегов"}
	f.pass()
	if f.st.hold != "" || f.st.link == "" {
		t.Errorf("hold %q, link %q", f.st.hold, f.st.link)
	}
}

func TestAReleaseClosedOrHeldJustBeforeStartingDoesntGoUp(t *testing.T) {
	for name, set := range map[string]func(*fixture){
		"closed": func(f *fixture) { f.st.release.Status = "rejected" },
		"held":   func(f *fixture) { f.st.hold = "the vote is out" },
	} {
		f := newFixture(t)
		set(f)
		f.pass()
		if len(f.yt.started) != 0 || f.q.booked != 0 || f.st.link != "" {
			t.Errorf("%s: started %d, booked %d, link %q", name, len(f.yt.started), f.q.booked, f.st.link)
		}
	}
}

func TestARefusedSlotUploadsNothing(t *testing.T) {
	f := newFixture(t)
	f.q.refused = &service.Error{Kind: service.KindConflict, Message: "too old"}
	f.pass()
	if len(f.yt.started) != 0 || f.st.link != "" || f.st.job.Attempts != 0 {
		t.Errorf("started %d, link %q, job %+v", len(f.yt.started), f.st.link, f.st.job)
	}
}

func TestAVideoYouTubeRejectedIsHeldAndStartsAfresh(t *testing.T) {
	f := newFixture(t)
	f.yt.afterUpload = func(v *video) { v.UploadStatus, v.RejectionReason = "rejected", "duplicate" }
	f.pass()
	if !strings.Contains(f.st.hold, "duplicate") || f.st.link != "" || f.st.job.VideoID != "" {
		t.Errorf("hold %q, link %q, job %+v", f.st.hold, f.st.link, f.st.job)
	}
}

func TestAVideoNotAsAskedIsRecordedAndHeld(t *testing.T) {
	for name, change := range map[string]func(*video){
		"public":   func(v *video) { v.Privacy = "public" },
		"no time":  func(v *video) { v.PublishAt = "" },
		"no label": func(v *video) { no := false; v.Synthetic = &no },
		"bad time": func(v *video) { v.PublishAt = "2030-01-01T00:00:00Z" },
	} {
		f := newFixture(t)
		f.yt.afterUpload = change
		f.pass()
		if f.st.hold == "" || !strings.HasPrefix(f.st.link, "https://www.youtube.com/shorts/") {
			t.Errorf("%s: hold %q, link %q", name, f.st.hold, f.st.link)
		}
		if len(f.yt.thumbs) != 0 {
			t.Errorf("%s: went on to the poster", name)
		}
	}
}

func TestALabelYouTubeDoesntReportDoesntHold(t *testing.T) {
	f := newFixture(t)
	f.yt.afterUpload = func(v *video) { v.Synthetic = nil }
	f.pass()
	if f.st.hold != "" || f.st.link == "" {
		t.Errorf("hold %q, link %q", f.st.hold, f.st.link)
	}
}

func TestTransientFailuresBackOff(t *testing.T) {
	f := newFixture(t)
	f.yt.startErr = func(w http.ResponseWriter) bool {
		writeErr(w, http.StatusServiceUnavailable, "backendError")
		return true
	}
	wants := []time.Duration{time.Minute, 5 * time.Minute, 15 * time.Minute, time.Hour, time.Hour}
	for i, want := range wants {
		f.pass()
		if f.st.job.Attempts != i+1 || !f.st.job.NextAttemptAt.Equal(f.now.Add(want)) || f.st.hold != "" {
			t.Fatalf("attempt %d: job %+v, hold %q, want a wait of %s", i, f.st.job, f.st.hold, want)
		}
		// Not before its time.
		before := f.st.job
		f.pass()
		if f.st.job.Attempts != before.Attempts {
			t.Fatalf("attempt %d: tried during the backoff", i)
		}
		f.due()
	}
	f.yt.startErr = nil
	f.pass()
	if f.st.link == "" || f.st.job.Attempts != 0 || f.st.job.LastError != "" {
		t.Errorf("after recovering: link %q, job %+v", f.st.link, f.st.job)
	}
}

func TestQuotaWaitsInsteadOfHolding(t *testing.T) {
	f := newFixture(t)
	f.yt.startErr = func(w http.ResponseWriter) bool { writeErr(w, http.StatusForbidden, "quotaExceeded"); return true }
	f.pass()
	if f.st.hold != "" || f.st.job.Attempts != 1 {
		t.Errorf("hold %q, job %+v", f.st.hold, f.st.job)
	}
}

func TestAPermanentRefusalHolds(t *testing.T) {
	f := newFixture(t)
	f.yt.startErr = func(w http.ResponseWriter) bool { writeErr(w, http.StatusBadRequest, "invalidTitle"); return true }
	f.pass()
	if !strings.Contains(f.st.hold, "invalidTitle") || f.st.job.Attempts != 0 {
		t.Errorf("hold %q, job %+v", f.st.hold, f.st.job)
	}
}

func TestRevokedAccessAlertsOnceAndLeavesTheChannel(t *testing.T) {
	f := newFixture(t)
	f.yt.revoked = true
	f.pass()
	if len(f.st.alerted) != 1 || len(f.say.said) != 1 || !strings.Contains(f.say.said[0], "/admin/youtube") {
		t.Fatalf("alerted %v, told %q", f.st.alerted, f.say.said)
	}
	if f.st.hold != "" || f.st.job.Attempts != 0 {
		t.Errorf("a revoked access isn't the release's fault: hold %q, job %+v", f.st.hold, f.st.job)
	}
	f.pass()
	if len(f.st.alerted) != 1 || len(f.say.said) != 1 {
		t.Errorf("told again: %v, %q", f.st.alerted, f.say.said)
	}
}

func TestAnUnreadableTokenAlertsAsALostAccess(t *testing.T) {
	f := newFixture(t)
	f.st.accounts[0].Tokens.RefreshSealed = "unreadable"
	f.pass()
	if len(f.st.alerted) != 1 || len(f.yt.started) != 0 {
		t.Errorf("alerted %v, started %d", f.st.alerted, len(f.yt.started))
	}
}

func TestAChannelNotConnectedIsLeftAlone(t *testing.T) {
	f := newFixture(t)
	f.q.items[0].ChannelID = "8bitnews-es"
	f.st.closed = []store.ToUnschedule{{ReleaseID: rel, ChannelID: "8bitnews-es", URL: "https://www.youtube.com/shorts/abcdefghijk"}}
	f.pass()
	if len(f.yt.started) != 0 || len(f.yt.unsched) != 0 || len(f.st.forgot) != 0 {
		t.Errorf("started %d, unscheduled %v, forgot %v", len(f.yt.started), f.yt.unsched, f.st.forgot)
	}
}

func TestAPosterThatFailsStaysRetriedThenTheVideoGoesOnWithoutIt(t *testing.T) {
	f := newFixture(t)
	f.yt.thumbErr = func(w http.ResponseWriter) bool {
		writeErr(w, http.StatusServiceUnavailable, "backendError")
		return true
	}
	f.pass()
	if f.st.link != "" || f.st.job.ThumbnailAttempts != 1 || f.st.job.Attempts != 1 {
		t.Fatalf("first failure: link %q, job %+v", f.st.link, f.st.job)
	}
	f.due()
	f.pass()
	if f.st.link != "" || f.st.job.ThumbnailAttempts != 2 {
		t.Fatalf("second failure: link %q, job %+v", f.st.link, f.st.job)
	}
	f.due()
	f.pass()
	if f.st.link == "" {
		t.Fatalf("third failure: no link, job %+v", f.st.job)
	}
	if len(f.say.said) != 1 || !strings.Contains(f.say.said[0], "Обложку") || !strings.Contains(f.say.said[0], "Загружен") {
		t.Errorf("told: %q", f.say.said)
	}
	if f.yt.puts != 1 || len(f.yt.started) != 1 {
		t.Errorf("uploaded again: puts %d, started %d", f.yt.puts, len(f.yt.started))
	}
}

func TestAPosterYouTubeRefusesGoesOnAtOnce(t *testing.T) {
	f := newFixture(t)
	f.yt.thumbErr = func(w http.ResponseWriter) bool { writeErr(w, http.StatusForbidden, "forbidden"); return true }
	f.pass()
	if f.st.link == "" || f.st.hold != "" || len(f.say.said) != 1 || !strings.Contains(f.say.said[0], "Обложку") {
		t.Errorf("link %q, hold %q, told %q", f.st.link, f.st.hold, f.say.said)
	}
}

func TestAPackageWithNoCoverSaysSo(t *testing.T) {
	f := newFixture(t)
	f.q.items[0].Cover = ""
	f.pass()
	if f.st.link == "" || len(f.say.said) != 1 || !strings.Contains(f.say.said[0], "нет обложки") {
		t.Errorf("link %q, told %q", f.st.link, f.say.said)
	}
}

func TestTakingDownAVideoWithALink(t *testing.T) {
	f := newFixture(t)
	f.q.items = nil
	f.yt.videos["abcdefghijk"] = &video{Privacy: "private", PublishAt: "2030-01-01T00:00:00Z"}
	f.st.closed = []store.ToUnschedule{{ReleaseID: rel, ChannelID: channel, Status: "rejected", URL: "https://www.youtube.com/shorts/abcdefghijk", PublishAt: f.now.Add(time.Hour)}}
	f.pass()
	if len(f.yt.unsched) != 1 || f.yt.unsched[0] != "abcdefghijk" || len(f.st.forgot) != 1 {
		t.Fatalf("unscheduled %v, forgot %v", f.yt.unsched, f.st.forgot)
	}
	if f.yt.videos["abcdefghijk"].PublishAt != "" {
		t.Error("the video still has its time")
	}
	if len(f.say.said) != 1 || !strings.Contains(f.say.said[0], "снят с расписания") {
		t.Errorf("told %q", f.say.said)
	}
}

func TestTakingDownWithNoLinkAsksTheJob(t *testing.T) {
	closed := func(f *fixture) []store.ToUnschedule {
		return []store.ToUnschedule{{ReleaseID: rel, ChannelID: channel, Status: "rejected", PublishAt: f.now.Add(time.Hour)}}
	}
	// The job knows the video.
	f := newFixture(t)
	f.q.items = nil
	f.st.closed = closed(f)
	f.st.job = store.PublishJob{ReleaseID: rel, VideoID: "knownvideo1"}
	f.pass()
	if len(f.yt.unsched) != 1 || f.yt.unsched[0] != "knownvideo1" || len(f.st.forgot) != 1 {
		t.Errorf("known: unscheduled %v, forgot %v", f.yt.unsched, f.st.forgot)
	}
	// The session finished, and the answer never reached hub.
	f = newFixture(t)
	f.q.items = nil
	f.st.closed = closed(f)
	f.yt.uploads["/session/1"] = &upload{size: 1000, received: 1000, videoID: "finished001"}
	f.st.job = store.PublishJob{ReleaseID: rel, SessionURI: f.yt.base + "/session/1", VideoSize: 1000}
	f.pass()
	if len(f.yt.unsched) != 1 || f.yt.unsched[0] != "finished001" {
		t.Errorf("finished: unscheduled %v", f.yt.unsched)
	}
	// The session never finished: no video exists, nothing to say.
	f = newFixture(t)
	f.q.items = nil
	f.st.closed = closed(f)
	f.yt.uploads["/session/1"] = &upload{size: 1000, received: 10}
	f.st.job = store.PublishJob{ReleaseID: rel, SessionURI: f.yt.base + "/session/1", VideoSize: 1000}
	f.pass()
	if len(f.yt.unsched) != 0 || len(f.st.forgot) != 1 || len(f.say.said) != 0 {
		t.Errorf("unfinished: unscheduled %v, forgot %v, told %q", f.yt.unsched, f.st.forgot, f.say.said)
	}
	// No job at all: a slot booked by hand, nothing to take down.
	f = newFixture(t)
	f.q.items = nil
	f.st.closed = closed(f)
	f.pass()
	if len(f.yt.unsched) != 0 || len(f.st.forgot) != 1 {
		t.Errorf("no job: unscheduled %v, forgot %v", f.yt.unsched, f.st.forgot)
	}
}

func TestATakeDownThatFailsKeepsTheVideoOnTheList(t *testing.T) {
	f := newFixture(t)
	f.q.items = nil
	f.st.closed = []store.ToUnschedule{{ReleaseID: rel, ChannelID: channel, Status: "rejected", URL: "https://www.youtube.com/shorts/abcdefghijk", PublishAt: f.now.Add(time.Hour)}}
	f.yt.unschedErr = func(w http.ResponseWriter) bool {
		writeErr(w, http.StatusServiceUnavailable, "backendError")
		return true
	}
	f.pass()
	if len(f.st.forgot) != 0 || len(f.say.said) != 0 {
		t.Errorf("transient: forgot %v, told %q", f.st.forgot, f.say.said)
	}
	f.yt.unschedErr = func(w http.ResponseWriter) bool { writeErr(w, http.StatusForbidden, "forbidden"); return true }
	f.pass()
	f.pass()
	if len(f.st.forgot) != 0 || len(f.say.said) != 1 || !strings.Contains(f.say.said[0], "не дал снять") {
		t.Errorf("refused: forgot %v, told %q", f.st.forgot, f.say.said)
	}
	f.yt.unschedErr = nil
	f.pass()
	if len(f.st.forgot) != 1 {
		t.Errorf("recovering: forgot %v", f.st.forgot)
	}
}

func TestRevokedAccessOnATakeDownAlerts(t *testing.T) {
	f := newFixture(t)
	f.yt.revoked = true
	f.st.closed = []store.ToUnschedule{{ReleaseID: rel, ChannelID: channel, Status: "rejected", URL: "https://www.youtube.com/shorts/abcdefghijk", PublishAt: f.now.Add(time.Hour)}}
	f.pass()
	if len(f.st.alerted) != 1 || len(f.st.forgot) != 0 || len(f.yt.started) != 0 {
		t.Errorf("alerted %v, forgot %v, started %d", f.st.alerted, f.st.forgot, len(f.yt.started))
	}
}

func TestAJobPickedUpAfterItsTimeIsRecordedNotHeld(t *testing.T) {
	f := newFixture(t)
	past := f.now.Add(-time.Hour)
	f.yt.videos["oldvideo001"] = &video{UploadStatus: "processed", Privacy: "public", PublishAt: past.Format(time.RFC3339)}
	f.st.job = store.PublishJob{ReleaseID: rel, VideoID: "oldvideo001", PublishAt: &past}
	f.pass()
	if f.st.hold != "" || f.st.link != "https://www.youtube.com/shorts/oldvideo001" {
		t.Errorf("hold %q, link %q", f.st.hold, f.st.link)
	}
}
