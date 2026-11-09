// Package publisher takes a finished video to YouTube's schedule by itself, on
// the channels connected to hub (/admin/youtube). A channel not connected stays
// with whoever uploads through the publishing API (docs/automation.md).
//
// Two loops run a pass a minute, under a Postgres lock so that only one process
// does any of this. One takes down what must not go out and reads videos back
// before their slot; the other uploads, one video at a time, so a long upload
// can't delay a take-down. How far an upload got is written down as it goes
// (store.PublishJob): the session before the first byte, the video id as soon as
// it is known. So a restart asks YouTube and goes on instead of starting over.
package publisher

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"path"
	"strings"
	"sync"
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
	// How many tries the poster gets before the video goes on without it.
	thumbnailAttempts = 3
	// How long before its slot a video is read back again: a video can be
	// rejected well after the read-back at upload.
	recheckBefore = 30 * time.Minute
	// How long a take-down fails before the group hears.
	takeDownAlertAfter = 10 * time.Minute
	// YouTube can answer a read with the old state for a moment after a write.
	surviveAlertAfter = time.Minute
	// A video missing this long is gone, not a read that hasn't caught up.
	goneConfirmAfter = 5 * time.Minute
	// Signs what hub recorded itself, in release_publications.
	setByHub = "hub"
)

// backoff is the wait after each failure in a row.
var backoff = []time.Duration{time.Minute, 5 * time.Minute, 15 * time.Minute, time.Hour}

var (
	errStillScheduled = errors.New("YouTube оставил расписание")
	errFinalising     = &youtube.Error{Retry: true, Message: "YouTube is finalising the upload"}
)

// Store is what the worker reads and writes (internal/store).
type Store interface {
	LockPublisher(ctx context.Context) (store.Lock, bool, error)
	ConnectedAccounts(ctx context.Context, platform string) ([]store.PlatformAccount, error)
	MarkAccountAlerted(ctx context.Context, channelID, platform string) error
	GetRelease(ctx context.Context, id string) (store.Release, error)
	PublishHold(ctx context.Context, releaseID string) (string, error)
	SetPublishHold(ctx context.Context, releaseID, reason string) error
	GetChannel(ctx context.Context, id string) (store.Channel, error)
	PublishJobOf(ctx context.Context, releaseID string) (store.PublishJob, error)
	UnannouncedJobs(ctx context.Context) ([]store.PublishJob, error)
	SavePublishJob(ctx context.Context, j store.PublishJob) error
	SetPublicationURLIfNone(ctx context.Context, releaseID, platform, url, by string) error
	ReleasesToUnschedule(ctx context.Context) ([]store.ToUnschedule, error)
	ForgetPublication(ctx context.Context, releaseID, platform string) error
	SlotsToRecheck(ctx context.Context, before time.Time) ([]store.RecheckDue, error)
	MarkSlotRechecked(ctx context.Context, releaseID string) error
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

	mu        sync.Mutex
	uploading string               // the release the upload loop has in hand
	failing   map[string]time.Time // take-downs failing, since when
	alerted   map[string]bool      // take-downs the group has heard about
	goneSince map[string]time.Time // videos YouTube doesn't show, since when

	// alertMu keeps the two loops from both telling the group about one channel.
	alertMu       sync.Mutex
	clientAlerted map[string]bool
}

// New builds the worker.
func New(st Store, q Queue, yt YouTube, media Media, notify Notifier, sealer Sealer, log *slog.Logger) *Publisher {
	return &Publisher{store: st, queue: q, yt: yt, media: media, notify: notify, sealer: sealer, log: log, now: time.Now,
		failing: map[string]time.Time{}, alerted: map[string]bool{}, goneSince: map[string]time.Time{}, clientAlerted: map[string]bool{}}
}

// Run works while it holds the lock, and waits for the lock otherwise, until
// ctx ends.
func (p *Publisher) Run(ctx context.Context) {
	p.log.Info("publisher running")
	for ctx.Err() == nil {
		lock, ok, err := p.store.LockPublisher(ctx)
		switch {
		case err != nil:
			p.log.Error("publisher: taking the lock", "error", err)
		case !ok:
			p.log.Info("publisher: another process holds the lock")
		default:
			p.runLocked(ctx, lock)
			lock.Release(context.WithoutCancel(ctx))
		}
		select {
		case <-ctx.Done():
		case <-time.After(passEvery):
		}
	}
}

