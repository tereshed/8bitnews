package moderation

import (
	"context"
	"fmt"
	"time"

	"github.com/tereshed/8bitnews/internal/publish"
	"github.com/tereshed/8bitnews/internal/store"
)

// Publishing without a per-video approval (docs/automation.md): on a channel
// with moderation.approval_required: false a card goes out as usual, for the
// record and for «🚫», and the release is approved at once. Hub then watches
// the platform and moves the release to "published" when the video is up.

const (
	publicationCheckEvery = 2 * time.Minute
	// YouTube flips a scheduled video within minutes; past this the group is told.
	publicationOverdueAfter = 30 * time.Minute
)

// SetVideoCheck makes hub look at the platform to learn when a video is up.
// known is false when the platform didn't answer.
func (m *Moderator) SetVideoCheck(check func(ctx context.Context, url string) (visible, known bool)) {
	m.videoVisible = check
}

// AutoApprove approves the releases whose card is out, on the channels that
// ask for no moderator. It also picks up what a restart interrupted between
// the card and the approval, and cards posted before a channel changed over.
func (m *Moderator) AutoApprove(ctx context.Context) {
	waiting, err := m.store.ReleasesAwaitingDecision(ctx)
	if err != nil {
		m.log.Error("moderation: listing releases awaiting a decision", "error", err)
		return
	}
	for _, r := range waiting {
		ch, err := m.store.GetChannel(ctx, r.ChannelID)
		if err != nil {
			m.log.Error("moderation: reading the channel of a release", "release_id", r.ID, "error", err)
			continue
		}
		if publish.ApprovalRequired(ch) {
			continue
		}
		approved, changed, err := m.store.DecideRelease(ctx, r.ID, "approved", store.AutoApprover)
		if err != nil {
			m.log.Error("moderation: approving a release automatically", "release_id", r.ID, "error", err)
			continue
		}
		if !changed {
			continue
		}
		m.event(ctx, map[string]any{"release_id": r.ID, "action": "approved", "by": store.AutoApprover})
		if approved.Moderation.MessageID == nil {
			continue
		}
		cardID := *approved.Moderation.MessageID
		m.setKeyboard(ctx, r.ID, cardID, approvedKeyboard(r.ID, false))
		if _, err := m.tg.SendMessage(ctx, m.chatID, autoApprovedReply, cardID, nil); err != nil {
			m.log.Error("moderation: telling the group about an automatic approval failed", "release_id", r.ID, "error", err)
		}
	}
}

const autoApprovedReply = "🤖 Одобрено автоматически: на этом канале подтверждение выключено. Ролик встанет в ближайший слот. " +
	"Остановить — «🚫 Закрыть страницу источников»."

// CheckPublications moves a release to "published" once its video is up, and
// tells the group once if a video hasn't gone up after its slot.
func (m *Moderator) CheckPublications(ctx context.Context) {
	if m.videoVisible == nil {
		return
	}
	checks, err := m.store.PublicationsToCheck(ctx)
	if err != nil {
		m.log.Error("moderation: listing publications to check", "error", err)
		return
	}
	for _, c := range checks {
		visible, known := m.videoVisible(ctx, c.URL)
		switch {
		case !known:
		case visible:
			m.markLive(ctx, c)
		case c.PublishAt != nil && !c.OverdueAlerted && time.Since(*c.PublishAt) > publicationOverdueAfter:
			m.alertOverdue(ctx, c)
		}
	}
}

func (m *Moderator) markLive(ctx context.Context, c store.PublicationCheck) {
	r, changed, err := m.store.SetPublished(ctx, c.ReleaseID, true)
	if err != nil {
		m.log.Error("moderation: recording a publication failed", "release_id", c.ReleaseID, "error", err)
		return
	}
	if !changed {
		return
	}
	m.event(ctx, map[string]any{"release_id": r.ID, "action": "published", "by": store.AutoApprover, "url": c.URL})
	if c.CardMessageID == nil {
		return
	}
	m.setKeyboard(ctx, r.ID, *c.CardMessageID, approvedKeyboard(r.ID, true))
	// A video that went out at its slot is news; one uploaded before slots were
	// recorded has been up for a while, and the group has seen it.
	if c.PublishAt == nil {
		return
	}
	if _, err := m.tg.SendMessage(ctx, m.chatID, "🟢 Вышел: "+c.URL, *c.CardMessageID, nil); err != nil {
		m.log.Error("moderation: telling the group about a publication failed", "release_id", r.ID, "error", err)
	}
}

func (m *Moderator) alertOverdue(ctx context.Context, c store.PublicationCheck) {
	text := fmt.Sprintf("⚠️ Ролик не вышел: слот был %s UTC, а YouTube его не показывает. %s",
		c.PublishAt.UTC().Format("02.01 15:04"), c.URL)
	var cardID int64
	if c.CardMessageID != nil {
		cardID = *c.CardMessageID
	}
	if _, err := m.tg.SendMessage(ctx, m.chatID, text, cardID, nil); err != nil {
		m.log.Error("moderation: telling the group about an overdue video failed", "release_id", c.ReleaseID, "error", err)
		return
	}
	if err := m.store.MarkPublicationOverdue(ctx, c.ReleaseID, store.PlatformYouTube); err != nil {
		m.log.Error("moderation: marking a video overdue failed", "release_id", c.ReleaseID, "error", err)
	}
	m.event(ctx, map[string]any{"release_id": c.ReleaseID, "action": "overdue", "url": c.URL})
}
