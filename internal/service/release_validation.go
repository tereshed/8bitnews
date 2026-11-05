package service

import (
	"encoding/json"
	"github.com/tereshed/8bitnews/internal/releasecontract"
)

func validateReleaseManifest(m ReleaseManifest) error {
	if _, err := parseID("research_dossier_artifact_id", m.ResearchDossierArtifactID); err != nil {
		return err
	}
	if m.PolymathsTaskID != "" {
		if _, err := parseID("polymaths_task_id", m.PolymathsTaskID); err != nil {
			return err
		}
	}
	if _, err := parseID("script_artifact_id", m.ScriptArtifactID); err != nil {
		return err
	}
	if _, err := parseID("video_package_artifact_id", m.VideoPackageArtifactID); err != nil {
		return err
	}
	for _, doc := range []struct {
		kind string
		raw  json.RawMessage
	}{
		{"research_dossier", m.ResearchDossier}, {"script", m.Script}, {"video_package", m.VideoPackage},
		{"validation_report", m.ValidationReport}, {"legal_review", m.LegalReview},
	} {
		if err := releasecontract.Validate(doc.kind, doc.raw); err != nil {
			return invalidf("%v", err)
		}
		var channel struct {
			ChannelID string `json:"channel_id"`
		}
		if err := json.Unmarshal(doc.raw, &channel); err != nil {
			return invalidf("%v", err)
		}
		if channel.ChannelID != m.ChannelID {
			return invalidf("%s.channel_id differs from release channel", doc.kind)
		}
	}
	var dossier struct {
		Facts []struct {
			ID string `json:"id"`
		} `json:"facts"`
		Groups []struct {
			ID string `json:"id"`
		} `json:"local_groups"`
		Clusters []struct {
			ID string `json:"id"`
		} `json:"world_clusters"`
	}
	_ = json.Unmarshal(m.ResearchDossier, &dossier)
	known := map[string]bool{}
	addID := func(id string) error {
		if known[id] {
			return invalidf("duplicate research ID %q", id)
		}
		known[id] = true
		return nil
	}
	for _, fact := range dossier.Facts {
		if err := addID(fact.ID); err != nil {
			return err
		}
	}
	for _, group := range dossier.Groups {
		if err := addID(group.ID); err != nil {
			return err
		}
	}
	for _, cluster := range dossier.Clusters {
		if err := addID(cluster.ID); err != nil {
			return err
		}
	}
	var script struct {
		Dossier string `json:"dossier"`
		Scenes  []struct {
			Facts    []string `json:"fact_ids"`
			Groups   []string `json:"group_ids"`
			Clusters []string `json:"cluster_ids"`
		} `json:"scenes"`
	}
	_ = json.Unmarshal(m.Script, &script)
	if script.Dossier != m.ResearchDossierArtifactID {
		return invalidf("script.dossier differs from research_dossier_artifact_id")
	}
	for _, scene := range script.Scenes {
		for _, refs := range [][]string{scene.Facts, scene.Groups, scene.Clusters} {
			for _, id := range refs {
				if !known[id] {
					return invalidf("script references unknown research ID %q", id)
				}
			}
		}
	}
	var video struct {
		Script   string        `json:"script"`
		Media    MediaManifest `json:"media"`
		Validate struct {
			Passed bool `json:"passed"`
			Checks []struct {
				OK bool `json:"ok"`
			} `json:"checks"`
		} `json:"validate"`
	}
	_ = json.Unmarshal(m.VideoPackage, &video)
	if video.Script != m.ScriptArtifactID {
		return invalidf("video_package.script differs from script_artifact_id")
	}
	if video.Media != m.Media {
		return invalidf("video_package.media differs from release media")
	}
	if !video.Validate.Passed || len(video.Validate.Checks) == 0 {
		return invalidf("video_package technical validation did not pass")
	}
	for _, check := range video.Validate.Checks {
		if !check.OK {
			return invalidf("video_package contains a failed technical check")
		}
	}
	var qa struct {
		VideoPackage string `json:"video_package"`
		Technical    struct {
			Passed       bool     `json:"passed"`
			FailedChecks []string `json:"failed_checks"`
		} `json:"technical"`
		Issues []struct {
			Severity string `json:"severity"`
		} `json:"issues"`
	}
	_ = json.Unmarshal(m.ValidationReport, &qa)
	if qa.VideoPackage != m.VideoPackageArtifactID {
		return invalidf("QA reviewed a different video_package")
	}
	if !qa.Technical.Passed || len(qa.Technical.FailedChecks) != 0 {
		return invalidf("QA technical validation did not pass")
	}
	for _, issue := range qa.Issues {
		if issue.Severity == "major" || issue.Severity == "blocker" {
			return invalidf("QA has an unresolved %s issue", issue.Severity)
		}
	}
	var legal struct {
		VideoPackage string `json:"video_package"`
	}
	_ = json.Unmarshal(m.LegalReview, &legal)
	if legal.VideoPackage != m.VideoPackageArtifactID {
		return invalidf("Legal reviewed a different video_package")
	}
	return nil
}
