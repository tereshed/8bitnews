"""Registry of `studio validate` checks, keyed by a fixed `id` so this
task's four checks and E2a-2b/E2a-3's later additions all show up the same
way in `package.json`'s `validate.checks` (video_package.schema.json)."""
from __future__ import annotations

import hashlib
import json
import re
import subprocess
from pathlib import Path
from typing import Callable

import jsonschema
import numpy as np
from PIL import Image

from . import repo
from .channel_assets import load_channel_assets
from .config import max_file_size_bytes, max_keyframe_cell_diff
from .constants import FONT_PX, FPS, LOGICAL_H, LOGICAL_W, OUTPUT_H, OUTPUT_W, SCALE
from .engine import _compose_anchor_sprite, _draw_plate, active_plates, cover_plates, scene_transition_duration
from .errors import TemplateValidationError
from .palette import channel_palette_set, hex_to_rgb
from .srt import parse_srt
from .template_pkg import load_template
from .timeline import load_timeline
from .validate import validate_template
from .video import probe, probe_duration_s

# How far package.duration_s may drift from the measured video.mp4 length.
DURATION_TOLERANCE_S = 0.1

CHECKS: dict[str, Callable[[Path, dict, dict], dict]] = {}


def register(check_id: str):
    def deco(fn: Callable[[Path, dict, dict], dict]):
        CHECKS[check_id] = fn
        return fn

    return deco


def _parse_rate(rate: str) -> float:
    num, _, den = rate.partition("/")
    return float(num) / float(den) if den else float(num)


@register("format")
def _check_format(package_dir: Path, package: dict, channel_profile: dict) -> dict:
    video_path = package_dir / package["files"]["video"]["path"]
    probe_data = probe(video_path)
    v_stream = next((s for s in probe_data["streams"] if s["codec_type"] == "video"), None)
    a_stream = next((s for s in probe_data["streams"] if s["codec_type"] == "audio"), None)
    value = {
        "width": v_stream.get("width") if v_stream else None,
        "height": v_stream.get("height") if v_stream else None,
        "fps": _parse_rate(v_stream["r_frame_rate"]) if v_stream else None,
        "video_codec": v_stream.get("codec_name") if v_stream else None,
        "audio_codec": a_stream.get("codec_name") if a_stream else None,
    }
    expected = {"width": 1080, "height": 1920, "fps": 30, "video_codec": "h264", "audio_codec": "aac"}
    ok = all(value[k] == expected[k] for k in expected)
    note = "1080x1920 30fps H.264+AAC" if ok else f"got {value}, expected {expected}"
    return {"id": "format", "ok": ok, "value": value, "expected": expected, "note": note}


@register("duration")
def _check_duration(package_dir: Path, package: dict, channel_profile: dict) -> dict:
    """Measures video.mp4 itself: package.duration_s alone can't catch a
    truncated or swapped file, so it only has to agree with the measurement."""
    measured = probe_duration_s(package_dir / package["files"]["video"]["path"])
    declared = package["duration_s"]
    bounds = channel_profile["format"]["duration_s"]
    lo, hi = bounds["min"], bounds["max"]
    problems = []
    if not lo <= measured <= hi:
        problems.append(f"outside channel range [{lo},{hi}]s")
    if abs(measured - declared) > DURATION_TOLERANCE_S:
        problems.append(f"differs from package.duration_s by more than {DURATION_TOLERANCE_S}s")
    summary = f"video.mp4 {measured:.2f}s, package.duration_s {declared:.2f}s"
    note = f"{summary}: " + "; ".join(problems) if problems else f"{summary}, within [{lo},{hi}]s"
    return {"id": "duration", "ok": not problems, "value": measured, "expected": {"min": lo, "max": hi}, "note": note}


@register("file_size")
def _check_file_size(package_dir: Path, package: dict, channel_profile: dict) -> dict:
    video_path = package_dir / package["files"]["video"]["path"]
    size = video_path.stat().st_size
    limit = max_file_size_bytes()
    ok = size <= limit
    note = f"{size} bytes <= {limit} byte limit" if ok else f"{size} bytes exceeds {limit} byte limit"
    return {"id": "file_size", "ok": ok, "value": size, "expected": limit, "note": note}


