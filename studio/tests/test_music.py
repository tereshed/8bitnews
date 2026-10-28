"""Picking the music (studio.music): catalog rules, --music-file/--music-track."""
import hashlib

import pytest

from studio.errors import BuildError
from studio.music import choose_music, select_track, synth_test_track

PLATFORMS = ["youtube", "tiktok", "instagram"]
PROFILE = {"platforms": {p: {"enabled": True} for p in PLATFORMS}}


def _track(track_id, mood, energy, status="approved", platforms=PLATFORMS, commercial=True, key=None, sha=None):
    return {
        "id": track_id,
        "status": status,
        "license": {"name": "CC0-1.0", "commercial": commercial, "platforms": platforms, "attribution": None},
        "audio": {"key": key or f"music/tracks/{track_id}.wav", "sha256": sha},
        "tags": {"mood": mood, "energy": energy},
    }


CATALOG = [
    _track("calm-a", ["neutral"], 2),
    _track("calm-b", ["neutral", "curious"], 4),
    _track("tense-a", ["tense"], 3),
    _track("tense-unapproved", ["tense"], 3, status="candidate"),
    _track("tense-no-tiktok", ["tense"], 3, platforms=["youtube", "instagram"]),
    _track("tense-noncommercial", ["tense"], 3, commercial=False),
]


def test_mood_picks_the_track():
    assert select_track({"mood": "neutral", "energy": 2}, PLATFORMS, CATALOG)["id"] == "calm-a"
    assert select_track({"mood": "tense", "energy": 3}, PLATFORMS, CATALOG)["id"] == "tense-a"


def test_energy_picks_the_nearest_track():
    assert select_track({"mood": "neutral", "energy": 4}, PLATFORMS, CATALOG)["id"] == "calm-b"
    assert select_track({"mood": "neutral", "energy": 1}, PLATFORMS, CATALOG)["id"] == "calm-a"


def test_unapproved_or_unlicensed_tracks_are_never_picked():
    catalog = [t for t in CATALOG if t["id"] != "tense-a"]
    with pytest.raises(BuildError, match="no approved track"):
        select_track({"mood": "tense", "energy": 3}, PLATFORMS, catalog)


def test_explicit_track_must_be_licensed_for_every_platform():
    assert select_track({}, PLATFORMS, CATALOG, track_id="tense-a")["id"] == "tense-a"
    with pytest.raises(BuildError, match="not approved and licensed"):
        select_track({}, PLATFORMS, CATALOG, track_id="tense-no-tiktok")


def _script(enabled=True, **music):
    return {"music": {"enabled": enabled, "mood": "tense", "energy": 3, "cue": "under the whole piece", **music}}


def _catalog_file(tmp_path, tracks):
    import yaml

    path = tmp_path / "catalog.yaml"
    path.write_text(yaml.safe_dump({"tracks": tracks}))
    return path


def test_script_without_music_gets_none_and_rejects_a_music_file(tmp_path):
    assert choose_music(_script(enabled=False), PROFILE) is None
    with pytest.raises(BuildError, match="script.music.enabled is false"):
        choose_music(_script(enabled=False), PROFILE, music_file=tmp_path / "x.wav")


def test_music_file_alone_is_unverified(tmp_path):
    track = synth_test_track(tmp_path / "loop.wav", bars=1)
    choice = choose_music(_script(), PROFILE, music_file=track)
    assert choice.path == track
    assert choice.license == "unverified" and "not for publishing" in choice.note


def test_catalog_track_file_comes_from_music_dir_and_is_checked(tmp_path):
    audio = synth_test_track(tmp_path / "music" / "tracks" / "tense-a.wav", bars=1)
    sha = hashlib.sha256(audio.read_bytes()).hexdigest()
    catalog = _catalog_file(tmp_path, [_track("tense-a", ["tense"], 3, key="music/tracks/tense-a.wav", sha=sha)])
    choice = choose_music(_script(), PROFILE, catalog_path=catalog, music_dir=tmp_path)
    assert (choice.track_id, choice.license, choice.path) == ("tense-a", "CC0-1.0", audio)

    bad = _catalog_file(tmp_path, [_track("tense-a", ["tense"], 3, key="music/tracks/tense-a.wav", sha="0" * 64)])
    with pytest.raises(BuildError, match="does not match the catalog sha256"):
        choose_music(_script(), PROFILE, catalog_path=bad, music_dir=tmp_path)


def test_catalog_track_needs_a_music_dir(tmp_path, monkeypatch):
    monkeypatch.delenv("STUDIO_MUSIC_DIR", raising=False)
    catalog = _catalog_file(tmp_path, [_track("tense-a", ["tense"], 3)])
    with pytest.raises(BuildError, match="STUDIO_MUSIC_DIR"):
        choose_music(_script(), PROFILE, catalog_path=catalog)


def test_music_file_with_track_must_be_that_tracks_audio(tmp_path):
    audio = synth_test_track(tmp_path / "a.wav", bars=1)
    other = synth_test_track(tmp_path / "b.wav", bars=2)
    sha = hashlib.sha256(audio.read_bytes()).hexdigest()
    catalog = _catalog_file(tmp_path, [_track("tense-a", ["tense"], 3, sha=sha)])
    assert choose_music(_script(), PROFILE, music_file=audio, track_id="tense-a", catalog_path=catalog).track_id == "tense-a"
    with pytest.raises(BuildError, match="sha256 differs"):
        choose_music(_script(), PROFILE, music_file=other, track_id="tense-a", catalog_path=catalog)


def test_script_mood_changes_the_chosen_file(tmp_path):
    tracks = []
    for tid, mood in (("calm-a", "neutral"), ("tense-a", "tense")):
        audio = synth_test_track(tmp_path / "music" / f"{tid}.wav", bars=1 if mood == "neutral" else 2)
        tracks.append(_track(tid, [mood], 3, key=f"music/{tid}.wav", sha=hashlib.sha256(audio.read_bytes()).hexdigest()))
    catalog = _catalog_file(tmp_path, tracks)
    calm = choose_music(_script(mood="neutral"), PROFILE, catalog_path=catalog, music_dir=tmp_path)
    tense = choose_music(_script(mood="tense"), PROFILE, catalog_path=catalog, music_dir=tmp_path)
    assert calm.track_id == "calm-a" and tense.track_id == "tense-a" and calm.path != tense.path


def test_real_catalog_has_a_licensed_track_for_every_script_mood():
    """music/catalog.yaml covers every mood script.schema allows."""
    from studio import repo
    from studio.music import load_catalog

    try:
        catalog = load_catalog()
    except repo.RepoRootNotSet:
        pytest.skip("needs STUDIO_REPO_ROOT")
    for mood in ("neutral", "curious", "tense", "urgent", "somber", "hopeful"):
        assert select_track({"mood": mood, "energy": 3}, PLATFORMS, catalog)["status"] == "approved"
