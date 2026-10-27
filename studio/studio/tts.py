"""TTS adapter interface and registry for `studio build --tts`.

`synthesize(text, voice_id) -> TtsResult(wav_path, sample_rate, words)`:
`words` has one entry per whitespace token of `text` (the *synthesis* text,
already carrying any `script.pronunciations` substitutions -- that mapping
back to the original words is general code, not an adapter's job; see
`studio.pronunciation`), in order, each with `start_s`/`end_s`.

`stub` is a deterministic, quiet-but-measurable sine tone standing in for a
real voice: no network, no model, same output on every run. Adapter-specific
settings (stub's words-per-minute pace; a future Kokoro's speed) are
constructor arguments, not `synthesize()` parameters, so `ADAPTERS` can grow
a `kokoro` entry in E2a-3 without changing this interface or anything that
calls it.
"""
from __future__ import annotations

import os
import tempfile
import wave
from dataclasses import dataclass
from pathlib import Path

import numpy as np

from .constants import DEFAULT_WPM
from .timeline import Word

_SAMPLE_RATE = 24000
_TONE_HZ = 220.0
_AMPLITUDE = 0.25  # of int16 full scale: quiet, but never digital silence


@dataclass(frozen=True)
class TtsResult:
    wav_path: Path
    sample_rate: int
    words: tuple[Word, ...]


def _write_tone_wav(path: Path, duration_s: float, sample_rate: int = _SAMPLE_RATE) -> None:
    n = max(int(round(duration_s * sample_rate)), 1)
    t = np.arange(n) / sample_rate
    samples = (_AMPLITUDE * np.sin(2 * np.pi * _TONE_HZ * t) * 32767).astype(np.int16)
    with wave.open(str(path), "wb") as f:
        f.setnchannels(1)
        f.setsampwidth(2)
        f.setframerate(sample_rate)
        f.writeframes(samples.tobytes())


class StubTtsAdapter:
    """Each word lasts `60/wpm` seconds with no gap to the next word within
    a scene (format.md's own word budget is defined the same way); `wpm`
    defaults to the channel pace (165, or 144 in serious mode -- the same
    constants `timeline.uniform_timeline` uses) but the caller passes the
    right one in explicitly."""

    def __init__(self, wpm: float = DEFAULT_WPM):
        self.wpm = wpm

    def synthesize(self, text: str, voice_id: str) -> TtsResult:
        tokens = text.split()
        word_dur = 60.0 / self.wpm
        words = tuple(
            Word(text=tok, start_s=i * word_dur, end_s=(i + 1) * word_dur) for i, tok in enumerate(tokens)
        )
        duration = max(len(tokens), 1) * word_dur
        fd, tmp_path = tempfile.mkstemp(suffix=".wav", prefix="studio-tts-")
        os.close(fd)
        out_path = Path(tmp_path)
        _write_tone_wav(out_path, duration)
        return TtsResult(wav_path=out_path, sample_rate=_SAMPLE_RATE, words=words)


ADAPTERS: dict[str, type] = {"stub": StubTtsAdapter}
