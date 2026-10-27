"""Registry of `studio validate` checks, keyed by a fixed `id` so this
task's four checks and E2a-2b/E2a-3's later additions all show up the same
way in `package.json`'s `validate.checks` (video_package.schema.json)."""
from __future__ import annotations

import json
from pathlib import Path
from typing import Callable

from .config import max_file_size_bytes
from .constants import FPS
from .srt import parse_srt
from .timeline import load_timeline
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


def run_checks(package_dir: Path, package: dict, channel_profile: dict) -> dict:
    checks = []
    for check_id, fn in CHECKS.items():
        try:
            checks.append(fn(package_dir, package, channel_profile))
        except Exception as e:  # noqa: BLE001 - a crashed check must fail loud, not vanish from the report
            checks.append({"id": check_id, "ok": False, "note": f"check crashed: {e}"})
    passed = all(c["ok"] for c in checks)
    return {"passed": passed, "checks": checks}
