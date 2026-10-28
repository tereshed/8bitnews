"""Voice concatenation, the music/sound-effect mix and loudness for `studio
build` (studio/README.md, «Звук»).

The mix happens in numpy at 48 kHz stereo; ffmpeg decodes, limits and
measures. Ducking follows the word timings instead of a sidechain detector:
the timeline already knows when the voice speaks, so the music dips a little
*before* each phrase instead of reacting after its first syllable.
Loudness: one gain to the channel target, then a true-peak limiter (run at
4x the sample rate, so its sample-peak ceiling holds between samples too),
measured again and corrected until the integrated loudness lands on target.
"""
from __future__ import annotations

import re
import subprocess
import wave
from dataclasses import dataclass
from pathlib import Path

import numpy as np

from . import constants


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


def prepend_silence(wav_path: Path, seconds: float) -> float:
    """Puts `seconds` of silence before a 16-bit wav, in place; returns the
    exact length added (whole samples)."""
    with wave.open(str(wav_path), "rb") as wf:
        params, frames = wf.getparams(), wf.readframes(wf.getnframes())
    n = int(round(seconds * params.framerate))
    with wave.open(str(wav_path), "wb") as out:
        out.setparams(params)
        out.writeframes(b"\x00" * (n * params.sampwidth * params.nchannels) + frames)
    return n / params.framerate


# --- decoding, writing, measuring -------------------------------------------------

def decode(path: Path, sample_rate: int, channels: int = 2, loop_to_s: float | None = None,
           stream: str = "0:a:0") -> np.ndarray:
    """Any audio (or the audio of a video) -> float32 array (n, channels).
    `loop_to_s` loops the input until it lasts that long, then cuts it."""
    cmd = ["ffmpeg", "-v", "error"]
    if loop_to_s is not None:
        cmd += ["-stream_loop", "-1"]
    cmd += ["-i", str(path), "-map", stream]
    if loop_to_s is not None:
        cmd += ["-t", f"{loop_to_s:.6f}"]
    cmd += ["-f", "f32le", "-ac", str(channels), "-ar", str(sample_rate), "-"]
    raw = subprocess.run(cmd, capture_output=True, check=True).stdout
    return np.frombuffer(raw, dtype=np.float32).reshape(-1, channels).copy()


def write_audio(path: Path, samples: np.ndarray, sample_rate: int, codec: str = "pcm_f32le") -> None:
    """float32 (n, channels) -> a file (wav by default, `flac` for stems)."""
    samples = np.ascontiguousarray(samples, dtype=np.float32)
    channels = samples.shape[1] if samples.ndim == 2 else 1
    path.parent.mkdir(parents=True, exist_ok=True)
    cmd = ["ffmpeg", "-v", "error", "-y", "-f", "f32le", "-ar", str(sample_rate), "-ac", str(channels),
           "-i", "-", "-c:a", codec, str(path)]
    subprocess.run(cmd, input=samples.tobytes(), capture_output=True, check=True)


_LUFS_RE = re.compile(r"I:\s*(-?\d+(?:\.\d+)?|-inf)\s*LUFS")
_PEAK_RE = re.compile(r"True peak:\s*Peak:\s*(-?\d+(?:\.\d+)?|-inf)\s*dBFS", re.S)


@dataclass(frozen=True)
class Loudness:
    lufs: float
    true_peak_dbtp: float


def _parse_ebur128(stderr: str, what: str) -> Loudness:
    summary = stderr[stderr.rfind("Summary:"):]
    lufs, peak = _LUFS_RE.findall(summary), _PEAK_RE.findall(summary)
    if not lufs or not peak:
        raise RuntimeError(f"could not parse ebur128 loudness for {what}")
    # Silence measures -inf; keep it a finite number package.json can hold.
    return Loudness(lufs=max(float(lufs[-1]), -120.0), true_peak_dbtp=max(float(peak[-1]), -120.0))


def measure(path: Path, stream: str = "0:a:0") -> Loudness:
    """Integrated loudness (EBU R128) and true peak of a file's audio."""
    cmd = ["ffmpeg", "-nostats", "-i", str(path), "-map", stream, "-filter:a", "ebur128=peak=true",
           "-f", "null", "-"]
    result = subprocess.run(cmd, capture_output=True, text=True)
    return _parse_ebur128(result.stderr, str(path))


def measure_array(samples: np.ndarray, sample_rate: int, dualmono: bool = False) -> Loudness:
    samples = np.ascontiguousarray(samples, dtype=np.float32)
    channels = samples.shape[1] if samples.ndim == 2 else 1
    flt = "ebur128=peak=true" + (":dualmono=true" if dualmono else "")
    cmd = ["ffmpeg", "-nostats", "-f", "f32le", "-ar", str(sample_rate), "-ac", str(channels), "-i", "-",
           "-filter:a", flt, "-f", "null", "-"]
    result = subprocess.run(cmd, input=samples.tobytes(), capture_output=True)
    return _parse_ebur128(result.stderr.decode(errors="replace"), "mix")


def measure_loudness_lufs(media_path: Path) -> float:
    return measure(media_path).lufs


