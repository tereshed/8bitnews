"""A generated character picture -> an anchor sprite in the channel palette.

The image model draws the character on a flat background, with its own
"pixels" several picture pixels wide and soft edges between them. Here:
  1. the background is whatever touches the border in the border's color
     (flood fill) and enclosed pockets of exactly that color, grown by a few
     pixels to drop the soft fringe;
  2. the figure is cropped and fitted into the sprite canvas, centered and
     standing on its bottom edge;
  3. every picture pixel is snapped to the palette *first*, and each sprite
     pixel takes the most common color of its block -- averaging before
     snapping smears outlines into colors that are in neither;
  4. snapping is by CIELAB distance, with hue weighed more for near-neutral
     colors, so white and grey stay white and grey (plain RGB distance turns
     a white blouse into pastel pink and green dots).

`SpriteFit` keeps step 2's crop and scale, so an edited version of the same
picture (closed eyes, open mouth) lands on exactly the same sprite pixels.
"""
from __future__ import annotations

from dataclasses import dataclass

import numpy as np
from PIL import Image, ImageDraw, ImageFilter

from .palette import Palette

BACKGROUND_TOLERANCE = 90  # RGB distance from the border color still counted as background
POCKET_TOLERANCE = 45  # closer than this, background even where the figure encloses it
FRINGE_PX = 3  # picture pixels of soft edge dropped around the figure


@dataclass(frozen=True)
class SpriteFit:
    crop: tuple[int, int, int, int]  # x0, y0, x1, y1 of the figure in the picture
    size: tuple[int, int]  # the figure's size on the canvas
    offset: tuple[int, int]  # its top-left corner on the canvas
    canvas: tuple[int, int]


def background_mask(img: Image.Image, tolerance: int = BACKGROUND_TOLERANCE, fringe_px: int = FRINGE_PX) -> np.ndarray:
    """True where the picture is background: connected to the border and
    close to the border's median color."""
    rgb = np.asarray(img.convert("RGB")).astype(np.int32)
    h, w, _ = rgb.shape
    border = np.concatenate([rgb[0], rgb[-1], rgb[:, 0], rgb[:, -1]])
    bg = np.median(border, axis=0)
    close = ((rgb - bg) ** 2).sum(-1) < tolerance * tolerance
    # .copy(): an image made by fromarray can share the array's memory, and
    # floodfill then silently changes nothing.
    marks = Image.fromarray(np.where(close, 255, 0).astype(np.uint8), "L").copy()
    for x, y in [(x, 0) for x in range(w)] + [(x, h - 1) for x in range(w)] + \
                [(0, y) for y in range(h)] + [(w - 1, y) for y in range(h)]:
        if marks.getpixel((x, y)) == 255:
            ImageDraw.floodfill(marks, (x, y), 128)
    # Plus pockets of the background color the figure encloses (between an
    # arm and the body): the brief asks for one flat color that no clothing uses.
    mask = (np.asarray(marks) == 128) | (((rgb - bg) ** 2).sum(-1) < POCKET_TOLERANCE * POCKET_TOLERANCE)
    if fringe_px:
        grown = Image.fromarray(np.where(mask, 255, 0).astype(np.uint8), "L").filter(ImageFilter.MaxFilter(2 * fringe_px + 1))
        mask = np.asarray(grown) > 0
    return mask


def fit_figure(mask: np.ndarray, canvas: tuple[int, int], top_margin: int = 2) -> SpriteFit:
    ys, xs = np.where(~mask)
    if ys.size == 0:
        raise ValueError("the picture has no figure: everything matches the background color")
    x0, x1, y0, y1 = int(xs.min()), int(xs.max()) + 1, int(ys.min()), int(ys.max()) + 1
    cw, ch = canvas
    scale = min(cw / (x1 - x0), (ch - top_margin) / (y1 - y0))
    w, h = max(1, round((x1 - x0) * scale)), max(1, round((y1 - y0) * scale))
    return SpriteFit(crop=(x0, y0, x1, y1), size=(w, h), offset=((cw - w) // 2, ch - h), canvas=canvas)


def _lab(rgb: np.ndarray) -> np.ndarray:
    c = rgb.astype(np.float64) / 255
    c = np.where(c > 0.04045, ((c + 0.055) / 1.055) ** 2.4, c / 12.92)
    xyz = c @ np.array([[0.4124, 0.3576, 0.1805], [0.2126, 0.7152, 0.0722], [0.0193, 0.1192, 0.9505]]).T
    xyz = xyz / np.array([0.95047, 1.0, 1.08883])
    f = np.where(xyz > 0.008856, np.cbrt(xyz), 7.787 * xyz + 16 / 116)
    return np.stack([116 * f[..., 1] - 16, 500 * (f[..., 0] - f[..., 1]), 200 * (f[..., 1] - f[..., 2])], -1)


def snap_to_palette(rgb: np.ndarray, palette: Palette) -> np.ndarray:
    """(..., 3) uint8 -> the nearest palette colors (see the module note)."""
    pal_rgb = np.array([[int(h[i:i + 2], 16) for i in (1, 3, 5)] for h in palette.hex_colors], dtype=np.uint8)
    pal_lab = _lab(pal_rgb)
    flat = rgb.reshape(-1, 3)
    uniq, inverse = np.unique(flat, axis=0, return_inverse=True)
    lab = _lab(uniq)
    d = (lab[:, None, :] - pal_lab[None]) ** 2
    chroma = np.hypot(lab[:, 1], lab[:, 2])
    hue_weight = 1 + 3 * np.exp(-((chroma / 8) ** 2))
    dist = d[..., 0] + hue_weight[:, None] * (d[..., 1] + d[..., 2])
    return pal_rgb[dist.argmin(1)][inverse.reshape(-1)].reshape(rgb.shape)


def render_sprite(img: Image.Image, fit: SpriteFit, palette: Palette, mask: np.ndarray) -> Image.Image:
    """The figure of `img` (background `mask`) on a transparent canvas."""
    x0, y0, x1, y1 = fit.crop
    rgb = np.asarray(img.convert("RGB"))[y0:y1, x0:x1]
    bg = mask[y0:y1, x0:x1]
    snapped = snap_to_palette(rgb, palette).astype(np.int64)
    packed = (snapped[..., 0] << 16) | (snapped[..., 1] << 8) | snapped[..., 2]
    w, h = fit.size
    ys = np.linspace(0, y1 - y0, h + 1).round().astype(int)
    xs = np.linspace(0, x1 - x0, w + 1).round().astype(int)
    out = np.zeros((h, w, 4), np.uint8)
    for j in range(h):
        for i in range(w):
            block_bg = bg[ys[j]:ys[j + 1], xs[i]:xs[i + 1]]
            block = packed[ys[j]:ys[j + 1], xs[i]:xs[i + 1]][~block_bg]
            if block.size * 2 < block_bg.size:
                continue  # mostly background: transparent
            values, counts = np.unique(block, return_counts=True)
            v = int(values[counts.argmax()])
            out[j, i] = ((v >> 16) & 255, (v >> 8) & 255, v & 255, 255)
    canvas = Image.new("RGBA", fit.canvas, (0, 0, 0, 0))
    canvas.alpha_composite(Image.fromarray(out, "RGBA"), fit.offset)
    return canvas


def sprite_from_picture(img: Image.Image, canvas: tuple[int, int], palette: Palette) -> tuple[Image.Image, SpriteFit, np.ndarray]:
    mask = background_mask(img)
    fit = fit_figure(mask, canvas)
    return render_sprite(img, fit, palette, mask), fit, mask
