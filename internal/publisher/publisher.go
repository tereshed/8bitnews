// Package publisher takes a finished video to YouTube's schedule by itself, on
// the channels connected to hub (/admin/youtube). A channel not connected stays
// with whoever uploads through the publishing API (docs/automation.md).
//
// One pass a minute, one upload at a time. A pass first takes down what must
// not go out, then goes through the queue. How far an upload got is written
// down as it goes (store.PublishJob): the session before the first byte, the
// video id as soon as it is known. So a restart asks YouTube and goes on, and
// never uploads a video twice.
package publisher

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"path"
	"strings"
	"time"

	"github.com/tereshed/8bitnews/internal/service"
	"github.com/tereshed/8bitnews/internal/store"
	"github.com/tereshed/8bitnews/internal/youtube"
)

const (
	passEvery = time.Minute
	// How long one video may take to go up.
	uploadTimeout = 30 * time.Minute
	// A session not finished with its time this close is dropped for a new slot.
	minSessionLead = 5 * time.Minute
	// How many passes the poster gets before the video goes on without it.
	thumbnailAttempts = 3
	// Signs what hub recorded itself, in release_publications.
	setByHub = "hub"
)

// backoff is the wait after each failure in a row.
var backoff = []time.Duration{time.Minute, 5 * time.Minute, 15 * time.Minute, time.Hour}

// Store is what the worker reads and writes (internal/store).
type Store interface {
	ConnectedAccounts(ctx context.Context, platform string) ([]store.PlatformAccount, error)
	MarkAccountAlerted(ctx context.Context, channelID, platform string) error
	GetRelease(ctx context.Context, id string) (store.Release, error)
	PublishHold(ctx context.Context, releaseID string) (string, error)
	SetPublishHold(ctx context.Context, releaseID, reason string) error
	GetChannel(ctx context.Context, id string) (store.Channel, error)
	PublishJobOf(ctx context.Context, releaseID string) (store.PublishJob, error)
	SavePublishJob(ctx context.Context, j store.PublishJob) error
	DeletePublishJob(ctx context.Context, releaseID string) error
	SetPublicationURL(ctx context.Context, releaseID, platform, url, by string) error
	ReleasesToUnschedule(ctx context.Context) ([]store.ToUnschedule, error)
	ForgetPublication(ctx context.Context, releaseID, platform string) error
}

// Queue is the same work and the same slots the publishing API hands out
// (internal/service).
type Queue interface {
	UploadItems(ctx context.Context) ([]service.PublishQueueItem, error)
	ReserveReleaseSlot(ctx context.Context, releaseID string) (service.ReleaseSlotResponse, error)
}

// YouTube is the platform (internal/youtube).
type YouTube interface {
	StartUpload(ctx context.Context, refresh string, u youtube.Upload, size int64) (string, error)
	QueryUpload(ctx context.Context, refresh, session string, size int64) (youtube.UploadState, error)
	SendBytes(ctx context.Context, refresh, session string, body io.Reader, offset, size int64) (youtube.UploadState, error)
	SetThumbnail(ctx context.Context, refresh, videoID string, body io.Reader, size int64, contentType string) error
	GetVideo(ctx context.Context, refresh, videoID string) (youtube.Video, error)
	Unschedule(ctx context.Context, refresh, videoID string) error
}

// Media opens a release's files (internal/storage).
type Media interface {
	Open(ctx context.Context, key string) (io.ReadCloser, int64, error)
}

// Notifier tells the moderation group (internal/moderation).
type Notifier interface {
	Notify(ctx context.Context, cardID *int64, text string) error
}

// Sealer opens a channel's refresh token (youtube.Sealer).
type Sealer interface {
	Open(sealed, channelID string) (string, error)
}

// Publisher is the worker.
type Publisher struct {
	store  Store
	queue  Queue
	yt     YouTube
	media  Media
	notify Notifier
	sealer Sealer
	log    *slog.Logger
	now    func() time.Time
	// alerted: take-downs YouTube refused, told once per run of hub.
	alerted map[string]bool
}

// New builds the worker.
func New(st Store, q Queue, yt YouTube, media Media, notify Notifier, sealer Sealer, log *slog.Logger) *Publisher {
	return &Publisher{store: st, queue: q, yt: yt, media: media, notify: notify, sealer: sealer, log: log, now: time.Now, alerted: map[string]bool{}}
}

