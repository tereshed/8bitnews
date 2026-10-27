"""`studio build`: script.json + channel profile + template package -> a
self-contained video_package directory (studio/README.md). Pipeline:
voice (per scene TTS) -> timeline from those word timings -> mix_audio
(no-op until E2a-3 adds music) -> frames (written to disk as they render,
kept around so a later pass can re-encode without music) -> encode ->
cover + keyframes -> package.json (schema-validated, with `studio validate`
run against the fresh package and recorded in it).
"""
from __future__ import annotations

import hashlib
import json
import math
import shutil
import sys
from pathlib import Path

import jsonschema
from PIL import Image

from . import repo
from .audio import concat_wavs_with_silence, measure_loudness_lufs, mix_audio
from .channel_assets import ChannelAssets, load_channel_assets
from .checks import run_checks
from .constants import (
    DEFAULT_WPM,
    FONT_PX,
    FPS,
    LOGICAL_H,
    LOGICAL_W,
    MAX_SUBTITLE_CHARS_PER_LINE,
    MAX_SUBTITLE_LINES,
    SERIOUS_WPM,
)
from .engine import active_plates, render_cover_frame, render_frame, scene_transition_duration
from .errors import BuildError
from .palette import hex_to_rgb, quantize_image
from .paths import TEMPLATES_DIR
from .pronunciation import build_synthesis_text, regroup_words, unused_pronunciations
from .quantize import quantize
from .srt import write_srt
from .subtitles import wrap_words
from .template_pkg import TemplatePackage, load_template
from .text_render import draw_text
from .timeline import SceneVoice, Timeline, timeline_from_voice, timeline_to_dict
from .tts import ADAPTERS
from .video import OUTPUT_H, OUTPUT_W, encode, probe_duration_s, upscale_nearest


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


_COVER_PAD = 2


