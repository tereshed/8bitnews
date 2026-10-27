"""The timeline is a standalone data structure, independent of how it was
produced: scenes with start/end, subtitle cues with words, and a voice
volume envelope the anchor's mouth animates against (docs: 'Таймлайн —
отдельная структура данных'). In E2a-2 a TTS adapter will produce one from
real audio; `uniform_timeline` below produces one from word count and a
words-per-minute pace, for preview and tests without any TTS.
"""
from __future__ import annotations

from dataclasses import dataclass, field

from .constants import DEFAULT_WPM, SERIOUS_WPM
from .subtitles import SubtitleCue, split_subtitles


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
        for scene in self.scenes:
            if scene.start_s <= t_abs < scene.end_s:
                return scene
        if self.scenes and t_abs >= self.scenes[-1].end_s:
            return self.scenes[-1]
        return None


_MIN_SCENE_DURATION_S = 1.0


def uniform_timeline(script: dict, wpm: float | None = None) -> Timeline:
    """Builds a Timeline purely from each scene's narration word count and a
    fixed words-per-minute pace (no audio): format.md's own word budget is
    defined this way (~165 wpm, ~2.4 words/s in serious mode), so this is
    also the right stand-in for tests and template preview.
    """
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
        subtitles = tuple(split_subtitles(scene["narration"], scene_start, scene_end))
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
