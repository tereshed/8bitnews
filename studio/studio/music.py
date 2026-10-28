"""Music for `studio build`: picking a track from music/catalog.yaml by the
script's mood and energy (or taking `--music-file`), plus the sounds the
studio synthesizes itself -- the test track for demos and tests, and the
transition sound effects the templates ship (studio/README.md, «Звук»).

Track audio is not in git: a track's file is `$STUDIO_MUSIC_DIR/<audio.key>`
and must match the catalog's sha256.
"""
from __future__ import annotations

import hashlib
import os
import wave
from dataclasses import dataclass
from pathlib import Path

import numpy as np
import yaml

from . import repo
from .errors import BuildError


@dataclass(frozen=True)
class MusicChoice:
    path: Path
    track_id: str
    license: str
    attribution: str | None = None
    note: str | None = None


def load_catalog(path: Path | None = None) -> list[dict]:
    path = Path(path) if path else repo.repo_root() / "music" / "catalog.yaml"
    with open(path) as f:
        return yaml.safe_load(f).get("tracks", [])


def _licensed_for(track: dict, platforms: list[str]) -> bool:
    lic = track.get("license", {})
    return track.get("status") == "approved" and lic.get("commercial") is True and set(platforms) <= set(
        lic.get("platforms", [])
    )


def select_track(music_cfg: dict, platforms: list[str], catalog: list[dict], track_id: str | None = None) -> dict:
    """`track_id` (hub's music_pick) or the approved track licensed for every
    platform whose mood tags include the script's mood, nearest in energy
    (ties by id, so the same script always gets the same track)."""
    if track_id:
        track = next((t for t in catalog if t["id"] == track_id), None)
        if track is None:
            raise BuildError(f"music track {track_id!r} is not in the catalog")
        if not _licensed_for(track, platforms):
            raise BuildError(f"music track {track_id!r} is not approved and licensed for {', '.join(platforms)}")
        return track
    mood, energy = music_cfg.get("mood"), music_cfg.get("energy", 3)
    fits = [t for t in catalog if _licensed_for(t, platforms) and mood in t.get("tags", {}).get("mood", [])]
    if not fits:
        raise BuildError(f"no approved track for mood {mood!r} licensed for {', '.join(platforms)} in the catalog")
    return min(fits, key=lambda t: (abs(t.get("tags", {}).get("energy", 3) - energy), t["id"]))


def _sha256(path: Path) -> str:
    h = hashlib.sha256()
    with open(path, "rb") as f:
        for chunk in iter(lambda: f.read(1 << 20), b""):
            h.update(chunk)
    return h.hexdigest()


def track_file(track: dict, music_dir: Path | None = None) -> Path:
    base = music_dir or (Path(os.environ["STUDIO_MUSIC_DIR"]) if os.environ.get("STUDIO_MUSIC_DIR") else None)
    if base is None:
        raise BuildError(f"music track {track['id']!r}: set STUDIO_MUSIC_DIR to the folder holding the catalog's "
                         "audio keys, or pass --music-file")
    path = Path(base) / track["audio"]["key"]
    if not path.is_file():
        raise BuildError(f"music track {track['id']!r}: {path} not found")
    if track["audio"].get("sha256") and _sha256(path) != track["audio"]["sha256"]:
        raise BuildError(f"music track {track['id']!r}: {path} does not match the catalog sha256")
    return path


def choose_music(script: dict, channel_profile: dict, music_file: Path | None = None, track_id: str | None = None,
                 catalog_path: Path | None = None, music_dir: Path | None = None) -> MusicChoice | None:
    """None when the script asks for no music. `music_file` alone is used as
    is and recorded with license "unverified" (a demo or test file, not for
    publishing); with `track_id` it must be that catalog track's audio."""
    if not script.get("music", {}).get("enabled"):
        if music_file or track_id:
            raise BuildError("--music-file/--music-track given, but script.music.enabled is false")
        return None
    platforms = [p for p, cfg in channel_profile.get("platforms", {}).items() if cfg.get("enabled")]
    if music_file and not track_id:
        music_file = Path(music_file)
        if not music_file.is_file():
            raise BuildError(f"--music-file {music_file} not found")
        return MusicChoice(path=music_file, track_id=f"file:{music_file.name}", license="unverified",
                           note=f"music is --music-file {music_file.name} with no catalog track: its license is "
                                "unverified, not for publishing")
    track = select_track(script["music"], platforms, load_catalog(catalog_path), track_id)
    if music_file:
        path = Path(music_file)
        if track["audio"].get("sha256") and _sha256(path) != track["audio"]["sha256"]:
            raise BuildError(f"--music-file {path} is not the audio of track {track['id']!r} (sha256 differs)")
    else:
        path = track_file(track, music_dir)
    lic = track["license"]
    return MusicChoice(path=path, track_id=track["id"], license=lic.get("name") or "see catalog",
                       attribution=lic.get("attribution"))


