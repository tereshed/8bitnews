"""studio.scene_prompt: what the image model is asked to draw."""
import json
import re

import pytest

from studio.scene_prompt import clean_visual, fold, leaked_names, name_words, strip_names

PEOPLE = ["Shirley Weber", "Judge Mark Bennett", "Gavin Newsom", "Mark DeSaulnier", "Bill Essayli"]


@pytest.mark.parametrize("people,visual", [
    (["Shirley Weber"], "Shirley Weber signs a letter"),
    (["Shirley Weber"], "Secretary Weber signs a letter"),
    (["Shirley Weber"], "Secretary of State Shirley Weber signs a letter"),
    (["Shirley Weber"], "SHIRLEY WEBER signs a letter"),
    (["Shirley Weber"], "weber signs a letter"),
    (["Shirley Weber"], "Weber's aide signs a letter"),
    (["Judge Mark Bennett", "Mark DeSaulnier"], "Judge Bennett and Rep. DeSaulnier sign a letter"),
    (["Gavin Newsom"], "a Newsom figure signs a letter"),
    (["Donald Trump"], "Donald J. Trump waves"),
    (["Kamala D. Harris"], "Kamala Harris speaks"),
    (["Gavin Newsom"], "Gov. Gavin C. Newsom signs"),
    (["Attorney General Rob Bonta"], "Rob Bonta speaks"),
    (["U.S. Attorney Bill Essayli"], "Bill Essayli at a podium, then Bill waves"),
    (["Vice President JD Vance"], "JD Vance waves"),
    (["Xi Jinping"], "President Xi shakes hands"),
    (["King Charles III"], "King Charles at the palace"),
    (["Pope Leo XIV"], "Pope Leo blesses a crowd"),
    (["Pedro Sánchez"], "Pedro Sanchez speaks"),
    (["Pedro Sanchez"], "Pedro Sánchez speaks"),
    (["Beto O'Rourke"], "O’Rourke smiles"),
    (["Alexandria Ocasio-Cortez"], "Ocasio-Cortez, then Cortez, then Alexandria waves"),
    (["William \"Bill\" Clinton"], "Bill Clinton waves"),
])
def test_names_never_reach_the_prompt(people, visual):
    out = clean_visual(visual, people)
    assert leaked_names(out, people) == [], out
    folded = fold(out).lower()
    for name in people:
        for word in name_words(name):
            for piece in [word] + word.split("-"):
                assert piece.lower() not in folded.replace("a person", ""), (visual, out)
    assert "a person" in out


def test_leaked_names_finds_what_cleaning_would_take():
    assert leaked_names("Secretary Weber signs", ["Shirley Weber"]) == ["Weber (Shirley Weber)"]
    assert leaked_names("Shirley waves", ["Shirley Weber"]) == ["Shirley (Shirley Weber)"]
    assert leaked_names("a check mark", ["Mark Bennett"]) == []  # a lowercase first name is a word


def test_a_name_inside_another_word_is_left_alone():
    assert strip_names("a webers-style bracket", ["Shirley Weber"]) == "a webers-style bracket"
    assert strip_names("the Webernet tower", ["Shirley Weber"]) == "the Webernet tower"
    assert clean_visual("World War III museum", ["King Charles III"]) == "World War III museum"


@pytest.mark.parametrize("visual", [
    "mayors stand", "marshals wait", "janitors sweep", "junior staffers sit", "they may talk",
    "a keynote speaker", "banknotes on a table", "the clerk noted it", "extra lifeguards", "a seal rests on a rock",
    "officers seal the door", "a half-mast flag",
])
def test_ordinary_words_survive(visual):
    out = clean_visual(visual, [])
    for word in visual.split():
        if word not in ("flag",):
            assert word in out, (visual, out)


def test_no_real_people_means_no_change():
    visual = "A courthouse door stays shut as the door light flips."
    assert clean_visual(visual, []) == visual


def test_names_used_as_labels_and_quoted_words_go():
    out = clean_visual("three monitor windows labeled Bennett, Bumatay and Sanchez glow; an envelope marked 'DOJ' "
                       "lands; a blank 'no ruling yet' card", ["Judge Mark Bennett", "Patrick Bumatay", "Gabriel Sanchez"])
    assert "DOJ" not in out and "no ruling" not in out and "labeled" not in out
    assert "three monitor windows glow" in out


@pytest.mark.parametrize("visual,expected", [
    ("a wall calendar flips from Oct 13 to Oct 15, circling the deadline", "a blank calendar page flips, circling the deadline"),
    ("a calendar page flips toward Oct. 27 under a stamp", "a blank calendar page flips under a stamp"),
    ("a calendar circled on October 27th, 2026 spins", "a blank calendar page circled spins"),
    ("a 48-hour clock icon counts down", "a clock icon counts down"),
    ("a list of 20,000 names and a 61.3% chart", "a list of names and a chart"),
    ("three monitors and a May 5 hearing", "three monitors and a hearing"),
])
def test_dates_and_numbers_go(visual, expected):
    assert clean_visual(visual, []) == expected


