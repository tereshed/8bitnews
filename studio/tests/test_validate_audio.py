"""`studio validate` audio checks (E2a-3): each one passes on a fresh
build and fails, with its own note, on a package broken in its one way."""
import json
import shutil
import subprocess
from pathlib import Path

import numpy as np
import pytest
import yaml

from conftest import FIXTURES_DIR, build_with_music
from studio.audio import decode, write_audio
from studio.build import build_package
from studio.channel_assets import load_anchor
from studio.checks import CHECKS, run_checks
from studio.config import tts_settings
from studio.kokoro import kokoro_available

PROFILE = yaml.safe_load((FIXTURES_DIR / "channels" / "8bitnews-us" / "channel.yaml").read_text())
SR = 48000


def _check(package_dir: Path, check_id: str) -> dict:
    package = json.loads((package_dir / "package.json").read_text())
    return CHECKS[check_id](package_dir, package, PROFILE)


def _replace_audio(video: Path, samples: np.ndarray) -> None:
    """Puts `samples` (48 kHz stereo) under video's own picture."""
    wav = video.with_name("_new.wav")
    out = video.with_name("_new.mp4")
    write_audio(wav, samples, SR)
    subprocess.run(["ffmpeg", "-y", "-v", "error", "-i", str(video), "-i", str(wav), "-map", "0:v:0", "-map", "1:a:0",
                    "-c:v", "copy", "-c:a", "aac", "-b:a", "192k", "-ar", str(SR), str(out)], check=True)
    out.replace(video)
    wav.unlink()


def _edit_audio(name: str, fn):
    def corrupt(d: Path, package: dict) -> None:
        path = d / package["files"][name]["path"]
        _replace_audio(path, fn(decode(path, SR)))
    return corrupt


# --- passing on fresh builds ---------------------------------------------------------

def test_music_build_passes_every_check(music_package, short_youtube_limit):
    package = music_package["package"]
    assert package["validate"]["passed"], [c for c in package["validate"]["checks"] if not c["ok"]]
    assert package["music"]["enabled"] and "video_no_music" in package["files"]
    assert {"voice_stem", "music_stem"} <= set(package["files"])
    rerun = run_checks(music_package["out_dir"], package, PROFILE)
    assert rerun["passed"], [c for c in rerun["checks"] if not c["ok"]]


def test_no_music_build_needs_no_music_checks(built_package):
    assert _check(built_package["out_dir"], "music_under_voice")["note"] == "no music"
    assert _check(built_package["out_dir"], "video_no_music")["ok"]


def test_every_audio_check_reports_a_value(music_package, short_youtube_limit):
    for check_id in ("loudness", "true_peak", "silence", "music_under_voice", "video_no_music"):
        result = _check(music_package["out_dir"], check_id)
        assert result["ok"] and "not implemented" not in result["note"], result


# --- loudness / true_peak / silence ------------------------------------------------

def test_loudness_fails_on_a_quiet_video(music_package, corrupt_copy, short_youtube_limit):
    d = corrupt_copy(music_package, _edit_audio("video", lambda x: x * 10 ** (-8 / 20)))
    result = _check(d, "loudness")
    assert not result["ok"] and "video.mp4" in result["note"] and "LUFS" in result["note"]


def test_loudness_also_covers_video_no_music(music_package, corrupt_copy, short_youtube_limit):
    d = corrupt_copy(music_package, _edit_audio("video_no_music", lambda x: x * 10 ** (-8 / 20)))
    result = _check(d, "loudness")
    assert not result["ok"] and "video_no_music.mp4" in result["note"]


def test_true_peak_fails_on_a_click(music_package, corrupt_copy, short_youtube_limit):
    def click(x):
        x = x.copy()
        x[SR * 5:SR * 5 + 24] = 0.999 * np.sign(np.sin(np.arange(24) * 1.3))[:, None]
        return x

    d = corrupt_copy(music_package, _edit_audio("video", click))
    result = _check(d, "true_peak")
    assert not result["ok"] and "true peak" in result["note"]
    assert _check(d, "loudness")["ok"]  # one click doesn't move the loudness


