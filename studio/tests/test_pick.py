"""studio.pick: template, anchor and track by code, rotating against the
channel's last videos; the cover layout alternating with the last one."""
import copy
import json

import pytest

from conftest import FIXTURES_DIR
from studio import repo
from studio.channel_assets import anchor_ids
from studio.pick import RECENT, pick

CHANNEL = "8bitnews-us"


@pytest.fixture
def profile():
    p = repo.load_channel_profile(CHANNEL)
    return dict(p, format=dict(p["format"], templates=["nes-desk", "night-report"]))


def _script(**kw):
    s = json.loads((FIXTURES_DIR / "scripts" / "demo.json").read_text())
    s.pop("template", None)
    s.update(kw)
    return s


def _record(p):
    """A recent video as hub has it: the studio's package plus the pick (newsroom/tools/video_build.py)."""
    return {"template": p.template, "anchor": {"id": p.anchor},
            "music": {"enabled": p.music_track is not None, "track_id": p.music_track},
            "cover": {"frame": "S1", "layout": p.cover_layout, "picture": "S1"}, "pick": p.to_dict()}


def _run(profile, n, **kw):
    recent, picks = [], []
    for i in range(n):
        s = _script(**kw)
        s["cover"] = dict(s["cover"], text=f"Story {i}")
        p = pick(s, profile, recent)
        picks.append(p)
        recent.insert(0, _record(p))
    return picks


def test_neighbouring_videos_do_not_repeat_anchor_or_track(profile):
    picks = _run(profile, 30)
    for i, p in enumerate(picks):
        before = picks[max(0, i - RECENT["anchor"]):i]
        assert p.anchor not in {q.anchor for q in before}
        music_before = picks[max(0, i - RECENT["music"]):i]
        assert p.music_track not in {q.music_track for q in music_before}
    assert {p.anchor for p in picks} == set(anchor_ids(CHANNEL))  # everyone gets airtime


def test_the_same_script_and_history_give_the_same_pick(profile):
    s = _script()
    assert pick(s, profile, []).to_dict() == pick(json.loads(json.dumps(s)), profile, []).to_dict()


def test_template_fits_serious_mode(profile):
    assert {p.template for p in _run(profile, 6, serious_mode=True, music={"enabled": False})} == {"night-report"}
    assert {p.template for p in _run(profile, 6)} == {"nes-desk"}  # night-report is for serious mode only


def test_an_experiment_arm_fixes_the_template(profile):
    p = pick(_script(template="night-report"), profile, [])
    assert p.template == "night-report" and "experiment" in p.reasons["template"]


def test_no_fitting_template_falls_back_to_the_default(profile):
    s = _script(music={"enabled": True, "mood": "urgent", "energy": 4})
    prof = dict(profile, format=dict(profile["format"], templates=["night-report"]))
    assert pick(s, prof, []).template == profile["format"]["default_template"]


def test_track_fits_mood_and_licence(profile):
    from studio.music import licensed_for, load_catalog

    catalog = {t["id"]: t for t in load_catalog()}
    platforms = [p for p, c in profile["platforms"].items() if c["enabled"]]
    for p in _run(profile, 10, music={"enabled": True, "mood": "neutral", "energy": 2}):
        track = catalog[p.music_track]
        assert "neutral" in track["tags"]["mood"] and licensed_for(track, platforms)


def test_no_music_no_track(profile):
    assert pick(_script(music={"enabled": False}), profile, []).music_track is None


def test_a_used_up_library_still_picks(profile):
    """Fewer candidates than the window: the least recently used one."""
    prof = dict(profile, format=dict(profile["format"], templates=["nes-desk"]))
    assert {p.template for p in _run(prof, 4)} == {"nes-desk"}


def test_excluded_choices_are_left_out_unless_nothing_remains(profile):
    first = pick(_script(), profile, [])
    again = pick(_script(), profile, [], exclude={"anchor": [first.anchor], "music_track": [first.music_track]})
    assert again.anchor != first.anchor and again.music_track != first.music_track
    only = dict(profile, format=dict(profile["format"], templates=["nes-desk"]))
    assert pick(_script(), only, [], exclude={"template": ["nes-desk"]}).template == "nes-desk"


