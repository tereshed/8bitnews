package publish

import (
	"encoding/json"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/tereshed/8bitnews/internal/store"
)

// Source is one checked source as the description and the sources page
// show it.
type Source struct {
	Outlet     string
	Title      string
	Date       string // "Oct 27, 2026"; empty when the page has no date
	URL        string // the canonical link: no tracking parameters
	ArchiveURL string
	// Gone: the page was dead, a "not found" page or its site unreachable
	// when hub checked it, so the archive copy is the link to follow
	// (policy.md: the sources page switches to the archive copy).
	Gone       bool
	Paywall    bool
	StateMedia bool
	Primary    bool
	Facts      int // how many of the video's facts it backs
	published  time.Time
	words      pageWords
}

// Link is where a reader should go: the live page, or its archive copy
// when the page is gone.
func (s Source) Link() string {
	if s.Gone && s.ArchiveURL != "" {
		return s.ArchiveURL
	}
	return s.URL
}

// Marks is " (paywall)", " (state media)", both or "", in the channel's
// language: the ones the outlet's name doesn't carry already.
func (s Source) Marks() string {
	words := s.words
	if words.Lang == "" {
		words = english
	}
	var m string
	name := strings.ToLower(s.Outlet)
	if s.Paywall && !strings.Contains(name, words.Paywall) {
		m += " (" + words.Paywall + ")"
	}
	if s.StateMedia && !strings.Contains(name, words.StateMedia) {
		m += " (" + words.StateMedia + ")"
	}
	return m
}

// outletFlags is what countries/<cc>/sources.yaml says about a domain.
type outletFlags struct {
	paywall, stateMedia, primary bool
}

type countryOutlet struct {
	Domain    string `json:"domain"`
	Ownership string `json:"ownership"`
	Paywall   bool   `json:"paywall"`
}

// outletsOf reads a country's sources.yaml (stored as countries.data):
// which domains are behind a paywall, which belong to state media
// (state_controlled or state_funded), and which publish primary sources
// (its "primary" list: courts, agencies).
func outletsOf(countryData json.RawMessage) map[string]outletFlags {
	var doc struct {
		Local []countryOutlet `json:"local"`
		World []struct {
			Outlets []countryOutlet `json:"outlets"`
		} `json:"world"`
		Primary []string `json:"primary"`
	}
	_ = json.Unmarshal(countryData, &doc) // no outlets known: no marks
	all := doc.Local
	for _, region := range doc.World {
		all = append(all, region.Outlets...)
	}
	out := map[string]outletFlags{}
	for _, o := range all {
		if o.Domain == "" {
			continue
		}
		out[strings.ToLower(o.Domain)] = outletFlags{
			paywall:    o.Paywall,
			stateMedia: o.Ownership == "state_controlled" || o.Ownership == "state_funded",
		}
	}
	for _, d := range doc.Primary {
		if d != "" {
			out[strings.ToLower(d)] = outletFlags{primary: true}
		}
	}
	return out
}

func flagsFor(rawURL string, outlets map[string]outletFlags) outletFlags {
	u, err := url.Parse(rawURL)
	if err != nil {
		return outletFlags{}
	}
	host := strings.ToLower(u.Hostname())
	for host != "" {
		if f, ok := outlets[host]; ok {
			return f
		}
		_, host, _ = strings.Cut(host, ".")
	}
	return outletFlags{}
}

// trackingParams are query parameters that only follow a click around.
var trackingParams = map[string]bool{"fbclid": true, "gclid": true, "dclid": true, "msclkid": true, "igshid": true,
	"mc_cid": true, "mc_eid": true, "ocid": true, "cmpid": true, "smid": true, "smtyp": true}

// canonical drops tracking parameters (utm_* and the like) from a link
// (policy.md: links are canonical).
func canonical(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil || u.RawQuery == "" {
		return rawURL
	}
	q := u.Query()
	changed := false
	for k := range q {
		if strings.HasPrefix(strings.ToLower(k), "utm_") || trackingParams[strings.ToLower(k)] {
			q.Del(k)
			changed = true
		}
	}
	if !changed {
		return rawURL
	}
	u.RawQuery = q.Encode()
	return u.String()
}

func parseDate(s string) (time.Time, bool) {
	for _, layout := range []string{time.RFC3339, "2006-01-02T15:04:05", "2006-01-02"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// sourcesOf turns a release's checked sources into what readers see.
func sourcesOf(rows []store.ReleaseSource, countryData json.RawMessage, language string) map[string]Source {
	outlets := outletsOf(countryData)
	words := wordsFor(language)
	out := make(map[string]Source, len(rows))
	for _, r := range rows {
		flags := flagsFor(r.URL, outlets)
		s := Source{
			Outlet:     oneLine(r.Outlet),
			Title:      oneLine(r.Title),
			URL:        canonical(r.URL),
			ArchiveURL: r.ArchiveURL,
			Gone:       r.CheckStatus == "dead" || r.CheckStatus == "soft_404" || r.CheckStatus == "unreachable",
			Paywall:    flags.paywall || r.Paywall,
			StateMedia: flags.stateMedia || r.StateMedia,
			Primary:    flags.primary || r.IsPrimary,
			Facts:      len(r.FactIDs),
			words:      words,
		}
		if t, ok := parseDate(r.PublishedAt); ok {
			s.published = t
			s.Date = words.date(t)
		}
		if s.Outlet == "" {
			s.Outlet = hostOf(r.URL)
		}
		out[r.URL] = s
	}
	return out
}

// oneLine collapses an agent's text to one line: a stray newline in a
// title would break the description's list.
func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func hostOf(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	return strings.TrimPrefix(u.Hostname(), "www.")
}

// ranked orders sources the way the description lists them
// (metadata.md "sources_block"): primary sources first, then the ones
// backing the most facts of the video, newest first among equals.
func ranked(sources map[string]Source) []Source {
	out := make([]Source, 0, len(sources))
	for _, s := range sources {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Primary != b.Primary {
			return a.Primary
		}
		if a.Facts != b.Facts {
			return a.Facts > b.Facts
		}
		if !a.published.Equal(b.published) {
			return a.published.After(b.published)
		}
		return a.URL < b.URL
	})
	return out
}
