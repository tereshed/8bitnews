"""Kokoro voice (studio.kokoro). Needs the model files the Docker image
fetches (make studio-image); skipped elsewhere."""
import wave

import numpy as np
import pytest

from studio.errors import BuildError
from studio.kokoro import KokoroTtsAdapter, _ends_sentence, _label_chars, kokoro_available, spoken_forms
from studio.tts import ADAPTERS

needs_model = pytest.mark.skipif(not kokoro_available(), reason="Kokoro model files not installed")


@pytest.fixture(scope="module")
def kokoro():
    if not kokoro_available():
        pytest.skip("Kokoro model files not installed")
    return KokoroTtsAdapter(speed=1.1)


def _samples(path):
    with wave.open(str(path), "rb") as f:
        return np.frombuffer(f.readframes(f.getnframes()), np.int16) / 32768.0, f.getframerate()


def _speech_segments(path, below_peak_db=35.0, hop_s=0.01, bridge_s=0.12):
    """Reference alignment, independent of the adapter: spans where the
    audio's 10 ms RMS is within `below_peak_db` of its loudest frame, gaps
    shorter than `bridge_s` bridged."""
    x, sr = _samples(path)
    hop = int(hop_s * sr)
    rms = np.sqrt((x[: len(x) // hop * hop].reshape(-1, hop) ** 2).mean(axis=1))
    on = 20 * np.log10(rms + 1e-12) > 20 * np.log10(rms.max()) - below_peak_db
    segs = []
    for i, v in enumerate(on):
        if v and (not segs or segs[-1][1] < i - 1):
            segs.append([i, i])
        elif v:
            segs[-1][1] = i
    merged = []
    for a, b in segs:
        if merged and (a - merged[-1][1]) * hop_s < bridge_s:
            merged[-1][1] = b
        else:
            merged.append([a, b])
    return [(a * hop_s, (b + 1) * hop_s) for a, b in merged]


@needs_model
@pytest.mark.parametrize("voice", ["af_sarah", "af_heart"])
def test_word_timings_within_100ms_of_the_audio(kokoro, voice):
    """Words separated by full stops: each word's span lands on its own run
    of speech in the audio, both edges within ±100 ms."""
    result = kokoro.synthesize("Alpha. Bravo. Charlie. Delta.", voice)
    segments = _speech_segments(result.wav_path)
    assert len(segments) == len(result.words) == 4
    for word, (start, end) in zip(result.words, segments):
        assert abs(word.start_s - start) <= 0.1, (word, start)
        assert abs(word.end_s - end) <= 0.1, (word, end)


@needs_model
def test_one_word_per_token_in_order_inside_the_audio(kokoro):
    text = "Outlets like Al Jazeera and The Guardian frame the fight, not a states'-rights dispute, in the same sweep."
    result = kokoro.synthesize(text, "af_sarah")
    assert [w.text for w in result.words] == text.split()
    x, sr = _samples(result.wav_path)
    assert result.duration_s == pytest.approx(len(x) / sr)
    prev = 0.0
    for w in result.words:
        assert prev <= w.start_s < w.end_s <= result.duration_s
        prev = w.end_s


@needs_model
def test_words_espeak_joins_still_get_their_own_time(kokoro):
    # espeak phonemizes "in the" as one word ("ɪnðə"), "not a" as "nˌɑːɾə".
    result = kokoro.synthesize("They met in the hall, not a room.", "af_sarah")
    by_text = {w.text: w for w in result.words}
    for a, b in (("in", "the"), ("not", "a")):
        assert by_text[a].end_s <= by_text[b].start_s
        assert by_text[a].end_s > by_text[a].start_s and by_text[b].end_s > by_text[b].start_s


def test_label_chars_splits_a_joined_word_by_its_parts():
    labels = _label_chars(list("ɪnðə"), ["ɪn", "ðə"])
    assert labels == [0, 0, 1, 1]


@needs_model
def test_sentence_pause_is_inserted(kokoro):
    result = kokoro.synthesize("Alpha. Bravo.", "af_sarah")
    a, b = result.words
    assert b.start_s - a.end_s >= kokoro.sentence_pause_s - 0.01


@needs_model
def test_word_timings_within_100ms_of_the_audio_with_numbers_and_titles(kokoro):
    """Same reference on words a respelling, a number and a title expand."""
    result = kokoro.synthesize("Representative. eh-SAY-lee. Sixty-one. 61.3. Tuesday.", "af_sarah", frozenset({1}))
    segments = _speech_segments(result.wav_path)
    assert len(segments) == len(result.words) == 5
    for word, (start, end) in zip(result.words, segments):
        assert abs(word.start_s - start) <= 0.1 and abs(word.end_s - end) <= 0.1, (word, start, end)


@needs_model
def test_connected_speech_times_the_right_words(kokoro):
    """No pauses to check edges against, so check attribution: long and
    short words alternate, and a timing shifted by even one word would give
    the short words the long durations."""
    text = "I congratulate a contemporaneous on unbelievably by extraordinary me intercontinental."
    words = kokoro.synthesize(text, "af_sarah").words
    durations = [w.end_s - w.start_s for w in words]
    for k in range(0, len(words) - 1, 2):
        short, long_ = durations[k], durations[k + 1]
        assert long_ > 2 * short, (words[k], words[k + 1])


@needs_model
def test_decimals_are_read_whole_and_keep_later_words_in_place(kokoro):
    result = kokoro.synthesize("Turnout was 61.3 percent. Inflation hit 2.4. The court ruled on Monday.", "af_sarah")
    by_text = {w.text: w for w in result.words}
    assert by_text["61.3"].end_s - by_text["61.3"].start_s > 0.6  # "sixty one point three", not "sixty one"
    assert by_text["percent."].end_s - by_text["percent."].start_s > 0.25
    assert by_text["The"].start_s - by_text["2.4."].end_s >= kokoro.sentence_pause_s - 0.01


def test_spoken_forms_write_out_what_espeak_misreads():
    tokens = "It cost $1.5 million, $20 and $1 at 3:30 or 9:00 for a 2-1 vote; Rep. Lee, No. 5, on Oct. 27.".split()
    assert spoken_forms(tokens) == [
        "It", "cost", "1 point 5", "million dollars,", "20 dollars", "and", "1 dollar", "at", "3 30", "or",
        "9 o'clock", "for", "a", "2 to 1", "vote;", "Representative", "Lee,", "Number", "5,", "on", "October", "27.",
    ]


def test_numbers_at_a_sentence_end_are_written_out_too():
    assert spoken_forms("It cost $5 million.".split())[2:] == ["5", "million dollars."]
    assert spoken_forms("It cost $5.".split())[2:] == ["5 dollars."]
    assert spoken_forms("at 3:30.".split())[1:] == ["3 30."]
    assert spoken_forms("a 2-1.".split())[1:] == ["2 to 1."]
    assert spoken_forms(["(Garcetti's)."]) == ["(Garcetti's)."]


def test_decimals_cents_and_minutes_are_read_digit_by_digit_or_as_cents():
    assert spoken_forms(["3.14"]) == ["3 point 1 4"]
    assert spoken_forms(["$1.00", "$2.50,", "$0.99", "$1,200.50."]) == [
        "1 dollar", "2 dollars and 50 cents,", "99 cents", "1,200 dollars and 50 cents."]
    assert spoken_forms(["$2.5", "billion"]) == ["2 point 5", "billion dollars"]
    assert spoken_forms(["10:05", "10:30"]) == ["10 oh 5", "10 30"]


def test_abbreviations_with_a_possessive_are_still_read_out():
    assert spoken_forms(["Rep.'s", "Gov.'s", "U.S.'s", "Apple", "Inc."]) == [
        "Representative's", "Governor's", "US's", "Apple", "Incorporated"]


def test_titles_and_initialisms_do_not_end_a_sentence():
    assert not _ends_sentence("Rep.", "Lee") and not _ends_sentence("Oct.", "27") and not _ends_sentence("Inc.", "rose")
    assert not _ends_sentence("U.S.", "Attorney") and _ends_sentence("U.S.", None)
    assert _ends_sentence("court.", "The") and _ends_sentence("floor.'", "He") and _ends_sentence("why?", "No")
    assert not _ends_sentence("No.", "5") and _ends_sentence("No.", "The")  # "Did he win? No. The vote failed."


def test_marks_inside_closing_quotes_count():
    from studio.kokoro import _clause_mark

    assert _ends_sentence('why?"', "He") and _ends_sentence("end.)", "He")
    assert _clause_mark('not,"') and _clause_mark("this;") and _clause_mark("—")
    assert not _clause_mark("plain") and not _ends_sentence("plain", "next")  # no mark is no pause


def _pauses(tokens, spans, length):
    from types import SimpleNamespace

    from studio.kokoro import KokoroTtsAdapter

    settings = SimpleNamespace(sentence_pause_s=0.25, clause_pause_s=0.1)
    audio = np.full(length, 0.5, dtype=np.float32)
    return KokoroTtsAdapter._insert_pauses(settings, audio, tokens, spans)


def test_sentence_pause_is_spliced_even_where_the_model_ran_on():
    audio, spans = _pauses(["One.", "Two"], [(0, 2400), (2400, 4800)], 4800)
    assert spans[1][0] - spans[0][1] == 6000  # 0.25 s at 24 kHz
    assert len(audio) == 4800 + 6000
    assert abs(audio[2399]) < 0.01 and abs(audio[spans[1][0]]) < 0.01  # faded, no click


def test_comma_pause_only_where_the_model_left_a_gap():
    _, ran_on = _pauses(["One,", "two"], [(0, 2400), (2400, 4800)], 4800)
    assert ran_on[1][0] - ran_on[0][1] == 0  # a comma the model ran through stays run through
    _, gapped = _pauses(["One,", "two"], [(0, 2400), (2400 + 720, 4800)], 4800)
    assert gapped[1][0] - gapped[0][1] == 2400  # a 30 ms gap topped up to 0.1 s


def test_a_dash_token_gives_the_word_before_it_a_pause():
    _, spans = _pauses(["said", "—", "then"], [(0, 2400), None, (2400 + 720, 4800)], 4800)
    assert spans[2][0] - spans[0][1] == 2400


@needs_model
def test_no_pause_is_spliced_between_words_without_a_mark(kokoro):
    result = kokoro.synthesize("The county refused to open the password protected drive on Tuesday morning",
                               "af_sarah")
    x, sr = _samples(result.wav_path)
    inner = x[int(0.1 * sr):-int(0.2 * sr)]
    zero = inner == 0
    longest, run = 0, 0
    for z in zero:
        run = run + 1 if z else 0
        longest = max(longest, run)
    assert longest < 0.01 * sr  # no stretch of digital silence: nothing was spliced in


@needs_model
def test_titles_and_initialisms_get_no_sentence_pause(kokoro):
    words = kokoro.synthesize("First Assistant U.S. Attorney said so. Rep. Mark agreed.", "af_sarah").words
    by_text = {w.text: w for w in words}
    assert by_text["Attorney"].start_s - by_text["U.S."].end_s < kokoro.sentence_pause_s
    assert by_text["Mark"].start_s - by_text["Rep."].end_s < kokoro.sentence_pause_s
    assert by_text["Rep."].start_s - by_text["so."].end_s >= kokoro.sentence_pause_s - 0.01


@needs_model
def test_only_hint_words_are_read_as_respellings(kokoro):
    assert kokoro._phonemize.respell("eh-SAY-lee") == "ɛˈseɪliː"  # not S-A-Y spelled out
    assert kokoro._phonemize.respell("duh-SOHL-nyer") == "dəˈsoʊlnjɚ"  # not "nigher"
    assert kokoro._phonemize.respell("gar-SET-ee's,") == "ɡɑːɹˈsɛtiːz,"
    plain, _ = kokoro._phonemes(["US-led"])
    hinted, _ = kokoro._phonemes(["US-led"], frozenset({0}))
    assert plain == kokoro._phonemize("US-led") and hinted != plain
    # by position: the same word elsewhere in the narration stays espeak's
    both, ref = kokoro._phonemes(["PIK-sel", "PIK-sel"], frozenset({0}))
    assert ref[0] == "ˈpɪksɛl" and ref[1] != ref[0]


@needs_model
def test_hints_keep_their_place_across_chunks(kokoro, monkeypatch):
    """A scene over 510 phonemes is read in chunks; the hint's index must
    land on the same token inside its chunk, and the same word elsewhere
    stays unhinted."""
    sentence = "The county refused to open the password protected drive on Tuesday morning."
    tokens = " ".join([sentence] * 7 + ["Then PIK-sel spoke and PIK-sel left."]).split()
    first = tokens.index("PIK-sel")
    calls = []
    real = type(kokoro)._phonemes

    def spy(self, chunk_tokens, hinted_idx=frozenset()):
        calls.append((list(chunk_tokens), set(hinted_idx)))
        return real(self, chunk_tokens, hinted_idx)

    monkeypatch.setattr(type(kokoro), "_phonemes", spy)
    kokoro.synthesize(" ".join(tokens), "af_sarah", frozenset({first}))
    final = [(t, h) for t, h in calls if t[-6:] == tokens[-6:]]
    assert final and all(len(t) < len(tokens) for t, _ in final)  # really chunked
    for chunk_tokens, hinted in final:
        assert hinted == {len(chunk_tokens) - 6 + 1}  # "Then [PIK-sel] spoke and PIK-sel left."


@needs_model
def test_a_word_espeak_loses_fails_the_build(kokoro, monkeypatch):
    """A sentence whose phonemes come back without one of its words must
    not shift every later word's timing silently."""
    real = kokoro._phonemize.__class__.__call__

    def drop_last_word(self, text):
        out = real(self, text)
        return " ".join(out.split()[:-1]) if len(text.split()) > 3 else out

    monkeypatch.setattr(kokoro._phonemize.__class__, "__call__", drop_last_word)
    with pytest.raises(BuildError, match="went missing"):
        kokoro.synthesize("The court ruled on Monday morning.", "af_sarah")


@needs_model
def test_voice_changes_the_audio(kokoro):
    a, _ = _samples(kokoro.synthesize("A hearing is set for Tuesday.", "af_sarah").wav_path)
    b, _ = _samples(kokoro.synthesize("A hearing is set for Tuesday.", "af_heart").wav_path)
    assert len(a) != len(b) or np.abs(a - b).max() > 0.01


@needs_model
def test_speed_changes_the_duration():
    text = "A hearing is set for Tuesday, October twenty-seventh."
    slow = KokoroTtsAdapter(speed=1.0).synthesize(text, "af_sarah")
    fast = KokoroTtsAdapter(speed=1.3).synthesize(text, "af_sarah")
    assert fast.duration_s < slow.duration_s * 0.9


@needs_model
def test_settings_pick_the_serious_speed():
    settings = {"speed": 1.1, "serious_speed": 1.0}
    assert ADAPTERS["kokoro"].from_settings(settings, serious=False).speed == 1.1
    assert ADAPTERS["kokoro"].from_settings(settings, serious=True).speed == 1.0


@needs_model
def test_same_text_same_audio(kokoro):
    a = kokoro.synthesize("Follow for what happens next.", "af_sarah")
    b = kokoro.synthesize("Follow for what happens next.", "af_sarah")
    assert a.words == b.words
    assert _samples(a.wav_path)[0].tobytes() == _samples(b.wav_path)[0].tobytes()


@needs_model
def test_unknown_voice_is_a_readable_error(kokoro):
    from studio.errors import BuildError

    with pytest.raises(BuildError, match="not in the image"):
        kokoro.synthesize("Hello.", "zz_nobody")


@needs_model
def test_scene_longer_than_one_pass_is_split_at_sentences(kokoro):
    sentence = "The county refused to open the password protected drive on Tuesday morning."
    text = " ".join([sentence] * 8)  # well over Kokoro's 510 phonemes
    phonemes, _ = kokoro._phonemes(text.split())
    assert len(phonemes) > 510
    result = kokoro.synthesize(text, "af_sarah")
    assert [w.text for w in result.words] == text.split()
    prev = 0.0
    for w in result.words:
        assert prev <= w.start_s < w.end_s <= result.duration_s
        prev = w.end_s
    ends = [w for w in result.words if w.text == "morning."]
    nexts = [result.words[result.words.index(w) + 1] for w in ends[:-1]]
    assert all(n.start_s - e.end_s >= kokoro.sentence_pause_s - 0.01 for e, n in zip(ends, nexts))