def test_things_the_model_writes_on_are_blank():
    out = clean_visual("a USB drive with a handwritten password note; small newspaper icons", [])
    assert out == "a USB drive with a blank note; small newspaper with blank grey columns icons"


def test_possessive_apostrophes_are_not_quotes():
    assert clean_visual("the county's desk and the clerk's chair", []) == "the county's desk and the clerk's chair"


@pytest.mark.parametrize("motif", ["a question mark", "a question-mark stamp", "a ?-block", "a mystery box",
                                   "a power-up", "a GAME OVER screen", "a warp pipe", "a 1-UP", "question blocks",
                                   "a question box", "Tetris pieces", "Pac-Man ghosts"])
def test_game_motifs_become_neutral(motif):
    out = clean_visual(f"Above the map hovers {motif}.", [])
    for word in ("question", "?", "mystery", "power", "game over", "pipe", "1-up", "tetris", "pac"):
        assert word not in out.lower(), out


@pytest.mark.parametrize("visual", [
    "Newsom at a podium with a California flag beside a state seal and the DOJ logo",
    "an American Flag, A Flag, FLAG and the Great Seal",
    "the seal of the State of California and a coat of arms",
])
def test_flags_become_plain_and_seals_logos_go(visual):
    out = clean_visual(visual, ["Gavin Newsom"])
    assert "seal" not in out.lower() and "logo" not in out.lower() and "arms" not in out.lower()
    assert all(m.lower() == "plain one-color " for m in re.findall(r"(\S+ \S+ )(?=flag)", out, flags=re.I)), out
    assert "California" not in out and "American" not in out and "Great" not in out


def test_every_trial_visual_cleans_to_something():
    """Every scene of every trial script, cleaned of everyone in that script."""
    from studio.repo import repo_root
    for path in (repo_root() / "newsroom" / "trials").rglob("script*.json"):
        script = json.loads(path.read_text())
        everyone = sorted({p for s in script.get("scenes", []) for p in s.get("real_people", [])})
        for scene in script.get("scenes", []):
            out = clean_visual(scene["visual"], everyone)
            assert len(out) > 20 and leaked_names(out, everyone) == [], (path.name, scene["id"], out)


@pytest.mark.parametrize("visual,expected", [
    ("The anchor at the studio desk; behind them, a screen shows a courthouse icon as a glass slides across a ledger.",
     "a courthouse icon as a glass slides across a ledger"),
    ("Host at the desk; the screen behind her lights up with a courthouse icon that pulses once.",
     "a courthouse icon that pulses once"),
    ("Anchor at the studio desk; behind her a stylized California map pulses with an alert icon.",
     "a stylized California map pulses with an alert icon"),
    ("The anchor at the studio desk, looking toward camera.", None),
    # abbreviations don't end the sentence
    ("Anchor at the desk; the screen behind the anchor shows a magnifying glass over the U.S. map of districts.",
     "a magnifying glass over the U.S. map of districts"),
    ("Anchor at the desk; the screen behind the anchor shows Gov. Newsom at a podium.", "Gov. Newsom at a podium"),
    # the studio itself is not what the screen shows
    ("Anchor at the desk; behind them, the studio wall glows blue.", None),
    ("Anchor at the desk; behind the anchor, the newsroom lights dim.", None),
    # a phone's screen is not the studio screen
    ("A phone whose screen shows a text alert; behind them a courthouse.", "a courthouse"),
    # what the anchor does is not on the screen
    ("Anchor at the desk; the screen behind the anchor shows a courthouse as the anchor leans forward.", "a courthouse"),
    ("Host at the desk; the screen behind her shows a text card fading in as she delivers the closing line.",
     "a text card fading in"),
    ("Behind the anchor: a ballot box.", "a ballot box"),
    ("Anchor at the desk; behind the anchor, a screen with a map of Ohio.", "a map of Ohio"),
    ("Anchor at the desk; the big screen: a map of Texas counties.", "a map of Texas counties"),
    ("Anchor at the desk. Screen shows a stack of ballots.", "a stack of ballots"),
])
def test_screen_content_is_what_the_anchor_scene_puts_on_the_screen(visual, expected):
    from studio.scene_prompt import screen_content
    assert screen_content(visual) == expected


@pytest.mark.parametrize("visual,want", [
    ("A plain federal office building facade with a flagpole out front, empty of people.", "a flagpole flying a plain one-color flag"),
    ("A flag staff by the gate.", "A flagpole flying a plain one-color flag"),
    ("A flag-pole by the door.", "A flagpole flying a plain one-color flag"),
    ("Three flagpoles outside the hall.", "flagpoles flying plain one-color flags"),
    # a flag the visual names itself is the one: plain, and only one
    ("A flagpole flying an American flag.", "A flagpole flying a plain one-color flag"),
    ("A flag at half-mast on a flagpole.", "A plain one-color flag at half-mast on a flagpole"),
])
def test_a_flagpole_flies_one_plain_flag(visual, want):
    """Trial 2, S5: a bare flagpole got an American flag every time, and no picture passed the check."""
    out = clean_visual(visual, [])
    assert want in out and out.lower().count("plain one-color") == 1 and "American" not in out, out
