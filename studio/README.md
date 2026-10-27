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

## Known gaps (left for E2a-2 / E2a-3, or explicitly deferred)

- No TTS, no real voice volume envelope (the uniform timeline stands in).
- No music, no loudness/ducking.
- No `studio build`, no `package.json`, no end-to-end video assembly from a
  real script end to end — `template preview` only runs the built-in demo
  script.
- Transition sounds (`transitions[].sound`) are validated as data but not
  played — the sound files themselves ship in E2a-3.
- The scene-title plate's rect height is fixed by the template (not shrunk
  when the wrapped title only needs one line), so a single-line title can
  leave an empty band under it. Cosmetic; not fixed in this pass.
