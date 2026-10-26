package sources

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
)

// Candidate is one source the video relies on, before it has been checked
// or archived.
type Candidate struct {
	URL         string
	Outlet      string
	Title       string
	PublishedAt string
	FactIDs     []string
	IsPrimary   bool
}

type dossierSource struct {
	URL         string `json:"url"`
	Outlet      string `json:"outlet"`
	Title       string `json:"title"`
	PublishedAt string `json:"published_at"`
}

type dossierFact struct {
	ID            string          `json:"id"`
	PrimarySource *dossierSource  `json:"primary_source"`
	Sources       []dossierSource `json:"sources"`
}

type dossierRep struct {
	Outlet      string `json:"outlet"`
	URL         string `json:"url"`
	PublishedAt string `json:"published_at"`
}

type dossierGroup struct {
	ID              string       `json:"id"`
	Representatives []dossierRep `json:"representatives"`
}

type dossierDoc struct {
	Facts         []dossierFact  `json:"facts"`
	LocalGroups   []dossierGroup `json:"local_groups"`
	WorldClusters []dossierGroup `json:"world_clusters"`
}

var (
	factIDPattern    = regexp.MustCompile(`^F[0-9]+$`)
	groupIDPattern   = regexp.MustCompile(`^L[0-9]+$`)
	clusterIDPattern = regexp.MustCompile(`^W[0-9]+$`)
)

// referencedIDs is which F*/L*/W* ids the script mentions anywhere (scene
// fact_ids, or any other field — the script contract is arbitrary JSON
// until E3, so this scans the whole document rather than a fixed field
// path).
type referencedIDs struct {
	fact    map[string]bool
	group   map[string]bool
	cluster map[string]bool
}

func (r referencedIDs) empty() bool {
	return len(r.fact) == 0 && len(r.group) == 0 && len(r.cluster) == 0
}

func scanReferencedIDs(script json.RawMessage) referencedIDs {
	ids := referencedIDs{fact: map[string]bool{}, group: map[string]bool{}, cluster: map[string]bool{}}
	if len(script) == 0 {
		return ids
	}
	var v any
	if err := json.Unmarshal(script, &v); err != nil {
		return ids
	}
	collectIDs(v, &ids)
	return ids
}

func collectIDs(v any, ids *referencedIDs) {
	switch t := v.(type) {
	case string:
		switch {
		case factIDPattern.MatchString(t):
			ids.fact[t] = true
		case groupIDPattern.MatchString(t):
			ids.group[t] = true
		case clusterIDPattern.MatchString(t):
			ids.cluster[t] = true
		}
	case []any:
		for _, item := range t {
			collectIDs(item, ids)
		}
	case map[string]any:
		for _, item := range t {
			collectIDs(item, ids)
		}
	}
}

// ExtractCandidates collects every source the video needs to cite: sources
// of the facts the script's fact_ids reference, plus quotes of the local
// opinion groups and world press clusters the script mentions
// (docs/architecture.md §6 "sources"). If the script does not reference any
// F*/L*/W* id at all, every source in the dossier is used instead — the
// script contract is still arbitrary JSON (E3 will add its schema), so an
// empty reference set cannot be told apart from "references everything".
func ExtractCandidates(researchDossier, script json.RawMessage) ([]Candidate, error) {
	var dossier dossierDoc
	if err := json.Unmarshal(researchDossier, &dossier); err != nil {
		return nil, fmt.Errorf("parsing research_dossier: %w", err)
	}

	referenced := scanReferencedIDs(script)
	useAll := referenced.empty()

	byURL := map[string]*Candidate{}
	order := make([]string, 0)
	add := func(c Candidate) {
		existing, ok := byURL[c.URL]
		if !ok {
			cp := c
			byURL[c.URL] = &cp
			order = append(order, c.URL)
			return
		}
		existing.IsPrimary = existing.IsPrimary || c.IsPrimary
		existing.FactIDs = append(existing.FactIDs, c.FactIDs...)
		if existing.Outlet == "" {
			existing.Outlet = c.Outlet
		}
		if existing.Title == "" {
			existing.Title = c.Title
		}
		if existing.PublishedAt == "" {
			existing.PublishedAt = c.PublishedAt
		}
	}

	for _, f := range dossier.Facts {
		if !useAll && !referenced.fact[f.ID] {
			continue
		}
		if f.PrimarySource != nil && f.PrimarySource.URL != "" {
			add(Candidate{
				URL: f.PrimarySource.URL, Outlet: f.PrimarySource.Outlet,
				Title: f.PrimarySource.Title, PublishedAt: f.PrimarySource.PublishedAt,
				FactIDs: []string{f.ID}, IsPrimary: true,
			})
		}
		for _, src := range f.Sources {
			if src.URL == "" {
				continue
			}
			add(Candidate{
				URL: src.URL, Outlet: src.Outlet, Title: src.Title, PublishedAt: src.PublishedAt,
				FactIDs: []string{f.ID},
			})
		}
	}

	for _, g := range dossier.LocalGroups {
		if !useAll && !referenced.group[g.ID] {
			continue
		}
		for _, rep := range g.Representatives {
			if rep.URL == "" {
				continue
			}
			add(Candidate{URL: rep.URL, Outlet: rep.Outlet, PublishedAt: rep.PublishedAt})
		}
	}

	for _, c := range dossier.WorldClusters {
		if !useAll && !referenced.cluster[c.ID] {
			continue
		}
		for _, rep := range c.Representatives {
			if rep.URL == "" {
				continue
			}
			add(Candidate{URL: rep.URL, Outlet: rep.Outlet, PublishedAt: rep.PublishedAt})
		}
	}

	sort.Strings(order)
	result := make([]Candidate, 0, len(order))
	for _, u := range order {
		c := *byURL[u]
		sort.Strings(c.FactIDs)
		c.FactIDs = dedupeStrings(c.FactIDs)
		result = append(result, c)
	}
	return result, nil
}

func dedupeStrings(in []string) []string {
	if len(in) == 0 {
		return in
	}
	out := make([]string, 0, len(in))
	var last string
	for i, s := range in {
		if i == 0 || s != last {
			out = append(out, s)
			last = s
		}
	}
	return out
}