// runLocked runs both loops for as long as the lock's connection lives.
func (p *Publisher) runLocked(ctx context.Context, lock store.Lock) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var loops sync.WaitGroup
	for _, pass := range []func(context.Context){p.maintenancePass, p.uploadPass} {
		loops.Add(1)
		go func() {
			defer loops.Done()
			t := time.NewTicker(passEvery)
			defer t.Stop()
			for ctx.Err() == nil {
				pass(ctx)
				select {
				case <-ctx.Done():
				case <-t.C:
				}
			}
		}()
	}
	t := time.NewTicker(passEvery)
	defer t.Stop()
	for ctx.Err() == nil {
		select {
		case <-ctx.Done():
		case <-t.C:
			if !lock.Alive(ctx) {
				p.log.Error("publisher: the lock's connection is gone")
				cancel()
			}
		}
	}
	loops.Wait()
}

// Pass does everything due now, one loop after the other.
func (p *Publisher) Pass(ctx context.Context) {
	p.maintenancePass(ctx)
	p.uploadPass(ctx)
}

func (p *Publisher) maintenancePass(ctx context.Context) {
	tokens := p.channels(ctx)
	if len(tokens) == 0 {
		return
	}
	p.takeDown(ctx, tokens)
	p.recheck(ctx, tokens)
}

func (p *Publisher) uploadPass(ctx context.Context) {
	tokens := p.channels(ctx)
	if len(tokens) == 0 {
		return
	}
	jobs, err := p.store.UnannouncedJobs(ctx)
	if err != nil {
		p.log.Error("publisher: reading unfinished uploads", "error", err)
		return
	}
	for _, job := range jobs {
		refresh, connected := tokens[job.ChannelID]
		if !connected || job.NextAttemptAt.After(p.now()) {
			continue
		}
		p.accessTrouble(ctx, p.finishJob(ctx, job, refresh), job.ChannelID, tokens)
	}
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
		p.accessTrouble(ctx, p.take(ctx, item, refresh), item.ChannelID, tokens)
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

// accessTrouble deals with the errors that are about the channel and not the
// video, and leaves the channel out of the rest of the pass.
func (p *Publisher) accessTrouble(ctx context.Context, err error, channelID string, tokens map[string]string) {
	switch {
	case errors.Is(err, youtube.ErrAuthRevoked):
		p.lostAccess(ctx, channelID)
		delete(tokens, channelID)
	case errors.Is(err, youtube.ErrClientRefused):
		p.clientRefused(ctx, channelID)
		delete(tokens, channelID)
	}
}

// lostAccess tells the group and only then marks the account, so a Telegram
// failure means trying again. The channel falls back to the browser queue until
// someone connects it again.
func (p *Publisher) lostAccess(ctx context.Context, channelID string) {
	p.alertMu.Lock()
	defer p.alertMu.Unlock()
	accounts, err := p.store.ConnectedAccounts(ctx, store.PlatformYouTube)
	if err != nil {
		p.log.Error("publisher: reading the accounts", "error", err)
		return
	}
	stillConnected := false
	for _, a := range accounts {
		stillConnected = stillConnected || a.ChannelID == channelID
	}
	if !stillConnected { // the other loop got here first
		return
	}
	if !p.tell(ctx, nil, "⚠️ hub потерял доступ к YouTube канала "+channelID+": ролики этого канала сами больше не загружаются, и снять их с расписания hub не может. Подключите его заново на /admin/youtube.") {
		return
	}
	if err := p.store.MarkAccountAlerted(ctx, channelID, store.PlatformYouTube); err != nil {
		p.log.Error("publisher: recording a lost access", "channel_id", channelID, "error", err)
	}
}

// clientRefused: a rotated secret is fixed in hub's configuration, and the
// tokens still work after it. So nothing is marked: one message per channel per
// run of hub, and the channel recovers by itself.
func (p *Publisher) clientRefused(ctx context.Context, channelID string) {
	p.alertMu.Lock()
	defer p.alertMu.Unlock()
	if p.clientAlerted[channelID] {
		return
	}
	if p.tell(ctx, nil, "⚠️ Google не принимает OAuth-клиент hub: мог смениться секрет. Загрузка и снятие роликов канала "+channelID+" стоят, очередь ждёт. Проверьте HUB_YOUTUBE_CLIENT_ID и HUB_YOUTUBE_CLIENT_SECRET.") {
		p.clientAlerted[channelID] = true
	}
}

// tell reports whether the group got the message.
func (p *Publisher) tell(ctx context.Context, cardID *int64, text string) bool {
	if err := p.notify.Notify(context.WithoutCancel(ctx), cardID, text); err != nil {
		p.log.Error("publisher: telling the group failed", "error", err)
		return false
	}
	return true
}

func (p *Publisher) setUploading(releaseID string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.uploading = releaseID
}

func (p *Publisher) isUploading(releaseID string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.uploading == releaseID
}

// missing says whether YouTube has not shown a video for long enough to call it
// gone. seen clears the count.
func (p *Publisher) missing(videoID string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	since, counting := p.goneSince[videoID]
	if !counting {
		p.goneSince[videoID] = p.now()
		return false
	}
	return p.now().Sub(since) >= goneConfirmAfter
}

func (p *Publisher) seen(videoID string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.goneSince, videoID)
}

