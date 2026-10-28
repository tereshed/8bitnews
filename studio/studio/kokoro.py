"""Kokoro-82M voice (`studio build --tts kokoro`), offline: the ONNX export
with per-token durations plus espeak-ng phonemes (studio/README.md, «Голос»).

Word timings are Kokoro's own: the model outputs how many 25 ms frames it
spends on every phoneme token, and the audio follows those frames exactly
(frames x 600 samples == the waveform length). Corrections on top:
- espeak-ng reads the spoken form of each word (`spoken_forms`: numbers,
  money, clock times, titles written out) and phonemizes the whole sentence
  at once, so function words come out reduced, as in speech; it sometimes
  joins words ("in the" -> "ɪnðə"), so phonemes are matched back to words
  character by character against each word phonemized alone, and a word
  that gets none of its sounds fails the build instead of shifting timings;
- the model folds silence into the edges of a phrase (half a second of it in
  a scene's last vowel) and starts a word after a pause a little before its
  token, so each word's edges are snapped onto the audio;
- Kokoro barely pauses at a full stop inside one pass, so the gap after a
  sentence or a clause is topped up to `sentence_pause_s`/`clause_pause_s`,
  splicing silence in the middle of the quiet gap the model left.
Pronunciation hints are read by the respelling key, not by espeak.
"""
from __future__ import annotations

import difflib
import json
import os
import re
from pathlib import Path

import numpy as np

from .errors import BuildError
from .timeline import Word
from .tts import TtsResult, temp_wav_path, write_wav_int16

SAMPLE_RATE = 24000
FRAME_SAMPLES = 600
MAX_PHONEMES = 510
STYLE_DIM = 256
DEFAULT_MODELS_DIR = "/opt/kokoro"

SENTENCE_MARKS = ".!?…"
CLAUSE_MARKS = ",;:—"
_NON_SOUND = set(";:,.!?—…\"()“” ")
_STRESS = set("ˈˌ")

_QUIET_DB = -40.0
_RMS_WINDOW = 240  # 10 ms
_MIN_WORD_S = 0.04
_MAX_GROW_S = 0.1
_HEAD_S = 0.05
_TAIL_S = 0.12
_EDGE_FADE_S = 0.005
_MIN_SPLICE_GAP = 2 * _RMS_WINDOW  # 20 ms
_SPLICE_FADE = 192  # 8 ms
_HEADROOM = 0.5  # -6 dB before 16-bit: Kokoro's peaks reach full scale

# How a `pronunciations[].say` respelling ("ess-EYE-lee", the script.schema
# example) sounds, spelling by spelling, longest first: espeak would read a
# capitalized syllable as letters ("SAY" -> S-A-Y) and "nyer" as "nigher".
_RESPELL = sorted({
    "eye": "aɪ", "air": "ɛɹ", "ear": "ɪɹ", "oor": "ʊɹ", "ar": "ɑːɹ", "or": "ɔːɹ", "er": "ɚ", "ur": "ɜː",
    "ir": "ɜː", "ay": "eɪ", "ee": "iː", "oh": "oʊ", "oo": "uː", "ow": "aʊ", "oy": "ɔɪ", "aw": "ɔː",
    "ah": "ɑː", "uh": "ʌ", "eh": "ɛ", "ih": "ɪ", "ew": "juː", "uu": "ʊ", "a": "æ", "e": "ɛ", "i": "ɪ",
    "o": "ɑː", "u": "ʌ", "tch": "tʃ", "ch": "tʃ", "sh": "ʃ", "th": "θ", "dh": "ð", "zh": "ʒ", "ng": "ŋ",
    "kh": "x", "j": "dʒ", "y": "j", "g": "ɡ", "c": "k", "q": "k", "x": "ks", "r": "ɹ", "b": "b", "d": "d",
    "f": "f", "h": "h", "k": "k", "l": "l", "m": "m", "n": "n", "p": "p", "s": "s", "t": "t", "v": "v",
    "w": "w", "z": "z",
}.items(), key=lambda kv: -len(kv[0]))
_UNSTRESSED = {"ʌ": "ə", "ɜː": "ɚ"}

