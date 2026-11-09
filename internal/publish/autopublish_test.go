package publish

import (
	"encoding/json"
	"html"
	"os"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"

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
	slots := SlotRules{Slots: []string{"18:00", "12:00"}} // the main slot first

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
	if _, err := NextSlot(now, "America/New_York", SlotRules{}, nil); err == nil {
		t.Error("no slots: no error")
	}
	if _, err := NextSlot(now, "America/New_York", SlotRules{Slots: []string{"6pm"}}, nil); err == nil {
		t.Error("a slot that isn't a time: no error")
	}
	if _, err := NextSlot(now, "Mars/Olympus", SlotRules{Slots: []string{"18:00"}}, nil); err == nil {
		t.Error("an unknown time zone: no error")
	}
	if _, err := NextSlot(now, "", SlotRules{Slots: []string{"18:00"}}, nil); err == nil {
		t.Error("no time zone: no error")
	}
	if _, err := NextSlot(now, "UTC", SlotRules{Slots: []string{"18:00"}, Blackout: []string{"28.11"}}, nil); err == nil {
		t.Error("a blackout date that isn't a date: no error")
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

// Legal's notes keep forbidding the reflection day and election day.
func TestNextSlotSkipsBlackoutDates(t *testing.T) {
	madrid, err := time.LoadLocation("Europe/Madrid")
	if err != nil {
		t.Fatal(err)
	}
	rules := SlotRules{Slots: []string{"21:00", "14:00"}, Blackout: []string{"2026-11-28", "2026-11-29"}}
	got, err := NextSlot(time.Date(2026, 11, 27, 21, 30, 0, 0, madrid), "Europe/Madrid", rules, nil)
	want := time.Date(2026, 11, 30, 21, 0, 0, 0, madrid)
	if err != nil || !got.Equal(want) {
		t.Errorf("NextSlot = %v, %v; want %v", got, err, want)
	}
	// A blackout date is local: 23:30 UTC on the 27th is already the 28th in Madrid.
	rules = SlotRules{Slots: []string{"00:30"}, Blackout: []string{"2026-11-28"}}
	got, err = NextSlot(time.Date(2026, 11, 27, 22, 0, 0, 0, time.UTC), "Europe/Madrid", rules, nil)
	want = time.Date(2026, 11, 29, 0, 30, 0, 0, madrid)
	if err != nil || !got.Equal(want) {
		t.Errorf("a local date: NextSlot = %v, %v; want %v", got, err, want)
	}
}

// A video with a condition no one rechecks goes out at the first chance.
func TestNextSlotSoonestGoesByTheClock(t *testing.T) {
	ny, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	notBefore := time.Date(2026, 11, 9, 8, 0, 0, 0, ny)
	rules := SlotRules{Slots: []string{"18:00", "12:00"}}
	if got, _ := NextSlot(notBefore, "America/New_York", rules, nil); got.Hour() != 18 {
		t.Errorf("without a stop note: %v, want the main slot", got)
	}
	rules.Soonest = true
	got, err := NextSlot(notBefore, "America/New_York", rules, nil)
	if err != nil || !got.Equal(time.Date(2026, 11, 9, 12, 0, 0, 0, ny)) {
		t.Errorf("with a stop note: %v, %v; want 12:00", got, err)
	}
	taken := map[int64]bool{time.Date(2026, 11, 9, 12, 0, 0, 0, ny).Unix(): true}
	if got, _ := NextSlot(notBefore, "America/New_York", rules, taken); got.Hour() != 18 || got.Day() != 9 {
		t.Errorf("12:00 taken: %v, want 18:00 the same day", got)
	}
}

func TestHasStopNote(t *testing.T) {
	for notes, want := range map[string]bool{
		"СТОП: if deaths are confirmed":  true,
		"  СТОП: leading space":          true,
		"Set the AI flags":               false,
		"Stop if deaths are confirmed":   false,
		"see СТОП: in the middle of one": false,
	} {
		if got := HasStopNote([]string{"first note", notes}); got != want {
			t.Errorf("HasStopNote(%q) = %v", notes, got)
		}
	}
	if HasStopNote(nil) {
		t.Error("no notes")
	}
}

// Nobody edits a description by hand any more: the marks Legal kept asking for
// have to come from the profile (countries/es/sources.yaml).
func TestSpanishStateMediaGetTheirMarkFromTheProfile(t *testing.T) {
	raw, err := os.ReadFile("../../countries/es/sources.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var decoded any
	if err := yaml.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(decoded)
	if err != nil {
		t.Fatal(err)
	}
	rows := []store.ReleaseSource{
		{URL: "https://efe.com/a", Outlet: "EFE"},
		{URL: "https://efeverde.com/b", Outlet: "EFE Verde"},
		{URL: "https://www.rtve.es/c", Outlet: "RTVE"},
		{URL: "https://www.infobae.com/d", Outlet: "EFE (via Infobae)"},
		{URL: "https://www.infobae.com/e", Outlet: "Infobae"},
		{URL: "https://www.example.org/f", Outlet: "EFEMÉRIDES"},
		{URL: "https://www.infobae.com/g", Outlet: "Infobae (EFE)"},
		{URL: "https://www.example.org/h", Outlet: "Agencia Efe"},
		{URL: "https://es.yahoo.com/i", Outlet: "RTVE (via Yahoo)"},
		{URL: "https://www.example.org/j", Outlet: "EFE EME"},
		{URL: "https://www.example.org/k", Outlet: "Yahoo via rtve"},
		{URL: "https://www.example.org/l", Outlet: "EFE Verde (via Infobae)"},
		{URL: "https://www.example.org/m", Outlet: "Revista RTVE Hoy"},
	}
	got := sourcesOf(rows, data, "es-ES")
	for url, want := range map[string]string{
		"https://efe.com/a":         " (medio estatal)",
		"https://efeverde.com/b":    " (medio estatal)",
		"https://www.rtve.es/c":     " (medio estatal)",
		"https://www.infobae.com/d": " (medio estatal)",
		"https://www.infobae.com/e": "",
		"https://www.example.org/f": "",
		"https://www.infobae.com/g": " (medio estatal)",
		"https://www.example.org/h": " (medio estatal)",
		"https://es.yahoo.com/i":    " (medio estatal)",
		"https://www.example.org/j": "",
		"https://www.example.org/k": " (medio estatal)",
		"https://www.example.org/l": " (medio estatal)",
		"https://www.example.org/m": "",
	} {
		if m := got[url].Marks(); m != want {
			t.Errorf("%s: marks %q, want %q", url, m, want)
		}
	}
}