// open re-reads a release: anything may have happened since the queue was read.
// It reports false if the release isn't approved and free to go any more.
func (p *Publisher) open(ctx context.Context, releaseID string) (store.Release, bool, error) {
	r, err := p.store.GetRelease(ctx, releaseID)
	if err != nil {
		return r, false, err
	}
	if r.Status != "approved" {
		return r, false, nil
	}
	hold, err := p.store.PublishHold(ctx, releaseID)
	if err != nil {
		return r, false, err
	}
	return r, hold == "", nil
}

// take makes one attempt at one release in the queue. Only an error about the
// channel comes back to the caller.
func (p *Publisher) take(ctx context.Context, item service.PublishQueueItem, refresh string) error {
	// Before anything is read: a take-down that begins later sees this and
	// waits, and one that began earlier means the read below finds it closed.
	p.setUploading(item.ReleaseID)
	defer p.setUploading("")
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
	return p.settle(ctx, item.ReleaseID, &job, p.publish(ctx, item, refresh, &job))
}

// finishJob goes on with an upload whose video is up: the release has left the
// queue, since its link is recorded.
func (p *Publisher) finishJob(ctx context.Context, job store.PublishJob, refresh string) error {
	p.setUploading(job.ReleaseID)
	defer p.setUploading("")
	r, free, err := p.open(ctx, job.ReleaseID)
	if err == nil && free {
		err = p.finish(ctx, r, refresh, &job)
	}
	return p.settle(ctx, job.ReleaseID, &job, err)
}

// settle books what came of an attempt.
func (p *Publisher) settle(ctx context.Context, releaseID string, job *store.PublishJob, err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, youtube.ErrAuthRevoked), errors.Is(err, youtube.ErrClientRefused):
		return err
	case youtube.Retryable(err):
		p.log.Warn("publisher: trying again later", "release_id", releaseID, "attempts", job.Attempts+1, "error", err)
		job.LastError = err.Error()
		job.NextAttemptAt = p.now().Add(backoff[min(job.Attempts, len(backoff)-1)])
		job.Attempts++
		if err := p.save(ctx, job); err != nil {
			p.log.Error("publisher: saving a job", "release_id", releaseID, "error", err)
		}
	default:
		p.log.Error("publisher: YouTube refused", "release_id", releaseID, "error", err)
		job.LastError = err.Error()
		if err := p.save(ctx, job); err != nil {
			p.log.Error("publisher: saving a job", "release_id", releaseID, "error", err)
		}
		p.hold(context.WithoutCancel(ctx), releaseID, "YouTube отказал: "+err.Error())
	}
	return nil
}

// hold takes a release out of the queue; hub announces holds (moderation).
func (p *Publisher) hold(ctx context.Context, releaseID, reason string) {
	if err := p.store.SetPublishHold(ctx, releaseID, reason); err != nil {
		p.log.Error("publisher: holding a release", "release_id", releaseID, "error", err)
	}
}