# --- the mix ----------------------------------------------------------------------

@dataclass(frozen=True)
class SfxEvent:
    """A sound effect placed on the timeline (a transition sound)."""

    t_s: float
    path: Path
    duration_s: float = 0.0


@dataclass
class MixResult:
    master: np.ndarray
    """The mastered mix, float32 (n, 2) -- what goes into the video."""
    music: np.ndarray | None
    """The music bed as mixed in (looped, faded, ducked), before the master
    gain: the template `studio validate` looks for in a soundtrack."""
    gain_db: float
    loudness: Loudness


def _db(x: float) -> float:
    return 10 ** (x / 20)


def speech_intervals(words: list[tuple[float, float]], hold_gap_s: float) -> list[tuple[float, float]]:
    """Word spans merged across gaps shorter than `hold_gap_s`: the music
    stays down through a short breath instead of pumping."""
    out: list[list[float]] = []
    for s, e in sorted(words):
        if e < s:
            continue
        if out and s - out[-1][1] < hold_gap_s:
            out[-1][1] = max(out[-1][1], e)
        else:
            out.append([s, e])
    return [(s, e) for s, e in out]


def duck_envelope(n: int, sample_rate: int, intervals: list[tuple[float, float]], duck_db: float,
                  attack_s: float, release_s: float) -> np.ndarray:
    """Per-sample music gain in dB: 0 in the clear, `duck_db` while the voice
    speaks, linear ramps of `attack_s` before and `release_s` after."""
    env = np.zeros(n, dtype=np.float32)
    t = np.arange(n, dtype=np.float32) / sample_rate
    for s, e in intervals:
        lo, hi = max(0, int((s - attack_s) * sample_rate)), min(n, int((e + release_s) * sample_rate) + 1)
        if lo >= hi:
            continue
        seg = t[lo:hi]
        ramp_in = np.clip((seg - (s - attack_s)) / max(attack_s, 1e-6), 0, 1)
        ramp_out = np.clip(((e + release_s) - seg) / max(release_s, 1e-6), 0, 1)
        env[lo:hi] = np.minimum(env[lo:hi], duck_db * np.minimum(ramp_in, ramp_out))
    return env


def limit(samples: np.ndarray, sample_rate: int, ceiling_db: float) -> np.ndarray:
    """True-peak limiter: ffmpeg alimiter at 4x the rate (its sample-peak
    ceiling then also holds between the original samples), latency
    compensated so nothing shifts against the picture."""
    samples = np.ascontiguousarray(samples, dtype=np.float32)
    up = sample_rate * 4
    flt = (f"aresample={up},alimiter=limit={_db(ceiling_db):.6f}:attack=2:release=80:level=false:latency=true,"
           f"aresample={sample_rate}")
    cmd = ["ffmpeg", "-v", "error", "-f", "f32le", "-ar", str(sample_rate), "-ac", str(samples.shape[1]), "-i", "-",
           "-af", flt, "-f", "f32le", "-"]
    raw = subprocess.run(cmd, input=samples.tobytes(), capture_output=True, check=True).stdout
    out = np.frombuffer(raw, dtype=np.float32).reshape(-1, samples.shape[1])
    if len(out) >= len(samples):
        return out[: len(samples)].copy()
    return np.concatenate([out, np.zeros((len(samples) - len(out), samples.shape[1]), np.float32)])


def master(samples: np.ndarray, sample_rate: int, target_lufs: float, ceiling_db: float,
           tolerance_lu: float = 0.2, max_rounds: int = 5) -> tuple[np.ndarray, float, Loudness]:
    """One gain to `target_lufs`, then the limiter; the limiter takes a
    little loudness off loud peaks, so the gain is raised by what is missing
    and the pair re-run until the result is within `tolerance_lu`.
    (ffmpeg loudnorm in linear mode would refuse the gain a peaky voice
    needs and quietly fall back to dynamic mode, under target.)"""
    gain = target_lufs - measure_array(samples, sample_rate).lufs
    out, got = samples, None
    for _ in range(max_rounds):
        out = limit(samples * _db(gain), sample_rate, ceiling_db)
        got = measure_array(out, sample_rate)
        if abs(got.lufs - target_lufs) <= tolerance_lu:
            break
        gain += target_lufs - got.lufs
    return out, gain, got


