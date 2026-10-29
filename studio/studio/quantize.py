"""Fit an arbitrary-size scene image into the engine's 9:16 logical frame and
snap it to the NES palette. Used by the frame engine for `shot: scene`
images, and will be reused as-is by the image provider once scenes are
generated (docs/architecture.md §12: 'студия сама приводит кадр к сетке и
палитре NES')."""
from __future__ import annotations

from PIL import Image

from .constants import LOGICAL_H, LOGICAL_W
from .palette import Palette, quantize_image


def crop_to_aspect(img: Image.Image, target_w: int, target_h: int) -> Image.Image:
    """Centered crop to the target aspect ratio (no distortion)."""
    w, h = img.size
    target_ratio = target_w / target_h
    current_ratio = w / h
    if current_ratio > target_ratio:
        # wider than target: crop the sides
        new_w = round(h * target_ratio)
        x0 = (w - new_w) // 2
        return img.crop((x0, 0, x0 + new_w, h))
    if current_ratio < target_ratio:
        # taller than target: crop top/bottom
        new_h = round(w / target_ratio)
        y0 = (h - new_h) // 2
        return img.crop((0, y0, w, y0 + new_h))
    return img


def quantize(
    img: Image.Image,
    palette: Palette | None,
    target_w: int = LOGICAL_W,
    target_h: int = LOGICAL_H,
) -> Image.Image:
    """Crop to the target aspect, resize to the logical frame, and snap every
    pixel to the nearest palette color (no dithering); with no palette the
    picture keeps its own colors. Deterministic for a given Pillow version
    (resampling is pinned via requirements.txt)."""
    img = img.convert("RGB")
    cropped = crop_to_aspect(img, target_w, target_h)
    resized = cropped.resize((target_w, target_h), Image.LANCZOS)
    if palette is None:
        return resized
    return quantize_image(resized.convert("RGBA"), palette).convert("RGB")
