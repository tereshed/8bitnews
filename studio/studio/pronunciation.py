"""Applies `script.pronunciations` hints before synthesis and maps the TTS
word timings back to the *original* words afterwards -- general code, not
part of any TTS adapter (studio/README.md task notes), so a future Kokoro
adapter gets this for free.

Matching is whole-word and case-sensitive. `pronunciations[].text` may be one
word or several ("Garcetti", "Deutsche Welle"): it matches that many
consecutive narration words whose joined text, with the punctuation around
the run and a trailing possessive ('s) stripped, equals `text` (whitespace in
`text` normalized). So "Weber's," matches a hint for "Weber" and
"«Deutsche Welle»," one for "Deutsche Welle"; punctuation *inside* the run
("Deutsche, Welle") does not match. The possessive and the punctuation are
re-attached around `say` in the text sent to the TTS engine. Where hints
overlap, the one with more words wins ("Charles Lamanna" over "Charles").

Each match becomes one `Group`: the original words it covers and the
synthesis tokens its `say` expands to. Unmatched words are groups of one
word and one token.
"""
from __future__ import annotations

from typing import NamedTuple

from .timeline import Word

_PUNCT = ".,!?;:\"'()[]{}‘’“”…¿¡«»"


class Group(NamedTuple):
    words: int
    """Original narration words in the group."""
    tokens: int
    """Synthesis tokens they became."""
    hinted: bool
    """True when a pronunciation hint put the tokens there."""


def _split_word(word: str) -> tuple[str, str, str, str]:
    """(lead_punct, core, possessive_suffix, trail_punct). The trailing-punct
    strip stops at the first non-punctuation character, so the 's' of a
    possessive ('s) is never mistaken for punctuation and stripped away.
    Works the same on a run of words joined by spaces."""
    i, j = 0, len(word)
    while i < j and word[i] in _PUNCT:
        i += 1
    while j > i and word[j - 1] in _PUNCT:
        j -= 1
    lead, body, trail = word[:i], word[i:j], word[j:]
    poss = ""
    if body[-2:].lower() in ("'s", "’s"):
        poss = body[-2:]
        body = body[:-2]
    return lead, body, poss, trail


def _norm(text: str) -> str:
    return " ".join(text.split())


def _hint_table(pronunciations: list[dict] | None) -> dict[int, dict[str, str]]:
    """{word count: {normalized text: say}}; of two hints for the same text
    the later one wins."""
    table: dict[int, dict[str, str]] = {}
    for p in pronunciations or []:
        text = _norm(p["text"])
        if text:
            table.setdefault(len(text.split()), {})[text] = p["say"]
    return table


def _match(words: list[str], i: int, table: dict[int, dict[str, str]]) -> tuple[int, str, str] | None:
    """(word count, the run's text with `say` in place of its core, the
    hint's normalized text) for the longest hint that starts at `words[i]`,
    or None."""
    for n in sorted(table, reverse=True):
        if i + n > len(words):
            continue
        lead, core, poss, trail = _split_word(" ".join(words[i : i + n]))
        say = table[n].get(core)
        if say is not None:
            return n, f"{lead}{say}{poss}{trail}", core
    return None


def _apply(narration: str, pronunciations: list[dict] | None) -> list[tuple[list[str], Group, str | None]]:
    """The narration cut into groups: (synthesis tokens, Group, the applied
    hint's normalized text or None)."""
    table = _hint_table(pronunciations)
    words = narration.split()
    out: list[tuple[list[str], Group, str | None]] = []
    i = 0
    while i < len(words):
        m = _match(words, i, table) if table else None
        if m is None:
            out.append(([words[i]], Group(1, 1, False), None))
            i += 1
            continue
        n, replacement, text = m
        tokens = replacement.split() or [replacement]
        out.append((tokens, Group(n, len(tokens), True), text))
        i += n
    return out


def build_synthesis_text(narration: str, pronunciations: list[dict] | None) -> tuple[str, list[Group]]:
    """Returns (synthesis_text, groups): consecutive `groups` cover the
    narration's words in order and the synthesis text's tokens in order."""
    applied = _apply(narration, pronunciations)
    return " ".join(t for tokens, _g, _h in applied for t in tokens), [g for _t, g, _h in applied]


def hinted_tokens(groups: list[Group]) -> frozenset[int]:
    """Indexes of the synthesis tokens a pronunciation hint put there (the
    `say` respelling), so an adapter can read exactly those its own way."""
    out, idx = set(), 0
    for g in groups:
        if g.hinted:
            out.update(range(idx, idx + g.tokens))
        idx += g.tokens
    return frozenset(out)


def _weight(word: str) -> int:
    return max(1, len(_split_word(word)[1]))


def regroup_words(narration: str, groups: list[Group], synth_words: tuple[Word, ...]) -> tuple[Word, ...]:
    """Maps `synth_words` (timed per synthesis token) back to one `Word` per
    original word of `narration` -- so subtitles/transcript/timeline.json
    always carry the original text and timing, never the pronunciation
    respelling. A group of one original word spans its tokens' first start
    to last end. A group of several words with as many tokens maps them one
    to one; otherwise the group's span is shared out in proportion to the
    words' length in letters."""
    orig_words = narration.split()
    result: list[Word] = []
    w = t = 0
    for g in groups:
        chunk = synth_words[t : t + g.tokens]
        words = orig_words[w : w + g.words]
        t += g.tokens
        w += g.words
        if g.words == 1:
            result.append(Word(text=words[0], start_s=chunk[0].start_s, end_s=chunk[-1].end_s))
        elif g.words == g.tokens:
            result.extend(Word(text=o, start_s=s.start_s, end_s=s.end_s) for o, s in zip(words, chunk))
        else:
            start, end = chunk[0].start_s, chunk[-1].end_s
            weights = [_weight(o) for o in words]
            total, acc = sum(weights), 0
            for o, wt in zip(words, weights):
                s = start + (end - start) * acc / total
                acc += wt
                result.append(Word(text=o, start_s=s, end_s=start + (end - start) * acc / total))
    return tuple(result)


def unused_pronunciations(narrations: list[str], pronunciations: list[dict] | None) -> list[str]:
    """`text` of every hint that is applied nowhere in any narration under
    the same rules as `build_synthesis_text` -- a phrase whose words appear
    with punctuation or another case, or a hint a longer one covers.
    `studio build` warns about these instead of dropping them silently."""
    used = {text for narration in narrations for _t, _g, text in _apply(narration, pronunciations) if text}
    return [p["text"] for p in pronunciations or [] if _norm(p["text"]) not in used]
