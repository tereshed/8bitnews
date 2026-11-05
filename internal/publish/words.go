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

	// The costs page (costs.go).
	CostsLink            string
	CostsTitle           string
	CostsTitleLower      string
	CostsLead            string
	CostsNoVideos        string
	CostsTileVideos      string
	CostsTileTokens      string
	CostsTilePictures    string
	CostsTilePicturesAll string
	CostsSharesTitle     string
	CostsSharesLead      string
	CostsTokensTitle     string
	CostsTokensLead      string
	CostsPicturesTitle   string
	CostsPicturesLead    string
	CostsTableTitle      string
	CostsColVideo        string
	CostsColTokens       string
	CostsColWritten      string
	CostsColBuilds       string
	CostsColPictures     string
	CostsColTotal        string
	CostsBuildsOf        string // argument: how many builds
	CostsOtherRuns       string // arguments: how many runs, tokens
	CostsOtherBuilds     string // arguments: how many builds, dollars
	CostsMethodTitle     string
	CostsMethodTokens    string
	CostsCached          string // arguments: share read from cache, share written
	CostsUnmeasured      string // arguments: runs with no record, all runs
	CostsMethodPictures  string
	CostsMethodFree      string
	CostsMethodExcluded  string
	CostsUpdated         string // argument: the date
	CostsStageResearch   string
	CostsStageScript     string
	CostsStageLegal      string
	CostsStageEditor     string
	CostsStageVideo      string
	CostsStageOther      string

	months     [12]string
	dateFormat string // arguments: month, day, year

	decimal, group             string
	thousand, million, billion string // billion empty: count in millions
	usdFormat                  string // argument: the amount
	percentFormat              string // argument: the whole percent
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

	CostsLink:            "What our videos cost",
	CostsTitle:           "What our videos cost",
	CostsTitleLower:      "what our videos cost",
	CostsLead:            "AI agents research, write and check every video. Here is what that takes, video by video: in dollars where we pay per use, and in tokens where a flat-rate subscription means one video has no price of its own.",
	CostsNoVideos:        "No videos listed here yet.",
	CostsTileVideos:      "Videos counted",
	CostsTileTokens:      "Tokens per video, median",
	CostsTilePictures:    "Pictures per video, median",
	CostsTilePicturesAll: "Pictures, all videos",
	CostsSharesTitle:     "Where the tokens go",
	CostsSharesLead:      "All the videos below, by stage of the work.",
	CostsTokensTitle:     "Tokens per video",
	CostsTokensLead:      "Newest first. Reruns and rebuilds count towards the video.",
	CostsPicturesTitle:   "Pictures per video, in dollars",
	CostsPicturesLead:    "What the provider charged, for every build of the video.",
	CostsTableTitle:      "All the numbers",
	CostsColVideo:        "Video",
	CostsColTokens:       "Tokens",
	CostsColWritten:      "Written",
	CostsColBuilds:       "Builds",
	CostsColPictures:     "Pictures",
	CostsColTotal:        "Total",
	CostsBuildsOf:        "builds: %d",
	CostsOtherRuns:       "Not in the table: runs with no video listed here so far: %d (%s tokens).",
	CostsOtherBuilds:     "Not in the table: builds with no video listed here so far: %d (%s in pictures).",
	CostsMethodTitle:     "How we count",
	CostsMethodTokens:    "Tokens are the units AI models read and write. The agents run on Claude under a flat-rate subscription, so we show tokens instead of a price, as our agent platform measures them for each run. \"Written\" in the table is what the models wrote.",
	CostsCached:          "The totals look large because agents reread the same material many times: of all the tokens above, %s were read again from a cache and %s were written.",
	CostsUnmeasured:      "%d of %d agent runs on this channel have no usage record and count as zero here, so the real token figures may be higher.",
	CostsMethodPictures:  "Pictures are drawn by Gemini through OpenRouter and paid per request. The figure comes from each build's package and includes pictures that were redrawn. A picture a rebuild takes from the cache adds nothing.",
	CostsMethodFree:      "The voice (Kokoro, an open model that runs inside the build) and the music (CC0 tracks) have no per-use fee.",
	CostsMethodExcluded:  "Not counted: the small model that routes work between the agents (paid per use, but not reported per video), hosting and storage, the coding assistant that supervises runs and helps upload videos, the time of the people who approve them, and building the system itself.",
	CostsUpdated:         "Figures updated %s.",
	CostsStageResearch:   "Research",
	CostsStageScript:     "Script",
	CostsStageLegal:      "Legal risk check",
	CostsStageEditor:     "Editor",
	CostsStageVideo:      "Video and QA",
	CostsStageOther:      "Other",

	months:     [12]string{"Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"},
	dateFormat: "%[1]s %[2]d, %[3]d",

	decimal: ".", group: ",",
	thousand: "K", million: "M", billion: "B",
	usdFormat:     "$%s",
	percentFormat: "%.0f%%",
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

	CostsLink:            "Cuánto cuestan nuestros vídeos",
	CostsTitle:           "Cuánto cuestan nuestros vídeos",
	CostsTitleLower:      "cuánto cuestan nuestros vídeos",
	CostsLead:            "Agentes de IA investigan, escriben y revisan cada vídeo. Aquí está lo que eso supone, vídeo a vídeo: en dólares cuando pagamos por uso y en tokens cuando una suscripción de tarifa plana hace que un vídeo no tenga precio propio.",
	CostsNoVideos:        "Todavía no hay vídeos en esta lista.",
	CostsTileVideos:      "Vídeos contados",
	CostsTileTokens:      "Tokens por vídeo, mediana",
	CostsTilePictures:    "Imágenes por vídeo, mediana",
	CostsTilePicturesAll: "Imágenes, todos los vídeos",
	CostsSharesTitle:     "A qué se van los tokens",
	CostsSharesLead:      "Todos los vídeos de abajo, por etapa del trabajo.",
	CostsTokensTitle:     "Tokens por vídeo",
	CostsTokensLead:      "Primero los más recientes. Las repeticiones y los remontajes cuentan para el vídeo.",
	CostsPicturesTitle:   "Imágenes por vídeo, en dólares",
	CostsPicturesLead:    "Lo que cobró el proveedor, por cada montaje del vídeo.",
	CostsTableTitle:      "Todas las cifras",
	CostsColVideo:        "Vídeo",
	CostsColTokens:       "Tokens",
	CostsColWritten:      "Escritos",
	CostsColBuilds:       "Montajes",
	CostsColPictures:     "Imágenes",
	CostsColTotal:        "Total",
	CostsBuildsOf:        "montajes: %d",
	CostsOtherRuns:       "Fuera de la tabla: ejecuciones sin vídeo en esta lista por ahora: %d (%s tokens).",
	CostsOtherBuilds:     "Fuera de la tabla: montajes sin vídeo en esta lista por ahora: %d (%s en imágenes).",
	CostsMethodTitle:     "Cómo contamos",
	CostsMethodTokens:    "Los tokens son las unidades que leen y escriben los modelos de IA. Los agentes usan Claude con una suscripción de tarifa plana, así que mostramos tokens en lugar de un precio, tal como los mide nuestra plataforma de agentes en cada ejecución. «Escritos» en la tabla es lo que escribieron los modelos.",
	CostsCached:          "Los totales parecen grandes porque los agentes releen el mismo material muchas veces: de todos los tokens de arriba, un %s se releyó de una caché y un %s se escribió.",
	CostsUnmeasured:      "%d de %d ejecuciones de agentes de este canal no tienen registro de uso y aquí cuentan como cero, así que las cifras reales de tokens pueden ser mayores.",
	CostsMethodPictures:  "Las imágenes las dibuja Gemini a través de OpenRouter y se pagan por petición. La cifra sale del paquete de cada montaje e incluye las imágenes que se volvieron a dibujar. Una imagen que un remontaje toma de la caché no suma nada.",
	CostsMethodFree:      "La voz (Kokoro, un modelo abierto que funciona dentro del montaje) y la música (pistas CC0) no tienen tarifa por uso.",
	CostsMethodExcluded:  "No se cuenta: el modelo pequeño que reparte el trabajo entre los agentes (se paga por uso, pero no se informa por vídeo), el alojamiento y el almacenamiento, el asistente de programación que supervisa las ejecuciones y ayuda a subir los vídeos, el tiempo de las personas que los aprueban y la construcción del propio sistema.",
	CostsUpdated:         "Cifras actualizadas el %s.",
	CostsStageResearch:   "Investigación",
	CostsStageScript:     "Guion",
	CostsStageLegal:      "Riesgos legales",
	CostsStageEditor:     "Edición",
	CostsStageVideo:      "Vídeo y control",
	CostsStageOther:      "Otros",

	months:     [12]string{"ene", "feb", "mar", "abr", "may", "jun", "jul", "ago", "sep", "oct", "nov", "dic"},
	dateFormat: "%[2]d %[1]s %[3]d",

	decimal: ",", group: ".",
	thousand: " mil", million: " M", billion: "",
	usdFormat:     "%s $",
	percentFormat: "%.0f %%",
}

// wordsFor picks by the channel's language ("es-ES" -> Spanish); English is
// the fallback, as on the pages hub had before channels got their own.
func wordsFor(language string) pageWords {
	if strings.EqualFold(strings.SplitN(language, "-", 2)[0], "es") {
		return spanish
	}
	return english
}