# Spoken forms espeak gets wrong on its own. phonemizer cuts a line at a "."
# between digits ("61.3 percent" loses "point three percent"); espeak reads
# "$5" as "dollar five", "3.14" as "three point fourteen", "Oct." as "ɑːkt".
_ABBREVIATIONS = {
    "Rep.": "Representative", "Sen.": "Senator", "Gov.": "Governor", "Lt.": "Lieutenant", "Gen.": "General",
    "Col.": "Colonel", "Sgt.": "Sergeant", "Capt.": "Captain", "Prof.": "Professor", "Dr.": "Doctor",
    "Jan.": "January", "Feb.": "February", "Aug.": "August", "Sept.": "September", "Sep.": "September",
    "Oct.": "October", "Nov.": "November", "Dec.": "December", "vs.": "versus", "Mr.": "Mister",
    "Mrs.": "Missus", "Ms.": "Miz", "Jr.": "Junior", "Sr.": "Senior", "St.": "Saint", "No.": "Number",
    "Inc.": "Incorporated", "Corp.": "Corporation", "Co.": "Company", "Ltd.": "Limited",
    "Ph.D.": "P H D", "etc.": "et cetera",
}
# An abbreviation's dot is not a full stop -- except "etc.", which usually
# ends one, and "No." when no number follows ("No. The vote failed.").
_NOT_SENTENCE_END = set(_ABBREVIATIONS) - {"etc.", "No."}
_SCALES = {"thousand", "million", "billion", "trillion"}
_DECIMAL = re.compile(r"(\d+)\.(\d+)")
_RANGE = re.compile(r"^(\d[\d,]*)[-–](\d[\d,]*)$")
_CLOCK = re.compile(r"^(\d{1,2}):(\d{2})$")
_MONEY = re.compile(r"^\$(\d[\d,]*)(?:\.(\d+))?$")
_INITIALISM = re.compile(r"^(?:[A-Za-z]\.){2,}$")
_AFFIX = re.compile(r"^(?P<lead>[\"'“‘(\[]*)(?P<core>.*?)(?P<poss>['’]s)?(?P<trail>[\"'”’)\],;:!?….—–]*)$", re.S)


def models_dir() -> Path:
    return Path(os.environ.get("STUDIO_KOKORO_DIR", DEFAULT_MODELS_DIR))


def kokoro_available() -> bool:
    """True when the model files are in place (the Docker image has them)."""
    return (models_dir() / "model.onnx").is_file()


def _affixes(token: str) -> tuple[str, str, str, str]:
    """(leading quotes, core, possessive 's, trailing punctuation)."""
    m = _AFFIX.match(token)
    return m["lead"], m["core"], m["poss"] or "", m["trail"]


def _dotted(core: str, trail: str) -> tuple[str | None, str]:
    """(the word with its abbreviation dot, the punctuation after that dot):
    ("Rep.", "") for "Rep." and for "Rep.'s"; (None, trail) with no dot."""
    if core.endswith("."):
        return core, trail
    if trail.startswith("."):
        return core + ".", trail[1:]
    return None, trail


def _final_mark(trail: str) -> str:
    """The last punctuation mark, looking through closing quotes and brackets
    ('why?"' -> "?"); "" when there is none."""
    return trail.rstrip("\"'”’)]")[-1:]


def _starts_with_digit(token: str | None) -> bool:
    return bool(token) and _affixes(token)[1][:1].isdigit()


def _ends_sentence(token: str, next_token: str | None) -> bool:
    """A sentence ends after this token (the model then gets a full pause).
    Titles never end one, "No." does unless a number follows, "U.S." only
    at the end of the narration -- a full stop after it can't be told apart."""
    _, core, _, trail = _affixes(token)
    dotted, rest = _dotted(core, trail)
    mark = _final_mark(rest) or ("." if dotted else "")
    if mark != ".":
        return bool(mark) and mark in SENTENCE_MARKS
    if rest.strip("\"'”’)]"):  # "U.S.." / "Inc.)." -- more punctuation after the dot
        return True
    if dotted == "No.":
        return not _starts_with_digit(next_token)
    if dotted in _NOT_SENTENCE_END:
        return False
    if dotted and _INITIALISM.match(dotted):
        return next_token is None
    return True


