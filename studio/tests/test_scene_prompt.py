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
    assert all(m.lower() == "plain light grey " for m in re.findall(r"(\S+ \S+ \S+ )(?=flag)", out, flags=re.I)), out
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
    # the screen shows the picture the rest of the visual describes (trial 2, S1)
    ("A wide dusk view of a U.S. federal courthouse, broad marble steps and tall columns, empty of people. "
     "The screen behind the anchor shows this same courthouse image.",
     "A wide dusk view of a U.S. federal courthouse, broad marble steps and tall columns, empty of people"),
    ("The anchor sits at the desk; a quiet harbor at dawn; the screen shows the same view.", "a quiet harbor at dawn"),
    ("A senator at a podium, his hand raised. The screen behind the anchor shows this same image.",
     "A senator at a podium, his hand raised"),
    ("The anchor at the desk. Calm, serious delivery. A harbor at dawn. Camera slowly pushes in. "
     "The screen behind the anchor shows that picture.", "A harbor at dawn"),
    # nothing else describes it: the reference is all there is
    ("The anchor sits at the desk; the screen shows this image.", "this image"),
    # a description that merely starts with "the" is still a description
    ("Anchor at the desk; the screen shows the image of a courthouse at night.", "the image of a courthouse at night"),
    ("Anchor at the desk; the screen shows the satellite view.", "the satellite view"),
    ("The anchor at the desk. Calm, serious delivery. The screen behind the anchor shows the same courthouse from "
     "the opening, now at night.", "the same courthouse from the opening, now at night"),
    ("Anchor at the desk; the screen shows the same image: a courthouse at dusk.", "the same image: a courthouse at dusk"),
])
def test_screen_content_is_what_the_anchor_scene_puts_on_the_screen(visual, expected):
    from studio.scene_prompt import screen_content
    assert screen_content(visual) == expected


@pytest.mark.parametrize("visual,want", [
    ("A plain federal office building facade with a flagpole out front, empty of people.", "a flagpole flying a plain light grey flag"),
    ("A flag staff by the gate.", "A flagpole flying a plain light grey flag"),
    ("A flag-pole by the door.", "A flagpole flying a plain light grey flag"),
    ("Three flagpoles outside the hall.", "flagpoles flying plain light grey flags"),
    # a flag the visual names itself is the one: plain, and only one
    ("A flagpole flying an American flag.", "A flagpole flying a plain light grey flag"),
    ("A flag at half-mast on a flagpole.", "A plain light grey flag at half-mast on a flagpole"),
])
def test_a_flagpole_flies_one_plain_flag(visual, want):
    """Trial 2, S5: a bare flagpole got an American flag every time, and no picture passed the check."""
    out = clean_visual(visual, [])
    assert want in out and out.lower().count("plain light grey") == 1 and "American" not in out, out


@pytest.mark.parametrize("visual", [
    "No flags, poles, stands, or tall or conical objects anywhere in the room",
    "NO flags anywhere",
    "a bare room with no flags or seals",
    "no logos, seals or flags",
    "commanders with no insignia on caps or uniforms",
    "a podium, not a flag, in front of the wall",
    "Neither flags nor emblems on the walls",
    "a wall with no logos",
    "a room with no flagpoles",
    "a flagpole with no flag",
    "flagpoles without flags outside the embassy",
    # lists and one-word qualifiers after the negation
    "No poles, stands or flags anywhere in the room.",
    "No stands, poles, flags or tall objects.",
    "a room with no visible flags",
    "no posters or flags on the walls",
    "no foreign flags",
    "no tall or conical flags",
    "commanders with no military insignia on caps",
    "no campaign logos",
    # a pole the visual leaves empty stays empty
    "a flagpole out front, no flag on it",
    "An embassy gate, a bare flagpole, no flag, no people present.",
])
def test_what_the_visual_rules_out_stays_ruled_out(visual):
    """Trial 3, S3: "No flags, poles, stands, or tall or conical objects" lost its "No" to the flag pattern
    ("No" read as a flag's name, like "California") and asked the model for a flag, poles and cones."""
    out = clean_visual(visual, [])
    assert out == visual, out


@pytest.mark.parametrize("visual,want", [
    ("No American flags anywhere in the room.", "No flags anywhere in the room."),
    ("NO AMERICAN FLAGS.", "NO flags."),
    ("No state flags", "No flags"),
    ("Without American flags, the hall looks bare.", "Without flags, the hall looks bare."),
    ("No Great Seal on the lectern.", "No seal on the lectern."),
    ("a lectern without any American flag", "a lectern without any flag"),
    ("commanders with no Iranian flag behind them", "commanders with no flag behind them"),
    ("Not the American flag but a plain banner.", "Not the flag but a plain banner."),
    ("a flagpole by the door, no American flag", "a flagpole by the door, no flag"),
])
def test_a_ruled_out_symbol_loses_its_name_and_stays_ruled_out(visual, want):
    """No name reaches the model, and no plain flag is described to a scene that has none."""
    assert clean_visual(visual, []) == want


