"""`studio build`: script.json + channel profile + template package -> a
self-contained video_package directory (studio/README.md). Pipeline:
voice (per scene TTS) -> timeline from those word timings, with the scene
pauses closed a little if the video is narrowly too long -> a script that
still misses the channel's length stops here, before pictures are paid for ->
scene pictures -> frames ->
the picture, encoded once -> soundtrack (voice, ducked music, transition
sounds; mastered to the loudness target) muxed onto it, and a second one
without music for a long video -> cover + keyframes -> package.json
(schema-validated, with `studio validate` run against the fresh package and
recorded in it).
"""
from __future__ import annotations

import hashlib
import json
import math
import shutil
import sys
import wave
from collections.abc import Collection
from pathlib import Path

import jsonschema
from PIL import Image, ImageDraw, ImageFont

from . import constants, repo
from .audio import (
    Loudness,
    MixResult,
    SfxEvent,
    concat_wavs_with_silence,
    decode,
    measure,
    mix,
    prepend_silence,
    write_audio,
)
from .channel_assets import ChannelAssets, anchor_ids, load_channel_assets, pick_anchor
from .checks import run_checks
from .config import audio_settings, image_settings, tts_settings
from .constants import (
    FONT_PX,
    FPS,
    LOGICAL_H,
    LOGICAL_W,
    MAX_SUBTITLE_CHARS_PER_LINE,
    MAX_SUBTITLE_LINES,
)
from .engine import (
    _next_scene_shot,
    active_plates,
    ai_label_box,
    draw_ai_label,
    plate_lines,
    plate_max_lines,
    render_frame,
    scene_palette,
    scene_transition_duration,
    transition_sound,
)
from .cover_title import COVER_LAYOUTS, cover_font_px, cover_title_layout, fit_cover_title
from .errors import BuildError
from .music import choose_music
from .palette import hex_to_rgb
from .paths import TEMPLATES_DIR
from .pronunciation import build_synthesis_text, hinted_tokens, regroup_words, unused_pronunciations
from .quantize import quantize
from .safe_zones import common_safe_rect
from .srt import write_srt
from .subtitles import wrap_words
from .template_pkg import TemplatePackage, load_template
from .text_render import draw_text
from .timeline import SceneVoice, Timeline, Word, fitted_pause, timeline_from_voice, timeline_to_dict
from .tts import ADAPTERS
from .video import OUTPUT_H, OUTPUT_W, encode_video_only, mux, probe_duration_s, upscale_nearest


def _sha256_and_size(path: Path) -> tuple[str, int]:
    h = hashlib.sha256()
    size = 0
    with open(path, "rb") as f:
        for chunk in iter(lambda: f.read(1 << 20), b""):
            h.update(chunk)
            size += len(chunk)
    return h.hexdigest(), size


def _media_entry(path: Path, base: Path) -> dict:
    sha256, size = _sha256_and_size(path)
    return {"path": str(path.relative_to(base)), "sha256": sha256, "bytes": size}


def _script_artifact(script_path: Path, root: Path | None) -> str:
    """`package.script`: the script's path relative to the repo root when it
    lives inside it (not the container mount point), else as given."""
    if root is not None:
        try:
            return str(Path(script_path).resolve().relative_to(Path(root).resolve()))
        except ValueError:
            pass
    return str(script_path)


def _load_script_validated(script_path: Path) -> dict:
    try:
        data = json.loads(Path(script_path).read_text())
    except json.JSONDecodeError as e:
        raise BuildError(f"{script_path} is not valid JSON: {e}") from e
    schema = repo.load_contract_schema("script.schema.json")
    validator = jsonschema.Draft202012Validator(schema)
    problems = [
        f"{'/'.join(str(p) for p in e.path) or '<root>'}: {e.message}"
        for e in sorted(validator.iter_errors(data), key=lambda e: list(e.path))
    ]
    if problems:
        raise BuildError(
            f"{script_path} failed script.schema.json validation ({len(problems)} problem(s)):\n"
            + "\n".join(f"  - {p}" for p in problems)
        )
    return data


def _resolve_template_dir(template_arg: str | None, script: dict, channel_profile: dict) -> Path:
    """`--template` -> `script.template` -> channel `format.default_template`
    (studio/README.md step 5). `--template` may be either a template id
    (looked up under studio/templates/) or a direct path to a package dir."""
    if template_arg:
        as_dir = Path(template_arg)
        if as_dir.is_dir():
            return as_dir
        as_id = TEMPLATES_DIR / template_arg
        if as_id.is_dir():
            return as_id
        raise BuildError(f"--template {template_arg!r} is not a directory and not a known template id")
    template_id = script.get("template") or channel_profile["format"]["default_template"]
    template_dir = TEMPLATES_DIR / template_id
    if not template_dir.is_dir():
        raise BuildError(f"template {template_id!r} (script.template / channel default_template) not found at {template_dir}")
    return template_dir


