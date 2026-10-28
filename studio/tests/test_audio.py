"""The mix (studio.audio): ducking, loudness, true peak, music level."""
import numpy as np
import pytest

from studio import constants
from studio.audio import (
    SfxEvent,
    decode,
    duck_envelope,
    limit,
    mix,
    speech_intervals,
    voice_over_rest_db,
)
from studio.config import audio_settings
from studio.music import synth_test_track, synth_transition_sound
from studio.tts import StubTtsAdapter

SR = 48000


@pytest.fixture(scope="module")
def voice():
    """A stub voice: 12 words in two phrases with a 1 s gap."""
    from studio.audio import concat_wavs_with_silence

    stub = StubTtsAdapter(wpm=144)
    a, b = stub.synthesize("one two three four five six", "v"), stub.synthesize("seven eight nine ten eleven twelve", "v")
    import tempfile
    from pathlib import Path

    out = Path(tempfile.mkdtemp()) / "voice.wav"
    concat_wavs_with_silence([a.wav_path, b.wav_path], 1.0, out)
    words = [(w.start_s, w.end_s) for w in a.words] + [(w.start_s + a.duration_s + 1.0, w.end_s + a.duration_s + 1.0)
                                                        for w in b.words]
    return {"path": out, "words": words, "duration": a.duration_s * 2 + 1.0}


@pytest.fixture(scope="module")
def track(tmp_path_factory):
    return synth_test_track(tmp_path_factory.mktemp("m") / "loop.wav", bars=2)


def _voice_over_music_db(result, voice):
    """What `music_under_voice` measures, on the master before encoding."""
    return voice_over_rest_db(result.master, decode(voice["path"], SR, channels=1)[:, 0], SR, voice["words"])


def test_speech_intervals_bridge_short_gaps_only():
    assert speech_intervals([(0, 1), (1.2, 2), (3.5, 4)], hold_gap_s=0.8) == [(0, 2), (3.5, 4)]


def test_duck_envelope_dips_ahead_of_the_voice_and_recovers():
    env = duck_envelope(SR * 4, SR, [(1.0, 2.0)], duck_db=-8, attack_s=0.1, release_s=0.3)
    at = lambda t: env[int(t * SR)]  # noqa: E731
    assert at(0.5) == 0 and at(3.5) == 0
    assert at(1.5) == pytest.approx(-8)
    assert -8 < at(0.95) < 0  # already going down before the first word
    assert -8 < at(2.15) < 0


def test_mix_hits_the_loudness_target_under_the_peak_ceiling(voice, track):
    cfg = audio_settings()
    result = mix(voice["path"], voice["duration"], voice["words"], cfg, music_path=track)
    assert result.loudness.lufs == pytest.approx(constants.LOUDNESS_LUFS, abs=0.2)
    assert result.loudness.true_peak_dbtp <= cfg["true_peak_ceiling_dbtp"] + 0.1
    assert len(result.master) == int(round(voice["duration"] * SR))


def test_music_bed_level_changes_voice_over_music(voice, track):
    quiet = mix(voice["path"], voice["duration"], voice["words"], audio_settings(), music_path=track)
    loud = mix(voice["path"], voice["duration"], voice["words"], audio_settings({"music_bed_db": 0.0}), music_path=track)
    q, lo = _voice_over_music_db(quiet, voice), _voice_over_music_db(loud, voice)
    assert q > 15 and lo < q - 8


def test_duck_depth_changes_voice_over_music(voice, track):
    deep = mix(voice["path"], voice["duration"], voice["words"], audio_settings({"duck_db": -12.0}), music_path=track)
    none = mix(voice["path"], voice["duration"], voice["words"], audio_settings({"duck_db": 0.0}), music_path=track)
    assert _voice_over_music_db(deep, voice) > _voice_over_music_db(none, voice) + 10


def test_music_file_changes_the_mix(voice, track, tmp_path):
    other = synth_test_track(tmp_path / "other.wav", bars=1)
    a = mix(voice["path"], voice["duration"], voice["words"], audio_settings(), music_path=track)
    b = mix(voice["path"], voice["duration"], voice["words"], audio_settings(), music_path=other)
    assert np.abs(a.master - b.master).max() > 0.01


def test_without_music_only_the_voice_is_left(voice):
    result = mix(voice["path"], voice["duration"], voice["words"], audio_settings())
    assert _voice_over_music_db(result, voice) > 30


def test_transition_sound_lands_where_placed(voice, tmp_path):
    blip = synth_transition_sound(tmp_path / "blip.wav")
    gap_t = voice["words"][5][1] + 0.4  # inside the 1 s pause between the phrases
    plain = mix(voice["path"], voice["duration"], voice["words"], audio_settings())
    with_sfx = mix(voice["path"], voice["duration"], voice["words"], audio_settings(), sfx=[SfxEvent(gap_t, blip, 0.22)])
    window = slice(int(gap_t * SR), int((gap_t + 0.2) * SR))
    assert np.abs(plain.master[window]).max() < 1e-3
    assert np.abs(with_sfx.master[window]).max() > 0.05


def test_limiter_keeps_length_and_timing():
    t = np.arange(SR) / SR
    x = np.zeros((SR, 2), np.float32)
    x[SR // 2:SR // 2 + 480] = 0.99 * np.sin(2 * np.pi * 1000 * t[:480])[:, None]
    y = limit(x, SR, -6.0)
    assert y.shape == x.shape
    assert np.abs(y).max() <= 10 ** (-6 / 20) + 0.01
    assert np.argmax(np.abs(y[:, 0]) > 0.05) == pytest.approx(SR // 2, abs=SR // 200)  # no shift beyond 5 ms
