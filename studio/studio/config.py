"""Studio-side settings that are neither part of a channel's publishing
profile (channels/<id>/channel.yaml) nor of a template package: the
`studio validate` thresholds, each TTS adapter's own settings and the audio
mix levels. `studio/config.json` overrides the defaults below key by key."""
from __future__ import annotations

import json

from .paths import STUDIO_ROOT

_CONFIG_PATH = STUDIO_ROOT / "config.json"

DEFAULT_MAX_FILE_SIZE_BYTES = 50 * 1024 * 1024
DEFAULT_MAX_KEYFRAME_CELL_DIFF = 32.0

DEFAULT_TTS = {
    "stub": {"wpm": 144.0, "serious_wpm": 131.0},
    "kokoro": {"speed": 1.1, "serious_speed": 1.0, "sentence_pause_s": 0.25, "clause_pause_s": 0.1},
}

DEFAULT_AUDIO = {
    "sample_rate": 48000,
    "aac_bitrate": "192k",
    "true_peak_ceiling_dbtp": -1.5,
    "music_bed_db": -10.0,
    "duck_db": -8.0,
    "duck_attack_s": 0.12,
    "duck_release_s": 0.35,
    "duck_hold_gap_s": 0.8,
    "fade_in_s": 1.0,
    "fade_out_s": 2.0,
    "sfx_db": -8.0,
}

DEFAULT_CHECKS = {
    "min_voice_over_music_db": 10.0,
    "no_music_max_music_share": 0.05,
    "no_music_max_other_rise_db": 6.0,
    "no_music_max_pause_db": -35.0,
}


def _load() -> dict:
    if _CONFIG_PATH.is_file():
        with open(_CONFIG_PATH) as f:
            return json.load(f)
    return {}


def max_file_size_bytes() -> int:
    return int(_load().get("max_file_size_bytes", DEFAULT_MAX_FILE_SIZE_BYTES))


def max_keyframe_cell_diff() -> float:
    """`video_matches_keyframes`: the largest mean difference (0-255) allowed
    in any 4x4 cell between a keyframe PNG and the H.264 frame it came from."""
    return float(_load().get("max_keyframe_cell_diff", DEFAULT_MAX_KEYFRAME_CELL_DIFF))


def tts_settings(name: str) -> dict:
    """One adapter's constructor settings (`ADAPTERS[name].from_settings`)."""
    return {**DEFAULT_TTS.get(name, {}), **_load().get("tts", {}).get(name, {})}


def audio_settings(overrides: dict | None = None) -> dict:
    """Mix levels for `studio build`; `overrides` (tests) win over the file."""
    return {**DEFAULT_AUDIO, **_load().get("audio", {}), **(overrides or {})}


def check_settings() -> dict:
    """Audio thresholds of `studio validate` that are a judgement of the
    studio's (how far under the voice the music must sit, how much music
    video_no_music.mp4 may hold); the channel policy is in `constants`."""
    return {**DEFAULT_CHECKS, **_load().get("checks", {})}
