"""NES master palette loading and nearest-color quantization (no dithering).

`quantize_to_palette` is shared by the frame engine (every drawn frame is
snapped to a template's palette subset) and by `studio template validate`
(checking that template PNG assets only use in-palette colors) and will
later be reused by the image provider (docs/architecture.md §12).
"""
from __future__ import annotations

import json
from functools import lru_cache
from pathlib import Path

import numpy as np
from PIL import Image

from .paths import channel_dir


def hex_to_rgb(hex_color: str) -> tuple[int, int, int]:
    h = hex_color.lstrip("#")
    return (int(h[0:2], 16), int(h[2:4], 16), int(h[4:6], 16))


def rgb_to_hex(rgb: tuple[int, int, int]) -> str:
    return "#%02x%02x%02x" % tuple(int(c) for c in rgb)


@lru_cache(maxsize=8)
def load_channel_palette(channel_id: str = "8bitnews-us") -> dict:
    path = channel_dir(channel_id) / "palette.json"
    with open(path) as f:
        return json.load(f)


def channel_palette_colors(channel_id: str = "8bitnews-us") -> list[str]:
    return load_channel_palette(channel_id)["colors"]


def channel_palette_set(channel_id: str = "8bitnews-us") -> set[str]:
    return {c.lower() for c in channel_palette_colors(channel_id)}


_CHUNK = 65536  # pixels per pass of Palette.quantize_array


class Palette:
    """A fixed, ordered list of RGB colors with nearest-color lookup."""

    def __init__(self, hex_colors: list[str]):
        self.hex_colors = [c.lower() for c in hex_colors]
        self._rgb = np.array([hex_to_rgb(c) for c in self.hex_colors], dtype=np.float64)

    def __len__(self) -> int:
        return len(self.hex_colors)

    def __contains__(self, hex_color: str) -> bool:
        return hex_color.lower() in self.hex_colors

    def nearest(self, rgb: tuple[int, int, int]) -> tuple[int, int, int]:
        px = np.array(rgb, dtype=np.float64)
        d2 = np.sum((self._rgb - px) ** 2, axis=1)
        idx = int(np.argmin(d2))
        return tuple(int(c) for c in self._rgb[idx])

    def quantize_array(self, rgb_array: np.ndarray) -> np.ndarray:
        """rgb_array: (H, W, 3) uint8 -> same shape, every pixel snapped to
        the nearest palette color (euclidean distance in RGB, no dithering)."""
        flat = rgb_array.reshape(-1, 3).astype(np.float64)
        # (N, P) squared distances, a chunk of pixels at a time: a pan/zoom
        # source is 540x960 or more, and against the whole 55-color palette
        # one pass over it held about 1 GB.
        idx = np.empty(len(flat), dtype=np.intp)
        for start in range(0, len(flat), _CHUNK):
            part = flat[start:start + _CHUNK]
            idx[start:start + _CHUNK] = np.argmin(((part[:, None, :] - self._rgb[None, :, :]) ** 2).sum(axis=2), axis=1)
        out = self._rgb[idx].astype(np.uint8)
        return out.reshape(rgb_array.shape)


def quantize_image(img: "Image.Image", palette: Palette) -> "Image.Image":
    """Snap every opaque pixel of an RGBA image to the nearest palette color.
    Transparent pixels (alpha==0) pass through unchanged so layered PNGs keep
    their transparency; partially transparent pixels are treated as opaque
    for color snapping (the engine only produces fully opaque or fully
    transparent pixels, so this keeps the function simple and deterministic).
    """
    rgba = np.array(img.convert("RGBA"))
    rgb = rgba[:, :, :3]
    alpha = rgba[:, :, 3]
    quantized_rgb = palette.quantize_array(rgb)
    out = np.dstack([quantized_rgb, alpha])
    return Image.fromarray(out.astype(np.uint8), mode="RGBA")


def image_colors_in_palette(path: Path, palette_hex: set[str]) -> list[str]:
    """Returns the sorted list of distinct out-of-palette hex colors found
    among the opaque pixels of the PNG at `path` (empty if all in-palette)."""
    img = Image.open(path).convert("RGBA")
    colors = img.getcolors(maxcolors=1_000_000) or []
    bad = set()
    for _count, (r, g, b, a) in colors:
        if a == 0:
            continue
        hexc = rgb_to_hex((r, g, b))
        if hexc not in palette_hex:
            bad.add(hexc)
    return sorted(bad)
