from studio.subtitles import SubtitleCue, split_cues, wrap_words
from studio.timeline import Word


def test_wrap_words_respects_max_chars():
    words = "the quick brown fox jumps over the lazy dog and keeps running".split()
    lines = wrap_words(words, max_chars=10)
    assert all(len(line) <= 10 for line in lines)
    assert " ".join(lines).split() == words  # no words dropped or reordered


def test_wrap_words_hard_breaks_overlong_word():
    lines = wrap_words(["supercalifragilisticexpialidocious"], max_chars=10)
    assert all(len(line) <= 10 for line in lines)
    assert "".join(lines) == "supercalifragilisticexpialidocious"


def _words(texts: list[str], word_dur: float = 1.0) -> list[Word]:
    return [Word(text=t, start_s=i * word_dur, end_s=(i + 1) * word_dur) for i, t in enumerate(texts)]


def test_split_cues_empty_words_is_empty():
    assert split_cues([]) == []


def test_split_cues_respects_line_and_char_limits():
    words = _words(
        "DOJ sent letters to California counties demanding they remove more than "
        "twenty thousand likely noncitizens from the voter rolls".split()
    )
    cues = split_cues(words, max_lines=2, max_chars=32)
    assert cues  # non-empty input must produce at least one cue
    for cue in cues:
        assert len(cue.lines) <= 2
        assert all(len(line) <= 32 for line in cue.lines)


def test_split_cues_fixed_phrase_gives_the_expected_split():
    """A deterministic, hand-checked split at max_lines=1, max_chars=10:
    'Hi there.' barely fits one 10-char line and ends in a full stop, so the
    cue is cut right there; 'Bye now.' is the rest."""
    words = _words(["Hi", "there.", "Bye", "now."])
    cues = split_cues(words, max_lines=1, max_chars=10)
    assert cues == [
        SubtitleCue(lines=("Hi there.",), start_s=0.0, end_s=2.0),
        SubtitleCue(lines=("Bye now.",), start_s=2.0, end_s=4.0),
    ]


def test_split_cues_cuts_after_last_sentence_punctuation_not_at_the_limit():
    """All 4 words fit the line/char budget together (so the maximal greedy
    group is all of them), but 'Stop.' ends in a full stop, so the cue is
    cut right there instead of running to the fit limit."""
    words = _words(["Stop.", "Then", "go", "somewhere"], word_dur=1.0)
    cues = split_cues(words, max_lines=2, max_chars=40)
    assert cues[0].lines == ("Stop.",)
    assert cues[0].end_s == 1.0
    assert cues[1].lines == ("Then go somewhere",)


def test_split_cues_comma_before_third_word_is_not_a_cut_point():
    """format.md: a comma only counts as a cut point from the 3rd word on,
    so an early aside like 'Well, actually' doesn't fragment into one word."""
    words = _words(["Well,", "actually,", "it", "changed,", "yes"], word_dur=1.0)
    cues = split_cues(words, max_lines=2, max_chars=40)
    # The comma after "changed," is the 4th word (>= 3rd) and is the only
    # valid cut point found within the first maximal group.
    assert cues[0].lines[0].startswith("Well, actually, it changed,")


def test_split_cues_holds_a_short_gap_to_the_next_cue():
    """If the next cue starts less than 0.3s after this one ends, this cue's
    end is extended to the next cue's start (no flicker between cues)."""
    words = [
        Word(text="One.", start_s=0.0, end_s=1.0),
        Word(text="Two.", start_s=1.2, end_s=2.2),
    ]
    cues = split_cues(words, max_lines=1, max_chars=3)  # force each word into its own cue
    assert cues[0].end_s == 1.2  # held to the next cue's start, not left at 1.0
    assert cues[1].start_s == 1.2
