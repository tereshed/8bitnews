"""Studio-side `studio validate` thresholds that are neither part of a
channel's publishing profile (channels/<id>/channel.yaml) nor of a template
package -- currently just the output file-size ceiling."""
from __future__ import annotations

import json

from .paths import STUDIO_ROOT

DEFAULT_MAX_FILE_SIZE_BYTES = 50 * 1024 * 1024
_CONFIG_PATH = STUDIO_ROOT / "config.json"


def max_file_size_bytes() -> int:
    if _CONFIG_PATH.is_file():
        with open(_CONFIG_PATH) as f:
            data = json.load(f)
        return int(data.get("max_file_size_bytes", DEFAULT_MAX_FILE_SIZE_BYTES))
    return DEFAULT_MAX_FILE_SIZE_BYTES


DEFAULT_MAX_KEYFRAME_CELL_DIFF = 32.0


def max_keyframe_cell_diff() -> float:
    """`video_matches_keyframes`: the largest mean difference (0-255) allowed
    in any 4x4 cell between a keyframe PNG and the H.264 frame it came from."""
    if _CONFIG_PATH.is_file():
        with open(_CONFIG_PATH) as f:
            data = json.load(f)
        return float(data.get("max_keyframe_cell_diff", DEFAULT_MAX_KEYFRAME_CELL_DIFF))
    return DEFAULT_MAX_KEYFRAME_CELL_DIFF
