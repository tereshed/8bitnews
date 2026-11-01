"""The image prompt of a scene (docs/architecture.md §12): the channel's
NES pixel-art style, the template's own look, and the scene's `visual`
cleaned of what the picture must not show.

`clean_visual` is deterministic and has its own tests:
  - real people never reach the image model by name. Every person in the
    script's `real_people` (all scenes: a scene can mention someone it
    doesn't show) is reduced to the parts of their name -- titles of any
    length ("Secretary of State", "U.S. Attorney"), initials and suffixes
    dropped, accents and curly apostrophes folded -- and the visual loses
    the full name (with any middle names or initials), the surname in any
    case (so Tom Price takes "the price of eggs" with him: a leak is worse
    than a clumsy scene), and every other part when capitalized (a lowercase
    "mark" stays for Mark Bennett), each with a title in front and a
    possessive behind; they become "a person". Nicknames and initialisms not
    in `real_people` ("AOC") are not known; `leaked_names` checks the
    finished prompt the same way, and the studio refuses to send one that
    fails it;
  - quoted words and what something is "labeled"/"marked" with are dropped,
    and so are dates and numbers ("a calendar flips from Oct 13 to Oct 15",
    "a 48-hour clock"): the model draws them as text, and no redraw talks it
    out of that -- dates and figures belong on the plates; calendars, notes
    and newspapers are asked for blank;
  - motifs of known games (a question mark, a "?" block, power-ups, "GAME
    OVER", Tetris, Pac-Man) become neutral ones (format.md forbids them);
  - flags become plain light grey flags (a half-mast flag stays possible), and
    a flagpole flies one;
    seals of a state or office, coats of arms, emblems and logos become plain
    shapes (a seal on a rock stays a seal).
Every prompt then ends with the same prohibitions, whatever the visual says.
"""
from __future__ import annotations

import re
import unicodedata

PERSON = "a person"

TITLES = (
    "President", "Vice President", "President-elect", "Governor", "Gov.", "Lieutenant Governor", "Lt. Gov.",
    "Senator", "Sen.", "Representative", "Rep.", "Congressman", "Congresswoman", "Delegate", "Secretary", "Sec.",
    "Attorney General", "AG", "U.S. Attorney", "US Attorney", "District Attorney", "DA", "Solicitor General",
    "Judge", "Justice", "Chief Justice", "Mayor", "Registrar", "Clerk", "Sheriff", "Commissioner", "Chair",
    "Chairman", "Chairwoman", "Speaker", "Leader", "Minister", "Prime Minister", "Premier", "Chancellor", "Pope",
    "King", "Queen", "Prince", "Princess", "Emperor", "Sir", "Dame", "Lord", "Lady", "General", "Gen.", "Col.",
    "Maj.", "Capt.", "Lt.", "Sgt.", "Adm.", "Officer", "Detective", "Agent", "Director", "Ambassador", "Rev.",
    "Dr.", "Mr.", "Mrs.", "Ms.", "Mx.", "Prof.",
)
SUFFIXES = {"jr", "sr", "ii", "iii", "iv", "v", "phd", "md", "esq"}
_TITLE = (r"(?:" + "|".join(re.escape(t) for t in sorted(TITLES, key=len, reverse=True)) + r")"
          r"(?:\s+of\s+(?:the\s+)?[A-Z][\w.'-]*)?")
_TITLES_BEFORE = rf"(?:(?i:{_TITLE})\s+)*"
_POSSESSIVE = r"(?P<pos>'s|s')?"