def test_silence_fails_on_a_dropout(music_package, corrupt_copy, short_youtube_limit):
    def dropout(x):
        x = x.copy()
        x[SR * 4:int(SR * 6.2)] = 0
        return x

    d = corrupt_copy(music_package, _edit_audio("video", dropout))
    result = _check(d, "silence")
    assert not result["ok"] and "silent from" in result["note"]


# --- music_under_voice ---------------------------------------------------------------

def test_loud_music_fails_music_under_voice(tmp_path, test_track, short_youtube_limit):
    """The negative build: the music bed at the voice's level and no ducking."""
    pkg = build_with_music(tmp_path, test_track, audio_overrides={"music_bed_db": 3.0, "duck_db": 0.0})
    result = next(c for c in pkg["package"]["validate"]["checks"] if c["id"] == "music_under_voice")
    assert not result["ok"] and "under the voice" in result["note"]
    assert not pkg["package"]["validate"]["passed"]


def test_music_made_louder_in_the_shipped_file_fails(music_package, corrupt_copy, test_track, short_youtube_limit):
    """Measured on video.mp4 itself: music pushed up after the mix (the
    stem left as it was) fails."""
    def louder_music(x):
        music = decode(test_track, SR, channels=2, loop_to_s=len(x) / SR)[: len(x)]
        return x + music * (np.sqrt(np.mean(x ** 2)) / np.sqrt(np.mean(music ** 2)))

    d = corrupt_copy(music_package, _edit_audio("video", louder_music))
    result = _check(d, "music_under_voice")
    assert not result["ok"] and "under the voice" in result["note"]


def test_a_voice_stem_that_is_not_the_voice_fails(music_package, corrupt_copy, short_youtube_limit):
    def noise_stem(d: Path, package: dict) -> None:
        path = d / package["files"]["voice_stem"]["path"]
        n = len(decode(path, 24000, channels=1))
        write_audio(path, np.random.RandomState(1).uniform(-0.3, 0.3, (n, 1)), 24000, codec="flac")

    result = _check(corrupt_copy(music_package, noise_stem), "music_under_voice")
    assert not result["ok"]


def test_music_under_voice_needs_the_voice_stem(music_package, corrupt_copy, short_youtube_limit):
    def drop_stem(d: Path, package: dict) -> None:
        (d / package["files"].pop("voice_stem")["path"]).unlink()

    result = _check(corrupt_copy(music_package, drop_stem), "music_under_voice")
    assert not result["ok"] and "voice_stem" in result["note"]


# --- video_no_music ------------------------------------------------------------------

def _music_gain_in(d: Path, package: dict) -> float:
    """The gain the music stem has in video.mp4 (the master gain)."""
    from studio.audio import fit

    voice = decode(d / package["files"]["voice_stem"]["path"], SR, channels=1)[:, 0]
    music = decode(d / package["files"]["music_stem"]["path"], SR, channels=2)[:, 0]
    full = decode(d / package["files"]["video"]["path"], SR)[:, 0]
    n = min(len(voice), len(music), len(full))
    (_, gain), _ = fit(full[:n], [voice[:n], music[:n]], np.ones(n, dtype=bool))
    return gain


def _drop_no_music(d: Path, package: dict) -> None:
    (d / package["files"].pop("video_no_music")["path"]).unlink()


def test_long_video_with_music_needs_video_no_music(music_package, corrupt_copy, short_youtube_limit):
    result = _check(corrupt_copy(music_package, _drop_no_music), "video_no_music")
    assert not result["ok"] and "required" in result["note"]


