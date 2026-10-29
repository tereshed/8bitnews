"""The timeline is a standalone data structure, independent of how it was
produced: scenes with absolute start/end, per-word timings, subtitle cues,
and a voice volume envelope the anchor's mouth animates against (docs:
'Таймлайн — отдельная структура данных'). `uniform_timeline` builds one from
word count and a words-per-minute pace (no audio) -- used by tests and by
`template preview`. `timeline_from_voice` (E2a-2a) builds the same structure
from a TTS adapter's real per-scene word timings plus the template's
`timing.scene_pause_s` gap between scenes.
"""
from __future__ import annotations

import json
from dataclasses import dataclass, field
from pathlib import Path

from .constants import DEFAULT_WPM, SERIOUS_WPM
from .subtitles import SubtitleCue, split_cues


@dataclass(frozen=True)
class Word:
    text: str
    start_s: float
    end_s: float


@dataclass(frozen=True)
class SceneTiming:
    scene_id: str
    shot: str
    start_s: float
    end_s: float
    words: tuple[Word, ...] = field(default_factory=tuple)
    subtitles: tuple[SubtitleCue, ...] = field(default_factory=tuple)

    @property
    def duration_s(self) -> float:
        return self.end_s - self.start_s

    def volume_at(self, t_abs: float) -> float:
        """Envelope in [0, 1] for the anchor's mouth: each word ramps up and
        back down across its span (a simple triangle), with silence in the
        gaps between words. Deterministic given the word boundaries."""
        if not (self.start_s <= t_abs <= self.end_s):
            return 0.0
        for word in self.words:
            if word.start_s <= t_abs <= word.end_s:
                span = word.end_s - word.start_s
                if span <= 0:
                    return 1.0
                phase = (t_abs - word.start_s) / span
                # Triangle peaking at the midpoint of the word.
                return max(0.0, 1.0 - abs(phase - 0.5) * 2.0)
        return 0.0


@dataclass(frozen=True)
class Timeline:
    scenes: tuple[SceneTiming, ...]

    @property
    def duration_s(self) -> float:
        return self.scenes[-1].end_s if self.scenes else 0.0

    def scene_at(self, t_abs: float) -> SceneTiming | None:
        """The scene on screen at `t_abs`: the last one that has started. The
        pause between two scenes (template `timing.scene_pause_s`) holds the
        previous scene's picture -- its words are over, so the mouth is shut
        and no subtitle shows -- instead of falling through to a black frame."""
        if not self.scenes:
            return None
        current = self.scenes[0]
        for scene in self.scenes:
            if scene.start_s > t_abs:
                break
            current = scene
        return current


_MIN_SCENE_DURATION_S = 1.0


def uniform_timeline(script: dict, wpm: float | None = None, subtitle_style: dict | None = None) -> Timeline:
    """Builds a Timeline purely from each scene's narration word count and a
    fixed words-per-minute pace (no audio): format.md's own word budget is
    defined this way (144 wpm, 131 in serious mode -- the af_sarah pace), so
    this is also the right stand-in for tests and template preview. Cues are
    split with a template's `subtitles.style` caps when given, as `studio
    build` does.
    """
    style = subtitle_style or {}
    caps = {k: style[f] for k, f in (("max_lines", "max_lines"), ("max_chars", "max_chars_per_line"),
                                     ("font_scale", "font_scale")) if f in style}
    if wpm is None:
        wpm = SERIOUS_WPM if script.get("serious_mode") else DEFAULT_WPM
    words_per_second = wpm / 60.0

    scenes: list[SceneTiming] = []
    t = 0.0
    for scene in script["scenes"]:
        words_text = scene["narration"].split()
        n = max(len(words_text), 1)
        duration = max(n / words_per_second, _MIN_SCENE_DURATION_S)
        scene_start, scene_end = t, t + duration
        word_dur = duration / n
        words = tuple(
            Word(text=w, start_s=scene_start + i * word_dur, end_s=scene_start + (i + 1) * word_dur)
            for i, w in enumerate(words_text)
        )
        subtitles = tuple(split_cues(words, **caps))
        scenes.append(
            SceneTiming(
                scene_id=scene["id"],
                shot=scene["shot"],
                start_s=scene_start,
                end_s=scene_end,
                words=words,
                subtitles=subtitles,
            )
        )
        t = scene_end
    return Timeline(scenes=tuple(scenes))


@dataclass(frozen=True)
class SceneVoice:
    """One scene's TTS output, already mapped back to the *original* words
    (studio.pronunciation.regroup_words): timings are local to the scene,
    starting at 0 -- `timeline_from_voice` places them on the absolute
    timeline."""

    scene_id: str
    shot: str
    words: tuple[Word, ...]
    duration_s: float | None = None
    """Length of the scene's voice audio; None means it ends with the last
    word. A real voice runs a little past its last word (a breath of tail),
    and the scene has to last as long as its audio or every later scene
    drifts off the voice track."""


def timeline_from_voice(
    scene_voices: list[SceneVoice],
    pause_s: float,
    max_lines: int,
    max_chars: int,
    font_scale: int = 1,
) -> Timeline:
    """Builds a Timeline from real per-scene voice word timings plus
    `pause_s` silence *between* scenes (template `timing.scene_pause_s`) --
    none before the first scene or after the last, so the rendered video has
    no dead air at either end."""
    scenes: list[SceneTiming] = []
    t = 0.0
    for i, sv in enumerate(scene_voices):
        if i > 0:
            t += pause_s
        start = t
        words = tuple(Word(text=w.text, start_s=start + w.start_s, end_s=start + w.end_s) for w in sv.words)
        end = words[-1].end_s if words else start
        if sv.duration_s is not None:
            end = max(end, start + sv.duration_s)
        subtitles = tuple(split_cues(words, max_lines, max_chars, font_scale))
        scenes.append(SceneTiming(scene_id=sv.scene_id, shot=sv.shot, start_s=start, end_s=end, words=words, subtitles=subtitles))
        t = end
    return Timeline(scenes=tuple(scenes))


def timeline_to_dict(timeline: Timeline) -> dict:
    return {
        "scenes": [
            {
                "scene_id": s.scene_id,
                "shot": s.shot,
                "start_s": s.start_s,
                "end_s": s.end_s,
                "words": [{"text": w.text, "start_s": w.start_s, "end_s": w.end_s} for w in s.words],
                "subtitles": [
                    {"lines": list(c.lines), "start_s": c.start_s, "end_s": c.end_s} for c in s.subtitles
                ],
            }
            for s in timeline.scenes
        ]
    }


def timeline_from_dict(data: dict) -> Timeline:
    scenes = []
    for s in data["scenes"]:
        words = tuple(Word(text=w["text"], start_s=w["start_s"], end_s=w["end_s"]) for w in s["words"])
        subs = tuple(
            SubtitleCue(lines=tuple(c["lines"]), start_s=c["start_s"], end_s=c["end_s"]) for c in s["subtitles"]
        )
        scenes.append(
            SceneTiming(scene_id=s["scene_id"], shot=s["shot"], start_s=s["start_s"], end_s=s["end_s"], words=words, subtitles=subs)
        )
    return Timeline(scenes=tuple(scenes))


def save_timeline(timeline: Timeline, path: Path) -> None:
    Path(path).write_text(json.dumps(timeline_to_dict(timeline), indent=2))


def load_timeline(path: Path) -> Timeline:
    """Reads back a Timeline written by `save_timeline` -- or the richer
    timeline.json a `studio build` package writes, which has the same
    top-level `scenes` key plus a `keyframes` list this function ignores."""
    return timeline_from_dict(json.loads(Path(path).read_text()))