def _resolve_tts_name(tts_arg: str | None, channel_profile: dict) -> str:
    name = tts_arg or channel_profile["format"]["voice"]["provider"]
    if name not in ADAPTERS:
        available = ", ".join(sorted(ADAPTERS))
        raise BuildError(
            f"TTS provider {name!r} is not implemented (available: {available}); pass --tts stub "
            "for a deterministic placeholder voice"
        )
    return name


def check_plate_text(script: dict, template: TemplatePackage) -> None:
    """Raises BuildError if any scene's `on_screen_text` or `source_plate`
    needs more lines than its plate's `max_lines`: plates never cut text
    (a source date dropped off the end is a wrong attribution)."""
    problems = []
    for scene in script["scenes"]:
        for field, plate_name in (("on_screen_text", "scene_title"), ("source_plate", "source_plate")):
            text = scene.get(field)
            if not text:
                continue
            lines = plate_lines(template, plate_name, text)
            limit = plate_max_lines(template, plate_name)
            if len(lines) > limit:
                problems.append(
                    f"scene {scene['id']}: {field} {text!r} needs {len(lines)} lines, "
                    f"plates.{plate_name} of template {template.id!r} holds {limit} (max_lines)"
                )
    if problems:
        raise BuildError("scene text does not fit its plate:\n" + "\n".join(f"  - {p}" for p in problems))


def cover_plate_layout(text: str, template: TemplatePackage, assets: ChannelAssets,
                       layout: str) -> tuple[dict, list[tuple[str, tuple[int, int]]], ImageFont.FreeTypeFont]:
    """(plate box, title lines with their origins, font) of `cover.text`: wrapped
    by `cover.layout.text_rect` w/h, the plate just above the AI strip
    ("bottom") or just under the top of the channel's common safe zone ("top")."""
    cover_cfg = template.data["cover"]["layout"]
    font = assets.font(cover_font_px(cover_cfg))
    ai_strip_top = ai_label_box(template, assets.labels.cover, assets.font(FONT_PX))["y"]
    box, lines = cover_title_layout(text, cover_cfg["text_rect"], font, layout, ai_strip_top,
                                    common_safe_rect(assets.channel_id)["y"])
    return box, lines, font


def _draw_cover_title(frame: Image.Image, text: str, template: TemplatePackage, assets: ChannelAssets,
                      layout: str) -> dict:
    """Draws `cover.text` on one solid black plate (`cover_plate_layout`), each
    line centered, in the subtitle colors with their outline; `frame` is a
    logical-size RGBA image, drawn on in place. Returns the plate's box."""
    sub_style = template.data["subtitles"]["style"]
    fg = hex_to_rgb(sub_style["color"])
    outline = hex_to_rgb(sub_style["outline_color"])
    box, lines, font = cover_plate_layout(text, template, assets, layout)
    ImageDraw.Draw(frame).rectangle([box["x"], box["y"], box["x"] + box["w"] - 1, box["y"] + box["h"] - 1],
                                    fill=(0, 0, 0, 255))
    for line, xy in lines:
        draw_text(frame, xy, line, font, fg, outline=outline)
    return box


def _cover_picture_id(script: dict, has_picture) -> str | None:
    """Whose picture the cover shows: "custom" or a scene id, None when there
    is none. A scene shot shows its own picture; an anchor shot what its screen
    shows (engine._render_anchor_shot): its own picture when it has one and a
    scene shot follows, else the next scene shot's; the last anchor shot's
    screen shows the channel logo, so it has none. `has_picture(name)`: is
    there an <assets>/<name>.png (custom: cover.png)."""
    frame_id = script["cover"]["frame"]
    if frame_id == "custom":
        return "custom" if has_picture("cover") else None
    scene = next((s for s in script["scenes"] if s["id"] == frame_id), None)
    if scene is None:
        raise BuildError(f"cover.frame {frame_id!r} does not match any scene id in the script")
    if scene["shot"] == "anchor":
        following = _next_scene_shot(script, frame_id)
        if following is None:
            return None
        if not has_picture(frame_id):
            frame_id = following["id"]
    return frame_id if has_picture(frame_id) else None


