"""Voice concatenation, the (currently trivial) music mix, and loudness
measurement for `studio build`."""
from __future__ import annotations

import re
import shutil
import subprocess
import wave
from pathlib import Path


def concat_wavs_with_silence(wav_paths: list[Path], pause_s: float, out_path: Path) -> int:
    """Concatenates per-scene wav files in order, inserting `pause_s` of
    silence *between* scenes only (none before the first or after the last),
    matching `timeline.timeline_from_voice`'s scene gaps exactly so the
    voice track and the Timeline always agree on timing. Returns the sample
    rate used."""
    sample_rate = sampwidth = nchannels = None
    chunks: list[bytes] = []
    for i, path in enumerate(wav_paths):
        with wave.open(str(path), "rb") as wf:
            sr, sw, nc = wf.getframerate(), wf.getsampwidth(), wf.getnchannels()
            if sample_rate is None:
                sample_rate, sampwidth, nchannels = sr, sw, nc
            elif (sr, sw, nc) != (sample_rate, sampwidth, nchannels):
                raise ValueError(f"{path}: wav format {(sr, sw, nc)} != {(sample_rate, sampwidth, nchannels)}")
            frames = wf.readframes(wf.getnframes())
        if i > 0:
            silence_n = int(round(pause_s * sample_rate))
            chunks.append(b"\x00" * (silence_n * sampwidth * nchannels))
        chunks.append(frames)
    out_path.parent.mkdir(parents=True, exist_ok=True)
    with wave.open(str(out_path), "wb") as out:
        out.setnchannels(nchannels)
        out.setsampwidth(sampwidth)
        out.setframerate(sample_rate)
        out.writeframes(b"".join(chunks))
    return sample_rate


def mix_audio(voice_wav: Path, music_cfg: dict, out_path: Path) -> Path:
    """Returns the voice track unchanged (E2a-3 adds the music bed and
    ducking here); kept as its own pipeline step so build()'s voice -> mix
    -> frames -> encode order doesn't change when that lands."""
    out_path.parent.mkdir(parents=True, exist_ok=True)
    shutil.copyfile(voice_wav, out_path)
    return out_path


_LUFS_RE = re.compile(r"I:\s*(-?\d+(?:\.\d+)?)\s*LUFS")


def measure_loudness_lufs(media_path: Path) -> float:
    cmd = ["ffmpeg", "-nostats", "-i", str(media_path), "-filter:a", "ebur128", "-f", "null", "-"]
    result = subprocess.run(cmd, capture_output=True, text=True)
    matches = _LUFS_RE.findall(result.stderr)
    if not matches:
        raise RuntimeError(f"could not parse ebur128 integrated loudness from ffmpeg output for {media_path}")
    return float(matches[-1])
