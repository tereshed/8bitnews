package service

import (
	"bytes"
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/tereshed/8bitnews/internal/sources"
)

// recentArchiveTTL is how long SourcesArchive remembers a source URL it has
// saved, so a dossier approved again after a rework doesn't send the same
// pages to Save Page Now again.
const recentArchiveTTL = 12 * time.Hour

// SourcesArchiveRequest is the body of POST /api/v1/sources/archive: a
// research_dossier Research's reviewer has just approved.
type SourcesArchiveRequest struct {
	ResearchDossier json.RawMessage `json:"research_dossier"`
}

// SourcesArchiveResponse is the 202 answer: how many of the dossier's source
// URLs went to the background job, and how many were left out because they
// were saved, or are being saved, within recentArchiveTTL.
type SourcesArchiveResponse struct {
	Queued        int `json:"queued"`
	SkippedRecent int `json:"skipped_recent"`
}

// SourcesArchive saves a research_dossier's sources to the Wayback Machine in
// the background as soon as Research's dossier is approved, hours before
// release_submit checks them. release_submit refuses a video whose source is
// down or blocks hub at submit time unless a snapshot already exists
// (resolveSource); archiving only then refused finished videos whose pages had
// been alive while Research collected them (rtve.es on 12.11, exteriores.gob.es
// answering 503 to everyone on 13.11).
//
// It takes every source of every fact, local group and world cluster: with no
// script, sources.ExtractCandidates takes the whole dossier, so the set covers
// whatever a script built on it will cite.
//
// The job runs on a fresh context bounded by sourcesBackgroundArchiveTimeout,
// sourcesConcurrency URLs at a time, and saves only what release_submit would
// save (resolveSource, archiveOKSourcesAsync): ok, bot_protected and
// unreachable sources. A dead or soft-404 page
// would only archive its error response, and a blocked URL is one hub refused
// to dial, so no third party is asked to fetch it either.
func (s *Service) SourcesArchive(req SourcesArchiveRequest) (SourcesArchiveResponse, error) {
	dossier := bytes.TrimSpace(req.ResearchDossier)
	if len(dossier) == 0 || bytes.Equal(dossier, []byte("null")) {
		return SourcesArchiveResponse{}, invalidf("research_dossier is required")
	}
	if dossier[0] != '{' {
		return SourcesArchiveResponse{}, invalidf("research_dossier must be a JSON object")
	}
	candidates, err := sources.ExtractCandidates(dossier, nil)
	if err != nil {
		return SourcesArchiveResponse{}, invalidf("extracting sources: %v", err)
	}
	urls := make([]string, 0, len(candidates))
	for _, c := range candidates {
		urls = append(urls, c.URL)
	}
	queued, skipped := s.recentArchives.claim(urls)
	if len(queued) > 0 {
		s.archiveJobs.Add(1)
		go func() {
			defer s.archiveJobs.Done()
			s.archiveSources(queued)
		}()
	}
	return SourcesArchiveResponse{Queued: len(queued), SkippedRecent: skipped}, nil
}

// archiveSources is SourcesArchive's background job. Each URL it was handed
// is released from recentArchives when done: remembered if saved, forgotten
// otherwise, so the next call tries it again.
func (s *Service) archiveSources(urls []string) {
	ctx, cancel := context.WithTimeout(context.Background(), s.sourcesBackgroundArchiveTimeout)
	defer cancel()

	var g errgroup.Group
	g.SetLimit(s.sourcesConcurrency)
	var saved atomic.Int64
	for _, u := range urls {
		g.Go(func() error {
			ok := s.archiveSource(ctx, u)
			s.recentArchives.done(u, ok)
			if ok {
				saved.Add(1)
			}
			return nil
		})
	}
	_ = g.Wait()
	s.log.Info("early source archiving finished", "sources", len(urls), "saved", saved.Load())
}

// archiveSource checks one cited URL and, if its status allows it, asks Save
// Page Now for a snapshot of that URL, not of where it redirects:
// release_submit looks for a copy of the URL the source cites
// (AvailabilityClient.Closest).
func (s *Service) archiveSource(ctx context.Context, url string) bool {
	if ctx.Err() != nil {
		s.log.Warn("early archiving skipped a source: background archive timeout", "url", url)
		return false
	}
	check := s.checker.Check(ctx, url)
	switch check.Status {
	case sources.StatusOK, sources.StatusBotProtected, sources.StatusUnreachable:
		archiveURL, err := s.wayback.Save(ctx, url)
		if err != nil {
			s.log.Warn("early archiving of a source failed", "url", url, "check_status", check.Status, "error", err)
			return false
		}
		s.log.Info("source archived early", "url", url, "check_status", check.Status, "archive_url", archiveURL)
		return true
	default: // dead, soft_404, blocked: see SourcesArchive
		s.log.Warn("source not archived early", "url", url, "check_status", check.Status, "http_status", check.HTTPStatus)
		return false
	}
}

// recentArchives remembers which source URLs SourcesArchive has saved within
// ttl, or is saving now, so repeated calls don't ask Save Page Now for the
// same page again. It is safe for concurrent use.
type recentArchives struct {
	mu  sync.Mutex
	ttl time.Duration
	now func() time.Time
	// savedAt is when each URL was saved; the zero time means it is being
	// saved now.
	savedAt map[string]time.Time
}

func newRecentArchives(ttl time.Duration) *recentArchives {
	return &recentArchives{ttl: ttl, now: time.Now, savedAt: map[string]time.Time{}}
}

// claim marks the URLs of urls that are neither saved within ttl nor being
// saved now as being saved, and returns them with the number of the others.
func (r *recentArchives) claim(urls []string) (fresh []string, skipped int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := r.now()
	for u, at := range r.savedAt {
		if !at.IsZero() && now.Sub(at) >= r.ttl {
			delete(r.savedAt, u)
		}
	}
	for _, u := range urls {
		if _, ok := r.savedAt[u]; ok {
			skipped++
			continue
		}
		r.savedAt[u] = time.Time{}
		fresh = append(fresh, u)
	}
	return fresh, skipped
}

// done ends the save of a claimed URL: a saved one is remembered for ttl, an
// unsaved one is forgotten.
func (r *recentArchives) done(url string, saved bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if saved {
		r.savedAt[url] = r.now()
		return
	}
	delete(r.savedAt, url)
}