def _clause_mark(token: str) -> bool:
    mark = _final_mark(_affixes(token)[3])
    return bool(mark) and mark in CLAUSE_MARKS


def _decimal(text: str) -> str:
    """"3.14" -> "3 point 1 4": digits after the point are read one by one."""
    return _DECIMAL.sub(lambda m: f"{m[1]} point {' '.join(m[2])}", text)


def _money(whole: str, cents: str | None, scale_follows: bool) -> str:
    if scale_follows:  # "$1.5 million" -> "1 point 5" + "million dollars"
        return _decimal(f"{whole}.{cents}") if cents else whole
    dollars = f"{whole} {'dollar' if whole == '1' else 'dollars'}"
    c = int(cents.ljust(2, "0")[:2]) if cents else 0
    if not c:
        return dollars
    cents_text = f"{c} {'cent' if c == 1 else 'cents'}"
    return cents_text if whole == "0" else f"{dollars} and {cents_text}"


def spoken_forms(tokens: list[str]) -> list[str]:
    """What espeak should read for each token: numbers, money, clock times
    and abbreviations written out; everything else as is."""
    out = []
    for k, token in enumerate(tokens):
        lead, core, poss, trail = _affixes(token)
        prev_core = _affixes(tokens[k - 1])[1] if k else ""
        next_token = tokens[k + 1] if k + 1 < len(tokens) else None
        dotted, rest = _dotted(core, trail)
        if dotted in _ABBREVIATIONS and (dotted != "No." or _starts_with_digit(next_token)):
            core, trail = _ABBREVIATIONS[dotted], rest
        elif dotted and _INITIALISM.match(dotted) and poss:  # "U.S.'s" -> "US's", not "U S S"
            core, trail = dotted.replace(".", ""), rest
        elif m := _MONEY.match(core):
            scale = bool(next_token) and _affixes(next_token)[1].lower() in _SCALES
            core = _money(m[1], m[2], scale)
        elif core.lower() in _SCALES and _MONEY.match(prev_core):
            core = core + " dollars"
        elif m := _RANGE.match(core):
            core = f"{m[1]} to {m[2]}"
        elif m := _CLOCK.match(core):
            minutes = m[2]
            core = (f"{m[1]} o'clock" if minutes == "00" else
                    f"{m[1]} oh {minutes[1]}" if minutes[0] == "0" else f"{m[1]} {minutes}")
        else:
            core = _decimal(core)
        out.append(lead + core + poss + trail)
    return out


class _Phonemizer:
    def __init__(self):
        import espeakng_loader
        from phonemizer.backend import EspeakBackend
        from phonemizer.backend.espeak.wrapper import EspeakWrapper

        EspeakWrapper.set_library(espeakng_loader.get_library_path())
        # espeak-ng ignores a data path longer than 160 characters (a deep
        # venv on a dev machine): STUDIO_ESPEAK_DATA points at a short copy.
        EspeakWrapper.set_data_path(os.environ.get("STUDIO_ESPEAK_DATA") or espeakng_loader.get_data_path())
        self._backend = EspeakBackend("en-us", preserve_punctuation=True, with_stress=True)

    def __call__(self, text: str) -> str:
        # One text per call: given a list, phonemizer's punctuation handling
        # can shift results onto the next item.
        if not text.strip():
            return ""
        return " ".join(self._backend.phonemize([text], strip=True)[0].split())

    def respell(self, token: str) -> str:
        """A respelling token -> phonemes, syllable by syllable, the stress on
        a capitalized syllable (unstressed "uh" -> ə)."""
        lead, core, poss, trail = _affixes(token)
        if core.endswith("."):
            core, trail = core[:-1], "." + trail
        out = ""
        for syllable in core.split("-"):
            stressed = syllable.isupper() and len(syllable) > 1
            text, ipa, i = syllable.lower(), [], 0
            while i < len(text):
                spelling = next(((sp, ph) for sp, ph in _RESPELL if text.startswith(sp, i)), None)
                if spelling is None:  # a letter outside the key: let espeak have the syllable
                    ipa = [c for c in self(text) if c not in _STRESS and c != " "]
                    break
                ph = spelling[1] if stressed else _UNSTRESSED.get(spelling[1], spelling[1])
                if not ipa or ipa[-1] != ph:  # "ss" is one s
                    ipa.append(ph)
                i += len(spelling[0])
            out += ("ˈ" if stressed else "") + "".join(ipa)
        return "".join(c for c in lead + out + ("z" if poss else "") + trail if c not in "\"'“”‘’()[]")