def mix(
    voice_path: Path,
    duration_s: float,
    words: list[tuple[float, float]],
    settings: dict,
    music_path: Path | None = None,
    sfx: list[SfxEvent] | None = None,
    ceiling_db: float | None = None,
) -> MixResult:
    """Voice + (looped, faded, ducked) music + sound effects, mastered."""
    sr = int(settings["sample_rate"])
    n = int(round(duration_s * sr))
    voice = decode(voice_path, sr, channels=1)[:, 0]
    voice = np.pad(voice, (0, max(0, n - len(voice))))[:n]
    voice_level = measure_array(voice, sr, dualmono=True).lufs

    music = None
    if music_path is not None:
        music = decode(music_path, sr, channels=2, loop_to_s=duration_s)
        music = np.pad(music, ((0, max(0, n - len(music))), (0, 0)))[:n]
        music_level = measure_array(music, sr).lufs
        music *= _db(voice_level + settings["music_bed_db"] - music_level)
        env_db = duck_envelope(n, sr, speech_intervals(words, settings["duck_hold_gap_s"]), settings["duck_db"],
                               settings["duck_attack_s"], settings["duck_release_s"])
        fade = np.ones(n, dtype=np.float32)
        fi, fo = min(int(settings["fade_in_s"] * sr), n), min(int(settings["fade_out_s"] * sr), n)
        fade[:fi] = np.linspace(0, 1, fi, dtype=np.float32)
        fade[n - fo:] = np.minimum(fade[n - fo:], np.linspace(1, 0, fo, dtype=np.float32))
        music *= (10 ** (env_db / 20) * fade)[:, None]

    effects = np.zeros((n, 2), dtype=np.float32)
    voice_peak = float(np.abs(voice).max()) or 1.0
    for ev in sfx or []:
        clip = decode(ev.path, sr, channels=2)
        clip *= voice_peak * _db(settings["sfx_db"]) / (float(np.abs(clip).max()) or 1.0)
        at = int(ev.t_s * sr)
        if at >= n:
            continue
        end = min(n, at + len(clip))
        effects[at:end] += clip[: end - at]

    bus = voice[:, None] + effects + (music if music is not None else 0)
    ceiling = settings["true_peak_ceiling_dbtp"] if ceiling_db is None else ceiling_db
    out, gain, got = master(bus, sr, constants.LOUDNESS_LUFS, ceiling)
    return MixResult(master=out, music=music, gain_db=gain, loudness=got)


# --- checks' helpers --------------------------------------------------------------

def fit(shipped: np.ndarray, stems: list[np.ndarray], mask: np.ndarray) -> tuple[list[float], float]:
    """Least-squares gains of mono `stems` in a mono soundtrack over `mask`,
    and how loud what they leave unexplained is next to the first stem's
    fitted part (dB of power)."""
    n = min(len(shipped), *(len(s) for s in stems), len(mask))
    m = mask[:n]
    a = np.stack([s[:n][m] for s in stems], axis=1).astype(np.float64)
    y = shipped[:n][m].astype(np.float64)
    gains, *_ = np.linalg.lstsq(a, y, rcond=None)
    rest = y - a @ gains
    first = a[:, 0] * gains[0]
    ratio = float(np.sum(rest ** 2)) / max(float(np.sum(first ** 2)), 1e-20)
    return [float(g) for g in gains], 10 * np.log10(max(ratio, 1e-20))


def voice_over_rest_db(shipped: np.ndarray, voice: np.ndarray, sample_rate: int,
                       intervals: list[tuple[float, float]]) -> float | None:
    """How far (dB, loudness) the voice sits above everything else in a
    soundtrack (n, channels) over `intervals`: `voice` (mono) is fitted to
    each channel by least squares there and subtracted, so music panned or
    out of phase between the channels stays in the remainder. None if the
    voice isn't in it."""
    n = min(len(shipped), len(voice))
    mask = masked(np.ones(n, dtype=np.float32), sample_rate, intervals) > 0
    v = voice[:n][mask].astype(np.float64)
    power = float(np.dot(v, v))
    fitted = np.zeros((n, shipped.shape[1]), dtype=np.float32)
    rest = np.zeros_like(fitted)
    for c in range(shipped.shape[1]):
        y = shipped[:n, c][mask].astype(np.float64)
        gain = float(np.dot(y, v)) / power if power > 0 else 0.0
        if gain <= 0:
            return None
        fitted[mask, c], rest[mask, c] = gain * v, y - gain * v
    return measure_array(fitted, sample_rate).lufs - measure_array(rest, sample_rate).lufs


def masked(samples: np.ndarray, sample_rate: int, intervals: list[tuple[float, float]]) -> np.ndarray:
    """Zero outside `intervals` (ebur128's absolute gate then skips those)."""
    mask = np.zeros(len(samples), dtype=bool)
    for s, e in intervals:
        mask[max(0, int(s * sample_rate)):min(len(samples), int(e * sample_rate))] = True
    return samples * mask[:, None] if samples.ndim == 2 else samples * mask


def silences(path: Path, threshold_db: float, min_s: float, stream: str = "0:a:0") -> list[tuple[float, float]]:
    """ffmpeg silencedetect: spans quieter than `threshold_db` for at least
    `min_s` (an unterminated span runs to the end of the file)."""
    cmd = ["ffmpeg", "-nostats", "-i", str(path), "-map", stream,
           "-af", f"silencedetect=noise={threshold_db}dB:d={min_s}", "-f", "null", "-"]
    err = subprocess.run(cmd, capture_output=True, text=True).stderr
    starts = [float(x) for x in re.findall(r"silence_start:\s*(-?[\d.]+)", err)]
    ends = [float(x) for x in re.findall(r"silence_end:\s*(-?[\d.]+)", err)]
    out = []
    for i, s in enumerate(starts):
        out.append((max(0.0, s), ends[i] if i < len(ends) else float("inf")))
    return out
