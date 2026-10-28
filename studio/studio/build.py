"""`studio build`: script.json + channel profile + template package -> a
self-contained video_package directory (studio/README.md). Pipeline:
voice (per scene TTS) -> timeline from those word timings -> frames ->
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
from pathlib import Path

import jsonschema
from PIL import Image

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
    active_plates,
    _draw_plate,
    plate_lines,
    plate_max_lines,
    render_cover_frame,
    render_frame,
    scene_palette,
    scene_transition_duration,
    transition_sound,
)
from .cover_title import cover_title_layout, fit_cover_title
from .errors import BuildError
from .music import choose_music
from .palette import hex_to_rgb, quantize_image
from .paths import TEMPLATES_DIR
from .pronunciation import build_synthesis_text, hinted_tokens, regroup_words, unused_pronunciations
from .quantize import quantize
from .srt import write_srt
from .subtitles import wrap_words
from .template_pkg import TemplatePackage, load_template
from .text_render import draw_text
from .timeline import SceneVoice, Timeline, Word, timeline_from_voice, timeline_to_dict
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
    script: dict, template: TemplatePackage, assets: ChannelAssets, assets_dir: Path | None, cover_cfg: dict,
    out_path: Path, generated: bool = False,
) -> None:
    """`cover.png` from `assets_dir` (a picture: the scene palette), else the
    template's own cover background (its own colors), with the title. A
    cover the image model drew carries the "AI illustration" plate: it shows
    the story, possibly its real people by role (format.md)."""
    serious = bool(script.get("serious_mode"))
    custom_path = Path(assets_dir) / "cover.png" if assets_dir else None
    if custom_path is not None and custom_path.is_file():
        palette = scene_palette(template, serious, assets.channel_id)
        bg = quantize(Image.open(custom_path), palette, target_w=LOGICAL_W, target_h=LOGICAL_H).convert("RGBA")
    else:
        palette = template.palette(serious=serious)
        bg_asset = template.load_asset_image(template.data["cover"]["layout"]["background"])
        bg = quantize(bg_asset.convert("RGB"), palette, target_w=LOGICAL_W, target_h=LOGICAL_H).convert("RGBA")

    frame = Image.new("RGBA", (LOGICAL_W, LOGICAL_H), (0, 0, 0, 255))
    frame.alpha_composite(bg)
    if generated:
        _draw_plate(frame, template.plate("ai_illustration"), ["AI illustration"], assets.font(FONT_PX))
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
    generated: bool = False,
) -> None:
    cover_cfg = script["cover"]
    if cover_cfg["frame"] == "custom":
        _render_custom_cover(script, template, assets, assets_dir, cover_cfg, out_path, generated)
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
    files: dict,
    voice: dict,
    synth_narrations: dict[str, str],
    loudness: Loudness,
    duration_s: float,
    music: dict,
    channel_profile: dict,
    extra_notes: list[str],
    anchor: dict,
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
) -> Path:
    """`keep_work` keeps frames/ (every rendered logical frame) after the
    build for debugging; by default it is deleted once nothing needs it.
    `anchor_id`: which of the channel's anchors presents the video (hub
    `anchor_pick`); without it, one picked by the script's content.
    `images="openrouter"`: draw the scene pictures with the image provider
    (studio.scene_images) instead of reading them from `assets_dir`;
    `image_client` replaces the OpenRouter client (tests).
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
    fit_cover_title(script["cover"]["text"], template.data["cover"]["layout"]["text_rect"], assets.font(FONT_PX))
    music_choice = choose_music(script, channel_profile, music_file, music_track, music_catalog, music_dir)
    audio_cfg = audio_settings(audio_overrides)

    images_report = None
    if images is not None:
        if images != "openrouter":
            raise BuildError(f"--images {images!r}: the only image provider is 'openrouter'")
        if assets_dir is not None:
            raise BuildError("--images and --assets both give the scene pictures: pass one")
        from .openrouter import OpenRouterClient
        from .scene_images import generate_scene_images

        cfg = image_settings()
        assets_dir, images_report = generate_scene_images(
            script, template, channel_id, out_dir, image_client or OpenRouterClient(),
            model=cfg["model"], check_model=cfg["check_model"],
        )

    tts_name_resolved = _resolve_tts_name(tts_name, channel_profile)
    # Each anchor speaks with its own voice (format.md, 'Ведущие').
    voice_id = assets.anchor.voice_id
    adapter = ADAPTERS[tts_name_resolved].from_settings(tts_settings(tts_name_resolved), serious=serious,
                                                        voice_id=voice_id)

    sub_style = template.data["subtitles"]["style"]
    max_lines = sub_style.get("max_lines", MAX_SUBTITLE_LINES)
    max_chars = sub_style.get("max_chars_per_line", MAX_SUBTITLE_CHARS_PER_LINE)
    pause_s = template.data.get("timing", {}).get("scene_pause_s", 0.3)

    pronunciations = script.get("pronunciations", [])
    extra_notes = list(anchor_notes)
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

    timeline = timeline_from_voice(scene_voices, pause_s, max_lines, max_chars)

    voice_raw_path = out_dir / "_voice_raw.wav"
    voice_sr = concat_wavs_with_silence(scene_wavs, pause_s, voice_raw_path)
    for p in scene_wavs:
        Path(p).unlink(missing_ok=True)

    sfx = [SfxEvent(t_s=timeline.scenes[i].start_s, path=p, duration_s=_wav_length(p)) for i, p in sorted(sounds.items())]

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
    cover_generated = images_report is not None and any(s["id"] == "cover" for s in images_report.scenes)
    _render_cover(script, timeline, template, assets, assets_dir, cover_path, cover_generated)

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
        voice={"provider": tts_name_resolved, "voice_id": voice_id, **adapter.describe()},
        synth_narrations=synth_narrations,
        loudness=loudness,
        duration_s=duration_s,
        music=music_out,
        channel_profile=channel_profile,
        extra_notes=extra_notes,
        anchor={"id": anchor_id, "chosen_by": chosen_by},
    )
    if images_report is not None:
        package["images"] = images_report.to_package()

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
