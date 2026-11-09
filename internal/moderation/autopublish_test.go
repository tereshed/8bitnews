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

const autoChannel = `{"moderation":{"approval_required":false}}`

func cardID(id int64) *int64 { return &id }

func TestAutoApproveApprovesOnAChannelWithoutModeration(t *testing.T) {
	m, tg, st, _ := newBot(t, "pending_approval")
	st.channelData = autoChannel
	st.release.Moderation.MessageID = cardID(77)

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
	m.AutoApprove(context.Background())
	if st.release.Status != "pending_approval" {
		t.Errorf("approved with the channel unreadable: %s", st.release.Status)
	}
}

func TestCheckPublicationsMarksAVideoThatIsUp(t *testing.T) {
	m, tg, st, _ := newBot(t, "approved")
	slot := time.Now().Add(-time.Minute)
	st.checks = []store.PublicationCheck{{ReleaseID: release, URL: "https://www.youtube.com/shorts/abcdefghijk", CardMessageID: cardID(77), PublishAt: &slot}}
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
	st.checks = []store.PublicationCheck{{ReleaseID: release, URL: "https://www.youtube.com/shorts/abcdefghijk", CardMessageID: cardID(77)}}
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

	st.checks = []store.PublicationCheck{{ReleaseID: release, URL: "u", CardMessageID: cardID(77), PublishAt: &soon}}
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
	st.checks = []store.PublicationCheck{{ReleaseID: release, URL: "u", CardMessageID: cardID(77), PublishAt: &long}}
	m.SetVideoCheck(func(context.Context, string) (bool, bool) { return false, false })
	m.CheckPublications(context.Background())
	if st.release.Status != "approved" || len(tg.byMethod("sendMessage")) != 0 {
		t.Errorf("acted without an answer: %s, %+v", st.release.Status, tg.byMethod("sendMessage"))
	}
}
