// Package service implements the 8 hub operations agents call
// (docs/architecture.md §6, task E1 §3) once, behind REST and MCP
// transports that share these exact request/response types.
package service

import (
	"encoding/json"
	"time"
)

// ChannelView is the response to channel_get: the channel profile plus the
// text of its format.md.
type ChannelView struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Country   string          `json:"country"`
	Language  string          `json:"language"`
	Timezone  string          `json:"timezone"`
	Platforms json.RawMessage `json:"platforms"`
	Format    json.RawMessage `json:"format"`
	FormatMD  string          `json:"format_md"`
	Profile   json.RawMessage `json:"profile"`
}

// StoryFindSimilarRequest is the input to story_find_similar.
type StoryFindSimilarRequest struct {
	ChannelID string `json:"channel_id" jsonschema:"the channel to search storylines for"`
	Title     string `json:"title" jsonschema:"candidate story title"`
	Summary   string `json:"summary" jsonschema:"candidate story summary"`
	Days      int    `json:"days,omitempty" jsonschema:"how many days back a storyline must have a development to match; default 30"`
}

// StoryFindSimilarResponse is the output of story_find_similar: matching
// storylines, each with its full development history and the releases made
// for each development, so editor can fill story_brief.prior_coverage.
type StoryFindSimilarResponse struct {
	Stories []SimilarStoryView `json:"stories"`
}

// SimilarStoryView is one storyline match.
type SimilarStoryView struct {
	StoryID      string            `json:"story_id"`
	Title        string            `json:"title"`
	Summary      string            `json:"summary"`
	Score        float64           `json:"score"`
	Developments []DevelopmentView `json:"developments"`
}

// DevelopmentView is one turn of a storyline.
type DevelopmentView struct {
	DevelopmentID string         `json:"development_id"`
	Summary       string         `json:"summary"`
	ReportedAt    time.Time      `json:"reported_at"`
	Releases      []ReleaseBrief `json:"releases"`
}

// ReleaseBrief is the release info shown next to a development in
// story_find_similar.
type ReleaseBrief struct {
	ReleaseID   string     `json:"release_id"`
	Status      string     `json:"status"`
	PublishedAt *time.Time `json:"published_at,omitempty"`
}

// StoryRegisterRequest is the input to story_register. StoryID nil
// registers a new storyline with its first development; a non-nil StoryID
// adds a new development (turn) to that existing storyline.
type StoryRegisterRequest struct {
	ChannelID          string    `json:"channel_id" jsonschema:"the channel registering this story"`
	StoryID            *string   `json:"story_id" jsonschema:"existing storyline id, or null to start a new storyline"`
	Title              string    `json:"title,omitempty" jsonschema:"storyline title; used only when story_id is null"`
	Summary            string    `json:"summary,omitempty" jsonschema:"storyline summary; used only when story_id is null"`
	DevelopmentSummary string    `json:"development_summary" jsonschema:"what is new in this turn of the story"`
	ReportedAt         time.Time `json:"reported_at" jsonschema:"when this development was first reported, UTC"`
	PolymathsTaskID    string    `json:"polymaths_task_id,omitempty" jsonschema:"PolyMaths task that registered this development"`
	SeedURLs           []string  `json:"seed_urls" jsonschema:"seed source URLs for this development"`
}

// StoryRegisterResponse is the output of story_register.
type StoryRegisterResponse struct {
	StoryID       string `json:"story_id"`
	DevelopmentID string `json:"development_id"`
}

// ExperimentAssignRequest is the input to experiment_assign.
type ExperimentAssignRequest struct {
	ChannelID     string `json:"channel_id" jsonschema:"the channel the video belongs to"`
	DevelopmentID string `json:"development_id" jsonschema:"the story development this video is about"`
}

// ExperimentAssignResponse is the output of experiment_assign. In E1 there
// are no experiments yet, so it always returns the channel's default arm;
// the shape is final so E6 can implement real assignment without changing
// callers (docs/architecture.md §9).
type ExperimentAssignResponse struct {
	ExperimentID *string         `json:"experiment_id"`
	ArmID        string          `json:"arm_id"`
	Params       json.RawMessage `json:"params"`
}

// MediaUploadURLRequest is the input to media_upload_url.
type MediaUploadURLRequest struct {
	DevelopmentID string `json:"development_id" jsonschema:"the story development this media belongs to"`
	Filename      string `json:"filename" jsonschema:"original file name, used to derive the object key"`
	ContentType   string `json:"content_type" jsonschema:"MIME type of the upload"`
}