def _subtitle_problems(package_dir: Path, package: dict) -> list[str]:
    """Problems with subtitles.srt against timeline.json and script.json,
    grouped by kind (scene words, cue order, cue text and timing); the first
    one is what the check reports. Stops at the first cue whose text doesn't
    match, since every later cue would be off as well."""
    cues = parse_srt(package_dir / package["files"]["subtitles"]["path"])
    timeline = load_timeline(package_dir / package["files"]["timeline"]["path"])
    script = json.loads((package_dir / package["files"]["script"]["path"]).read_text())
    tol = 1.0 / FPS
    problems: list[str] = []

    # Timeline words are the script's original words: a pronunciation
    # respelling must never reach the subtitles.
    script_ids = [scene["id"] for scene in script["scenes"]]
    timeline_ids = [scene.scene_id for scene in timeline.scenes]
    if timeline_ids != script_ids:
        problems.append(f"timeline.json scenes {timeline_ids} != script scenes {script_ids}")
    narration = {scene["id"]: scene["narration"].split() for scene in script["scenes"]}
    for scene in timeline.scenes:
        got = [w.text for w in scene.words]
        want = narration.get(scene.scene_id, [])
        if got != want:
            i = next((k for k, (a, b) in enumerate(zip(got, want)) if a != b), min(len(got), len(want)))
            problems.append(
                f"timeline.json {scene.scene_id} word {i + 1}: {got[i] if i < len(got) else '<none>'!r}, "
                f"script narration has {want[i] if i < len(want) else '<none>'!r}"
            )

    for a, b in zip(cues, cues[1:]):
        if b.start_s < a.end_s - tol:
            problems.append(
                f"SRT cue {a.index} ({a.start_s:.3f}-{a.end_s:.3f}s) overlaps cue {b.index} starting {b.start_s:.3f}s"
            )
    for cue in cues:
        if not cue.start_s < cue.end_s:
            problems.append(f"SRT cue {cue.index} ends at {cue.end_s:.3f}s, not after its start {cue.start_s:.3f}s")

    # Hand words to cues in order: a cue's text without whitespace must equal
    # its words glued together (wrap_words may split a long word across
    # lines, so cue tokens and words don't map one to one).
    words = [w for scene in timeline.scenes for w in scene.words]
    wi = 0
    for cue in cues:
        target = "".join(cue.text.split())
        first = wi
        glued = ""
        while wi < len(words) and len(glued) < len(target):
            glued += words[wi].text
            wi += 1
        mine = words[first:wi]
        if not mine or glued != target:
            problems.append(
                f"SRT cue {cue.index} text {' '.join(cue.text.split())!r} != timeline words "
                f"{' '.join(w.text for w in mine)!r}"
            )
            return problems
        for w in mine:
            if not (w.start_s >= cue.start_s - tol and w.end_s <= cue.end_s + tol):
                problems.append(
                    f"word {w.text!r} {w.start_s:.3f}-{w.end_s:.3f}s is outside its SRT cue {cue.index} "
                    f"({cue.start_s:.3f}-{cue.end_s:.3f}s)"
                )
    if wi < len(words):
        problems.append(
            f"{len(words) - wi} timeline word(s) after the last SRT cue, starting with {words[wi].text!r}"
        )
    return problems


@register("subtitles_cover_words")
def _check_subtitles_cover_words(package_dir: Path, package: dict, channel_profile: dict) -> dict:
    problems = _subtitle_problems(package_dir, package)
    ok = not problems
    note = (
        "every word is in the text and time of its SRT cue (1 frame tolerance)"
        if ok
        else f"{problems[0]}" + (f" (+{len(problems) - 1} more)" if len(problems) > 1 else "")
    )
    return {"id": "subtitles_cover_words", "ok": ok, "value": len(problems), "expected": 0, "note": note}


