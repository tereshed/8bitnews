"""Procedurally draws the placeholder pixel-art assets shipped with the repo:
the channel logo, the placeholder anchor sprite set, and the two template
asset packs (nes-desk, night-report).

These are deliberately simple geometric pixel art so the engine and its
tests have something real to draw. A future Design pass replaces the PNG
files in place (studio/channels/8bitnews-us/anchor/, studio/templates/*/assets/)
without touching any engine code — see studio/README.md.

Run from the studio/ directory: `python3 scripts/generate_assets.py`
(needs Pillow; the Docker image or the dev venv both have it).
"""
from __future__ import annotations

import json
import sys
from pathlib import Path

from PIL import Image

ROOT = Path(__file__).resolve().parent.parent
CHANNEL_DIR = ROOT / "channels" / "8bitnews-us"
TEMPLATES_DIR = ROOT / "templates"

with open(CHANNEL_DIR / "palette.json") as f:
    PALETTE = json.load(f)["colors"]


def hx(c: str) -> tuple[int, int, int]:
    c = c.lstrip("#")
    return tuple(int(c[i : i + 2], 16) for i in (0, 2, 4))  # type: ignore[return-value]


BLACK = hx("#000000")
WHITE = hx("#fcfcfc")
SKIN = hx("#fce0a8")
SKIN_SHADOW = hx("#fca044")
HAIR = hx("#503000")
SUIT = hx("#004058")
SUIT_LIGHT = hx("#008888")
DESK = hx("#ac7c00")
DESK_DARK = hx("#503000")
WALL = hx("#0058f8")
SCREEN_BG = hx("#000000")
NIGHT_WALL = hx("#004058")
NIGHT_DESK = hx("#787878")
ACCENT_RED = hx("#f83800")
ACCENT_GREEN = hx("#00b800")


def new_rgba(w: int, h: int) -> Image.Image:
    return Image.new("RGBA", (w, h), (0, 0, 0, 0))


def rect(img: Image.Image, x0: int, y0: int, x1: int, y1: int, color) -> None:
    px = img.load()
    for y in range(y0, y1):
        for x in range(x0, x1):
            if 0 <= x < img.width and 0 <= y < img.height:
                px[x, y] = (*color, 255)


# ---------------------------------------------------------------------------
# Logo: an 8BN monogram on transparent background, 32x16 logical px.
# ---------------------------------------------------------------------------
def build_logo() -> None:
    img = new_rgba(32, 16)
    rect(img, 1, 2, 30, 14, BLACK)
    rect(img, 2, 3, 29, 13, WHITE)
    # Blocky "8BN" using coarse rectangles (pixel-art monogram, not a real font).
    rect(img, 4, 5, 9, 11, ACCENT_RED)
    rect(img, 5, 6, 8, 10, WHITE)
    rect(img, 12, 5, 17, 11, WALL)
    rect(img, 13, 6, 16, 10, WHITE)
    rect(img, 20, 5, 25, 11, ACCENT_GREEN)
    rect(img, 21, 6, 24, 10, WHITE)
    img.save(CHANNEL_DIR / "logo.png")


# ---------------------------------------------------------------------------
# Anchor sprite set: body (per pose) + eyes + mouth layers, 64x96 logical px.
# ---------------------------------------------------------------------------
ANCHOR_W, ANCHOR_H = 64, 96
EYES_OFFSET = (20, 28)
EYES_SIZE = (24, 6)
MOUTH_OFFSET = (24, 40)
MOUTH_SIZE = (16, 10)


def build_body(pose: str) -> Image.Image:
    img = new_rgba(ANCHOR_W, ANCHOR_H)
    # desk in foreground
    rect(img, 0, 78, 64, 96, DESK_DARK)
    rect(img, 0, 74, 64, 78, DESK)
    # suit / torso
    rect(img, 12, 50, 52, 80, SUIT)
    rect(img, 12, 50, 52, 54, SUIT_LIGHT)
    # neck + head
    rect(img, 24, 40, 40, 50, SKIN)
    rect(img, 18, 14, 46, 42, SKIN)
    rect(img, 18, 14, 46, 18, SKIN_SHADOW)
    # hair: neutral/serious pose keeps it flatter and darker (muted, no shine)
    if pose == "serious":
        rect(img, 16, 10, 48, 20, BLACK)
        rect(img, 16, 10, 48, 22, HAIR)
    else:
        rect(img, 16, 8, 48, 18, HAIR)
        rect(img, 14, 14, 20, 30, HAIR)
        rect(img, 44, 14, 50, 30, HAIR)
    # eyebrows: neutral pose flatter/lower than the default calm pose
    brow_y = 25 if pose == "serious" else 24
    rect(img, 22, brow_y, 29, brow_y + 2, HAIR)
    rect(img, 35, brow_y, 42, brow_y + 2, HAIR)
    return img


def build_eyes(state: str) -> Image.Image:
    img = new_rgba(*EYES_SIZE)
    if state == "open":
        rect(img, 1, 1, 6, 5, WHITE)
        rect(img, 2, 2, 5, 4, BLACK)
        rect(img, 17, 1, 22, 5, WHITE)
        rect(img, 18, 2, 21, 4, BLACK)
    else:  # closed: a thin lid line
        rect(img, 1, 3, 6, 4, BLACK)
        rect(img, 17, 3, 22, 4, BLACK)
    return img


