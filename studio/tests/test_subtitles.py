from studio.subtitles import split_subtitles, wrap_words


def test_wrap_words_respects_max_chars():
    words = "the quick brown fox jumps over the lazy dog and keeps running".split()
    lines = wrap_words(words, max_chars=10)
    assert all(len(line) <= 10 for line in lines)
    assert " ".join(lines).split() == words  # no words dropped or reordered


def test_wrap_words_hard_breaks_overlong_word():
    lines = wrap_words(["supercalifragilisticexpialidocious"], max_chars=10)
    assert all(len(line) <= 10 for line in lines)
    assert "".join(lines) == "supercalifragilisticexpialidocious"


def test_split_subtitles_respects_line_and_char_limits():
    narration = (
        "DOJ sent letters to California counties demanding they remove more than "
        "twenty thousand likely noncitizens from the voter rolls within forty eight hours"
    )
    cues = split_subtitles(narration, start_s=0.0, end_s=10.0, max_lines=2, max_chars=32)
    assert cues  # non-empty narration must produce at least one cue
    for cue in cues:
        assert len(cue.lines) <= 2
        assert all(len(line) <= 32 for line in cue.lines)


def test_split_subtitles_covers_full_time_range_in_order():
    narration = "Sources are in the description below this video for anyone who wants to check them"
    start, end = 2.0, 9.5
    cues = split_subtitles(narration, start, end)
    assert cues[0].start_s == start
    assert cues[-1].end_s == end
    for a, b in zip(cues, cues[1:]):
        assert a.end_s == b.start_s  # cues tile the range with no gaps or overlaps


def test_split_subtitles_empty_narration_is_empty():
    assert split_subtitles("", 0.0, 1.0) == []
