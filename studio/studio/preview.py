"""`studio template preview <dir> --out <dir>`: renders the built-in 3-scene
demo script through a template package without any TTS (a test tone
stands in for the voice) and writes an MP4 plus a PNG contact sheet."""
from __future__ import annotations

from pathlib import Path

from .builtin_script import BUILTIN_SCRIPT, builtin_timeline
from .channel_assets import load_channel_assets
from .constants import FPS
from .engine import render_frame
from .template_pkg import load_template
from .video import build_contact_sheet, encode_mp4, write_frames_png

_CONTACT_SHEET_FRACTIONS = (0.0, 0.2, 0.45, 0.7, 0.95)


def render_preview(template_dir: Path, out_dir: Path) -> dict:
    template_dir = Path(template_dir)
    out_dir = Path(out_dir)
    template = load_template(template_dir)
    assets = load_channel_assets(BUILTIN_SCRIPT["channel_id"])
    timeline = builtin_timeline()

    n_frames = max(int(round(timeline.duration_s * FPS)), 1)
    frames = [render_frame(BUILTIN_SCRIPT, timeline, template, assets, i / FPS) for i in range(n_frames)]

    frames_dir = out_dir / "frames"
    write_frames_png(frames, frames_dir)

    mp4_path = out_dir / f"{template.id}_preview.mp4"
    encode_mp4(frames_dir, mp4_path, duration_s=timeline.duration_s)

    sheet_indices = sorted({min(int(f * n_frames), n_frames - 1) for f in _CONTACT_SHEET_FRACTIONS})
    sheet_frames = [frames[i] for i in sheet_indices]
    contact_sheet_path = out_dir / f"{template.id}_contact_sheet.png"
    build_contact_sheet(sheet_frames, contact_sheet_path)

    return {
        "mp4": mp4_path,
        "contact_sheet": contact_sheet_path,
        "frames_dir": frames_dir,
        "duration_s": timeline.duration_s,
        "n_frames": n_frames,
    }
