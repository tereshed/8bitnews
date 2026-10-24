package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

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
		return ReleaseSubmitResponse{}, err
	}

	for i := range sourceRecords {
		sourceRecords[i].ReleaseID = release.ID
	}
	if err := s.store.InsertReleaseSources(ctx, sourceRecords); err != nil {
		return ReleaseSubmitResponse{}, err
	}

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

// checkAndArchiveSources checks and archives every source candidate the
// release's dossier and script reference, returning the records to store
// and the URLs that are broken and have no archive copy — the policy
// blocker in countries/us/policy.md "Источники в ролике".
func (s *Service) checkAndArchiveSources(ctx context.Context, researchDossier, script json.RawMessage) ([]store.ReleaseSource, []string, error) {
	candidates, err := sources.ExtractCandidates(nonEmptyJSON(researchDossier), nonEmptyJSON(script))
	if err != nil {
		return nil, nil, invalidf("extracting sources: %v", err)
	}

	records := make([]store.ReleaseSource, 0, len(candidates))
	var badLinks []string
	now := time.Now().UTC()

	for _, c := range candidates {
		check := s.checker.Check(ctx, c.URL)

		archiveTarget := check.FinalURL
		if archiveTarget == "" {
			archiveTarget = c.URL
		}
		archiveURL, archErr := s.wayback.Save(ctx, archiveTarget)
		if archErr != nil {
			s.log.Warn("archiving source failed", "url", c.URL, "error", archErr)
		}

		if check.Status != sources.StatusOK && archErr != nil {
			badLinks = append(badLinks, c.URL)
		}

		var httpStatus *int
		if check.HTTPStatus != 0 {
			hs := check.HTTPStatus
			httpStatus = &hs
		}

		records = append(records, store.ReleaseSource{
			URL:         c.URL,
			FinalURL:    check.FinalURL,
			Outlet:      c.Outlet,
			Title:       c.Title,
			PublishedAt: c.PublishedAt,
			FactIDs:     c.FactIDs,
			IsPrimary:   c.IsPrimary,
			CheckStatus: string(check.Status),
			HTTPStatus:  httpStatus,
			ArchiveURL:  archiveURL,
			CheckedAt:   now,
		})
	}

	sort.Strings(badLinks)
	return records, badLinks, nil
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
