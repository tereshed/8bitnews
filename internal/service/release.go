package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/tereshed/8bitnews/internal/sources"
	"github.com/tereshed/8bitnews/internal/store"
)

type validationReportDoc struct {
	Verdict string `json:"verdict"`
}

type legalReviewPlatform struct {
	Verdict string `json:"verdict"`
}

type legalReviewDoc struct {
	Mode      string                         `json:"mode"`
	Verdict   string                         `json:"verdict"`
	Platforms map[string]legalReviewPlatform `json:"platforms"`
}

// ReleaseSubmit validates a release manifest and, if it passes every check,
// creates the release in status "submitted" with its sources checked and
// archived (task E1 §3, countries/us/policy.md "Источники в ролике").
func (s *Service) ReleaseSubmit(ctx context.Context, manifest ReleaseManifest) (ReleaseSubmitResponse, error) {
	if manifest.DevelopmentID == "" {
		return ReleaseSubmitResponse{}, invalidf("development_id is required")
	}
	if manifest.ChannelID == "" {
		return ReleaseSubmitResponse{}, invalidf("channel_id is required")
	}

	dev, err := s.store.GetDevelopment(ctx, manifest.DevelopmentID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return ReleaseSubmitResponse{}, notFoundf("development %q not found", manifest.DevelopmentID)
		}
		return ReleaseSubmitResponse{}, err
	}

	story, err := s.store.GetStory(ctx, dev.StoryID)
	if err != nil {
		return ReleaseSubmitResponse{}, err
	}
	if story.ChannelID != manifest.ChannelID {
		return ReleaseSubmitResponse{}, invalidf("development %q belongs to channel %q, not %q", manifest.DevelopmentID, story.ChannelID, manifest.ChannelID)
	}

	active, err := s.store.ActiveReleaseExists(ctx, manifest.DevelopmentID)
	if err != nil {
		return ReleaseSubmitResponse{}, err
	}
	if active {
		return ReleaseSubmitResponse{}, conflictf("development %q already has an active release (not rejected, changes_requested or failed)", manifest.DevelopmentID)
	}

	if err := s.checkMediaObjects(ctx, manifest.Media); err != nil {
		return ReleaseSubmitResponse{}, err
	}

	var validation validationReportDoc
	if err := json.Unmarshal(nonEmptyJSON(manifest.ValidationReport), &validation); err != nil {
		return ReleaseSubmitResponse{}, invalidf("parsing validation_report: %v", err)
	}
	if validation.Verdict != "pass" {
		return ReleaseSubmitResponse{}, invalidf("validation_report.verdict is %q, must be %q", validation.Verdict, "pass")
	}

	var legal legalReviewDoc
	if err := json.Unmarshal(nonEmptyJSON(manifest.LegalReview), &legal); err != nil {
		return ReleaseSubmitResponse{}, invalidf("parsing legal_review: %v", err)
	}
	if legal.Mode != "final" {
		return ReleaseSubmitResponse{}, invalidf("legal_review.mode is %q, must be %q", legal.Mode, "final")
	}
	if legal.Verdict != "pass" {
		return ReleaseSubmitResponse{}, invalidf("legal_review.verdict is %q, must be %q", legal.Verdict, "pass")
	}
	allowedPlatforms := make([]string, 0, len(legal.Platforms))
	for platform, p := range legal.Platforms {
		if p.Verdict == "pass" {
			allowedPlatforms = append(allowedPlatforms, platform)
		}
	}
	sort.Strings(allowedPlatforms)

	sourceRecords, badLinks, err := s.checkAndArchiveSources(ctx, manifest.ResearchDossier, manifest.Script)
	if err != nil {
		return ReleaseSubmitResponse{}, err
	}
	if len(badLinks) > 0 {
		return ReleaseSubmitResponse{}, invalidf("broken sources with no archive copy: %s", strings.Join(badLinks, ", "))
	}

	mediaJSON, err := json.Marshal(manifest.Media)
	if err != nil {
		return ReleaseSubmitResponse{}, invalidf("encoding media: %v", err)
	}

	release, err := s.store.CreateRelease(ctx, store.NewReleaseParams{
		DevelopmentID:    manifest.DevelopmentID,
		ChannelID:        manifest.ChannelID,
		ArmID:            defaultArmID,
		Media:            mediaJSON,
		ResearchDossier:  nonEmptyJSON(manifest.ResearchDossier),
		Script:           nonEmptyJSON(manifest.Script),
		VideoPackage:     nonEmptyJSON(manifest.VideoPackage),
		ValidationReport: nonEmptyJSON(manifest.ValidationReport),
		LegalReview:      nonEmptyJSON(manifest.LegalReview),
		AllowedPlatforms: allowedPlatforms,
	})
	if err != nil {
		if errors.Is(err, store.ErrConflict) {
			return ReleaseSubmitResponse{}, conflictf("development %q already has an active release", manifest.DevelopmentID)
		}
		return ReleaseSubmitResponse{}, err
	}

	for i := range sourceRecords {
		sourceRecords[i].ReleaseID = release.ID
	}
	if err := s.store.InsertReleaseSources(ctx, sourceRecords); err != nil {
		return ReleaseSubmitResponse{}, err
	}

	s.archiveOKSourcesAsync(release.ID, sourceRecords)

	views := make([]ReleaseSourceView, 0, len(sourceRecords))
	for _, src := range sourceRecords {
		views = append(views, toReleaseSourceView(src))
	}

	return ReleaseSubmitResponse{
		ReleaseID:        release.ID,
		Status:           release.Status,
		AllowedPlatforms: allowedPlatforms,
		Sources:          views,
	}, nil
}