func (p *Publisher) save(ctx context.Context, job *store.PublishJob) error {
	job.Saved = true
	return p.store.SavePublishJob(context.WithoutCancel(ctx), *job)
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
	if _, free, err := p.open(ctx, item.ReleaseID); err != nil || !free {
		return err
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
		// A slot with no job didn't come from here: someone uploading through
		// the publishing API booked it, and that copy may already be on
		// YouTube. Never a second one. The row makes lifting the hold mean "go".
		if !job.Saved && item.PublishAt != nil {
			if err := p.save(ctx, job); err != nil {
				return err
			}
			p.hold(ctx, item.ReleaseID, "слот уже брали до hub: ролик мог уйти на YouTube через браузер. Посмотрите в Studio. Если его там нет, снимите задержку, и hub загрузит его сам.")
			return nil
		}
		// The row before the slot: a crash between them can't look like the above.
		if err := p.save(ctx, job); err != nil {
			return err
		}
		booked, err := p.sendVideo(ctx, item, ch, refresh, job)
		if err != nil || !booked {
			return err
		}
	}
	// Announced before, and back in the queue: someone cleared the link on the
	// admin page. It comes back only if the video is still there.
	if job.AnnouncedAt != nil {
		if _, there, err := p.stillThere(ctx, item.ReleaseID, refresh, job); err != nil || !there {
			return err
		}
	}
	// As soon as the video exists: it can go out, so hub must know of it.
	if err := p.store.SetPublicationURLIfNone(ctx, item.ReleaseID, store.PlatformYouTube, shortsURL(job.VideoID), setByHub); err != nil {
		return err
	}
	if job.AnnouncedAt != nil {
		return nil
	}
	r, free, err := p.open(ctx, item.ReleaseID)
	if err != nil || !free {
		return err
	}
	return p.finish(ctx, r, refresh, job)
}

// forgetSession drops a session that can't be used again.
func (p *Publisher) forgetSession(ctx context.Context, job *store.PublishJob) {
	job.SessionURI, job.PublishAt = "", nil
	if err := p.save(ctx, job); err != nil {
		p.log.Error("publisher: saving a job", "release_id", job.ReleaseID, "error", err)
	}
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
			p.forgetSession(ctx, job)
			state = youtube.UploadState{}
		case youtube.SessionDead(err):
			p.forgetSession(ctx, job)
			return false, err
		case err != nil:
			return false, err
		case state.VideoID != "":
			job.VideoID = state.VideoID
			return true, p.save(ctx, job)
		case state.Finalising(size):
			return false, errFinalising
		case job.PublishAt == nil || job.PublishAt.Before(p.now().Add(minSessionLead)):
			// Nothing exists on YouTube until the upload finishes, so this
			// session can simply be left behind.
			p.forgetSession(ctx, job)
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
	if youtube.SessionDead(err) {
		p.forgetSession(ctx, job)
		return false, err
	}
	if err != nil {
		return false, err
	}
	if state.Finalising(size) {
		return false, errFinalising
	}
	if state.VideoID == "" {
		return false, &youtube.Error{Retry: true, Message: fmt.Sprintf("the upload stopped at byte %d of %d", state.Offset, size)}
	}
	job.VideoID = state.VideoID
	return true, p.save(ctx, job)
}

func shortsURL(videoID string) string {
	return "https://www.youtube.com/shorts/" + videoID
}

// finish reads back what YouTube made of the video, sets the poster and tells
// the group.
func (p *Publisher) finish(ctx context.Context, r store.Release, refresh string, job *store.PublishJob) error {
	v, there, err := p.stillThere(ctx, r.ID, refresh, job)
	if err != nil || !there {
		return err
	}
	// A time gone by: a job picked up late, and the video may rightly be public.
	past := job.PublishAt != nil && job.PublishAt.Before(p.now())
	if !past {
		if wrong := notAsAsked(v, job); len(wrong) > 0 {
			p.hold(ctx, r.ID, "загруженный ролик "+job.VideoID+" не такой, как просили: "+strings.Join(wrong, "; "))
			return nil
		}
	}
	poster, err := p.poster(ctx, r, refresh, job)
	if err != nil {
		return err
	}
	ch, err := p.store.GetChannel(ctx, r.ChannelID)
	if err != nil {
		return err
	}
	text := "📤 Загружен на YouTube: " + shortsURL(job.VideoID)
	if past {
		text += "\nЕго час уже наступил: " + when(job.PublishAt, ch.Timezone) + "."
	} else {
		text += "\nПо расписанию выйдет " + when(job.PublishAt, ch.Timezone) + "."
	}
	if v.SyntheticMedia == nil {
		text += "\n⚠️ YouTube не подтвердил метку AI: проверьте её в Studio."
	}
	if poster != "" {
		text += "\n⚠️ Обложку поставить не удалось: " + poster + ". Ролик в расписании без неё; поставьте её в Studio."
	}
	// The message first: if it doesn't go out, this comes round again.
	if err := p.notify.Notify(context.WithoutCancel(ctx), r.Moderation.MessageID, text); err != nil {
		return fmt.Errorf("telling the group: %w", err)
	}
	at := p.now()
	job.AnnouncedAt, job.Attempts, job.LastError = &at, 0, ""
	return p.save(ctx, job)
}