GAME_MOTIFS = (
    (r"question[- ]?marks?(?:[- ](?:stamps?|blocks?|icons?|signs?|box(?:es)?|cubes?))?", "blank card"),
    (r"question[- ](?:blocks?|box(?:es)?|cubes?)", "blank card"),
    (r"\?[- ]?(?:blocks?|box(?:es)?)", "blank card"),
    (r"mystery (?:blocks?|box(?:es)?)", "blank card"),
    (r"power[- ]?ups?", "glow"),
    (r"1[- ]?ups?|extra li(?:fe|ves)", "small star"),
    (r"game[- ]?over(?: screens?)?", "dark screen"),
    (r"warp pipes?|green pipes?", "plain column"),
    (r"super mushrooms?", "plain shape"),
    (r"health bars?|hit points?|hp bars?", "plain bar"),
    (r"high[- ]?scores?", "plain counter"),
    (r"boss (?:fights?|battles?|levels?)", "standoff"),
    (r"tetris(?: (?:blocks?|pieces?))?|tetrominoe?s?", "stacked blocks"),
    (r"pac-?man(?: ghosts?)?", "small round shape"),
    (r"space invaders?", "small shapes"),
    (r"super mario|mario|luigi|zelda|pikachu|pok[eé]mon|sonic the hedgehog", "a generic figure"),
)
# Things the model writes on unless told they are blank.
TEXT_OBJECTS = (
    (r"(?:blank\s+)?(?:(?:wall|desk|digital|paper)\s+)?calendars?(?:\s+pages?)?", "blank calendar page"),
    (r"(?:blank\s+)?(?:handwritten\s+)?(?:password\s+|sticky\s+)?notes?", "blank note"),
    (r"(?:blank\s+)?newspapers?", "newspaper with blank grey columns"),
    # Trial 2, S6: Legal's "lit red ON AIR lamp" came back lettered "ON AIR" every time, and the check refused it.
    (r"(?:lit\s+)?(?:red\s+)?on[\s-]air\s+(?:(?:signal|warning|indicator)\s+)?(?:lamps?|lights?|signs?|signals?)",
     "plain red lamp"),
)
_CAPITALIZED = r"(?:(?!(?:A|An|The|This|That|Its|His|Her|Their)\b)[A-Z][\w.'-]*[\s-]+)"
# Case-sensitive outside (?i:...): a capitalized word in front names whose
# flag or seal it is ("California flag", "Great Seal").
SYMBOLS = (
    (rf"{_CAPITALIZED}*(?i:(?:state|national|official|american|u\.s\.)\s+)?(?i:flags?)(?![\s-]?(?i:(?:pole|staff)s?))",
     "plain light grey flag"),
    (rf"(?:{_CAPITALIZED}+|(?i:(?:state|official|government|great|presidential|federal|national|city|county)[\s-]+))"
     r"(?i:seals?)", "plain round shape"),
    (r"(?i:seals?\s+of\s+(?:the\s+)?)[A-Z][\w.'-]*(?:\s+(?:(?:of|the)\s+)*[A-Z][\w.'-]*)*", "plain round shape"),
    (rf"{_CAPITALIZED}*(?i:coats? of arms|emblems?|crests?|logos?|insignias?)", "plain round shape"),
)

# A bare flagpole got an American flag from the image model every time (trial 2, S5): when the visual names
# no flag of its own (that one is plain already, SYMBOLS), its flagpoles fly a plain one. Grey, because "one-color"
# came back red, and QA read a red flag in a story about Republican lawmakers as a hint.
_FLAGPOLE = re.compile(r"(?<![\w])flag[\s-]?(?:pole|staff)(s?)(?![\w])", re.I)
_A_FLAG = re.compile(r"(?<![\w])flags?(?![\s-]?(?:pole|staff)s?)(?![\w])", re.I)


def _plain_flag_on_poles(text: str) -> str:
    if _A_FLAG.search(text):
        return text
    return _FLAGPOLE.sub(
        lambda m: "flagpoles flying plain light grey flags" if m.group(1) else "flagpole flying a plain light grey flag", text)
_LABELED = re.compile(
    r"\s*,?\s*\b(?:labeled|labelled|marked|reading|that reads|which reads|saying|stamped|titled|with the (?:words?|text|label))"
    r"\s+(?:'[^']*'|\"[^\"]*\")",
    re.I,
)
_MONTHS = (r"(?:Jan(?:uary)?|Feb(?:ruary)?|Mar(?:ch)?|Apr(?:il)?|May|June?|July?|Aug(?:ust)?|Sep(?:t(?:ember)?)?|"
           r"Oct(?:ober)?|Nov(?:ember)?|Dec(?:ember)?)(?:\.|(?![A-Za-z]))")
_DATE = rf"(?:{_MONTHS}\s*\d{{1,2}}(?:st|nd|rd|th)?(?:,?\s+\d{{4}})?|\d{{1,2}}(?:st|nd|rd|th)?\s+(?:of\s+)?{_MONTHS}(?:,?\s+\d{{4}})?)"
_DATES = re.compile(
    rf"\s*\b(?:from\s+{_DATE}\s+(?:to|toward|towards|until|through)\s+{_DATE}|(?:from|to|toward|towards|until|through|on|by|at|for)\s+{_DATE}|{_DATE})(?![\w])",
    re.I,
)
_NUMBER = re.compile(r"\s*(?<![\w])[$€£]?\d[\d,.:/]*(?:%|-\w+)?(?![\w])")
_QUOTED = re.compile(r"(?<![\w])(?:'[^'\n]{1,80}'|\"[^\"\n]{1,80}\")(?![\w])")