def test_over_60s_with_music_needs_video_no_music_at_the_real_limit(music_package, corrupt_copy):
    def long_without_cut(d: Path, package: dict) -> None:
        _drop_no_music(d, package)
        package["duration_s"] = 61.0

    result = _check(corrupt_copy(music_package, long_without_cut), "video_no_music")
    assert not result["ok"] and "over 60s" in result["note"]


def test_video_no_music_with_music_in_it_fails(music_package, corrupt_copy, short_youtube_limit):
    def music_in_cut(d: Path, package: dict) -> None:
        shutil.copyfile(d / package["files"]["video"]["path"], d / package["files"]["video_no_music"]["path"])

    result = _check(corrupt_copy(music_package, music_in_cut), "video_no_music")
    assert not result["ok"] and "there is music in it" in result["note"]


def test_video_no_music_with_quiet_music_still_fails(music_package, corrupt_copy, test_track, short_youtube_limit):
    """The music at a tenth of its level in video.mp4 (20 dB down: faint,
    still enough for a Content ID match) is found by its shape."""
    def quiet_stem_music(d: Path, package: dict) -> None:
        cut = d / package["files"]["video_no_music"]["path"]
        music = decode(d / package["files"]["music_stem"]["path"], SR, channels=2)
        x = decode(cut, SR)
        n = min(len(x), len(music))
        _replace_audio(cut, x[:n] + music[:n] * _music_gain_in(d, package) * 0.1)

    result = _check(corrupt_copy(music_package, quiet_stem_music), "video_no_music")
    assert not result["ok"] and "there is music in it" in result["note"]


def test_video_no_music_with_another_track_fails(music_package, corrupt_copy, test_track, short_youtube_limit):
    def other_music(x):
        # The test loop played backwards: music, but not the stem's.
        music = decode(test_track, SR, channels=2, loop_to_s=len(x) / SR)[: len(x)][::-1]
        return x + music * 0.3 * np.sqrt(np.mean(x ** 2)) / np.sqrt(np.mean(music ** 2))

    result = _check(corrupt_copy(music_package, _edit_audio("video_no_music", other_music)), "video_no_music")
    assert not result["ok"] and "there is music in it" in result["note"]
    assert result["value"]["other_rise_db"] > result["expected"]["max_other_rise_db"]  # unexplained by the stems


def test_video_no_music_with_another_picture_fails(music_package, corrupt_copy, short_youtube_limit):
    def other_picture(d: Path, package: dict) -> None:
        path = d / package["files"]["video_no_music"]["path"]
        tmp = path.with_name("_flip.mp4")
        subprocess.run(["ffmpeg", "-y", "-v", "error", "-i", str(path), "-vf", "hflip", "-c:a", "copy", str(tmp)],
                       check=True)
        tmp.replace(path)

    result = _check(corrupt_copy(music_package, other_picture), "video_no_music")
    assert not result["ok"] and "same picture" in result["note"]


# --- serious mode, sound effects ----------------------------------------------------

def test_transition_sounds_play_with_their_transition_before_the_voice(built_package):
    timeline = json.loads((built_package["out_dir"] / "timeline.json").read_text())
    scenes = {s["scene_id"]: s for s in timeline["scenes"]}
    # nes-desk alternates cut / pixel-wipe; only pixel-wipe has a sound.
    assert len(timeline["sfx"]) == 2
    for event, scene in zip(timeline["sfx"], ("S2", "S4")):
        assert event["sound"] == "assets/sfx/pixel-wipe.wav"
        assert event["t_s"] == scenes[scene]["start_s"]  # with the wipe, as the scene comes in
        assert scenes[scene]["words"][0]["start_s"] >= event["t_s"] + event["duration_s"] - 1e-6  # voice after it
    # a scene without a sound keeps its voice at its start
    assert scenes["S3"]["words"][0]["start_s"] == pytest.approx(scenes["S3"]["start_s"])