def _result(check_id: str, problems: list[str], ok_note: str, value=None, expected=None) -> dict:
    note = ok_note if not problems else problems[0] + (f" (+{len(problems) - 1} more)" if len(problems) > 1 else "")
    return {"id": check_id, "ok": not problems, "value": len(problems) if value is None else value,
            "expected": 0 if expected is None else expected, "note": note}


def _media_entries(package: dict) -> list[tuple[str, dict]]:
    """Every file record of the package: `files.*` and each scene's keyframes."""
    entries = [(f"files.{name}", entry) for name, entry in package["files"].items()]
    for scene in package.get("scenes", []):
        for i, entry in enumerate(scene.get("keyframes", [])):
            entries.append((f"scenes[{scene['id']}].keyframes[{i}]", entry))
    return entries


@register("package_schema")
def _check_package_schema(package_dir: Path, package: dict, channel_profile: dict) -> dict:
    """package.json against video_package.schema.json (with this run's
    validate block standing in, since build runs the checks before writing
    it), and every recorded sha256/bytes against the file on disk."""
    problems = []
    candidate = package if "validate" in package else dict(
        package, validate={"passed": True, "checks": [{"id": "package_schema", "ok": True}]}
    )
    validator = jsonschema.Draft202012Validator(repo.load_contract_schema("video_package.schema.json"))
    for e in sorted(validator.iter_errors(candidate), key=lambda e: [str(p) for p in e.path]):
        problems.append(f"schema: {'/'.join(str(p) for p in e.path) or '<root>'}: {e.message}")
    root = package_dir.resolve()
    for where, entry in _media_entries(package):
        if "path" not in entry:
            problems.append(f"{where}: no local path (key-only record), so the file can't be checked")
            continue
        path = (package_dir / entry["path"]).resolve()
        if not path.is_relative_to(root):
            problems.append(f"{where}: {entry['path']!r} points outside the package directory")
            continue
        if not path.is_file():
            problems.append(f"{where}: {entry['path']!r} does not exist")
            continue
        data = path.read_bytes()
        if len(data) != entry.get("bytes"):
            problems.append(f"{where}: {entry['path']} is {len(data)} bytes, package says {entry.get('bytes')}")
        if hashlib.sha256(data).hexdigest() != entry.get("sha256"):
            problems.append(f"{where}: {entry['path']} sha256 differs from the package record")
    problems.extend(_keyframe_record_problems(package_dir, package))
    return _result("package_schema", problems, "package.json matches the schema and every file hash")


def _keyframe_record_problems(package_dir: Path, package: dict) -> list[str]:
    """package.scenes[].keyframes (what Legal and QA open) and timeline.json's
    keyframe records (frame number and time the pixel checks use) describe
    the same frames."""
    timeline = load_timeline(package_dir / package["files"]["timeline"]["path"])
    records = {kf["path"]: kf for kf in json.loads((package_dir / package["files"]["timeline"]["path"]).read_text())["keyframes"]}
    listed = set()
    problems = []
    for scene in package["scenes"]:
        for entry in scene["keyframes"]:
            listed.add(entry["path"])
            rec = records.get(entry["path"])
            if rec is None:
                problems.append(f"{entry['path']}: listed for {scene['id']} but has no timeline.json keyframe record")
                continue
            if rec["scene_id"] != scene["id"]:
                problems.append(f"{entry['path']}: timeline.json says scene {rec['scene_id']}, package says {scene['id']}")
            if abs(rec["t_s"] - rec["frame"] / FPS) > 1e-6:
                problems.append(f"{entry['path']}: t_s {rec['t_s']} is not frame {rec['frame']} / {FPS}")
            on_screen = timeline.scene_at(rec["t_s"])
            if on_screen is None or on_screen.scene_id != scene["id"]:
                problems.append(f"{entry['path']}: at {rec['t_s']:.2f}s the timeline shows {getattr(on_screen, 'scene_id', None)}, not {scene['id']}")
    for path in sorted(set(records) - listed):
        problems.append(f"{path}: timeline.json keyframe not listed in package scenes")
    return problems


