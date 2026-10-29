package moderation

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"unicode/utf16"

	"github.com/tereshed/8bitnews/internal/store"
)

// Telegram's limits: 1024 characters for a media caption and 4096 for a
// message, counted in UTF-16 units (an emoji takes two).
const (
	captionLimit = 1024
	messageLimit = 4096
)

// Callback actions on the card's buttons, and the release statuses they set.
var actions = map[string]string{
	"approve": "approved",
	"changes": "changes_requested",
	"reject":  "rejected",
}

const callbackPrefix = "mod:"

func callbackData(action, releaseID string) string {
	return callbackPrefix + action + ":" + releaseID
}

// parseCallback reads "mod:<action>:<release id>" back.
func parseCallback(data string) (action, releaseID string, ok bool) {
	rest, found := strings.CutPrefix(data, callbackPrefix)
	if !found {
		return "", "", false
	}
	action, releaseID, found = strings.Cut(rest, ":")
	if _, known := actions[action]; !found || !known || releaseID == "" {
		return "", "", false
	}
	return action, releaseID, true
}

func cardKeyboard(releaseID string) *Keyboard {
	return &Keyboard{InlineKeyboard: [][]Button{
		{{Text: "✅ Опубликовать", CallbackData: callbackData("approve", releaseID)}},
		{
			{Text: "✏️ Доработать", CallbackData: callbackData("changes", releaseID)},
			{Text: "❌ Отклонить", CallbackData: callbackData("reject", releaseID)},
		},
	}}
}

// The parts of the release's stored contracts the card shows.
type videoPackage struct {
	DurationS float64 `json:"duration_s"`
	Template  string  `json:"template"`
	Anchor    struct {
		ID string `json:"id"`
	} `json:"anchor"`
}

type qaReport struct {
	Verdict string `json:"verdict"`
	Issues  []struct {
		Severity string `json:"severity"`
		SceneID  string `json:"scene_id"`
		Finding  string `json:"finding"`
		FixOwner string `json:"fix_owner"`
	} `json:"issues"`
}

type legalReview struct {
	Verdict      string   `json:"verdict"`
	PublishNotes []string `json:"publish_notes"`
	Issues       []struct {
		Severity string `json:"severity"`
		Fix      string `json:"fix"`
	} `json:"issues"`
	Platforms map[string]struct {
		Verdict string `json:"verdict"`
	} `json:"platforms"`
}

func decode[T any](raw json.RawMessage) T {
	var v T
	_ = json.Unmarshal(raw, &v) // a missing or odd report shows as empty, not as no card
	return v
}

// Card is what the bot posts for a release: the video's caption and a
// follow-up message with the reviewers' notes.
type Card struct {
	Caption string
	Details string
}

