"""Reads/writes SRT subtitle files -- shared by `studio build` (writing
subtitles.srt from the same cues the frame engine renders, so the file and
the on-screen text always agree) and `studio validate`'s
subtitles_cover_words check (reading them back)."""
from __future__ import annotations

import re
from dataclasses import dataclass
from pathlib import Path
from typing import Iterable


@dataclass(frozen=True)
class SrtCue:
    index: int
    start_s: float
    end_s: float
    text: str


def _format_timestamp(t: float) -> str:
    t = max(t, 0.0)
    total_ms = round(t * 1000)
    hours, rem = divmod(total_ms, 3_600_000)
    minutes, rem = divmod(rem, 60_000)
    seconds, millis = divmod(rem, 1000)
    return f"{hours:02d}:{minutes:02d}:{seconds:02d},{millis:03d}"


def write_srt(cues: Iterable, path: Path) -> None:
    """`cues`: objects with `.lines` (tuple of strings, one per on-screen
    line), `.start_s`, `.end_s` -- i.e. `timeline.SubtitleCue`, so the same
    cues rendered on screen are written verbatim to the file."""
    blocks = []
    for i, cue in enumerate(cues, start=1):
        text = "\n".join(cue.lines)
        blocks.append(f"{i}\n{_format_timestamp(cue.start_s)} --> {_format_timestamp(cue.end_s)}\n{text}\n")
    Path(path).write_text("\n".join(blocks), encoding="utf-8")


_TIME_RE = re.compile(r"(\d+):(\d+):(\d+)[,.](\d+)")


def _parse_timestamp(s: str) -> float:
    m = _TIME_RE.match(s.strip())
    if m is None:
        raise ValueError(f"not an SRT timestamp: {s!r}")
    h, mi, sec, ms = (int(x) for x in m.groups())
    return h * 3600 + mi * 60 + sec + ms / 1000.0


def parse_srt(path: Path) -> list[SrtCue]:
    text = Path(path).read_text(encoding="utf-8").strip()
    if not text:
        return []
    cues = []
    for block in re.split(r"\n\s*\n", text):
        lines = block.splitlines()
        if len(lines) < 2:
            continue
        index = int(lines[0].strip())
        start_str, end_str = (p.strip() for p in lines[1].split("-->"))
        body = "\n".join(lines[2:])
        cues.append(SrtCue(index=index, start_s=_parse_timestamp(start_str), end_s=_parse_timestamp(end_str), text=body))
    return cues
