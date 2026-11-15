import pytest

from studio.pronunciation import Group, build_synthesis_text, hinted_tokens, regroup_words, unused_pronunciations
from studio.timeline import Word

ONE = Group(1, 1, False)


def _timed(text: str, step: float = 0.5) -> tuple[Word, ...]:
    """TTS-like timings: each synthesis token `step` seconds long."""
    return tuple(Word(text=t, start_s=k * step, end_s=(k + 1) * step) for k, t in enumerate(text.split()))


def test_build_synthesis_text_substitutes_bare_word():
    text, sizes = build_synthesis_text("Garcetti spoke today.", [{"text": "Garcetti", "say": "gar-SET-ee"}])
    assert text == "gar-SET-ee spoke today."
    assert sizes == [Group(1, 1, True), ONE, ONE]


def test_build_synthesis_text_preserves_possessive_and_surrounding_punctuation():
    text, sizes = build_synthesis_text(
        "(Garcetti's) plan passed.", [{"text": "Garcetti", "say": "gar-SET-ee"}]
    )
    assert text == "(gar-SET-ee's) plan passed."
    assert sizes == [Group(1, 1, True), ONE, ONE]


def test_build_synthesis_text_is_case_sensitive():
    text, sizes = build_synthesis_text("garcetti spoke.", [{"text": "Garcetti", "say": "gar-SET-ee"}])
    assert text == "garcetti spoke."  # lowercase doesn't match the capitalized hint
    assert sizes == [ONE, ONE]


def test_build_synthesis_text_multiword_say_expands_to_several_tokens():
    text, sizes = build_synthesis_text("Xi spoke.", [{"text": "Xi", "say": "SHEE jin ping"}])
    assert text == "SHEE jin ping spoke."
    assert sizes == [Group(1, 3, True), ONE]  # the 3-token hint still maps back to Xi as one original word


def test_regroup_words_collapses_multiword_hint_into_one_original_word():
    narration = "Xi spoke."
    _synth_text, sizes = build_synthesis_text(narration, [{"text": "Xi", "say": "SHEE jin ping"}])
    # Simulate TTS timing for "SHEE jin ping spoke." (4 tokens).
    synth_words = (
        Word(text="SHEE", start_s=0.0, end_s=0.4),
        Word(text="jin", start_s=0.4, end_s=0.8),
        Word(text="ping", start_s=0.8, end_s=1.2),
        Word(text="spoke.", start_s=1.2, end_s=1.6),
    )
    words = regroup_words(narration, sizes, synth_words)
    assert [w.text for w in words] == ["Xi", "spoke."]
    assert words[0].start_s == 0.0
    assert words[0].end_s == 1.2  # spans all 3 sub-tokens of the hint
    assert words[1].start_s == 1.2
    assert words[1].end_s == 1.6


def test_regroup_words_passes_through_unmatched_words_one_to_one():
    narration = "No hints here."
    _text, sizes = build_synthesis_text(narration, [])
    synth_words = (
        Word(text="No", start_s=0.0, end_s=0.3),
        Word(text="hints", start_s=0.3, end_s=0.6),
        Word(text="here.", start_s=0.6, end_s=0.9),
    )
    words = regroup_words(narration, sizes, synth_words)
    assert [(w.text, w.start_s, w.end_s) for w in words] == [
        ("No", 0.0, 0.3),
        ("hints", 0.3, 0.6),
        ("here.", 0.6, 0.9),
    ]


def test_unused_pronunciations_lists_hints_matching_no_word():
    hints = [
        {"text": "Garcetti", "say": "gar-SET-ee"},
        {"text": "Ninth Circuit", "say": "NINE-th SIR-kit"},
        {"text": "Bumatay", "say": "boo-MAH-tie"},
        {"text": "Tenth Circuit", "say": "TENTH SIR-kit"},
    ]
    assert unused_pronunciations(["Mayor Garcetti's plan.", "The Ninth Circuit ruled.", "The Tenth, Circuit."], hints) == [
        "Bumatay",
        "Tenth Circuit",
    ]