def build_mouth(level: int) -> Image.Image:
    # level 0 = closed, 1 = mid (talking softly), 2 = open (loud)
    img = new_rgba(*MOUTH_SIZE)
    heights = {0: 1, 1: 3, 2: 6}
    h = heights[level]
    y0 = 4
    rect(img, 3, y0, 13, y0 + h, BLACK)
    if level > 0:
        rect(img, 4, y0 + 1, 12, y0 + h - 1, hx("#a80020"))
    return img


def build_anchor_set() -> None:
    out = CHANNEL_DIR / "anchor"
    out.mkdir(parents=True, exist_ok=True)
    for pose in ("normal", "serious"):
        build_body(pose).save(out / f"body_{pose}.png")
    for state in ("open", "closed"):
        build_eyes(state).save(out / f"eyes_{state}.png")
    for level in (0, 1, 2):
        build_mouth(level).save(out / f"mouth_{level}.png")

    manifest = {
        "$comment": "Placeholder adult anchor sprite set; see studio/README.md "
        "for how a future Design pass swaps these files without touching engine code.",
        "canvas": {"w": ANCHOR_W, "h": ANCHOR_H},
        "face_rect": {
            "x": 14,
            "y": 8,
            "w": 36,
            "h": 42,
            "$comment": "Covers hair/head/eyebrows/eyes/mouth drawn by build_body/build_eyes/build_mouth "
            "below, in canvas-local pixels; template validate uses this to reject plates/subtitles "
            "that would cover the anchor's face.",
        },
        "poses": {
            "normal": {"body": "body_normal.png"},
            "serious": {"body": "body_serious.png"},
        },
        "eyes": {
            "offset": {"x": EYES_OFFSET[0], "y": EYES_OFFSET[1]},
            "frames": {"open": "eyes_open.png", "closed": "eyes_closed.png"},
        },
        "mouth": {
            "offset": {"x": MOUTH_OFFSET[0], "y": MOUTH_OFFSET[1]},
            "frames": {"0": "mouth_0.png", "1": "mouth_1.png", "2": "mouth_2.png"},
            "volume_thresholds": {"1": 0.15, "2": 0.5},
        },
        "blink": {"period_s": 4.0, "duration_s": 0.12},
        "license": "Original placeholder art drawn for this repository; no third-party assets.",
    }
    with open(out / "manifest.json", "w") as f:
        json.dump(manifest, f, indent=2)
        f.write("\n")


# ---------------------------------------------------------------------------
# Template asset packs
# ---------------------------------------------------------------------------
DESK_Y = 318  # the desk's top edge: below every anchor's face at studio.anchor y=196


def build_nes_desk_assets() -> None:
    out = TEMPLATES_DIR / "nes-desk" / "assets"
    out.mkdir(parents=True, exist_ok=True)

    # A plain wall and the desk in front of the anchor (`front: true`): the
    # owner asked for nothing that carries no meaning (29.10: no windows, no box).
    back = new_rgba(270, 480)
    rect(back, 0, 0, 270, 480, WALL)
    back.save(out / "background_layer0.png")

    desk = new_rgba(270, 480)
    rect(desk, 0, DESK_Y, 270, 480, DESK_DARK)
    rect(desk, 0, DESK_Y, 270, DESK_Y + 12, DESK)
    desk.save(out / "desk.png")

    screen = new_rgba(270, 480)
    rect(screen, 170, 60, 260, 170, BLACK)
    rect(screen, 175, 65, 255, 165, hx("#787878"))
    screen.save(out / "screen_frame.png")

    sample = new_rgba(270, 480)
    rect(sample, 0, 0, 270, 480, hx("#4428bc"))
    rect(sample, 30, 60, 240, 420, hx("#9878f8"))
    rect(sample, 60, 90, 210, 390, hx("#d8b8f8"))
    sample.save(out / "sample_scene.png")


def build_night_report_assets() -> None:
    out = TEMPLATES_DIR / "night-report" / "assets"
    out.mkdir(parents=True, exist_ok=True)

    back = new_rgba(270, 480)
    rect(back, 0, 0, 270, 480, NIGHT_WALL)
    back.save(out / "background_layer0.png")

    desk = new_rgba(270, 480)
    rect(desk, 0, DESK_Y, 270, 480, hx("#000000"))
    rect(desk, 0, DESK_Y, 270, DESK_Y + 12, NIGHT_DESK)
    desk.save(out / "desk.png")

    screen = new_rgba(270, 480)
    rect(screen, 160, 50, 250, 160, BLACK)
    rect(screen, 165, 55, 245, 155, hx("#7c7c7c"))
    screen.save(out / "screen_frame.png")

    sample = new_rgba(270, 480)
    rect(sample, 0, 0, 270, 480, hx("#000000"))
    rect(sample, 20, 40, 250, 440, hx("#004058"))
    rect(sample, 60, 80, 210, 400, hx("#008888"))
    sample.save(out / "sample_scene.png")


if __name__ == "__main__" and sys.argv[1:] == ["templates"]:
    build_nes_desk_assets()
    build_night_report_assets()
    print("template assets generated")
elif __name__ == "__main__":
    build_logo()
    build_anchor_set()
    build_nes_desk_assets()
    build_night_report_assets()
    print("assets generated")
