"""Encodes a sequence of logical 270x480 frames into the channel's final
1080x1920 H.264 MP4 (docs/architecture.md §12: '×4 nearest-neighbour...
(-sws_flags neighbor)'), and builds a PNG contact sheet for inspection
without needing to open the video."""
from __future__ import annotations

import subprocess
from pathlib import Path

from PIL import Image

from .constants import FPS, LOGICAL_H, LOGICAL_W, OUTPUT_H, OUTPUT_W


def write_frames_png(frames: list[Image.Image], out_dir: Path) -> list[Path]:
    out_dir.mkdir(parents=True, exist_ok=True)
    paths = []
    for i, frame in enumerate(frames):
        assert frame.size == (LOGICAL_W, LOGICAL_H), frame.size
        path = out_dir / f"frame_{i:06d}.png"
        frame.save(path)
        paths.append(path)
    return paths


def upscale_nearest(frame: Image.Image) -> Image.Image:
    """Exact ×4 pixel replication; mathematically identical to ffmpeg's
    scale=...:flags=neighbor for an integer scale factor, so the contact
    sheet (built in Python) and the MP4 (built by ffmpeg below) always
    agree pixel-for-pixel."""
    return frame.resize((OUTPUT_W, OUTPUT_H), Image.NEAREST)


def build_contact_sheet(frames: list[Image.Image], out_path: Path) -> None:
    sheet = Image.new("RGB", (OUTPUT_W, OUTPUT_H * len(frames)))
    for i, frame in enumerate(frames):
        sheet.paste(upscale_nearest(frame), (0, i * OUTPUT_H))
    out_path.parent.mkdir(parents=True, exist_ok=True)
    sheet.save(out_path)


def encode_mp4(frames_dir: Path, out_path: Path, duration_s: float, fps: int = FPS, tone_hz: float = 440.0) -> None:
    """ffmpeg assembles frame_%06d.png (logical 270x480) into an H.264 MP4 at
    1080x1920, nearest-neighbour scaled, with a test sine tone standing in
    for the voice track (no TTS in E2a-1)."""
    out_path.parent.mkdir(parents=True, exist_ok=True)
    cmd = [
        "ffmpeg",
        "-y",
        "-framerate",
        str(fps),
        "-i",
        str(frames_dir / "frame_%06d.png"),
        "-f",
        "lavfi",
        "-i",
        f"sine=frequency={tone_hz}:duration={duration_s}",
        "-vf",
        f"scale={OUTPUT_W}:{OUTPUT_H}:flags=neighbor",
        "-c:v",
        "libx264",
        "-pix_fmt",
        "yuv420p",
        "-c:a",
        "aac",
        "-shortest",
        str(out_path),
    ]
    subprocess.run(cmd, check=True, capture_output=True, text=True)
