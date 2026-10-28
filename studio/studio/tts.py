"""TTS adapter interface and registry for `studio build --tts`.

`synthesize(text, voice_id, hinted) -> TtsResult(wav_path, sample_rate,
words, duration_s)`: `hinted` are the indexes of the tokens of `text` that a
pronunciation hint put there (its `say` respelling), for an adapter that
reads them its own way; `words` has one entry per whitespace token of `text` (the
*synthesis* text, already carrying any `script.pronunciations` substitutions
-- that mapping back to the original words is general code, not an
adapter's job; see `studio.pronunciation`), in order, each with
`start_s`/`end_s`. `duration_s` is the wav's own length, which may run past
the last word (a breath of tail); the timeline uses it so the concatenated
voice track and the scene timings always agree.

Each adapter builds itself from its own settings (`config.tts_settings`):
`ADAPTERS[name].from_settings(settings, serious=...)` -- the stub takes a
words-per-minute pace, Kokoro a speed -- so `build` never branches on the
adapter's name. `describe()` is what the package records under `voice`.

`stub` is a deterministic, quiet-but-measurable sine tone standing in for a
real voice: no network, no model, same output on every run.
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
    duration_s: float | None = None
    """Length of the wav; None means it ends with the last word."""


def write_wav_int16(path: Path, samples: np.ndarray, sample_rate: int) -> None:
    """Mono float samples in [-1, 1] -> 16-bit PCM wav."""
    pcm = (np.clip(samples, -1.0, 1.0) * 32767).astype(np.int16)
    with wave.open(str(path), "wb") as f:
        f.setnchannels(1)
        f.setsampwidth(2)
        f.setframerate(sample_rate)
        f.writeframes(pcm.tobytes())


def temp_wav_path() -> Path:
    fd, tmp_path = tempfile.mkstemp(suffix=".wav", prefix="studio-tts-")
    os.close(fd)
    return Path(tmp_path)


class StubTtsAdapter:
    """Each word lasts `60/wpm` seconds with no gap to the next word within
    a scene (format.md's own word budget is defined the same way)."""

    def __init__(self, wpm: float = DEFAULT_WPM):
        self.wpm = wpm

    @classmethod
    def from_settings(cls, settings: dict, serious: bool = False) -> "StubTtsAdapter":
        return cls(wpm=float(settings["serious_wpm" if serious else "wpm"]))

    def describe(self) -> dict:
        return {"model": f"stub sine tone at {self.wpm:g} wpm"}

    def synthesize(self, text: str, voice_id: str, hinted: frozenset[int] = frozenset()) -> TtsResult:
        tokens = text.split()
        word_dur = 60.0 / self.wpm
        words = tuple(
            Word(text=tok, start_s=i * word_dur, end_s=(i + 1) * word_dur) for i, tok in enumerate(tokens)
        )
        duration = max(len(tokens), 1) * word_dur
        n = max(int(round(duration * _SAMPLE_RATE)), 1)
        t = np.arange(n) / _SAMPLE_RATE
        out_path = temp_wav_path()
        write_wav_int16(out_path, _AMPLITUDE * np.sin(2 * np.pi * _TONE_HZ * t), _SAMPLE_RATE)
        return TtsResult(wav_path=out_path, sample_rate=_SAMPLE_RATE, words=words, duration_s=n / _SAMPLE_RATE)


def _kokoro_adapter():
    # onnxruntime and espeak-ng load only when Kokoro is actually used.
    from .kokoro import KokoroTtsAdapter

    return KokoroTtsAdapter


class _LazyAdapters(dict):
    def __getitem__(self, name):
        value = super().__getitem__(name)
        return value() if callable(value) and not isinstance(value, type) else value


ADAPTERS: dict[str, type] = _LazyAdapters(stub=StubTtsAdapter, kokoro=_kokoro_adapter)