def _no_cover_picture(script: dict) -> BuildError:
    frame_id = script["cover"]["frame"]
    scene = next((s for s in script["scenes"] if s["id"] == frame_id), None)
    if scene is not None and scene["shot"] == "anchor" and _next_scene_shot(script, frame_id) is None:
        why = "it is the last anchor scene, whose screen shows the channel logo"
    else:
        why = "its picture is missing"
    return BuildError(f"cover.frame {frame_id!r}: no picture for the cover ({why}); choose the scene whose picture "
                      "best shows the new turn, preferably a shot: scene")


def check_cover(script: dict, assets: ChannelAssets, assets_dir: Path | None, images: str | None) -> None:
    """Raises BuildError, before TTS and before any picture is paid for, on a
    cover the build can't make: `cover.text` with the AI line's own words in it
    (the cover's AI line says them, format.md), or a `cover.frame` with no
    picture to show (`_cover_picture_id`) among the pictures `--images` will
    draw (`scene_images.picture_jobs`) or `assets_dir` holds. With neither, the
    cover is the template's background (sample builds, tests)."""
    text, words = script["cover"]["text"], assets.labels.text
    if words.lower() in text.lower():
        raise BuildError(f"cover text {text!r} contains {words!r}: the cover's AI line already says "
                         f"{assets.labels.cover!r}, leave it out of cover.text")
    if images is not None:
        from .scene_images import picture_jobs

        drawn = {name for name, _visual, _kind in picture_jobs(script)}
        has_picture = drawn.__contains__
    elif assets_dir is not None:
        has_picture = lambda name: (Path(assets_dir) / f"{name}.png").is_file()  # noqa: E731
    else:
        return
    if _cover_picture_id(script, has_picture) is None:
        raise _no_cover_picture(script)


def _cover_picture(script: dict, template: TemplatePackage, assets: ChannelAssets,
                   assets_dir: Path | None) -> tuple[Image.Image, str]:
    """The picture across the whole cover and whose it is (`_cover_picture_id`),
    fitted to 270x480 and snapped like a scene picture (`scene_palette`); an
    anchor scene's landscape screen picture is center-cropped. Without
    `assets_dir` (sample builds, tests), the template's cover background."""
    serious = bool(script.get("serious_mode"))
    if assets_dir is None:
        _cover_picture_id(script, lambda name: True)  # an unknown cover.frame still fails
        background = template.load_asset_image(template.data["cover"]["layout"]["background"]).convert("RGB")
        palette = template.palette(serious=serious)
        return quantize(background, palette, target_w=LOGICAL_W, target_h=LOGICAL_H).convert("RGBA"), "template"
    source = _cover_picture_id(script, lambda name: (Path(assets_dir) / f"{name}.png").is_file())
    if source is None:
        raise _no_cover_picture(script)
    path = Path(assets_dir) / f"{'cover' if source == 'custom' else source}.png"
    palette = scene_palette(template, serious, assets.channel_id)
    return quantize(Image.open(path), palette, target_w=LOGICAL_W, target_h=LOGICAL_H).convert("RGBA"), source


def _render_cover(
    script: dict,
    template: TemplatePackage,
    assets: ChannelAssets,
    assets_dir: Path | None,
    out_path: Path,
    layout: str,
) -> dict:
    """`cover.png` (format.md 'Обложка', owner 07.11.2026): the story picture
    (`_cover_picture`) across the whole frame -- no anchor, no studio -- with the
    title on its plate at the bottom or the top (`layout`, alternated by `studio
    pick`) and the cover's AI line (`AiLabels.cover`: the picture and the title
    are generated). Drawn at 270x480 and upscaled, not re-quantized: the title
    keeps the subtitle colors. Returns package.json's `cover` record."""
    picture, source = _cover_picture(script, template, assets, assets_dir)
    frame = Image.new("RGBA", (LOGICAL_W, LOGICAL_H), (0, 0, 0, 255))
    frame.alpha_composite(picture)
    _draw_cover_title(frame, script["cover"]["text"], template, assets, layout)
    draw_ai_label(frame, template, assets.labels.cover, assets.font(FONT_PX))
    upscale_nearest(frame.convert("RGB")).save(out_path)
    return {"frame": script["cover"]["frame"], "layout": layout, "picture": source}