def _template_dir(package_dir: Path, package: dict) -> Path:
    return package_dir / package.get("render", {}).get("template_dir", "template")


@register("template_valid")
def _check_template_valid(package_dir: Path, package: dict, channel_profile: dict) -> dict:
    template_dir = _template_dir(package_dir, package)
    try:
        validate_template(template_dir, package["channel_id"])
    except TemplateValidationError as e:
        lines = str(e).splitlines()
        return _result("template_valid", [" ".join(lines[:2])] + lines[2:], "")
    template_id = json.loads((template_dir / "template.json").read_text())["id"]
    if template_id != package["template"]:
        return _result("template_valid", [f"{template_dir.name}/ is template {template_id!r}, package says {package['template']!r}"], "")
    return _result("template_valid", [], f"{template_dir.name}/ passes studio template validate")


def _keyframes(package_dir: Path, package: dict) -> list[dict]:
    """The keyframes the package lists (scenes[].keyframes: what Legal and QA
    open), each as its timeline.json record {path, scene_id, kind, frame,
    t_s}. A listed keyframe without a record is an error (package_schema
    reports the details)."""
    records = {kf["path"]: kf for kf in json.loads((package_dir / package["files"]["timeline"]["path"]).read_text())["keyframes"]}
    out = []
    for scene in package["scenes"]:
        for entry in scene["keyframes"]:
            if entry["path"] not in records:
                raise ValueError(f"{entry['path']} has no timeline.json keyframe record")
            out.append(records[entry["path"]])
    return out


def _logical(img: Image.Image) -> np.ndarray:
    """A 1080x1920 frame back at 270x480 (one pixel per 4x4 cell)."""
    return np.asarray(img.convert("RGB"))[::SCALE, ::SCALE]


@register("keyframes_grid_palette")
def _check_keyframes_grid_palette(package_dir: Path, package: dict, channel_profile: dict) -> dict:
    palette = channel_palette_set(package["channel_id"])
    paths = [kf["path"] for kf in _keyframes(package_dir, package)] + [package["files"]["cover"]["path"]]
    problems = []
    for rel in paths:
        img = Image.open(package_dir / rel)
        if img.size != (OUTPUT_W, OUTPUT_H):
            problems.append(f"{rel}: {img.size[0]}x{img.size[1]}, expected {OUTPUT_W}x{OUTPUT_H}")
            continue
        a = np.asarray(img.convert("RGB"))
        cells = a.reshape(LOGICAL_H, SCALE, LOGICAL_W, SCALE, 3)
        if not (cells == cells[:, :1, :, :1, :]).all():
            bad = int((~(cells == cells[:, :1, :, :1, :]).all(axis=(1, 3, 4))).sum())
            problems.append(f"{rel}: {bad} of {LOGICAL_W * LOGICAL_H} {SCALE}x{SCALE} cells are not one color")
        packed = np.unique(a.reshape(-1, 3).astype(np.uint32) @ np.array([1 << 16, 1 << 8, 1], dtype=np.uint32))
        off = sorted(f"#{int(c):06x}" for c in packed if f"#{int(c):06x}" not in palette)
        if off:
            problems.append(f"{rel}: {len(off)} color(s) outside the channel NES palette: {', '.join(off[:5])}")
    return _result("keyframes_grid_palette", problems, f"{len(paths)} images: 1080x1920, 4x4 grid, NES palette only")


