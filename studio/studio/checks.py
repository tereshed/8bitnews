"""Registry of `studio validate` checks, keyed by a fixed `id` so this
task's four checks and E2a-2b/E2a-3's later additions all show up the same
way in `package.json`'s `validate.checks` (video_package.schema.json)."""
from __future__ import annotations

import json
import subprocess
from pathlib import Path
from typing import Callable

from .config import max_file_size_bytes
from .srt import parse_srt
from .timeline import load_timeline

CHECKS: dict[str, Callable[[Path, dict, dict], dict]] = {}


def register(check_id: str):
    def deco(fn: Callable[[Path, dict, dict], dict]):
        CHECKS[check_id] = fn
        return fn

    return deco


def _ffprobe(path: Path) -> dict:
    cmd = ["ffprobe", "-v", "error", "-print_format", "json", "-show_format", "-show_streams", str(path)]
    result = subprocess.run(cmd, capture_output=True, text=True, check=True)
    return json.loads(result.stdout)


def _parse_rate(rate: str) -> float:
    num, _, den = rate.partition("/")
    return float(num) / float(den) if den else float(num)


@register("format")
def _check_format(package_dir: Path, package: dict, channel_profile: dict) -> dict:
    video_path = package_dir / package["files"]["video"]["path"]
    probe = _ffprobe(video_path)
    v_stream = next((s for s in probe["streams"] if s["codec_type"] == "video"), None)
    a_stream = next((s for s in probe["streams"] if s["codec_type"] == "audio"), None)
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
    duration = package["duration_s"]
    bounds = channel_profile["format"]["duration_s"]
    lo, hi = bounds["min"], bounds["max"]
    ok = lo <= duration <= hi
    note = f"{duration:.2f}s within [{lo},{hi}]s" if ok else f"{duration:.2f}s outside channel range [{lo},{hi}]s"
    return {"id": "duration", "ok": ok, "value": duration, "expected": {"min": lo, "max": hi}, "note": note}


@register("file_size")
def _check_file_size(package_dir: Path, package: dict, channel_profile: dict) -> dict:
    video_path = package_dir / package["files"]["video"]["path"]
    size = video_path.stat().st_size
    limit = max_file_size_bytes()
    ok = size <= limit
    note = f"{size} bytes <= {limit} byte limit" if ok else f"{size} bytes exceeds {limit} byte limit"
    return {"id": "file_size", "ok": ok, "value": size, "expected": limit, "note": note}


@register("subtitles_cover_words")
def _check_subtitles_cover_words(package_dir: Path, package: dict, channel_profile: dict) -> dict:
    srt_path = package_dir / package["files"]["subtitles"]["path"]
    timeline_path = package_dir / package["files"]["timeline"]["path"]
    cues = parse_srt(srt_path)
    timeline = load_timeline(timeline_path)
    tol = 1.0 / 30.0
    uncovered = [
        word.text
        for scene in timeline.scenes
        for word in scene.words
        if not any(word.start_s >= c.start_s - tol and word.end_s <= c.end_s + tol for c in cues)
    ]
    ok = not uncovered
    note = (
        "every word falls within a subtitle cue (1 frame tolerance)"
        if ok
        else f"{len(uncovered)} word(s) not covered by any subtitle cue: {uncovered[:5]}"
    )
    return {"id": "subtitles_cover_words", "ok": ok, "value": len(uncovered), "expected": 0, "note": note}


def run_checks(package_dir: Path, package: dict, channel_profile: dict) -> dict:
    checks = []
    for check_id, fn in CHECKS.items():
        try:
            checks.append(fn(package_dir, package, channel_profile))
        except Exception as e:  # noqa: BLE001 - a crashed check must fail loud, not vanish from the report
            checks.append({"id": check_id, "ok": False, "note": f"check crashed: {e}"})
    passed = all(c["ok"] for c in checks)
    return {"passed": passed, "checks": checks}