// Run makes a pass a minute until ctx ends.
func (p *Publisher) Run(ctx context.Context) {
	p.log.Info("publisher running")
	t := time.NewTicker(passEvery)
	defer t.Stop()
	for {
		p.Pass(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// Pass does what is due now.
func (p *Publisher) Pass(ctx context.Context) {
	tokens := p.channels(ctx)
	if len(tokens) == 0 {
		return
	}
	p.takeDown(ctx, tokens)
	items, err := p.queue.UploadItems(ctx)
	if err != nil {
		p.log.Error("publisher: reading the queue", "error", err)
		return
	}
	for _, item := range items {
		refresh, connected := tokens[item.ChannelID]
		if !connected {
			continue
		}
		if err := p.take(ctx, item, refresh); errors.Is(err, youtube.ErrAuthRevoked) {
			p.lostAccess(ctx, item.ChannelID)
			delete(tokens, item.ChannelID)
		}
		if ctx.Err() != nil {
			return
		}
	}
}

// channels returns the refresh token of each channel hub can use.
func (p *Publisher) channels(ctx context.Context) map[string]string {
	accounts, err := p.store.ConnectedAccounts(ctx, store.PlatformYouTube)
	if err != nil {
		p.log.Error("publisher: reading the accounts", "error", err)
		return nil
	}
	tokens := make(map[string]string, len(accounts))
	for _, a := range accounts {
		refresh, err := p.sealer.Open(a.Tokens.RefreshSealed, a.ChannelID)
		if err != nil {
			p.log.Error("publisher: opening a channel's token", "channel_id", a.ChannelID, "error", err)
			p.lostAccess(ctx, a.ChannelID)
			continue
		}
		tokens[a.ChannelID] = refresh
	}
	return tokens
}

// lostAccess tells the group once; the channel falls back to the browser queue
// until someone connects it again.
func (p *Publisher) lostAccess(ctx context.Context, channelID string) {
	if err := p.store.MarkAccountAlerted(ctx, channelID, store.PlatformYouTube); err != nil {
		p.log.Error("publisher: recording a lost access", "channel_id", channelID, "error", err)
		return
	}
	p.tell(ctx, nil, "⚠️ hub потерял доступ к YouTube канала "+channelID+": ролики этого канала сами больше не загружаются. Подключите его заново на /admin/youtube.")
}

func (p *Publisher) tell(ctx context.Context, cardID *int64, text string) {
	if err := p.notify.Notify(ctx, cardID, text); err != nil {
		p.log.Error("publisher: telling the group failed", "error", err)
	}
}

// take makes one attempt at one release and books what came of it. Only a
// revoked access comes back to the caller.
func (p *Publisher) take(ctx context.Context, item service.PublishQueueItem, refresh string) error {
	job, err := p.store.PublishJobOf(ctx, item.ReleaseID)
	if err != nil {
		p.log.Error("publisher: reading a job", "release_id", item.ReleaseID, "error", err)
		return nil
	}
	if job.NextAttemptAt.After(p.now()) {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, uploadTimeout)
	defer cancel()
	err = p.publish(ctx, item, refresh, &job)
	switch {
	case err == nil:
		return nil
	case errors.Is(err, youtube.ErrAuthRevoked):
		return err
	case youtube.Retryable(err):
		p.log.Warn("publisher: trying again later", "release_id", item.ReleaseID, "attempts", job.Attempts+1, "error", err)
		job.LastError = err.Error()
		job.NextAttemptAt = p.now().Add(backoff[min(job.Attempts, len(backoff)-1)])
		job.Attempts++
		if err := p.store.SavePublishJob(context.WithoutCancel(ctx), job); err != nil {
			p.log.Error("publisher: saving a job", "release_id", item.ReleaseID, "error", err)
		}
	default:
		p.log.Error("publisher: YouTube refused", "release_id", item.ReleaseID, "error", err)
		job.LastError = err.Error()
		if err := p.store.SavePublishJob(context.WithoutCancel(ctx), job); err != nil {
			p.log.Error("publisher: saving a job", "release_id", item.ReleaseID, "error", err)
		}
		p.hold(context.WithoutCancel(ctx), item.ReleaseID, "YouTube отказал: "+err.Error())
	}
	return nil
}

// hold takes a release out of the queue; hub announces holds (moderation).
func (p *Publisher) hold(ctx context.Context, releaseID, reason string) {
	if err := p.store.SetPublishHold(ctx, releaseID, reason); err != nil {
		p.log.Error("publisher: holding a release", "release_id", releaseID, "error", err)
	}
}

// youtubeWarnings leaves out what is about other platforms: a TikTok caption
// over its limit doesn't stop a YouTube upload.
func youtubeWarnings(warnings []string) []string {
	var out []string
	for _, w := range warnings {
		if !strings.HasPrefix(w, "tiktok:") && !strings.HasPrefix(w, "instagram:") {
			out = append(out, w)
		}
	}
	return out
}

func (p *Publisher) publish(ctx context.Context, item service.PublishQueueItem, refresh string, job *store.PublishJob) error {
	// Right before anything goes out: the queue was read a while ago.
	r, err := p.store.GetRelease(ctx, item.ReleaseID)
	if err != nil {
		return err
	}
	if r.Status != "approved" {
		return nil
	}
	hold, err := p.store.PublishHold(ctx, item.ReleaseID)
	if err != nil {
		return err
	}
	if hold != "" {
		return nil
	}
	if w := youtubeWarnings(item.Warnings); len(w) > 0 {
		p.hold(ctx, item.ReleaseID, "пакет с предупреждениями: "+strings.Join(w, "; "))
		return nil
	}
	ch, err := p.store.GetChannel(ctx, item.ChannelID)
	if err != nil {
		return err
	}
	if job.VideoID == "" {
		booked, err := p.sendVideo(ctx, item, ch, refresh, job)
		if err != nil || !booked {
			return err
		}
	}
	proceed, err := p.verify(ctx, item, r, refresh, job)
	if err != nil || !proceed {
		return err
	}
	poster, err := p.poster(ctx, item, refresh, job)
	if err != nil {
		return err
	}
	return p.record(ctx, item, r, ch, job, poster)
}

// sendVideo gets the video up, going on from where a session stands. It reports
// false if the slot was refused: the service has held the release, or it isn't
// approved any more.
func (p *Publisher) sendVideo(ctx context.Context, item service.PublishQueueItem, ch store.Channel, refresh string, job *store.PublishJob) (bool, error) {
	body, size, err := p.media.Open(ctx, item.Video)
	if err != nil {
		return false, fmt.Errorf("opening the video: %w", err)
	}
	defer func() { _ = body.Close() }()

	var state youtube.UploadState
	if job.SessionURI != "" {
		state, err = p.yt.QueryUpload(ctx, refresh, job.SessionURI, size)
		switch {
		case errors.Is(err, youtube.ErrSessionGone):
			job.SessionURI, job.PublishAt = "", nil
			state = youtube.UploadState{}
		case err != nil:
			return false, err
		case state.VideoID != "":
			job.VideoID = state.VideoID
			return true, p.save(ctx, job)
		case job.PublishAt == nil || job.PublishAt.Before(p.now().Add(minSessionLead)):
			// Nothing exists on YouTube until the upload finishes, so this
			// session can simply be left behind.
			job.SessionURI, job.PublishAt = "", nil
			state = youtube.UploadState{}
		}
	}
	if job.SessionURI == "" {
		slot, err := p.queue.ReserveReleaseSlot(ctx, item.ReleaseID)
		var refused *service.Error
		if errors.As(err, &refused) && refused.Kind == service.KindConflict {
			p.log.Info("publisher: no slot", "release_id", item.ReleaseID, "reason", refused.Message)
			return false, nil
		}
		if err != nil {
			return false, err
		}
		session, err := p.yt.StartUpload(ctx, refresh, youtube.Upload{
			Title: item.Title, Description: item.Description, Tags: item.Tags, Language: ch.Language, PublishAt: slot.PublishAt,
		}, size)
		if err != nil {
			return false, err
		}
		at := slot.PublishAt
		job.SessionURI, job.VideoSize, job.PublishAt = session, size, &at
		if err := p.save(ctx, job); err != nil {
			return false, err
		}
	}
	if state.Offset > 0 {
		if _, err := io.CopyN(io.Discard, body, state.Offset); err != nil {
			return false, fmt.Errorf("skipping to byte %d of the video: %w", state.Offset, err)
		}
	}
	state, err = p.yt.SendBytes(ctx, refresh, job.SessionURI, body, state.Offset, size)
	if err != nil {
		return false, err
	}
	if state.VideoID == "" {
		return false, &youtube.Error{Retry: true, Message: fmt.Sprintf("the upload stopped at byte %d of %d", state.Offset, size)}
	}
	job.VideoID = state.VideoID
	return true, p.save(ctx, job)
}

func (p *Publisher) save(ctx context.Context, job *store.PublishJob) error {
	return p.store.SavePublishJob(context.WithoutCancel(ctx), *job)
}

func shortsURL(videoID string) string {
	return "https://www.youtube.com/shorts/" + videoID
}

// verify reads back what YouTube made of the video. It reports false if the
// release has been held: refused by YouTube, or not what was asked for. Once a
// video exists its link is recorded, so a video on the schedule is never lost.
func (p *Publisher) verify(ctx context.Context, item service.PublishQueueItem, r store.Release, refresh string, job *store.PublishJob) (bool, error) {
	v, err := p.yt.GetVideo(ctx, refresh, job.VideoID)
	if err != nil {
		return false, err
	}
	if !v.Found {
		return false, &youtube.Error{Retry: true, Message: "YouTube doesn't show video " + job.VideoID + " yet"}
	}
	if reason := v.Refused(); reason != "" {
		// Nothing to take down, and a lifted hold should upload afresh.
		if err := p.store.DeletePublishJob(ctx, item.ReleaseID); err != nil {
			return false, err
		}
		p.hold(ctx, item.ReleaseID, "YouTube не принял ролик "+job.VideoID+": "+reason)
		return false, nil
	}
	// A time gone by: a job picked up late, and the video may rightly be public.
	if job.PublishAt != nil && job.PublishAt.Before(p.now()) {
		return true, nil
	}
	var wrong []string
	if v.PrivacyStatus != "private" {
		wrong = append(wrong, "видимость "+v.PrivacyStatus+", а не private")
	}
	if v.PublishAt == nil || job.PublishAt == nil || !v.PublishAt.Equal(*job.PublishAt) {
		wrong = append(wrong, "YouTube не поставил расписание на нужный час")
	}
	if v.SyntheticMedia != nil && !*v.SyntheticMedia {
		wrong = append(wrong, "метка AI не поставилась")
	}
	if len(wrong) == 0 {
		return true, nil
	}
	if err := p.store.SetPublicationURL(ctx, item.ReleaseID, store.PlatformYouTube, shortsURL(job.VideoID), setByHub); err != nil {
		return false, err
	}
	p.hold(ctx, item.ReleaseID, "загруженный ролик "+job.VideoID+" не такой, как просили: "+strings.Join(wrong, "; "))
	return false, nil
}

func thumbnailType(key string) string {
	switch strings.ToLower(path.Ext(key)) {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	}
	return "image/png"
}

// poster sets the cover. It returns what the group should hear if the video
// goes on without one: the owner requires a poster, but a video on the schedule
// isn't worth losing over it.
func (p *Publisher) poster(ctx context.Context, item service.PublishQueueItem, refresh string, job *store.PublishJob) (string, error) {
	if job.ThumbnailSet {
		return "", nil
	}
	if item.Cover == "" {
		return "в пакете нет обложки", nil
	}
	body, size, err := p.media.Open(ctx, item.Cover)
	if err != nil {
		return "", fmt.Errorf("opening the cover: %w", err)
	}
	defer func() { _ = body.Close() }()
	err = p.yt.SetThumbnail(ctx, refresh, job.VideoID, body, size, thumbnailType(item.Cover))
	switch {
	case err == nil:
		job.ThumbnailSet = true
		return "", p.save(ctx, job)
	case errors.Is(err, youtube.ErrAuthRevoked):
		return "", err
	case youtube.Retryable(err) && job.ThumbnailAttempts+1 < thumbnailAttempts:
		job.ThumbnailAttempts++
		return "", err
	}
	p.log.Error("publisher: the poster didn't go up", "release_id", item.ReleaseID, "error", err)
	return err.Error(), nil
}

// record writes the link down and tells the group. From here hub watches the
// video go out (moderation.CheckPublications).
func (p *Publisher) record(ctx context.Context, item service.PublishQueueItem, r store.Release, ch store.Channel, job *store.PublishJob, poster string) error {
	link := shortsURL(job.VideoID)
	if err := p.store.SetPublicationURL(ctx, item.ReleaseID, store.PlatformYouTube, link, setByHub); err != nil {
		return err
	}
	job.Attempts, job.LastError = 0, ""
	if err := p.save(ctx, job); err != nil {
		return err
	}
	text := "📤 Загружен на YouTube: " + link + "\nВыйдет " + when(job.PublishAt, ch.Timezone) + "."
	if poster != "" {
		text += "\n⚠️ Обложку поставить не удалось: " + poster + ". Ролик в расписании без неё; поставьте её в Studio."
	}
	p.tell(ctx, r.Moderation.MessageID, text)
	return nil
}

func when(at *time.Time, tz string) string {
	if at == nil {
		return "в свой слот"
	}
	utc := at.UTC().Format("02.01 15:04") + " UTC"
	loc, err := time.LoadLocation(tz)
	if err != nil || tz == "" {
		return utc
	}
	return at.In(loc).Format("02.01 15:04") + " (" + tz + "), " + utc
}

// takeDown takes off the schedule what must not go out: its release has been
// closed or held. An empty link means a slot was booked and no link came, so
// the job says whether a video exists.
func (p *Publisher) takeDown(ctx context.Context, tokens map[string]string) {
	list, err := p.store.ReleasesToUnschedule(ctx)
	if err != nil {
		p.log.Error("publisher: reading what to take down", "error", err)
		return
	}
	for _, u := range list {
		refresh, connected := tokens[u.ChannelID]
		if !connected {
			continue
		}
		videoID, err := p.videoOf(ctx, u, refresh)
		if err == nil && videoID != "" {
			err = p.yt.Unschedule(ctx, refresh, videoID)
		}
		switch {
		case err == nil:
		case errors.Is(err, youtube.ErrAuthRevoked):
			p.lostAccess(ctx, u.ChannelID)
			delete(tokens, u.ChannelID)
			continue
		case youtube.Retryable(err):
			p.log.Warn("publisher: taking a video down, trying again", "release_id", u.ReleaseID, "error", err)
			continue
		default:
			p.log.Error("publisher: YouTube refused to take a video down", "release_id", u.ReleaseID, "error", err)
			if !p.alerted[u.ReleaseID] {
				p.alerted[u.ReleaseID] = true
				p.tellOnCard(ctx, u.ReleaseID, "⚠️ YouTube не дал снять ролик с расписания: "+err.Error()+". Он выйдет "+
					when(&u.PublishAt, "")+", если его не снять в Studio: "+shortsURL(videoID))
			}
			continue
		}
		if err := p.store.ForgetPublication(ctx, u.ReleaseID, store.PlatformYouTube); err != nil {
			p.log.Error("publisher: forgetting a taken-down video", "release_id", u.ReleaseID, "error", err)
			continue
		}
		if videoID != "" {
			p.tellOnCard(ctx, u.ReleaseID, "🚫 Ролик снят с расписания: "+shortsURL(videoID)+". На YouTube он остался как Private.")
		}
	}
}

// videoOf finds the video behind what must come down, if there is one.
func (p *Publisher) videoOf(ctx context.Context, u store.ToUnschedule, refresh string) (string, error) {
	if u.URL != "" {
		return path.Base(u.URL), nil
	}
	job, err := p.store.PublishJobOf(ctx, u.ReleaseID)
	if err != nil {
		return "", err
	}
	if job.VideoID != "" || job.SessionURI == "" {
		return job.VideoID, nil
	}
	state, err := p.yt.QueryUpload(ctx, refresh, job.SessionURI, job.VideoSize)
	if errors.Is(err, youtube.ErrSessionGone) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return state.VideoID, nil
}

func (p *Publisher) tellOnCard(ctx context.Context, releaseID, text string) {
	var card *int64
	if r, err := p.store.GetRelease(ctx, releaseID); err == nil {
		card = r.Moderation.MessageID
	}
	p.tell(ctx, card, text)
}