@pytest.mark.parametrize("visual,want", [
    # the "no" belongs to something else: a comma, a conjunction, a preposition or a verb ends it
    ("A room with no people, a California flag on the wall.", "no people, a plain light grey flag on the wall"),
    ("No people; a flag by the door.", "a plain light grey flag by the door"),
    ("An empty podium with no people and an American flag beside it.", "no people and a plain light grey flag"),
    ("An empty lectern with no people beside the US flag.", "beside the plain light grey flag"),
    ("protesters with no signs wave Mexican flags", "wave plain light grey flag"),
    ("protesters with no signs wave flags", "wave plain light grey flag"),
    ("a man with no hat salutes the American flag", "salutes the plain light grey flag"),
    ("no longer a California flag", "no longer a plain light grey flag"),
    ("not far from the Texas flag", "not far from the plain light grey flag"),
    ("no people and flags", "no people and plain light grey flag"),
    ("Not far away, the State Seal.", "the plain round shape"),
    ("no flag\nthe Great Seal on the podium", "the plain round shape on the podium"),
    # names that start with a negation are names
    ("the No Kings flag", "the plain light grey flag"),
    ("No Kings protesters wave American flags", "wave plain light grey flag"),
    ("No Kings protesters wave flags", "wave plain light grey flag"),
    ("the No Labels logo on a banner", "the plain round shape on a banner"),
    ("Doctors Without Borders logo", "plain round shape"),
    ("Never Trump flag", "plain light grey flag"),
])
def test_a_symbol_the_visual_asks_for_is_still_made_plain(visual, want):
    out = clean_visual(visual, [])
    assert want in out, out
    # "No Kings protesters" stay protesters: only the symbol loses its name
    for name in ("California", "American", "US", "Mexican", "Texas", "Seal", "Labels", "Borders", "Trump"):
        assert name not in out, out


def test_a_flagpole_flies_a_plain_flag_when_the_flag_ruled_out_is_elsewhere():
    out = clean_visual("No flags in the room; outside, a flagpole.", [])
    assert out == "No flags in the room; outside, a flagpole flying a plain light grey flag.", out


def test_the_trial_3_command_room_keeps_its_prohibitions():
    visual = ("Iran's supreme leader stands before a row of senior military commanders in a bare, plain command room, "
              "only a wall map silhouette of the Gulf region behind them. No flags, poles, stands, or tall or conical "
              "objects anywhere in the room; no insignia on caps or uniforms; no text visible.")
    assert clean_visual(visual, []) == visual


def test_only_a_scene_with_a_flag_hears_about_flags():
    """Trial 3: "(a plain light grey flag is fine)" in every prompt put grey flags rolled on stands into rooms
    that named none (S3), and QA and Legal read them as missiles next to a retaliation order. Now the prompt
    describes a flag only to a scene that has one, and asks for it hanging open."""
    from studio.paths import TEMPLATES_DIR
    from studio.scene_images import PLAIN_FLAG_LOOK, load_style, scene_prompt
    from studio.template_pkg import load_template
    style = load_style("8bitnews-us")
    template = load_template(TEMPLATES_DIR / "nes-desk")
    assert "grey flag" not in style["prohibitions"] and "no flag at all unless the scene names one" in style["prohibitions"]
    without = scene_prompt(style, template, clean_visual("A bare command room. No flags or poles.", []))
    with_flag = scene_prompt(style, template, clean_visual("A lectern with a small American flag beside it.", []))
    assert "grey flag" not in without and PLAIN_FLAG_LOOK not in without
    assert PLAIN_FLAG_LOOK in with_flag and "American" not in with_flag



@pytest.mark.parametrize("visual", [
    "A talk-radio studio desk with an on-air microphone and a lit red ON AIR lamp above it, an empty chair facing it.",
    "A studio with an on-air sign glowing.",
    "A red On-Air light over the door.",
    # the script Legal approved, word for word
    "A talk-radio studio desk with an on-air microphone and a lit red on-air signal lamp above it, an empty chair facing it.",
])
def test_an_on_air_lamp_is_a_plain_lamp(visual):
    """Trial 2, S6: the lamp came back lettered "ON AIR" every time."""
    out = clean_visual(visual, [])
    assert "plain red lamp" in out and "air lamp" not in out.lower() and "air sign" not in out.lower() and "air light" not in out.lower(), out
