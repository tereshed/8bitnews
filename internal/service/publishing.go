package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/tereshed/8bitnews/internal/publish"
	"github.com/tereshed/8bitnews/internal/store"
)

// slotLead is how much of a slot's lead time an upload needs: processing,
// the poster, and a margin for a slow day. A slot closer than this is skipped.
const slotLead = 45 * time.Minute

// setByAPI signs a link recorded through the API, in release_publications.
const setByAPI = "api"

// queueMaxAge is how long after its submission a video may go out by itself.
// News goes stale: a video the slots can't reach by then isn't given one.
const queueMaxAge = 72 * time.Hour

var youtubeVideoID = regexp.MustCompile(`^[A-Za-z0-9_-]{11}$`)

// PublishQueueItem is one upload to make: the package a moderator used to get
// on the card, and what Legal left for the moment of publication.
type PublishQueueItem struct {
	ReleaseID    string    `json:"release_id"`
	ChannelID    string    `json:"channel_id"`
	StoryTitle   string    `json:"story_title"`
	Development  string    `json:"development_summary"`
	CreatedAt    time.Time `json:"created_at"`
	Title        string    `json:"title"`
	Description  string    `json:"description"`
	Tags         []string  `json:"tags"`
	Settings     []string  `json:"settings"`
	Video        string    `json:"video"`
	Cover        string    `json:"cover"`
	Warnings     []string  `json:"warnings"`
	Notes        []string  `json:"notes"`
	PublishNotes []string  `json:"publish_notes"`
	// PublishAt: an earlier attempt booked this time, and may have got as far
	// as YouTube without recording it. Look in Studio before uploading again.
	PublishAt *time.Time `json:"publish_at,omitempty"`
}

// UnscheduleItem is a video to take off YouTube's schedule: its release has
// been closed or held, and it would go out at its slot.
type UnscheduleItem struct {
	ReleaseID string    `json:"release_id"`
	ChannelID string    `json:"channel_id"`
	Status    string    `json:"status"`
	Hold      string    `json:"hold,omitempty"`
	URL       string    `json:"url"`
	PublishAt time.Time `json:"publish_at"`
}

// PublishQueueResponse is the work waiting for whoever uploads.
type PublishQueueResponse struct {
	Upload     []PublishQueueItem `json:"upload"`
	Unschedule []UnscheduleItem   `json:"unschedule"`
}

// PublishQueue returns what to upload and what to take off the schedule on the
// channels hub doesn't take care of itself, so nobody races internal/publisher.
func (s *Service) PublishQueue(ctx context.Context) (PublishQueueResponse, error) {
	resp := PublishQueueResponse{Upload: []PublishQueueItem{}, Unschedule: []UnscheduleItem{}}
	connected, err := s.connectedChannels(ctx)
	if err != nil {
		return resp, err
	}
	items, err := s.UploadItems(ctx)
	if err != nil {
		return resp, err
	}
	for _, item := range items {
		if !connected[item.ChannelID] {
			resp.Upload = append(resp.Upload, item)
		}
	}
	closed, err := s.store.ReleasesToUnschedule(ctx)
	if err != nil {
		return resp, err
	}
	for _, u := range closed {
		if !connected[u.ChannelID] {
			resp.Unschedule = append(resp.Unschedule, UnscheduleItem{
				ReleaseID: u.ReleaseID, ChannelID: u.ChannelID, Status: u.Status, Hold: u.Hold, URL: u.URL, PublishAt: u.PublishAt,
			})
		}
	}
	return resp, nil
}

