from studio.timeline import (
    SceneVoice,
    Timeline,
    Word,
    load_timeline,
    save_timeline,
    timeline_from_voice,
)


def _scene_voice(scene_id: str, shot: str, texts: list[str], word_dur: float = 0.5) -> SceneVoice:
    words = tuple(Word(text=t, start_s=i * word_dur, end_s=(i + 1) * word_dur) for i, t in enumerate(texts))
    return SceneVoice(scene_id=scene_id, shot=shot, words=words)


def test_timeline_from_voice_has_no_silence_at_start_or_end():
    scenes = [_scene_voice("S1", "anchor", ["Hi"]), _scene_voice("S2", "scene", ["Bye"])]
    timeline = timeline_from_voice(scenes, pause_s=0.3, max_lines=2, max_chars=32)
    assert timeline.scenes[0].start_s == 0.0
    assert timeline.duration_s == timeline.scenes[-1].end_s  # no trailing pause either


def test_timeline_from_voice_inserts_pause_between_scenes_only():
    scenes = [
        _scene_voice("S1", "anchor", ["One"], word_dur=1.0),
        _scene_voice("S2", "scene", ["Two"], word_dur=1.0),
        _scene_voice("S3", "scene", ["Three"], word_dur=1.0),
    ]
    timeline = timeline_from_voice(scenes, pause_s=0.4, max_lines=2, max_chars=32)
    assert timeline.scenes[0].start_s == 0.0
    assert timeline.scenes[0].end_s == 1.0
    assert timeline.scenes[1].start_s == 1.4  # 1.0 + pause
    assert timeline.scenes[1].end_s == 2.4
    assert timeline.scenes[2].start_s == 2.8  # 2.4 + pause


def test_timeline_from_voice_pause_changes_total_duration():
    scenes = [_scene_voice("S1", "anchor", ["One"]), _scene_voice("S2", "scene", ["Two"])]
    short_pause = timeline_from_voice(scenes, pause_s=0.1, max_lines=2, max_chars=32)
    long_pause = timeline_from_voice(scenes, pause_s=0.9, max_lines=2, max_chars=32)
    assert long_pause.duration_s > short_pause.duration_s


def test_timeline_round_trips_through_save_and_load(tmp_path):
    scenes = [
        _scene_voice("S1", "anchor", ["Hello", "there."]),
        _scene_voice("S2", "scene", ["Goodbye."]),
    ]
    timeline = timeline_from_voice(scenes, pause_s=0.3, max_lines=2, max_chars=32)

    path = tmp_path / "timeline.json"
    save_timeline(timeline, path)
    loaded = load_timeline(path)

    assert isinstance(loaded, Timeline)
    assert loaded.duration_s == timeline.duration_s
    assert len(loaded.scenes) == len(timeline.scenes)
    for a, b in zip(timeline.scenes, loaded.scenes):
        assert a.scene_id == b.scene_id
        assert a.shot == b.shot
        assert a.start_s == b.start_s
        assert a.end_s == b.end_s
        assert [(w.text, w.start_s, w.end_s) for w in a.words] == [(w.text, w.start_s, w.end_s) for w in b.words]
        assert [(c.lines, c.start_s, c.end_s) for c in a.subtitles] == [(c.lines, c.start_s, c.end_s) for c in b.subtitles]


def test_load_timeline_ignores_extra_top_level_keys(tmp_path):
    """The package's real timeline.json also carries a `keyframes` list;
    load_timeline must still parse it back via the `scenes` key alone."""
    import json

    scenes = [_scene_voice("S1", "anchor", ["Hi"])]
    timeline = timeline_from_voice(scenes, pause_s=0.3, max_lines=2, max_chars=32)
    path = tmp_path / "timeline.json"
    save_timeline(timeline, path)
    data = json.loads(path.read_text())
    data["keyframes"] = [{"path": "keyframes/S1_first.png", "scene_id": "S1", "kind": "first", "frame": 0, "t_s": 0.0}]
    path.write_text(json.dumps(data))

    loaded = load_timeline(path)
    assert loaded.duration_s == timeline.duration_s
