LOGICAL_W = 270
LOGICAL_H = 480
SCALE = 4
OUTPUT_W = LOGICAL_W * SCALE
OUTPUT_H = LOGICAL_H * SCALE
FPS = 30

AI_VOICE_PLATE_S = 3.0
"""format.md: the 'AI · AI voice' plate shows top-of-frame for the first 3s
of the whole video; a small 'AI' icon persists after that."""

DEFAULT_WPM = 165.0
"""format.md 'Длительность и темп': ~2.7 words/s == ~165 wpm, the channel's default pace."""

SERIOUS_WPM = 2.4 * 60.0
"""format.md 'Серьёзный режим': slower, ~2.4 words/s."""

FONT_PX = 8

MAX_SUBTITLE_LINES = 2
MAX_SUBTITLE_CHARS_PER_LINE = 23
"""format.md caps subtitle lines at "до 32 символов" assuming the full
270px logical width; the channel's actual common safe rect (common to
YouTube/TikTok/Instagram, channels/8bitnews-us/safe_zones.json) is 190px
wide, which at the 8px pixel font fits 23 monospace characters after
padding. The timeline (studio.timeline) wraps subtitles once, independent
of which template renders them, so this is a fixed channel-wide ceiling
rather than a per-template computation -- 23 stays within format.md's
"up to 32" cap.
"""