// UploadItems returns everything waiting to go up on YouTube, on every channel.
func (s *Service) UploadItems(ctx context.Context) ([]PublishQueueItem, error) {
	releases, err := s.store.PublishQueue(ctx)
	if err != nil {
		return nil, err
	}
	items := make([]PublishQueueItem, 0, len(releases))
	for _, r := range releases {
		item, err := s.publishQueueItem(ctx, r)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, nil
}

func (s *Service) connectedChannels(ctx context.Context) (map[string]bool, error) {
	connected := map[string]bool{}
	if !s.uploaderOn {
		return connected, nil
	}
	accounts, err := s.store.ConnectedAccounts(ctx, store.PlatformYouTube)
	if err != nil {
		return nil, err
	}
	for _, a := range accounts {
		connected[a.ChannelID] = true
	}
	return connected, nil
}

func (s *Service) publishQueueItem(ctx context.Context, r store.Release) (PublishQueueItem, error) {
	item := PublishQueueItem{ReleaseID: r.ID, ChannelID: r.ChannelID, CreatedAt: r.CreatedAt}
	rc, err := s.store.ReleaseContextOf(ctx, r.ID)
	if err != nil {
		return item, err
	}
	item.StoryTitle, item.Development = rc.StoryTitle, rc.DevelopmentSummary
	ch, err := s.store.GetChannel(ctx, r.ChannelID)
	if err != nil {
		return item, fmt.Errorf("reading the channel of release %s: %w", r.ID, err)
	}
	sources, err := s.store.ListReleaseSources(ctx, r.ID)
	if err != nil {
		return item, err
	}
	country, err := s.store.GetCountry(ctx, ch.CountryCC)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return item, err
	}
	pkg := publish.Build(publish.Input{Release: r, Sources: sources, Channel: ch, Country: country, Config: s.publish})
	item.Warnings, item.Notes = pkg.Warnings, pkg.Notes
	for _, p := range pkg.Platforms {
		if p.Platform == store.PlatformYouTube {
			item.Title, item.Description, item.Tags, item.Settings, item.Video = p.Title, p.Description, p.Tags, p.Settings, p.Video
		}
	}
	if item.Video == "" {
		item.Warnings = append(item.Warnings, "the package has no YouTube part")
	}
	var media struct {
		Cover string `json:"cover"`
	}
	if err := json.Unmarshal(nonEmptyJSON(r.Media), &media); err != nil {
		return item, fmt.Errorf("decoding the media of release %s: %w", r.ID, err)
	}
	item.Cover = media.Cover
	var legal struct {
		PublishNotes []string `json:"publish_notes"`
	}
	if err := json.Unmarshal(nonEmptyJSON(r.LegalReview), &legal); err != nil {
		return item, fmt.Errorf("decoding the legal review of release %s: %w", r.ID, err)
	}
	item.PublishNotes = legal.PublishNotes
	if item.PublishAt, err = s.store.SlotOf(ctx, r.ID); err != nil {
		return item, err
	}
	return item, nil
}

// ReleaseSlotResponse is the time a release goes out at.
type ReleaseSlotResponse struct {
	PublishAt time.Time `json:"publish_at"`
	Local     string    `json:"local"` // the same time on the channel's clock
	TimeZone  string    `json:"time_zone"`
}

func (s *Service) releaseForPublishing(ctx context.Context, rawID string) (store.Release, error) {
	id, err := parseID("release_id", rawID)
	if err != nil {
		return store.Release{}, err
	}
	r, err := s.store.GetRelease(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		return r, notFoundf("release %q not found", id)
	}
	return r, err
}

// ReserveReleaseSlot books the release's time on its channel's schedule. The
// same release asking again keeps its time, unless that is too close by now.
// It refuses a release that is held, and one too old by the time a slot is free.
func (s *Service) ReserveReleaseSlot(ctx context.Context, releaseID string) (ReleaseSlotResponse, error) {
	r, err := s.releaseForPublishing(ctx, releaseID)
	if err != nil {
		return ReleaseSlotResponse{}, err
	}
	if r.Status != "approved" {
		return ReleaseSlotResponse{}, conflictf("release %s is %s, not approved", r.ID, r.Status)
	}
	hold, err := s.store.PublishHold(ctx, r.ID)
	if err != nil {
		return ReleaseSlotResponse{}, err
	}
	if hold != "" {
		return ReleaseSlotResponse{}, conflictf("release %s is held: %s", r.ID, hold)
	}
	ch, err := s.store.GetChannel(ctx, r.ChannelID)
	if err != nil {
		return ReleaseSlotResponse{}, fmt.Errorf("reading the channel of release %s: %w", r.ID, err)
	}
	rules, err := publish.SlotRulesOf(ch)
	if err != nil {
		return ReleaseSlotResponse{}, err
	}
	var legal struct {
		PublishNotes []string `json:"publish_notes"`
	}
	if err := json.Unmarshal(nonEmptyJSON(r.LegalReview), &legal); err != nil {
		return ReleaseSlotResponse{}, fmt.Errorf("decoding the legal review of release %s: %w", r.ID, err)
	}
	// Nobody rechecks a stop condition after Legal: keep the wait short.
	rules.Soonest = publish.HasStopNote(legal.PublishNotes)
	notBefore := time.Now().Add(slotLead)
	at, err := s.store.ReserveSlot(ctx, r.ID, r.ChannelID, notBefore, func(taken map[int64]bool) (time.Time, error) {
		at, err := publish.NextSlot(notBefore, ch.Timezone, rules, taken)
		if err != nil {
			return at, err
		}
		if at.After(r.CreatedAt.Add(queueMaxAge)) {
			return at, conflictf("release %s is too old: the next free slot is %s, past %s of its submission", r.ID, at.UTC().Format(time.RFC3339), queueMaxAge)
		}
		return at, nil
	})
	var refused *Error
	if errors.As(err, &refused) && refused.Kind == KindConflict {
		// Out of the queue, and hub tells the group why.
		if holdErr := s.store.SetPublishHold(ctx, r.ID, refused.Message); holdErr != nil {
			return ReleaseSlotResponse{}, holdErr
		}
	}
	if err != nil {
		return ReleaseSlotResponse{}, err
	}
	return slotResponse(at, ch.Timezone), nil
}