# --- sounds the studio makes itself ------------------------------------------------

def _write_wav(path: Path, samples: np.ndarray, sample_rate: int) -> None:
    samples = samples[:, None] if samples.ndim == 1 else samples
    pcm = (np.clip(samples, -1, 1) * 32767).astype(np.int16)
    path.parent.mkdir(parents=True, exist_ok=True)
    with wave.open(str(path), "wb") as f:
        f.setnchannels(pcm.shape[1])
        f.setsampwidth(2)
        f.setframerate(sample_rate)
        f.writeframes(pcm.tobytes())


def _square(freq: float, t: np.ndarray, duty: float) -> np.ndarray:
    """A pulse wave centred on zero (a narrow pulse alone carries DC)."""
    return np.where((t * freq) % 1.0 < duty, 1.0, -1.0) - (2 * duty - 1)


def synth_test_track(path: Path, bars: int = 8, sample_rate: int = 48000) -> Path:
    """A plain 8-bit loop (square lead, triangle bass, noise hats) made from
    nothing but code: a stand-in bed for demos and tests when the catalog's
    audio isn't around. Deterministic."""
    bpm, beats = 112.0, bars * 4
    step = 60.0 / bpm / 2  # eighth notes
    n_steps = beats * 2
    total = int(n_steps * step * sample_rate)
    out = np.zeros((total, 2))
    chords = [(57, 60, 64), (53, 57, 60), (48, 52, 55), (55, 59, 62)]  # Am F C G
    rng = np.random.RandomState(7)
    noise = rng.uniform(-1, 1, int(step * sample_rate) + 2)
    for i in range(n_steps):
        a, b = int(i * step * sample_rate), int((i + 1) * step * sample_rate)
        t = np.arange(b - a) / sample_rate
        chord = chords[(i // 8) % len(chords)]
        lead = 440.0 * 2 ** ((chord[i % 3] + 12 - 69) / 12)
        bass = 440.0 * 2 ** ((chord[0] - 12 - 69) / 12)
        env = np.exp(-t * 7)
        out[a:b, 0] += 0.20 * _square(lead, t, 0.25) * env
        tri = 2 * np.abs(2 * ((t * bass) % 1) - 1) - 1
        out[a:b, :] += (0.30 * tri)[:, None]
        if i % 2 == 1:
            out[a:b, 1] += 0.08 * noise[: b - a] * np.exp(-t * 40)
    out[:, 1] += 0.5 * out[:, 0]
    out[:, 0] += 0.3 * out[:, 1]
    _write_wav(Path(path), out / np.abs(out).max() * 0.7, sample_rate)
    return Path(path)


def synth_transition_sound(path: Path, sample_rate: int = 48000) -> Path:
    """The `pixel-wipe` transition blip: a falling square-wave arpeggio,
    0.22 s, mono. Deterministic."""
    notes = [88, 84, 79, 76]
    seg = int(0.055 * sample_rate)
    parts = []
    for k, note in enumerate(notes):
        t = np.arange(seg) / sample_rate
        f = 440.0 * 2 ** ((note - 69) / 12)
        parts.append(0.45 * _square(f, t, 0.125) * np.exp(-t * 18) * (1 - 0.15 * k))
    out = np.concatenate(parts)
    fade = int(0.004 * sample_rate)
    out[:fade] *= np.linspace(0, 1, fade)
    out[-fade:] *= np.linspace(1, 0, fade)
    _write_wav(Path(path), out, sample_rate)
    return Path(path)
