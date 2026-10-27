"""Loads the assets shared by every template for a channel: font, logo,
anchor sprite set (docs/architecture.md §5, 'Общее для канала')."""
from __future__ import annotations

import json
from dataclasses import dataclass
from functools import lru_cache
from pathlib import Path

from PIL import Image, ImageFont

from .paths import channel_dir


@dataclass(frozen=True)
class AnchorSprites:
    manifest: dict
    canvas: tuple[int, int]
    bodies: dict[str, Image.Image]
    eyes: dict[str, Image.Image]
    mouths: dict[str, Image.Image]

    def eyes_offset(self) -> tuple[int, int]:
        o = self.manifest["eyes"]["offset"]
        return o["x"], o["y"]

    def mouth_offset(self) -> tuple[int, int]:
        o = self.manifest["mouth"]["offset"]
        return o["x"], o["y"]

    def mouth_level_for_volume(self, volume: float) -> int:
        thresholds = self.manifest["mouth"]["volume_thresholds"]
        if volume >= thresholds["2"]:
            return 2
        if volume >= thresholds["1"]:
            return 1
        return 0

    def is_blinking(self, t_abs: float) -> bool:
        blink = self.manifest["blink"]
        period = blink["period_s"]
        phase = t_abs % period
        return phase < blink["duration_s"]

    def face_rect_in_frame(self, anchor_cfg: dict) -> dict:
        """The anchor's face rect (manifest `face_rect`, canvas-local) mapped
        into frame coordinates for a template's `studio.anchor` placement
        (x, y, integer scale). Used by `studio template validate` to reject
        plates/subtitles that would cover the anchor's face."""
        fr = self.manifest["face_rect"]
        scale = anchor_cfg["scale"]
        return {
            "x": anchor_cfg["x"] + round(fr["x"] * scale),
            "y": anchor_cfg["y"] + round(fr["y"] * scale),
            "w": round(fr["w"] * scale),
            "h": round(fr["h"] * scale),
        }


@dataclass(frozen=True)
class ChannelAssets:
    channel_id: str
    font_path: Path
    logo: Image.Image
    anchor: AnchorSprites

    def font(self, size_px: int) -> ImageFont.FreeTypeFont:
        return ImageFont.truetype(str(self.font_path), size_px)


@lru_cache(maxsize=4)
def load_channel_assets(channel_id: str = "8bitnews-us") -> ChannelAssets:
    base = channel_dir(channel_id)
    font_path = base / "font" / "PressStart2P-Regular.ttf"
    logo = Image.open(base / "logo.png").convert("RGBA")

    anchor_dir = base / "anchor"
    with open(anchor_dir / "manifest.json") as f:
        manifest = json.load(f)
    bodies = {
        pose: Image.open(anchor_dir / spec["body"]).convert("RGBA")
        for pose, spec in manifest["poses"].items()
    }
    eyes = {
        state: Image.open(anchor_dir / fname).convert("RGBA")
        for state, fname in manifest["eyes"]["frames"].items()
    }
    mouths = {
        level: Image.open(anchor_dir / fname).convert("RGBA")
        for level, fname in manifest["mouth"]["frames"].items()
    }
    anchor = AnchorSprites(
        manifest=manifest,
        canvas=(manifest["canvas"]["w"], manifest["canvas"]["h"]),
        bodies=bodies,
        eyes=eyes,
        mouths=mouths,
    )
    return ChannelAssets(channel_id=channel_id, font_path=font_path, logo=logo, anchor=anchor)