// MediaUploadURLResponse is the output of media_upload_url: a presigned PUT
// URL and the S3 object key it uploads to.
type MediaUploadURLResponse struct {
	UploadURL string `json:"upload_url"`
	ObjectKey string `json:"object_key"`
}

// MediaManifest is the set of media object keys release_submit requires.
type MediaManifest struct {
	Video     string `json:"video" jsonschema:"S3 object key of the rendered video"`
	Subtitles string `json:"subtitles" jsonschema:"S3 object key of the subtitles file"`
	Cover     string `json:"cover" jsonschema:"S3 object key of the cover image"`
}

// ReleaseManifest is the input to release_submit (task E1 §3).
type ReleaseManifest struct {
	DevelopmentID    string          `json:"development_id" jsonschema:"the story development this release is for"`
	ChannelID        string          `json:"channel_id" jsonschema:"the channel this release is for"`
	Media            MediaManifest   `json:"media" jsonschema:"S3 object keys of the release media"`
	ResearchDossier  json.RawMessage `json:"research_dossier" jsonschema:"research_dossier contract output"`
	Script           json.RawMessage `json:"script" jsonschema:"script contract output"`
	VideoPackage     json.RawMessage `json:"video_package" jsonschema:"video_package contract output"`
	ValidationReport json.RawMessage `json:"validation_report" jsonschema:"validation_report contract output"`
	LegalReview      json.RawMessage `json:"legal_review" jsonschema:"final-mode legal_review contract output"`
}

// ReleaseSourceView is one checked, archived source attached to a release.
type ReleaseSourceView struct {
	URL         string   `json:"url"`
	FinalURL    string   `json:"final_url"`
	CheckStatus string   `json:"check_status"`
	ArchiveURL  string   `json:"archive_url"`
	FactIDs     []string `json:"fact_ids"`
	IsPrimary   bool     `json:"is_primary"`
}

// ReleaseSubmitResponse is the output of release_submit.
type ReleaseSubmitResponse struct {
	ReleaseID        string              `json:"release_id"`
	Status           string              `json:"status"`
	AllowedPlatforms []string            `json:"allowed_platforms"`
	Sources          []ReleaseSourceView `json:"sources"`
}

// ReleaseView is the output of release_get.
type ReleaseView struct {
	ID               string              `json:"id"`
	DevelopmentID    string              `json:"development_id"`
	ChannelID        string              `json:"channel_id"`
	Status           string              `json:"status"`
	Media            MediaManifest       `json:"media"`
	ResearchDossier  json.RawMessage     `json:"research_dossier"`
	Script           json.RawMessage     `json:"script"`
	VideoPackage     json.RawMessage     `json:"video_package"`
	ValidationReport json.RawMessage     `json:"validation_report"`
	LegalReview      json.RawMessage     `json:"legal_review"`
	AllowedPlatforms []string            `json:"allowed_platforms"`
	Sources          []ReleaseSourceView `json:"sources"`
	CreatedAt        time.Time           `json:"created_at"`
	UpdatedAt        time.Time           `json:"updated_at"`
}

// ReleasesRecentRequest is the input to releases_recent.
type ReleasesRecentRequest struct {
	ChannelID string `json:"channel_id" jsonschema:"the channel to list releases for"`
	N         int    `json:"n,omitempty" jsonschema:"how many releases to return; default 10"`
}

// ReleasesRecentResponse is the output of releases_recent, used by Legal to
// check a new release against the channel's recent history for
// un-originality (docs/architecture.md §6).
type ReleasesRecentResponse struct {
	Releases []RecentReleaseView `json:"releases"`
}

// RecentReleaseView is one entry of releases_recent.
type RecentReleaseView struct {
	ReleaseID          string          `json:"release_id"`
	Status             string          `json:"status"`
	CreatedAt          time.Time       `json:"created_at"`
	StoryTitle         string          `json:"story_title"`
	DevelopmentSummary string          `json:"development_summary"`
	Script             json.RawMessage `json:"script"`
	VideoPackage       json.RawMessage `json:"video_package"`
}

// IDRequest is the shared input shape for release_get (and any future
// single-id lookup), named explicitly per field below to keep tool schemas
// self-descriptive.
type ReleaseGetRequest struct {
	ReleaseID string `json:"release_id" jsonschema:"the release id"`
}

// ChannelGetRequest is the input to channel_get.
type ChannelGetRequest struct {
	ChannelID string `json:"channel_id" jsonschema:"the channel id"`
}
