"""What a video is made with: the template, the anchor, the music track and
where the cover's title goes, picked by code, not by a model
(docs/architecture.md §5, «Шаблоны», «Ведущие: ротация», «Музыка»), so
neighbouring videos of a channel don't look and sound alike.

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

The cover layout (the title plate at the "bottom" or the "top", owner
07.11.2026) strictly alternates: the opposite of the newest recent package
that recorded one (the studio's `cover.layout`, or `pick.cover_layout`);
when none did, the script's hash decides. Only videos that reached (or may
still reach) the feed count: a release hub marks rejected, changes_requested
or failed doesn't (`recent_from_hub`), so a rebuild after the moderator's
"changes requested" keeps the layout its turn had.

A rebuild keeps what its earlier package was made with, so it differs only
where asked (`keep`, `_keep_earlier`): each kind stays unless `exclude` names
the kind, or the choice no longer holds (it doesn't fit the script or the
channel any more, a video released since has taken its turn), and then that
kind alone is picked as above, with a note.
"""
from __future__ import annotations

import hashlib
import json
from dataclasses import dataclass

from .channel_assets import anchor_ids
from .cover_title import COVER_LAYOUTS
from .errors import BuildError
from .music import licensed_for, load_catalog
from .paths import TEMPLATES_DIR

RECENT = {"template": 3, "anchor": 2, "music": 5}
# Releases that never reach the feed (hub releases_recent `status`; internal/store/releases.go
# terminalReleaseStatuses): the cover layout doesn't alternate against them.
NOT_IN_FEED = ("rejected", "changes_requested", "failed")


@dataclass(frozen=True)
class Pick:
    template: str
    anchor: str
    music_track: str | None
    reasons: dict
    music_key: str | None = None  # the track's audio key in the store (music/catalog.yaml audio.key)
    cover_layout: str = "bottom"  # where the cover title's plate goes (`studio build --cover-layout`)
    notes: tuple[str, ...] = ()  # what a rebuild couldn't keep from its earlier package, and why

    def to_dict(self) -> dict:
        out = {"template": self.template, "anchor": self.anchor, "music_track": self.music_track,
               "music_key": self.music_key, "cover_layout": self.cover_layout, "reasons": self.reasons}
        return {**out, "notes": list(self.notes)} if self.notes else out


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


def _recorded_cover_layout(pkg) -> str | None:
    """The cover layout a recent package recorded: `cover.layout` (what
    `studio build` drew), else `pick.cover_layout` (newsroom/tools/video_build.py
    stores the pick); None for an older package or anything else."""
    if not isinstance(pkg, dict):
        return None
    for section, field in (("cover", "layout"), ("pick", "cover_layout")):
        record = pkg.get(section)
        value = record.get(field) if isinstance(record, dict) else None
        if value in COVER_LAYOUTS:
            return value
    return None


def recent_from_hub(answer: dict) -> tuple[list, list]:
    """hub releases_recent's answer -> (each release's `video_package`, newest
    first; the same list with the releases that never reached the feed
    (`NOT_IN_FEED`) as None: what the cover layout alternates against)."""
    releases = answer.get("releases") or []
    packages = [r.get("video_package") if isinstance(r, dict) else None for r in releases]
    in_feed = [pkg if isinstance(r, dict) and r.get("status") not in NOT_IN_FEED else None
               for r, pkg in zip(releases, packages)]
    return packages, in_feed


def _cover_layout(recent: list, key: str) -> tuple[str, str]:
    """(layout, why): the opposite of the newest recorded one, else by the script's hash."""
    for i, pkg in enumerate(recent):
        last = _recorded_cover_layout(pkg)
        if last is not None:
            return next(c for c in COVER_LAYOUTS if c != last), f"the opposite of {last!r}, {i + 1} video(s) ago"
    choice = min(COVER_LAYOUTS, key=lambda c: hashlib.sha256(f"{c}\n{key}".encode()).hexdigest())
    return choice, f"no recorded cover layout in the last {len(recent)} video(s): the script's hash"


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


def _template_candidates(script: dict, fmt: dict) -> list[str]:
    fitting = sorted(t for t in fmt["templates"] if _template_fits(t, script))
    if bool(script.get("serious_mode")):
        # A template made for serious mode beats one that merely allows it.
        only = [t for t in fitting if _serious_only(t)]
        fitting = only or fitting
    return fitting


def _track_candidates(music: dict, channel_profile: dict, catalog: list[dict]) -> dict[str, str]:
    """Track id -> audio key: licensed, in the script's mood, nearest in energy; the rotation chooses among them."""
    platforms = [p for p, cfg in channel_profile.get("platforms", {}).items() if cfg.get("enabled")]
    fits = [t for t in catalog if licensed_for(t, platforms) and music.get("mood") in t.get("tags", {}).get("mood", [])]
    if not fits:
        raise BuildError(f"no approved track for mood {music.get('mood')!r} licensed for {', '.join(platforms)}")
    best = min(abs(t.get("tags", {}).get("energy", 3) - music.get("energy", 3)) for t in fits)
    return {t["id"]: t["audio"]["key"] for t in sorted(fits, key=lambda t: t["id"])
            if abs(t.get("tags", {}).get("energy", 3) - music.get("energy", 3)) <= best + 1}


