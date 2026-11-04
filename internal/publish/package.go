package publish

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf16"

	"github.com/tereshed/8bitnews/internal/store"
)

// Config is what the package and the pages need besides the release.
type Config struct {
	// PublicURL is where hub's public pages are reachable
	// ("https://hub.8bitlang.org"); the sources page links are built on it.
	PublicURL string
	// CorrectionsContact is the address for corrections (metadata.md
	// "Corrections: {corrections_contact}").
	CorrectionsContact string
}

// Code is the short id of a release in its sources page address: the first
// ten hex digits of its id.
func Code(releaseID string) string {
	c := strings.ReplaceAll(releaseID, "-", "")
	if len(c) > codeLen {
		c = c[:codeLen]
	}
	return c
}

const codeLen = 10

// SourcesPageURL is the address of a release's sources page.
func (c Config) SourcesPageURL(releaseID string) string {
	if c.PublicURL == "" {
		return ""
	}
	return strings.TrimRight(c.PublicURL, "/") + "/s/" + Code(releaseID)
}

// ChannelPageURL is the address of a channel's list of videos with their
// sources: the link in the profile (policy.md).
func (c Config) ChannelPageURL(channelID string) string {
	if c.PublicURL == "" {
		return ""
	}
	return strings.TrimRight(c.PublicURL, "/") + "/c/" + channelID
}

func short(u string) string {
	return strings.TrimPrefix(strings.TrimPrefix(u, "https://"), "http://")
}

// Input is everything one release's package is built from.
type Input struct {
	Release store.Release
	Sources []store.ReleaseSource
	Channel store.Channel
	Country store.Country
	Config  Config
}

// Package is what the moderator uploads by hand, platform by platform.
type Package struct {
	SourcesPage string
	Platforms   []PlatformPackage
	// Warnings: what is missing or over a platform's limit. The package is
	// still built; the moderator sees what to fix first.
	Warnings []string
	// Notes: what the moderator should know but can't fix.
	Notes []string
}

// PlatformPackage is one platform's upload.
type PlatformPackage struct {
	Platform string // "youtube"
	Name     string // "YouTube Shorts"
	// Title is the title field; empty when the platform has none and the
	// title opens the description instead (TikTok, Instagram).
	Title       string
	Description string
	Tags        []string
	Settings    []string
	// Video is the object key of the video to upload: on YouTube the cut
	// without music when there is one (format.md «Музыка и длина»).
	Video string
	// Cover is the object key of the cover image, for the platforms that
	// take an uploaded one (Instagram).
	Cover string
}

// Platform limits (platforms/<platform>/rules.md).
const (
	youtubeTitleMax       = 100
	youtubeDescriptionMax = 5000 // bytes
	captionMax            = 2200 // TikTok counts UTF-16 units, Instagram characters
	instagramHashtagsMax  = 30
	sourcesBlockMax       = 8
)

type scriptMeta struct {
	Title    string   `json:"title"`
	Summary  string   `json:"summary"`
	Hashtags []string `json:"hashtags"`
	Tags     []string `json:"tags"`
}

type scriptDoc struct {
	Metadata map[string]scriptMeta `json:"metadata"`
	Scenes   []scene               `json:"scenes"`
}

type mediaDoc struct {
	Video        string `json:"video"`
	Cover        string `json:"cover"`
	VideoNoMusic string `json:"video_no_music"`
}