# --- hints of several words (ES 12–14.11: «Klaas Knot», «New START», «Deutsche Welle» were never applied) ---


def test_a_phrase_hint_replaces_the_words_in_a_row():
    text, groups = build_synthesis_text(
        "Según «Deutsche Welle», el pacto sigue.", [{"text": "Deutsche Welle", "say": "Dóiche Vele"}]
    )
    assert text == "Según «Dóiche Vele», el pacto sigue."
    assert groups == [ONE, Group(2, 2, True), ONE, ONE, ONE]
    assert hinted_tokens(groups) == frozenset({1, 2})


def test_a_phrase_hint_keeps_the_possessive_and_matches_every_occurrence():
    hints = [{"text": "Klaas Knot", "say": "Klas Knot"}]
    text, groups = build_synthesis_text("Klaas Knot's plan. Then Klaas Knot spoke.", hints)
    assert text == "Klas Knot's plan. Then Klas Knot spoke."
    assert [g.hinted for g in groups] == [True, False, False, True, False]


def test_a_phrase_hint_does_not_match_across_punctuation_or_another_case():
    hints = [{"text": "New START", "say": "Niu Estart"}]
    for narration in ("El tratado New, START expira.", "El tratado New Start expira.", "El tratado New expira."):
        text, groups = build_synthesis_text(narration, hints)
        assert text == narration
        assert all(not g.hinted for g in groups)
        assert unused_pronunciations([narration], hints) == ["New START"]


def test_whitespace_inside_a_hint_text_is_normalized():
    text, _groups = build_synthesis_text("El tratado New START expira.", [{"text": " New  START ", "say": "Niu Estart"}])
    assert text == "El tratado Niu Estart expira."


def test_the_longer_hint_wins_where_hints_overlap():
    hints = [{"text": "Charles", "say": "Charls"}, {"text": "Charles Lamanna", "say": "Charls Lamana"}]
    text, groups = build_synthesis_text("Charles Lamanna y Charles hablan.", hints)
    assert text == "Charls Lamana y Charls hablan."
    assert groups == [Group(2, 2, True), ONE, Group(1, 1, True), ONE]
    assert unused_pronunciations(["Charles Lamanna habla."], hints) == ["Charles"]


def test_regroup_maps_a_phrase_one_to_one_when_the_word_counts_match():
    narration = "Según Deutsche Welle, sí."
    text, groups = build_synthesis_text(narration, [{"text": "Deutsche Welle", "say": "Dóiche Vele"}])
    words = regroup_words(narration, groups, _timed(text))
    assert [(w.text, w.start_s, w.end_s) for w in words] == [
        ("Según", 0.0, 0.5),
        ("Deutsche", 0.5, 1.0),
        ("Welle,", 1.0, 1.5),
        ("sí.", 1.5, 2.0),
    ]


def test_regroup_shares_a_phrase_span_by_word_length_when_the_counts_differ():
    narration = "El AB Cdef ganó."
    text, groups = build_synthesis_text(narration, [{"text": "AB Cdef", "say": "a be ce de efe"}])
    assert groups == [ONE, Group(2, 5, True), ONE]
    words = regroup_words(narration, groups, _timed(text))
    assert [w.text for w in words] == narration.split()
    ab, cdef = words[1], words[2]
    assert (ab.start_s, cdef.end_s) == (0.5, 3.0)  # the 5 hint tokens span 0.5–3.0
    assert ab.end_s == cdef.start_s
    assert ab.end_s - ab.start_s == pytest.approx(2.5 * 2 / 6)  # "AB" is 2 of the 6 letters
    assert words[3].start_s == 3.0


def test_regroup_collapses_a_phrase_respelled_as_one_token():
    narration = "La New START expira."
    text, groups = build_synthesis_text(narration, [{"text": "New START", "say": "Niuestart"}])
    assert text == "La Niuestart expira."
    words = regroup_words(narration, groups, _timed(text))
    assert [w.text for w in words] == ["La", "New", "START", "expira."]
    assert words[1].start_s == 0.5 and words[2].end_s == 1.0 and words[1].end_s == words[2].start_s
