"""What a video is made with: the template, the anchor and the music track,
picked by code, not by a model (docs/architecture.md §5, «Шаблоны»,
«Ведущие: ротация», «Музыка»), so neighbouring videos of a channel don't
look and sound alike.

The input is the script, the channel profile and the channel's last videos
(their `package.json`, newest first: hub `releases_recent` → `video_package`).
For each of the three:
  1. the candidates that fit the script;
  2. without those the last few videos used (`RECENT`), unless that would
     leave none;
  3. the one used least recently in the history given, never-used first; a
     tie goes to the candidate the script's hash ranks highest (rendezvous
     hashing), so the same script and history always give the same pick and
     different stories spread over the library.
A value the script already fixes (an experiment arm's `script.template`)
wins over the pick. `exclude` ({"anchor": [...], "template": [...],
"music_track": [...]}) leaves out choices QA or Legal objected to, unless
that would leave nothing.
"""
from __future__ import annotations

import hashlib
import json
from dataclasses import dataclass

from .channel_assets import anchor_ids
from .errors import BuildError
from .music import licensed_for, load_catalog
from .paths import TEMPLATES_DIR

RECENT = {"template": 3, "anchor": 2, "music": 5}


@dataclass(frozen=True)
class Pick:
    template: str
    anchor: str
    music_track: str | None
    reasons: dict
    music_key: str | None = None  # the track's audio key in the store (music/catalog.yaml audio.key)

    def to_dict(self) -> dict:
        return {"template": self.template, "anchor": self.anchor, "music_track": self.music_track,
                "music_key": self.music_key, "reasons": self.reasons}


def _used(recent: list, field: str) -> list[str | None]:
    """What the recent packages (newest first) used for `field`; a package
    that isn't a JSON object counts as nothing used."""
    out = []
    for pkg in recent:
        if not isinstance(pkg, dict):
            out.append(None)
            continue
        if field == "template":
            value = pkg.get("template")
        elif field == "anchor":
            anchor = pkg.get("anchor")
            value = anchor.get("id") if isinstance(anchor, dict) else None
        else:
            music = pkg.get("music")
            value = music.get("track_id") if isinstance(music, dict) and music.get("enabled") else None
        out.append(value if isinstance(value, str) else None)
    return out


def _choose(candidates: list[str], used: list[str | None], recent_n: int, key: str,
            exclude: list[str] = ()) -> tuple[str, str]:
    """(choice, why) by the module's rules 2 and 3."""
    if not candidates:
        raise BuildError("nothing to pick from")
    candidates = [c for c in candidates if c not in exclude] or list(candidates)
    blocked = {u for u in used[:recent_n] if u}
    pool = [c for c in candidates if c not in blocked] or list(candidates)
    last_seen = {}
    for i, u in enumerate(used):
        if u and u not in last_seen:
            last_seen[u] = i  # 0 = the newest video
    never = len(used) + 1

    def rank(c: str):
        h = hashlib.sha256(f"{c}\n{key}".encode()).hexdigest()
        return (-last_seen.get(c, never), h)  # older (or never) first, then the hash

    choice = min(pool, key=rank)
    why = (f"{len(pool)} of {len(candidates)} not used by the last {recent_n} video(s); "
           + ("never used in the last %d" % len(used) if choice not in last_seen
              else "last used %d video(s) ago" % (last_seen[choice] + 1)))
    return choice, why


def _template_fits(template_id: str, script: dict) -> bool:
    with open(TEMPLATES_DIR / template_id / "template.json") as f:
        fit = json.load(f).get("fit", {})
    serious = bool(script.get("serious_mode"))
    mode = fit.get("serious_mode", "allowed")
    if (mode == "required" and not serious) or (mode == "forbidden" and serious):
        return False
    music = script.get("music") or {}
    if music.get("enabled") and fit.get("moods") and music.get("mood") not in fit["moods"]:
        return False
    return True


def pick(script: dict, channel_profile: dict, recent: list, catalog: list[dict] | None = None,
         exclude: dict | None = None) -> Pick:
    channel_id = channel_profile["id"]
    key = hashlib.sha256(json.dumps(script, sort_keys=True).encode()).hexdigest()
    exclude = exclude or {}
    reasons = {}

    fmt = channel_profile["format"]
    if script.get("template"):
        template, reasons["template"] = script["template"], "fixed by the script (experiment arm)"
    else:
        fitting = sorted(t for t in fmt["templates"] if _template_fits(t, script))
        if bool(script.get("serious_mode")):
            # A template made for serious mode beats one that merely allows it.
            only = [t for t in fitting if _serious_only(t)]
            fitting = only or fitting
        if fitting:
            template, why = _choose(fitting, _used(recent, "template"), RECENT["template"], key, exclude.get("template", []))
            reasons["template"] = why
        else:
            template = fmt["default_template"]
            reasons["template"] = "no template of the channel fits the script: the channel's default"

    anchors = anchor_ids(channel_id)
    if not anchors:
        raise BuildError(f"channel {channel_id!r} has no active anchor")
    anchor, reasons["anchor"] = _choose(anchors, _used(recent, "anchor"), min(RECENT["anchor"], len(anchors) - 1), key,
                                        exclude.get("anchor", []))

    music = script.get("music") or {}
    track = key_ = None
    if music.get("enabled"):
        catalog = catalog if catalog is not None else load_catalog()
        platforms = [p for p, cfg in channel_profile.get("platforms", {}).items() if cfg.get("enabled")]
        fits = [t for t in catalog if licensed_for(t, platforms) and music.get("mood") in t.get("tags", {}).get("mood", [])]
        if not fits:
            raise BuildError(f"no approved track for mood {music.get('mood')!r} licensed for {', '.join(platforms)}")
        # Nearest in energy first; the rotation chooses among those.
        best = min(abs(t.get("tags", {}).get("energy", 3) - music.get("energy", 3)) for t in fits)
        near = sorted(t["id"] for t in fits if abs(t.get("tags", {}).get("energy", 3) - music.get("energy", 3)) <= best + 1)
        track, why = _choose(near, _used(recent, "music"), RECENT["music"], key, exclude.get("music_track", []))
        key_ = next(t["audio"]["key"] for t in fits if t["id"] == track)
        reasons["music_track"] = f"mood {music.get('mood')}, energy within 1 of the nearest: {why}"
    else:
        reasons["music_track"] = "the script has no music"
    return Pick(template, anchor, track, reasons, key_)


def _serious_only(template_id: str) -> bool:
    with open(TEMPLATES_DIR / template_id / "template.json") as f:
        return json.load(f).get("fit", {}).get("serious_mode") == "required"
