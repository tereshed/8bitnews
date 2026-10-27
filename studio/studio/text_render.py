"""Pixel-exact (non-anti-aliased) text drawing: Pillow anti-aliases glyph
edges by default, which would paint off-palette blend colors onto frames
that must stay strictly inside the NES palette. We rasterize each glyph run
to a bilevel mask and stamp the palette color through it, so every painted
pixel is exactly the requested fill or outline color."""
from __future__ import annotations

from PIL import Image, ImageDraw, ImageFont

_THRESHOLD = 128


def _binary_mask(text: str, font: ImageFont.FreeTypeFont) -> tuple[Image.Image, tuple[int, int, int, int]]:
    bbox = font.getbbox(text)
    x0, y0, x1, y1 = bbox
    w, h = max(x1 - x0, 1), max(y1 - y0, 1)
    mask = Image.new("L", (w, h), 0)
    draw = ImageDraw.Draw(mask)
    draw.text((-x0, -y0), text, font=font, fill=255)
    mask = mask.point(lambda p: 255 if p >= _THRESHOLD else 0)
    return mask, bbox


def text_size(text: str, font: ImageFont.FreeTypeFont) -> tuple[int, int]:
    _mask, (x0, y0, x1, y1) = _binary_mask(text, font)
    return x1 - x0, y1 - y0


def draw_text(
    img: Image.Image,
    xy: tuple[int, int],
    text: str,
    font: ImageFont.FreeTypeFont,
    fill: tuple[int, int, int],
    outline: tuple[int, int, int] | None = None,
) -> None:
    """Draws `text` with its top-left glyph origin at `xy`, in exact `fill`
    color, with an optional 1px solid `outline` behind it (both bilevel, no
    anti-aliasing)."""
    if not text:
        return
    mask, _bbox = _binary_mask(text, font)
    x, y = xy
    if outline is not None:
        solid_outline = Image.new("RGBA", mask.size, (*outline, 255))
        for dx, dy in ((-1, 0), (1, 0), (0, -1), (0, 1)):
            img.paste(solid_outline, (x + dx, y + dy), mask)
    solid_fill = Image.new("RGBA", mask.size, (*fill, 255))
    img.paste(solid_fill, (x, y), mask)
