"""Applies `script.pronunciations` hints before synthesis and maps the TTS
word timings back to the *original* words afterwards -- general code, not
part of any TTS adapter (studio/README.md task notes), so a future Kokoro
adapter gets this for free.

Matching is whole-word, case-sensitive, tolerant of punctuation on either
side of the word and of a trailing possessive ('s): `pronunciations[].text`
is matched against the word's bare core, e.g. "Weber's," matches a hint for
"Weber" and the possessive/punctuation are re-attached around `say` in the
text sent to the TTS engine. A `say` that itself contains spaces expands to
several synthesis tokens that all map back to the single original word.
"""
from __future__ import annotations

from .timeline import Word

_PUNCT = ".,!?;:\"'()[]{}‘’“”…"


def _split_word(word: str) -> tuple[str, str, str, str]:
    """(lead_punct, core, possessive_suffix, trail_punct). The trailing-punct
    strip stops at the first non-punctuation character, so the 's' of a
    possessive ('s) is never mistaken for punctuation and stripped away."""
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


def build_synthesis_text(narration: str, pronunciations: list[dict] | None) -> tuple[str, list[int]]:
    """Returns (synthesis_text, group_sizes): `group_sizes[i]` is how many
    whitespace-separated tokens of `synthesis_text` correspond to the i-th
    original word of `narration`."""
    lookup = {p["text"]: p["say"] for p in pronunciations or []}
    orig_words = narration.split()
    synth_tokens: list[str] = []
    group_sizes: list[int] = []
    for word in orig_words:
        lead, core, poss, trail = _split_word(word)
        say = lookup.get(core)
        if say is None:
            synth_tokens.append(word)
            group_sizes.append(1)
            continue
        replacement = f"{lead}{say}{poss}{trail}"
        sub_tokens = replacement.split() or [replacement]
        synth_tokens.extend(sub_tokens)
        group_sizes.append(len(sub_tokens))
    return " ".join(synth_tokens), group_sizes


def hinted_tokens(group_sizes: list[int], narration: str, pronunciations: list[dict] | None) -> frozenset[int]:
    """Indexes of the synthesis tokens a pronunciation hint put there (the
    `say` respelling), so an adapter can read exactly those its own way."""
    lookup = {p["text"] for p in pronunciations or []}
    out, idx = set(), 0
    for word, size in zip(narration.split(), group_sizes):
        if _split_word(word)[1] in lookup:
            out.update(range(idx, idx + size))
        idx += size
    return frozenset(out)


def regroup_words(narration: str, group_sizes: list[int], synth_words: tuple[Word, ...]) -> tuple[Word, ...]:
    """Collapses `synth_words` (timed per synthesis token) back to one `Word`
    per original word of `narration`, each spanning its whole group's first
    start to last end -- so subtitles/transcript/timeline.json always carry
    the original text and timing, never the pronunciation respelling."""
    orig_words = narration.split()
    result = []
    idx = 0
    for word, size in zip(orig_words, group_sizes):
        chunk = synth_words[idx : idx + size]
        idx += size
        result.append(Word(text=word, start_s=chunk[0].start_s, end_s=chunk[-1].end_s))
    return tuple(result)


def unused_pronunciations(narrations: list[str], pronunciations: list[dict] | None) -> list[str]:
    """`text` of every hint that matches no word of any narration under the
    same rules as `build_synthesis_text` -- e.g. a multi-word phrase, which
    single-word matching never applies. `studio build` warns about these
    instead of dropping them silently."""
    cores = {_split_word(word)[1] for narration in narrations for word in narration.split()}
    return [p["text"] for p in pronunciations or [] if p["text"] not in cores]