def fold(text: str) -> str:
    """Accents off, curly quotes straight: 'José' and 'Jose', 'O’Rourke'
    and "O'Rourke" are one name to the patterns."""
    text = text.replace("’", "'").replace("‘", "'").replace("“", '"').replace("”", '"')
    return "".join(c for c in unicodedata.normalize("NFKD", text) if not unicodedata.combining(c))


def name_words(name: str) -> list[str]:
    """'Secretary of State Shirley N. Weber Jr.' -> ['Shirley', 'Weber']:
    titles, initials and suffixes dropped; the last word is the surname."""
    s = fold(name).strip()
    title = re.compile(rf"(?i:{_TITLE})\s+")
    while (m := title.match(s)) and m.end() < len(s):
        s = s[m.end():]
    words = [w.rstrip(".") for w in re.findall(r"[A-Za-z][A-Za-z'-]*\.?", s)]
    return [w for w in words if len(w) > 1 and w.lower() not in SUFFIXES]


def _pieces(word: str) -> list[str]:
    """A hyphenated name and each of its pieces: 'Ocasio-Cortez' -> also 'Ocasio', 'Cortez'."""
    return [word] + ([p for p in word.split("-") if len(p) > 1] if "-" in word else [])


def _person_patterns(name: str) -> list[re.Pattern]:
    words = name_words(name)
    if not words:
        return []
    surname = words[-1]
    patterns = []
    if len(words) > 1:
        between = r"(?:\s+(?:[A-Z]\.?|[A-Z][\w'-]+|\"[^\"]+\"))*?"
        full = rf"{re.escape(words[0])}{between}\s+{re.escape(surname)}"
        patterns.append(re.compile(rf"{_TITLES_BEFORE}(?<![\w])(?i:{full}){_POSSESSIVE}(?![\w])"))
    for piece in _pieces(surname):
        patterns.append(re.compile(rf"{_TITLES_BEFORE}(?<![\w])(?i:{re.escape(piece)}){_POSSESSIVE}(?![\w])"))
    for part in [p for w in words[:-1] for p in _pieces(w)]:
        forms = "|".join(re.escape(f) for f in {part[0].upper() + part[1:], part.upper()})
        patterns.append(re.compile(rf"{_TITLES_BEFORE}(?<![\w])(?:{forms}){_POSSESSIVE}(?![\w])"))
    return patterns


def strip_names(text: str, real_people: list[str]) -> str:
    out = fold(text)
    for name in real_people:
        for pattern in _person_patterns(name):
            out = pattern.sub(lambda m: PERSON + ("'s" if m.group("pos") else ""), out)
    # "a Newsom figure" -> "a a person figure"
    out = re.sub(rf"\b(?:a|an|the)\s+{PERSON}\b", PERSON, out, flags=re.I)
    out = re.sub(rf"\b{PERSON}(?:\s+{PERSON})+\b", PERSON, out)
    return out


def leaked_names(text: str, real_people: list[str]) -> list[str]:
    """Name parts of `real_people` still in `text` by the same rules
    `strip_names` removes them (surname in any case, other parts
    capitalized): [] for a prompt that may be sent."""
    folded = fold(text)
    found = []
    for name in real_people:
        words = name_words(name)
        if not words:
            continue
        surname_pieces = set(_pieces(words[-1]))
        for part in [p for w in words for p in _pieces(w)]:
            if part in surname_pieces:
                hit = re.search(rf"(?<![\w])(?i:{re.escape(part)})(?![\w])", folded)
            else:
                forms = "|".join(re.escape(f) for f in {part[0].upper() + part[1:], part.upper()})
                hit = re.search(rf"(?<![\w])(?:{forms})(?![\w])", folded)
            if hit:
                found.append(f"{part} ({name})")
    return found


_LABELED_PEOPLE = re.compile(
    rf"\s*\b(?:labeled|labelled|marked|named|reading)\s+{PERSON}(?:\s*,\s*{PERSON})*(?:,?\s+(?:and|or)\s+{PERSON})?", re.I)


def _neutralize(text: str, table: tuple, case_insensitive: bool) -> str:
    for pattern, neutral in table:
        text = re.sub(rf"(?<![\w])(?:{pattern})(?![\w])", neutral, text, flags=re.I if case_insensitive else 0)
    return text