class _Context:
    """What the pixel checks re-derive a frame's expected content from: the
    package's own script, timeline and template copy."""

    def __init__(self, package_dir: Path, package: dict):
        self.script = json.loads((package_dir / package["files"]["script"]["path"]).read_text())
        self.timeline = load_timeline(package_dir / package["files"]["timeline"]["path"])
        self.template = load_template(_template_dir(package_dir, package))
        self.assets = load_channel_assets(package["channel_id"])
        self.scenes = {s["id"]: s for s in self.script["scenes"]}

    def timing(self, scene_id: str):
        return next(s for s in self.timeline.scenes if s.scene_id == scene_id)

    def in_transition(self, scene_id: str, t: float) -> bool:
        """A scene shorter than its transition gets keyframes mid-blend, where
        plates and face legitimately mix two scenes: the pixel checks skip them."""
        index = next(i for i, s in enumerate(self.timeline.scenes) if s.scene_id == scene_id)
        return t - self.timeline.scenes[index].start_s < scene_transition_duration(self.template, index)

    def real_person_in_frame(self, scene_id: str) -> bool:
        """format.md's rule, stated independently of the engine: a real person
        in the scene, or -- for an anchor shot -- in the next `scene` shot,
        which the screen behind her shows."""
        order = self.script["scenes"]
        i = next(k for k, s in enumerate(order) if s["id"] == scene_id)
        if order[i].get("real_people"):
            return True
        if order[i]["shot"] == "anchor":
            screen = next((s for s in order[i + 1:] if s["shot"] == "scene"), None)
            return bool(screen and screen.get("real_people"))
        return False


_AI_PLATES = {"ai_voice", "ai_icon", "ai_illustration"}
_AI_PLATE_TEXT = {"ai_voice": "AI · AI voice", "ai_icon": "AI", "ai_illustration": "AI illustration"}  # format.md
_AI_VOICE_S = 3.0  # format.md: «AI · AI voice» for the first 3 s


def _plate_problem(frame: np.ndarray, ctx: "_Context", name: str) -> str | None:
    """The plate's rect must be exactly that plate with its format.md label:
    nothing else is drawn over the AI plates, and keyframes are lossless."""
    cfg = ctx.template.plate(name)
    r = cfg["rect"]
    canvas = Image.new("RGBA", (LOGICAL_W, LOGICAL_H), (0, 0, 0, 255))
    _draw_plate(canvas, cfg, [_AI_PLATE_TEXT[name]], ctx.assets.font(FONT_PX))
    want = np.asarray(canvas.convert("RGB"))[r["y"]:r["y"] + r["h"], r["x"]:r["x"] + r["w"]]
    got = frame[r["y"]:r["y"] + r["h"], r["x"]:r["x"] + r["w"]]
    if got.shape != want.shape:
        return f"rect {r} is outside the {frame.shape[1]}x{frame.shape[0]} image"
    differ = int((got != want).any(axis=2).sum())
    if differ:
        return f"{differ} of {r['w'] * r['h']} pixels differ from the {_AI_PLATE_TEXT[name]!r} plate"
    return None


def _image(package_dir: Path, rel: str) -> np.ndarray:
    img = Image.open(package_dir / rel)
    if img.size != (OUTPUT_W, OUTPUT_H):
        raise ValueError(f"{rel} is {img.size[0]}x{img.size[1]}, not {OUTPUT_W}x{OUTPUT_H}")
    return _logical(img)


@register("ai_plates")
def _check_ai_plates(package_dir: Path, package: dict, channel_profile: dict) -> dict:
    ctx = _Context(package_dir, package)
    problems = []
    frames = [(kf["path"], kf["scene_id"], kf["t_s"], False) for kf in _keyframes(package_dir, package)]
    cover_frame = ctx.script["cover"]["frame"]
    if cover_frame != "custom":
        st = ctx.timing(cover_frame)
        frames.append((package["files"]["cover"]["path"], cover_frame, (st.start_s + st.end_s) / 2, True))
    checked = 0
    for rel, scene_id, t, is_cover in frames:
        if not is_cover and ctx.in_transition(scene_id, t):
            continue
        checked += 1
        st = ctx.timing(scene_id)
        if is_cover:
            shown = cover_plates(ctx.script, st, ctx.template, t)
            required = {"ai_illustration"} if ctx.real_person_in_frame(scene_id) else set()
        else:
            shown = active_plates(ctx.script, st, ctx.template, t) & _AI_PLATES
            required = {"ai_voice" if t < _AI_VOICE_S else "ai_icon"}
            if ctx.real_person_in_frame(scene_id):
                required.add("ai_illustration")
        for name in sorted(required - shown):
            problems.append(f"{rel} ({scene_id}, {t:.2f}s): the engine does not put {name} on this frame")
        frame = _image(package_dir, rel)
        for name in sorted(required | shown):
            why = _plate_problem(frame, ctx, name)
            if why:
                problems.append(f"{rel} ({scene_id}, {t:.2f}s): {name} plate missing: {why}")
    return _result("ai_plates", problems, f"AI plates in place on {checked} images")


