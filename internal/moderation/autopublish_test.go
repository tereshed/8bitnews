package moderation

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/tereshed/8bitnews/internal/store"
)

func (s *fakeStore) ReleasesAwaitingDecision(context.Context) ([]store.Release, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.release.Status == "pending_approval" && s.release.Moderation.MessageID != nil {
		return []store.Release{s.release}, nil
	}
	return nil, nil
}

func (s *fakeStore) PublicationsToCheck(context.Context) ([]store.PublicationCheck, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.checks, nil
}

func (s *fakeStore) MarkPublicationOverdue(_ context.Context, id, _ string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.overdue = append(s.overdue, id)
	return nil
}

func (s *fakeStore) MarkPublicationClosedAlerted(_ context.Context, id, _ string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closedAlerted = append(s.closedAlerted, id)
	return nil
}

func (s *fakeStore) UploadAlerts(context.Context, time.Time) ([]store.UploadAlert, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.uploadAlerts, nil
}

func (s *fakeStore) MarkUploadAlerted(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.uploadAlerted = append(s.uploadAlerted, id)
	s.uploadAlerts = nil
	return nil
}

func (s *fakeStore) MarkHoldAlerted(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.holdAlerted = append(s.holdAlerted, id)
	s.uploadAlerts = nil
	return nil
}

const autoChannel = `{"moderation":{"approval_required":false}}`

func cardID(id int64) *int64 { return &id }

func TestAutoApproveApprovesOnAChannelWithoutModeration(t *testing.T) {
	m, tg, st, _ := newBot(t, "pending_approval")
	st.channelData = autoChannel
	st.release.Moderation.MessageID = cardID(77)
	st.release.CreatedAt = time.Now()

	m.AutoApprove(context.Background())

	if st.release.Status != "approved" || st.release.Moderation.By != store.AutoApprover {
		t.Fatalf("release = %s by %q, want approved by auto", st.release.Status, st.release.Moderation.By)
	}
	sent := tg.byMethod("sendMessage")
	if len(sent) != 1 || !strings.Contains(sent[0].params["text"].(string), "автоматически") {
		t.Errorf("the group wasn't told: %+v", sent)
	}
	if kb := tg.byMethod("editMessageReplyMarkup"); len(kb) != 1 {
		t.Errorf("the card's buttons didn't change: %+v", kb)
	}
	// Once only: it is no longer waiting.
	m.AutoApprove(context.Background())
	if sent := tg.byMethod("sendMessage"); len(sent) != 1 {
		t.Errorf("told the group twice: %d", len(sent))
	}
}

func TestAutoApproveLeavesThePersonInPlace(t *testing.T) {
	for name, data := range map[string]string{
		"approval required": `{"moderation":{"approval_required":true}}`,
		"no such field":     `{}`,
		"no profile":        ``,
	} {
		m, tg, st, _ := newBot(t, "pending_approval")
		st.channelData = data
		st.release.Moderation.MessageID = cardID(77)
		st.release.CreatedAt = time.Now()
		m.AutoApprove(context.Background())
		if st.release.Status != "pending_approval" {
			t.Errorf("%s: release = %s", name, st.release.Status)
		}
		if sent := tg.byMethod("sendMessage"); len(sent) != 0 {
			t.Errorf("%s: messages: %+v", name, sent)
		}
	}
}

func TestAutoApproveStopsWhenTheChannelCantBeRead(t *testing.T) {
	m, _, st, _ := newBot(t, "pending_approval")
	st.channelData = autoChannel
	st.failChannel = true
	st.release.Moderation.MessageID = cardID(77)
	st.release.CreatedAt = time.Now()
	m.AutoApprove(context.Background())
	if st.release.Status != "pending_approval" {
		t.Errorf("approved with the channel unreadable: %s", st.release.Status)
	}
}