def clean_visual(visual: str, real_people: list[str]) -> str:
    text = strip_names(visual, real_people)
    text = _LABELED_PEOPLE.sub("", text)  # "windows labeled Bennett, Bumatay and Sanchez": names as text
    text = _LABELED.sub("", text)
    text = _QUOTED.sub("", text)
    text = _DATES.sub("", text)
    text = _NUMBER.sub("", text)
    text = _neutralize(text, GAME_MOTIFS + TEXT_OBJECTS, case_insensitive=True)
    text = _neutralize(text, SYMBOLS, case_insensitive=False)
    text = _plain_flag_on_poles(text)
    text = re.sub(r"\b([Aa])n(\s+(?:plain|small|blank|stacked|standoff|glow|dark|newspaper)\b)", r"\1\2", text)
    text = re.sub(r"\s+([,.;:])", r"\1", text)
    text = re.sub(r"\s{2,}", " ", text).strip()
    return text


# "U.S.", "D.C.", "Gov." do not end a sentence.
_ABBREV = re.compile(r"\b(?:[A-Z]\.)+|\b(?:Gov|Sen|Rep|Dr|Mr|Mrs|Ms|St|Jr|Sr|No|vs|Lt|Gen|Col|Sgt|Prof|Rev|Mt|Ft|Inc|Co|Corp|Ltd)\.")
# The studio screen ("the screen", "the big screen", a sentence's "Screen"), not a phone's or a laptop's.
_SCREEN = re.compile(
    r"(?:^|\b(?:the|a)\s+(?:(?:big|large|main|wall|studio|tv|video|news)\s+)?)screen\b"
    r"(?:\s+behind\s+(?:her|him|them|the\s+(?:anchor|host)))?"
    r"(?:\s*:\s*|\s*,?\s+(?:with|showing|displaying)\s+|[^:]{0,60}?\b(?:shows|displays|switches\s+to|"
    r"lights\s+up\s+with|fills\s+with|flips\s+to|cuts\s+to)\s+)(?P<what>.+)",
    re.I,
)
# "behind her a stylized map pulses …": what is behind the anchor is the screen, unless it is the studio itself.
_BEHIND = re.compile(
    r"\bbehind\s+(?:her|him|them|the\s+(?:anchor|host))\s*[,:]?\s+"
    r"(?!(?:the\s+|a\s+)?(?:studio|newsroom|wall|walls|desk|lights?|windows?|set|backdrop|camera|screen)\b)(?P<what>.+)",
    re.I,
)
# "… as she delivers the closing line": the screen picture has no anchor in it.
_ANCHOR_TAIL = re.compile(r",?\s+(?:as|while|when|and)\s+(?:the\s+(?:anchor|host)|she|he|they)\b.*$", re.I)
# "the screen shows this same courthouse image": the picture is described elsewhere in the visual.
_SAME_PICTURE = re.compile(
    r"(?:this|that|(?:the|its)\s+same)\s+(?:same\s+)?(?:[\w-]+\s+){0,2}(?:image|picture|view|shot|illustration|scene)",
    re.I)
# A sentence about the anchor, the studio or the delivery, not about a picture.
_STUDIO_CLAUSE = re.compile(r"\b(?:anchor|host|presenter|desk|studio|camera|delivery|tone|posture)\b", re.I)


def screen_content(visual: str) -> str | None:
    """What an anchor scene's `visual` says the screen behind the anchor
    shows ("…; behind them, a screen shows a courthouse icon as she …" -> "a
    courthouse icon"; "…; behind her a stylized map pulses …" -> "a stylized
    map pulses …"), or None when it doesn't say. A screen that shows "this
    same image" shows what the rest of the visual describes."""
    protected = _ABBREV.sub(lambda m: m.group(0).replace(".", "\0"), visual)
    clauses = [c.strip() for c in re.split(r";|[.!?](?=\s|$)", protected) if c.strip()]
    reference = None
    for pattern in (_SCREEN, _BEHIND):
        for clause in clauses:
            m = pattern.search(clause)
            if not m:
                continue
            what = _ANCHOR_TAIL.sub("", m.group("what")).replace("\0", ".").strip(" ,:")
            if _SAME_PICTURE.fullmatch(what):
                # Trial 2, S1: "this same courthouse image" went to the generator as the whole scene, and the
                # courthouse the visual describes "empty of people" came back with people on its steps.
                picture = [c for c in clauses if not (_STUDIO_CLAUSE.search(c) or _SCREEN.search(c) or _BEHIND.search(c))]
                if picture:
                    return ". ".join(picture).replace("\0", ".")
                reference = reference or what
            elif len(what.split()) >= 2:
                return what
    return reference