def test_broken_history_entries_are_ignored(profile):
    recent = [None, "oops", {"anchor": None, "music": "x"}, {"anchor": {"id": 5}}]
    assert pick(_script(), profile, recent).anchor


# --- the cover layout: the title plate at the bottom or the top, by turns ---


def test_the_cover_layout_alternates_from_video_to_video(profile):
    layouts = [p.cover_layout for p in _run(profile, 8)]
    assert all(a != b for a, b in zip(layouts, layouts[1:])), layouts


@pytest.mark.parametrize("record", [lambda layout: {"cover": {"layout": layout}},
                                    lambda layout: {"pick": {"cover_layout": layout}}])
def test_the_cover_layout_is_the_opposite_of_the_newest_recorded_one(profile, record):
    s = _script()
    assert pick(s, profile, [record("bottom")]).cover_layout == "top"
    assert pick(s, profile, [record("top")]).cover_layout == "bottom"
    # videos from before the cover layout (and broken entries) are skipped: the newest one that recorded it counts
    older = [None, {"template": "nes-desk"}, {"pick": {"template": "nes-desk"}, "cover": {"layout": "middle"}}]
    p = pick(s, profile, older + [record("top"), record("top")])
    assert p.cover_layout == "bottom" and "4 video(s) ago" in p.reasons["cover_layout"]
    assert p.to_dict()["cover_layout"] == "bottom"


def test_without_a_recorded_cover_layout_the_script_hash_decides(profile):
    layouts = set()
    for i in range(20):
        s = _script()
        s["cover"] = dict(s["cover"], text=f"Story {i}")
        p = pick(s, profile, [{"template": "nes-desk", "anchor": {"id": "x"}}])
        assert "hash" in p.reasons["cover_layout"]
        assert p.cover_layout == pick(json.loads(json.dumps(s)), profile, []).cover_layout  # the same script, the same one
        layouts.add(p.cover_layout)
    assert layouts == {"bottom", "top"}  # different stories spread over both


def _studio_pick(tmp_path, capsys, recent) -> dict:
    from studio.cli import main

    script = tmp_path / "script.json"
    script.write_text(json.dumps(_script()))
    recent_path = tmp_path / "recent.json"
    recent_path.write_text(json.dumps(recent))
    assert main(["pick", "--script", str(script), "--channel", CHANNEL, "--recent", str(recent_path)]) == 0
    return json.loads(capsys.readouterr().out)


def test_studio_pick_prints_the_cover_layout(tmp_path, capsys):
    printed = _studio_pick(tmp_path, capsys, {"releases": [{"video_package": {"cover": {"layout": "bottom"}}}]})
    assert printed["cover_layout"] == "top" and "'bottom'" in printed["reasons"]["cover_layout"]
    # --recent as a plain list of package.json, newest first: every one counts
    plain = _studio_pick(tmp_path, capsys, [{"cover": {"layout": "top"}}, {"cover": {"layout": "bottom"}}])
    assert plain["cover_layout"] == "bottom"


@pytest.mark.parametrize("status,want", [("changes_requested", "top"), ("rejected", "top"), ("failed", "top"),
                                         ("pending", "bottom"), ("approved", "bottom"), ("published", "bottom")])
def test_the_cover_layout_skips_releases_that_never_reached_the_feed(tmp_path, capsys, status, want):
    """A rebuild after the moderator's "changes requested" finds its own old
    release first in hub releases_recent: alternating against it would give
    two published videos in a row the same layout."""
    hub = {"releases": [{"status": status, "video_package": {"cover": {"layout": "top"}}},
                        {"status": "published", "video_package": {"cover": {"layout": "bottom"}}}]}
    assert _studio_pick(tmp_path, capsys, hub)["cover_layout"] == want


def test_template_anchor_and_track_still_rotate_against_every_release():
    from studio.pick import recent_from_hub

    rework = {"template": "night-report", "cover": {"layout": "top"}}
    packages, in_feed = recent_from_hub({"releases": [{"status": "changes_requested", "video_package": rework},
                                                      {"status": "published", "video_package": {"template": "nes-desk"}},
                                                      "broken"]})
    assert packages == [rework, {"template": "nes-desk"}, None]
    assert in_feed == [None, {"template": "nes-desk"}, None]