def resolve_redraw(script: dict, redraw: Collection[str]) -> tuple[set[str], list[str]]:
    """`--redraw` names -> the pictures to draw again (`scene_images.picture_jobs`), and notes on any name
    that isn't one. A remark on the cover or on an anchor shot is about the picture it shows: for a cover
    without its own, the scene's (`_cover_picture_id`); on an anchor shot without its own, the next scene
    shot's (engine._render_anchor_shot). A name that isn't in this script stays: it may be a scene id of the
    earlier package, before Script moved the scenes."""
    from .scene_images import picture_jobs

    drawn = {name for name, _visual, _kind in picture_jobs(script)}
    names, notes = set(redraw), []
    for name in sorted(set(redraw) - drawn):
        scene = next((s for s in script["scenes"] if s["id"] == name), None)
        if name == "cover":
            shown = _cover_picture_id(script, drawn.__contains__)
        elif scene is not None:
            following = _next_scene_shot(script, name)
            shown = following["id"] if following else None
        else:
            continue
        if shown in drawn:
            names.add(shown)
            notes.append(f"--redraw {name}: it shows {shown}'s picture, which is drawn again")
        else:
            notes.append(f"--redraw {name}: it shows no drawn picture, so there is nothing to draw again")
    return names, notes


def _keyframe_targets(template: TemplatePackage, timeline: Timeline, n_frames: int) -> dict[int, list[tuple[str, str]]]:
    """Frame index -> list of (scene_id, kind) keyframes to also save at
    that index: the first frame after the scene's transition ends, and the
    scene's middle frame."""
    targets: dict[int, list[tuple[str, str]]] = {}
    for scene_index, scene_timing in enumerate(timeline.scenes):
        transition_dur = scene_transition_duration(template, scene_index)
        first_t = min(scene_timing.start_s + transition_dur, scene_timing.end_s - 1.0 / FPS)
        first_frame = min(max(0, math.ceil(first_t * FPS)), n_frames - 1)
        mid_t = (scene_timing.start_s + scene_timing.end_s) / 2
        mid_frame = min(max(first_frame, round(mid_t * FPS)), n_frames - 1)
        targets.setdefault(first_frame, []).append((scene_timing.scene_id, "first"))
        targets.setdefault(mid_frame, []).append((scene_timing.scene_id, "mid"))
    return targets


def _assemble_package(
    *,
    script: dict,
    script_artifact: str,
    channel_id: str,
    template: TemplatePackage,
    timeline: Timeline,
    out_dir: Path,
    files: dict,
    cover: dict,
    voice: dict,
    synth_narrations: dict[str, str],
    loudness: Loudness,
    duration_s: float,
    music: dict,
    channel_profile: dict,
    extra_notes: list[str],
    anchor: dict,
    images: dict | None = None,
) -> dict:
    total_words = 0
    total_span = 0.0
    scenes_out = []
    for scene in script["scenes"]:
        scene_timing = next(s for s in timeline.scenes if s.scene_id == scene["id"])
        words = scene_timing.words
        if words:
            total_words += len(words)
            total_span += words[-1].end_s - words[0].start_s
        mid_t = (scene_timing.start_s + scene_timing.end_s) / 2
        plates_mid = active_plates(script, scene_timing, template, mid_t)
        ai_label_shown = "ai_illustration" in plates_mid
        keyframes = [
            _media_entry(out_dir / "keyframes" / f"{scene['id']}_{kind}.png", out_dir) for kind in ("first", "mid")
        ]
        scenes_out.append(
            {
                "id": scene["id"],
                "start_s": scene_timing.start_s,
                "end_s": scene_timing.end_s,
                "narration": synth_narrations[scene["id"]],
                "on_screen_text": scene.get("on_screen_text", ""),
                "keyframes": keyframes,
                "real_people": scene.get("real_people", []),
                "ai_label_shown": ai_label_shown,
            }
        )

    wpm = (total_words * 60.0 / total_span) if total_span > 0 else 0.0

    enabled_platforms = [p for p, cfg in channel_profile.get("platforms", {}).items() if cfg.get("enabled")]
    disclosures = {
        "ai_voice": True,
        "ai_visuals": True,
        "platforms": {p: {"ai_label": True} for p in enabled_platforms},
    }

    package: dict = {
        "channel_id": channel_id,
        "script": script_artifact,
        "template": template.id,
        "anchor": anchor,
        "duration_s": duration_s,
        "files": files,
        "cover": cover,
        "render": {
            "width": OUTPUT_W,
            "height": OUTPUT_H,
            "fps": float(FPS),
            "video_codec": "h264",
            "audio_codec": "aac",
            "loudness_lufs": loudness.lufs,
            "true_peak_dbtp": loudness.true_peak_dbtp,
            "engine": "studio-engine",
            "template_dir": "template",
        },
        "voice": {**voice, "wpm": wpm},
        "transcript": " ".join(s["narration"] for s in script["scenes"]),
        "scenes": scenes_out,
        "music": music,
        "disclosures": disclosures,
        "notes": extra_notes,
    }
    if images is not None:  # before the checks: picture_fallback reads it
        package["images"] = images
    package["validate"] = run_checks(out_dir, package, channel_profile)
    return package


