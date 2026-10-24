package sources

import (
	"encoding/json"
	"testing"
)

const testDossier = `{
	"facts": [
		{
			"id": "F1",
			"primary_source": {"url": "https://example.com/f1-primary", "outlet": "Example Wire"},
			"sources": [{"url": "https://example.com/f1-secondary", "outlet": "Example Press"}]
		},
		{
			"id": "F2",
			"sources": [{"url": "https://example.com/f2", "outlet": "Example Daily"}]
		}
	],
	"local_groups": [
		{"id": "L1", "representatives": [{"url": "https://example.com/l1-rep", "outlet": "Local Outlet"}]}
	],
	"world_clusters": [
		{"id": "W1", "representatives": [{"url": "https://example.com/w1-rep", "outlet": "World Outlet"}]}
	]
}`

func TestExtractCandidates_ReferencedOnly(t *testing.T) {
	script := `{"scenes": [{"text": "hook", "fact_ids": ["F1"]}, {"text": "local reaction", "group_ids": ["L1"]}]}`

	candidates, err := ExtractCandidates(json.RawMessage(testDossier), json.RawMessage(script))
	if err != nil {
		t.Fatalf("ExtractCandidates: %v", err)
	}

	urls := map[string]Candidate{}
	for _, c := range candidates {
		urls[c.URL] = c
	}

	if _, ok := urls["https://example.com/f1-primary"]; !ok {
		t.Errorf("expected F1's primary source to be included")
	}
	if _, ok := urls["https://example.com/f1-secondary"]; !ok {
		t.Errorf("expected F1's secondary source to be included")
	}
	if _, ok := urls["https://example.com/f2"]; ok {
		t.Errorf("F2 was not referenced by the script and should be excluded")
	}
	if _, ok := urls["https://example.com/l1-rep"]; !ok {
		t.Errorf("expected L1's representative to be included (L1 mentioned in script)")
	}
	if _, ok := urls["https://example.com/w1-rep"]; ok {
		t.Errorf("W1 was not mentioned by the script and should be excluded")
	}

	if primary := urls["https://example.com/f1-primary"]; !primary.IsPrimary {
		t.Errorf("f1-primary should be marked IsPrimary")
	}
}

func TestExtractCandidates_NoReferences_UsesWholeDossier(t *testing.T) {
	script := `{"scenes": [{"text": "a script with no fact or group ids at all"}]}`

	candidates, err := ExtractCandidates(json.RawMessage(testDossier), json.RawMessage(script))
	if err != nil {
		t.Fatalf("ExtractCandidates: %v", err)
	}
	if len(candidates) != 5 {
		t.Fatalf("len(candidates) = %d, want 5 (every dossier source) when the script references nothing", len(candidates))
	}
}

func TestExtractCandidates_EmptyScript_UsesWholeDossier(t *testing.T) {
	candidates, err := ExtractCandidates(json.RawMessage(testDossier), nil)
	if err != nil {
		t.Fatalf("ExtractCandidates: %v", err)
	}
	if len(candidates) != 5 {
		t.Fatalf("len(candidates) = %d, want 5", len(candidates))
	}
}
