package publish

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/tereshed/8bitnews/internal/store"
)

// How far ahead a slot is booked, at most: more than this and the channel's
// queue, not the slots, is the problem.
const slotSearchDays = 60

type channelPublishing struct {
	Moderation struct {
		ApprovalRequired *bool `json:"approval_required"`
	} `json:"moderation"`
	Schedule struct {
		PublishSlots    []string `json:"publish_slots"`
		PublishBlackout []string `json:"publish_blackout"`
	} `json:"schedule"`
}

func publishingOf(ch store.Channel) (channelPublishing, error) {
	var p channelPublishing
	if len(ch.Data) == 0 {
		return p, nil
	}
	if err := json.Unmarshal(ch.Data, &p); err != nil {
		return p, fmt.Errorf("decoding the profile of channel %s: %w", ch.ID, err)
	}
	return p, nil
}

// ApprovalRequired says whether a person must approve each video of the
// channel. Only an explicit false in the profile says no: a profile that is
// missing or unreadable leaves the person in place.
func ApprovalRequired(ch store.Channel) bool {
	p, err := publishingOf(ch)
	if err != nil || p.Moderation.ApprovalRequired == nil {
		return true
	}
	return *p.Moderation.ApprovalRequired
}

// SlotRules is when a channel publishes.
type SlotRules struct {
	// Slots are "15:04" in the channel's time zone, the main one first.
	Slots []string
	// Blackout are local dates ("2006-01-02") with nothing going out.
	Blackout []string
	// Soonest takes the first free slot by the clock, not the main one first.
	Soonest bool
}

// SlotRulesOf reads a channel's profile.
func SlotRulesOf(ch store.Channel) (SlotRules, error) {
	p, err := publishingOf(ch)
	if err != nil {
		return SlotRules{}, err
	}
	return SlotRules{Slots: p.Schedule.PublishSlots, Blackout: p.Schedule.PublishBlackout}, nil
}

// StopNotePrefix opens a Legal publish note naming a fact that would stop the
// video (newsroom/teams/legal/_common.md).
const StopNotePrefix = "СТОП:"

// HasStopNote says whether Legal named such a fact. Nobody rechecks it after
// Legal, so the wait until the video goes out should be short.
func HasStopNote(notes []string) bool {
	for _, n := range notes {
		if strings.HasPrefix(strings.TrimSpace(n), StopNotePrefix) {
			return true
		}
	}
	return false
}

// NextSlot is the first free daily slot, in time zone tz, not before notBefore,
// not in taken (Unix seconds) and not on a blackout date. It goes day by day
// and, within a day, in the order the profile lists them: the first is the
// channel's main slot, and a later one is for the days with more than one
// video. With rules.Soonest, within a day it goes by the clock.
func NextSlot(notBefore time.Time, tz string, rules SlotRules, taken map[int64]bool) (time.Time, error) {
	if tz == "" {
		return time.Time{}, fmt.Errorf("the channel has no time zone")
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return time.Time{}, fmt.Errorf("the channel's time zone %q: %w", tz, err)
	}
	if len(rules.Slots) == 0 {
		return time.Time{}, fmt.Errorf("the channel has no schedule.publish_slots")
	}
	parsed := make([]time.Time, len(rules.Slots))
	for i, s := range rules.Slots {
		if parsed[i], err = time.Parse("15:04", s); err != nil {
			return time.Time{}, fmt.Errorf("publish slot %q: %w", s, err)
		}
	}
	if rules.Soonest {
		sort.Slice(parsed, func(i, j int) bool { return parsed[i].Before(parsed[j]) })
	}
	blackout := make(map[string]bool, len(rules.Blackout))
	for _, d := range rules.Blackout {
		if _, err := time.Parse(time.DateOnly, d); err != nil {
			return time.Time{}, fmt.Errorf("blackout date %q: %w", d, err)
		}
		blackout[d] = true
	}
	day := notBefore.In(loc)
	for d := 0; d <= slotSearchDays; d++ {
		for _, s := range parsed {
			at := time.Date(day.Year(), day.Month(), day.Day()+d, s.Hour(), s.Minute(), 0, 0, loc)
			if !at.Before(notBefore) && !taken[at.Unix()] && !blackout[at.Format(time.DateOnly)] {
				return at, nil
			}
		}
	}
	return time.Time{}, fmt.Errorf("no free slot in the next %d days", slotSearchDays)
}
