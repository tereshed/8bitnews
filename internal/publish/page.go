package publish

import (
	"encoding/json"
	"sort"
	"strings"
	"time"

	"github.com/tereshed/8bitnews/internal/store"
)

type scene struct {
	Narration  string   `json:"narration"`
	FactIDs    []string `json:"fact_ids"`
	GroupIDs   []string `json:"group_ids"`
	ClusterIDs []string `json:"cluster_ids"`
}

type dossierDoc struct {
	Facts []struct {
		ID            string        `json:"id"`
		PrimarySource *dossierLink  `json:"primary_source"`
		Sources       []dossierLink `json:"sources"`
	} `json:"facts"`
	LocalGroups   []dossierGroup `json:"local_groups"`
	WorldClusters []dossierGroup `json:"world_clusters"`
}

type dossierLink struct {
	URL string `json:"url"`
}

type dossierGroup struct {
	ID              string        `json:"id"`
	Representatives []dossierLink `json:"representatives"`
}

// Page is a video's sources page (metadata.md «Страница источников»): what
// the video says, line by line, and where each line comes from.
type Page struct {
	Channel    string
	Title      string
	Summary    string
	Date       string
	Statements []Statement
	// Others are the checked sources no scene cites by itself: the page
	// lists every source of the video (policy.md).
	Others      []Source
	Corrections string // the contact; empty when none is set
	ChannelURL  string
}

// Statement is one scene's narration and the checked sources behind it.
type Statement struct {
	Text    string
	Sources []Source
}

// BuildPage writes the sources page of a release. Only sources hub checked
// at release_submit appear: the list is hub's, not the model's (policy.md).
func BuildPage(in Input) Page {
	r := in.Release
	var script scriptDoc
	_ = json.Unmarshal(r.Script, &script)
	var dossier dossierDoc
	_ = json.Unmarshal(r.ResearchDossier, &dossier)

	byID := map[string][]string{}
	for _, f := range dossier.Facts {
		if f.PrimarySource != nil {
			byID[f.ID] = append(byID[f.ID], f.PrimarySource.URL)
		}
		for _, s := range f.Sources {
			byID[f.ID] = append(byID[f.ID], s.URL)
		}
	}
	for _, g := range append(append([]dossierGroup{}, dossier.LocalGroups...), dossier.WorldClusters...) {
		for _, s := range g.Representatives {
			byID[g.ID] = append(byID[g.ID], s.URL)
		}
	}

	checked := sourcesOf(in.Sources, in.Country.Data)
	page := Page{
		Channel:     in.Channel.Name,
		Corrections: in.Config.CorrectionsContact,
		ChannelURL:  in.Config.ChannelPageURL(r.ChannelID),
	}
	page.Title, page.Summary = titleOf(script)
	page.Date = dateOf(r, in.Channel.Timezone)
	shown := map[string]bool{}
	for _, sc := range script.Scenes {
		seen := map[string]bool{}
		var srcs []Source
		for _, id := range append(append(append([]string{}, sc.FactIDs...), sc.GroupIDs...), sc.ClusterIDs...) {
			for _, u := range byID[id] {
				s, ok := checked[u]
				if !ok || seen[u] {
					continue
				}
				seen[u] = true
				srcs = append(srcs, s)
			}
		}
		if len(srcs) == 0 || strings.TrimSpace(sc.Narration) == "" {
			continue
		}
		sort.SliceStable(srcs, func(i, j int) bool { return srcs[i].Primary && !srcs[j].Primary })
		page.Statements = append(page.Statements, Statement{Text: strings.TrimSpace(sc.Narration), Sources: srcs})
		for u := range seen {
			shown[u] = true
		}
	}
	others := map[string]Source{}
	for u, s := range checked {
		if !shown[u] {
			others[u] = s
		}
	}
	page.Others = ranked(others)
	return page
}

// titleOf is the video's title and summary: YouTube's, or the first
// platform's that has them.
func titleOf(script scriptDoc) (string, string) {
	for _, p := range Platforms {
		if m, ok := script.Metadata[p]; ok && m.Title != "" {
			return strings.TrimSpace(m.Title), strings.TrimSpace(m.Summary)
		}
	}
	return "", ""
}

// dateOf is the day the release was approved, in the channel's time zone.
func dateOf(r store.Release, tz string) string {
	t := r.CreatedAt
	if r.Moderation.At != nil {
		t = *r.Moderation.At
	}
	if loc, err := time.LoadLocation(tz); err == nil && tz != "" {
		t = t.In(loc)
	}
	return t.Format("Jan 2, 2006")
}

// IndexEntry is one video on the channel's list.
type IndexEntry struct {
	Title string
	Date  string
	URL   string
}

// Index is a channel's list of videos with their sources pages.
type Index struct {
	Channel     string
	Entries     []IndexEntry
	Corrections string
}

// BuildIndex writes a channel's list from its listed releases, newest first.
func BuildIndex(ch store.Channel, releases []store.Release, cfg Config) Index {
	idx := Index{Channel: ch.Name, Corrections: cfg.CorrectionsContact}
	for _, r := range releases {
		var script scriptDoc
		_ = json.Unmarshal(r.Script, &script)
		title, _ := titleOf(script)
		if title == "" {
			title = "Video of " + dateOf(r, ch.Timezone)
		}
		idx.Entries = append(idx.Entries, IndexEntry{Title: title, Date: dateOf(r, ch.Timezone), URL: "/s/" + Code(r.ID)})
	}
	return idx
}
