# studio

The frame engine, template toolkit, voice and sound mix for 8bitnews video
rendering (`docs/architecture.md` §5 "Шаблоны: библиотека и выбор", §12).
Python 3.12, Pillow + NumPy for drawing and quantization, Kokoro-82M (ONNX)
for the voice, ffmpeg for the final MP4. No Remotion, no paid image/voice
APIs; nothing needs the network once the image is built.

## What's in this package (E2a-1)

- `template.schema.json` — the JSON Schema a template package must satisfy.
- `channels/8bitnews-us/` — assets shared by every template for this
  channel: the OFL pixel font, the NES palette, the logo, the anchors
  (`anchors/`: the brief they are drawn from and one sprite set per anchor),
  and the platform safe-zone rectangles.
- `templates/nes-desk/`, `templates/night-report/` — two template packages
  (data only: `template.json` + PNGs), proving the engine draws any valid
  package without template-specific code.
- `studio/` (the Python package) — the deterministic frame engine, the
  timeline data structure, and the `template validate` / `template preview`
  CLI.
- `tests/` — pytest suite, run in Docker (`make studio-test`).

## Engine model

A frame is a pure function of `(script, timeline, template, channel_assets,
t_abs)` — same inputs always produce the same PNG bytes (`tests/test_determinism.py`).

- **Script** — a `newsroom/contracts/script.schema.json` document (read-only
  input; the engine does not change that contract).
- **Timeline** — `studio/timeline.py`: scenes with absolute start/end,
  per-word timings, subtitle cues, and a voice volume envelope the anchor's
  mouth animates against. It is independent of how it was produced.
  `studio/timeline.py:uniform_timeline` builds one purely from each scene's
  word count and a words-per-minute pace (no audio) — used by tests and by
  `template preview`. E2a-2 adds a TTS adapter that builds the same
  structure from real audio.
- **Template package** — data, not code (`docs/architecture.md` §5): the
  engine (`studio/engine.py`) renders any package that validates against
  `template.schema.json`. It never branches on a template's `id`.
- **Rendering** — every frame is drawn at the logical 270×480 resolution
  and later scaled ×4 to 1080×1920 with nearest-neighbour (`-sws_flags
  neighbor` in ffmpeg; `studio/video.py:upscale_nearest` does the pixel-
  identical thing in Python for the contact sheet). All drawing — plate
  backgrounds, scene images (via `studio/quantize.py:quantize`), and text
  (via `studio/text_render.py`, which rasterizes glyphs to a bilevel mask
  so there is no anti-aliased blend color) — only ever paints colors from
  `channels/8bitnews-us/palette.json`, so every frame is palette-pure by
  construction, not by a final clean-up pass.
- **Palette subset vs. the anchor** — studio background/foreground layers
  and scene images are snapped to the *template's* `palette.colors` (or
  `serious_colors` in serious mode) before anything else is drawn on top,
  in both modes, not just serious mode. The anchor sprite is composited
  **after** that and is never run through the template's palette at all —
  its pixels stay exactly whatever `channels/8bitnews-us/anchors/<id>/*.png`
  contain (skin/hair/suit tones that are deliberately not in every
  template's muted subset). This is the fix for two review findings: (1)
  serious mode used to re-quantize the *whole* frame, which turned the
  anchor's skin grey; (2) normal mode never restricted scene art to the
  template subset at all, which is why a no-asset scene in `night-report`
  rendered its debug placeholder in `nes-desk`'s pink, not night-report's
  muted colors.

### AI disclosure plates (`channels/8bitnews-us/format.md`, Legal R4/R5)

`studio/engine.py:active_plates` is the single source of truth for plate
timing, kept separate from pixel drawing so it is unit-testable
(`tests/test_ai_plates_timing.py`):

- `ai_voice` — the whole video's first 3 seconds (`AI_VOICE_PLATE_S` in
  `studio/constants.py`), top of frame.
- `ai_icon` — every frame after that (mutually exclusive with `ai_voice`).
- `ai_illustration` — any frame where a real person's likeness is in frame:
  either the current scene's own `real_people` is non-empty, or (for an
  anchor shot) the scene shown on the over-the-shoulder screen
  (`_next_scene_shot`) has non-empty `real_people` — the plate holds for
  that scene's entire duration either way.
