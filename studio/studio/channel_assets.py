"""Loads the assets shared by every template for a channel: font, logo and
the anchors (docs/architecture.md §5, 'Общее для канала').

The channel has a roster of anchors, one per video (format.md, 'Ведущие'):
studio/channels/<id>/anchors/<anchor_id>/manifest.json and its frames. Each
manifest names the anchor's voice (`voice_id`) and its `status`: only
`active` anchors are picked for new videos; a `retired` one stays loadable so
an old package still validates.
"""
from __future__ import annotations

import hashlib
import json
from dataclasses import dataclass
from functools import lru_cache
from pathlib import Path

from PIL import Image, ImageFont

from .paths import channel_dir


@dataclass(frozen=True)
class AnchorSprites:
    id: str
    manifest: dict
    canvas: tuple[int, int]
    bodies: dict[str, Image.Image]
    eyes: dict[str, Image.Image]
    mouths: dict[str, Image.Image]

    @property
    def voice_id(self) -> str:
        return self.manifest["voice_id"]

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


def load_channel_logo(channel_id: str) -> Image.Image:
    return Image.open(channel_dir(channel_id) / "logo.png").convert("RGBA")


def anchors_dir(channel_id: str) -> Path:
    return channel_dir(channel_id) / "anchors"


def anchor_ids(channel_id: str, include_retired: bool = False) -> list[str]:
    """The channel's anchors, sorted by id: active ones, or all of them."""
    ids = []
    base = anchors_dir(channel_id)
    for manifest_path in sorted(base.glob("*/manifest.json")):
        with open(manifest_path) as f:
            status = json.load(f).get("status", "active")
        if include_retired or status == "active":
            ids.append(manifest_path.parent.name)
    return ids


def pick_anchor(channel_id: str, script: dict) -> str:
    """The anchor for a build that was not given one: an active anchor chosen
    by the script's content, so rebuilding a script keeps its anchor and
    different stories spread over the roster. Each anchor scores the script
    by a hash of both and the highest wins, so adding or retiring an anchor
    moves only the scripts it wins or held. In the pipeline hub picks the
    anchor (`anchor_pick`, not repeating the last videos'), and passes it."""
    ids = anchor_ids(channel_id)
    if not ids:
        raise FileNotFoundError(f"channel {channel_id!r} has no active anchor under {anchors_dir(channel_id)}")
    key = json.dumps(script, sort_keys=True)
    return max(ids, key=lambda anchor_id: hashlib.sha256(f"{anchor_id}\n{key}".encode()).digest())


@lru_cache(maxsize=32)
def load_anchor(channel_id: str, anchor_id: str) -> AnchorSprites:
    anchor_dir = anchors_dir(channel_id) / anchor_id
    if not (anchor_dir / "manifest.json").is_file():
        have = ", ".join(anchor_ids(channel_id, include_retired=True)) or "none"
        raise FileNotFoundError(f"channel {channel_id!r} has no anchor {anchor_id!r} (have: {have})")
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
    return AnchorSprites(
        id=anchor_id,
        manifest=manifest,
        canvas=(manifest["canvas"]["w"], manifest["canvas"]["h"]),
        bodies=bodies,
        eyes=eyes,
        mouths=mouths,
    )


@lru_cache(maxsize=32)
def load_channel_assets(channel_id: str = "8bitnews-us", anchor_id: str | None = None) -> ChannelAssets:
    """With no `anchor_id`, the first active anchor (template previews and
    checks that need some anchor in the studio)."""
    base = channel_dir(channel_id)
    font_path = base / "font" / "PressStart2P-Regular.ttf"
    logo = Image.open(base / "logo.png").convert("RGBA")
    if anchor_id is None:
        ids = anchor_ids(channel_id)
        if not ids:
            raise FileNotFoundError(f"channel {channel_id!r} has no active anchor under {anchors_dir(channel_id)}")
        anchor_id = ids[0]
    return ChannelAssets(channel_id=channel_id, font_path=font_path, logo=logo, anchor=load_anchor(channel_id, anchor_id))
