"""A voice that misses the channel's length: a narrow overrun closes the scene
gaps and keeps the words; anything else stops before pictures are paid for."""
import json

import pytest

from studio.build import build_package, check_narration_length
from studio.errors import BuildError
from studio.timeline import LENGTH_MARGIN_S, MAX_PAUSE_FIT_S, MIN_SCENE_PAUSE_S, fitted_pause


PROFILE = {"format": {"duration_s": {"min": 45, "max": 90}, "words": {"rate": 2.5, "serious_rate": 2.25}}}


@pytest.mark.parametrize("seen_s", [90.12, 90.33])
def test_the_two_real_overruns_fit_without_touching_the_words(seen_s):
    gaps = 11
    spoken = seen_s - gaps * 0.3
    fitted = fitted_pause(spoken, gaps, 0.3, 90)
    assert MIN_SCENE_PAUSE_S <= fitted < 0.3
    assert spoken + gaps * fitted <= 90 - LENGTH_MARGIN_S
    # no more than it takes: a thousandth of a second per gap at most
    assert spoken + gaps * (fitted + 0.002) > 90 - LENGTH_MARGIN_S


@pytest.mark.parametrize("spoken,gaps,why", [
    (80.0, 11, "already fits"),
    (90.0 - LENGTH_MARGIN_S - 11 * 0.3, 11, "exactly at the target"),
    (90.0 - LENGTH_MARGIN_S + MAX_PAUSE_FIT_S + 0.01 - 11 * 0.3, 11, "more than the fit may take"),
    (89.0, 2, "too few gaps: they would get shorter than the floor"),
    (91.0, 0, "no gaps at all"),
])
def test_pause_stays_when(spoken, gaps, why):
    assert fitted_pause(spoken, gaps, 0.3, 90) == 0.3, why


@pytest.mark.parametrize("duration", [45.0, 60.0, 90.0])
def test_a_voice_in_range_passes(duration):
    check_narration_length(duration, PROFILE, False)


def test_too_long_says_how_much_to_cut_and_no_more():
    with pytest.raises(BuildError, match="narration length") as e:
        check_narration_length(94.3, PROFILE, False)
    text = str(e.value)
    assert "94.30s" in text and "about 4.4s" in text and "about 11 words" in text and "no more" in text


def test_a_serious_script_counts_words_at_its_own_rate():
    with pytest.raises(BuildError, match="about 10 words"):
        check_narration_length(94.3, PROFILE, True)


def test_too_short_says_how_much_to_add():
    with pytest.raises(BuildError, match=r"under the channel's 45s\. Add about 2\.1s"):
        check_narration_length(43.0, PROFILE, False)


class NoCalls:
    def generate_image(self, *a, **k):
        pytest.fail("a picture was asked for: the script should have stopped first")

    def check_image(self, *a, **k):
        pytest.fail("a picture was checked: the script should have stopped first")


def _profile_dir(test_channels_dir, tmp_path, low, high):
    """The test channel with another length."""
    import shutil

    import yaml

    out = tmp_path / "channels"
    shutil.copytree(test_channels_dir, out)
    path = out / "8bitnews-us" / "channel.yaml"
    data = yaml.safe_load(path.read_text())
    data["format"]["duration_s"] = {"min": low, "max": high}
    path.write_text(yaml.safe_dump(data, allow_unicode=True))
    return out


def test_a_script_too_long_stops_before_any_picture(short_script_path, test_channels_dir, tmp_path):
    channels = _profile_dir(test_channels_dir, tmp_path, 1, 2)
    with pytest.raises(BuildError, match="narration length"):
        build_package(script_path=short_script_path, channel_id="8bitnews-us", out_dir=tmp_path / "out",
                      channels_dir=channels, tts_name="stub", images="openrouter", image_client=NoCalls())


def test_a_narrow_overrun_builds_with_closed_gaps_and_a_note(short_script_path, test_channels_dir, tmp_path):
    plain = build_package(script_path=short_script_path, channel_id="8bitnews-us", out_dir=tmp_path / "plain",
                          channels_dir=test_channels_dir, tts_name="stub")
    timeline = json.loads((plain.parent / "timeline.json").read_text())
    scenes = timeline["scenes"]
    assert len(scenes) > 2, "the fixture needs gaps to close"
    length = scenes[-1]["end_s"]
    # A maximum the plain video misses by a little: closing the gaps is enough.
    high = round(length - 0.15, 3)
    channels = _profile_dir(test_channels_dir, tmp_path, 1, high)
    fitted = build_package(script_path=short_script_path, channel_id="8bitnews-us", out_dir=tmp_path / "fitted",
                           channels_dir=channels, tts_name="stub")
    after = json.loads(fitted.read_text())
    fitted_scenes = json.loads((fitted.parent / "timeline.json").read_text())["scenes"]
    assert fitted_scenes[-1]["end_s"] <= high - LENGTH_MARGIN_S + 0.01
    assert json.loads((fitted.parent / "script.json").read_text()) == json.loads((plain.parent / "script.json").read_text())
    assert any(n.startswith("scene pauses") and "the words are unchanged" in n for n in after["notes"])
    assert next(c for c in after["validate"]["checks"] if c["id"] == "duration")["ok"]
    # The voice track moved with the timeline: every scene still starts on its own words.
    for scene in fitted_scenes:
        assert not scene["words"] or scene["words"][0]["start_s"] >= scene["start_s"]


def test_a_plain_build_keeps_its_pause_and_adds_no_note(short_script_path, test_channels_dir, tmp_path):
    path = build_package(script_path=short_script_path, channel_id="8bitnews-us", out_dir=tmp_path / "plain",
                         channels_dir=test_channels_dir, tts_name="stub")
    assert not any(n.startswith("scene pauses") for n in json.loads(path.read_text()).get("notes", []))
