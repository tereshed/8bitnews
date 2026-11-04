package publish

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/tereshed/8bitnews/internal/sources"
	"github.com/tereshed/8bitnews/internal/store"
)

const releaseID = "f874ebb1-a01a-419d-960c-447e826ad150"

var cfg = Config{PublicURL: "https://hub.example.org", CorrectionsContact: "corrections@example.org"}

func read(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func yamlAsJSON(t *testing.T, path string) json.RawMessage {
	t.Helper()
	var v any
	if err := yaml.Unmarshal(read(t, path), &v); err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// trial is the second end-to-end trial's release (newsroom/trials): its
// script, its dossier, and the sources hub would have checked for it.
func trial(t *testing.T) Input {
	t.Helper()
	script := read(t, "../../newsroom/trials/2026-10-29-hutchinson-no-bill/script.json")
	dossier := read(t, "testdata/hutchinson_dossier.json")
	candidates, err := sources.ExtractCandidates(dossier, script)
	if err != nil {
		t.Fatal(err)
	}
	var rows []store.ReleaseSource
	for _, c := range candidates {
		rows = append(rows, store.ReleaseSource{
			URL: c.URL, Outlet: c.Outlet, Title: c.Title, PublishedAt: c.PublishedAt, FactIDs: c.FactIDs,
			IsPrimary: c.IsPrimary, CheckStatus: "ok", ArchiveURL: "https://web.archive.org/web/2026/" + c.URL,
		})
	}
	approved := time.Date(2026, 11, 1, 14, 0, 0, 0, time.UTC)
	return Input{
		Release: store.Release{
			ID: releaseID, ChannelID: "8bitnews-us", Status: "approved",
			AllowedPlatforms: []string{"instagram", "tiktok", "youtube"},
			Media:            json.RawMessage(`{"video":"releases/d/v-video.mp4","subtitles":"releases/d/s.srt","cover":"releases/d/c-cover.png"}`),
			VideoPackage:     json.RawMessage(`{"media":{"video":"releases/d/v-video.mp4","video_no_music":"releases/d/n-video_no_music.mp4"}}`),
			Script:           script,
			ResearchDossier:  dossier,
			CreatedAt:        approved.Add(-time.Hour),
			Moderation:       store.Moderation{At: &approved},
		},
		Sources: rows,
		Channel: store.Channel{
			ID: "8bitnews-us", Name: "8BitNewsUS", CountryCC: "US", Timezone: "America/New_York",
			Platforms:  json.RawMessage(`{"youtube":{"enabled":true},"tiktok":{"enabled":true},"instagram":{"enabled":true}}`),
			MetadataMD: string(read(t, "../../channels/8bitnews-us/metadata.md")),
		},
		Country: store.Country{CC: "US", Data: yamlAsJSON(t, "../../countries/us/sources.yaml")},
		Config:  cfg,
	}
}

func TestTheChannelsMetadataHasATemplateForEveryPlatform(t *testing.T) {
	templates := ParseTemplates(string(read(t, "../../channels/8bitnews-us/metadata.md")))
	known := map[string]bool{"title": true, "summary": true, "hashtags": true, "sources_block": true,
		"sources_page_url": true, "sources_page_short": true, "corrections_contact": true}
	for _, p := range Platforms {
		tpl, ok := templates[p]
		if !ok || tpl.Description == "" || len(tpl.Settings) == 0 || tpl.Name == "" {
			t.Fatalf("%s: %+v", p, tpl)
		}
		for _, m := range placeholder.FindAllStringSubmatch(tpl.Description, -1) {
			if !known[m[1]] {
				t.Errorf("%s: hub fills no {%s}", p, m[1])
			}
		}
		for _, s := range tpl.Settings {
			if strings.Contains(s, "**") || strings.Contains(s, "`") || strings.Contains(s, "[предложение]") {
				t.Errorf("%s: markdown left in %q", p, s)
			}
		}
	}
	if yt := templates["youtube"].Description; !strings.Contains(yt, "{sources_block}") || !strings.Contains(yt, "{corrections_contact}") {
		t.Errorf("youtube template: %s", yt)
	}
	if templates["youtube"].Name != "YouTube Shorts" {
		t.Errorf("youtube heading: %q", templates["youtube"].Name)
	}
}

func TestParseTemplatesReadsOnlyThePlatformsSections(t *testing.T) {
	md := "## Общие правила\n```\nnot a template\n```\n\n## TikTok\n\n```\n{title}\n{hashtags}\n```\n\n```\nsecond block\n```\n\n" +
		"**Настройки:**\n- метка AI — да (`is_aigc`);\n- Duet выключен **[предложение]**: новостной ролик\n  легко вырвать;\n\n## Страница источников\n- not a setting\n"
	got := ParseTemplates(md)
	if len(got) != 1 {
		t.Fatalf("templates: %v", got)
	}
	tt := got["tiktok"]
	if tt.Description != "{title}\n{hashtags}" {
		t.Errorf("description %q", tt.Description)
	}
	want := []string{"метка AI — да (is_aigc);", "Duet выключен: новостной ролик легко вырвать;"}
	if strings.Join(tt.Settings, "|") != strings.Join(want, "|") {
		t.Errorf("settings %q", tt.Settings)
	}
}

func TestTheTrialsPackage(t *testing.T) {
	pkg := Build(trial(t))
	if len(pkg.Warnings) != 0 {
		t.Errorf("warnings: %v", pkg.Warnings)
	}
	if pkg.SourcesPage != "https://hub.example.org/s/f874ebb1a0" {
		t.Errorf("sources page %q", pkg.SourcesPage)
	}
	var names []string
	for _, p := range pkg.Platforms {
		names = append(names, p.Platform)
	}
	if strings.Join(names, ",") != "youtube,tiktok,instagram" {
		t.Fatalf("platforms %v", names)
	}

	yt := pkg.Platforms[0]
	if yt.Title != "Grand Jury Won't Indict Cassidy Hutchinson Over Jan. 6 Testimony" || yt.Video != "releases/d/n-video_no_music.mp4" ||
		len(yt.Tags) != 9 || yt.Cover != "releases/d/c-cover.png" {
		t.Errorf("youtube: %+v", yt)
	}
	for _, want := range []string{
		"A federal grand jury refused to indict former White House aide Cassidy Hutchinson",
		"\n\nSources:\n• ",
		"All sources and archived copies: https://hub.example.org/s/f874ebb1a0",
		"AI disclosure: synthetic narration",
		"Corrections: corrections@example.org",
		"#8BitNews #DOJ #Jan6 #GrandJury #Politics",
	} {
		if !strings.Contains(yt.Description, want) {
			t.Errorf("youtube description lacks %q:\n%s", want, yt.Description)
		}
	}
	block := yt.Description[strings.Index(yt.Description, "Sources:\n")+len("Sources:\n") : strings.Index(yt.Description, "\nAll sources")]
	lines := strings.Split(block, "\n")
	if len(lines) != sourcesBlockMax {
		t.Fatalf("%d source lines:\n%s", len(lines), block)
	}
	// No primary sources in this dossier: NBC's story backs the most facts of the video (7), and leads.
	if !strings.HasPrefix(lines[0], "• NBC News: Grand jury declines to indict former Trump aide Cassidy Hutchinson") ||
		!strings.Contains(lines[0], "(Oct 27, 2026) https://www.nbcnews.com/") {
		t.Errorf("first source: %s", lines[0])
	}
	if n := len(yt.Description); n > youtubeDescriptionMax {
		t.Errorf("youtube description is %d bytes", n)
	}
	if strings.Contains(yt.Description, "{") {
		t.Errorf("a placeholder is left:\n%s", yt.Description)
	}

	tt := pkg.Platforms[1]
	if tt.Title != "" || tt.Video != "releases/d/v-video.mp4" ||
		!strings.HasPrefix(tt.Description, "Grand Jury Won't Indict Cassidy Hutchinson Over Jan. 6 Testimony\nA federal grand jury") ||
		!strings.Contains(tt.Description, "Sources: hub.example.org/s/f874ebb1a0 (link in bio)") {
		t.Errorf("tiktok: %+v", tt)
	}
	if ig := pkg.Platforms[2]; ig.Cover != "releases/d/c-cover.png" || !strings.Contains(ig.Description, "real people are shown as stylized illustrations") {
		t.Errorf("instagram: %+v", ig)
	}
}

func TestAPackageSaysWhatIsMissing(t *testing.T) {
	in := trial(t)
	in.Config = Config{}
	in.Release.AllowedPlatforms = []string{"youtube", "tiktok"}
	in.Channel.Platforms = json.RawMessage(`{"youtube":{"enabled":true},"tiktok":{"enabled":false}}`)
	in.Sources[0].CheckStatus, in.Sources[0].ArchiveURL = "dead", ""
	in.Sources[1].ArchiveURL = ""
	pkg := Build(in)
	if len(pkg.Platforms) != 1 || pkg.Platforms[0].Platform != "youtube" {
		t.Fatalf("platforms %+v", pkg.Platforms)
	}
	all := strings.Join(pkg.Warnings, "\n")
	for _, want := range []string{"HUB_PUBLIC_URL", "HUB_CORRECTIONS_CONTACT", "tiktok: Legal допустил, но в профиле канала площадка выключена",
		"источник недоступен и без архивной копии: " + in.Sources[0].URL} {
		if !strings.Contains(all, want) {
			t.Errorf("no warning %q in:\n%s", want, all)
		}
	}
	if len(pkg.Notes) != 1 || !strings.Contains(pkg.Notes[0], "1 из 12 источников пока без архивной копии") {
		t.Errorf("notes: %v", pkg.Notes)
	}
	// What hub can't fill stays visible in the text.
	if d := pkg.Platforms[0].Description; !strings.Contains(d, "{sources_page_url}") || !strings.Contains(d, "{corrections_contact}") {
		t.Errorf("description:\n%s", d)
	}
}

func TestSourcesAreCanonicalAndMarked(t *testing.T) {
	country := json.RawMessage(`{"local":[{"domain":"wsj.com","paywall":true}],"primary":["justice.gov"],
		"world":[{"outlets":[{"domain":"aljazeera.com","ownership":"state_funded"},{"domain":"news.cn","ownership":"state_controlled"},
		{"domain":"bbc.com","ownership":"public_broadcaster"}]}]}`)
	rows := []store.ReleaseSource{
		{URL: "https://www.wsj.com/a?utm_source=x&id=7&fbclid=1", Outlet: "WSJ", PublishedAt: "2026-10-27", CheckStatus: "ok"},
		{URL: "https://english.news.cn/b", Outlet: "Xinhua", PublishedAt: "unknown", CheckStatus: "ok"},
		{URL: "https://www.aljazeera.com/c", Outlet: "Al Jazeera", CheckStatus: "dead", ArchiveURL: "https://web.archive.org/c"},
		{URL: "https://www.bbc.com/d", CheckStatus: "ok", IsPrimary: true},
		{URL: "https://www.justice.gov/e", Outlet: "DOJ", CheckStatus: "ok"},
		{URL: "https://gone.example/f", Outlet: "Gone\nDaily", Title: " two\n lines ", CheckStatus: "unreachable", ArchiveURL: "https://web.archive.org/f"},
	}
	got := ranked(sourcesOf(rows, country))
	if !got[0].Primary || !got[1].Primary || got[2].Primary {
		t.Errorf("primary sources lead: %+v", got[:3])
	}
	by := map[string]Source{}
	for _, s := range got {
		by[s.Outlet] = s
	}
	if s := by["WSJ"]; s.URL != "https://www.wsj.com/a?id=7" || s.Marks() != " (paywall)" || s.Date != "Oct 27, 2026" {
		t.Errorf("wsj: %+v", s)
	}
	if s := by["Xinhua"]; s.Marks() != " (state media)" || s.Date != "" {
		t.Errorf("xinhua: %+v", s)
	}
	if s := (Source{Outlet: "RT (state media)", StateMedia: true}); s.Marks() != "" {
		t.Errorf("the mark is doubled: %q", s.Outlet+s.Marks())
	}
	if s := by["Al Jazeera"]; s.Link() != "https://web.archive.org/c" || s.Marks() != " (state media)" {
		t.Errorf("a dead page links its archive copy: %+v", s)
	}
	if s := by["bbc.com"]; s.Marks() != "" {
		t.Errorf("a source with no outlet is named by its site, unmarked: %+v", s)
	}
	if s := by["Gone Daily"]; s.Title != "two lines" || s.Link() != "https://web.archive.org/f" {
		t.Errorf("an unreachable site links its archive copy, text on one line: %+v", s)
	}
}

func TestYouTubeGetsNoAngleBrackets(t *testing.T) {
	in := trial(t)
	for i, s := range in.Sources {
		if strings.Contains(s.URL, "pbs.org") {
			in.Sources[i].Title = "Judge <quietly> rules"
		}
	}
	pkg := Build(in)
	if d := pkg.Platforms[0].Description; strings.ContainsAny(d, "<>") || !strings.Contains(d, "Judge ‹quietly› rules") {
		t.Errorf("youtube description:\n%s", d)
	}
}

func TestCode(t *testing.T) {
	if c := Code(releaseID); c != "f874ebb1a0" {
		t.Errorf("code %q", c)
	}
}