@register("face_clear")
def _check_face_clear(package_dir: Path, package: dict, channel_profile: dict) -> dict:
    """On anchor keyframes (and an anchor cover) the anchor's face is exactly
    her sprite for that moment: nothing drawn over it."""
    ctx = _Context(package_dir, package)
    anchor = ctx.assets.anchor
    cfg = ctx.template.data["studio"]["anchor"]
    face = anchor.face_rect_in_frame(cfg)
    pose = "serious" if ctx.script.get("serious_mode") else "normal"
    frames = [(kf["path"], kf["scene_id"], kf["t_s"]) for kf in _keyframes(package_dir, package)]
    cover_frame = ctx.script["cover"]["frame"]
    if cover_frame != "custom":
        st = ctx.timing(cover_frame)
        frames.append((package["files"]["cover"]["path"], cover_frame, (st.start_s + st.end_s) / 2))
    problems, checked = [], 0
    for rel, scene_id, t in frames:
        if ctx.scenes[scene_id]["shot"] != "anchor" or (rel != package["files"]["cover"]["path"] and ctx.in_transition(scene_id, t)):
            continue
        checked += 1
        st = ctx.timing(scene_id)
        eyes = "closed" if anchor.is_blinking(t) else "open"
        mouth = anchor.mouth_level_for_volume(st.volume_at(t))
        sprite = _compose_anchor_sprite(ctx.assets, pose, eyes, mouth)
        sprite = np.asarray(sprite.resize((sprite.width * cfg["scale"], sprite.height * cfg["scale"]), Image.NEAREST))
        sx, sy = face["x"] - cfg["x"], face["y"] - cfg["y"]
        want = sprite[sy:sy + face["h"], sx:sx + face["w"]]
        got = _image(package_dir, rel)[face["y"]:face["y"] + face["h"], face["x"]:face["x"] + face["w"]]
        opaque = want[:, :, 3] == 255
        differ = int((got[opaque] != want[opaque][:, :3]).any(axis=1).sum())
        if differ:
            problems.append(f"{rel} ({scene_id}, {t:.2f}s): {differ} of {int(opaque.sum())} face pixels differ from the anchor sprite")
    return _result("face_clear", problems, f"anchor face clear on {checked} image(s)")


@register("video_matches_keyframes")
def _check_video_matches_keyframes(package_dir: Path, package: dict, channel_profile: dict) -> dict:
    """Each keyframe PNG against the video frame with the same number, cell by
    4x4 cell: the keyframes Legal and QA look at are the frames that ship.
    A per-cell limit catches a missing plate that a whole-frame mean hides."""
    keyframes = _keyframes(package_dir, package)
    numbers = sorted({kf["frame"] for kf in keyframes})
    select = "+".join(f"eq(n\\,{n})" for n in numbers)
    cmd = ["ffmpeg", "-v", "error", "-i", str(package_dir / package["files"]["video"]["path"]),
           "-vf", f"select={select}", "-fps_mode", "passthrough", "-f", "rawvideo", "-pix_fmt", "rgb24", "-"]
    raw = subprocess.run(cmd, capture_output=True, check=True).stdout
    size = OUTPUT_W * OUTPUT_H * 3
    if len(raw) != size * len(numbers):
        return _result("video_matches_keyframes",
                       [f"video.mp4 gave {len(raw) // size} of the {len(numbers)} keyframe frame numbers"], "")
    video = {n: np.frombuffer(raw[i * size:(i + 1) * size], np.uint8).reshape(OUTPUT_H, OUTPUT_W, 3)
             for i, n in enumerate(numbers)}
    limit = max_keyframe_cell_diff()
    problems, worst = [], 0.0
    for kf in keyframes:
        png = np.asarray(Image.open(package_dir / kf["path"]).convert("RGB")).astype(np.int16)
        if png.shape != (OUTPUT_H, OUTPUT_W, 3):
            problems.append(f"{kf['path']}: {png.shape[1]}x{png.shape[0]}, not {OUTPUT_W}x{OUTPUT_H}")
            continue
        cells = np.abs(png - video[kf["frame"]]).reshape(LOGICAL_H, SCALE, LOGICAL_W, SCALE, 3).mean(axis=(1, 3, 4))
        worst = max(worst, float(cells.max()))
        over = int((cells > limit).sum())
        if over:
            problems.append(f"{kf['path']}: {over} 4x4 cell(s) differ from video frame {kf['frame']} by up to "
                            f"{cells.max():.0f} (limit {limit})")
    return _result("video_matches_keyframes", problems,
                   f"{len(keyframes)} keyframes match their video frames (worst cell diff {worst:.1f}, limit {limit})",
                   value=round(worst, 1), expected=limit)