def test_the_voice_track_waits_out_the_transition_sound(built_package):
    """The silence is really in the voice, not only in timeline.json: the
    voice stem is silent from the scene start to its first word."""
    out = built_package["out_dir"]
    timeline = json.loads((out / "timeline.json").read_text())
    voice = decode(out / built_package["package"]["files"]["voice_stem"]["path"], SR, channels=1)[:, 0]
    for scene in (s for s in timeline["scenes"] if s["scene_id"] in ("S2", "S4")):
        start, first = scene["start_s"], scene["words"][0]["start_s"]
        assert np.abs(voice[int(start * SR):int((first - 0.005) * SR)]).max() < 1e-4
        assert np.sqrt(np.mean(voice[int(first * SR):int((first + 0.1) * SR)] ** 2)) > 0.01


def test_music_panned_or_out_of_phase_is_still_measured(music_package, corrupt_copy, test_track, short_youtube_limit):
    """Music the two channels cancel in a mono sum still counts."""
    def out_of_phase(x):
        music = decode(test_track, SR, channels=1, loop_to_s=len(x) / SR)[: len(x), 0]
        level = np.sqrt(np.mean(x ** 2)) / np.sqrt(np.mean(music ** 2))
        return x + np.stack([music, -music], axis=1) * level

    result = _check(corrupt_copy(music_package, _edit_audio("video", out_of_phase)), "music_under_voice")
    assert not result["ok"] and "under the voice" in result["note"]


def test_serious_mode_has_no_transition_sounds_and_reads_slower(built_package, tmp_path):
    script = json.loads((FIXTURES_DIR / "scripts" / "short.json").read_text())
    script["serious_mode"] = True
    script["serious_reason"] = "Test fixture: exercises the serious-mode sound rules."
    path = tmp_path / "serious.json"
    path.write_text(json.dumps(script))
    package_path = build_package(script_path=path, channel_id="8bitnews-us", out_dir=tmp_path / "out",
                                 channels_dir=FIXTURES_DIR / "channels", tts_name="stub")
    package = json.loads(package_path.read_text())
    timeline = json.loads((tmp_path / "out" / "timeline.json").read_text())
    assert timeline["sfx"] == []
    assert any("transition sounds were left out" in n for n in package["notes"])
    assert package["duration_s"] > built_package["package"]["duration_s"] * 1.05  # 131 vs 144 wpm
    assert package["validate"]["passed"], [c for c in package["validate"]["checks"] if not c["ok"]]


# --- the real voice end to end ------------------------------------------------------

@pytest.fixture(scope="module")
def kokoro_music_package(tmp_path_factory, test_track):
    if not kokoro_available():
        pytest.skip("Kokoro model files not installed")
    tmp_path = tmp_path_factory.mktemp("kokoro-music")
    profile = dict(PROFILE)
    profile["format"] = {**PROFILE["format"], "voice": {"provider": "kokoro"}}
    channels = tmp_path / "channels" / "8bitnews-us"
    channels.mkdir(parents=True)
    (channels / "channel.yaml").write_text(yaml.safe_dump(profile))
    return build_with_music(tmp_path, test_track, channels_dir=tmp_path / "channels", tts_name=None)


def test_kokoro_build_with_music_passes_validate(kokoro_music_package, short_youtube_limit):
    """The real voice through the whole mix: ducking, the limiter, the
    YouTube cut, every check."""
    pkg = kokoro_music_package
    package = pkg["package"]
    assert "video_no_music" in package["files"]
    voice_id = package["voice"]["voice_id"]
    assert voice_id == load_anchor(package["channel_id"], package["anchor"]["id"]).voice_id
    assert package["voice"]["provider"] == "kokoro"
    assert package["voice"]["speed"] == tts_settings("kokoro")["voices"][voice_id]["speed"]
    timeline = json.loads((pkg["out_dir"] / "timeline.json").read_text())
    words = sum(len(s["words"]) for s in timeline["scenes"])
    span = sum(s["words"][-1]["end_s"] - s["words"][0]["start_s"] for s in timeline["scenes"])
    assert package["voice"]["wpm"] == pytest.approx(words * 60 / span)
    assert package["validate"]["passed"], [c for c in package["validate"]["checks"] if not c["ok"]]