def _sentence_chunks(tokens: list[str]) -> list[list[int]]:
    """Token indexes grouped into sentences."""
    chunks, current = [], []
    for i, tok in enumerate(tokens):
        current.append(i)
        if _ends_sentence(tok, tokens[i + 1] if i + 1 < len(tokens) else None):
            chunks.append(current)
            current = []
    if current:
        chunks.append(current)
    return chunks


def _label_chars(chars: list[str], ref_words: list[str]) -> list[int | None]:
    """For each synthesized phoneme char, the index of the word it belongs
    to (None for spaces and punctuation): a character alignment of the
    sentence's phonemes against each word phonemized on its own."""
    ref_chars, ref_labels = [], []
    for k, ph in enumerate(ref_words):
        for c in ph:
            if c not in _NON_SOUND and c not in _STRESS:
                ref_chars.append(c)
                ref_labels.append(k)
    syn_pos = [i for i, c in enumerate(chars) if c not in _NON_SOUND and c not in _STRESS]
    syn_chars = [chars[i] for i in syn_pos]
    labels: list[int | None] = [None] * len(chars)
    matcher = difflib.SequenceMatcher(None, ref_chars, syn_chars, autojunk=False)
    for op, i1, i2, j1, j2 in matcher.get_opcodes():
        for j in range(j1, j2):
            if op in ("equal", "replace"):
                i = i1 + (j - j1) * (i2 - i1) // (j2 - j1)
                labels[syn_pos[j]] = ref_labels[min(i, i2 - 1)]
    # Inserted sound chars take the word of the char before them; stress
    # marks belong to the sound they precede.
    last = None
    for p in syn_pos:
        if labels[p] is None:
            labels[p] = last
        last = labels[p]
    first = next((labels[p] for p in syn_pos if labels[p] is not None), 0)
    for p in syn_pos:
        if labels[p] is None:
            labels[p] = first
    for i in range(len(chars) - 1, -1, -1):
        if chars[i] in _STRESS:
            labels[i] = labels[i + 1] if i + 1 < len(chars) and chars[i + 1] not in _NON_SOUND else None
    # Keep word order monotonic: a stray char matched to a later word can't
    # pull that word's start before an earlier word's end.
    running = -1
    for i, lab in enumerate(labels):
        if lab is not None:
            running = max(running, lab)
            labels[i] = running
    return labels


def _quiet_mask(audio: np.ndarray) -> np.ndarray:
    n = len(audio) // _RMS_WINDOW
    rms = np.sqrt((audio[: n * _RMS_WINDOW].reshape(n, _RMS_WINDOW) ** 2).mean(axis=1))
    return rms <= rms.max() * 10 ** (_QUIET_DB / 20) if n else np.zeros(0, bool)


