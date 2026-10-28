"""Encodes a sequence of logical 270x480 frames into the channel's final
1080x1920 H.264 MP4 (docs/architecture.md §12: '×4 nearest-neighbour...
(-sws_flags neighbor)'), and builds a PNG contact sheet for inspection
without needing to open the video."""
from __future__ import annotations

import json
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


def encode(frames_dir: Path, audio_path: Path, out_path: Path, fps: int = FPS) -> None:
    """`studio build`'s encode step: frame_%06d.png (logical 270x480) plus a
    real audio track (voice, or voice+music once E2a-3 lands) into the
    channel's final H.264/AAC 1080x1920 MP4 -- same ×4 nearest-neighbour
    scaling as `encode_mp4`, but with real audio instead of a test tone."""
    out_path.parent.mkdir(parents=True, exist_ok=True)
    cmd = [
        "ffmpeg",
        "-y",
        "-framerate",
        str(fps),
        "-i",
        str(frames_dir / "frame_%06d.png"),
        "-i",
        str(audio_path),
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


def encode_video_only(frames_dir: Path, out_path: Path, fps: int = FPS) -> None:
    """frame_%06d.png (logical 270x480) -> the H.264 1080x1920 picture with
    no audio; `mux` then adds each soundtrack to this one picture, so
    video.mp4 and video_no_music.mp4 carry the very same video stream."""
    out_path.parent.mkdir(parents=True, exist_ok=True)
    cmd = [
        "ffmpeg", "-y", "-framerate", str(fps), "-i", str(frames_dir / "frame_%06d.png"),
        "-vf", f"scale={OUTPUT_W}:{OUTPUT_H}:flags=neighbor",
        "-c:v", "libx264", "-pix_fmt", "yuv420p", "-an", str(out_path),
    ]
    subprocess.run(cmd, check=True, capture_output=True, text=True)


def mux(video_path: Path, audio_path: Path, out_path: Path, bitrate: str = "192k", sample_rate: int = 48000) -> None:
    """Copies the picture of `video_path` and encodes `audio_path` to AAC."""
    cmd = [
        "ffmpeg", "-y", "-i", str(video_path), "-i", str(audio_path),
        "-map", "0:v:0", "-map", "1:a:0", "-c:v", "copy",
        "-c:a", "aac", "-b:a", bitrate, "-ar", str(sample_rate), "-shortest", str(out_path),
    ]
    subprocess.run(cmd, check=True, capture_output=True, text=True)


def video_stream_md5(path: Path) -> str:
    """MD5 of the video packets (not the container): equal for two files
    that share one encoded picture."""
    cmd = ["ffmpeg", "-v", "error", "-i", str(path), "-map", "0:v:0", "-c", "copy", "-f", "md5", "-"]
    out = subprocess.run(cmd, capture_output=True, text=True, check=True).stdout.strip()
    return out.removeprefix("MD5=")


def probe(path: Path) -> dict:
    """ffprobe's format + streams description of a media file."""
    cmd = ["ffprobe", "-v", "error", "-print_format", "json", "-show_format", "-show_streams", str(path)]
    result = subprocess.run(cmd, capture_output=True, text=True, check=True)
    return json.loads(result.stdout)


def probe_duration_s(path: Path) -> float:
    """Measured length of a rendered file (ffprobe `format.duration`): what
    `studio build` writes to `package.duration_s` and what the `duration`
    check compares against the channel range."""
    return float(probe(path)["format"]["duration"])
