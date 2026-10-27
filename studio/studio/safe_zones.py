"""Platform safe-zone rectangles for the channel (logical 270x480 px), with
their sources: channels/8bitnews-us/safe_zones.json. `studio template
validate` checks every plate/subtitle rect against the common rectangle."""
from __future__ import annotations

import json
from functools import lru_cache

from .paths import channel_dir


@lru_cache(maxsize=8)
def load_safe_zones(channel_id: str = "8bitnews-us") -> dict:
    with open(channel_dir(channel_id) / "safe_zones.json") as f:
        return json.load(f)


def common_safe_rect(channel_id: str = "8bitnews-us") -> dict:
    return load_safe_zones(channel_id)["common_safe_rect"]


def rect_within(rect: dict, container: dict) -> bool:
    return (
        rect["x"] >= container["x"]
        and rect["y"] >= container["y"]
        and rect["x"] + rect["w"] <= container["x"] + container["w"]
        and rect["y"] + rect["h"] <= container["y"] + container["h"]
    )


def rects_intersect(a: dict, b: dict) -> bool:
    return a["x"] < b["x"] + b["w"] and b["x"] < a["x"] + a["w"] and a["y"] < b["y"] + b["h"] and b["y"] < a["y"] + a["h"]