def _snap(spans: list[tuple[int, int] | None], quiet: np.ndarray) -> list[tuple[int, int] | None]:
    """Moves each word's edges (samples) onto the audio: trims quiet the
    model counted into the word (keeping at least _MIN_WORD_S of it), then
    widens an edge that cuts into sound by up to _MAX_GROW_S -- an onset
    after a pause starts a few frames before the token boundary -- never
    into a neighbouring word."""
    w = _RMS_WINDOW
    min_len = int(_MIN_WORD_S * SAMPLE_RATE)
    grow = int(_MAX_GROW_S * SAMPLE_RATE)

    def is_quiet(sample: int) -> bool:
        i = sample // w
        return i >= len(quiet) or i < 0 or bool(quiet[i])

    out: list[tuple[int, int] | None] = []
    for span in spans:
        if span is None:
            out.append(None)
            continue
        s, e = span
        while e - s > min_len and is_quiet(e - 1):
            e = max(s + min_len, ((e - 1) // w) * w)
        while e - s > min_len and is_quiet(s):
            s = min(e - min_len, (s // w + 1) * w)
        out.append((s, e))
    for k, span in enumerate(out):
        if span is None:
            continue
        s, e = span
        prev_end = max((sp[1] for sp in out[:k] if sp), default=0)
        next_start = min((sp[0] for sp in out[k + 1:] if sp), default=len(quiet) * w)
        floor, ceil = max(prev_end, s - grow), min(next_start, e + grow)
        while s - w >= floor and not is_quiet(s - 1):
            s = (s - 1) // w * w
        while e + w <= ceil and not is_quiet(e):
            e = (e // w + 1) * w
        out[k] = (s, e)
    return out


class KokoroTtsAdapter:
    def __init__(self, speed: float = 1.1, sentence_pause_s: float = 0.25, clause_pause_s: float = 0.1,
                 models: Path | None = None):
        import onnxruntime as ort

        self.speed = float(speed)
        self.sentence_pause_s = float(sentence_pause_s)
        self.clause_pause_s = float(clause_pause_s)
        self.models = Path(models) if models else models_dir()
        model_path = self.models / "model.onnx"
        if not model_path.is_file():
            raise BuildError(f"Kokoro model not found at {model_path}: build the studio image (make studio-image) "
                             "or set STUDIO_KOKORO_DIR")
        opts = ort.SessionOptions()
        opts.log_severity_level = 3
        self._session = ort.InferenceSession(str(model_path), opts, providers=["CPUExecutionProvider"])
        self._vocab = json.loads((self.models / "tokenizer.json").read_text())["model"]["vocab"]
        self._phonemize = _Phonemizer()
        self._voices: dict[str, np.ndarray] = {}

    @classmethod
    def from_settings(cls, settings: dict, serious: bool = False) -> "KokoroTtsAdapter":
        return cls(
            speed=settings["serious_speed" if serious else "speed"],
            sentence_pause_s=settings.get("sentence_pause_s", 0.25),
            clause_pause_s=settings.get("clause_pause_s", 0.1),
        )

    def describe(self) -> dict:
        manifest_path = self.models / "models.json"
        name = "Kokoro-82M v1.0"
        if manifest_path.is_file():
            meta = json.loads(manifest_path.read_text())["kokoro"]
            name = f"{meta['name']} @ {meta['revision'][:12]}"
        return {"model": name, "speed": self.speed}

    def _voice(self, voice_id: str) -> np.ndarray:
        if voice_id not in self._voices:
            path = self.models / "voices" / f"{voice_id}.bin"
            if not path.is_file():
                have = sorted(p.stem for p in (self.models / "voices").glob("*.bin"))
                raise BuildError(f"Kokoro voice {voice_id!r} is not in the image (have: {', '.join(have)}); "
                                 "add it to studio/models.json")
            self._voices[voice_id] = np.fromfile(path, dtype=np.float32).reshape(-1, STYLE_DIM)
        return self._voices[voice_id]

    def _phonemes(self, tokens: list[str], hinted_idx: frozenset[int] = frozenset()) -> tuple[str, list[str]]:
        """(sentence phonemes, each token's phonemes on its own). Runs of
        plain words are phonemized together, in their spoken forms; a token
        a pronunciation hint put there is built syllable by syllable."""
        forms = spoken_forms(tokens)
        hinted = [k in hinted_idx for k in range(len(tokens))]
        ref = [self._phonemize.respell(t) if h else self._phonemize(f) for t, f, h in zip(tokens, forms, hinted)]
        parts, run = [], []
        for f, h, r in zip(forms, hinted, ref):
            if h:
                parts.append(self._phonemize(" ".join(run)))
                run = []
                parts.append(r)
            else:
                run.append(f)
        parts.append(self._phonemize(" ".join(run)))
        return " ".join(p for p in parts if p), ref

    def _run(self, chars: list[str], voice: np.ndarray) -> tuple[np.ndarray, np.ndarray]:
        ids = [self._vocab[c] for c in chars]
        style = voice[min(len(ids), len(voice) - 1)][None, :]
        audio, durations = self._session.run(None, {
            "input_ids": np.array([[0, *ids, 0]], dtype=np.int64),
            "style": style.astype(np.float32),
            "speed": np.array([self.speed], dtype=np.float32),
        })
        audio = audio.ravel().astype(np.float32)
        frames = np.maximum(np.round(durations.ravel()), 1)
        edges = np.concatenate([[0], np.cumsum(frames)])
        edges = np.round(edges * (len(audio) / edges[-1])).astype(np.int64)  # == frames * 600
        return audio, edges

    def _synthesize_chunk(self, tokens: list[str], voice: np.ndarray,
                          hinted: frozenset[int]) -> tuple[np.ndarray, list[tuple[int, int] | None]]:
        phonemes, ref = self._phonemes(tokens, hinted)
        chars = [c for c in phonemes if c in self._vocab]
        if len(chars) > MAX_PHONEMES:
            raise BuildError(f"sentence too long for Kokoro ({len(chars)} phonemes > {MAX_PHONEMES}): "
                             f"{' '.join(tokens)[:80]!r}...")
        if not any(c not in _NON_SOUND for c in chars):
            raise BuildError(f"nothing to say in {' '.join(tokens)!r}")
        audio, edges = self._run(chars, voice)
        labels = _label_chars(chars, ref)
        spans: list[tuple[int, int] | None] = [None] * len(tokens)
        for i, (c, lab) in enumerate(zip(chars, labels)):
            if lab is None:
                continue
            s, e = int(edges[i + 1]), int(edges[i + 2])  # edges[0..1] is the leading pad
            cur = spans[lab]
            spans[lab] = (s, e) if cur is None else (min(cur[0], s), max(cur[1], e))
        # A word with sounds of its own must have got some: anything else
        # means espeak dropped or merged words and the timings are off.
        said = [sum(1 for lab, c in zip(labels, chars) if lab == k and c not in _STRESS) for k in range(len(tokens))]
        for tok, r, n in zip(tokens, ref, said):
            want = sum(1 for c in r if c not in _NON_SOUND and c not in _STRESS)
            if want and n < max(1, want // 3):
                raise BuildError(f"Kokoro: the phonemes for {tok!r} went missing in {' '.join(tokens)[:80]!r} "
                                 f"({n} of {want} sounds); rephrase the word or add a pronunciation hint")
        return audio, _snap(spans, _quiet_mask(audio))

    def synthesize(self, text: str, voice_id: str, hinted: frozenset[int] = frozenset()) -> TtsResult:
        """`hinted`: indexes of the tokens a pronunciation hint put there
        (its `say` respelling), read by the respelling key."""
        tokens = text.split()
        if not tokens:
            raise BuildError("empty narration")
        voice = self._voice(voice_id)

        # Sentences go to the model together while they fit in one pass.
        chunks: list[list[int]] = []
        for sentence in _sentence_chunks(tokens):
            candidate = (chunks[-1] + sentence) if chunks else sentence
            if chunks and self._fits(candidate, tokens, hinted):
                chunks[-1] = candidate
            else:
                chunks.append(sentence)

        pieces: list[np.ndarray] = []
        spans: list[tuple[int, int] | None] = []
        offset = 0
        for n, chunk in enumerate(chunks):
            local = frozenset(j for j, i in enumerate(chunk) if i in hinted)
            audio, chunk_spans = self._synthesize_chunk([tokens[i] for i in chunk], voice, local)
            voiced = [s for s in chunk_spans if s]
            start = max(0, voiced[0][0] - int(_HEAD_S * SAMPLE_RATE)) if n == 0 else max(0, voiced[0][0] - int(0.02 * SAMPLE_RATE))
            end = min(len(audio), voiced[-1][1] + int(_TAIL_S * SAMPLE_RATE))
            audio = audio[start:end]
            if n > 0:
                gap = np.zeros(int(self.sentence_pause_s * SAMPLE_RATE), dtype=np.float32)
                pieces.append(gap)
                offset += len(gap)
            pieces.append(audio)
            spans += [(sp[0] - start + offset, sp[1] - start + offset) if sp else None for sp in chunk_spans]
            offset += len(audio)
        audio = np.concatenate(pieces)
        audio, spans = self._insert_pauses(audio, tokens, spans)

        fade = int(_EDGE_FADE_S * SAMPLE_RATE)
        audio[:fade] *= np.linspace(0, 1, fade, dtype=np.float32)
        audio[-fade:] *= np.linspace(1, 0, fade, dtype=np.float32)

        words = []
        prev_end = 0
        for tok, span in zip(tokens, spans):
            s, e = span if span else (prev_end, prev_end)
            words.append(Word(text=tok, start_s=s / SAMPLE_RATE, end_s=e / SAMPLE_RATE))
            prev_end = e
        out_path = temp_wav_path()
        write_wav_int16(out_path, audio * _HEADROOM, SAMPLE_RATE)
        return TtsResult(wav_path=out_path, sample_rate=SAMPLE_RATE, words=tuple(words),
                         duration_s=len(audio) / SAMPLE_RATE)

    def _fits(self, idx: list[int], tokens: list[str], hinted: frozenset[int]) -> bool:
        local = frozenset(j for j, i in enumerate(idx) if i in hinted)
        phonemes, _ = self._phonemes([tokens[i] for i in idx], local)
        return sum(c in self._vocab for c in phonemes) <= MAX_PHONEMES

    def _insert_pauses(self, audio: np.ndarray, tokens: list[str],
                       spans: list[tuple[int, int] | None]) -> tuple[np.ndarray, list[tuple[int, int] | None]]:
        """Lengthens the gap after a word that ends a sentence or a clause to
        the configured pause, splicing silence into the middle of the gap."""
        # The pause after each spoken word, from its own mark or from a
        # punctuation-only token after it ("said — then").
        wants: dict[int, tuple[float, bool]] = {}
        for k in range(len(tokens) - 1):
            sentence = _ends_sentence(tokens[k], tokens[k + 1])
            want = self.sentence_pause_s if sentence else self.clause_pause_s if _clause_mark(tokens[k]) else 0
            spoken = next((j for j in range(k, -1, -1) if spans[j]), None)
            if want and spoken is not None:
                prev_want, prev_sentence = wants.get(spoken, (0.0, False))
                wants[spoken] = (max(want, prev_want), sentence or prev_sentence)
        cuts: list[tuple[int, int]] = []  # (position, samples to insert)
        for k, (want, sentence) in sorted(wants.items()):
            nxt = next((spans[j] for j in range(k + 1, len(tokens)) if spans[j]), None)
            if nxt is None:
                continue
            gap = nxt[0] - spans[k][1]
            missing = int(want * SAMPLE_RATE) - gap
            # A sentence always gets its pause; a comma only where the model
            # left a quiet gap -- one it ran straight through it didn't mean
            # to stop on.
            if missing > 0 and gap >= 0 and (sentence or gap >= _MIN_SPLICE_GAP):
                cuts.append(((spans[k][1] + nxt[0]) // 2, missing))
        if not cuts:
            return audio, spans
        audio = audio.copy()
        fade = np.linspace(1, 0, _SPLICE_FADE, dtype=np.float32)
        parts, prev = [], 0
        for pos, n in cuts:
            # Short fades on both sides of the splice: no click even where
            # the gap the model left was not quite silent.
            lo, hi = max(prev, pos - _SPLICE_FADE), min(len(audio), pos + _SPLICE_FADE)
            audio[lo:pos] *= fade[_SPLICE_FADE - (pos - lo):]
            audio[pos:hi] *= fade[::-1][: hi - pos]
            parts += [audio[prev:pos], np.zeros(n, dtype=np.float32)]
            prev = pos
        parts.append(audio[prev:])

        # Silence spliced exactly at a word's edge goes after its end and
        # before the next word's start.
        def shift(x: int, is_end: bool) -> int:
            return x + sum(n for pos, n in cuts if (pos < x if is_end else pos <= x))

        return np.concatenate(parts), [(shift(sp[0], False), shift(sp[1], True)) if sp else None for sp in spans]
