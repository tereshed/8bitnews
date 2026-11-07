"""studio.pick: template, anchor and track by code, rotating against the
channel's last videos; the cover layout alternating with the last one."""
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
