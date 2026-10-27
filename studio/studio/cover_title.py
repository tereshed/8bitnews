"""Where a cover title goes inside `cover.layout.text_rect`: shared by
`studio build` (drawing it, failing on a title that doesn't fit) and
`studio template validate` (rejecting a rect too small for a schema-valid
30-character title)."""
from __future__ import annotations

from .constants import FONT_PX
from .errors import BuildError
from .subtitles import wrap_words

COVER_PAD = 2
COVER_TEXT_MAX_CHARS = 30  # script.schema.json cover.text maxLength


def cover_text_capacity(text_rect: dict) -> tuple[int, int]:
    """(characters per line, lines) that fit in `text_rect` at FONT_PX.
    Line i sits at y = pad + i*(FONT_PX+2), each FONT_PX tall: the first line
    costs FONT_PX, every additional one FONT_PX+2."""
    max_chars = max(1, (text_rect["w"] - 2 * COVER_PAD) // FONT_PX)
    max_rows = 1 + max(0, text_rect["h"] - 2 * COVER_PAD - FONT_PX) // (FONT_PX + 2)
    return max_chars, max_rows


def worst_case_title_lines(max_chars: int, max_len: int = COVER_TEXT_MAX_CHARS) -> int:
    """Most lines greedy wrapping (`wrap_words`) can produce for a title of up
    to `max_len` characters whose words each fit on one line. Searches every
    sequence of word lengths by (characters used, current line length);
    e.g. "Voter disenfranchisement fears" needs 3 lines at 23 characters.
    Titles with a word longer than a line (hard-broken mid-word) are left
    out: `build` checks the actual title before TTS and names it if it
    doesn't fit."""
    best: dict[tuple[int, int], int] = {}
    stack = [((w, w), 1) for w in range(1, min(max_chars, max_len) + 1)]
    worst = 0
    while stack:
        (used, line), lines = stack.pop()
        if best.get((used, line), 0) >= lines:
            continue
        best[(used, line)] = lines
        worst = max(worst, lines)
        for w in range(1, max_chars + 1):
            if used + 1 + w > max_len:
                break
            if line + 1 + w <= max_chars:
                stack.append(((used + 1 + w, line + 1 + w), lines))
            else:
                stack.append(((used + 1 + w, w), lines + 1))
    return worst


def fit_cover_title(text: str, text_rect: dict, font) -> list[str]:
    """Word-wraps `text` inside `text_rect` at the channel's fixed pixel
    font, raising BuildError if it needs more lines than the rect has room
    for. Used by both cover kinds (`S*` and `custom`)."""
    max_chars, max_rows = cover_text_capacity(text_rect)
    lines = wrap_words(text.split(), max_chars)
    if len(lines) > max_rows:
        raise BuildError(
            f"cover text {text!r} needs {len(lines)} line(s) but cover.layout.text_rect {text_rect} only "
            f"fits {max_rows} at {FONT_PX}px"
        )
    return lines


def cover_title_layout(text: str, text_rect: dict, font) -> list[tuple[str, tuple[int, int]]]:
    """Each cover title line with its top-left origin in logical 270x480
    coordinates: the same geometry `fit_cover_title` checks against."""
    lines = fit_cover_title(text, text_rect, font)
    x = text_rect["x"] + COVER_PAD
    y0 = text_rect["y"] + COVER_PAD
    return [(line, (x, y0 + i * (FONT_PX + 2))) for i, line in enumerate(lines)]
