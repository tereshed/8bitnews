"""How a cover title wraps (`cover.layout.text_rect` w/h: how many lines of
how many characters) and where its black plate goes on the cover: shared by
`studio build` (drawing it, failing on a title that doesn't fit) and
`studio template validate` (rejecting a rect too small for a schema-valid
30-character title, or a plate that would leave the safe zone or cover the
AI line)."""
from __future__ import annotations

from .constants import FONT_PX, LOGICAL_W
from .errors import BuildError
from .subtitles import wrap_words
from .text_render import text_size

COVER_PAD = 2
COVER_TEXT_MAX_CHARS = 30  # script.schema.json cover.text maxLength
# Where the title plate sits on the cover (owner, 07.11.2026): low in the safe
# zone just above the AI line, or at its top. `studio pick` alternates them.
COVER_LAYOUTS = ("bottom", "top")
COVER_PLATE_PAD_X, COVER_PLATE_PAD_Y = 6, 4  # the subtitle plate's padding at the same 16px font
COVER_PLATE_GAP = 4  # between the plate and the AI strip ("bottom") or the safe zone's top ("top")


def cover_font_px(layout: dict) -> int:
    """Cover-only font size; older templates keep their original size."""
    return FONT_PX * layout.get("font_scale", 1)


def cover_text_capacity(text_rect: dict, font_px: int = FONT_PX) -> tuple[int, int]:
    """(characters per line, lines) at the cover's configured font size.
    The first line costs font_px, every additional one font_px+2."""
    max_chars = max(1, (text_rect["w"] - 2 * COVER_PAD) // font_px)
    max_rows = max(0, 1 + (text_rect["h"] - 2 * COVER_PAD - font_px) // (font_px + 2))
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
    """Word-wraps `text` inside `text_rect` at the cover's configured pixel
    font, raising BuildError if it needs more lines than the rect has room
    for. Used by both cover kinds (`S*` and `custom`)."""
    max_chars, max_rows = cover_text_capacity(text_rect, font.size)
    if font.size > FONT_PX and any(len(word) > max_chars for word in text.split()):
        raise BuildError(f"cover text {text!r} has a word longer than {max_chars} characters at {font.size}px; "
                         "shorten the wording instead of shrinking the font or splitting a word")
    lines = wrap_words(text.split(), max_chars)
    if len(lines) > max_rows:
        raise BuildError(
            f"cover text {text!r} needs {len(lines)} line(s) but cover.layout.text_rect {text_rect} only "
            f"fits {max_rows} at {font.size}px"
        )
    return lines


def cover_plate_box(n_lines: int, text_w: int, font_px: int, max_w: int, layout: str, ai_strip_top: int,
                    safe_top: int) -> dict:
    """The black plate behind a cover title of `n_lines` lines, the widest
    `text_w` px: as wide as that line plus padding but no wider than `max_w`
    (the text rect's width, which a line never exceeds), centered in the frame.
    "bottom": its lower edge COVER_PLATE_GAP above the AI strip; "top": its
    top COVER_PLATE_GAP below the top of the common safe zone."""
    if layout not in COVER_LAYOUTS:
        raise BuildError(f"cover layout {layout!r}: expected one of {', '.join(COVER_LAYOUTS)}")
    w = min(text_w + 2 * COVER_PLATE_PAD_X, max_w)
    h = n_lines * (font_px + 2) - 2 + 2 * COVER_PLATE_PAD_Y
    y = ai_strip_top - COVER_PLATE_GAP - h if layout == "bottom" else safe_top + COVER_PLATE_GAP
    return {"x": (LOGICAL_W - w) // 2, "y": y, "w": w, "h": h}


def cover_title_layout(text: str, text_rect: dict, font, layout: str, ai_strip_top: int,
                       safe_top: int) -> tuple[dict, list[tuple[str, tuple[int, int]]]]:
    """The plate (`cover_plate_box`) and each title line with its top-left
    origin in logical 270x480 coordinates, centered on the plate: the lines
    `fit_cover_title` wraps and checks."""
    lines = fit_cover_title(text, text_rect, font)
    widths = [text_size(line, font)[0] for line in lines]
    box = cover_plate_box(len(lines), max(widths, default=0), font.size, text_rect["w"], layout, ai_strip_top, safe_top)
    y0 = box["y"] + COVER_PLATE_PAD_Y
    return box, [(line, (box["x"] + (box["w"] - w) // 2, y0 + i * (font.size + 2)))
                 for i, (line, w) in enumerate(zip(lines, widths))]
