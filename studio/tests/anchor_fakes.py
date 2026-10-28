"""A drawn stand-in for the image model's anchor picture, and a client that
answers like OpenRouter without the network (tests of studio.anchors)."""
from __future__ import annotations

import io

from PIL import Image, ImageDraw

from studio.openrouter import ImageReply, JsonReply, NoImageError

W, H = 480, 640
GREEN = (4, 248, 4)
SKIN, HAIR, SUIT, SHIRT = (240, 208, 176), (80, 48, 0), (0, 64, 88), (252, 252, 252)
HEAD = (180, 60, 300, 230)  # x0, y0, x1, y1
EYES = (200, 120, 280, 140)
MOUTH = (220, 180, 260, 196)


def picture(eyes_closed: bool = False, mouth: int = 0, shift: tuple[int, int] = (0, 0), face_color=SKIN,
            head_top: int | None = None) -> bytes:
    """A figure on the flat green background, drawn in 4-pixel blocks with a
    black outline. `shift` moves everything (an edit redrawn a little off)."""
    img = Image.new("RGB", (W, H), GREEN)
    d = ImageDraw.Draw(img)
    dx, dy = shift
    hx0, hy0, hx1, hy1 = HEAD
    if head_top is not None:
        hy0 = head_top
    d.rectangle((120 + dx, 230 + dy, 360 + dx, H), fill=SUIT, outline=(0, 0, 0), width=4)  # body to the bottom edge
    d.rectangle((210 + dx, 230 + dy, 270 + dx, 330 + dy), fill=SHIRT)
    d.rectangle((hx0 + dx, hy0 + dy, hx1 + dx, hy1 + dy), fill=face_color, outline=(0, 0, 0), width=4)
    d.rectangle((hx0 + dx, hy0 + dy, hx1 + dx, hy0 + 40 + dy), fill=HAIR)
    ex0, ey0, ex1, ey1 = EYES
    if eyes_closed:
        d.rectangle((ex0 + dx, ey0 + 8 + dy, ex0 + 28 + dx, ey0 + 12 + dy), fill=(0, 0, 0))
        d.rectangle((ex1 - 28 + dx, ey0 + 8 + dy, ex1 + dx, ey0 + 12 + dy), fill=(0, 0, 0))
    else:
        d.rectangle((ex0 + dx, ey0 + dy, ex0 + 28 + dx, ey1 + dy), fill=(252, 252, 252), outline=(0, 0, 0), width=4)
        d.rectangle((ex1 - 28 + dx, ey0 + dy, ex1 + dx, ey1 + dy), fill=(252, 252, 252), outline=(0, 0, 0), width=4)
    mx0, my0, mx1, my1 = MOUTH
    if mouth == 0:
        d.rectangle((mx0 + dx, my0 + 6 + dy, mx1 + dx, my0 + 10 + dy), fill=(168, 16, 0))
    else:
        d.rectangle((mx0 + dx, my0 + dy, mx1 + dx, my0 + (8 if mouth == 1 else 16) + dy), fill=(136, 20, 0),
                    outline=(0, 0, 0), width=4)
    buf = io.BytesIO()
    img.save(buf, "PNG")
    return buf.getvalue()


def norm_box(box) -> list[int]:
    """(x0, y0, x1, y1) pixels -> [ymin, xmin, ymax, xmax] in 0-1000."""
    x0, y0, x1, y1 = box
    return [round(y0 / H * 1000), round(x0 / W * 1000), round(y1 / H * 1000), round(x1 / W * 1000)]


LANDMARKS = {"head": norm_box(HEAD), "eyes": norm_box(EYES), "mouth": norm_box(MOUTH)}


class FakeClient:
    """`images`: what generate_image answers, in order (bytes, or an
    exception to raise); `landmarks`: what ask_json answers (one dict, or a
    list answered in turn)."""

    def __init__(self, images: list, landmarks: dict | list | None = None):
        self.images = list(images)
        self.landmarks = landmarks if landmarks is not None else LANDMARKS
        self.calls: list[dict] = []

    def generate_image(self, model, prompt, aspect_ratio, images=(), variant=0):
        self.calls.append({"kind": "image", "prompt": prompt, "variant": variant, "inputs": len(images)})
        nxt = self.images.pop(0)
        if isinstance(nxt, Exception):
            raise nxt
        return ImageReply(nxt, 0.067, False, model)

    def ask_json(self, model, prompt, images=(), variant=0):
        self.calls.append({"kind": "json", "prompt": prompt, "inputs": len(images), "variant": variant})
        answer = self.landmarks.pop(0) if isinstance(self.landmarks, list) else self.landmarks
        return JsonReply(dict(answer), 0.001, False, model)


def no_image() -> NoImageError:
    return NoImageError("fake refusal")


def as_image(png: bytes) -> Image.Image:
    return Image.open(io.BytesIO(png))
