// Package testfixture supplies complete content contracts for integration tests.
package testfixture

import (
	_ "embed"
	"encoding/json"

	"github.com/tereshed/8bitnews/internal/service"
)

//go:embed release.json
var example []byte

// Complete fills the content fields source-pipeline tests formerly omitted.
// Their source URLs, fact references and verdicts remain the scenario inputs.
func Complete(m service.ReleaseManifest) service.ReleaseManifest {
	var base service.ReleaseManifest
	if err := json.Unmarshal(example, &base); err != nil {
		panic(err)
	}
	base.DevelopmentID, base.ChannelID, base.Media = m.DevelopmentID, m.ChannelID, m.Media
	var dossier, supplied map[string]any
	_ = json.Unmarshal(base.ResearchDossier, &dossier)
	_ = json.Unmarshal(m.ResearchDossier, &supplied)
	factTemplate := dossier["facts"].([]any)[0].(map[string]any)
	if facts, ok := supplied["facts"].([]any); ok && len(facts) > 0 {
		var completed []any
		for _, fact := range facts {
			f := clone(factTemplate)
			for k, v := range fact.(map[string]any) {
				f[k] = v
			}
			if sources, ok := f["sources"].([]any); !ok || len(sources) == 0 {
				if primary, ok := f["primary_source"].(map[string]any); ok {
					f["sources"] = []any{primary}
				}
			}
			completed = append(completed, f)
		}
		for len(completed) < 3 {
			f := clone(completed[0].(map[string]any))
			f["id"] = []string{"F901", "F902", "F903"}[len(completed)-1]
			completed = append(completed, f)
		}
		dossier["facts"] = completed
	}
	// Representatives need a real fixture URL too when the script cites all sources.
	firstFact := dossier["facts"].([]any)[0].(map[string]any)
	sourceURL := "https://example.com/ok"
	if primary, ok := firstFact["primary_source"].(map[string]any); ok {
		sourceURL, _ = primary["url"].(string)
	}
	replaceURLs(dossier["local_groups"], sourceURL)
	replaceURLs(dossier["world_clusters"], sourceURL)
	dossier["channel_id"] = m.ChannelID
	base.ResearchDossier = marshal(dossier)
	var script, inputScript map[string]any
	_ = json.Unmarshal(base.Script, &script)
	_ = json.Unmarshal(m.Script, &inputScript)
	refs := []any{}
	if scenes, ok := inputScript["scenes"].([]any); ok {
		for _, scene := range scenes {
			if ids, ok := scene.(map[string]any)["fact_ids"].([]any); ok {
				refs = append(refs, ids...)
			}
		}
	}
	if len(refs) == 0 {
		for _, fact := range dossier["facts"].([]any) {
			refs = append(refs, fact.(map[string]any)["id"])
		}
	}
	for _, scene := range script["scenes"].([]any) {
		scene.(map[string]any)["fact_ids"] = refs
	}
	script["channel_id"] = m.ChannelID
	base.Script = marshal(script)
	for _, field := range []struct {
		dst *json.RawMessage
		src json.RawMessage
	}{
		{&base.VideoPackage, m.VideoPackage}, {&base.ValidationReport, m.ValidationReport}, {&base.LegalReview, m.LegalReview},
	} {
		var doc, override map[string]any
		_ = json.Unmarshal(*field.dst, &doc)
		_ = json.Unmarshal(field.src, &override)
		for k, v := range override {
			doc[k] = v
		}
		doc["channel_id"] = m.ChannelID
		*field.dst = marshal(doc)
	}
	var video map[string]any
	_ = json.Unmarshal(base.VideoPackage, &video)
	video["media"] = m.Media
	base.VideoPackage = marshal(video)
	return base
}

func marshal(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}
func clone(v map[string]any) map[string]any {
	var out map[string]any
	_ = json.Unmarshal(marshal(v), &out)
	return out
}
func replaceURLs(v any, url string) {
	switch v := v.(type) {
	case map[string]any:
		for k, child := range v {
			if k == "url" {
				v[k] = url
			} else {
				replaceURLs(child, url)
			}
		}
	case []any:
		for _, child := range v {
			replaceURLs(child, url)
		}
	}
}
