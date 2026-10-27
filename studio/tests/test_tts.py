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
