import shutil
import sys
from pathlib import Path

STUDIO_DIR = Path(__file__).resolve().parent.parent
if str(STUDIO_DIR) not in sys.path:
    sys.path.insert(0, str(STUDIO_DIR))

import pytest  # noqa: E402

from studio.build import build_package  # noqa: E402
from studio.paths import TEMPLATES_DIR  # noqa: E402

FIXTURES_DIR = Path(__file__).resolve().parent / "fixtures"


@pytest.fixture
def nes_desk_dir() -> Path:
    return TEMPLATES_DIR / "nes-desk"


@pytest.fixture
def night_report_dir() -> Path:
    return TEMPLATES_DIR / "night-report"


@pytest.fixture
def bad_safezone_dir() -> Path:
    return FIXTURES_DIR / "bad-safezone"


@pytest.fixture
def bad_palette_dir() -> Path:
    return FIXTURES_DIR / "bad-palette"


@pytest.fixture
def missing_license_note_dir() -> Path:
    return FIXTURES_DIR / "missing-license-note"


@pytest.fixture
def bad_face_overlap_dir() -> Path:
    return FIXTURES_DIR / "bad-face-overlap"


@pytest.fixture
def bad_plate_too_small_dir() -> Path:
    return FIXTURES_DIR / "bad-plate-too-small"


@pytest.fixture
def bad_referenced_file_dir() -> Path:
    return FIXTURES_DIR / "bad-referenced-file"


@pytest.fixture
def short_script_path() -> Path:
    return FIXTURES_DIR / "scripts" / "short.json"


@pytest.fixture
def test_channels_dir() -> Path:
    return FIXTURES_DIR / "channels"


@pytest.fixture(scope="session")
def built_package(tmp_path_factory) -> dict:
    """One full `studio build` of the short fixture script, for the whole
    test session: the build itself (and encoding video.mp4 under ffmpeg) is
    the slow part, so every test that only inspects the *result* reuses
    this instead of re-building."""
    out_dir = tmp_path_factory.mktemp("studio-build-session")
    package_path = build_package(
        script_path=FIXTURES_DIR / "scripts" / "short.json",
        channel_id="8bitnews-us",
        out_dir=out_dir,
        channels_dir=FIXTURES_DIR / "channels",
        tts_name="stub",
    )
    import json

    return {"out_dir": out_dir, "package_path": package_path, "package": json.loads(package_path.read_text())}


# A video counts as "long" (gets video_no_music.mp4) above this in the music
# fixtures, so a 15 s test build exercises the YouTube cut.
SHORT_YOUTUBE_LIMIT_S = 5.0


def music_script(tmp_dir: Path, **music) -> Path:
    """short.json with music switched on (mood/energy overridable)."""
    import json

    script = json.loads((FIXTURES_DIR / "scripts" / "short.json").read_text())
    script["music"] = {"enabled": True, "mood": "neutral", "energy": 2,
                       "cue": "Soft bed under the whole piece, fades out at the end.", **music}
    path = Path(tmp_dir) / "short_music.json"
    path.write_text(json.dumps(script, indent=2))
    return path


@pytest.fixture(scope="session")
def test_track(tmp_path_factory) -> Path:
    from studio.music import synth_test_track

    return synth_test_track(tmp_path_factory.mktemp("music") / "track.wav")


def build_with_music(tmp_dir: Path, track: Path, **kwargs) -> dict:
    """A build of short.json with music (stub voice unless `tts_name` says
    otherwise), counted as a long video (SHORT_YOUTUBE_LIMIT_S) so it also
    gets video_no_music.mp4."""
    import json

    from studio import constants

    out_dir = Path(tmp_dir) / "out"
    options = {"channels_dir": FIXTURES_DIR / "channels", "tts_name": "stub", **kwargs}
    with pytest.MonkeyPatch.context() as mp:
        mp.setattr(constants, "YOUTUBE_MUSIC_MAX_S", SHORT_YOUTUBE_LIMIT_S)
        package_path = build_package(
            script_path=music_script(tmp_dir),
            channel_id="8bitnews-us",
            out_dir=out_dir,
            music_file=track,
            **options,
        )
    return {"out_dir": out_dir, "package_path": package_path, "package": json.loads(package_path.read_text())}


@pytest.fixture(scope="session")
def music_package(tmp_path_factory, test_track) -> dict:
    """short.json with music: video.mp4, video_no_music.mp4 and both stems."""
    return build_with_music(tmp_path_factory.mktemp("studio-build-music"), test_track)


@pytest.fixture
def short_youtube_limit(monkeypatch):
    """Checks see the music fixtures' 15 s build as a long video too."""
    from studio import constants

    monkeypatch.setattr(constants, "YOUTUBE_MUSIC_MAX_S", SHORT_YOUTUBE_LIMIT_S)


def _corrupt_copy(pkg: dict, fn, rehash: bool = True) -> Path:
    """Copies the built package directory, applies `fn(copy_dir, package)`
    to corrupt it, and recomputes sha256/bytes of every file record (`files`
    and scenes' keyframes) so only the check under test sees the damage --
    the shared helper every registry-check negative test uses.
    `rehash=False` leaves the records stale (for the package_schema check)."""
    import hashlib
    import json

    src_dir: Path = pkg["out_dir"]
    copy_dir = src_dir.parent / f"{src_dir.name}-corrupt-{id(fn)}"
    if copy_dir.exists():
        shutil.rmtree(copy_dir)
    shutil.copytree(src_dir, copy_dir)

    package = json.loads((copy_dir / "package.json").read_text())
    fn(copy_dir, package)

    entries = list(package["files"].values()) + [kf for scene in package["scenes"] for kf in scene["keyframes"]]
    for entry in entries if rehash else []:
        path = copy_dir / entry["path"]
        h = hashlib.sha256()
        size = 0
        with open(path, "rb") as f:
            for chunk in iter(lambda: f.read(1 << 20), b""):
                h.update(chunk)
                size += len(chunk)
        entry["sha256"] = h.hexdigest()
        entry["bytes"] = size

    (copy_dir / "package.json").write_text(json.dumps(package, indent=2))
    return copy_dir


@pytest.fixture
def corrupt_copy():
    return _corrupt_copy
