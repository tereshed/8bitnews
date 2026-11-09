package moderation

import (
	"context"
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

func (s *fakeStore) StalledUploads(context.Context, time.Time) ([]store.StalledUpload, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stalled, nil
}

func (s *fakeStore) MarkUploadAlerted(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.uploadAlerted = append(s.uploadAlerted, id)
	s.stalled = nil
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
		if len(sent) != 1 || !strings.Contains(sent[0].params["text"].(string), "снять") || len(st.overdue) != 1 {
			t.Fatalf("%s: alert: %+v, marked %v", name, sent, st.overdue)
		}
		if st.release.Status != row.Status {
			t.Errorf("%s: release moved to %s", name, st.release.Status)
		}

		st.checks[0].OverdueAlerted = true
		m.CheckPublications(context.Background())
		if sent := tg.byMethod("sendMessage"); len(sent) != 1 {
			t.Errorf("%s: alerted twice: %d", name, len(sent))
		}
	}
}

func TestCheckStalledUploadsTellsTheGroupOnce(t *testing.T) {
	m, tg, st, _ := newBot(t, "approved")
	st.stalled = []store.StalledUpload{{ReleaseID: release, CardMessageID: cardID(77), ApprovedAt: time.Now().Add(-7 * time.Hour)}}
	m.CheckStalledUploads(context.Background())
	sent := tg.byMethod("sendMessage")
	if len(sent) != 1 || !strings.Contains(sent[0].params["text"].(string), "загрузчик") {
		t.Fatalf("alert: %+v", sent)
	}
	if len(st.uploadAlerted) != 1 {
		t.Errorf("not recorded: %v", st.uploadAlerted)
	}
	m.CheckStalledUploads(context.Background())
	if sent := tg.byMethod("sendMessage"); len(sent) != 1 {
		t.Errorf("told twice: %d", len(sent))
	}
}