func TestCheckPublicationsMarksAVideoThatIsUp(t *testing.T) {
	m, tg, st, _ := newBot(t, "approved")
	slot := time.Now().Add(-time.Minute)
	st.checks = []store.PublicationCheck{{ReleaseID: release, Status: "approved", URL: "https://www.youtube.com/shorts/abcdefghijk", CardMessageID: cardID(77), PublishAt: &slot}}
	m.SetVideoCheck(func(context.Context, string) (bool, bool) { return true, true })

	m.CheckPublications(context.Background())

	if st.release.Status != "published" {
		t.Fatalf("release = %s", st.release.Status)
	}
	sent := tg.byMethod("sendMessage")
	if len(sent) != 1 || !strings.Contains(sent[0].params["text"].(string), "abcdefghijk") {
		t.Errorf("the group wasn't told: %+v", sent)
	}
}

func TestCheckPublicationsIsQuietAboutAVideoFromBeforeSlots(t *testing.T) {
	m, tg, st, _ := newBot(t, "approved")
	st.checks = []store.PublicationCheck{{ReleaseID: release, Status: "approved", URL: "https://www.youtube.com/shorts/abcdefghijk", CardMessageID: cardID(77)}}
	m.SetVideoCheck(func(context.Context, string) (bool, bool) { return true, true })

	m.CheckPublications(context.Background())

	if st.release.Status != "published" {
		t.Fatalf("release = %s", st.release.Status)
	}
	if sent := tg.byMethod("sendMessage"); len(sent) != 0 {
		t.Errorf("a message per old video: %+v", sent)
	}
}

func TestCheckPublicationsAlertsOnceWhenAVideoMissesItsSlot(t *testing.T) {
	m, tg, st, _ := newBot(t, "approved")
	long, soon := time.Now().Add(-2*publicationOverdueAfter), time.Now().Add(-time.Minute)
	hidden := func(context.Context, string) (bool, bool) { return false, true }
	m.SetVideoCheck(hidden)

	st.checks = []store.PublicationCheck{{ReleaseID: release, Status: "approved", URL: "u", CardMessageID: cardID(77), PublishAt: &soon}}
	m.CheckPublications(context.Background())
	if sent := tg.byMethod("sendMessage"); len(sent) != 0 {
		t.Errorf("alerted before giving YouTube time: %+v", sent)
	}

	st.checks[0].PublishAt = &long
	m.CheckPublications(context.Background())
	sent := tg.byMethod("sendMessage")
	if len(sent) != 1 || !strings.Contains(sent[0].params["text"].(string), "не вышел") || len(st.overdue) != 1 {
		t.Fatalf("alert: %+v, overdue %v", sent, st.overdue)
	}

	st.checks[0].OverdueAlerted = true
	m.CheckPublications(context.Background())
	if sent := tg.byMethod("sendMessage"); len(sent) != 1 {
		t.Errorf("alerted twice: %d", len(sent))
	}
	if st.release.Status != "approved" {
		t.Errorf("a hidden video was marked: %s", st.release.Status)
	}
}

func TestCheckPublicationsWaitsWhenYouTubeDoesntAnswer(t *testing.T) {
	m, tg, st, _ := newBot(t, "approved")
	long := time.Now().Add(-2 * publicationOverdueAfter)
	st.checks = []store.PublicationCheck{{ReleaseID: release, Status: "approved", URL: "u", CardMessageID: cardID(77), PublishAt: &long}}
	m.SetVideoCheck(func(context.Context, string) (bool, bool) { return false, false })
	m.CheckPublications(context.Background())
	if st.release.Status != "approved" || len(tg.byMethod("sendMessage")) != 0 {
		t.Errorf("acted without an answer: %s, %+v", st.release.Status, tg.byMethod("sendMessage"))
	}
}

func TestAutoApproveLeavesAnOldCardToAPerson(t *testing.T) {
	m, tg, st, _ := newBot(t, "pending_approval")
	st.channelData = autoChannel
	st.release.Moderation.MessageID = cardID(77)
	st.release.CreatedAt = time.Now().Add(-autoApproveMaxAge - time.Hour)
	m.AutoApprove(context.Background())
	if st.release.Status != "pending_approval" || len(tg.byMethod("sendMessage")) != 0 {
		t.Errorf("a stale card was approved: %s", st.release.Status)
	}
}

