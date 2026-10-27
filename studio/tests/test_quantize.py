import numpy as np
from PIL import Image

from studio.constants import LOGICAL_H, LOGICAL_W, OUTPUT_H, OUTPUT_W, SCALE
from studio.palette import Palette, channel_palette_colors
from studio.quantize import quantize
from studio.video import upscale_nearest


def _random_source_image(w: int, h: int, seed: int) -> Image.Image:
    rng = np.random.default_rng(seed)
    arr = rng.integers(0, 256, size=(h, w, 3), dtype=np.uint8)
    return Image.fromarray(arr, mode="RGB")


def test_quantize_output_size():
    palette = Palette(channel_palette_colors())
    src = _random_source_image(800, 600, seed=1)
    out = quantize(src, palette)
    assert out.size == (LOGICAL_W, LOGICAL_H)


def test_quantize_pixels_all_in_palette():
    palette = Palette(channel_palette_colors())
    palette_set = set(palette.hex_colors)
    src = _random_source_image(640, 360, seed=2)  # 16:9, exercises the crop path too
    out = quantize(src, palette)
    colors = out.getcolors(maxcolors=1_000_000)
    assert colors is not None
    for _count, rgb in colors:
        hexc = "#%02x%02x%02x" % rgb
        assert hexc in palette_set


def test_quantize_handles_portrait_source():
    palette = Palette(channel_palette_colors())
    src = _random_source_image(300, 1000, seed=3)  # taller than 9:16
    out = quantize(src, palette)
    assert out.size == (LOGICAL_W, LOGICAL_H)


def test_upscale_nearest_4x4_blocks_are_monochrome():
    palette = Palette(channel_palette_colors())
    src = _random_source_image(500, 500, seed=4)
    logical = quantize(src, palette)
    upscaled = upscale_nearest(logical)
    assert upscaled.size == (OUTPUT_W, OUTPUT_H)
    arr = np.array(upscaled)
    for by in range(0, OUTPUT_H, SCALE):
        for bx in range(0, OUTPUT_W, SCALE):
            block = arr[by : by + SCALE, bx : bx + SCALE]
            assert (block == block[0, 0]).all(), f"block at ({bx},{by}) is not monochrome"


def test_upscale_nearest_colors_all_in_palette():
    palette = Palette(channel_palette_colors())
    palette_set = set(palette.hex_colors)
    src = _random_source_image(500, 500, seed=5)
    logical = quantize(src, palette)
    upscaled = upscale_nearest(logical)
    colors = upscaled.getcolors(maxcolors=1_000_000)
    assert colors is not None
    for _count, rgb in colors:
        assert ("#%02x%02x%02x" % rgb) in palette_set
