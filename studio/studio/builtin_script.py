"""The built-in 3-scene demo script used by `studio template preview` and by
the test suite: an anchor scene, a scene with a real person (must show the
'AI illustration' plate), and a scene without one (must not)."""
from __future__ import annotations

from .timeline import Timeline, uniform_timeline

BUILTIN_SCRIPT: dict = {
    "channel_id": "8bitnews-us",
    "language": "en-US",
    "duration_s": 11,
    "word_count": 31,
    "serious_mode": False,
    "hook_type": "new_fact",
    "music": {"enabled": False},
    "scenes": [
        {
            "id": "S1",
            "blocks": ["hook"],
            "shot": "anchor",
            "duration_s": 3,
            "narration": "Template preview rendered by the studio engine.",
            "word_count": 7,
            "on_screen_text": "Template preview",
            "visual": "The anchor sits at the studio desk and introduces the segment.",
            "real_people": [],
            "fact_ids": [],
        },
        {
            "id": "S2",
            "blocks": ["whats_new"],
            "shot": "scene",
            "duration_s": 4,
            "narration": "A real person appears here, so the AI illustration plate stays on screen.",
            "word_count": 13,
            "on_screen_text": "Real person on screen",
            "source_plate": "Source: studio preview · placeholder",
            "visual": "A stylized pixel-art depiction of a real person speaking at a podium.",
            "real_people": ["Preview Person"],
            "fact_ids": ["F1"],
        },
        {
            "id": "S3",
            "blocks": ["known"],
            "shot": "scene",
            "duration_s": 4,
            "narration": "This scene has no real people, so no illustration plate appears.",
            "word_count": 11,
            "on_screen_text": "No real people here",
            "visual": "A generic illustrative scene with charts and no real people.",
            "real_people": [],
            "fact_ids": ["F1"],
        },
    ],
    "metadata": {
        "youtube": {
            "title": "Studio engine preview - nes-desk template",
            "summary": "A short internal preview rendered by the studio engine to check the nes-desk template package.",
            "hashtags": ["#preview"],
        }
    },
    "cover": {"frame": "S1", "visual": "The anchor at the studio desk, template preview cover frame", "text": "Template preview"},
}


def builtin_timeline(wpm: float | None = None, subtitle_style: dict | None = None) -> Timeline:
    return uniform_timeline(BUILTIN_SCRIPT, wpm=wpm, subtitle_style=subtitle_style)
