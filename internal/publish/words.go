package publish

import (
	"fmt"
	"strings"
	"time"
)

// pageWords is what hub itself says to viewers of a channel, in its language:
// the sources pages and the marks on sources. The descriptions' own text
// comes from the channel's metadata.md.
type pageWords struct {
	Lang string // the page's lang attribute

	Paywall    string // the mark on a paywalled source, lower case
	StateMedia string // the mark on state media, lower case

	Sources         string
	Says            string
	Archived        string
	ArchivedGone    string
	NoSources       string
	Others          string
	How             string
	HowDossier      string
	HowAI           string
	Corrections     string
	NoCorrections   string
	WriteTo         string
	IndexTitle      string
	IndexTitleLower string
	IndexLead       string
	NoVideos        string
	VideoOf         string

	months     [12]string
	dateFormat string // arguments: month, day, year
}

func (w pageWords) date(t time.Time) string {
	return fmt.Sprintf(w.dateFormat, w.months[t.Month()-1], t.Day(), t.Year())
}

var english = pageWords{
	Lang:            "en",
	Paywall:         "paywall",
	StateMedia:      "state media",
	Sources:         "Sources",
	Says:            "What the video says, and where it comes from",
	Archived:        "archived copy",
	ArchivedGone:    "archived copy; the original page is gone",
	NoSources:       "No sources are listed for this video.",
	Others:          "Other sources",
	How:             "How we make these videos",
	HowDossier:      "Every video starts from a research dossier: what is new, what is already known, how positions split at home and how the press abroad covers the story. Each line of the video is traced to the sources above, primary sources first. When a video is released, we check every source and save a copy to the Internet Archive's Wayback Machine where it lets us; the archived copy is linked next to the source, and a source whose page was gone when we checked it links to its archived copy.",
	HowAI:           "The narration is a synthetic voice and the pictures are AI-generated pixel art; real people are shown as stylized illustrations, not photographs. A human editor approves every video before it is published.",
	Corrections:     "Corrections",
	NoCorrections:   "No corrections to this video so far.",
	WriteTo:         "Spotted an error? Write to",
	IndexTitle:      "Videos and their sources",
	IndexTitleLower: "videos and their sources",
	IndexLead:       "Each video has a page with every source behind it, with archived copies where the Wayback Machine has them.",
	NoVideos:        "No videos yet.",
	VideoOf:         "Video of",
	months:          [12]string{"Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"},
	dateFormat:      "%[1]s %[2]d, %[3]d",
}

var spanish = pageWords{
	Lang:            "es",
	Paywall:         "de pago",
	StateMedia:      "medio estatal",
	Sources:         "Fuentes",
	Says:            "Qué dice el vídeo y de dónde sale",
	Archived:        "copia archivada",
	ArchivedGone:    "copia archivada; la página original ya no existe",
	NoSources:       "Este vídeo no tiene fuentes en la lista.",
	Others:          "Otras fuentes",
	How:             "Cómo hacemos estos vídeos",
	HowDossier:      "Cada vídeo parte de un dosier de investigación: qué hay de nuevo, qué se sabe ya, cómo se reparten las posiciones en España y cómo cuenta la historia la prensa extranjera. Cada frase del vídeo remite a las fuentes de arriba, primero las fuentes primarias. Al publicar un vídeo comprobamos todas las fuentes y guardamos una copia en la Wayback Machine de Internet Archive cuando lo permite; la copia archivada aparece junto a la fuente, y si la página ya no existía al comprobarla, el enlace lleva a su copia archivada.",
	HowAI:           "La narración es una voz sintética y las imágenes son pixel art generado con IA; las personas reales aparecen como ilustraciones estilizadas, no como fotografías. Una persona de la redacción aprueba cada vídeo antes de publicarlo.",
	Corrections:     "Correcciones",
	NoCorrections:   "Este vídeo no tiene correcciones por ahora.",
	WriteTo:         "¿Has visto un error? Escribe a",
	IndexTitle:      "Vídeos y sus fuentes",
	IndexTitleLower: "vídeos y sus fuentes",
	IndexLead:       "Cada vídeo tiene una página con todas sus fuentes y, cuando la Wayback Machine las tiene, sus copias archivadas.",
	NoVideos:        "Todavía no hay vídeos.",
	VideoOf:         "Vídeo del",
	months:          [12]string{"ene", "feb", "mar", "abr", "may", "jun", "jul", "ago", "sep", "oct", "nov", "dic"},
	dateFormat:      "%[2]d %[1]s %[3]d",
}

// wordsFor picks by the channel's language ("es-ES" -> Spanish); English is
// the fallback, as on the pages hub had before channels got their own.
func wordsFor(language string) pageWords {
	if strings.EqualFold(strings.SplitN(language, "-", 2)[0], "es") {
		return spanish
	}
	return english
}