func TestCheckPublicationsAlertsWhenAClosedOrHeldVideoGoesOut(t *testing.T) {
	slot := time.Now().Add(-time.Minute)
	for name, row := range map[string]store.PublicationCheck{
		"withdrawn": {ReleaseID: release, Status: "rejected", URL: "https://www.youtube.com/shorts/abcdefghijk", CardMessageID: cardID(77), PublishAt: &slot},
		"held":      {ReleaseID: release, Status: "approved", Held: true, URL: "https://www.youtube.com/shorts/abcdefghijk", CardMessageID: cardID(77), PublishAt: &slot},
	} {
		m, tg, st, _ := newBot(t, row.Status)
		st.checks = []store.PublicationCheck{row}
		m.SetVideoCheck(func(context.Context, string) (bool, bool) { return false, true })
		m.CheckPublications(context.Background())
		if len(tg.byMethod("sendMessage")) != 0 {
			t.Errorf("%s: alerted while it stays down", name)
		}

		m.SetVideoCheck(func(context.Context, string) (bool, bool) { return true, true })
		m.CheckPublications(context.Background())
		sent := tg.byMethod("sendMessage")
		if len(sent) != 1 || !strings.Contains(sent[0].params["text"].(string), "снять") || len(st.closedAlerted) != 1 {
			t.Fatalf("%s: alert: %+v, marked %v", name, sent, st.closedAlerted)
		}
		if st.release.Status != row.Status {
			t.Errorf("%s: release moved to %s", name, st.release.Status)
		}

		st.checks[0].ClosedAlerted = true
		m.CheckPublications(context.Background())
		if sent := tg.byMethod("sendMessage"); len(sent) != 1 {
			t.Errorf("%s: alerted twice: %d", name, len(sent))
		}

		// A warning that it hadn't come out doesn't use up the one that it has.
		st.checks[0].ClosedAlerted, st.checks[0].OverdueAlerted = false, true
		m.CheckPublications(context.Background())
		if sent := tg.byMethod("sendMessage"); len(sent) != 2 {
			t.Errorf("%s: an earlier overdue alert silenced this one: %d", name, len(sent))
		}
	}
}

func TestCheckUploadsTellsTheGroupOnce(t *testing.T) {
	for name, c := range map[string]struct {
		alert              store.UploadAlert
		want, not          string
		uploadMark, holdMk int
	}{
		"stalled": {alert: store.UploadAlert{ReleaseID: release, CardMessageID: cardID(77), ApprovedAt: time.Now().Add(-7 * time.Hour)},
			want: "загрузчик", uploadMark: 1},
		"held, nothing on YouTube": {alert: store.UploadAlert{ReleaseID: release, CardMessageID: cardID(77), ApprovedAt: time.Now(), Hold: "the vote is out"},
			want: "Сам он не выйдет", not: "Studio", holdMk: 1},
		"held, maybe scheduled": {alert: store.UploadAlert{ReleaseID: release, CardMessageID: cardID(77), ApprovedAt: time.Now(), Hold: "the vote is out", MayBeScheduled: true},
			want: "Studio", not: "Сам он не выйдет", holdMk: 1},
	} {
		m, tg, st, _ := newBot(t, "approved")
		st.uploadAlerts = []store.UploadAlert{c.alert}
		m.CheckUploads(context.Background())
		sent := tg.byMethod("sendMessage")
		if len(sent) != 1 {
			t.Fatalf("%s: alert: %+v", name, sent)
		}
		text := sent[0].params["text"].(string)
		if !strings.Contains(text, c.want) || (c.not != "" && strings.Contains(text, c.not)) {
			t.Errorf("%s: %q", name, text)
		}
		if c.alert.Hold != "" && !strings.Contains(text, c.alert.Hold) {
			t.Errorf("%s: no reason in %q", name, text)
		}
		if len(st.uploadAlerted) != c.uploadMark || len(st.holdAlerted) != c.holdMk {
			t.Errorf("%s: recorded stalls %v, holds %v", name, st.uploadAlerted, st.holdAlerted)
		}
		m.CheckUploads(context.Background())
		if sent := tg.byMethod("sendMessage"); len(sent) != 1 {
			t.Errorf("%s: told twice: %d", name, len(sent))
		}
	}
}

