package publish

import (
	"encoding/json"
	"html"
	"strings"
	"testing"
	"time"

	"github.com/tereshed/8bitnews/internal/store"
)

func TestApprovalRequiredOnlyAnExplicitFalseRemovesThePerson(t *testing.T) {
	for _, c := range []struct {
		data string
		want bool
	}{
		{`{"moderation":{"approval_required":false}}`, false},
		{`{"moderation":{"approval_required":true}}`, true},
		{`{"moderation":{}}`, true},
		{`{}`, true},
		{``, true},
		{`not json`, true},
		{`{"moderation":{"approval_required":"false"}}`, true},
	} {
		if got := ApprovalRequired(store.Channel{ID: "c", Data: json.RawMessage(c.data)}); got != c.want {
			t.Errorf("ApprovalRequired(%s) = %v, want %v", c.data, got, c.want)
		}
	}
}

func TestNextSlot(t *testing.T) {
	ny, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	at := func(y int, m time.Month, d, h, min int) time.Time { return time.Date(y, m, d, h, min, 0, 0, ny) }
	slots := []string{"18:00", "12:00"} // the main slot first

	cases := []struct {
		name      string
		notBefore time.Time
		taken     []time.Time
		want      time.Time
	}{
		{"the main slot, even with an earlier one free", at(2026, 11, 9, 8, 0), nil, at(2026, 11, 9, 18, 0)},
		{"exactly at a slot counts", at(2026, 11, 9, 18, 0), nil, at(2026, 11, 9, 18, 0)},
		{"the second slot once the main one is taken", at(2026, 11, 9, 8, 0), []time.Time{at(2026, 11, 9, 18, 0)}, at(2026, 11, 9, 12, 0)},
		{"never one already gone", at(2026, 11, 9, 13, 0), []time.Time{at(2026, 11, 9, 18, 0)}, at(2026, 11, 10, 18, 0)},
		{"tomorrow", at(2026, 11, 9, 18, 1), nil, at(2026, 11, 10, 18, 0)},
		{"a full day moves on", at(2026, 11, 9, 8, 0), []time.Time{at(2026, 11, 9, 12, 0), at(2026, 11, 9, 18, 0)}, at(2026, 11, 10, 18, 0)},
		{"across the end of a month", at(2026, 11, 30, 19, 0), nil, at(2026, 12, 1, 18, 0)},
		// Clocks went back on 1 Nov 2026: 18:00 local stays 18:00 local.
		{"over the clock change", at(2026, 10, 31, 19, 0), nil, at(2026, 11, 1, 18, 0)},
	}
	for _, c := range cases {
		taken := map[int64]bool{}
		for _, x := range c.taken {
			taken[x.Unix()] = true
		}
		got, err := NextSlot(c.notBefore.UTC(), "America/New_York", slots, taken)
		if err != nil || !got.Equal(c.want) {
			t.Errorf("%s: NextSlot = %v, %v; want %v", c.name, got, err, c.want)
		}
	}
}

func TestNextSlotRefusesABadProfile(t *testing.T) {
	now := time.Date(2026, 11, 9, 8, 0, 0, 0, time.UTC)
	if _, err := NextSlot(now, "America/New_York", nil, nil); err == nil {
		t.Error("no slots: no error")
	}
	if _, err := NextSlot(now, "America/New_York", []string{"6pm"}, nil); err == nil {
		t.Error("a slot that isn't a time: no error")
	}
	if _, err := NextSlot(now, "Mars/Olympus", []string{"18:00"}, nil); err == nil {
		t.Error("an unknown time zone: no error")
	}
	if _, err := NextSlot(now, "", []string{"18:00"}, nil); err == nil {
		t.Error("no time zone: no error")
	}
}

// The page says what is true of this video: that a person approved it, or
// that nobody looked at it first. Both languages, and never the other sentence.
func TestSourcesPageSaysWhoLookedAtTheVideo(t *testing.T) {
	for _, lang := range []string{"en-US", "es-ES"} {
		w := wordsFor(lang)
		for _, auto := range []bool{true, false} {
			var b strings.Builder
			if err := sourcesTemplate.Execute(&b, Page{T: w, Channel: "C", Title: "T", AutoApproved: auto}); err != nil {
				t.Fatal(err)
			}
			want, not := w.HowReviewHuman, w.HowReviewAuto
			if auto {
				want, not = not, want
			}
			page := html.UnescapeString(b.String())
			if want == "" || not == "" || !strings.Contains(page, want) || strings.Contains(page, not) {
				t.Errorf("%s auto=%v: want %q, not %q", lang, auto, want, not)
			}
		}
	}
}