func (s *Service) checkMediaObjects(ctx context.Context, media MediaManifest) error {
	keys := map[string]string{
		"video":     media.Video,
		"subtitles": media.Subtitles,
		"cover":     media.Cover,
	}
	var missing []string
	for field, key := range keys {
		if key == "" {
			missing = append(missing, fmt.Sprintf("%s (empty object key)", field))
			continue
		}
		ok, err := s.media.Exists(ctx, key)
		if err != nil {
			return err
		}
		if !ok {
			missing = append(missing, key)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return invalidf("media objects not found in S3: %s", strings.Join(missing, ", "))
	}
	return nil
}

// sourceOutcome is one candidate's resolveSource result: the release_source
// row to store, and whether the policy in countries/us/policy.md "Источники
// в ролике" treats it as broken (no live page and no qualifying archive
// copy).
type sourceOutcome struct {
	record store.ReleaseSource
	bad    bool
}

// resolveSource checks one candidate source and, synchronously, does
// exactly the archive lookups the policy decision needs — no more:
//
//   - ok: passes outright. Archiving is deferred (see
//     archiveOKSourcesAsync): it is not needed to decide pass/fail, and
//     Save Page Now can take tens of seconds per URL.
//   - bot_protected / unreachable: the hub cannot see the page right now,
//     but may have seen it while researching, so a Save Page Now capture
//     made *right now* is acceptable proof it still exists. Try that
//     first; if it fails, fall back to an existing Wayback snapshot.
//   - dead / soft_404: the server is actively saying "not found" (or
//     redirecting away from it). A Save Page Now capture made now would
//     only archive that error response, so it is not attempted at all —
//     only a *pre-existing* snapshot (recorded with HTTP 200 before the
//     page went bad) counts.
//   - blocked: the URL's scheme or destination address is one this
//     process refuses to connect to at all (SSRF protection). It never
//     passes, and no archive lookup is attempted either — the hub must
//     not ask a third party to fetch an address it itself refused to
//     dial.
func (s *Service) resolveSource(ctx context.Context, c sources.Candidate, now time.Time) sourceOutcome {
	check := s.checker.Check(ctx, c.URL)

	var httpStatus *int
	if check.HTTPStatus != 0 {
		hs := check.HTTPStatus
		httpStatus = &hs
	}

	rec := store.ReleaseSource{
		URL:         c.URL,
		FinalURL:    check.FinalURL,
		Outlet:      c.Outlet,
		Title:       c.Title,
		PublishedAt: c.PublishedAt,
		FactIDs:     c.FactIDs,
		IsPrimary:   c.IsPrimary,
		CheckStatus: string(check.Status),
		HTTPStatus:  httpStatus,
		CheckedAt:   now,
	}

	archiveTarget := check.FinalURL
	if archiveTarget == "" {
		archiveTarget = c.URL
	}

	switch check.Status {
	case sources.StatusOK:
		return sourceOutcome{record: rec}

	case sources.StatusBotProtected, sources.StatusUnreachable:
		archiveURL, err := s.wayback.Save(ctx, archiveTarget)
		if err == nil {
			rec.ArchiveURL = archiveURL
			return sourceOutcome{record: rec}
		}
		s.log.Warn("archiving source failed, falling back to existing snapshot lookup", "url", c.URL, "error", err)
		if archiveURL, found, err := s.availability.Closest(ctx, archiveTarget); err != nil {
			s.log.Warn("availability lookup failed", "url", c.URL, "error", err)
		} else if found {
			rec.ArchiveURL = archiveURL
			return sourceOutcome{record: rec}
		}
		return sourceOutcome{record: rec, bad: true}

	case sources.StatusDead, sources.StatusSoft404:
		if archiveURL, found, err := s.availability.Closest(ctx, archiveTarget); err != nil {
			s.log.Warn("availability lookup failed", "url", c.URL, "error", err)
		} else if found {
			rec.ArchiveURL = archiveURL
			return sourceOutcome{record: rec}
		}
		return sourceOutcome{record: rec, bad: true}

	default: // sources.StatusBlocked
		return sourceOutcome{record: rec, bad: true}
	}
}

// checkAndArchiveSources checks and, where the policy decision needs it,
// synchronously archives every source candidate the release's dossier and
// script reference. It returns the records to store and the URLs that are
// broken with no qualifying archive copy — the policy blocker in
// countries/us/policy.md "Источники в ролике".
//
// Checks run with bounded concurrency (sourcesConcurrency) under one
// overall deadline (sourcesCheckDeadline): a real dossier can reference
// dozens of URLs, and Save Page Now alone can take tens of seconds per
// call, so running them one at a time would make release_submit time out
// long before an agent's own MCP call timeout.
func (s *Service) checkAndArchiveSources(ctx context.Context, researchDossier, script json.RawMessage) ([]store.ReleaseSource, []string, error) {
	candidates, err := sources.ExtractCandidates(nonEmptyJSON(researchDossier), nonEmptyJSON(script))
	if err != nil {
		return nil, nil, invalidf("extracting sources: %v", err)
	}

	deadlineCtx, cancel := context.WithTimeout(ctx, s.sourcesCheckDeadline)
	defer cancel()

	now := time.Now().UTC()
	outcomes := make([]sourceOutcome, len(candidates))

	g, gctx := errgroup.WithContext(deadlineCtx)
	g.SetLimit(s.sourcesConcurrency)
	for i, c := range candidates {
		i, c := i, c
		g.Go(func() error {
			// Every worker returns nil: one slow/broken source must not
			// cancel the others via errgroup's own first-error
			// cancellation. The shared deadlineCtx is what bounds the
			// whole batch.
			outcomes[i] = s.resolveSource(gctx, c, now)
			return nil
		})
	}
	_ = g.Wait()

	records := make([]store.ReleaseSource, 0, len(outcomes))
	var badLinks []string
	for _, o := range outcomes {
		records = append(records, o.record)
		if o.bad {
			badLinks = append(badLinks, o.record.URL)
		}
	}
	sort.Strings(badLinks)
	return records, badLinks, nil
}

// archiveOKSourcesAsync archives every already-passing "ok" source of a
// just-created release via Save Page Now in the background, writing each
// result back into release_sources as it completes. This runs after
// release_submit has already returned the release to its caller (task E1
// revision point 4): archiving is not needed to decide pass/fail for an
// "ok" source, and Save Page Now can take tens of seconds per URL — doing
// it synchronously for every live source would make release_submit time
// out on a dossier with dozens of them.
//
// It uses a fresh background context (not ctx from the request, which will
// be canceled once the HTTP/MCP call returns) bounded by
// sourcesBackgroundArchiveTimeout, so a hung Save Page Now call cannot leak
// goroutines forever.
func (s *Service) archiveOKSourcesAsync(releaseID string, records []store.ReleaseSource) {
	var toArchive []store.ReleaseSource
	for _, r := range records {
		if r.CheckStatus == string(sources.StatusOK) {
			toArchive = append(toArchive, r)
		}
	}
	if len(toArchive) == 0 {
		return
	}

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), s.sourcesBackgroundArchiveTimeout)
		defer cancel()

		g, gctx := errgroup.WithContext(ctx)
		g.SetLimit(s.sourcesConcurrency)
		for _, r := range toArchive {
			r := r
			g.Go(func() error {
				target := r.FinalURL
				if target == "" {
					target = r.URL
				}
				archiveURL, err := s.wayback.Save(gctx, target)
				if err != nil {
					s.log.Warn("background archiving failed", "release_id", releaseID, "url", r.URL, "error", err)
					return nil
				}
				if err := s.store.UpdateReleaseSourceArchiveURL(gctx, releaseID, r.URL, archiveURL); err != nil {
					s.log.Warn("saving background archive url failed", "release_id", releaseID, "url", r.URL, "error", err)
				}
				return nil
			})
		}
		_ = g.Wait()
	}()
}

