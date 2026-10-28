"""studio.sprite: a generated picture -> an anchor sprite."""
import numpy as np
import pytest
from PIL import Image, ImageDraw

from studio.palette import Palette, channel_palette_colors
from studio.sprite import background_mask, fit_figure, snap_to_palette, sprite_from_picture

PAL = Palette(channel_palette_colors())
GREEN = (6, 249, 5)


def _figure(pocket=False) -> Image.Image:
    img = Image.new("RGB", (400, 600), GREEN)
    d = ImageDraw.Draw(img)
    d.rectangle((100, 200, 300, 599), fill=(0, 64, 88), outline=(0, 0, 0), width=8)  # body to the bottom edge
    d.rectangle((150, 60, 250, 200), fill=(240, 208, 176), outline=(0, 0, 0), width=8)  # head
    d.rectangle((170, 220, 230, 400), fill=(250, 250, 250))  # white shirt
    d.rectangle((120, 420, 160, 460), fill=(118, 118, 118))  # grey patch
    if pocket:
        d.rectangle((260, 300, 285, 340), fill=GREEN)  # background seen between arm and body
    return img


def test_background_and_enclosed_pockets_are_transparent():
    sprite, fit, _ = sprite_from_picture(_figure(pocket=True), (120, 160), PAL)
    a = np.asarray(sprite)
    assert a[0, 0, 3] == 0 and a[5, 5, 3] == 0  # beside the head
    assert a[78, 80, 3] == 0  # the pocket between "arm" and body
    plain = np.asarray(sprite_from_picture(_figure(), (120, 160), PAL)[0])
    assert plain[78, 80, 3] == 255
    rgb = a[..., :3][a[..., 3] > 0]
    assert not any(tuple(c) == (0, 168, 0) or (c[1] > 200 and c[0] < 60 and c[2] < 60) for c in rgb), "green left in"


def test_figure_stands_on_the_bottom_edge_centered():
    sprite, fit, _ = sprite_from_picture(_figure(), (120, 160), PAL)
    alpha = np.asarray(sprite)[..., 3] > 0
    assert alpha[-1].any() and not alpha[0].any()
    cols = np.where(alpha.any(0))[0]
    assert abs((cols.min() + cols.max()) / 2 - 59.5) <= 1


def test_every_pixel_is_in_the_palette():
    sprite, _, _ = sprite_from_picture(_figure(), (120, 160), PAL)
    colors = {"#%02x%02x%02x" % c[1][:3] for c in sprite.getcolors(1 << 16) if c[1][3]}
    assert colors <= set(PAL.hex_colors)


def test_white_and_grey_stay_neutral():
    snapped = snap_to_palette(np.array([[[250, 250, 250], [118, 118, 118], [236, 236, 232], [60, 60, 62]]], np.uint8), PAL)
    for r, g, b in snapped[0]:
        assert r == g == b, (r, g, b)


def test_an_outline_stays_black_instead_of_blending():
    """Averaging a black outline into skin gives brown/orange halos; taking
    each block's most common palette color keeps the outline black."""
    sprite, _, _ = sprite_from_picture(_figure(), (120, 160), PAL)
    a = np.asarray(sprite)
    opaque = a[..., 3] > 0
    colors = {tuple(c) for c in a[..., :3][opaque]}
    assert (0, 0, 0) in colors
    assert len(colors) <= 6  # suit, outline, skin, shirt, grey: no blend colors


def test_a_picture_with_no_figure_is_refused():
    img = Image.new("RGB", (50, 50), GREEN)
    with pytest.raises(ValueError, match="no figure"):
        fit_figure(background_mask(img), (120, 160))
