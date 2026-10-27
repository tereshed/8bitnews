"""Splits timed words into burned-in subtitle cues: each cue has at most
`max_lines` lines of at most `max_chars` characters (format.md: 'Субтитры —
вшиты, по фразам: до 2 строк, до 32 символов в строке'). Deterministic: the
same words always produce the same cues.

`split_cues` replaces E2a-1's `split_subtitles`: instead of re-splitting
scene narration text with proportional-by-character timing, it groups the
*actual* timed `timeline.Word`s a TTS adapter (or `uniform_timeline`'s
synthetic pace) produced, so cue boundaries always land on real word
boundaries.
"""
from __future__ import annotations

from dataclasses import dataclass
from typing import TYPE_CHECKING, Sequence

from .constants import MAX_SUBTITLE_CHARS_PER_LINE, MAX_SUBTITLE_LINES

if TYPE_CHECKING:
    from .timeline import Word

_HOLD_GAP_S = 0.3  # format.md 'паузы 0,3-0,5 с между блоками': also the min visible gap between cues
_SENTENCE_PUNCT = ".!?;"


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


def _fits(texts: list[str], max_lines: int, max_chars: int) -> bool:
    return len(wrap_words(texts, max_chars)) <= max_lines


def _cut_at_punctuation(group: Sequence["Word"]) -> int:
    """How many words of `group` (a maximal greedy fit) to actually keep in
    this cue: cut right after the last word ending in '.', '!', '?' or ';',
    or a comma that isn't the 1st or 2nd word -- else keep the whole group."""
    cut = len(group)
    for k, word in enumerate(group, start=1):
        text = word.text
        if not text:
            continue
        last = text[-1]
        if last in _SENTENCE_PUNCT:
            cut = k
        elif last == "," and k >= 3:
            cut = k
    return cut


def split_cues(
    words: Sequence["Word"],
    max_lines: int = MAX_SUBTITLE_LINES,
    max_chars: int = MAX_SUBTITLE_CHARS_PER_LINE,
) -> list[SubtitleCue]:
    """Greedily packs `words` into cues capped at `max_lines` lines of
    `max_chars` characters each; within the maximal group that still fits,
    cuts after the last sentence/clause punctuation if there is one (so cues
    break on natural phrase boundaries, not just whichever word happened to
    fill the line). A cue starts at its first word's start and ends at its
    last word's end, extended to the next cue's start if the gap between
    them is under 0.3s (no visible flicker between consecutive cues)."""
    if not words:
        return []

    groups: list[tuple[Sequence["Word"], list[str]]] = []
    i, n = 0, len(words)
    while i < n:
        texts = [words[i].text]
        j = i + 1
        while j < n and _fits(texts + [words[j].text], max_lines, max_chars):
            texts.append(words[j].text)
            j += 1
        maximal_group = words[i:j]
        cut = _cut_at_punctuation(maximal_group)
        group = maximal_group[:cut]
        lines = wrap_words([w.text for w in group], max_chars)[:max_lines]
        groups.append((group, lines))
        i += cut

    cues: list[SubtitleCue] = []
    for idx, (group, lines) in enumerate(groups):
        start_s = group[0].start_s
        end_s = group[-1].end_s
        if idx + 1 < len(groups):
            next_start = groups[idx + 1][0][0].start_s
            if 0 <= next_start - end_s < _HOLD_GAP_S:
                end_s = next_start
        cues.append(SubtitleCue(lines=tuple(lines), start_s=start_s, end_s=end_s))
    return cues
