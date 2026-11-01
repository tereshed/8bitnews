// Package publish turns an approved release into what goes out with the
// video (channels/<id>/metadata.md): each platform's title and description,
// the moderator's upload checklist, and the video's public sources page
// (countries/us/policy.md "Источники в ролике"). The description's blocks —
// the sources, the sources page, the AI disclosure, the corrections line —
// come from hub, never from a model.
package publish

import (
	"regexp"
	"strings"
)

// Template is one platform's part of metadata.md: the description template
// (the first fenced block under the platform's "## " heading) and the
// "**Настройки:**" list the moderator follows when uploading.
type Template struct {
	Name        string // the heading: "YouTube Shorts"
	Description string
	Settings    []string
}

// Platforms is the order the package lists them in (docs/architecture.md
// §13: YouTube → TikTok → Instagram).
var Platforms = []string{"youtube", "tiktok", "instagram"}

const settingsLabel = "**Настройки:**"

var markdownNoise = strings.NewReplacer(" **[предложение]**", "", "**[предложение]**", "", "**", "", "`", "")

// ParseTemplates reads the platform sections of a channel's metadata.md. A
// platform's section is the "## " heading that names it; a platform with no
// section is absent from the result.
func ParseTemplates(md string) map[string]Template {
	out := map[string]Template{}
	var platform string
	var t Template
	var inBlock, inSettings, blockDone bool
	var block []string
	flush := func() {
		if platform != "" {
			out[platform] = t
		}
	}
	for _, line := range strings.Split(md, "\n") {
		if inBlock {
			if strings.HasPrefix(line, "```") {
				inBlock, blockDone = false, true
				t.Description = strings.TrimSpace(strings.Join(block, "\n"))
				continue
			}
			block = append(block, line)
			continue
		}
		if heading, ok := strings.CutPrefix(line, "## "); ok {
			flush()
			platform, t, inSettings, blockDone, block = platformOf(heading), Template{Name: strings.TrimSpace(heading)}, false, false, nil
			continue
		}
		if platform == "" {
			continue
		}
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "```") && !blockDone:
			inBlock = true
		case trimmed == settingsLabel:
			inSettings = true
		case inSettings && strings.HasPrefix(line, "- "):
			t.Settings = append(t.Settings, clean(line[2:]))
		case inSettings && strings.HasPrefix(line, "  ") && trimmed != "" && len(t.Settings) > 0:
			t.Settings[len(t.Settings)-1] += " " + clean(trimmed)
		case inSettings:
			inSettings = false
		}
	}
	flush()
	return out
}

func platformOf(heading string) string {
	h := strings.ToLower(heading)
	for _, p := range Platforms {
		if strings.Contains(h, p) {
			return p
		}
	}
	return ""
}

func clean(s string) string {
	return strings.Join(strings.Fields(markdownNoise.Replace(s)), " ")
}

var placeholder = regexp.MustCompile(`\{([a-z_]+)\}`)

// fill puts values into a template's placeholders. A placeholder with no
// value stays as it is, so an unfinished description is plain to see, and
// is reported.
func fill(template string, values map[string]string) (string, []string) {
	var missing []string
	text := placeholder.ReplaceAllStringFunc(template, func(m string) string {
		name := m[1 : len(m)-1]
		if v, ok := values[name]; ok && v != "" {
			return v
		}
		missing = append(missing, name)
		return m
	})
	return text, missing
}
