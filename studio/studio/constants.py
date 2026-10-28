LOGICAL_W = 270
LOGICAL_H = 480
SCALE = 4
OUTPUT_W = LOGICAL_W * SCALE
OUTPUT_H = LOGICAL_H * SCALE
FPS = 30

AI_VOICE_PLATE_S = 3.0
"""format.md: the 'AI · AI voice' plate shows top-of-frame for the first 3s
of the whole video; a small 'AI' icon persists after that."""

DEFAULT_WPM = 144.0
"""The af_sarah pace at Kokoro speed 1.1 (format.md 'Длительность и темп',
recalibrated 27.10): the `stub` voice and `uniform_timeline` read at it so a
stub build lasts about as long as the real voice would."""

SERIOUS_WPM = 131.0
"""format.md 'Серьёзный режим': Kokoro speed 1.0, about 2.2 words/s."""

LOUDNESS_LUFS = -14.0
LOUDNESS_TOLERANCE_LU = 1.0
MAX_TRUE_PEAK_DBTP = -1.0
"""docs/architecture.md §8: every soundtrack at about -14 LUFS, true peak at
most -1 dBTP -- channel policy, not a studio setting, so config.json can't
move what `studio validate` accepts."""

MAX_SILENCE_S = 1.5
SILENCE_THRESHOLD_DB = -50.0
"""docs/architecture.md §8: no pause of silence (quieter than the threshold)
longer than this."""

YOUTUBE_MUSIC_MAX_S = 60.0
"""format.md 'Музыка и длина': a video longer than this with music also gets
`video_no_music.mp4` -- the YouTube cut, same picture, no music."""

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
