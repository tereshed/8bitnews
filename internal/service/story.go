package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/url"
	"sort"
	"strings"

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

	// An agent's tool call sometimes sends the string "null" (or "") for a new
	// storyline instead of JSON null: they mean the same. Anything else must be
	// a story id, or the database answers with a cryptic uuid error.
	if req.StoryID != nil {
		if id := strings.TrimSpace(*req.StoryID); id == "" || strings.EqualFold(id, "null") {
			req.StoryID = nil
		} else {
			canonical, err := parseID("story_id", id)
			if err != nil {
				return StoryRegisterResponse{}, invalidf("story_id %q is not a story id: pass an id from story_find_similar, or null for a new storyline", id)
			}
			req.StoryID = &canonical
		}
	}

	if req.StoryID == nil && (req.Title == "" || req.Summary == "") {
		return StoryRegisterResponse{}, invalidf("title and summary are required when story_id is null")
	}
	seedURLs, err := json.Marshal(req.SeedURLs)
	if err != nil {
		return StoryRegisterResponse{}, invalidf("encoding seed_urls: %v", err)
	}
	if req.PolymathsTaskID != "" {
		if _, err := parseID("polymaths_task_id", req.PolymathsTaskID); err != nil {
			return StoryRegisterResponse{}, err
		}
	}
	storyID, devID, err := s.store.RegisterDevelopment(ctx, store.Registration{
		ChannelID: req.ChannelID, StoryID: req.StoryID, Title: req.Title, Summary: req.Summary,
		DevelopmentSummary: req.DevelopmentSummary, ReportedAt: req.ReportedAt, SeedURLs: seedURLs,
		TaskID: req.PolymathsTaskID, EventKey: registrationEventKey(req),
	})
	if errors.Is(err, store.ErrNotFound) {
		return StoryRegisterResponse{}, notFoundf("story not found")
	}
	if errors.Is(err, store.ErrConflict) {
		return StoryRegisterResponse{}, conflictf("story/task reservation belongs to another event or channel")
	}
	if err != nil {
		return StoryRegisterResponse{}, err
	}
	return StoryRegisterResponse{StoryID: storyID, DevelopmentID: devID}, nil
}

// Canonical URLs and event time identify a retry even if an agent rephrases its
// title. Semantic deduplication of differently sourced events remains Research's job.
func registrationEventKey(req StoryRegisterRequest) string {
	unique := map[string]bool{}
	for _, raw := range req.SeedURLs {
		u, err := url.Parse(strings.TrimSpace(raw))
		if err != nil {
			continue
		}
		u.Fragment = ""
		u.Host = strings.ToLower(u.Host)
		query := u.Query()
		for key := range query {
			if strings.HasPrefix(strings.ToLower(key), "utm_") || key == "fbclid" || key == "gclid" {
				query.Del(key)
			}
		}
		u.RawQuery = query.Encode()
		unique[u.String()] = true
	}
	urls := make([]string, 0, len(unique))
	for u := range unique {
		urls = append(urls, u)
	}
	sort.Strings(urls)
	identity := req.ReportedAt.UTC().Format("2006-01-02T15:04:05.999999999Z07:00") + "\n" + strings.Join(urls, "\n")
	if len(urls) == 0 {
		identity += "\n" + strings.ToLower(strings.Join(strings.Fields(req.DevelopmentSummary), " "))
	}
	digest := sha256.Sum256([]byte(identity))
	return hex.EncodeToString(digest[:])
}
