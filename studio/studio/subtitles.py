"""Splits scene narration into burned-in subtitle cues: each cue has at most
`MAX_SUBTITLE_LINES` lines of at most `MAX_SUBTITLE_CHARS_PER_LINE` characters
(format.md: 'Субтитры — вшиты, по фразам: до 2 строк, до 32 символов в
строке'). Deterministic: the same narration always wraps to the same lines
and the same cue durations.
"""
from __future__ import annotations

from dataclasses import dataclass

from .constants import MAX_SUBTITLE_CHARS_PER_LINE, MAX_SUBTITLE_LINES


@dataclass(frozen=True)
class SubtitleCue:
    lines: tuple[str, ...]
    start_s: float
    end_s: float

    @property
    def text(self) -> str:
        return " ".join(self.lines)


def wrap_words(words: list[str], max_chars: int) -> list[str]:
    """Greedy word-wrap into lines of at most `max_chars` characters. A
    single word longer than max_chars is hard-broken (not expected for
    English narration, but keeps the function total)."""
    lines: list[str] = []
    current: list[str] = []
    current_len = 0
    for word in words:
        if len(word) > max_chars:
            if current:
                lines.append(" ".join(current))
                current, current_len = [], 0
            for i in range(0, len(word), max_chars):
                lines.append(word[i : i + max_chars])
            continue
        added_len = len(word) + (1 if current else 0)
        if current_len + added_len > max_chars:
            lines.append(" ".join(current))
            current, current_len = [word], len(word)
        else:
            current.append(word)
            current_len += added_len
    if current:
        lines.append(" ".join(current))
    return lines


def split_subtitles(
    narration: str,
    start_s: float,
    end_s: float,
    max_lines: int = MAX_SUBTITLE_LINES,
    max_chars: int = MAX_SUBTITLE_CHARS_PER_LINE,
) -> list[SubtitleCue]:
    """Wraps `narration` into lines, then groups lines into cues of at most
    `max_lines`, and distributes [start_s, end_s] across cues proportionally
    to each cue's character count (so a short second line doesn't eat a
    disproportionate share of screen time)."""
    words = narration.split()
    if not words:
        return []
    lines = wrap_words(words, max_chars)
    cues_lines = [lines[i : i + max_lines] for i in range(0, len(lines), max_lines)]
    weights = [sum(len(line) for line in cue) or 1 for cue in cues_lines]
    total_weight = sum(weights)
    duration = end_s - start_s
    cues: list[SubtitleCue] = []
    t = start_s
    for i, (cue_lines, weight) in enumerate(zip(cues_lines, weights)):
        is_last = i == len(cues_lines) - 1
        cue_end = end_s if is_last else t + duration * (weight / total_weight)
        cues.append(SubtitleCue(lines=tuple(cue_lines), start_s=t, end_s=cue_end))
        t = cue_end
    return cues