def test_another_track_in_the_no_music_cut_fails_with_the_real_voice(kokoro_music_package, corrupt_copy, test_track,
                                                                     short_youtube_limit):
    """A track that isn't the stem, 16 dB under the voice: the limiter's
    error on a real voice hides it from the fit, the voice's pauses don't."""
    def other_music(x):
        music = decode(test_track, SR, channels=2, loop_to_s=len(x) / SR)[: len(x)][::-1]
        return x + music * 10 ** (-16 / 20) * np.sqrt(np.mean(x ** 2)) / np.sqrt(np.mean(music ** 2))

    result = _check(corrupt_copy(kokoro_music_package, _edit_audio("video_no_music", other_music)), "video_no_music")
    assert not result["ok"] and "there is music in it" in result["note"]
    assert result["value"]["pause_db"] > result["expected"]["max_pause_db"]


# --- the music parameters through `studio build` ------------------------------------

def _tiny_build(tmp_path: Path, name: str, **build_kwargs) -> dict:
    """short.json cut to a few words a scene (about 6 s): enough to
    compare soundtracks."""
    music = build_kwargs.pop("music", {})
    script = json.loads((FIXTURES_DIR / "scripts" / "short.json").read_text())
    for scene, words in zip(script["scenes"], ("Pixels report.", "Garcetti's plan passed.", "Vote delayed twice.",
                                               "Follow for more.")):
        scene["narration"] = words
    script["music"] = {"enabled": True, "mood": "neutral", "energy": 2, "cue": "Soft bed under the whole piece.", **music}
    path = tmp_path / f"{name}.json"
    path.write_text(json.dumps(script))
    out = tmp_path / name
    package_path = build_package(script_path=path, channel_id="8bitnews-us", out_dir=out,
                                 channels_dir=FIXTURES_DIR / "channels", tts_name="stub", **build_kwargs)
    return {"package": json.loads(package_path.read_text()), "audio": decode(out / "video.mp4", SR)}


def _catalog(tmp_path: Path) -> Path:
    import hashlib

    from studio.music import synth_test_track

    tracks = []
    for tid, mood, bars in (("calm-a", "neutral", 1), ("tense-a", "tense", 3)):
        audio = synth_test_track(tmp_path / "music" / f"{tid}.wav", bars=bars)
        tracks.append({
            "id": tid, "status": "approved",
            "license": {"name": "CC0-1.0", "commercial": True, "platforms": ["youtube", "tiktok", "instagram"]},
            "audio": {"key": f"music/{tid}.wav", "sha256": hashlib.sha256(audio.read_bytes()).hexdigest()},
            "tags": {"mood": [mood], "energy": 2},
        })
    path = tmp_path / "catalog.yaml"
    path.write_text(yaml.safe_dump({"tracks": tracks}))
    return path


def test_music_file_and_script_mood_change_the_built_soundtrack(tmp_path, test_track):
    catalog = _catalog(tmp_path)
    by_file = _tiny_build(tmp_path, "file", music_file=test_track)
    calm = _tiny_build(tmp_path, "calm", music_catalog=catalog, music_dir=tmp_path)
    tense = _tiny_build(tmp_path, "tense", music={"mood": "tense"}, music_catalog=catalog, music_dir=tmp_path)

    assert by_file["package"]["music"]["track_id"] == "file:track.wav"
    assert (calm["package"]["music"]["track_id"], calm["package"]["music"]["license"]) == ("calm-a", "CC0-1.0")
    assert tense["package"]["music"]["track_id"] == "tense-a"

    def differ(a, b):
        n = min(len(a["audio"]), len(b["audio"]))
        return float(np.abs(a["audio"][:n] - b["audio"][:n]).max()) > 0.01

    assert differ(by_file, calm) and differ(calm, tense)
