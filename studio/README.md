# studio

The frame engine and template toolkit for 8bitnews video rendering
(`docs/architecture.md` §5 "Шаблоны: библиотека и выбор", §12). Python 3.12,
Pillow + NumPy for drawing and quantization, ffmpeg for the final MP4.
No Remotion, no paid image/voice APIs here — those are E2a-2 (TTS,
`studio build`, `package.json`) and E2a-3 (Kokoro voice, music, demo reel).

## What's in this package (E2a-1)

- `template.schema.json` — the JSON Schema a template package must satisfy.
- `channels/8bitnews-us/` — assets shared by every template for this
  channel: the OFL pixel font, the NES palette, the logo, the placeholder
  anchor sprite set, and the platform safe-zone rectangles.
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
  its pixels stay exactly whatever `channels/8bitnews-us/anchor/*.png`
  contains (skin/hair/suit tones that are deliberately not in every
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
the file today — and, separately, that none of them overlap the anchor's
face (`channels/8bitnews-us/anchor/manifest.json`'s `face_rect`, mapped
into frame coordinates by `studio.anchor.x/y/scale` via
`ChannelAssets.face_rect_in_frame`).

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
  Transition sounds (`transitions[].sound`) ship in E2a-3.
- `shot: anchor` scenes separately animate the anchor's blink and mouth
  (2-3 frames, driven by the timeline's volume envelope) regardless of
  which motion preset the *next* scene-shot (shown on her over-the-shoulder
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

## Replacing the placeholder anchor sprites

`channels/8bitnews-us/anchor/manifest.json` declares two poses (`normal`,
`serious`), two eye states (`open`, `closed`, for blinking), three mouth
frames (`0`/`1`/`2`, picked by `channels/8bitnews-us/anchor/manifest.json`'s
`mouth.volume_thresholds` against the timeline's volume envelope), and a
`face_rect` (canvas-local `x`/`y`/`w`/`h` covering hair/eyes/mouth) that
`studio template validate` maps into frame coordinates via each template's
`studio.anchor.x`/`y`/`scale` to reject any plate or subtitle rect that
would cover her face. A future Design pass replaces the PNG files in place
(same filenames, same canvas size, same `face_rect`) — no engine code
changes.

## Running things

```sh
make studio-image           # build the Docker image (Python 3.12 + ffmpeg)
make studio-test             # pytest, in Docker
make studio-preview          # renders templates/nes-desk into out/studio-preview/
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
[--template <dir-or-id>] [--assets <dir>] [--tts stub] --out <dir>` turns a
`newsroom/contracts/script.schema.json` document into a self-contained
`newsroom/contracts/video_package.schema.json` package: `video.mp4`,
`subtitles.srt`, `cover.png`, `timeline.json`, a copy of `script.json` and
of the template package used, every scene's `keyframes/<id>_first.png` /
`_mid.png`, and `package.json` itself.

Pipeline (`studio/studio/build.py`): per-scene TTS (`studio/studio/tts.py`;
`studio.pronunciation` applies/undoes `script.pronunciations` hints around
it, general code so a future Kokoro adapter gets it for free) ->
`timeline.timeline_from_voice` (real word timings + the template's
`timing.scene_pause_s` gap between scenes, none at either end) ->
`audio.mix_audio` (passthrough until E2a-3 adds music) -> frames rendered
and written to disk one at a time (kept around after the build so a later
pass can re-encode without music) -> `video.encode` (H.264+AAC, ×4
nearest-neighbour to 1080×1920 @ 30fps) -> cover + keyframes -> a
schema-validated `package.json` with `studio validate`'s result already
attached in `validate`.

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
  keeps her colors.
- `cover.frame: custom` — `<assets>/cover.png` quantized to the template's
  palette at 270×480, or `cover.layout.background` without one, then the
  title.

**Build scratch.** `frames/` (every logical frame) is deleted as the last
step of the build; `--keep-work` keeps it for debugging. `voice.wav` stays —
E2a-3 needs the voice apart from the music. `package.duration_s` is the
measured length of `video.mp4` (ffprobe), and `package.script` is the
script's path relative to the repo root when it lives inside it.

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
| `loudness`, `true_peak`, `silence`, `music_under_voice`, `video_no_music` | placeholders returning `ok` with "not implemented (E2a-3)"; E2a-3 replaces them by registering real functions under the same ids |

Keyframes taken mid-transition (only a scene shorter than its transition
gets them) are skipped by `ai_plates` and `face_clear`: there the picture
legitimately mixes two scenes.

Negative tests in `tests/test_validate_checks.py` corrupt a copy of the
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
make studio-build-sample   # builds newsroom/trials/2026-10-26-ninth-circuit
                            # with the stub voice into out/studio-sample/
```

## Known gaps (left for E2a-3, or explicitly deferred)

- No real voice: `--tts stub` is a deterministic quiet tone; E2a-3 adds a
  `kokoro` entry to `studio.tts.ADAPTERS` without changing the interface.
- No music, no loudness ducking: `audio.mix_audio` is a passthrough and
  `package.json`'s `music.enabled` is always `false` (E2a-3).
- Audio checks are placeholders until E2a-3 (see the table above).
- Transition sounds (`transitions[].sound`) are validated as data but not
  played — the sound files themselves ship in E2a-3.
- The scene-title plate's rect height is fixed by the template (not shrunk
  when the wrapped title only needs one line), so a single-line title can
  leave an empty band under it. Cosmetic; not fixed in this pass.
