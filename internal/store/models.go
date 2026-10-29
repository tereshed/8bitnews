package store

import (
	"encoding/json"
	"time"
)

// Country mirrors the countries table, synced from countries/<cc>/ by
// `hub sync-profiles`.
type Country struct {
	CC        string
	Name      string
	Language  string
	PolicyMD  string
	LegalMD   string
	Data      json.RawMessage
	UpdatedAt time.Time
}

// Channel mirrors the channels table, synced from channels/<id>/ by
// `hub sync-profiles`.
type Channel struct {
	ID         string
	CountryCC  string
	Name       string
	Language   string
	Timezone   string
	Platforms  json.RawMessage
	Format     json.RawMessage
	FormatMD   string
	MetadataMD string
	Data       json.RawMessage
	UpdatedAt  time.Time
}

// Story is a developing storyline for one channel (docs/architecture.md §7).
type Story struct {
	ID        string
	ChannelID string
	Title     string
	Summary   string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// StoryDevelopment is one turn of a Story: what is new about it.
type StoryDevelopment struct {
	ID              string
	StoryID         string
	Summary         string
	ReportedAt      time.Time
	SeedURLs        json.RawMessage
	PolymathsTaskID string
	CreatedAt       time.Time
}

// SimilarStory is a Story match returned by FindSimilarStories, together
// with its developments and the releases made for each of them.
type SimilarStory struct {
	Story        Story
	Score        float64
	Developments []DevelopmentWithReleases
}

// DevelopmentWithReleases pairs a StoryDevelopment with the releases
// produced for it, so story_find_similar can show prior coverage.
type DevelopmentWithReleases struct {
	Development StoryDevelopment
	Releases    []ReleaseSummary
}

// ReleaseSummary is the subset of a Release shown alongside a development in
// story_find_similar and in releases_recent.
type ReleaseSummary struct {
	ID          string
	Status      string
	PublishedAt *time.Time
}

// Release is a submitted video package (docs/architecture.md §7).
type Release struct {
	ID               string
	DevelopmentID    string
	ChannelID        string
	ExperimentID     *string
	ArmID            string
	Status           string
	Media            json.RawMessage
	ResearchDossier  json.RawMessage
	Script           json.RawMessage
	VideoPackage     json.RawMessage
	ValidationReport json.RawMessage
	LegalReview      json.RawMessage
	AllowedPlatforms []string
	CreatedAt        time.Time
	UpdatedAt        time.Time
	Moderation       Moderation
}

// Moderation is a release's card in the moderation group and the
// moderator's decision (E4). ChatID/MessageID are nil until the bot has
// posted the card; By/At stay empty until someone decides.
type Moderation struct {
	ChatID    *int64
	MessageID *int64
	By        string
	At        *time.Time
	Note      string
}

// ReleaseSource is one verified, archived source of a Release
// (docs/architecture.md §7, countries/us/policy.md "Источники в ролике").
type ReleaseSource struct {
	ID              string
	ReleaseID       string
	URL             string
	FinalURL        string
	Outlet          string
	Title           string
	PublishedAt     string
	FactIDs         []string
	IsPrimary       bool
	Paywall         bool
	StateMedia      bool
	CheckStatus     string
	HTTPStatus      *int
	ArchiveURL      string
	TextFingerprint string
	CheckedAt       time.Time
}

// Assignment records the experiment arm given to a development
// (docs/architecture.md §9); in E1 it is always the channel's default arm.
type Assignment struct {
	ID            string
	DevelopmentID string
	ChannelID     string
	ExperimentID  *string
	ArmID         string
	Params        json.RawMessage
	CreatedAt     time.Time
}

// RecentRelease is one entry of releases_recent: enough of a past release
// for Legal to judge whether the new one is too similar.
type RecentRelease struct {
	ID                 string
	Status             string
	CreatedAt          time.Time
	StoryTitle         string
	DevelopmentSummary string
	Script             json.RawMessage
	VideoPackage       json.RawMessage
}