// stillThere reads the video back. It reports false if the video is refused or
// has stayed gone: hub has then forgotten it and held the release, which also
// means lifting the hold uploads afresh instead of holding again.
func (p *Publisher) stillThere(ctx context.Context, releaseID, refresh string, job *store.PublishJob) (youtube.Video, bool, error) {
	v, err := p.yt.GetVideo(ctx, refresh, job.VideoID)
	if err != nil {
		return v, false, err
	}
	if !v.Found {
		if !p.missing(job.VideoID) {
			return v, false, &youtube.Error{Retry: true, Message: "YouTube doesn't show video " + job.VideoID + " yet"}
		}
		return v, false, p.forgetAndHold(ctx, releaseID, "ролика "+job.VideoID+" больше нет на YouTube: похоже, его удалили в Studio. Снимите задержку, и hub загрузит его заново.")
	}
	p.seen(job.VideoID)
	if reason := v.Refused(); reason != "" {
		return v, false, p.forgetAndHold(ctx, releaseID, "YouTube не принял ролик "+job.VideoID+": "+reason)
	}
	return v, true, nil
}

func (p *Publisher) forgetAndHold(ctx context.Context, releaseID, reason string) error {
	if err := p.store.ForgetPublication(ctx, releaseID, store.PlatformYouTube); err != nil {
		return err
	}
	p.hold(ctx, releaseID, reason)
	return nil
}

