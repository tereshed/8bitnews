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

	revoked       bool
	startErr      func(w http.ResponseWriter) bool
	putErr        func(w http.ResponseWriter, u *upload) bool
	queryErr      func(w http.ResponseWriter, u *upload) bool
	invalidClient bool
	getErr        map[string]bool // videos whose read answers 503
	thumbErr      func(w http.ResponseWriter) bool
	unschedErr    func(w http.ResponseWriter) bool
	afterUpload   func(v *video)
}

type upload struct {
	received int
	size     int
	videoID  string
}

type video struct {
	UploadStatus, RejectionReason, Privacy, PublishAt string
	Synthetic                                         *bool
	// Processing is processingDetails.processingStatus; "" reads as
	// "succeeded", and "none" leaves processingDetails out.
	Processing string
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
		if f.invalidClient {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"invalid_client"}`))
			return
		}
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
		if f.getErr[r.URL.Query().Get("id")] {
			writeErr(w, http.StatusServiceUnavailable, "backendError")
			return
		}
		items := []any{}
		if v, ok := f.videos[r.URL.Query().Get("id")]; ok {
			item := map[string]any{"status": map[string]any{"uploadStatus": v.UploadStatus, "rejectionReason": v.RejectionReason,
				"privacyStatus": v.Privacy, "publishAt": v.PublishAt, "containsSyntheticMedia": v.Synthetic}}
			switch v.Processing {
			case "":
				item["processingDetails"] = map[string]any{"processingStatus": "succeeded"}
			case "none":
			default:
				item["processingDetails"] = map[string]any{"processingStatus": v.Processing}
			}
			items = append(items, item)
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
		if f.queryErr != nil && f.queryErr(w, u) {
			return
		}
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
	mu        sync.Mutex
	accounts  []store.PlatformAccount
	alerted   []string
	release   store.Release
	hold      string
	job       store.PublishJob
	link      string
	linkBy    string
	closed    []store.ToUnschedule
	forgot    []string
	channel   store.Channel
	recheck   []store.RecheckDue
	rechecked []string
	locked    bool
	holdErr   error
}

type fakeLock struct{}

func (fakeLock) Alive(context.Context) bool { return true }
func (fakeLock) Release(context.Context)    {}

func (s *fakeStore) LockPublisher(context.Context) (store.Lock, bool, error) {
	if s.locked {
		return nil, false, nil
	}
	return fakeLock{}, true, nil
}

func (s *fakeStore) UnannouncedJobs(context.Context) ([]store.PublishJob, error) {
	if s.job.VideoID == "" || s.job.AnnouncedAt != nil || s.release.Status != "approved" || s.hold != "" {
		return nil, nil
	}
	j := s.job
	j.ChannelID = channel
	return []store.PublishJob{j}, nil
}

func (s *fakeStore) SetPublicationURLIfNone(_ context.Context, _, _, url, by string) error {
	if s.link == "" {
		s.link, s.linkBy = url, by
	}
	return nil
}

func (s *fakeStore) SlotsToRecheck(context.Context, time.Time) ([]store.RecheckDue, error) {
	return s.recheck, nil
}

func (s *fakeStore) MarkSlotRechecked(_ context.Context, id string) error {
	s.rechecked = append(s.rechecked, id)
	s.recheck = nil
	return nil
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
	if s.holdErr != nil {
		return s.holdErr
	}
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
func (s *fakeStore) ReleasesToUnschedule(context.Context) ([]store.ToUnschedule, error) {
	return s.closed, nil
}
func (s *fakeStore) forget(id string) {
	s.forgot = append(s.forgot, id)
	s.closed = nil
	s.link = ""
	s.job = store.PublishJob{ReleaseID: rel, Saved: false}
	s.recheck = nil
}

func (s *fakeStore) ForgetIfOutOfPlay(_ context.Context, id, _ string) (bool, error) {
	if s.release.Status == "approved" && s.hold == "" {
		return false, nil
	}
	s.forget(id)
	return true, nil
}

func (s *fakeStore) ForgetRefused(_ context.Context, id, _, ownURL, reason string) (bool, error) {
	if s.holdErr != nil {
		return false, s.holdErr
	}
	if s.link != "" && s.link != ownURL {
		s.job = store.PublishJob{ReleaseID: rel}
		return false, nil
	}
	s.hold = reason
	s.forget(id)
	return true, nil
}

// fakeQueue is the service's side: one item, and a slot.
type fakeQueue struct {
	st      *fakeStore
	items   []service.PublishQueueItem
	slot    time.Time
	refused *service.Error
	booked  int
}

// UploadItems does what the real queue does: no link, approved, not held.
func (q *fakeQueue) UploadItems(context.Context) ([]service.PublishQueueItem, error) {
	if q.st.link != "" || q.st.release.Status != "approved" || q.st.hold != "" {
		return nil, nil
	}
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
	down  bool
	tries int
}

func (n *fakeNotifier) Notify(_ context.Context, card *int64, text string) error {
	n.tries++
	if n.down {
		return errors.New("telegram down")
	}
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
			release: store.Release{ID: rel, ChannelID: channel, Status: "approved", Moderation: store.Moderation{MessageID: &card},
				Media: json.RawMessage(`{"cover":"releases/x/cover.png"}`)},
			job:     store.PublishJob{ReleaseID: rel},
			channel: store.Channel{ID: channel, Language: "en-US", Timezone: "America/New_York"},
		},
		q: &fakeQueue{slot: now.Add(6 * time.Hour).UTC(), items: []service.PublishQueueItem{{
			ReleaseID: rel, ChannelID: channel, Title: "A title", Description: "A description", Tags: []string{"a", "b"},
			Video: "releases/x/video_no_music.mp4", Cover: "releases/x/cover.png",
		}}},
	}
	f.q.st = f.st
	f.p = New(f.st, f.q, client, f.media, f.say, plainSealer{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	f.p.now = func() time.Time { return f.now }
	return f
}

func (f *fixture) pass() { f.p.Pass(context.Background()) }

// due moves past any backoff.
func (f *fixture) due() {
	if f.st.job.NextAttemptAt.After(f.now) {
		f.now = f.st.job.NextAttemptAt.Add(time.Second)
	}
}

// closeIt makes the release closed and on the take-down list, with a link.
func (f *fixture) closeIt(url string, at time.Time) {
	f.q.items = nil
	f.st.release.Status = "rejected"
	f.st.closed = []store.ToUnschedule{{ReleaseID: rel, ChannelID: channel, Status: "rejected", URL: url, PublishAt: at}}
}

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
	f.st.job = store.PublishJob{Saved: true, ReleaseID: rel, SessionURI: f.yt.base + "/session/1", VideoSize: int64(len(videoBytes)), PublishAt: &at}

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
	f.st.job = store.PublishJob{Saved: true, ReleaseID: rel, SessionURI: f.yt.base + "/session/1", VideoSize: int64(len(videoBytes)), PublishAt: &at}

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
	f.st.job = store.PublishJob{Saved: true, ReleaseID: rel, SessionURI: f.yt.base + "/session/forgotten", PublishAt: &at}
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
	f.st.job = store.PublishJob{Saved: true, ReleaseID: rel, SessionURI: f.yt.base + "/session/1", VideoSize: int64(len(videoBytes)), PublishAt: &soon}
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
		t.Fatalf("hold %q, link %q, job %+v", f.st.hold, f.st.link, f.st.job)
	}
	// Lifting the hold uploads afresh.
	f.yt.afterUpload = nil
	f.st.hold = ""
	f.pass()
	if f.st.hold != "" || f.st.link == "" || len(f.yt.started) != 2 {
		t.Errorf("after lifting: hold %q, link %q, sessions %d", f.st.hold, f.st.link, len(f.yt.started))
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

func TestALabelYouTubeDoesntReportDoesntHoldButIsMentioned(t *testing.T) {
	f := newFixture(t)
	f.yt.afterUpload = func(v *video) { v.Synthetic = nil }
	f.pass()
	if f.st.hold != "" || f.st.link == "" {
		t.Errorf("hold %q, link %q", f.st.hold, f.st.link)
	}
	if len(f.say.said) != 1 || !strings.Contains(f.say.said[0], "не подтвердил метку AI") || !strings.Contains(f.say.said[0], "Studio") {
		t.Errorf("told %q", f.say.said)
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
	if f.st.link == "" || f.st.job.Attempts != 0 || f.st.job.LastError != "" || f.st.job.AnnouncedAt == nil {
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

func TestAPosterThatFailsIsRetriedThenTheVideoGoesOnWithoutIt(t *testing.T) {
	f := newFixture(t)
	f.yt.thumbErr = func(w http.ResponseWriter) bool {
		writeErr(w, http.StatusServiceUnavailable, "backendError")
		return true
	}
	f.pass()
	// The link is recorded before the poster: the video exists and can go out.
	if f.st.link == "" || f.st.job.ThumbnailAttempts != 1 || f.st.job.Attempts != 1 || len(f.say.said) != 0 {
		t.Fatalf("first failure: link %q, job %+v, told %q", f.st.link, f.st.job, f.say.said)
	}
	f.due()
	f.pass()
	if f.st.job.ThumbnailAttempts != 2 || len(f.say.said) != 0 {
		t.Fatalf("second failure: job %+v, told %q", f.st.job, f.say.said)
	}
	f.due()
	f.pass()
	if len(f.say.said) != 1 || !strings.Contains(f.say.said[0], "Обложку") || !strings.Contains(f.say.said[0], "Загружен") || f.st.job.AnnouncedAt == nil {
		t.Errorf("told %q, job %+v", f.say.said, f.st.job)
	}
	if f.yt.puts != 1 || len(f.yt.started) != 1 {
		t.Errorf("uploaded again: puts %d, started %d", f.yt.puts, len(f.yt.started))
	}
	f.due()
	f.pass()
	if len(f.say.said) != 1 {
		t.Errorf("told again: %q", f.say.said)
	}
}

// processing sets how far YouTube is with a video.
func (f *fixture) processing(id, status string) {
	f.yt.mu.Lock()
	defer f.yt.mu.Unlock()
	f.yt.videos[id].Processing = status
}

func TestThePosterWaitsForYouTubeToProcessTheVideo(t *testing.T) {
	f := newFixture(t)
	f.yt.afterUpload = func(v *video) { v.Processing = "processing" }
	f.pass()
	vid := f.st.job.VideoID
	// The link is there at once: the video exists and can go out.
	if vid == "" || f.st.link == "" || f.st.hold != "" {
		t.Fatalf("job %+v, link %q, hold %q", f.st.job, f.st.link, f.st.hold)
	}
	if len(f.yt.thumbs) != 0 || len(f.say.said) != 0 || f.st.job.Attempts != 1 || !f.st.job.NextAttemptAt.Equal(f.now.Add(time.Minute)) {
		t.Fatalf("didn't wait: thumbs %d, told %q, job %+v", len(f.yt.thumbs), f.say.said, f.st.job)
	}
	f.due()
	f.pass()
	if len(f.yt.thumbs) != 0 || len(f.say.said) != 0 || f.st.job.Attempts != 2 {
		t.Fatalf("second pass: thumbs %d, told %q, job %+v", len(f.yt.thumbs), f.say.said, f.st.job)
	}
	f.processing(vid, "succeeded")
	f.due()
	f.pass()
	if string(f.yt.thumbs[vid]) != "PNGDATA" || !f.st.job.ThumbnailSet || f.st.job.AnnouncedAt == nil || f.st.job.Attempts != 0 || f.st.job.LastError != "" {
		t.Fatalf("after processing: thumbs %q, job %+v", f.yt.thumbs[vid], f.st.job)
	}
	if len(f.say.said) != 1 || strings.Contains(f.say.said[0], "Обложку") || !strings.Contains(f.say.said[0], "Загружен") {
		t.Errorf("told %q", f.say.said)
	}
	if f.yt.puts != 1 || len(f.yt.started) != 1 {
		t.Errorf("uploaded again: puts %d, started %d", f.yt.puts, len(f.yt.started))
	}
}

func TestAPosterStillProcessingAfterTheWaitsGoesUpWithAWarning(t *testing.T) {
	for _, status := range []string{"processing", "none"} {
		f := newFixture(t)
		f.yt.afterUpload = func(v *video) { v.Processing = status }
		f.pass()
		for i := 1; i < processingWaits; i++ {
			f.due()
			f.pass()
		}
		if len(f.yt.thumbs) != 0 || len(f.say.said) != 0 || f.st.job.Attempts != processingWaits {
			t.Fatalf("%s: waits: thumbs %d, told %q, job %+v", status, len(f.yt.thumbs), f.say.said, f.st.job)
		}
		// Telegram is down at that moment: the warning mustn't be lost.
		f.say.down = true
		f.due()
		f.pass()
		vid := f.st.job.VideoID
		if string(f.yt.thumbs[vid]) != "PNGDATA" || f.st.job.AnnouncedAt != nil {
			t.Fatalf("%s: after the waits: thumbs %q, job %+v", status, f.yt.thumbs[vid], f.st.job)
		}
		f.say.down = false
		f.due()
		f.pass()
		if f.st.job.AnnouncedAt == nil || f.st.hold != "" {
			t.Fatalf("%s: job %+v, hold %q", status, f.st.job, f.st.hold)
		}
		if len(f.say.said) != 1 || !strings.Contains(f.say.said[0], "Обложку hub поставил") || !strings.Contains(f.say.said[0], "Studio") {
			t.Errorf("%s: told %q", status, f.say.said)
		}
	}
}

func TestAnUploadsFailuresDontShortenThePostersWait(t *testing.T) {
	f := newFixture(t)
	f.yt.afterUpload = func(v *video) { v.Processing = "processing" }
	f.yt.startErr = func(w http.ResponseWriter) bool {
		writeErr(w, http.StatusServiceUnavailable, "backendError")
		return true
	}
	for i := 0; i < processingWaits; i++ {
		f.pass()
		f.due()
	}
	f.yt.startErr = nil
	f.pass()
	if f.st.job.VideoID == "" || len(f.yt.thumbs) != 0 || len(f.say.said) != 0 || f.st.job.Attempts != 1 {
		t.Errorf("thumbs %d, told %q, job %+v", len(f.yt.thumbs), f.say.said, f.st.job)
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
	f.st.release.Media = json.RawMessage(`{}`)
	f.pass()
	if f.st.link == "" || len(f.say.said) != 1 || !strings.Contains(f.say.said[0], "нет обложки") {
		t.Errorf("link %q, told %q", f.st.link, f.say.said)
	}
}

func scheduled(at time.Time) *video {
	return &video{UploadStatus: "uploaded", Privacy: "private", PublishAt: at.Format(time.RFC3339), Synthetic: yes()}
}

func TestTakingDownAVideoWithALink(t *testing.T) {
	f := newFixture(t)
	at := f.now.Add(3 * time.Hour)
	f.yt.videos["abcdefghijk"] = scheduled(at)
	f.closeIt("https://www.youtube.com/shorts/abcdefghijk", at)
	f.pass()
	if len(f.yt.unsched) != 1 || f.yt.unsched[0] != "abcdefghijk" || len(f.st.forgot) != 1 {
		t.Fatalf("unscheduled %v, forgot %v", f.yt.unsched, f.st.forgot)
	}
	if f.yt.videos["abcdefghijk"].PublishAt != "" {
		t.Error("the video still has its time")
	}
	if len(f.say.said) != 1 || !strings.Contains(f.say.said[0], "снят с расписания") || !strings.Contains(f.say.said[0], "Private") {
		t.Errorf("told %q", f.say.said)
	}
}

func TestATakeDownThatFailsKeepsTryingAndTellsOnceAfterTenMinutes(t *testing.T) {
	for _, c := range []struct {
		status int
		reason string
	}{{403, "quotaExceeded"}, {500, "backendError"}, {503, ""}} {
		f := newFixture(t)
		at := f.now.Add(90 * time.Minute)
		f.yt.videos["abcdefghijk"] = scheduled(at)
		f.closeIt("https://www.youtube.com/shorts/abcdefghijk", at)
		calls := 0
		f.yt.unschedErr = func(w http.ResponseWriter) bool { calls++; writeErr(w, c.status, c.reason); return true }
		start := f.now
		for f.now.Sub(start) < 9*time.Minute {
			f.pass()
			f.now = f.now.Add(time.Minute)
		}
		if len(f.say.said) != 0 {
			t.Fatalf("%s: before ten minutes: told %q", c.reason, f.say.said)
		}
		for f.now.Sub(start) < 20*time.Minute {
			f.pass()
			f.now = f.now.Add(time.Minute)
		}
		// Not every pass: an update costs quota. But it keeps going.
		if calls < 4 || calls > 8 || len(f.st.forgot) != 0 {
			t.Errorf("%s: calls %d, forgot %v", c.reason, calls, f.st.forgot)
		}
		if len(f.say.said) != 1 || !strings.Contains(f.say.said[0], "abcdefghijk") || !strings.Contains(f.say.said[0], at.UTC().Format("02.01 15:04")) {
			t.Errorf("%s: told %q", c.reason, f.say.said)
		}
		// YouTube comes back: it comes off, and the group hears that too.
		f.yt.unschedErr = nil
		f.now = f.now.Add(5 * time.Minute)
		f.pass()
		if len(f.st.forgot) != 1 || len(f.say.said) != 2 {
			t.Errorf("%s: after recovering: forgot %v, told %q", c.reason, f.st.forgot, f.say.said)
		}
	}
}

func TestATakeDownFailingCloseToTheSlotTellsAtOnce(t *testing.T) {
	f := newFixture(t)
	at := f.now.Add(8 * time.Minute)
	f.yt.videos["abcdefghijk"] = scheduled(at)
	f.closeIt("https://www.youtube.com/shorts/abcdefghijk", at)
	f.yt.unschedErr = func(w http.ResponseWriter) bool { writeErr(w, 503, ""); return true }
	f.pass()
	if len(f.say.said) != 1 {
		t.Errorf("told %q", f.say.said)
	}
}

func TestATakeDownYouTubeRefusesTellsOnceAndKeepsTrying(t *testing.T) {
	f := newFixture(t)
	at := f.now.Add(3 * time.Hour)
	f.yt.videos["abcdefghijk"] = scheduled(at)
	f.closeIt("https://www.youtube.com/shorts/abcdefghijk", at)
	f.yt.unschedErr = func(w http.ResponseWriter) bool { writeErr(w, 403, "forbidden"); return true }
	f.pass()
	f.pass()
	if len(f.st.forgot) != 0 || len(f.say.said) != 1 || !strings.Contains(f.say.said[0], "не может снять") {
		t.Errorf("forgot %v, told %q", f.st.forgot, f.say.said)
	}
}

func TestAScheduleThatSurvivesTheUpdateIsToldAboutAndKept(t *testing.T) {
	f := newFixture(t)
	at := f.now.Add(90 * time.Minute)
	f.yt.videos["abcdefghijk"] = scheduled(at)
	f.closeIt("https://www.youtube.com/shorts/abcdefghijk", at)
	f.yt.unschedErr = func(http.ResponseWriter) bool { return true } // answers 200, changes nothing
	f.pass()
	if len(f.st.forgot) != 0 {
		t.Fatalf("forgot %v", f.st.forgot)
	}
	f.now = f.now.Add(2 * time.Minute)
	f.pass()
	f.now = f.now.Add(6 * time.Minute)
	f.pass()
	if len(f.st.forgot) != 0 || len(f.say.said) != 1 || !strings.Contains(f.say.said[0], "Studio") || !strings.Contains(f.say.said[0], "abcdefghijk") {
		t.Fatalf("forgot %v, told %q", f.st.forgot, f.say.said)
	}
	// It does come off in the end: the record goes then.
	f.yt.unschedErr = nil
	f.now = f.now.Add(6 * time.Minute)
	f.pass()
	if len(f.st.forgot) != 1 {
		t.Errorf("after it came off: forgot %v", f.st.forgot)
	}
}

// An update answering 404 while the video is there.
func TestAnUpdateThatCantFindAVideoStillOnTheScheduleIsNotBelieved(t *testing.T) {
	f := newFixture(t)
	at := f.now.Add(3 * time.Hour)
	f.yt.videos["abcdefghijk"] = scheduled(at)
	f.closeIt("https://www.youtube.com/shorts/abcdefghijk", at)
	f.yt.unschedErr = func(w http.ResponseWriter) bool { writeErr(w, 404, "videoNotFound"); return true }
	f.pass()
	if len(f.st.forgot) != 0 {
		t.Errorf("forgot %v", f.st.forgot)
	}
}

// A listing can lag behind a video that is scheduled: "not found" drops nothing.
func TestAVideoNotShownOnATakeDownIsTriedAndNeverForgotten(t *testing.T) {
	f := newFixture(t)
	at := f.now.Add(110 * time.Minute)
	f.st.job = store.PublishJob{Saved: true, ReleaseID: rel, VideoID: "abcdefghijk", PublishAt: &at}
	f.closeIt("https://www.youtube.com/shorts/abcdefghijk", at)
	f.pass()
	if len(f.yt.unsched) != 1 || f.yt.unsched[0] != "abcdefghijk" {
		t.Fatalf("not tried: %v", f.yt.unsched)
	}
	if len(f.say.said) != 0 {
		t.Errorf("said on the first miss: %q", f.say.said)
	}
	for i := 0; i < 8; i++ {
		f.now = f.now.Add(5 * time.Minute)
		f.pass()
	}
	if len(f.st.forgot) != 0 || f.st.job.VideoID != "abcdefghijk" {
		t.Fatalf("the record went: forgot %v, job %+v", f.st.forgot, f.st.job)
	}
	if len(f.say.said) != 1 || !strings.Contains(f.say.said[0], "не показывает") || !strings.Contains(f.say.said[0], "Studio") || strings.Contains(f.say.said[0], "Private") {
		t.Errorf("told %q", f.say.said)
	}
	if len(f.yt.unsched) < 3 {
		t.Errorf("tried only %d times", len(f.yt.unsched))
	}
	// The listing catches up: it comes off the schedule properly.
	f.yt.videos["abcdefghijk"] = scheduled(at)
	f.now = f.now.Add(5 * time.Minute)
	f.pass()
	if len(f.st.forgot) != 1 || f.yt.videos["abcdefghijk"].PublishAt != "" {
		t.Errorf("forgot %v, publishAt %q", f.st.forgot, f.yt.videos["abcdefghijk"].PublishAt)
	}
}

func TestAVideoNotShownCloseToItsSlotIsSaidAtOnce(t *testing.T) {
	f := newFixture(t)
	f.closeIt("https://www.youtube.com/shorts/abcdefghijk", f.now.Add(8*time.Minute))
	f.pass()
	if len(f.say.said) != 1 || len(f.st.forgot) != 0 {
		t.Errorf("told %q, forgot %v", f.say.said, f.st.forgot)
	}
}

// A read without publishAt proves nothing about the write that follows.
func TestATakeDownAlwaysUpdatesAVideoItFinds(t *testing.T) {
	f := newFixture(t)
	f.yt.videos["abcdefghijk"] = &video{UploadStatus: "uploaded", Privacy: "private"}
	f.closeIt("https://www.youtube.com/shorts/abcdefghijk", f.now.Add(3*time.Hour))
	f.pass()
	if len(f.yt.unsched) != 1 || len(f.st.forgot) != 1 {
		t.Errorf("unscheduled %v, forgot %v", f.yt.unsched, f.st.forgot)
	}
}

// Hold lifted while the take-down was under way: the upload goes on, and its
// record must survive.
func TestAHoldLiftedMidTakeDownKeepsTheRecord(t *testing.T) {
	f := newFixture(t)
	at := f.now.Add(3 * time.Hour)
	f.yt.videos["abcdefghijk"] = scheduled(at)
	f.st.hold = "held"
	f.st.link = shortsURL("abcdefghijk")
	f.st.job = store.PublishJob{Saved: true, ReleaseID: rel, VideoID: "abcdefghijk", PublishAt: &at}
	f.st.closed = []store.ToUnschedule{{ReleaseID: rel, ChannelID: channel, Status: "approved", Hold: "held", URL: f.st.link, PublishAt: at}}
	f.yt.unschedErr = func(http.ResponseWriter) bool {
		f.st.hold = "" // lifted just now
		f.yt.videos["abcdefghijk"].PublishAt = ""
		return true
	}
	f.p.takeDown(context.Background(), map[string]string{channel: refresh})
	if len(f.st.forgot) != 0 || f.st.link == "" || f.st.job.VideoID == "" {
		t.Fatalf("wiped: forgot %v, link %q, job %+v", f.st.forgot, f.st.link, f.st.job)
	}
	if len(f.say.said) != 1 || !strings.Contains(f.say.said[0], "без расписания") {
		t.Errorf("told %q", f.say.said)
	}
}

// A failed hold must not leave a release free to upload again.
func TestAFailedHoldForgetsNothing(t *testing.T) {
	f := newFixture(t)
	f.yt.afterUpload = func(v *video) { v.UploadStatus, v.RejectionReason = "rejected", "duplicate" }
	f.st.holdErr = errors.New("database down")
	f.pass()
	if f.st.link == "" || f.st.job.VideoID == "" || len(f.st.forgot) != 0 {
		t.Fatalf("forgot with no hold: link %q, job %+v, forgot %v", f.st.link, f.st.job, f.st.forgot)
	}
	f.due()
	f.pass()
	if len(f.yt.started) != 1 {
		t.Errorf("uploaded again: %d sessions", len(f.yt.started))
	}
	f.st.holdErr = nil
	f.due()
	f.pass()
	if !strings.Contains(f.st.hold, "duplicate") {
		t.Errorf("hold %q", f.st.hold)
	}
}

// A hold lifted and put back later is a new case.
func TestAnAlertIsToldAgainAfterTheReleaseLeftTheList(t *testing.T) {
	f := newFixture(t)
	at := f.now.Add(5 * time.Minute)
	f.yt.videos["abcdefghijk"] = scheduled(at)
	closed := []store.ToUnschedule{{ReleaseID: rel, ChannelID: channel, Status: "approved", Hold: "x", URL: shortsURL("abcdefghijk"), PublishAt: at}}
	f.q.items = nil
	f.st.closed = closed
	f.st.hold = "x"
	f.yt.unschedErr = func(w http.ResponseWriter) bool { writeErr(w, 503, ""); return true }
	f.pass()
	if len(f.say.said) != 1 {
		t.Fatalf("first: told %q", f.say.said)
	}
	f.st.closed, f.st.hold = nil, "" // lifted
	f.pass()
	f.st.closed, f.st.hold = closed, "y" // held again
	f.pass()
	if len(f.say.said) != 2 {
		t.Errorf("told %q", f.say.said)
	}
}

// A wrong link mustn't keep hub's own video on the schedule.
func TestOneVideosErrorDoesntStopTheOther(t *testing.T) {
	f := newFixture(t)
	at := f.now.Add(3 * time.Hour)
	f.yt.videos["hubsvideo01"] = scheduled(at)
	f.yt.getErr = map[string]bool{"wronglink01": true}
	f.st.job = store.PublishJob{Saved: true, ReleaseID: rel, VideoID: "hubsvideo01", PublishAt: &at}
	f.closeIt(shortsURL("wronglink01"), at)
	f.pass()
	if f.yt.videos["hubsvideo01"].PublishAt != "" || len(f.yt.unsched) != 1 || f.yt.unsched[0] != "hubsvideo01" {
		t.Fatalf("hub's video: publishAt %q, unscheduled %v", f.yt.videos["hubsvideo01"].PublishAt, f.yt.unsched)
	}
	if len(f.st.forgot) != 0 {
		t.Errorf("forgot %v with one video unaccounted for", f.st.forgot)
	}
	f.now = f.now.Add(11 * time.Minute)
	f.pass()
	if len(f.say.said) != 1 || !strings.Contains(f.say.said[0], "wronglink01") || strings.Contains(f.say.said[0], "hubsvideo01") {
		t.Errorf("told %q", f.say.said)
	}
}

// A passing 403 ends nothing: the upload may have finished.
func TestAPassingRateLimitKeepsTheSession(t *testing.T) {
	f := newFixture(t)
	at := f.q.slot
	f.yt.uploads["/session/1"] = &upload{size: len(videoBytes), received: len(videoBytes), videoID: "video000042"}
	f.yt.videos["video000042"] = scheduled(at)
	f.st.job = store.PublishJob{Saved: true, ReleaseID: rel, SessionURI: f.yt.base + "/session/1", VideoSize: int64(len(videoBytes)), PublishAt: &at}
	f.yt.queryErr = func(w http.ResponseWriter, u *upload) bool { writeErr(w, 403, "rateLimitExceeded"); return true }
	f.pass()
	if f.st.job.SessionURI == "" || len(f.yt.started) != 0 || f.st.hold != "" {
		t.Fatalf("job %+v, sessions %d, hold %q", f.st.job, len(f.yt.started), f.st.hold)
	}
	f.yt.queryErr = nil
	f.due()
	f.pass()
	if f.st.job.VideoID != "video000042" || len(f.yt.started) != 0 || f.yt.puts != 0 {
		t.Errorf("job %+v, sessions %d, puts %d", f.st.job, len(f.yt.started), f.yt.puts)
	}
}

func TestAPassingRateLimitOnATakeDownForgetsNothing(t *testing.T) {
	f := newFixture(t)
	at := f.now.Add(90 * time.Minute)
	f.yt.uploads["/session/1"] = &upload{size: len(videoBytes), received: len(videoBytes), videoID: "video000042"}
	f.yt.videos["video000042"] = scheduled(at)
	f.st.job = store.PublishJob{Saved: true, ReleaseID: rel, SessionURI: f.yt.base + "/session/1", VideoSize: int64(len(videoBytes)), PublishAt: &at}
	f.closeIt("", at)
	f.yt.queryErr = func(w http.ResponseWriter, u *upload) bool { writeErr(w, 403, "rateLimitExceeded"); return true }
	f.pass()
	if len(f.st.forgot) != 0 {
		t.Fatalf("forgot %v", f.st.forgot)
	}
	f.yt.queryErr = nil
	f.now = f.now.Add(time.Minute)
	f.pass()
	if len(f.yt.unsched) != 1 || f.yt.unsched[0] != "video000042" || len(f.st.forgot) != 1 {
		t.Errorf("unscheduled %v, forgot %v", f.yt.unsched, f.st.forgot)
	}
}

func TestNoLockTellsTheGroupOnceAfterTenMinutes(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.p.withoutLock(ctx)
	f.now = f.now.Add(9 * time.Minute)
	f.p.withoutLock(ctx)
	if len(f.say.said) != 0 {
		t.Fatalf("too soon: %q", f.say.said)
	}
	f.now = f.now.Add(time.Minute)
	f.say.down = true
	f.p.withoutLock(ctx)
	f.say.down = false
	f.p.withoutLock(ctx)
	f.p.withoutLock(ctx)
	if len(f.say.said) != 1 || !strings.Contains(f.say.said[0], "блокировки") {
		t.Errorf("told %q", f.say.said)
	}
}

func TestAVideoYouTubeRejectedHasNothingToTakeDown(t *testing.T) {
	f := newFixture(t)
	f.yt.videos["abcdefghijk"] = &video{UploadStatus: "rejected", RejectionReason: "duplicate", Privacy: "private"}
	f.closeIt("https://www.youtube.com/shorts/abcdefghijk", f.now.Add(3*time.Hour))
	f.pass()
	if len(f.yt.unsched) != 0 || len(f.st.forgot) != 1 || !strings.Contains(f.say.said[0], "не принял") {
		t.Errorf("unscheduled %v, forgot %v, told %q", f.yt.unsched, f.st.forgot, f.say.said)
	}
}

func TestAPublicVideoOfAClosedReleaseIsMadePrivateToo(t *testing.T) {
	f := newFixture(t)
	f.yt.videos["abcdefghijk"] = &video{UploadStatus: "processed", Privacy: "public"}
	f.closeIt("https://www.youtube.com/shorts/abcdefghijk", f.now.Add(3*time.Hour))
	f.yt.unschedErr = func(http.ResponseWriter) bool { f.yt.videos["abcdefghijk"].Privacy = "private"; return true }
	f.pass()
	if len(f.st.forgot) != 1 {
		t.Errorf("forgot %v", f.st.forgot)
	}
}

func TestTakingDownWithNoLinkAsksTheJob(t *testing.T) {
	// The job knows the video.
	f := newFixture(t)
	at := f.now.Add(3 * time.Hour)
	f.yt.videos["knownvideo1"] = scheduled(at)
	f.closeIt("", at)
	f.st.job = store.PublishJob{Saved: true, ReleaseID: rel, VideoID: "knownvideo1"}
	f.pass()
	if len(f.yt.unsched) != 1 || f.yt.unsched[0] != "knownvideo1" || len(f.st.forgot) != 1 {
		t.Errorf("known: unscheduled %v, forgot %v", f.yt.unsched, f.st.forgot)
	}
	// The session finished, and the answer never reached hub.
	f = newFixture(t)
	at = f.now.Add(3 * time.Hour)
	f.yt.videos["finished001"] = scheduled(at)
	f.closeIt("", at)
	f.yt.uploads["/session/1"] = &upload{size: 1000, received: 1000, videoID: "finished001"}
	f.st.job = store.PublishJob{Saved: true, ReleaseID: rel, SessionURI: f.yt.base + "/session/1", VideoSize: 1000}
	f.pass()
	if len(f.yt.unsched) != 1 || f.yt.unsched[0] != "finished001" {
		t.Errorf("finished: unscheduled %v", f.yt.unsched)
	}
	// The session never finished: no video exists, nothing to say.
	f = newFixture(t)
	f.closeIt("", f.now.Add(3*time.Hour))
	f.yt.uploads["/session/1"] = &upload{size: 1000, received: 10}
	f.st.job = store.PublishJob{Saved: true, ReleaseID: rel, SessionURI: f.yt.base + "/session/1", VideoSize: 1000}
	f.pass()
	if len(f.yt.unsched) != 0 || len(f.st.forgot) != 1 || len(f.say.said) != 0 {
		t.Errorf("unfinished: unscheduled %v, forgot %v, told %q", f.yt.unsched, f.st.forgot, f.say.said)
	}
	// A slot with no job row and no link: not hub's, so hub can't vouch for it.
	f = newFixture(t)
	f.closeIt("", f.now.Add(3*time.Hour))
	f.pass()
	if len(f.yt.unsched) != 0 || len(f.st.forgot) != 1 || len(f.say.said) != 1 || !strings.Contains(f.say.said[0], "Studio") {
		t.Errorf("no job: unscheduled %v, forgot %v, told %q", f.yt.unsched, f.st.forgot, f.say.said)
	}
}

// Every byte has arrived, and YouTube hasn't made the video yet.
func TestAFinalisingUploadIsWaitedForNotForgotten(t *testing.T) {
	f := newFixture(t)
	at := f.now.Add(3 * time.Hour)
	f.closeIt("", at)
	f.yt.uploads["/session/1"] = &upload{size: len(videoBytes), received: len(videoBytes)}
	f.st.job = store.PublishJob{Saved: true, ReleaseID: rel, SessionURI: f.yt.base + "/session/1", VideoSize: int64(len(videoBytes)), PublishAt: &at}
	f.pass()
	if len(f.st.forgot) != 0 {
		t.Fatalf("take-down forgot it: %v", f.st.forgot)
	}
	// The same state on the way up: no second send.
	g := newFixture(t)
	g.yt.uploads["/session/1"] = &upload{size: len(videoBytes), received: len(videoBytes)}
	g.st.job = store.PublishJob{Saved: true, ReleaseID: rel, SessionURI: g.yt.base + "/session/1", VideoSize: int64(len(videoBytes)), PublishAt: &at}
	g.pass()
	if g.yt.puts != 0 || g.st.hold != "" || g.st.job.Attempts != 1 || len(g.yt.started) != 0 {
		t.Errorf("puts %d, hold %q, job %+v, sessions %d", g.yt.puts, g.st.hold, g.st.job, len(g.yt.started))
	}
}

func TestAStatusQueryThatRepeatsARefusalDropsTheSession(t *testing.T) {
	f := newFixture(t)
	f.yt.putErr = func(w http.ResponseWriter, u *upload) bool { writeErr(w, 400, "invalidVideoMetadata"); return true }
	f.pass()
	if f.st.hold == "" || f.st.job.SessionURI != "" {
		t.Fatalf("hold %q, job %+v", f.st.hold, f.st.job)
	}
	// Held with a slot: on the take-down list, with no video behind it.
	f.st.closed = []store.ToUnschedule{{ReleaseID: rel, ChannelID: channel, Status: "approved", Hold: f.st.hold, PublishAt: f.q.slot}}
	f.pass()
	if len(f.st.forgot) != 1 {
		t.Errorf("forgot %v", f.st.forgot)
	}
	for _, said := range f.say.said {
		if strings.HasSuffix(strings.TrimSpace(said), "/shorts/") {
			t.Errorf("a link to nothing: %q", said)
		}
	}
	// Someone lifts the hold, YouTube is fine now: a new session, no second hold.
	f.yt.putErr = nil
	f.st.closed, f.st.hold = nil, ""
	f.pass()
	if f.st.hold != "" || f.st.link == "" || len(f.yt.started) != 2 {
		t.Errorf("after lifting: hold %q, link %q, sessions %d", f.st.hold, f.st.link, len(f.yt.started))
	}
}

func TestADeadSessionFoundOnTheStatusQueryIsDropped(t *testing.T) {
	f := newFixture(t)
	at := f.q.slot
	f.yt.uploads["/session/1"] = &upload{size: len(videoBytes), received: 10}
	f.yt.queryErr = func(w http.ResponseWriter, u *upload) bool { writeErr(w, 400, "invalidVideoMetadata"); return true }
	f.st.job = store.PublishJob{Saved: true, ReleaseID: rel, SessionURI: f.yt.base + "/session/1", VideoSize: int64(len(videoBytes)), PublishAt: &at}
	f.pass()
	if f.st.hold == "" || f.st.job.SessionURI != "" {
		t.Fatalf("hold %q, job %+v", f.st.hold, f.st.job)
	}
	f.st.hold = ""
	f.yt.queryErr = nil
	f.pass()
	if f.st.hold != "" || f.st.link == "" || len(f.yt.started) != 1 {
		t.Errorf("after lifting: hold %q, link %q, sessions %d", f.st.hold, f.st.link, len(f.yt.started))
	}
}

// uploadLimitExceeded passes with time, but the session it ended doesn't come back.
func TestARetryableErrorEndingASessionStartsANewOneNextTime(t *testing.T) {
	f := newFixture(t)
	f.yt.putErr = func(w http.ResponseWriter, u *upload) bool { writeErr(w, 400, "uploadLimitExceeded"); return true }
	f.yt.queryErr = func(w http.ResponseWriter, u *upload) bool { writeErr(w, 400, "uploadLimitExceeded"); return true }
	f.pass()
	if f.st.hold != "" || f.st.job.SessionURI != "" || f.st.job.Attempts != 1 {
		t.Fatalf("hold %q, job %+v", f.st.hold, f.st.job)
	}
	f.due()
	f.pass()
	if len(f.yt.started) != 2 || f.st.job.Attempts != 2 {
		t.Errorf("sessions %d, job %+v", len(f.yt.started), f.st.job)
	}
	f.yt.putErr, f.yt.queryErr = nil, nil
	f.due()
	f.pass()
	if f.st.link == "" {
		t.Errorf("after the limit passed: job %+v", f.st.job)
	}
}

func TestABrowserSlotWithNoJobHoldsAndNeverUploadsASecondCopy(t *testing.T) {
	f := newFixture(t)
	at := f.q.slot
	f.q.items[0].PublishAt = &at
	f.pass()
	if len(f.yt.started) != 0 || f.yt.puts != 0 || f.st.link != "" || !strings.Contains(f.st.hold, "Studio") {
		t.Fatalf("sessions %d, puts %d, link %q, hold %q", len(f.yt.started), f.yt.puts, f.st.link, f.st.hold)
	}
	// The owner looked, and it isn't there: lifting the hold means go.
	f.st.hold = ""
	f.pass()
	if f.st.hold != "" || f.st.link == "" || len(f.yt.started) != 1 {
		t.Errorf("after lifting: hold %q, link %q, sessions %d", f.st.hold, f.st.link, len(f.yt.started))
	}
}

func TestHubsOwnSlotKeepsGoingAfterACrash(t *testing.T) {
	f := newFixture(t)
	at := f.q.slot
	f.q.items[0].PublishAt = &at
	f.st.job = store.PublishJob{Saved: true, ReleaseID: rel}
	f.pass()
	if f.st.hold != "" || f.st.link == "" {
		t.Errorf("hold %q, link %q", f.st.hold, f.st.link)
	}
}

func TestTheLinkIsRecordedAsSoonAsTheVideoExists(t *testing.T) {
	f := newFixture(t)
	f.yt.thumbErr = func(w http.ResponseWriter) bool { writeErr(w, 503, ""); return true }
	f.pass()
	if f.st.job.VideoID == "" || f.st.link != shortsURL(f.st.job.VideoID) {
		t.Errorf("job %+v, link %q", f.st.job, f.st.link)
	}
}

func TestAModeratorsLinkIsLeftInPlace(t *testing.T) {
	f := newFixture(t)
	f.st.link = "https://www.youtube.com/shorts/moderators1"
	f.st.linkBy = "Kostya"
	f.st.job = store.PublishJob{Saved: true, ReleaseID: rel, VideoID: "hubsvideo01"}
	f.yt.videos["hubsvideo01"] = scheduled(f.q.slot)
	at := f.q.slot
	f.st.job.PublishAt = &at
	f.pass()
	if f.st.link != "https://www.youtube.com/shorts/moderators1" {
		t.Errorf("link %q", f.st.link)
	}
}

// coverGone is a store with no cover in it.
type coverGone struct{ *fakeMedia }

func (m coverGone) Open(ctx context.Context, key string) (io.ReadCloser, int64, error) {
	if strings.HasSuffix(key, "cover.png") {
		return nil, 0, errors.New("NoSuchKey")
	}
	return m.fakeMedia.Open(ctx, key)
}

func TestAnUnreadableCoverCountsAsAttemptsThenOneAlert(t *testing.T) {
	f := newFixture(t)
	f.p.media = coverGone{f.media}
	f.pass()
	if f.st.link == "" {
		t.Fatal("no link")
	}
	for i := 0; i < 8; i++ {
		f.due()
		f.pass()
	}
	if f.st.job.ThumbnailAttempts != thumbnailAttempts-1 || f.st.job.AnnouncedAt == nil || f.st.hold != "" {
		t.Errorf("job %+v, hold %q", f.st.job, f.st.hold)
	}
	if len(f.say.said) != 1 || !strings.Contains(f.say.said[0], "Обложку") {
		t.Errorf("told %q", f.say.said)
	}
}

func TestAVideoNotShownAfterUploadHoldsAndKeepsItsRecord(t *testing.T) {
	f := newFixture(t)
	f.pass()
	vid := f.st.job.VideoID
	delete(f.yt.videos, vid)
	f.st.link = "" // cleared on the admin page
	f.say.said = nil
	for i := 0; i < 5 && f.st.hold == ""; i++ {
		f.pass()
		f.now = f.now.Add(10 * time.Minute)
	}
	if !strings.Contains(f.st.hold, "не показывает") || !strings.Contains(f.st.hold, "DELETE") {
		t.Fatalf("hold %q", f.st.hold)
	}
	if f.st.link != shortsURL(vid) || f.st.job.VideoID != vid || len(f.st.forgot) != 0 {
		t.Fatalf("the record went: link %q, job %+v, forgot %v", f.st.link, f.st.job, f.st.forgot)
	}
	if len(f.yt.unsched) == 0 || f.yt.unsched[0] != vid {
		t.Errorf("no attempt to take it down: %v", f.yt.unsched)
	}
	// Lifting the hold doesn't upload a second copy, or hold again.
	f.st.hold = ""
	f.st.closed = nil
	for i := 0; i < 3; i++ {
		f.now = f.now.Add(time.Hour)
		f.pass()
	}
	if len(f.yt.started) != 1 || f.st.hold != "" {
		t.Errorf("after lifting: sessions %d, hold %q", len(f.yt.started), f.st.hold)
	}
	// A deliberate re-upload: the operator's DELETE /youtube, which forgets.
	f.st.forget(rel)
	f.pass()
	if len(f.yt.started) != 2 {
		t.Errorf("after DELETE /youtube: sessions %d", len(f.yt.started))
	}
}

func TestAVideoNotShownBeforeTheFirstAnnouncementHolds(t *testing.T) {
	f := newFixture(t)
	f.yt.afterUpload = func(*video) {}
	f.p.yt = hiddenVideo{f.p.yt}
	f.pass()
	if f.st.link == "" || f.st.hold != "" {
		t.Fatalf("first miss: link %q, hold %q", f.st.link, f.st.hold)
	}
	for i := 0; i < 4 && f.st.hold == ""; i++ {
		f.due()
		f.pass()
	}
	if !strings.Contains(f.st.hold, "не показывает") || f.st.link == "" || f.st.job.VideoID == "" || len(f.st.forgot) != 0 {
		t.Errorf("hold %q, link %q, job %+v, forgot %v", f.st.hold, f.st.link, f.st.job, f.st.forgot)
	}
}

// hiddenVideo is a listing that never shows the video.
type hiddenVideo struct{ YouTube }

func (hiddenVideo) GetVideo(context.Context, string, string) (youtube.Video, error) {
	return youtube.Video{}, nil
}

func TestAClearedLinkOnAVideoThatIsThereComesBackQuietly(t *testing.T) {
	f := newFixture(t)
	f.pass()
	f.st.link = ""
	f.say.said = nil
	f.pass()
	if f.st.link == "" || len(f.say.said) != 0 || len(f.yt.started) != 1 {
		t.Errorf("link %q, told %q, sessions %d", f.st.link, f.say.said, len(f.yt.started))
	}
}

func TestAReleaseClosedMidUploadIsNotPromised(t *testing.T) {
	f := newFixture(t)
	f.yt.afterUpload = func(v *video) { f.st.release.Status = "rejected" }
	f.pass()
	if f.st.link == "" {
		t.Error("the video exists: its link must be on record for the take-down")
	}
	for _, said := range f.say.said {
		if strings.Contains(said, "выйдет") {
			t.Errorf("promised: %q", said)
		}
	}
}

// A take-down must not forget a release the upload loop has in hand.
func TestATakeDownWaitsForAnUploadInProgress(t *testing.T) {
	f := newFixture(t)
	at := f.now.Add(3 * time.Hour)
	f.closeIt("", at)
	f.p.setUploading(rel)
	f.p.takeDown(context.Background(), map[string]string{channel: refresh})
	if len(f.st.forgot) != 0 {
		t.Fatalf("forgot %v while uploading", f.st.forgot)
	}
	f.p.setUploading("")
	f.p.takeDown(context.Background(), map[string]string{channel: refresh})
	if len(f.st.forgot) != 1 {
		t.Errorf("after the upload: forgot %v", f.st.forgot)
	}
}

func TestAModeratorsReplacedLinkTakesDownBothVideos(t *testing.T) {
	f := newFixture(t)
	f.q.slot = f.now.Add(90 * time.Minute).UTC()
	f.pass()
	hubs := f.st.job.VideoID
	theirs := "video00000X"
	f.yt.videos[theirs] = scheduled(f.q.slot)
	f.closeIt(shortsURL(theirs), f.q.slot)
	first := true
	f.yt.unschedErr = func(w http.ResponseWriter) bool {
		// The first one goes through, the other doesn't: nothing is forgotten yet.
		if first {
			first = false
			f.yt.unsched = append(f.yt.unsched, theirs)
			f.yt.videos[theirs].PublishAt = ""
			return true
		}
		writeErr(w, 503, "")
		return true
	}
	f.pass()
	if len(f.st.forgot) != 0 || f.st.job.VideoID != hubs {
		t.Fatalf("hub's video still on the schedule, yet: forgot %v, job %+v", f.st.forgot, f.st.job)
	}
	f.yt.unschedErr = nil
	f.now = f.now.Add(time.Minute)
	f.pass()
	if len(f.st.forgot) != 1 || f.yt.videos[hubs].PublishAt != "" || f.yt.videos[theirs].PublishAt != "" {
		t.Errorf("forgot %v, hub's %q, theirs %q", f.st.forgot, f.yt.videos[hubs].PublishAt, f.yt.videos[theirs].PublishAt)
	}
}

func TestARecheckBeforeTheSlot(t *testing.T) {
	due := func(f *fixture) {
		f.st.recheck = []store.RecheckDue{{ReleaseID: rel, ChannelID: channel, URL: shortsURL("abcdefghijk"), PublishAt: f.now.Add(20 * time.Minute)}}
		f.st.link = shortsURL("abcdefghijk")
		f.q.items = nil
	}
	// All is well: read once.
	f := newFixture(t)
	due(f)
	f.yt.videos["abcdefghijk"] = scheduled(f.now.Add(20 * time.Minute))
	f.pass()
	if len(f.st.rechecked) != 1 || f.st.hold != "" || len(f.say.said) != 0 {
		t.Errorf("fine: rechecked %v, hold %q, told %q", f.st.rechecked, f.st.hold, f.say.said)
	}
	// Rejected since the read-back at upload.
	f = newFixture(t)
	due(f)
	f.yt.videos["abcdefghijk"] = &video{UploadStatus: "rejected", RejectionReason: "duplicate", Privacy: "private"}
	f.pass()
	if !strings.Contains(f.st.hold, "duplicate") || len(f.st.forgot) != 1 {
		t.Errorf("rejected: hold %q, forgot %v", f.st.hold, f.st.forgot)
	}
	// Failed in processing.
	f = newFixture(t)
	due(f)
	f.yt.videos["abcdefghijk"] = &video{UploadStatus: "failed", Privacy: "private"}
	f.pass()
	if !strings.Contains(f.st.hold, "failed") {
		t.Errorf("failed: hold %q", f.st.hold)
	}
	// YouTube not answering: asked again next pass, not marked.
	f = newFixture(t)
	due(f)
	f.yt.videos["abcdefghijk"] = scheduled(f.now.Add(20 * time.Minute))
	f.p.yt = failingVideo{f.p.yt}
	f.pass()
	if len(f.st.rechecked) != 0 || f.st.hold != "" {
		t.Errorf("no answer: rechecked %v, hold %q", f.st.rechecked, f.st.hold)
	}
	// Not shown: said only once it stays so, tried anyway, and the record stays.
	f = newFixture(t)
	due(f)
	f.pass()
	if f.st.hold != "" {
		t.Fatalf("the first miss: hold %q", f.st.hold)
	}
	f.now = f.now.Add(goneConfirmAfter)
	f.st.recheck[0].PublishAt = f.now.Add(15 * time.Minute)
	f.pass()
	if !strings.Contains(f.st.hold, "не показывает") || len(f.st.forgot) != 0 || f.st.link == "" {
		t.Errorf("not shown: hold %q, forgot %v, link %q", f.st.hold, f.st.forgot, f.st.link)
	}
	if len(f.yt.unsched) == 0 || len(f.st.rechecked) != 1 {
		t.Errorf("not shown: unscheduled %v, rechecked %v", f.yt.unsched, f.st.rechecked)
	}
}

// A mistyped replacement link mustn't stop or forget hub's own video.
func TestARecheckReadsHubsOwnVideoAsWellAsTheLink(t *testing.T) {
	at := func(f *fixture) time.Time { return f.now.Add(20 * time.Minute) }
	setup := func(f *fixture) {
		slot := at(f)
		f.st.job = store.PublishJob{Saved: true, ReleaseID: rel, VideoID: "hubsvideo01", PublishAt: &slot, AnnouncedAt: &slot}
		f.st.recheck = []store.RecheckDue{{ReleaseID: rel, ChannelID: channel, URL: shortsURL("mistyped001"), PublishAt: slot}}
		f.st.link = shortsURL("mistyped001")
		f.q.items = nil
	}
	f := newFixture(t)
	setup(f)
	f.yt.videos["hubsvideo01"] = scheduled(at(f))
	f.pass()
	if f.st.hold != "" || len(f.st.forgot) != 0 || f.st.job.VideoID != "hubsvideo01" {
		t.Fatalf("hold %q, forgot %v, job %+v", f.st.hold, f.st.forgot, f.st.job)
	}
	f.now = f.now.Add(goneConfirmAfter)
	f.st.recheck[0].PublishAt = at(f)
	f.pass()
	if f.st.hold != "" || len(f.st.forgot) != 0 || len(f.st.rechecked) != 1 {
		t.Errorf("hold %q, forgot %v, rechecked %v", f.st.hold, f.st.forgot, f.st.rechecked)
	}
	if len(f.say.said) != 1 || !strings.Contains(f.say.said[0], "mistyped001") || !strings.Contains(f.say.said[0], "hubsvideo01") {
		t.Errorf("told %q", f.say.said)
	}
	// Hub's own rejected, the moderator's link fine: that one goes out.
	f = newFixture(t)
	setup(f)
	f.yt.videos["hubsvideo01"] = &video{UploadStatus: "rejected", RejectionReason: "duplicate", Privacy: "private"}
	f.yt.videos["mistyped001"] = scheduled(at(f))
	f.pass()
	if f.st.hold != "" || len(f.st.forgot) != 0 || len(f.st.rechecked) != 1 {
		t.Errorf("one fine: hold %q, forgot %v, rechecked %v", f.st.hold, f.st.forgot, f.st.rechecked)
	}
}

type failingVideo struct{ YouTube }

func (failingVideo) GetVideo(context.Context, string, string) (youtube.Video, error) {
	return youtube.Video{}, &youtube.Error{Retry: true, Message: "503"}
}

// Telegram down must not silence what the group has to hear.
func TestAnAlertWaitsForTelegram(t *testing.T) {
	// A lost access.
	f := newFixture(t)
	f.yt.revoked = true
	f.say.down = true
	f.pass()
	if len(f.st.alerted) != 0 || f.say.tries == 0 {
		t.Fatalf("marked while nobody was told: %v, tries %d", f.st.alerted, f.say.tries)
	}
	f.say.down = false
	f.pass()
	if len(f.st.alerted) != 1 || len(f.say.said) != 1 {
		t.Errorf("after Telegram came back: alerted %v, told %q", f.st.alerted, f.say.said)
	}
	// The poster warning.
	f = newFixture(t)
	f.yt.thumbErr = func(w http.ResponseWriter) bool { writeErr(w, 403, "forbidden"); return true }
	f.say.down = true
	f.pass()
	if f.st.link == "" || f.st.job.AnnouncedAt != nil {
		t.Fatalf("link %q, job %+v", f.st.link, f.st.job)
	}
	f.say.down = false
	f.due()
	f.pass()
	if len(f.say.said) != 1 || !strings.Contains(f.say.said[0], "Обложку") || f.st.job.AnnouncedAt == nil {
		t.Errorf("told %q, job %+v", f.say.said, f.st.job)
	}
	// A take-down's alert, and its report.
	f = newFixture(t)
	at := f.now.Add(5 * time.Minute)
	f.yt.videos["abcdefghijk"] = scheduled(at)
	f.closeIt(shortsURL("abcdefghijk"), at)
	f.yt.unschedErr = func(w http.ResponseWriter) bool { writeErr(w, 503, ""); return true }
	f.say.down = true
	f.pass()
	f.say.down = false
	f.pass()
	if len(f.say.said) != 1 {
		t.Errorf("the alert: told %q", f.say.said)
	}
	f.yt.unschedErr = nil
	f.say.down = true
	f.pass()
	if len(f.st.forgot) != 0 {
		t.Errorf("forgotten with nobody told: %v", f.st.forgot)
	}
	f.say.down = false
	f.pass()
	if len(f.st.forgot) != 1 || len(f.say.said) != 2 {
		t.Errorf("after Telegram came back: forgot %v, told %q", f.st.forgot, f.say.said)
	}
}

func TestARefusedClientIsOneAlertAndHoldsNothing(t *testing.T) {
	f := newFixture(t)
	f.yt.invalidClient = true
	f.pass()
	f.due()
	f.pass()
	if f.st.hold != "" || f.st.job.Attempts != 0 || len(f.st.alerted) != 0 {
		t.Errorf("hold %q, job %+v, alerted %v", f.st.hold, f.st.job, f.st.alerted)
	}
	if len(f.say.said) != 1 || !strings.Contains(f.say.said[0], "HUB_YOUTUBE_CLIENT_SECRET") {
		t.Errorf("told %q", f.say.said)
	}
	// The secret is fixed: the channel goes on without being connected again.
	f.yt.invalidClient = false
	f.pass()
	if f.st.link == "" {
		t.Errorf("after the fix: job %+v", f.st.job)
	}
}

func TestOnlyTheLockHolderWorks(t *testing.T) {
	f := newFixture(t)
	f.st.locked = true
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	f.p.Run(ctx)
	if len(f.yt.started) != 0 {
		t.Errorf("uploaded without the lock: %d", len(f.yt.started))
	}
}

func TestRunUploadsWithTheLock(t *testing.T) {
	f := newFixture(t)
	// Real time here: Run's clock isn't the fixture's to move.
	f.p.now = time.Now
	f.q.slot = time.Now().Add(6 * time.Hour).Truncate(time.Second).UTC()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { f.p.Run(ctx); close(done) }()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		f.yt.mu.Lock()
		up := len(f.yt.videos)
		f.yt.mu.Unlock()
		if up == 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	<-done
	if len(f.yt.videos) != 1 {
		t.Errorf("videos: %d", len(f.yt.videos))
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
	f.st.job = store.PublishJob{Saved: true, ReleaseID: rel, VideoID: "oldvideo001", PublishAt: &past}
	f.pass()
	if f.st.hold != "" || f.st.link != "https://www.youtube.com/shorts/oldvideo001" {
		t.Errorf("hold %q, link %q", f.st.hold, f.st.link)
	}
	if len(f.say.said) != 1 || strings.Contains(f.say.said[0], "выйдет") {
		t.Errorf("told %q", f.say.said)
	}
}

// One stuck take-down mustn't eat the quota others need.
func TestAStuckTakeDownFarFromItsSlotIsHourly(t *testing.T) {
	for name, stick := range map[string]func(*fixture){
		"not shown": func(*fixture) {},
		"failing": func(f *fixture) {
			f.yt.videos["abcdefghijk"] = scheduled(f.now.Add(24 * time.Hour))
			f.yt.unschedErr = func(w http.ResponseWriter) bool { writeErr(w, 503, ""); return true }
		},
	} {
		f := newFixture(t)
		at := f.now.Add(24 * time.Hour)
		f.closeIt(shortsURL("abcdefghijk"), at)
		stick(f)
		updates := 0
		f.p.yt = countingUnschedule{YouTube: f.p.yt, n: &updates}
		for at.Sub(f.now) > 2*time.Hour {
			f.pass()
			f.now = f.now.Add(time.Minute)
		}
		if updates < 20 || updates > 24 {
			t.Errorf("%s: %d updates in 22 hours, want about one an hour", name, updates)
		}
		far := updates
		for f.now.Before(at) {
			f.pass()
			f.now = f.now.Add(time.Minute)
		}
		// Quota: 10,000 units a day at 50 an update.
		if updates > 80 || updates-far < 30 {
			t.Errorf("%s: %d updates in all, %d in the last two hours", name, updates, updates-far)
		}
		if len(f.st.forgot) != 0 {
			t.Errorf("%s: forgot %v", name, f.st.forgot)
		}
	}
}

// The pace changes, not the promise: the group still hears within minutes.
func TestAHourlyTakeDownStillTellsOnTime(t *testing.T) {
	f := newFixture(t)
	at := f.now.Add(24 * time.Hour)
	f.yt.videos["abcdefghijk"] = scheduled(at)
	f.closeIt(shortsURL("abcdefghijk"), at)
	f.yt.unschedErr = func(w http.ResponseWriter) bool { writeErr(w, 503, ""); return true }
	f.pass()
	f.now = f.now.Add(9 * time.Minute)
	f.pass()
	if len(f.say.said) != 0 {
		t.Fatalf("too soon: %q", f.say.said)
	}
	f.now = f.now.Add(2 * time.Minute)
	f.pass()
	f.pass()
	if len(f.say.said) != 1 || !strings.Contains(f.say.said[0], "abcdefghijk") {
		t.Errorf("told %q", f.say.said)
	}

	g := newFixture(t)
	at = g.now.Add(24 * time.Hour)
	g.closeIt(shortsURL("abcdefghijk"), at)
	g.pass()
	g.now = g.now.Add(goneConfirmAfter)
	g.pass()
	if len(g.say.said) != 1 || !strings.Contains(g.say.said[0], "не показывает") {
		t.Errorf("not shown: told %q", g.say.said)
	}
}

func TestAHourlyWaitEndsWhenTheSlotGetsClose(t *testing.T) {
	f := newFixture(t)
	at := f.now.Add(2*time.Hour + 10*time.Minute)
	f.closeIt(shortsURL("abcdefghijk"), at)
	updates := 0
	f.p.yt = countingUnschedule{YouTube: f.p.yt, n: &updates}
	f.pass()
	f.now = f.now.Add(9 * time.Minute)
	f.pass()
	if updates != 1 {
		t.Errorf("before the two hours: %d updates", updates)
	}
	f.now = f.now.Add(2 * time.Minute)
	f.pass()
	if updates != 2 {
		t.Errorf("inside the two hours, it should try again: %d updates", updates)
	}
}

type countingUnschedule struct {
	YouTube
	n *int
}

func (c countingUnschedule) Unschedule(ctx context.Context, refresh, id string) error {
	*c.n++
	return c.YouTube.Unschedule(ctx, refresh, id)
}

// A moderator's video stands in for hub's: it goes out, and it stays on record.
func TestARefusedUploadKeepsAModeratorsLink(t *testing.T) {
	f := newFixture(t)
	f.yt.afterUpload = func(v *video) { v.UploadStatus, v.RejectionReason = "rejected", "duplicate" }
	f.st.link = shortsURL("moderators1")
	at := f.q.slot
	f.yt.videos["hubsvideo01"] = &video{UploadStatus: "rejected", RejectionReason: "duplicate", Privacy: "private"}
	f.st.job = store.PublishJob{Saved: true, ReleaseID: rel, VideoID: "hubsvideo01", PublishAt: &at}
	f.pass()
	if f.st.link != shortsURL("moderators1") || len(f.st.forgot) != 0 || f.st.hold != "" {
		t.Fatalf("link %q, forgot %v, hold %q", f.st.link, f.st.forgot, f.st.hold)
	}
	if f.st.job.VideoID != "" {
		t.Errorf("hub's refused upload is still on: %+v", f.st.job)
	}
	if len(f.say.said) != 1 || !strings.Contains(f.say.said[0], "duplicate") || !strings.Contains(f.say.said[0], "вручную") {
		t.Errorf("told %q", f.say.said)
	}
}
