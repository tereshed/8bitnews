import wave

from studio.tts import ADAPTERS, StubTtsAdapter


def test_adapters_registry_has_stub():
    assert ADAPTERS["stub"] is StubTtsAdapter


def test_stub_word_timing_matches_wpm_with_no_gaps():
    adapter = StubTtsAdapter(wpm=120.0)  # 0.5s/word
    result = adapter.synthesize("one two three", "any-voice")
    assert [w.text for w in result.words] == ["one", "two", "three"]
    assert result.words[0].start_s == 0.0
    assert result.words[0].end_s == 0.5
    assert result.words[1].start_s == 0.5  # no gap between words within a scene
    assert result.words[2].end_s == 1.5


def test_stub_faster_tempo_produces_shorter_words():
    slow = StubTtsAdapter(wpm=120.0).synthesize("one two", "v")
    fast = StubTtsAdapter(wpm=240.0).synthesize("one two", "v")
    assert fast.words[-1].end_s < slow.words[-1].end_s


def test_stub_wav_is_not_digital_silence():
    adapter = StubTtsAdapter(wpm=165.0)
    result = adapter.synthesize("hello", "v")
    with wave.open(str(result.wav_path), "rb") as wf:
        frames = wf.readframes(wf.getnframes())
    assert any(b != 0 for b in frames)  # a real tone, not silence


def test_stub_settings_pace_and_serious_pace():
    from studio.config import tts_settings

    settings = tts_settings("stub")
    assert (settings["wpm"], settings["serious_wpm"]) == (144.0, 131.0)  # format.md, af_sarah pace
    assert StubTtsAdapter.from_settings(settings).wpm == 144.0
    assert StubTtsAdapter.from_settings(settings, serious=True).wpm == 131.0


def test_stub_reports_its_wav_length():
    result = StubTtsAdapter(wpm=120.0).synthesize("one two three", "v")
    with wave.open(str(result.wav_path), "rb") as wf:
        assert result.duration_s == wf.getnframes() / wf.getframerate() == 1.5


def test_registry_lists_kokoro_without_loading_it():
    assert "kokoro" in ADAPTERS and sorted(ADAPTERS) == ["kokoro", "stub"]