// notAsAsked compares a video with what the upload asked for. A label YouTube
// doesn't report isn't counted: only one it reports as off.
func notAsAsked(v youtube.Video, job *store.PublishJob) []string {
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
	return wrong
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
func (p *Publisher) poster(ctx context.Context, r store.Release, refresh string, job *store.PublishJob) (string, error) {
	if job.ThumbnailSet {
		return "", nil
	}
	var media struct {
		Cover string `json:"cover"`
	}
	if len(r.Media) > 0 {
		if err := json.Unmarshal(r.Media, &media); err != nil {
			return "", fmt.Errorf("decoding the media of release %s: %w", r.ID, err)
		}
	}
	if media.Cover == "" {
		return "в пакете нет обложки", nil
	}
	err := p.sendPoster(ctx, media.Cover, refresh, job)
	switch {
	case err == nil:
		job.ThumbnailSet = true
		return "", p.save(ctx, job)
	case errors.Is(err, youtube.ErrAuthRevoked), errors.Is(err, youtube.ErrClientRefused):
		return "", err
	case youtube.Retryable(err) && job.ThumbnailAttempts+1 < thumbnailAttempts:
		job.ThumbnailAttempts++
		return "", err
	}
	p.log.Error("publisher: the poster didn't go up", "release_id", r.ID, "error", err)
	return err.Error(), nil
}

func (p *Publisher) sendPoster(ctx context.Context, key, refresh string, job *store.PublishJob) error {
	body, size, err := p.media.Open(ctx, key)
	if err != nil {
		return fmt.Errorf("opening the cover: %w", err)
	}
	defer func() { _ = body.Close() }()
	return p.yt.SetThumbnail(ctx, refresh, job.VideoID, body, size, thumbnailType(key))
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

// recheck reads a video back shortly before its slot. YouTube can reject a video
// well after the upload, while it reads as "uploaded", and the owner should
// hear before the slot, not after it.
func (p *Publisher) recheck(ctx context.Context, tokens map[string]string) {
	due, err := p.store.SlotsToRecheck(ctx, p.now().Add(recheckBefore))
	if err != nil {
		p.log.Error("publisher: reading slots to recheck", "error", err)
		return
	}
	for _, d := range due {
		refresh, connected := tokens[d.ChannelID]
		if !connected {
			continue
		}
		videoID := path.Base(d.URL)
		v, err := p.yt.GetVideo(ctx, refresh, videoID)
		switch {
		case errors.Is(err, youtube.ErrAuthRevoked), errors.Is(err, youtube.ErrClientRefused):
			p.accessTrouble(ctx, err, d.ChannelID, tokens)
			continue
		case err != nil:
			p.log.Warn("publisher: rechecking a video, trying again", "release_id", d.ReleaseID, "error", err)
			continue
		case !v.Found:
			if p.missing(videoID) {
				p.forgetOrLog(ctx, d.ReleaseID, "ролика "+videoID+" больше нет на YouTube, а его слот близко. Снимите задержку, и hub загрузит его заново.")
			}
			continue
		}
		p.seen(videoID)
		if reason := v.Refused(); reason != "" {
			p.forgetOrLog(ctx, d.ReleaseID, "YouTube не принял ролик "+videoID+", и в свой слот он не выйдет: "+reason)
			continue
		}
		if err := p.store.MarkSlotRechecked(ctx, d.ReleaseID); err != nil {
			p.log.Error("publisher: recording a recheck", "release_id", d.ReleaseID, "error", err)
		}
	}
}

func (p *Publisher) forgetOrLog(ctx context.Context, releaseID, reason string) {
	if err := p.forgetAndHold(ctx, releaseID, reason); err != nil {
		p.log.Error("publisher: holding a release", "release_id", releaseID, "error", err)
	}
}

// takeDown takes off the schedule what must not go out: its release has been
// closed or held. Nothing is forgotten until YouTube shows the video off the
// schedule, because what's forgotten nobody watches any more. A release the
// upload loop has in hand waits for the next pass: its video may be about to
// appear, and hub would have no record of it.
func (p *Publisher) takeDown(ctx context.Context, tokens map[string]string) {
	list, err := p.store.ReleasesToUnschedule(ctx)
	if err != nil {
		p.log.Error("publisher: reading what to take down", "error", err)
		return
	}
	for _, u := range list {
		refresh, connected := tokens[u.ChannelID]
		if !connected || p.isUploading(u.ReleaseID) {
			continue
		}
		videos, err := p.takeDownOne(ctx, u, refresh)
		switch {
		case err == nil:
		case errors.Is(err, youtube.ErrAuthRevoked), errors.Is(err, youtube.ErrClientRefused):
			p.accessTrouble(ctx, err, u.ChannelID, tokens)
		default:
			p.takeDownFailed(ctx, u, videos, err)
		}
	}
}

// videosBehind lists what hub knows of on YouTube for a release: the link and
// hub's own upload, which differ if a moderator replaced the link.
func (p *Publisher) videosBehind(ctx context.Context, u store.ToUnschedule, job store.PublishJob, refresh string) ([]string, error) {
	var videos []string
	if u.URL != "" {
		videos = append(videos, path.Base(u.URL))
	}
	id := job.VideoID
	if id == "" && job.SessionURI != "" {
		state, err := p.yt.QueryUpload(ctx, refresh, job.SessionURI, job.VideoSize)
		switch {
		case errors.Is(err, youtube.ErrSessionGone), youtube.SessionDead(err):
			// That upload ended with no video.
		case err != nil:
			return videos, err
		case state.Finalising(job.VideoSize):
			return videos, errFinalising
		default:
			id = state.VideoID
		}
	}
	if id != "" && !contains(videos, id) {
		videos = append(videos, id)
	}
	return videos, nil
}

func contains(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

// takeDownOne returns the videos it knows of, for the message if it fails.
func (p *Publisher) takeDownOne(ctx context.Context, u store.ToUnschedule, refresh string) ([]string, error) {
	job, err := p.store.PublishJobOf(ctx, u.ReleaseID)
	if err != nil {
		return nil, err
	}
	videos, err := p.videosBehind(ctx, u, job, refresh)
	if err != nil {
		return videos, err
	}
	var told []string
	for _, id := range videos {
		line, done, err := p.offSchedule(ctx, refresh, id)
		if err != nil {
			return videos, err
		}
		if !done {
			return videos, nil // not seen long enough to call it gone: next pass
		}
		told = append(told, line)
	}
	if len(videos) == 0 && !job.Saved {
		told = append(told, "Слот этого релиза брали не через hub, и ссылки на ролик нет: он может стоять в расписании. Посмотрите в Studio.")
	}
	// The message first: if it doesn't go out, the record stays and this comes
	// round again, with nothing left to change on YouTube.
	if len(told) > 0 && !p.tellOnCard(ctx, u.ReleaseID, strings.Join(told, "\n")) {
		return videos, nil
	}
	if err := p.store.ForgetPublication(ctx, u.ReleaseID, store.PlatformYouTube); err != nil {
		return videos, err
	}
	p.mu.Lock()
	delete(p.failing, u.ReleaseID)
	delete(p.alerted, u.ReleaseID)
	p.mu.Unlock()
	return videos, nil
}

// offSchedule keeps a video from going out and checks that it worked. It returns
// what to tell the group, and false if the video can't be called gone yet.
func (p *Publisher) offSchedule(ctx context.Context, refresh, id string) (string, bool, error) {
	link := shortsURL(id)
	v, err := p.yt.GetVideo(ctx, refresh, id)
	if err != nil {
		return "", false, err
	}
	if v.Found && v.Refused() != "" {
		return "🚫 Ролик не выйдет, YouTube его не принял: " + link, true, nil
	}
	if v.Found && (v.PublishAt != nil || v.PrivacyStatus != "private") {
		if err := p.yt.Unschedule(ctx, refresh, id); err != nil {
			return "", false, err
		}
		if v, err = p.yt.GetVideo(ctx, refresh, id); err != nil {
			return "", false, err
		}
		if v.Found && (v.PublishAt != nil || v.PrivacyStatus != "private") {
			return "", false, errStillScheduled
		}
	}
	if !v.Found {
		if !p.missing(id) {
			return "", false, nil
		}
		return "🚫 Ролика больше нет на YouTube, снимать нечего: " + link, true, nil
	}
	p.seen(id)
	return "🚫 Ролик снят с расписания, на YouTube он сейчас Private: " + link, true, nil
}

// takeDownFailed keeps the release on the list, to try again next pass, and
// tells the group once: at once if YouTube refuses or the slot is near, and
// otherwise if it's still failing after a while.
func (p *Publisher) takeDownFailed(ctx context.Context, u store.ToUnschedule, videos []string, err error) {
	p.log.Warn("publisher: taking a video down, trying again", "release_id", u.ReleaseID, "error", err)
	now := p.now()
	p.mu.Lock()
	since, counting := p.failing[u.ReleaseID]
	if !counting {
		since = now
		p.failing[u.ReleaseID] = now
	}
	told := p.alerted[u.ReleaseID]
	p.mu.Unlock()
	if told {
		return
	}
	survived := errors.Is(err, errStillScheduled)
	wait := takeDownAlertAfter
	if survived {
		wait = surviveAlertAfter
	}
	if youtube.Retryable(err) && now.Sub(since) < wait && u.PublishAt.Sub(now) > takeDownAlertAfter {
		return
	}
	links := make([]string, len(videos))
	for i, id := range videos {
		links[i] = shortsURL(id)
	}
	where := "Ссылки на ролик у hub нет, посмотрите в Studio."
	if len(links) > 0 {
		where = strings.Join(links, " ")
	}
	reason := "hub не может снять ролик с расписания: " + err.Error()
	if survived {
		reason = "hub попросил YouTube снять ролик с расписания, а расписание осталось"
	}
	if p.tellOnCard(ctx, u.ReleaseID, "⚠️ "+reason+". Он выйдет "+when(&u.PublishAt, "")+", если его не снять в Studio. hub продолжает пробовать. "+where) {
		p.mu.Lock()
		p.alerted[u.ReleaseID] = true
		p.mu.Unlock()
	}
}

func (p *Publisher) tellOnCard(ctx context.Context, releaseID, text string) bool {
	var card *int64
	if r, err := p.store.GetRelease(ctx, releaseID); err == nil {
		card = r.Moderation.MessageID
	}
	return p.tell(ctx, card, text)
}