- `scene_title` / `source_plate` — straight from the scene's
  `on_screen_text` / `source_plate` fields, wrapped to fit the template's
  own plate rect width (`studio/engine.py:_max_chars_for_rect`).

### Safe zones

`channels/8bitnews-us/safe_zones.json` has one rectangle per platform
(YouTube Shorts, TikTok, Instagram Reels) plus their intersection,
`common_safe_rect`. Templates in this repo place every plate and the
subtitle rect inside that intersection. **These rectangles are
conservative engineering estimates, not numbers read off a live official
guide**: YouTube's, TikTok's and Instagram's own safe-zone pages were
unreachable from this sandbox when this file was written (403/404s; see
`safe_zones.json`'s own `sourcing_note`). Re-verify against each
platform's current guide with a
working fetch before this ships, and update the file (and the template
rects that key off it) if the real numbers differ. `studio template
validate` checks every plate/subtitle/cover rect against whatever is in
the file today — and, separately, that none of them overlap the face of
any active anchor (each `channels/8bitnews-us/anchors/<id>/manifest.json`
`face_rect`, mapped into frame coordinates by `studio.anchor.x/y/scale`
via `AnchorSprites.face_rect_in_frame`).

### Subtitles

`channels/8bitnews-us/format.md` caps subtitle lines at "up to 32
characters" assuming the full 270px logical width. The timeline
(`studio/timeline.py:uniform_timeline`) splits narration into cues once,
independently of which template renders them, using the channel-wide
`MAX_SUBTITLE_CHARS_PER_LINE`/`MAX_SUBTITLE_LINES` in `studio/constants.py`
(23/2 — a tighter, still-compliant choice against the common safe
rect's 190px width at the 8px font). At render time `engine.py:_render_core`
**re-wraps** each cue's text using the *active template's*
`subtitles.style.max_lines`/`max_chars_per_line` (schema fields that were
previously validated but never read) — so a template that wants shorter or
longer lines than the channel default actually gets them, without the
timeline needing to know which template will render it.

### Motion

Every `shot: scene` picks one of its template's `pan`/`zoom` presets
deterministically by the scene's ordinal position in the script, round-
robin (`engine.py:_select_scene_motion`) — `studio template validate`
requires at least one `pan` or `zoom` preset to exist, since a template
with none would leave some scenes with no motion at all.

- **`pan`** (`dx_px`/`dy_px`, `duration_s`): the scene's image is rendered
  once at an oversized canvas (`LOGICAL_W + dx_px` × `LOGICAL_H + dy_px`),
  and the visible window crops a `LOGICAL_W`×`LOGICAL_H` rectangle that
  slides across it by whole pixels as the scene plays. No resize is ever
  applied to produce the pan itself — only `Image.crop`.
- **`zoom`** (`max_px`, `duration_s`): for the first half of the preset's
  duration the plain frame is shown; for the second half, the scene is
  rendered again at an integer multiple of the logical size -- 3× if
  `max_px` is more than a sixth of the frame width, 2× otherwise -- and a
  centered `LOGICAL_W`×`LOGICAL_H` window is cropped out of it. This is the
  "целыми кратностями ... без нецелого ресайза" approach the review asked
  for: the only resize involved is the deliberate integer-multiple render,
  and the zoom step itself is a crop, never a resize-back-up (which is what
  produced inconsistent pixel widths in the previous version). The
  trade-off is a single discrete zoom step rather than a continuous ramp;
  that was the simplest implementation that guarantees every logical pixel
  of the output stays an exact 1:1 NES-grid pixel.
- **`parallax`** (`layer_indices`, `shift_px`): while an anchor shot plays,
  the studio's background/foreground layers whose index is in
  `layer_indices` drift horizontally by `layer.parallax * shift_px`,
  following a triangle wave that is exactly 0 at both ends of the scene
  (so consecutive anchor shots never jump at the cut).
- **Transitions** (`cut`/`wipe`/`pixelate`/`slide`, `duration_s`): picked
  deterministically per scene boundary, round-robin over
  `template.transitions` by the new scene's index. Each type blends the
  previous scene's frozen last frame into the new scene's first
  `duration_s` seconds using only crop/paste (never pixel blending), so
  every pixel is always an exact color from one of the two source frames.
  A transition may carry a `sound` (below, «Sound»).
- `shot: anchor` scenes separately animate the anchor's blink and mouth
  (2-3 frames, driven by the timeline's volume envelope) regardless of
  which motion preset the *next* scene-shot (shown on the over-the-shoulder
  screen) is using.

## Making a new template without writing code

1. Copy `templates/nes-desk/` to `templates/<your-id>/`.
2. Edit `template.json`: studio layers, anchor position, palette subset
   (+ `serious_colors` if it supports serious mode), plate rects/colors
   (must stay inside `channels/8bitnews-us/safe_zones.json`'s
   `common_safe_rect`), subtitle rect/style, motion presets, transitions,
   cover layout, and `fit` (serious mode support, moods, tags).
3. Replace the PNGs in `assets/` — same filenames or update `template.json`
   and the `assets` list. Every color must come from
   `channels/8bitnews-us/palette.json`.
4. `python -m studio template validate templates/<your-id>` until it's
   clean, then `python -m studio template preview templates/<your-id> --out
   /tmp/preview` to look at it.

No change to any `.py` file is needed — `tests/test_render_second_template.py`
is exactly this check against `templates/night-report/`.

## Anchors (E2b-1)

The channel has several anchors, women and men, and each video is presented
by one of them (`channels/8bitnews-us/format.md`, "Ведущие";
`docs/architecture.md` §5, "Ведущие: ротация"). Each anchor is a sprite set in
`channels/8bitnews-us/anchors/<id>/`:

- `manifest.json`: `status` (`active` anchors are picked for new videos; a
  `retired` one stays loadable so an old package still validates),
  `voice_id` (the anchor's own Kokoro voice: one face, one voice), the
  `canvas` every anchor of the channel shares (`anchors/brief.json`, so any
  template places any anchor), `face_rect`, the poses, blink and mouth
  frames with their offsets, and `source`: the model, prompt and edits it
  was drawn with;
- `body.png` (the calm, neutral candidate itself: both the normal and the
  serious pose, since format.md wants the anchor neither smiling nor
  gesturing in serious mode), `eyes_open`/`eyes_closed` and
  `mouth_0`/`1`/`2` patches pasted over it at the manifest's offsets.

**Which anchor.** `studio build --anchor <id>`: in the pipeline hub picks it
(`anchor_pick`, E2c: not one of the channel's last videos' anchors). Without
it, the studio picks an active anchor from the script's content
(rendezvous hashing: adding or retiring an anchor moves only the scripts
it wins or held), so a rebuild keeps its anchor. A retired anchor is
accepted only by `--anchor`, to rebuild a video it presented, and the
package notes say so. `package.json` records `anchor.id` and
`anchor.chosen_by` (`argument` or `script`); `voice.voice_id` is that
anchor's voice.

**Every voice keeps the channel's pace.** Kokoro voices read at different
speeds at the same setting, and format.md's word budget assumes ~144 words
a minute whoever reads, so each voice has its own `speed` and
`serious_speed` in `config.json` `tts.kokoro.voices`, measured on the demo
script (words over the audio's length, scene pauses included, as the voice
casting measured). A voice without one fails the build.

**Drawing a new anchor** is a one-off with a person choosing, not part of a
video build (key: env `STUDIO_OPENROUTER_API_KEY`; every reply is cached
under `STUDIO_IMAGE_CACHE`, so running again pays nothing):

1. `studio anchor candidates --channel 8bitnews-us --out <dir> [--looks w1,m3]`
   draws one picture per look of `anchors/brief.json` (Gemini Flash Image
   through OpenRouter, about $0.07 each): an adult anchor, front view, on a
   flat green background, from a prompt that carries format.md's rules (not
   chibi, not a mascot, resembling no real person, no text or logos). Each
   becomes a sprite (`studio/sprite.py`): the background, and any pocket of
   it the figure encloses, is cut out; every picture pixel is snapped to the
   NES palette *before* downscaling and each sprite pixel takes its block's
   most common color, so outlines stay black instead of smearing into
   halos; snapping weighs hue more for near-neutral colors, so a white
   shirt stays white. `<dir>/<look>/studio.png` shows it in the default
   template's studio; a head cut off by the top edge or a refusal is drawn
   once more.
2. A person picks faces and a voice for each.
3. `studio anchor build --channel 8bitnews-us --from <dir>/<look> --id <id> --voice <voice>`
   asks a vision model (`google/gemini-3.5-flash-lite`) for the head, eyes
   and mouth boxes, has the image model edit the *same* picture three times
   (eyes closed; mouth slightly open; mouth open), lines each edit up with
   the original (image models redraw an edit a few pixels off) and cuts
   only the eyes or mouth patch out of it. An edit that changed nothing
   there, or redrew more than 15% of the rest of the face (the patch would
   not fit), is drawn again, up to three times; so is a set of boxes that
   doesn't make a face. About $0.21 an anchor. Before any paid call the
   voice must be pinned in `models.json`, measured in `config.json` and not
   another active anchor's. The set is built beside the roster
   (`anchors/.<id>.building`) and checked (`anchors.anchor_problems`, also
   run by the tests over the whole roster, with `roster_problems`: someone
   to pick, no two active anchors sharing a voice): canvas, sizes, palette,
   frames inside `face_rect`, opaque patches, the voice. Only a set that
   passes replaces `anchors/<id>/`.
   A replied refusal or an unreadable answer is never cached, so running
   the command again asks again.

`studio template validate` checks plates, subtitles and the cover title
against the face of *every* active anchor, and that every anchor fits in the
frame at the template's `studio.anchor` place.

## Running things

```sh
make studio-image           # build the Docker image (Python 3.12, ffmpeg, Kokoro model)
make studio-test             # pytest, in Docker
make studio-preview          # renders templates/nes-desk into out/studio-preview/
make studio-build-sample     # the demo script, stub voice: quick, no model run
make studio-demo             # the demo reel with the real voice, then studio validate
```

Without Docker (needs a local Python 3.12 + the pinned `requirements.txt`):

```sh
cd studio
python -m studio template validate templates/nes-desk
python -m studio template preview templates/nes-desk --out /tmp/preview
```

`scripts/generate_assets.py` is the script that drew the placeholder PNGs
in `channels/8bitnews-us/` and `templates/*/assets/`; it is not part of the
shipped engine, just how the placeholders were made (documented so the
next pass can regenerate or replace them deliberately).

## `studio build` (E2a-2a)

`studio build --script <script.json> --channel <id> [--channels-dir <dir>]
[--template <dir-or-id>] [--assets <dir>] [--tts kokoro|stub]
[--music-file <file>] [--music-track <id>] --out <dir>` turns a
`newsroom/contracts/script.schema.json` document into a self-contained
`newsroom/contracts/video_package.schema.json` package: `video.mp4`,
`video_no_music.mp4` (a video with music longer than 60 s), `subtitles.srt`,
`cover.png`, `timeline.json`, a copy of `script.json` and of the template
package used, every scene's `keyframes/<id>_first.png` / `_mid.png`, the
sound stems `stems/voice.flac` and `stems/music.flac`, and `package.json`
itself.

Pipeline (`studio/studio/build.py`): per-scene TTS (`studio/studio/tts.py`;
`studio.pronunciation` applies/undoes `script.pronunciations` hints around
it, general code, not the adapter's) -> `timeline.timeline_from_voice`
(real word timings, each scene as long as its voice audio, plus the
template's `timing.scene_pause_s` gap between scenes, none at either end)
-> frames rendered and written to disk one at a time -> the picture,
encoded once (`video.encode_video_only`: H.264, ×4 nearest-neighbour to
1080×1920 @ 30fps) -> the soundtrack (`audio.mix`, below) muxed onto it as
AAC, and for a long video with music a second soundtrack without music
muxed onto the same picture -> cover + keyframes -> a schema-validated
`package.json` with `studio validate`'s result already attached in
`validate`.

The TTS adapter comes from `--tts`, else the channel's
`format.voice.provider`; the voice is the anchor's (`voice_id` in its
manifest). Each adapter builds itself from its own settings in
`config.json` (`tts.<name>`: the stub's `wpm`/`serious_wpm`, each Kokoro
voice's `speed`/`serious_speed`), so `build` never branches on its name.

**Files in the container.** `studio-test` and `studio-build-sample` mount
the repo root read-only at `/repo` and set `STUDIO_REPO_ROOT=/repo`: the
channel's publishing profile (`<channels-dir>/<id>/channel.yaml`, default
`$STUDIO_REPO_ROOT/channels`, overridable with `--channels-dir` on both
`build` and `validate`) and the contract schemas
(`$STUDIO_REPO_ROOT/newsroom/contracts/*.schema.json`) are read from there,
separately from `studio/channels/<id>/` (the font/palette/anchor assets
`template.palette()`/`channel_assets.py` use). If the root isn't mounted,
`studio.repo.repo_root()` raises instead of silently skipping anything that
needs it.

**Subtitles.** `studio.subtitles.split_cues` replaces E2a-1's
`split_subtitles`: it groups actual timed words (not re-split narration
text) into cues capped at the active template's
`subtitles.style.max_lines`/`max_chars_per_line`, cutting after the last
sentence punctuation (or a comma from the 3rd word on) inside the maximal
group that still fits, and holding a cue up to the next one's start if the
gap is under 0.3s. The same cues go on screen and into `subtitles.srt`.

**Cover.** Both kinds carry `cover.text` (format.md: a cover is a frame
with a title of up to 30 characters), word-wrapped inside
`cover.layout.text_rect` in the subtitle colors
(`cover_title.cover_title_layout`); `cover_title.fit_cover_title` raises a
readable `BuildError` if the title needs more lines than the rect has room
for, checked before TTS. `studio template validate` requires the rect to
hold any 30-character title and to clear the anchor's face and the
AI-illustration plate.
- `cover.frame: S*` — that scene's middle frame (`engine.render_cover_frame`)
  with `engine.cover_plates` only: the AI-illustration plate by the same rule
  as the video (a real person in the scene or on the screen behind the
  anchor), no scene title or source plate (the cover title replaces them),
  no AI-voice/AI-icon plate, no subtitles. The title is drawn at 270×480
  before the ×4 upscale and the frame is not re-quantized, so the anchor
  keeps its colors.
- `cover.frame: custom` — `<assets>/cover.png` quantized to the template's
  palette at 270×480, or `cover.layout.background` without one, then the
  title.

**Build scratch.** `frames/` (every logical frame) and the `_*` work files
are deleted at the end of the build; `--keep-work` keeps `frames/` for
debugging. `package.duration_s` is the measured length of `video.mp4`
(ffprobe), and `package.script` is the script's path relative to the repo
root when it lives inside it.

**Pronunciations** match single whole words. A hint that matches no word
(e.g. a multi-word phrase) is not applied: `build` prints a warning and
records it in `package.notes`.

**Plates never cut text.** `scene_title` and `source_plate` wrap scene text
to the plate width and hold up to `max_lines` lines (template schema, 1–3,
default 2). `build` refuses a script whose
`on_screen_text` or `source_plate` needs more lines (format.md: title up to
2 lines, source up to 3 — `scene_title` uses 2, `source_plate` 3), naming the scene, the
field and the line count (`build.check_plate_text`), before any TTS or
rendering. `studio template validate` requires such a plate to be at least
`10*max_lines+2` px high.

**Pauses between scenes** hold the previous scene's picture
(`Timeline.scene_at` returns the last scene that has started); before
E2a-2b they rendered black.

**`studio validate --package <dir>/package.json [--channels-dir <dir>]`**
runs the registry in `studio/studio/checks.py` and exits non-zero if any
check fails. Each check is a function registered under a fixed `id` with
`@register("<id>")`, returning `{id, ok, value, expected, note}`:

| id | what it checks |
|---|---|
| `format` | 1080×1920, 30 fps, H.264 + AAC (ffprobe) |
| `duration` | measured `video.mp4` length inside the channel range and within 0.1 s of `package.duration_s` |
| `file_size` | `video.mp4` ≤ `max_file_size_bytes` (`studio/config.json`) |
| `subtitles_cover_words` | SRT cue text and timing against `timeline.json` words (below) |
| `package_schema` | `package.json` against `video_package.schema.json`; `sha256`/`bytes` of every file record (`files.*` including `files.template` = `template/template.json`, scenes' keyframes) against the file, no path outside the package; every keyframe the package lists has its `timeline.json` record (scene, `t_s = frame/30`) |
| `template_valid` | the package's `template/` copy passes `studio template validate` and is the template `package.template` names |
| `keyframes_grid_palette` | keyframes and cover: 1080×1920, every 4×4 cell one color, NES palette only |
| `ai_plates` | on every keyframe and an `S*` cover, by pixels: the plate rect equals that plate drawn with its format.md label («AI · AI voice», «AI», «AI illustration»). Required: `ai_voice` before 3 s, `ai_icon` after, `ai_illustration` whenever a real person is in frame (scene or the screen behind the anchor) — stated independently of `engine.active_plates`, which must agree |
| `face_clear` | on anchor keyframes and an anchor cover, the face rect equals the anchor sprite for that moment (pose, blink, mouth) |
| `video_matches_keyframes` | each keyframe PNG against the video frame with the same number, per 4×4 cell: no cell's mean difference above `max_keyframe_cell_diff` (`studio/config.json`) |
| `no_black_frames` | ffmpeg `blackdetect` (whole picture black, `pic_th=1.0`): no black frame anywhere; an unreadable video fails |
| `disclosures` | `ai_voice`, `ai_visuals` true and `ai_label` true for every enabled platform |
| `loudness` | integrated loudness (EBU R128, ffmpeg `ebur128`) of `video.mp4` and `video_no_music.mp4`, measured on the encoded files: −14 ± 1 LUFS (channel policy, `studio/constants.py`, not a setting) |
| `true_peak` | true peak of both files ≤ −1 dBTP (`constants.MAX_TRUE_PEAK_DBTP`) |
| `silence` | ffmpeg `silencedetect`: no stretch below −50 dB longer than 1.5 s in either file (`constants.SILENCE_THRESHOLD_DB`, `MAX_SILENCE_S`) |
| `music_under_voice` | with music, measured on `video.mp4` itself: over the word spans of `timeline.json`, the voice stem is fitted to each channel by least squares and subtracted; the voice must be at least 10 dB louder than what is left (`checks.min_voice_over_music_db`). The remainder is the music plus ~20–35 dB-down limiter and AAC error; channel by channel, so panned or out-of-phase music isn't lost in a mono sum; a stem that isn't the voice in the video leaves the voice in the remainder and fails |
| `video_no_music` | a video with music longer than 60 s (`constants.YOUTUBE_MUSIC_MAX_S`) has `files.video_no_music`; when there is one, it carries the very same video stream as `video.mp4` (packet MD5) and the same length, and holds no music: the voice and music stems are fitted to both soundtracks channel by channel (outside the sound effects), and the music's gain in the cut must stay under 5 % of its gain in `video.mp4` (`checks.no_music_max_music_share`) — quiet music, enough for a Content ID match, is found by its shape — while sound neither stem explains may exceed what `video.mp4` itself leaves (limiter and AAC error) by at most 6 dB (`checks.no_music_max_other_rise_db`), which catches another track; and in the voice's pauses (outside the sound effects) the cut is at least 35 dB under the voice (`checks.no_music_max_pause_db`) — a real Kokoro cut measures about −53 dB there, any music shows |

Keyframes taken mid-transition (only a scene shorter than its transition
gets them) are skipped by `ai_plates` and `face_clear`: there the picture
legitimately mixes two scenes.

Negative tests in `tests/test_validate_checks.py` and
`tests/test_validate_audio.py` corrupt a copy of the
session build with `corrupt_copy` (which re-hashes every file record unless
`rehash=False`) and expect that one check to fail.
- `duration` measures `video.mp4` with ffprobe: it must be inside the
  channel's `format.duration_s` and within 0.1 s of `package.duration_s`.
- `subtitles_cover_words` reads cues from `subtitles.srt` and words from
  `timeline.json`. Words are handed to cues in order; each cue's text without
  whitespace must equal its words glued together (a word `wrap_words` split
  across lines still matches), each word must lie inside its cue's time
  (±1 frame), cues must not overlap, and every scene's words must equal the
  script's `narration.split()` (no pronunciation respelling in subtitles). The same registry backs
the `validate` field a fresh `studio build` writes into its own
`package.json`.

```sh
make studio-build-sample   # builds tests/fixtures/scripts/demo.json with the
                            # stub voice and the test loop into out/studio-sample/
```

## Voice: Kokoro (E2a-3)

`--tts kokoro` (the channel default, `channels/8bitnews-us/channel.yaml`
`format.voice`) runs Kokoro-82M v1.0 (Apache-2.0) offline through
onnxruntime: the onnx-community export that also outputs every phoneme
token's duration. `docker build` downloads the model, its tokenizer and the
voices listed in `models.json` from fixed Hugging Face URLs (one pinned
revision) and checks each file's sha256 (`scripts/fetch_models.py`);
espeak-ng 1.52 (the build the voice casting used) comes from the
`espeakng-loader` wheel, installed with `--require-hashes`.

**Voices.** Each anchor names its voice (`voice_id` in its manifest, see
"Anchors"). Another Kokoro voice: add its `voices/<id>.bin` entry from the
same revision to `models.json`, rebuild the image, and give it a measured
`speed`/`serious_speed` in `config.json` `tts.kokoro.voices`. Pauses after a
sentence and a clause: `sentence_pause_s` (0.25) and `clause_pause_s` (0.1).

**Word timings: Kokoro's own, not forced alignment.** The model reports how
many 25 ms frames it spends on every phoneme, and its audio follows those
frames exactly, so word timings come from the same pass that made the
sound — no second model, no extra dependency. Three corrections
(`studio/studio/kokoro.py`):
- espeak-ng phonemizes the whole scene at once (function words come out
  reduced, as in speech) and sometimes joins words ("in the" -> `ɪnðə`), so
  the phonemes are matched back to words character by character against
  each word phonemized on its own;
- the model folds silence into the edges of a phrase (half a second into a
  scene's last vowel) and starts a word after a pause a few frames before
  its token: each word's edges are snapped onto the audio (trimmed over
  quiet, widened over sound by up to 0.1 s, never into a neighbour);
- in running text the model pauses by itself (0.4–0.7 s after a full stop,
  0.2–0.5 s after a comma), but not always — "Alpha. Bravo." runs on with
  25 ms between — so the gap after a sentence is topped up to
  `sentence_pause_s` by splicing silence into its middle, with 8 ms fades
  on both sides; after a clause only where the model left a quiet gap (a
  comma it ran straight through is one it didn't mean to stop on). With
  this, the reading pace matches the voice casting the owner picked from:
  the ca-ballots trial read at 143 wpm (casting: 144). Each voice's speed
  is measured to that pace (see "Anchors": 144 wpm, 131 in serious mode).
- espeak misreads some written forms on its own: phonemizer cuts a line at
  a "." between digits ("61.3 percent" lost "point three percent" and
  shifted every later word), "$5" comes out "dollar five", "Oct." "ɑːkt".
  `kokoro.spoken_forms` hands espeak a spoken form instead — decimals digit
  by digit ("3.14" -> "3 point 1 4"), money with cents or its scale word
  ("$2.50" -> "2 dollars and 50 cents", "$1.5 million" -> "1 point 5
  million dollars"), ranges ("2-1" -> "2 to 1"), clock times ("10:05" ->
  "10 oh 5"), titles, months and company abbreviations, "U.S.'s" -> "US's";
  words and subtitles keep the original text. A word whose sounds still go
  missing fails the build rather than shifting the timings after it.
  Abbreviations ("Rep.", "Inc.") and initialisms ("U.S.") don't end a
  sentence (no 0.25 s pause) — "U.S." does at the end of a scene, "No."
  when no number follows; a mark inside closing quotes counts, and a
  standalone dash gives the word before it a clause pause.

`tests/test_kokoro.py` checks timings against an energy segmentation of the
audio where one exists — words separated by full stops, plain and spelled
out (a respelling, a number, a title): every edge within ±100 ms (measured
while building this: within 30 ms at speeds 1.0–1.3). Inside connected
speech there is no pause to check an edge against, so it checks
attribution instead: long and short words alternate, and a timing shifted
by one word fails.

**Pronunciation hints** (`script.pronunciations[].say`, a respelling such as
"ess-EYE-lee") are read by the respelling key (`kokoro._RESPELL`: "eye" ->
aɪ, "ay" -> eɪ, "y" -> j, ...) syllable by syllable, the stress on the
capitalized syllable and unstressed "uh" as ə — espeak alone reads "SAY" as
the letters S-A-Y and "nyer" as "nigher". Only the hints' own words are
read this way: `build` passes the adapter the positions of the tokens a
hint put there (`pronunciation.hinted_tokens`), so "US-led" — or the same
word elsewhere in the narration — stays espeak's.

espeak-ng and phonemizer are GPL-3.0: fine for running our own pipeline,
but the image must not be handed to anyone else as is.

## Sound (E2a-3)

`audio.mix` (numpy at 48 kHz, ffmpeg to decode, limit and measure):
- **Music.** `--music-file`, else the catalog: `music/catalog.yaml`'s
  approved track licensed (commercial) for every enabled platform whose
  mood tags include `script.music.mood`, nearest in energy, ties by id
  (`studio.music.select_track`); `--music-track <id>` takes a given catalog
  track (hub `music_pick`). The track's file is
  `$STUDIO_MUSIC_DIR/<audio.key>` and must match the catalog sha256 (track
  audio isn't in git). `--music-file` alone is recorded as license
  "unverified" — demos and tests, not for publishing. No music when
  `script.music.enabled` is false; serious mode follows the script.
- **Mix.** The track is looped or cut to length, faded in (1 s) and out
  (2 s), set `music_bed_db` (−10 dB) under the voice's loudness and ducked
  by `duck_db` (−8 dB) under every phrase. Ducking follows the word timings,
  not a sidechain detector: the timeline already knows when the voice
  speaks, so the music dips 0.12 s *before* a phrase and recovers 0.35 s
  after; gaps under 0.8 s stay ducked so the bed doesn't pump.
  `script.music.cue` is not followed (noted in the package).
- **Transition sounds.** A template transition's `sound` plays at the start
  of the scene it brings in, with the transition, `sfx_db` (−8 dB) under the
  voice's peak; that scene's voice starts after it (the build puts that much
  silence before it), so the sound never covers a word. None in serious
  mode. `nes-desk`'s
  `pixel-wipe` blip is synthesized in code (`scripts/generate_sounds.py`)
  and its license note sits next to it (`<file>.license.txt`, required by
  `studio template validate`).
- **Loudness.** One gain to −14 LUFS (`constants.LOUDNESS_LUFS`), then a
  true-peak limiter: ffmpeg
  `alimiter` run at 4× the sample rate (its sample-peak ceiling then also
  holds between the original samples), latency compensated so nothing
  shifts against the picture. The limiter takes a little loudness off
  peaks, so the gain is raised by what is missing and the pair re-run until
  the result is within 0.2 LU. `loudnorm` isn't used: in linear mode it
  refuses a gain that a peaky voice needs and silently falls back to
  dynamic mode, under target. AAC encoding raises the true peak a little,
  so it is measured on the encoded file; above −1.2 dBTP the ceiling comes
  down by the overshoot and the soundtrack is redone.
- **The YouTube cut.** A video with music longer than 60 s also gets
  `video_no_music.mp4`: the same encoded picture (stream copy) with voice and
  sound effects only, mastered the same way (format.md «Музыка и длина»).
- **Stems.** `stems/voice.flac` is the voice as synthesized and
  `stems/music.flac` the music bed as mixed in (looped, faded, ducked), both
  before the master gain, so nothing clips. They are what `studio validate`
  looks for in the shipped soundtracks; it never takes their levels on
  trust.

`studio music test-track --out <wav>` writes the synthesized 8-bit loop
`studio-build-sample`, `studio-demo` (without `STUDIO_MUSIC_DIR`) and the
tests use as music.

**The demo.** `make studio-demo` builds `tests/fixtures/scripts/demo.json`
(newsroom/trials/2026-10-24-ca-ballots cut to 187 words, S3–S6 with real
people and the pronunciation hints kept) with Kokoro into
`out/studio-demo/` and runs `studio validate`. `make studio-demo
STUDIO_MUSIC_DIR=<folder holding music/tracks/...>` takes the music from
the catalog instead of the test loop. The reel runs about 80 s; on an
Apple-silicon Mac under OrbStack the build takes about 5.5 minutes, nearly
all of it drawing frames (Kokoro reads the 187 words in well under a
minute). A sandbox with fewer or slower cores takes proportionally longer.
`make studio-test` takes about 8 minutes on the same Mac.

## Known gaps

- Pronunciation hints match single words only; a multi-word hint is
  reported, not applied.
- `script.music.cue` is not followed beyond fades and ducking.
- Kokoro reads at most 510 phonemes per pass: a scene is split at sentence
  ends, and one sentence longer than that fails the build.
- The scene-title plate's rect height is fixed by the template (not shrunk
  when the wrapped title only needs one line), so a single-line title can
  leave an empty band under it. Cosmetic; not fixed in this pass.