@register("no_black_frames")
def _check_no_black_frames(package_dir: Path, package: dict, channel_profile: dict) -> dict:
    """No black frame anywhere in the video (a frame outside every scene
    renders black: the pauses between scenes once did)."""
    # pic_th=1.0: only an entirely black picture counts -- a dark night-report
    # scene still carries the AI plate's light lettering.
    cmd = ["ffmpeg", "-nostats", "-i", str(package_dir / package["files"]["video"]["path"]),
           "-vf", f"blackdetect=d={1 / FPS:.3f}:pix_th=0.10:pic_th=1.0", "-an", "-f", "null", "-"]
    proc = subprocess.run(cmd, capture_output=True, text=True)
    if proc.returncode != 0:
        return _result("no_black_frames", [f"ffmpeg could not read video.mp4: {proc.stderr.strip()[-200:]}"], "")
    err = proc.stderr
    spans = re.findall(r"black_start:\s*([\d.]+)\s+black_end:\s*([\d.]+)", err)
    problems = [f"black from {float(a):.2f}s to {float(b):.2f}s" for a, b in spans]
    return _result("no_black_frames", problems, "no black frames")


@register("disclosures")
def _check_disclosures(package_dir: Path, package: dict, channel_profile: dict) -> dict:
    """countries/us/policy.md «Раскрытие»: AI voice and visuals disclosed, and
    the AI-content flag set for every enabled platform of the channel."""
    d = package.get("disclosures", {})
    problems = [f"disclosures.{k} is not true" for k in ("ai_voice", "ai_visuals") if d.get(k) is not True]
    enabled = [p for p, cfg in channel_profile.get("platforms", {}).items() if cfg.get("enabled")]
    for platform in enabled:
        if d.get("platforms", {}).get(platform, {}).get("ai_label") is not True:
            problems.append(f"disclosures.platforms.{platform}.ai_label is not true")
    return _result("disclosures", problems, f"AI voice and visuals disclosed; ai_label on {', '.join(enabled)}")


def _not_implemented(check_id: str):
    @register(check_id)
    def _stub(package_dir: Path, package: dict, channel_profile: dict) -> dict:
        return {"id": check_id, "ok": True, "note": "not implemented (E2a-3)"}

    return _stub


# Audio checks land in E2a-3 (README «studio validate»): replace these stubs
# by registering real functions under the same ids.
for _id in ("loudness", "true_peak", "silence", "music_under_voice", "video_no_music"):
    _not_implemented(_id)


def run_checks(package_dir: Path, package: dict, channel_profile: dict) -> dict:
    checks = []
    for check_id, fn in CHECKS.items():
        try:
            checks.append(fn(package_dir, package, channel_profile))
        except Exception as e:  # noqa: BLE001 - a crashed check must fail loud, not vanish from the report
            checks.append({"id": check_id, "ok": False, "note": f"check crashed: {e}"})
    passed = all(c["ok"] for c in checks)
    return {"passed": passed, "checks": checks}
