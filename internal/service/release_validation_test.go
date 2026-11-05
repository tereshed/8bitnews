package service

import (
	"encoding/json"
	"os"
	"testing"
	"time"
)

func TestReleaseManifestRejectsMixedBuildsAndUnvalidatedReports(t *testing.T) {
	fixture, err := os.ReadFile("../testfixture/release.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, field string
		mutate      func(map[string]any)
	}{
		{"wrong channel", "script", func(d map[string]any) { d["channel_id"] = "another-channel" }},
		{"different script", "video_package", func(d map[string]any) { d["script"] = "33333333-3333-4333-8333-333333333333" }},
		{"old QA", "validation_report", func(d map[string]any) { d["video_package"] = "33333333-3333-4333-8333-333333333333" }},
		{"old Legal", "legal_review", func(d map[string]any) { d["video_package"] = "33333333-3333-4333-8333-333333333333" }},
		{"failed studio", "video_package", func(d map[string]any) { d["validate"].(map[string]any)["passed"] = false }},
		{"contradictory checks", "video_package", func(d map[string]any) {
			d["validate"].(map[string]any)["checks"].([]any)[0].(map[string]any)["ok"] = false
		}},
		{"failed QA technical", "validation_report", func(d map[string]any) { d["technical"].(map[string]any)["passed"] = false }},
		{"missing contract", "research_dossier", func(d map[string]any) { delete(d, "facts") }},
		{"unknown fact", "script", func(d map[string]any) { d["scenes"].([]any)[0].(map[string]any)["fact_ids"] = []string{"F99999"} }},
		{"unknown opinion group", "script", func(d map[string]any) { d["scenes"].([]any)[0].(map[string]any)["group_ids"] = []string{"L99999"} }},
		{"unbound media", "video_package", func(d map[string]any) { d["media"] = map[string]any{"video": "another/video"} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var m ReleaseManifest
			_ = json.Unmarshal(fixture, &m)
			if err := validateReleaseManifest(m); err != nil {
				t.Fatalf("invalid baseline fixture: %v", err)
			}
			fields := map[string]*json.RawMessage{"script": &m.Script, "video_package": &m.VideoPackage, "validation_report": &m.ValidationReport, "legal_review": &m.LegalReview, "research_dossier": &m.ResearchDossier}
			var doc map[string]any
			_ = json.Unmarshal(*fields[tc.field], &doc)
			tc.mutate(doc)
			*fields[tc.field], _ = json.Marshal(doc)
			if err := validateReleaseManifest(m); err == nil {
				t.Fatal("invalid manifest accepted")
			}
		})
	}
}

func TestEventIdentityIgnoresTrackingAndHeadlineChanges(t *testing.T) {
	a := StoryRegisterRequest{Title: "First headline", ReportedAt: time.Unix(100, 0), SeedURLs: []string{"https://EXAMPLE.com/article?utm_source=feed#top", "https://other.example/news"}}
	b := a
	b.Title = "A rewritten headline"
	b.SeedURLs = []string{"https://other.example/news", "https://example.com/article"}
	if registrationEventKey(a) != registrationEventKey(b) {
		t.Fatal("tracking/order/title changes created another event")
	}
	b.ReportedAt = b.ReportedAt.Add(time.Hour)
	if registrationEventKey(a) == registrationEventKey(b) {
		t.Fatal("a later development was collapsed into the old event")
	}
}
