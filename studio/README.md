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
  construction, not by a final clean-up pass (serious mode is the one
  exception: the whole frame is re-quantized to the template's muted
  `serious_colors` subset at the end, which is the simplest way to
  guarantee "приглушённая палитра" regardless of what was drawn).

### AI disclosure plates (`channels/8bitnews-us/format.md`, Legal R4/R5)

`studio/engine.py:active_plates` is the single source of truth for plate
timing, kept separate from pixel drawing so it is unit-testable
(`tests/test_ai_plates_timing.py`):

- `ai_voice` — the whole video's first 3 seconds (`AI_VOICE_PLATE_S` in
  `studio/constants.py`), top of frame.
- `ai_icon` — every frame after that (mutually exclusive with `ai_voice`).
- `ai_illustration` — any frame of a scene whose `real_people` is
  non-empty, for that scene's entire duration.
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
the file today.

### Subtitles

`channels/8bitnews-us/format.md` caps subtitle lines at "up to 32
characters" assuming the full 270px logical width. The channel's actual
`common_safe_rect` is 190px wide, which at the 8px pixel font fits 23
monospace characters after padding — so `MAX_SUBTITLE_CHARS_PER_LINE` in
`studio/constants.py` is 23 (a tighter, still-compliant choice, documented
there). The timeline wraps subtitles once, independently of which
template renders them.

### Motion

Every scene has motion (`format.md`): `shot: scene` images zoom in by
whole pixels over the scene's duration (`studio/engine.py:_zoom_crop` crops
a shrinking border and rescales with `NEAREST`, so no new colors are
introduced); `shot: anchor` scenes animate the anchor's blink and mouth
(2-3 frames, driven by the timeline's volume envelope). A template's
`motion_presets` can also declare `pan` and `parallax` entries (schema
supports them, for backgrounds with multiple parallax layers) — the engine
in this increment reads `zoom` and `element_appear` presets; `pan`/
`parallax` are validated data today and are a natural follow-up once a
template actually needs them.

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
`serious`), two eye states (`open`, `closed`, for blinking) and three mouth
frames (`0`/`1`/`2`, picked by `channels/8bitnews-us/anchor/manifest.json`'s
`mouth.volume_thresholds` against the timeline's volume envelope). A future
Design pass replaces the PNG files in place (same filenames, same canvas
size) — no engine code changes.

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

## Known gaps (left for E2a-2 / E2a-3)

- No TTS, no real voice volume envelope (the uniform timeline stands in).
- No music, no loudness/ducking.
- No `studio build`, no `package.json`, no end-to-end video assembly from a
  real script end to end — `template preview` only runs the built-in demo
  script.
- `pan` / `parallax` motion presets are schema-valid data but not yet
  consumed by the renderer (see "Motion" above).
