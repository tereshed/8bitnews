"""Synthesizes the transition sound effects the template packages ship
(studio.music.synth_transition_sound) and writes each one's license note
next to it -- format.md allows only our own or CC0 sounds.

Run from the studio/ directory: `python3 scripts/generate_sounds.py`
(needs numpy; the Docker image or the dev venv both have it).
"""
from __future__ import annotations

import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
sys.path.insert(0, str(ROOT))

from studio.music import synth_transition_sound  # noqa: E402

LICENSE = (
    "pixel-wipe.wav: original sound synthesized for this repository by "
    "studio/scripts/generate_sounds.py (studio.music.synth_transition_sound), "
    "a square-wave arpeggio computed in code. No third-party material. "
    "Dedicated to the public domain under CC0-1.0; commercial use on every platform.\n"
)


def main() -> None:
    out = ROOT / "templates" / "nes-desk" / "assets" / "sfx" / "pixel-wipe.wav"
    synth_transition_sound(out)
    (out.parent / (out.name + ".license.txt")).write_text(LICENSE)
    print(f"wrote {out}")


if __name__ == "__main__":
    main()
