"""The owner's tests of the format that the studio itself carries out
(channel.yaml `experiments`).

`music_off` (owner, 18.11.2026): between `from` and `until` (UTC dates,
inclusive) half of the videos whose script asks for music are made without
it, to see whether the music changes how far the Shorts feed pushes a video.
The half is decided by the hash of the story's research dossier
(`script.dossier`): a rework of the script or a rebuild keeps the video in its
arm (a new dossier, after Legal sends the story back to Research, may change
it), and the arm can't follow the topic, the slot or anything else about the
video. A script in serious mode has no music anyway and is not in the test.
`studio pick` picks no track for the arm "off"; `studio build` leaves the
music out and records the arm in package.json (`experiment`), whatever track
it was given.
"""
from __future__ import annotations

import datetime
import hashlib
import json

MUSIC_OFF = "music_off"


def _date(value) -> datetime.date:
    return value if isinstance(value, datetime.date) else datetime.date.fromisoformat(str(value))


def music_arm(script: dict, channel_profile: dict, today: datetime.date | None = None) -> dict | None:
    """{"id": "music_off", "arm": "off" | "on"} for a script in the test; None
    when the channel runs no such test today or the script has no music."""
    experiments = channel_profile.get("experiments") or {}
    cfg = experiments.get(MUSIC_OFF)
    if not experiments.get("enabled") or not isinstance(cfg, dict):
        return None
    if not (script.get("music") or {}).get("enabled"):
        return None
    today = today or datetime.datetime.now(datetime.timezone.utc).date()
    if today < _date(cfg["from"]) or today > _date(cfg["until"]):
        return None
    unit = script.get("dossier") or json.dumps(script, sort_keys=True)
    digest = hashlib.sha256(f"{MUSIC_OFF}\n{unit}".encode()).digest()
    return {"id": MUSIC_OFF, "arm": "off" if digest[0] % 2 else "on"}


def music_left_out(experiment: dict | None) -> bool:
    return bool(experiment) and experiment.get("id") == MUSIC_OFF and experiment.get("arm") == "off"