def _soundtrack(
    picture: Path, out_path: Path, work_wav: Path, voice_raw: Path, timeline: Timeline, audio_cfg: dict,
    music_path: Path | None, sfx: list[SfxEvent],
) -> MixResult:
    """Mixes and masters one soundtrack and muxes it onto `picture`. AAC
    encoding raises the true peak a little, so it is measured on the encoded
    file; above the check's limit (less a margin) the limiter ceiling comes
    down by the overshoot and the soundtrack is redone."""
    words = [(w.start_s, w.end_s) for s in timeline.scenes for w in s.words]
    allowed = constants.MAX_TRUE_PEAK_DBTP - _TRUE_PEAK_MARGIN_DB
    ceiling = audio_cfg["true_peak_ceiling_dbtp"]
    result = None
    for _ in range(_TRUE_PEAK_ROUNDS):
        result = mix(voice_raw, timeline.duration_s, words, audio_cfg, music_path=music_path, sfx=sfx,
                     ceiling_db=ceiling)
        write_audio(work_wav, result.master, int(audio_cfg["sample_rate"]))
        mux(picture, work_wav, out_path, audio_cfg["aac_bitrate"], int(audio_cfg["sample_rate"]))
        peak = measure(out_path).true_peak_dbtp
        if peak <= allowed:
            break
        ceiling -= peak - allowed + 0.1
    work_wav.unlink(missing_ok=True)
    return result


_TRUE_PEAK_MARGIN_DB = 0.2
_TRUE_PEAK_ROUNDS = 4


# How much room a requested cut leaves below the channel's limit.
CUT_MARGIN_S = 1.0


def check_narration_length(duration_s: float, word_count: int, channel_profile: dict) -> None:
    """A script whose voice misses the channel's length goes back before any
    picture is paid for, told how far off it is: without a number, a writer
    told "too long" cut a video 0.12 s over by 18 s. Words are counted at this
    voice's own pace, not the channel's budget, which is slower; the rebuild
    may also pick another anchor, which CUT_MARGIN_S leaves room for."""
    length = channel_profile["format"]["duration_s"]
    low, high = length["min"], length["max"]
    if low <= duration_s <= high:
        return
    off = (duration_s - high if duration_s > high else low - duration_s) + CUT_MARGIN_S
    words = math.ceil(off * word_count / duration_s)
    amount = f"about {off:.1f}s, about {words} words at this voice's pace, and no more; leave the rest of the script as it is"
    if duration_s > high:
        raise BuildError(f"narration length: the voice runs {duration_s:.2f}s, over the channel's {high}s. Cut {amount}")
    raise BuildError(f"narration length: the voice runs {duration_s:.2f}s, under the channel's {low}s. Add {amount}")


