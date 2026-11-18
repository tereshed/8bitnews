"""The owner's music_off test (studio.experiments): half of the scripts with
music, by the hash of their dossier, get no music in `studio pick` and
`studio build`, and the arm is recorded."""
import copy
import datetime
import json
import shutil

import jsonschema
import pytest
import yaml

from conftest import FIXTURES_DIR, music_script
from studio import repo
from studio.build import build_package
from studio.experiments import music_arm
from studio.pick import pick

CHANNEL = "8bitnews-us"
TODAY = datetime.date(2026, 11, 20)
WIDE = {"enabled": True, "music_off": {"from": "2000-01-01", "until": "2999-12-31"}}


@pytest.fixture
def profile():
    p = repo.load_channel_profile(CHANNEL)
    return dict(p, experiments=copy.deepcopy(WIDE))


def _script(dossier, **kw):
    s = json.loads((FIXTURES_DIR / "scripts" / "demo.json").read_text())
    s.pop("template", None)
    s["music"] = {"enabled": True, "mood": "neutral", "energy": 2, "cue": "Soft bed under the whole piece."}
    s["dossier"] = dossier
    s.update(kw)
    return s


def _dossier(profile, arm):
    """A dossier id the test puts in `arm`."""
    return next(d for d in (f"dossier-{i}" for i in range(100)) if music_arm(_script(d), profile)["arm"] == arm)


def test_scripts_split_about_half_and_half(profile):
    arms = [music_arm(_script(f"dossier-{i}"), profile)["arm"] for i in range(400)]
    assert 160 <= arms.count("off") <= 240


def test_the_arm_follows_the_dossier_not_the_script_text(profile):
    """A reworked script of the same story stays in its arm."""
    d = _dossier(profile, "off")
    s = _script(d)
    reworked = copy.deepcopy(s)
    reworked["scenes"][0]["narration"] += " Otra frase."
    reworked["cover"] = dict(reworked["cover"], text="Otro titular")
    assert music_arm(s, profile) == music_arm(reworked, profile) == {"id": "music_off", "arm": "off"}


@pytest.mark.parametrize("experiments", [
    {"enabled": False, "music_off": WIDE["music_off"]},
    {"enabled": True},
    {"enabled": True, "music_off": {"from": "2026-11-21", "until": "2026-12-02"}},
    {"enabled": True, "music_off": {"from": "2026-11-01", "until": "2026-11-19"}},
])
def test_no_test_running_means_no_arm(profile, experiments):
    p = dict(profile, experiments=experiments)
    assert music_arm(_script("dossier-1"), p, today=TODAY) is None


def test_the_dates_are_inclusive(profile):
    p = dict(profile, experiments={"enabled": True, "music_off": {"from": "2026-11-20", "until": "2026-11-20"}})
    assert music_arm(_script("dossier-1"), p, today=TODAY) is not None


def test_a_script_without_music_is_not_in_the_test(profile):
    s = _script("dossier-1", serious_mode=True, music={"enabled": False})
    assert music_arm(s, profile) is None
    assert "experiment" not in pick(s, profile, []).to_dict()


def test_pick_leaves_the_track_out_only_in_the_arm_off(profile):
    off = pick(_script(_dossier(profile, "off")), profile, [])
    on = pick(_script(_dossier(profile, "on")), profile, [])
    assert off.music_track is None and off.music_key is None
    assert "music_off" in off.reasons["music_track"]
    assert off.to_dict()["experiment"] == {"id": "music_off", "arm": "off"}
    assert on.music_track is not None
    assert on.to_dict()["experiment"] == {"id": "music_off", "arm": "on"}


def test_a_rebuild_of_an_arm_off_story_drops_the_earlier_track(profile):
    """A video built with music before the test started, rebuilt during it, follows its arm."""
    s = _script(_dossier(profile, "off"))
    before = pick(s, dict(profile, experiments={"enabled": False}), [])
    assert before.music_track
    keep = {"template": before.template, "anchor": before.anchor, "music_track": before.music_track,
            "cover_layout": before.cover_layout, "package_archive": "releases/x/package.tar.gz"}
    again = pick(s, profile, [], keep=keep)
    assert again.music_track is None
    assert again.anchor == before.anchor
    assert any("music_off" in n for n in again.notes)
    assert again.to_dict()["experiment"]["arm"] == "off"


def test_the_pick_with_an_arm_fits_the_package_contract(profile):
    schema = repo.load_contract_schema("video_package.schema.json")
    pick_schema = dict(schema["properties"]["pick"], **{"$defs": schema["$defs"]})
    jsonschema.validate(pick(_script(_dossier(profile, "off")), profile, []).to_dict(), pick_schema)


def test_build_in_the_arm_off_has_no_music_whatever_it_is_given(tmp_path, test_track):
    channels = tmp_path / "channels"
    shutil.copytree(FIXTURES_DIR / "channels", channels)
    profile_path = channels / CHANNEL / "channel.yaml"
    profile = yaml.safe_load(profile_path.read_text())
    profile["experiments"] = copy.deepcopy(WIDE)
    profile_path.write_text(yaml.safe_dump(profile, allow_unicode=True))
    script_path = music_script(tmp_path)
    script = json.loads(script_path.read_text())
    script["dossier"] = next(f"dossier-{i}" for i in range(100)
                             if music_arm(dict(script, dossier=f"dossier-{i}"), profile)["arm"] == "off")
    script_path.write_text(json.dumps(script, indent=2))

    package_path = build_package(script_path=script_path, channel_id=CHANNEL, out_dir=tmp_path / "out",
                                 channels_dir=channels, tts_name="stub", music_file=test_track)
    package = json.loads(package_path.read_text())
    assert package["music"] == {"enabled": False}
    assert package["experiment"] == {"id": "music_off", "arm": "off"}
    assert "music_stem" not in package["files"] and "video_no_music" not in package["files"]
    assert any("music_off" in n for n in package["notes"])
    assert any("--music-file/--music-track ignored" in n for n in package["notes"])
