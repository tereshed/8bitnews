from studio.pronunciation import build_synthesis_text, regroup_words
from studio.timeline import Word


def test_build_synthesis_text_substitutes_bare_word():
    text, sizes = build_synthesis_text("Garcetti spoke today.", [{"text": "Garcetti", "say": "gar-SET-ee"}])
    assert text == "gar-SET-ee spoke today."
    assert sizes == [1, 1, 1]


def test_build_synthesis_text_preserves_possessive_and_surrounding_punctuation():
    text, sizes = build_synthesis_text(
        "(Garcetti's) plan passed.", [{"text": "Garcetti", "say": "gar-SET-ee"}]
    )
    assert text == "(gar-SET-ee's) plan passed."
    assert sizes == [1, 1, 1]


def test_build_synthesis_text_is_case_sensitive():
    text, sizes = build_synthesis_text("garcetti spoke.", [{"text": "Garcetti", "say": "gar-SET-ee"}])
    assert text == "garcetti spoke."  # lowercase doesn't match the capitalized hint
    assert sizes == [1, 1]


def test_build_synthesis_text_multiword_say_expands_to_several_tokens():
    text, sizes = build_synthesis_text("Xi spoke.", [{"text": "Xi", "say": "SHEE jin ping"}])
    assert text == "SHEE jin ping spoke."
    assert sizes == [3, 1]  # the 3-token hint still maps back to Xi as one original word


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
    from studio.pronunciation import unused_pronunciations

    hints = [
        {"text": "Garcetti", "say": "gar-SET-ee"},
        {"text": "Ninth Circuit", "say": "NINE-th SIR-kit"},
        {"text": "Bumatay", "say": "boo-MAH-tie"},
    ]
    assert unused_pronunciations(["Mayor Garcetti's plan.", "The Ninth Circuit ruled."], hints) == [
        "Ninth Circuit",
        "Bumatay",
    ]