def build_package(
    script_path: Path,
    channel_id: str,
    out_dir: Path,
    channels_dir: Path | None = None,
    template_dir_arg: str | None = None,
    assets_dir: Path | None = None,
    tts_name: str | None = None,
    keep_work: bool = False,
    music_file: Path | None = None,
    music_track: str | None = None,
    music_catalog: Path | None = None,
    music_dir: Path | None = None,
    audio_overrides: dict | None = None,
    anchor_id: str | None = None,
    images: str | None = None,
    image_client=None,
    cover_layout: str = "bottom",
    reuse_from: Path | None = None,
    reuse_package_id: str | None = None,
    redraw: Collection[str] = (),
    notes: Collection[str] = (),
) -> Path:
    """`keep_work` keeps frames/ (every rendered logical frame) after the
    build for debugging; by default it is deleted once nothing needs it.
    `anchor_id`: which of the channel's anchors presents the video (hub
    `anchor_pick`); without it, one picked by the script's content.
    `images="openrouter"`: draw the scene pictures with the image provider
    (studio.scene_images) instead of reading them from `assets_dir`;
    `image_client` replaces the OpenRouter client (tests).
    `reuse_from`: the directory of an earlier package of this video (its
    package.json and images/; `reuse_package_id` is how the report names it),
    whose accepted pictures stand in where nothing that feeds them changed
    (studio.scene_images), except those `redraw` names; an unreadable one
    means every picture is drawn, with a note. `notes`: added to package.notes.
    `cover_layout`: where the cover title's plate goes, "bottom" or "top"
    (`studio pick` alternates them).
    `music_file`/`music_track`: see `studio.music.choose_music`;
    `audio_overrides` replaces mix levels from config.json (tests)."""
    script_path = Path(script_path)
    out_dir = Path(out_dir)
    out_dir.mkdir(parents=True, exist_ok=True)

    script = _load_script_validated(script_path)
    channel_profile = repo.load_channel_profile(channel_id, channels_dir=channels_dir)
    serious = bool(script.get("serious_mode"))

    template_dir = _resolve_template_dir(template_dir_arg, script, channel_profile)
    template = load_template(template_dir)
    anchor_notes = []
    if anchor_id is None:
        anchor_id, chosen_by = pick_anchor(channel_id, script), "script"
    else:
        chosen_by = "argument"
        if anchor_id not in anchor_ids(channel_id, include_retired=True):
            have = ", ".join(anchor_ids(channel_id)) or "none"
            raise BuildError(f"anchor {anchor_id!r} is not an anchor of {channel_id} (active: {have})")
        if anchor_id not in anchor_ids(channel_id):
            # Rebuilding an old video keeps its anchor; a new one never gets a retired anchor from hub.
            anchor_notes.append(f"anchor {anchor_id!r} is retired: only for rebuilding a video it already presented")
    assets = load_channel_assets(channel_id, anchor_id)
    # Fail before TTS and rendering, not after them, if plate text, the cover
    # title or the music can't work out.
    check_plate_text(script, template)
    cover_cfg = template.data["cover"]["layout"]
    fit_cover_title(script["cover"]["text"], cover_cfg["text_rect"], assets.font(cover_font_px(cover_cfg)))
    if cover_layout not in COVER_LAYOUTS:
        raise BuildError(f"cover layout {cover_layout!r}: expected one of {', '.join(COVER_LAYOUTS)}")
    check_cover(script, assets, assets_dir, images)
    music_choice = choose_music(script, channel_profile, music_file, music_track, music_catalog, music_dir)
    audio_cfg = audio_settings(audio_overrides)

    images_report = None
    if images is None and (reuse_from is not None or redraw):
        raise BuildError("--reuse-from and --redraw are about drawn pictures: they need --images")
    if images is not None:
        if images != "openrouter":
            raise BuildError(f"--images {images!r}: the only image provider is 'openrouter'")
        if assets_dir is not None:
            raise BuildError("--images and --assets both give the scene pictures: pass one")
        from .openrouter import OpenRouterClient

        image_client = image_client or OpenRouterClient()
        redraw_names, redraw_notes = resolve_redraw(script, redraw)

    tts_name_resolved = _resolve_tts_name(tts_name, channel_profile)
    # Each anchor speaks with its own voice (format.md, 'Ведущие').
    voice_id = assets.anchor.voice_id
    adapter = ADAPTERS[tts_name_resolved].from_settings(tts_settings(tts_name_resolved), serious=serious,
                                                        voice_id=voice_id, language=script.get("language"))

    sub_style = template.data["subtitles"]["style"]
    max_lines = sub_style.get("max_lines", MAX_SUBTITLE_LINES)
    max_chars = sub_style.get("max_chars_per_line", MAX_SUBTITLE_CHARS_PER_LINE)
    pause_s = template.data.get("timing", {}).get("scene_pause_s", 0.3)

    pronunciations = script.get("pronunciations", [])
    extra_notes = list(notes) + anchor_notes
    for text in unused_pronunciations([s["narration"] for s in script["scenes"]], pronunciations):
        note = (
            f"pronunciation hint {text!r} matched no narration word and was not applied "
            "(hints match single whole words only)"
        )
        print(f"warning: {note}", file=sys.stderr)
        extra_notes.append(note)
    scene_voices: list[SceneVoice] = []
    scene_wavs: list[Path] = []
    synth_narrations: dict[str, str] = {}
    # Transition sounds: none in serious mode (format.md). A scene that
    # enters with one gets that much silence before its voice, so the sound
    # plays with the transition and never over a word.
    sounds = {i: transition_sound(template, i) for i in range(len(script["scenes"]))} if not serious else {}
    sounds = {i: p for i, p in sounds.items() if p is not None}
    for i, scene in enumerate(script["scenes"]):
        synth_text, group_sizes = build_synthesis_text(scene["narration"], pronunciations)
        synth_narrations[scene["id"]] = synth_text
        hinted = hinted_tokens(group_sizes, scene["narration"], pronunciations)
        tts_result = adapter.synthesize(synth_text, voice_id, hinted)
        words = regroup_words(scene["narration"], group_sizes, tts_result.words)
        duration = tts_result.duration_s
        if i in sounds:
            lead = prepend_silence(tts_result.wav_path, _wav_length(sounds[i]))
            words = tuple(Word(w.text, w.start_s + lead, w.end_s + lead) for w in words)
            duration = (duration if duration is not None else words[-1].end_s - lead) + lead
        scene_voices.append(SceneVoice(scene_id=scene["id"], shot=scene["shot"], words=words, duration_s=duration))
        scene_wavs.append(tts_result.wav_path)

    font_scale = sub_style.get("font_scale", 1)
    timeline = timeline_from_voice(scene_voices, pause_s, max_lines, max_chars, font_scale)
    length = channel_profile["format"]["duration_s"]
    gaps = len(scene_voices) - 1
    fitted = fitted_pause(timeline.duration_s - gaps * pause_s, gaps, pause_s, length["max"])
    if fitted != pause_s:
        extra_notes.append(f"scene pauses {pause_s:.3f}s -> {fitted:.3f}s: the narration ran {timeline.duration_s:.2f}s "
                           f"against the channel's {length['max']}s; the words are unchanged")
        pause_s = fitted
        timeline = timeline_from_voice(scene_voices, pause_s, max_lines, max_chars, font_scale)
    if images is not None:
        check_narration_length(timeline.duration_s, sum(len(s["narration"].split()) for s in script["scenes"]), channel_profile)

    voice_raw_path = out_dir / "_voice_raw.wav"
    voice_sr = concat_wavs_with_silence(scene_wavs, pause_s, voice_raw_path)
    for p in scene_wavs:
        Path(p).unlink(missing_ok=True)

    sfx = [SfxEvent(t_s=timeline.scenes[i].start_s, path=p, duration_s=_wav_length(p)) for i, p in sorted(sounds.items())]

    if images is not None:
        from .scene_images import generate_scene_images, load_earlier_pictures

        cfg = image_settings()
        earlier, unreadable = (None, None) if reuse_from is None else load_earlier_pictures(
            reuse_from, reuse_package_id or str(reuse_from))
        assets_dir, images_report = generate_scene_images(
            script, template, channel_id, out_dir, image_client,
            model=cfg["model"], check_model=cfg["check_model"], earlier=earlier, redraw=redraw_names,
        )
        if unreadable:
            images_report.notes.append(f"the earlier package's pictures can't be reused ({unreadable}): every picture "
                                       "was drawn again")
        images_report.notes.extend(redraw_notes)

    frames_dir = out_dir / "frames"
    if frames_dir.exists():
        shutil.rmtree(frames_dir)
    frames_dir.mkdir(parents=True)
    keyframes_dir = out_dir / "keyframes"
    if keyframes_dir.exists():
        shutil.rmtree(keyframes_dir)
    keyframes_dir.mkdir(parents=True)

    n_frames = max(int(round(timeline.duration_s * FPS)), 1)
    keyframe_targets = _keyframe_targets(template, timeline, n_frames)
    keyframe_records = []
    for i in range(n_frames):
        t = i / FPS
        frame = render_frame(script, timeline, template, assets, t, assets_dir)
        frame.save(frames_dir / f"frame_{i:06d}.png")
        for scene_id, kind in keyframe_targets.get(i, []):
            kf_path = keyframes_dir / f"{scene_id}_{kind}.png"
            upscale_nearest(frame).save(kf_path)
            keyframe_records.append(
                {"path": str(kf_path.relative_to(out_dir)), "scene_id": scene_id, "kind": kind, "frame": i, "t_s": t}
            )

    picture_path = out_dir / "_picture.mp4"
    encode_video_only(frames_dir, picture_path)
    work_wav = out_dir / "_master.wav"
    video_path = out_dir / "video.mp4"
    mixed = _soundtrack(picture_path, video_path, work_wav, voice_raw_path, timeline, audio_cfg,
                        music_choice.path if music_choice else None, sfx)
    duration_s = probe_duration_s(video_path)

    # The voice alone, as synthesized, and the music bed as mixed in, both
    # before the master gain (nothing clips): the checks look for them in
    # the shipped soundtracks.
    stems_dir = out_dir / "stems"
    if stems_dir.exists():
        shutil.rmtree(stems_dir)
    write_audio(stems_dir / "voice.flac", decode(voice_raw_path, voice_sr, channels=1), voice_sr, codec="flac")
    no_music_path = out_dir / "video_no_music.mp4"
    no_music_path.unlink(missing_ok=True)
    if music_choice is not None:
        write_audio(stems_dir / "music.flac", mixed.music, int(audio_cfg["sample_rate"]), codec="flac")
        if duration_s > constants.YOUTUBE_MUSIC_MAX_S:
            _soundtrack(picture_path, no_music_path, work_wav, voice_raw_path, timeline, audio_cfg, None, sfx)
    picture_path.unlink(missing_ok=True)
    voice_raw_path.unlink(missing_ok=True)

    all_cues = [cue for scene_timing in timeline.scenes for cue in scene_timing.subtitles]
    srt_path = out_dir / "subtitles.srt"
    write_srt(all_cues, srt_path)

    cover_path = out_dir / "cover.png"
    cover = _render_cover(script, template, assets, assets_dir, cover_path, cover_layout)

    timeline_path = out_dir / "timeline.json"
    timeline_dict = timeline_to_dict(timeline)
    timeline_dict["keyframes"] = keyframe_records
    timeline_dict["sfx"] = [
        {"t_s": ev.t_s, "duration_s": ev.duration_s, "sound": str(Path(ev.path).relative_to(template.dir))}
        for ev in sfx
    ]
    timeline_path.write_text(json.dumps(timeline_dict, indent=2))

    script_out_path = out_dir / "script.json"
    shutil.copyfile(script_path, script_out_path)

    template_out_dir = out_dir / "template"
    if template_out_dir.exists():
        shutil.rmtree(template_out_dir)
    shutil.copytree(template_dir, template_out_dir)

    loudness = measure(video_path)
    try:
        root = repo.repo_root()
    except repo.RepoRootNotSet:
        root = None

    files = {
        "video": _media_entry(video_path, out_dir),
        "subtitles": _media_entry(srt_path, out_dir),
        "cover": _media_entry(cover_path, out_dir),
        "timeline": _media_entry(timeline_path, out_dir),
        "template": _media_entry(template_out_dir / "template.json", out_dir),
        "script": _media_entry(script_out_path, out_dir),
        "voice_stem": _media_entry(stems_dir / "voice.flac", out_dir),
    }
    music_out: dict = {"enabled": False}
    if music_choice is not None:
        files["music_stem"] = _media_entry(stems_dir / "music.flac", out_dir)
        if no_music_path.is_file():
            files["video_no_music"] = _media_entry(no_music_path, out_dir)
        music_out = {"enabled": True, "track_id": music_choice.track_id, "license": music_choice.license,
                     "duck_db": float(audio_cfg["duck_db"])}
        if music_choice.attribution:
            music_out["attribution"] = music_choice.attribution
        if music_choice.note:
            extra_notes.append(music_choice.note)
        if script["music"].get("cue"):
            extra_notes.append("script.music.cue is not followed: the studio fades the track in and out and ducks "
                               "it under every phrase")
    if serious and template_has_sounds(template):
        extra_notes.append("serious mode: the template's transition sounds were left out (format.md)")

    if images_report is not None:
        extra_notes.extend(images_report.notes)
    package = _assemble_package(
        script=script,
        script_artifact=_script_artifact(script_path, root),
        channel_id=channel_id,
        template=template,
        timeline=timeline,
        out_dir=out_dir,
        files=files,
        cover=cover,
        voice={"provider": tts_name_resolved, "voice_id": voice_id, **adapter.describe()},
        synth_narrations=synth_narrations,
        loudness=loudness,
        duration_s=duration_s,
        music=music_out,
        channel_profile=channel_profile,
        extra_notes=extra_notes,
        anchor={"id": anchor_id, "chosen_by": chosen_by},
        images=images_report.to_package() if images_report is not None else None,
    )

    schema = repo.load_contract_schema("video_package.schema.json")
    validator = jsonschema.Draft202012Validator(schema)
    problems = [
        f"{'/'.join(str(p) for p in e.path) or '<root>'}: {e.message}"
        for e in sorted(validator.iter_errors(package), key=lambda e: list(e.path))
    ]
    if problems:
        raise BuildError(
            "built package.json failed video_package.schema.json validation ({} problem(s)):\n{}".format(
                len(problems), "\n".join(f"  - {p}" for p in problems)
            )
        )

    package_path = out_dir / "package.json"
    package_path.write_text(json.dumps(package, indent=2))
    # Last step: frames/ is only build scratch.
    if not keep_work:
        shutil.rmtree(frames_dir, ignore_errors=True)
    return package_path


def template_has_sounds(template: TemplatePackage) -> bool:
    return any(t.get("sound") for t in template.data.get("transitions", []))


def _wav_length(path: Path) -> float:
    with wave.open(str(path), "rb") as wf:
        return wf.getnframes() / wf.getframerate()