func toReleaseSourceView(src store.ReleaseSource) ReleaseSourceView {
	return ReleaseSourceView{
		URL:         src.URL,
		FinalURL:    src.FinalURL,
		CheckStatus: src.CheckStatus,
		ArchiveURL:  src.ArchiveURL,
		FactIDs:     src.FactIDs,
		IsPrimary:   src.IsPrimary,
	}
}

// ReleaseGet returns a release and its checked sources (task E1 §3).
func (s *Service) ReleaseGet(ctx context.Context, req ReleaseGetRequest) (ReleaseView, error) {
	if req.ReleaseID == "" {
		return ReleaseView{}, invalidf("release_id is required")
	}
	r, err := s.store.GetRelease(ctx, req.ReleaseID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return ReleaseView{}, notFoundf("release %q not found", req.ReleaseID)
		}
		return ReleaseView{}, err
	}
	srcs, err := s.store.ListReleaseSources(ctx, req.ReleaseID)
	if err != nil {
		return ReleaseView{}, err
	}

	var media MediaManifest
	if err := json.Unmarshal(nonEmptyJSON(r.Media), &media); err != nil {
		return ReleaseView{}, fmt.Errorf("decoding stored media for release %s: %w", r.ID, err)
	}

	views := make([]ReleaseSourceView, 0, len(srcs))
	for _, src := range srcs {
		views = append(views, toReleaseSourceView(src))
	}

	return ReleaseView{
		ID:               r.ID,
		DevelopmentID:    r.DevelopmentID,
		ChannelID:        r.ChannelID,
		Status:           r.Status,
		Media:            media,
		ResearchDossier:  r.ResearchDossier,
		Script:           r.Script,
		VideoPackage:     r.VideoPackage,
		ValidationReport: r.ValidationReport,
		LegalReview:      r.LegalReview,
		AllowedPlatforms: r.AllowedPlatforms,
		Sources:          views,
		CreatedAt:        r.CreatedAt,
		UpdatedAt:        r.UpdatedAt,
	}, nil
}

// ReleasesRecent returns a channel's most recent releases, newest first, so
// Legal can compare a new release against them for un-originality
// (task E1 §3).
func (s *Service) ReleasesRecent(ctx context.Context, req ReleasesRecentRequest) (ReleasesRecentResponse, error) {
	if req.ChannelID == "" {
		return ReleasesRecentResponse{}, invalidf("channel_id is required")
	}
	n := req.N
	if n <= 0 {
		n = defaultRecentN
	}

	releases, err := s.store.ListRecentReleases(ctx, req.ChannelID, n)
	if err != nil {
		return ReleasesRecentResponse{}, err
	}

	views := make([]RecentReleaseView, 0, len(releases))
	for _, r := range releases {
		views = append(views, RecentReleaseView{
			ReleaseID:          r.ID,
			Status:             r.Status,
			CreatedAt:          r.CreatedAt,
			StoryTitle:         r.StoryTitle,
			DevelopmentSummary: r.DevelopmentSummary,
			Script:             r.Script,
			VideoPackage:       r.VideoPackage,
		})
	}
	return ReleasesRecentResponse{Releases: views}, nil
}

func nonEmptyJSON(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return json.RawMessage("{}")
	}
	return raw
}