# --- a rebuild keeps what its earlier package was made with -----------------------

def _keep(p, archive="releases/x/package.tar.gz"):
    return {"template": p.template, "anchor": p.anchor, "music_track": p.music_track, "cover_layout": p.cover_layout,
            "package_archive": archive}


def _other_choice(values, value):
    return next(v for v in values if v != value)


def test_without_keep_the_pick_says_nothing_new(profile):
    assert "notes" not in pick(_script(), profile, []).to_dict()


def test_a_rebuild_keeps_all_of_it_though_a_fresh_pick_would_differ(profile):
    """A fresh pick after the earlier package reached hub avoids what it used, which is the rework's whole problem."""
    s = _script()
    first = pick(s, profile, [])
    recent = [dict(_record(first), media={"package_archive": "releases/x/package.tar.gz"})]
    assert pick(s, profile, recent).anchor != first.anchor
    again = pick(s, profile, recent, keep=_keep(first))
    assert (again.template, again.anchor, again.music_track, again.music_key, again.cover_layout) == (
        first.template, first.anchor, first.music_track, first.music_key, first.cover_layout)
    assert "notes" not in again.to_dict()
    assert all(again.reasons[k] == "kept from the earlier package" for k in ("template", "anchor", "music_track", "cover_layout"))


def test_an_inherited_exclude_of_another_value_keeps_the_earlier_choice(profile):
    s = _script()
    first = pick(s, profile, [])
    value = first.to_dict()["anchor"]
    other = "some-anchor-rejected-earlier"
    assert other != value
    assert pick(s, profile, [], exclude={"anchor": [other]}, keep=_keep(first)).to_dict()["anchor"] == value


@pytest.mark.parametrize("kind", ["anchor", "template", "music_track"])
def test_a_kind_named_in_exclude_is_picked_again_and_the_others_stay(profile, kind):
    s = _script()
    first = pick(s, profile, [])
    value = first.to_dict()[kind]
    again = pick(s, profile, [], exclude={kind: [value]}, keep=_keep(first)).to_dict()
    if kind != "template":  # demo.json's mood may fit one template only: `_choose` then keeps it, as today
        assert again[kind] != value
    for other in {"anchor", "template", "music_track", "cover_layout"} - {kind}:
        assert again[other] == first.to_dict()[other]
    assert "notes" not in again


def test_a_retired_anchor_is_picked_again_with_a_note(profile, monkeypatch):
    s = _script()
    first = pick(s, profile, [])
    active = [a for a in anchor_ids(CHANNEL) if a != first.anchor]
    monkeypatch.setattr("studio.pick.anchor_ids", lambda channel_id: active)
    again = pick(s, profile, [], keep=_keep(first))
    assert again.anchor in active
    assert len(again.notes) == 1 and f"anchor {first.anchor!r}" in again.notes[0] and again.template == first.template


def test_a_template_that_no_longer_fits_the_script_is_picked_again_with_a_note(profile):
    s = _script()
    keep = dict(_keep(pick(s, profile, [])), template="night-report")  # night-report requires serious mode
    again = pick(s, profile, [], keep=keep)
    assert again.template == "nes-desk" and "template 'night-report'" in again.notes[0]


def test_a_template_the_channel_dropped_is_picked_again(profile):
    s = _script()
    first = pick(s, profile, [])
    keep = dict(_keep(first), template="retired-template")
    assert pick(s, profile, [], keep=keep).template != "retired-template"


def test_an_experiment_arm_wins_over_the_earlier_template(profile):
    s = _script(template="nes-desk")
    keep = dict(_keep(pick(_script(), profile, [])), template="night-report")
    again = pick(s, profile, [], keep=keep)
    assert again.template == "nes-desk" and again.notes