def fit_cover_title(text: str, text_rect: dict, font) -> list[str]:
    """Word-wraps `text` inside `text_rect` at the channel's fixed pixel
    font, raising BuildError if it needs more lines than the rect has room
    for. Used by both cover kinds (`S*` and `custom`) and testable without
    rendering a frame."""
    pad = _COVER_PAD
    max_chars = max(1, (text_rect["w"] - 2 * pad) // FONT_PX)
    lines = wrap_words(text.split(), max_chars)
    # cover_title_layout puts line i at y = pad + i*(FONT_PX+2), each
    # FONT_PX tall: the first line only costs FONT_PX, every additional line
    # costs FONT_PX+2, so max_rows = 1 + how many extra (FONT_PX+2) slots
    # fit after that first line.
    max_rows = 1 + max(0, text_rect["h"] - 2 * pad - FONT_PX) // (FONT_PX + 2)
    if len(lines) > max_rows:
        raise BuildError(
            f"cover text {text!r} needs {len(lines)} line(s) but cover.layout.text_rect {text_rect} only "
            f"fits {max_rows} at {FONT_PX}px"
        )
    return lines


def cover_title_layout(text: str, text_rect: dict, font) -> list[tuple[str, tuple[int, int]]]:
    """Each cover title line with its top-left origin in logical 270x480
    coordinates: the same geometry `fit_cover_title` checks against."""
    lines = fit_cover_title(text, text_rect, font)
    x = text_rect["x"] + _COVER_PAD
    y0 = text_rect["y"] + _COVER_PAD
    return [(line, (x, y0 + i * (FONT_PX + 2))) for i, line in enumerate(lines)]


def _draw_cover_title(frame: Image.Image, text: str, template: TemplatePackage, assets: ChannelAssets) -> None:
    """Draws `cover.text` into `cover.layout.text_rect` in the subtitle
    colors; `frame` is a logical-size RGBA image, drawn on in place."""
    sub_style = template.data["subtitles"]["style"]
    fg = hex_to_rgb(sub_style["color"])
    outline = hex_to_rgb(sub_style["outline_color"])
    font = assets.font(FONT_PX)
    for line, xy in cover_title_layout(text, template.data["cover"]["layout"]["text_rect"], font):
        draw_text(frame, xy, line, font, fg, outline=outline)


def _render_custom_cover(
    script: dict, template: TemplatePackage, assets: ChannelAssets, assets_dir: Path | None, cover_cfg: dict, out_path: Path
) -> None:
    serious = bool(script.get("serious_mode"))
    palette = template.palette(serious=serious)
    custom_path = Path(assets_dir) / "cover.png" if assets_dir else None
    if custom_path is not None and custom_path.is_file():
        bg = quantize(Image.open(custom_path), palette, target_w=LOGICAL_W, target_h=LOGICAL_H).convert("RGBA")
    else:
        bg_asset = template.load_asset_image(template.data["cover"]["layout"]["background"])
        bg = quantize(bg_asset.convert("RGB"), palette, target_w=LOGICAL_W, target_h=LOGICAL_H).convert("RGBA")

    frame = Image.new("RGBA", (LOGICAL_W, LOGICAL_H), (0, 0, 0, 255))
    frame.alpha_composite(bg)
    _draw_cover_title(frame, cover_cfg["text"], template, assets)

    quantized = quantize_image(frame, palette)
    upscale_nearest(quantized.convert("RGB")).save(out_path)


def _render_cover(
    script: dict,
    timeline: Timeline,
    template: TemplatePackage,
    assets: ChannelAssets,
    assets_dir: Path | None,
    out_path: Path,
) -> None:
    cover_cfg = script["cover"]
    if cover_cfg["frame"] == "custom":
        _render_custom_cover(script, template, assets, assets_dir, cover_cfg, out_path)
        return
    scene_timing = next((s for s in timeline.scenes if s.scene_id == cover_cfg["frame"]), None)
    if scene_timing is None:
        raise BuildError(f"cover.frame {cover_cfg['frame']!r} does not match any scene id in the script")
    mid_t = (scene_timing.start_s + scene_timing.end_s) / 2
    # The title goes onto the rendered frame as is: re-quantizing it would
    # recolor the anchor sprite (see engine._render_anchor_shot).
    frame = render_cover_frame(script, timeline, template, assets, mid_t, assets_dir).convert("RGBA")
    _draw_cover_title(frame, cover_cfg["text"], template, assets)
    upscale_nearest(frame.convert("RGB")).save(out_path)


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
    video_entry: dict,
    srt_entry: dict,
    cover_entry: dict,
    timeline_entry: dict,
    script_entry: dict,
    tts_name: str,
    voice_id: str,
    synth_narrations: dict[str, str],
    loudness: float,
    duration_s: float,
    channel_profile: dict,
    extra_notes: list[str],
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
        "duration_s": duration_s,
        "files": {
            "video": video_entry,
            "subtitles": srt_entry,
            "cover": cover_entry,
            "timeline": timeline_entry,
            "script": script_entry,
        },
        "render": {
            "width": OUTPUT_W,
            "height": OUTPUT_H,
            "fps": float(FPS),
            "video_codec": "h264",
            "audio_codec": "aac",
            "loudness_lufs": loudness,
            "engine": "studio-engine",
        },
        "voice": {"provider": tts_name, "voice_id": voice_id, "wpm": wpm},
        "transcript": " ".join(s["narration"] for s in script["scenes"]),
        "scenes": scenes_out,
        "music": {"enabled": False},
        "disclosures": disclosures,
        "notes": [
            "music.enabled is always false in this build pipeline: mixing a real music bed (catalog pick, "
            "ducking under narration) is E2a-3's job, not this one's (studio/README.md E2a-2a scope).",
            *extra_notes,
        ],
    }
    package["validate"] = run_checks(out_dir, package, channel_profile)
    return package


