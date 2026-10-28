"""studio.pick: template, anchor and track by code, rotating against the
channel's last videos."""
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
    return {"template": p.template, "anchor": {"id": p.anchor},
            "music": {"enabled": p.music_track is not None, "track_id": p.music_track}}


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