@pytest.mark.parametrize("break_track", [
    lambda t: t.update(status="pending"),  # no longer approved
    lambda t: t["license"].update(platforms=[]),  # no longer licensed for the channel's enabled platforms
    lambda t: t["tags"].update(mood=["other-mood"]),  # no longer in the script's mood
])
def test_a_track_that_no_longer_holds_is_picked_again_with_a_note(profile, break_track):
    from studio.music import load_catalog

    s = _script()
    catalog = load_catalog()
    first = pick(s, profile, [], catalog=catalog)
    assert first.music_track
    catalog = copy.deepcopy(catalog)
    break_track(next(t for t in catalog if t["id"] == first.music_track))
    again = pick(s, profile, [], catalog=catalog, keep=_keep(first))
    assert again.music_track != first.music_track and again.music_key
    assert any(f"track {first.music_track!r}" in n for n in again.notes)


def test_a_track_kept_takes_its_key_from_the_catalog_not_the_old_pick(profile):
    from studio.music import load_catalog

    s = _script()
    catalog = copy.deepcopy(load_catalog())
    first = pick(s, profile, [], catalog=catalog)
    next(t for t in catalog if t["id"] == first.music_track)["audio"]["key"] = "music/tracks/moved.ogg"
    assert pick(s, profile, [], catalog=catalog, keep=_keep(first)).music_key == "music/tracks/moved.ogg"


def test_a_script_that_dropped_its_music_drops_the_track_with_a_note(profile):
    s = _script()
    first = pick(s, profile, [])
    silent = _script(music={"enabled": False})
    again = pick(silent, profile, [], keep=_keep(first))
    assert again.music_track is None and again.music_key is None
    assert any("the script has no music now" in n for n in again.notes)


def test_a_script_that_gained_music_gets_a_track(profile):
    s = _script()
    first = pick(s, profile, [])
    again = pick(s, profile, [], keep=dict(_keep(first), music_track=None))
    assert again.music_track and again.notes


def test_a_video_released_since_takes_the_choice_away(profile):
    s = _script()
    first = pick(s, profile, [])
    neighbour = _record(first)
    neighbour["cover"]["layout"] = first.cover_layout
    again = pick(s, profile, [neighbour], keep=_keep(first))
    assert again.anchor != first.anchor and again.music_track != first.music_track
    assert again.cover_layout != first.cover_layout
    joined = " ".join(again.notes)
    assert f"anchor {first.anchor!r}" in joined and f"track {first.music_track!r}" in joined and "cover layout" in joined
    # only one template fits: nothing to move to, so it stays, as `_choose` would leave it
    assert again.template == first.template and "template" not in joined


def test_the_earlier_package_itself_is_no_neighbour(profile):
    s = _script()
    first = pick(s, profile, [])
    itself = dict(_record(first), media={"package_archive": "releases/x/package.tar.gz"})
    again = pick(s, profile, [itself], keep=_keep(first))
    assert again.anchor == first.anchor and again.cover_layout == first.cover_layout and not again.notes


def test_a_video_that_never_reached_the_feed_is_no_neighbour(profile):
    from studio.pick import recent_from_hub

    s = _script()
    first = pick(s, profile, [])
    packages, in_feed = recent_from_hub({"releases": [{"status": "changes_requested", "video_package": _record(first)}]})
    again = pick(s, profile, packages, cover_recent=in_feed, keep=_keep(first))
    assert again.anchor == first.anchor and not again.notes


def test_a_cover_layout_not_on_record_is_picked_with_a_note(profile):
    s = _script()
    first = pick(s, profile, [])
    again = pick(s, profile, [], keep=dict(_keep(first), cover_layout=None))
    assert again.cover_layout in ("bottom", "top") and "cover layout None" in again.notes[0]


def test_studio_pick_takes_keep_from_a_file(tmp_path, capsys):
    from studio.cli import main

    s = _script()
    p = pick(s, repo.load_channel_profile(CHANNEL), [])
    script_path, keep_path = tmp_path / "s.json", tmp_path / "keep.json"
    script_path.write_text(json.dumps(s))
    keep_path.write_text(json.dumps(dict(_keep(p), anchor="no-such-anchor")))
    assert main(["pick", "--script", str(script_path), "--channel", CHANNEL, "--keep", str(keep_path)]) == 0
    out = json.loads(capsys.readouterr().out)
    assert out["template"] == p.template and "anchor 'no-such-anchor'" in out["notes"][0]