func slotResponse(at time.Time, tz string) ReleaseSlotResponse {
	resp := ReleaseSlotResponse{PublishAt: at.UTC(), TimeZone: tz}
	if loc, err := time.LoadLocation(tz); err == nil {
		resp.Local = at.In(loc).Format("2006-01-02 15:04")
	}
	return resp
}

// YouTubePublicationRequest records an upload. PublishAt is the time the video
// stands in YouTube's schedule for, if not the release's reserved slot.
type YouTubePublicationRequest struct {
	VideoID   string     `json:"video_id"`
	PublishAt *time.Time `json:"publish_at"`
}

// SetYouTubePublication records where a release's video is. Hub then watches
// it: it moves the release to "published" when the video is up. A release
// closed while its upload was under way is recorded too, or nobody would know
// a video stands in the schedule for it.
func (s *Service) SetYouTubePublication(ctx context.Context, releaseID string, req YouTubePublicationRequest) error {
	if !youtubeVideoID.MatchString(req.VideoID) {
		return invalidf("video_id %q is not a YouTube video id", req.VideoID)
	}
	r, err := s.releaseForPublishing(ctx, releaseID)
	if err != nil {
		return err
	}
	switch r.Status {
	case "submitted", "pending_approval":
		return conflictf("release %s is %s: nothing decided yet", r.ID, r.Status)
	}
	allowed := false
	for _, p := range r.AllowedPlatforms {
		allowed = allowed || p == store.PlatformYouTube
	}
	if !allowed {
		return conflictf("Legal didn't let release %s onto YouTube", r.ID)
	}
	url := "https://www.youtube.com/shorts/" + req.VideoID
	owner, err := s.store.PublicationOwner(ctx, store.PlatformYouTube, url)
	if err != nil {
		return err
	}
	if owner != "" && owner != r.ID {
		return conflictf("video %s is recorded for release %s", req.VideoID, owner)
	}
	if req.PublishAt != nil && req.PublishAt.Before(time.Now()) {
		return invalidf("publish_at %s has passed", req.PublishAt.UTC().Format(time.RFC3339))
	}
	// The link first: the video is on YouTube whatever becomes of its time.
	if err := s.store.SetPublicationURL(ctx, r.ID, store.PlatformYouTube, url, setByAPI); err != nil {
		return err
	}
	if req.PublishAt == nil {
		return nil
	}
	err = s.store.SetSlot(ctx, r.ID, r.ChannelID, *req.PublishAt)
	if errors.Is(err, store.ErrSlotTaken) {
		return conflictf("the link is recorded, but another release of channel %s holds %s", r.ChannelID, req.PublishAt.UTC().Format(time.RFC3339))
	}
	return err
}

// ForgetYouTubePublication drops a release's link and slot: the video is off
// the schedule, or was never going to be on it.
func (s *Service) ForgetYouTubePublication(ctx context.Context, releaseID string) error {
	id, err := parseID("release_id", releaseID)
	if err != nil {
		return err
	}
	if _, err := s.store.GetRelease(ctx, id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return notFoundf("release %q not found", id)
		}
		return err
	}
	return s.store.ForgetPublication(ctx, id, store.PlatformYouTube)
}

// HoldRelease keeps a release out of the publish queue for a reason: what
// Legal said to stop on has happened. A video already scheduled goes to the
// list of those to take off the schedule.
func (s *Service) HoldRelease(ctx context.Context, releaseID, reason string) error {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return invalidf("a hold needs a reason")
	}
	return s.setHold(ctx, releaseID, reason)
}

// ReleaseHold lets a held release back into the queue.
func (s *Service) ReleaseHold(ctx context.Context, releaseID string) error {
	return s.setHold(ctx, releaseID, "")
}

func (s *Service) setHold(ctx context.Context, releaseID, reason string) error {
	id, err := parseID("release_id", releaseID)
	if err != nil {
		return err
	}
	err = s.store.SetPublishHold(ctx, id, reason)
	if errors.Is(err, store.ErrNotFound) {
		return notFoundf("release %q not found", id)
	}
	return err
}
