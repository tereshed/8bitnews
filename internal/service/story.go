package service

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/tereshed/8bitnews/internal/store"
)

// StoryFindSimilar finds storylines of a channel whose title/summary are
// close to the given candidate, each with its full development history and
// the releases made for every development, so editor can decide whether
// there is something new and fill story_brief.prior_coverage (task E1 §3).
func (s *Service) StoryFindSimilar(ctx context.Context, req StoryFindSimilarRequest) (StoryFindSimilarResponse, error) {
	if req.ChannelID == "" {
		return StoryFindSimilarResponse{}, invalidf("channel_id is required")
	}
	if req.Title == "" && req.Summary == "" {
		return StoryFindSimilarResponse{}, invalidf("title or summary is required")
	}
	days := req.Days
	if days <= 0 {
		days = defaultSimilarDays
	}

	matches, err := s.store.FindSimilarStories(ctx, req.ChannelID, req.Title, req.Summary, days, 10)
	if err != nil {
		return StoryFindSimilarResponse{}, err
	}

	resp := StoryFindSimilarResponse{Stories: make([]SimilarStoryView, 0, len(matches))}
	for _, m := range matches {
		resp.Stories = append(resp.Stories, toSimilarStoryView(m))
	}
	return resp, nil
}

func toSimilarStoryView(m store.SimilarStory) SimilarStoryView {
	view := SimilarStoryView{
		StoryID: m.Story.ID,
		Title:   m.Story.Title,
		Summary: m.Story.Summary,
		Score:   m.Score,
	}
	for _, d := range m.Developments {
		view.Developments = append(view.Developments, toDevelopmentView(d))
	}
	return view
}

func toDevelopmentView(d store.DevelopmentWithReleases) DevelopmentView {
	dv := DevelopmentView{
		DevelopmentID: d.Development.ID,
		Summary:       d.Development.Summary,
		ReportedAt:    d.Development.ReportedAt,
	}
	for _, r := range d.Releases {
		dv.Releases = append(dv.Releases, ReleaseBrief{
			ReleaseID:   r.ID,
			Status:      r.Status,
			PublishedAt: r.PublishedAt,
		})
	}
	return dv
}

// StoryRegister registers a new storyline (story_id: null) with its first
// development, or adds a new development to an existing storyline
// (task E1 §3).
func (s *Service) StoryRegister(ctx context.Context, req StoryRegisterRequest) (StoryRegisterResponse, error) {
	if req.ChannelID == "" {
		return StoryRegisterResponse{}, invalidf("channel_id is required")
	}
	if req.DevelopmentSummary == "" {
		return StoryRegisterResponse{}, invalidf("development_summary is required")
	}
	if req.ReportedAt.IsZero() {
		return StoryRegisterResponse{}, invalidf("reported_at is required")
	}

	var storyID string
	if req.StoryID == nil {
		if req.Title == "" || req.Summary == "" {
			return StoryRegisterResponse{}, invalidf("title and summary are required when story_id is null")
		}
		created, err := s.store.CreateStory(ctx, req.ChannelID, req.Title, req.Summary)
		if err != nil {
			return StoryRegisterResponse{}, err
		}
		storyID = created.ID
	} else {
		existing, err := s.store.GetStory(ctx, *req.StoryID)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				return StoryRegisterResponse{}, notFoundf("story %q not found", *req.StoryID)
			}
			return StoryRegisterResponse{}, err
		}
		if existing.ChannelID != req.ChannelID {
			return StoryRegisterResponse{}, invalidf("story %q belongs to channel %q, not %q", existing.ID, existing.ChannelID, req.ChannelID)
		}
		storyID = existing.ID
	}

	seedURLs, err := json.Marshal(req.SeedURLs)
	if err != nil {
		return StoryRegisterResponse{}, invalidf("encoding seed_urls: %v", err)
	}

	dev, err := s.store.CreateDevelopment(ctx, storyID, req.DevelopmentSummary, req.ReportedAt, seedURLs, req.PolymathsTaskID)
	if err != nil {
		return StoryRegisterResponse{}, err
	}
	if err := s.store.TouchStory(ctx, storyID); err != nil {
		return StoryRegisterResponse{}, err
	}

	return StoryRegisterResponse{StoryID: storyID, DevelopmentID: dev.ID}, nil
}