// BuildCard writes the card from a release, its story and its sources.
func BuildCard(r store.Release, c store.ReleaseContext, sources []store.ReleaseSource) Card {
	vp := decode[videoPackage](r.VideoPackage)
	qa := decode[qaReport](r.ValidationReport)
	legal := decode[legalReview](r.LegalReview)

	var b strings.Builder
	fmt.Fprintf(&b, "🎬 %s\n", truncate(c.StoryTitle, 200))
	if c.DevelopmentSummary != "" {
		fmt.Fprintf(&b, "%s\n", truncate(c.DevelopmentSummary, 300))
	}
	b.WriteString("\n")
	var facts []string
	if vp.DurationS > 0 {
		facts = append(facts, fmt.Sprintf("%.0f с", vp.DurationS))
	}
	if vp.Anchor.ID != "" {
		facts = append(facts, "ведущий "+truncate(vp.Anchor.ID, 40))
	}
	if vp.Template != "" {
		facts = append(facts, "шаблон "+truncate(vp.Template, 40))
	}
	if len(facts) > 0 {
		fmt.Fprintf(&b, "⏱ %s\n", strings.Join(facts, " · "))
	}
	platforms := "нет площадок"
	if len(r.AllowedPlatforms) > 0 {
		platforms = truncate(strings.Join(r.AllowedPlatforms, ", "), 80)
	}
	fmt.Fprintf(&b, "📺 %s\n", platforms)
	fmt.Fprintf(&b, "🧪 QA: %s%s\n", truncate(orDash(qa.Verdict), 40), countBySeverity(len(qa.Issues), func(i int) string { return qa.Issues[i].Severity }, "замечаний"))
	fmt.Fprintf(&b, "⚖️ Legal: %s%s", truncate(orDash(legal.Verdict), 40), countBySeverity(len(legal.Issues), func(i int) string { return legal.Issues[i].Severity }, "замечаний"))
	if len(legal.PublishNotes) > 0 {
		fmt.Fprintf(&b, " · заметок к публикации %d", len(legal.PublishNotes))
	}
	b.WriteString("\n")
	archived := 0
	for _, s := range sources {
		if s.ArchiveURL != "" {
			archived++
		}
	}
	fmt.Fprintf(&b, "🔗 Источников %d, в архиве %d\n", len(sources), archived)

	var d strings.Builder
	if len(legal.PublishNotes) > 0 {
		d.WriteString("⚖️ Юристы — заметки к публикации:\n")
		for _, n := range legal.PublishNotes {
			fmt.Fprintf(&d, "• %s\n", truncate(n, 400))
		}
	}
	if len(qa.Issues) > 0 {
		if d.Len() > 0 {
			d.WriteString("\n")
		}
		d.WriteString("🧪 QA — замечания:\n")
		for _, i := range qa.Issues {
			where := i.SceneID
			if where == "" {
				where = "ролик"
			}
			fmt.Fprintf(&d, "• [%s] %s: %s\n", orDash(i.Severity), where, truncate(i.Finding, 300))
		}
	}
	// The release id closes the caption whatever gets cut above it.
	tail := "\nРелиз " + r.ID
	return Card{
		Caption: truncate(b.String(), captionLimit-utf16Len(tail)) + tail,
		Details: truncate(strings.TrimSpace(d.String()), messageLimit),
	}
}

// countBySeverity renders " · замечаний 5 (minor 4, major 1)", or "" for none.
func countBySeverity(n int, severity func(int) string, noun string) string {
	if n == 0 {
		return ""
	}
	by := map[string]int{}
	for i := 0; i < n; i++ {
		by[truncate(orDash(severity(i)), 20)]++
	}
	names := make([]string, 0, len(by))
	for s := range by {
		names = append(names, s)
	}
	sort.Strings(names)
	parts := make([]string, len(names))
	for i, s := range names {
		parts[i] = fmt.Sprintf("%s %d", s, by[s])
	}
	return fmt.Sprintf(" · %s %d (%s)", noun, n, strings.Join(parts, ", "))
}

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

func utf16Len(s string) int {
	return len(utf16.Encode([]rune(s)))
}

// truncate cuts s to at most limit UTF-16 units, Telegram's measure,
// marking the cut with "…".
func truncate(s string, limit int) string {
	if utf16Len(s) <= limit {
		return s
	}
	n := 1 // the "…"
	var b strings.Builder
	for _, r := range s {
		if n += utf16.RuneLen(r); n > limit {
			break
		}
		b.WriteRune(r)
	}
	return b.String() + "…"
}

// statusText is a release status as the group reads it.
func statusText(status string) string {
	switch status {
	case "submitted":
		return "ждёт карточки"
	case "pending_approval":
		return "ждёт решения"
	case "approved":
		return "одобрен"
	case "rejected":
		return "отклонён"
	case "changes_requested":
		return "отправлен на доработку"
	case "scheduled":
		return "в очереди на публикацию"
	case "published":
		return "опубликован"
	case "failed":
		return "сбой"
	}
	return status
}