// Build writes the package of an approved release: for every platform Legal
// allowed and the channel has, the title, description and tags from the
// script's metadata, filled into the channel's metadata.md template.
func Build(in Input) Package {
	r := in.Release
	var script scriptDoc
	_ = json.Unmarshal(r.Script, &script) // checked at release_submit; a gap shows as a warning below
	var media mediaDoc
	_ = json.Unmarshal(r.Media, &media)
	var vp struct {
		Media mediaDoc `json:"media"`
	}
	_ = json.Unmarshal(r.VideoPackage, &vp)

	pkg := Package{SourcesPage: in.Config.SourcesPageURL(r.ID)}
	warn := func(format string, args ...any) { pkg.Warnings = append(pkg.Warnings, fmt.Sprintf(format, args...)) }
	if pkg.SourcesPage == "" {
		warn("нет адреса страницы источников: не задан HUB_PUBLIC_URL")
	}
	if in.Config.CorrectionsContact == "" {
		warn("нет контакта для исправлений: не задан HUB_CORRECTIONS_CONTACT")
	}

	templates := ParseTemplates(in.Channel.MetadataMD)
	enabled := enabledPlatforms(in.Channel.Platforms)
	allowed := map[string]bool{}
	for _, p := range r.AllowedPlatforms {
		allowed[p] = true
	}
	sources := sourcesOf(in.Sources, in.Country.Data, in.Channel.Language)
	unarchived := 0
	for _, s := range sources {
		switch {
		case s.Gone && s.ArchiveURL == "":
			warn("источник недоступен и без архивной копии: %s", s.URL)
		case s.ArchiveURL == "":
			unarchived++
		}
	}
	if unarchived > 0 {
		pkg.Notes = append(pkg.Notes, fmt.Sprintf("%d из %d источников пока без архивной копии: Wayback сохраняет их в фоне "+
			"несколько минут после сдачи релиза или отказал. На странице источников у них не будет архивной ссылки.", unarchived, len(sources)))
	}
	block := sourcesBlock(ranked(sources))

	for _, p := range Platforms {
		if !allowed[p] {
			continue
		}
		if !enabled[p] {
			warn("%s: Legal допустил, но в профиле канала площадка выключена — пропускаю", p)
			continue
		}
		meta, ok := script.Metadata[p]
		if !ok {
			warn("%s: в сценарии нет метаданных для площадки — пропускаю", p)
			continue
		}
		t, ok := templates[p]
		if !ok || t.Description == "" {
			warn("%s: в metadata.md канала нет шаблона описания — пропускаю", p)
			continue
		}
		values := map[string]string{
			"title":               strings.TrimSpace(meta.Title),
			"summary":             strings.TrimSpace(meta.Summary),
			"hashtags":            strings.Join(meta.Hashtags, " "),
			"sources_block":       block,
			"sources_page_url":    pkg.SourcesPage,
			"sources_page_short":  short(pkg.SourcesPage),
			"corrections_contact": in.Config.CorrectionsContact,
		}
		description, missing := fill(t.Description, values)
		for _, name := range missing {
			if name != "sources_page_url" && name != "sources_page_short" && name != "corrections_contact" { // warned above
				warn("%s: в описании не заполнено {%s}", p, name)
			}
		}
		pp := PlatformPackage{Platform: p, Name: t.Name, Description: description, Settings: t.Settings, Video: media.Video}
		if !strings.Contains(t.Description, "{title}") {
			pp.Title = values["title"]
		}
		switch p {
		case "youtube":
			pp.Tags = meta.Tags
			// YouTube refuses "<" and ">" in a title or description.
			pp.Title, pp.Description = noAngles(pp.Title), noAngles(pp.Description)
			if v := vp.Media.VideoNoMusic; strings.HasPrefix(v, "releases/") {
				pp.Video = v
			}
			if n := len([]rune(pp.Title)); n > youtubeTitleMax {
				warn("youtube: заголовок %d символов, YouTube берёт до %d", n, youtubeTitleMax)
			}
			if n := len(pp.Description); n > youtubeDescriptionMax {
				warn("youtube: описание %d байт, YouTube берёт до %d", n, youtubeDescriptionMax)
			}
		case "tiktok":
			if n := len(utf16.Encode([]rune(pp.Description))); n > captionMax {
				warn("tiktok: подпись %d символов, TikTok берёт до %d", n, captionMax)
			}
		case "instagram":
			pp.Cover = media.Cover
			if n := len([]rune(pp.Description)); n > captionMax {
				warn("instagram: подпись %d символов, Instagram берёт до %d", n, captionMax)
			}
			if n := strings.Count(pp.Description, "#"); n > instagramHashtagsMax {
				warn("instagram: %d хэштегов, Instagram берёт до %d", n, instagramHashtagsMax)
			}
		}
		pkg.Platforms = append(pkg.Platforms, pp)
	}
	if len(pkg.Platforms) == 0 {
		warn("нет ни одной площадки для публикации")
	}
	return pkg
}

// enabledPlatforms reads channel.yaml's platforms: {youtube: {enabled: true}}.
func enabledPlatforms(raw json.RawMessage) map[string]bool {
	var platforms map[string]struct {
		Enabled bool `json:"enabled"`
	}
	_ = json.Unmarshal(raw, &platforms)
	out := map[string]bool{}
	for p, v := range platforms {
		out[p] = v.Enabled
	}
	return out
}

// sourcesBlock is the description's list of key sources:
// "• {outlet}{marks}: {title} ({date}) {url}", at most sourcesBlockMax lines.
func sourcesBlock(sources []Source) string {
	var b strings.Builder
	for i, s := range sources {
		if i == sourcesBlockMax {
			break
		}
		b.WriteString("• " + s.Outlet + s.Marks())
		if s.Title != "" {
			b.WriteString(": " + s.Title)
		}
		if s.Date != "" {
			b.WriteString(" (" + s.Date + ")")
		}
		b.WriteString(" " + s.Link() + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

var angles = strings.NewReplacer("<", "‹", ">", "›")

func noAngles(s string) string { return angles.Replace(s) }
