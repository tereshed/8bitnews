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