func TestAHoldReasonFitsATelegramMessageWhateverItsLength(t *testing.T) {
	text := holdText(store.UploadAlert{Hold: strings.Repeat("🗳", 5000), MayBeScheduled: true}, false)
	if utf16Len(text) > messageLimit || !strings.Contains(text, "Studio") {
		t.Errorf("%d units, limit %d", utf16Len(text), messageLimit)
	}
}

// Nobody has to say the video is up; saying so early would list it on the channel.
func TestAnAutomaticCardHasNoPublishedButton(t *testing.T) {
	m, tg, st, _ := newBot(t, "pending_approval")
	st.channelData = autoChannel
	st.release.Moderation.MessageID = cardID(77)
	st.release.CreatedAt = time.Now()
	m.AutoApprove(context.Background())
	kb := tg.byMethod("editMessageReplyMarkup")
	if len(kb) != 1 {
		t.Fatalf("keyboard calls: %+v", kb)
	}
	text := fmt.Sprint(kb[0].params["reply_markup"])
	if strings.Contains(text, "🟢") || !strings.Contains(text, "🚫") {
		t.Errorf("keyboard: %s", text)
	}
}

func TestWhatTheGroupIsToldDependsOnWhoTakesAVideoDown(t *testing.T) {
	for _, c := range []struct {
		name, text string
		says, not  string
	}{
		{"withdrawn, browser", withdrawnReply("K", false), "снимите вручную", "hub попробует"},
		{"withdrawn, hub", withdrawnReply("K", true), "hub попробует", "до слота"},
		{"approved, browser", autoApprovedReply(false), "снять в Studio", "hub попробует"},
		{"approved, hub", autoApprovedReply(true), "hub попробует", "Studio"},
		{"held, browser", holdText(store.UploadAlert{Hold: "x", MayBeScheduled: true}, false), "снимите его в Studio", "hub попробует"},
		{"held, hub", holdText(store.UploadAlert{Hold: "x", MayBeScheduled: true}, true), "hub попробует", "Studio"},
		{"held, nothing up", holdText(store.UploadAlert{Hold: "x"}, true), "Сам он не выйдет", "снять"},
	} {
		if !strings.Contains(c.text, c.says) || strings.Contains(c.text, c.not) {
			t.Errorf("%s: %q", c.name, c.text)
		}
	}
}

func TestAutoApprovalSaysWhoTakesAVideoDownOnThisChannel(t *testing.T) {
	m, tg, st, _ := newBot(t, "pending_approval")
	st.channelData = autoChannel
	st.release.Moderation.MessageID = cardID(77)
	st.release.CreatedAt = time.Now()
	m.SetUploaderCheck(func(context.Context, string) bool { return true })
	m.AutoApprove(context.Background())
	if sent := tg.byMethod("sendMessage"); len(sent) != 1 || !strings.Contains(sent[0].params["text"].(string), "hub попробует") {
		t.Errorf("sent %+v", sent)
	}
}

func TestNotifyGoesOnTheCardWhenThereIsOne(t *testing.T) {
	m, tg, _, _ := newBot(t, "approved")
	if err := m.Notify(context.Background(), cardID(77), "uploaded"); err != nil {
		t.Fatal(err)
	}
	if err := m.Notify(context.Background(), nil, strings.Repeat("🗳", 5000)); err != nil {
		t.Fatal(err)
	}
	sent := tg.byMethod("sendMessage")
	if len(sent) != 2 || !strings.Contains(fmt.Sprint(sent[0].params["reply_parameters"]), "message_id:77") {
		t.Fatalf("sent %+v", sent)
	}
	if _, onCard := sent[1].params["reply_parameters"]; onCard {
		t.Errorf("no card, yet a reply: %+v", sent[1].params)
	}
	if utf16Len(sent[1].params["text"].(string)) > messageLimit {
		t.Error("over Telegram's limit")
	}
}
