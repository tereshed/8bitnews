package publish

import (
	"encoding/json"
	"fmt"
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
		PublishSlots []string `json:"publish_slots"`
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

// PublishSlots returns the channel's slots, "15:04" in its time zone.
func PublishSlots(ch store.Channel) ([]string, error) {
	p, err := publishingOf(ch)
	if err != nil {
		return nil, err
	}
	return p.Schedule.PublishSlots, nil
}

// NextSlot is the first free daily slot, in time zone tz, not before notBefore
// and not in taken (Unix seconds). It goes day by day and, within a day, in the
// order the profile lists them: the first is the channel's main slot, and a
// later one is for the days with more than one video.
func NextSlot(notBefore time.Time, tz string, slots []string, taken map[int64]bool) (time.Time, error) {
	if tz == "" {
		return time.Time{}, fmt.Errorf("the channel has no time zone")
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return time.Time{}, fmt.Errorf("the channel's time zone %q: %w", tz, err)
	}
	if len(slots) == 0 {
		return time.Time{}, fmt.Errorf("the channel has no schedule.publish_slots")
	}
	parsed := make([]time.Time, len(slots))
	for i, s := range slots {
		if parsed[i], err = time.Parse("15:04", s); err != nil {
			return time.Time{}, fmt.Errorf("publish slot %q: %w", s, err)
		}
	}
	day := notBefore.In(loc)
	for d := 0; d <= slotSearchDays; d++ {
		for _, s := range parsed {
			at := time.Date(day.Year(), day.Month(), day.Day()+d, s.Hour(), s.Minute(), 0, 0, loc)
			if !at.Before(notBefore) && !taken[at.Unix()] {
				return at, nil
			}
		}
	}
	return time.Time{}, fmt.Errorf("no free slot in the next %d days", slotSearchDays)
}