def _keep_problem(value: str | None, candidates: list[str], used: list[str | None], recent_n: int) -> str | None:
    """Why an earlier choice can't stay, by the same rules as `_choose`: it must still be a candidate, and
    not what a neighbour just used, unless every candidate is."""
    if value not in candidates:
        return "it no longer fits the script and the channel"
    blocked = {u for u in used[:recent_n] if u}
    if value in blocked and any(c not in blocked for c in candidates):
        return "a video released since uses it"
    return None


def _neighbours(packages: list, earlier_archive: str | None) -> list[dict]:
    """The videos a kept choice must not clash with: those that reached or may reach the feed (`recent_from_hub`
    leaves None for the others), without the earlier package itself."""
    def is_earlier(pkg: dict) -> bool:
        media = pkg.get("media")
        return bool(earlier_archive) and isinstance(media, dict) and media.get("package_archive") == earlier_archive
    return [pkg for pkg in packages if isinstance(pkg, dict) and not is_earlier(pkg)]


def _keep_earlier(fresh: Pick, keep: dict, script: dict, channel_profile: dict, neighbours: list[dict],
                  catalog: list[dict] | None, exclude: dict, key: str) -> Pick:
    """`fresh` (what `pick` chooses today) with each kind the earlier package's where that kind still holds."""
    reasons, notes = dict(fresh.reasons), []
    fmt = channel_profile["format"]

    def settled(kind: str, field: str, candidates: list[str], recent_n: int, used_field: str, fresh_value):
        value = keep.get(field)
        if value in (exclude.get(field) or []):
            return fresh_value
        problem = _keep_problem(value, candidates, _used(neighbours, used_field), recent_n)
        if problem:
            notes.append(f"{kind} {value!r} of the earlier package can't stay ({problem}): picked {fresh_value!r}")
            return fresh_value
        reasons[field] = "kept from the earlier package"
        return value

    templates = [script["template"]] if script.get("template") else (_template_candidates(script, fmt)
                                                                     or [fmt["default_template"]])
    template = settled("template", "template", templates, RECENT["template"], "template", fresh.template)
    anchors = anchor_ids(channel_profile["id"])
    anchor = settled("anchor", "anchor", anchors, min(RECENT["anchor"], len(anchors) - 1), "anchor", fresh.anchor)

    track, music_key = fresh.music_track, fresh.music_key
    music = script.get("music") or {}
    if not music.get("enabled"):
        if keep.get("music_track"):
            notes.append(f"track {keep['music_track']!r} of the earlier package can't stay (the script has no music now)")
    else:
        keys = _track_candidates(music, channel_profile, catalog if catalog is not None else load_catalog())
        track = settled("track", "music_track", sorted(keys), RECENT["music"], "music", fresh.music_track)
        music_key = keys[track]

    layout = keep.get("cover_layout")
    last = next((recorded for recorded in map(_recorded_cover_layout, neighbours) if recorded), None)
    problem = ("it is not on record" if layout not in COVER_LAYOUTS
               else "a video released since uses it, and the layouts alternate" if layout == last else None)
    if problem:
        was = layout
        layout, reasons["cover_layout"] = _cover_layout(neighbours, key)
        notes.append(f"cover layout {was!r} of the earlier package can't stay ({problem}): picked {layout!r}")
    else:
        reasons["cover_layout"] = "kept from the earlier package"
    return Pick(template, anchor, track, reasons, music_key, layout, tuple(notes))


def pick(script: dict, channel_profile: dict, recent: list, catalog: list[dict] | None = None,
         exclude: dict | None = None, cover_recent: list | None = None, keep: dict | None = None) -> Pick:
    """`cover_recent`: the packages the cover layout alternates against
    (`recent_from_hub`), when not all of `recent` reached the feed. `keep`: the
    earlier package's choices (`template`, `anchor`, `music_track`,
    `cover_layout`, and its `package_archive`), for a rebuild."""
    channel_id = channel_profile["id"]
    key = hashlib.sha256(json.dumps(script, sort_keys=True).encode()).hexdigest()
    exclude = exclude or {}
    reasons = {}

    fmt = channel_profile["format"]
    if script.get("template"):
        template, reasons["template"] = script["template"], "fixed by the script (experiment arm)"
    else:
        fitting = _template_candidates(script, fmt)
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
        keys = _track_candidates(music, channel_profile, catalog if catalog is not None else load_catalog())
        track, why = _choose(sorted(keys), _used(recent, "music"), RECENT["music"], key, exclude.get("music_track", []))
        key_ = keys[track]
        reasons["music_track"] = f"mood {music.get('mood')}, energy within 1 of the nearest: {why}"
    else:
        reasons["music_track"] = "the script has no music"
    feed = recent if cover_recent is None else cover_recent
    cover_layout, reasons["cover_layout"] = _cover_layout(feed, key)
    fresh = Pick(template, anchor, track, reasons, key_, cover_layout)
    if keep is None:
        return fresh
    return _keep_earlier(fresh, keep, script, channel_profile, _neighbours(feed, keep.get("package_archive")), catalog,
                         exclude, key)


def _serious_only(template_id: str) -> bool:
    with open(TEMPLATES_DIR / template_id / "template.json") as f:
        return json.load(f).get("fit", {}).get("serious_mode") == "required"