def build_package(
    script_path: Path,
    channel_id: str,
    out_dir: Path,
    channels_dir: Path | None = None,
    template_dir_arg: str | None = None,
    assets_dir: Path | None = None,
    tts_name: str | None = None,
    keep_work: bool = False,
) -> Path:
    """`keep_work` keeps frames/ (every rendered logical frame) after the
    build for debugging; by default it is deleted once nothing needs it."""
    script_path = Path(script_path)
    out_dir = Path(out_dir)
    out_dir.mkdir(parents=True, exist_ok=True)

    script = _load_script_validated(script_path)
    channel_profile = repo.load_channel_profile(channel_id, channels_dir=channels_dir)

    template_dir = _resolve_template_dir(template_dir_arg, script, channel_profile)
    template = load_template(template_dir)
    assets = load_channel_assets(channel_id)
    # Fail before TTS and rendering, not after them, if the cover title can't fit.
    fit_cover_title(script["cover"]["text"], template.data["cover"]["layout"]["text_rect"], assets.font(FONT_PX))

    tts_name_resolved = _resolve_tts_name(tts_name, channel_profile)
    voice_cfg = channel_profile["format"]["voice"]
    voice_id = voice_cfg["voice_id"]
    wpm = SERIOUS_WPM if script.get("serious_mode") else DEFAULT_WPM
    adapter = ADAPTERS[tts_name_resolved](wpm=wpm)

    sub_style = template.data["subtitles"]["style"]
    max_lines = sub_style.get("max_lines", MAX_SUBTITLE_LINES)
    max_chars = sub_style.get("max_chars_per_line", MAX_SUBTITLE_CHARS_PER_LINE)
    pause_s = template.data.get("timing", {}).get("scene_pause_s", 0.3)

    pronunciations = script.get("pronunciations", [])
    extra_notes = []
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
    for scene in script["scenes"]:
        synth_text, group_sizes = build_synthesis_text(scene["narration"], pronunciations)
        synth_narrations[scene["id"]] = synth_text
        tts_result = adapter.synthesize(synth_text, voice_id)
        words = regroup_words(scene["narration"], group_sizes, tts_result.words)
        scene_voices.append(SceneVoice(scene_id=scene["id"], shot=scene["shot"], words=words))
        scene_wavs.append(tts_result.wav_path)

    timeline = timeline_from_voice(scene_voices, pause_s, max_lines, max_chars)

    voice_raw_path = out_dir / "_voice_raw.wav"
    concat_wavs_with_silence(scene_wavs, pause_s, voice_raw_path)
    for p in scene_wavs:
        Path(p).unlink(missing_ok=True)

    voice_wav_path = out_dir / "voice.wav"
    mix_audio(voice_raw_path, script["music"], voice_wav_path)
    voice_raw_path.unlink(missing_ok=True)

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

    video_path = out_dir / "video.mp4"
    encode(frames_dir, voice_wav_path, video_path)

    all_cues = [cue for scene_timing in timeline.scenes for cue in scene_timing.subtitles]
    srt_path = out_dir / "subtitles.srt"
    write_srt(all_cues, srt_path)

    cover_path = out_dir / "cover.png"
    _render_cover(script, timeline, template, assets, assets_dir, cover_path)

    timeline_path = out_dir / "timeline.json"
    timeline_dict = timeline_to_dict(timeline)
    timeline_dict["keyframes"] = keyframe_records
    timeline_path.write_text(json.dumps(timeline_dict, indent=2))

    script_out_path = out_dir / "script.json"
    shutil.copyfile(script_path, script_out_path)

    template_out_dir = out_dir / "template"
    if template_out_dir.exists():
        shutil.rmtree(template_out_dir)
    shutil.copytree(template_dir, template_out_dir)

    loudness = measure_loudness_lufs(video_path)
    duration_s = probe_duration_s(video_path)
    try:
        root = repo.repo_root()
    except repo.RepoRootNotSet:
        root = None

    package = _assemble_package(
        script=script,
        script_artifact=_script_artifact(script_path, root),
        channel_id=channel_id,
        template=template,
        timeline=timeline,
        out_dir=out_dir,
        video_entry=_media_entry(video_path, out_dir),
        srt_entry=_media_entry(srt_path, out_dir),
        cover_entry=_media_entry(cover_path, out_dir),
        timeline_entry=_media_entry(timeline_path, out_dir),
        script_entry=_media_entry(script_out_path, out_dir),
        tts_name=tts_name_resolved,
        voice_id=voice_id,
        synth_narrations=synth_narrations,
        loudness=loudness,
        duration_s=duration_s,
        channel_profile=channel_profile,
        extra_notes=extra_notes,
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
    # Last step, after the last encode: frames/ is only build scratch
    # (voice.wav stays -- E2a-3 needs the voice apart from the music).
    if not keep_work:
        shutil.rmtree(frames_dir, ignore_errors=True)
    return package_path
