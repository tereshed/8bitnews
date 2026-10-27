import json
import shutil

import yaml

from studio.checks import run_checks


def _channel_profile(test_channels_dir):
    with open(test_channels_dir / "8bitnews-us" / "channel.yaml") as f:
        return yaml.safe_load(f)


def test_all_checks_pass_on_the_freshly_built_package(built_package, test_channels_dir):
    package = built_package["package"]
    assert package["validate"]["passed"] is True
    ids = {c["id"] for c in package["validate"]["checks"]}
    assert ids == {"format", "duration", "file_size", "subtitles_cover_words"}
    assert all(c["ok"] for c in package["validate"]["checks"])


def test_format_check_fails_on_wrong_codec(built_package, test_channels_dir, corrupt_copy):
    def corrupt(copy_dir, package):
        shutil.copyfile(copy_dir / "cover.png", copy_dir / package["files"]["video"]["path"])

    copy_dir = corrupt_copy(built_package, corrupt)
    package = json.loads((copy_dir / "package.json").read_text())
    result = run_checks(copy_dir, package, _channel_profile(test_channels_dir))
    check = next(c for c in result["checks"] if c["id"] == "format")
    assert check["ok"] is False
    assert check["note"]


def test_duration_check_fails_when_package_duration_disagrees_with_video(built_package, test_channels_dir, corrupt_copy):
    def corrupt(copy_dir, package):
        package["duration_s"] = 9999.0

    copy_dir = corrupt_copy(built_package, corrupt)
    package = json.loads((copy_dir / "package.json").read_text())
    result = run_checks(copy_dir, package, _channel_profile(test_channels_dir))
    check = next(c for c in result["checks"] if c["id"] == "duration")
    assert check["ok"] is False
    assert "differs from package.duration_s" in check["note"] and "9999.00s" in check["note"]
    assert "outside channel range" not in check["note"]  # the measured video is in range


def test_file_size_check_fails_over_the_threshold(built_package, test_channels_dir, corrupt_copy):
    def corrupt(copy_dir, package):
        path = copy_dir / package["files"]["video"]["path"]
        with open(path, "wb") as f:
            f.seek(60 * 1024 * 1024 - 1)
            f.write(b"\0")

    copy_dir = corrupt_copy(built_package, corrupt)
    package = json.loads((copy_dir / "package.json").read_text())
    result = run_checks(copy_dir, package, _channel_profile(test_channels_dir))
    check = next(c for c in result["checks"] if c["id"] == "file_size")
    assert check["ok"] is False
    assert check["note"]


def test_subtitles_cover_words_check_fails_on_uncovered_word(built_package, test_channels_dir, corrupt_copy):
    def corrupt(copy_dir, package):
        timeline_path = copy_dir / package["files"]["timeline"]["path"]
        data = json.loads(timeline_path.read_text())
        data["scenes"][0]["words"].append({"text": "orphan", "start_s": 9999.0, "end_s": 9999.5})
        timeline_path.write_text(json.dumps(data))

    copy_dir = corrupt_copy(built_package, corrupt)
    package = json.loads((copy_dir / "package.json").read_text())
    result = run_checks(copy_dir, package, _channel_profile(test_channels_dir))
    check = next(c for c in result["checks"] if c["id"] == "subtitles_cover_words")
    assert check["ok"] is False
    assert "orphan" in check["note"]


# --- subtitles_cover_words: SRT text and timing against timeline.json ---


class _Cue:
    def __init__(self, start_s, end_s, text):
        self.start_s, self.end_s, self.lines = start_s, end_s, tuple(text.split("\n"))


def _rewrite_srt(copy_dir, package, fn):
    """fn(list[_Cue]) -> list[_Cue]: edits subtitles.srt cue by cue."""
    from studio.srt import parse_srt, write_srt

    path = copy_dir / package["files"]["subtitles"]["path"]
    cues = [_Cue(c.start_s, c.end_s, c.text) for c in parse_srt(path)]
    write_srt(fn(cues), path)


def _subtitles_check(built_package, test_channels_dir, corrupt_copy, corrupt):
    copy_dir = corrupt_copy(built_package, corrupt)
    package = json.loads((copy_dir / "package.json").read_text())
    result = run_checks(copy_dir, package, _channel_profile(test_channels_dir))
    return next(c for c in result["checks"] if c["id"] == "subtitles_cover_words")


def test_subtitles_check_fails_when_cue_texts_are_replaced(built_package, test_channels_dir, corrupt_copy):
    def corrupt(copy_dir, package):
        def fn(cues):
            for c in cues:
                c.lines = ("lorem ipsum",)
            return cues

        _rewrite_srt(copy_dir, package, fn)

    check = _subtitles_check(built_package, test_channels_dir, corrupt_copy, corrupt)
    assert check["ok"] is False
    assert "lorem ipsum" in check["note"]


def test_subtitles_check_fails_when_cue_texts_shift_by_one(built_package, test_channels_dir, corrupt_copy):
    def corrupt(copy_dir, package):
        def fn(cues):
            texts = [c.lines for c in cues]
            for c, lines in zip(cues, texts[1:] + texts[:1]):
                c.lines = lines
            return cues

        _rewrite_srt(copy_dir, package, fn)

    check = _subtitles_check(built_package, test_channels_dir, corrupt_copy, corrupt)
    assert check["ok"] is False
    assert "SRT cue 1 " in check["note"]


def test_subtitles_check_fails_on_one_replaced_word(built_package, test_channels_dir, corrupt_copy):
    def corrupt(copy_dir, package):
        def fn(cues):
            first, *rest = cues[1].lines[0].split(" ")
            cues[1].lines = (" ".join(["Replaced", *rest]), *cues[1].lines[1:])
            return cues

        _rewrite_srt(copy_dir, package, fn)

    check = _subtitles_check(built_package, test_channels_dir, corrupt_copy, corrupt)
    assert check["ok"] is False
    assert "SRT cue 2 " in check["note"] and "Replaced" in check["note"]


def test_subtitles_check_fails_when_a_cue_starts_after_its_words(built_package, test_channels_dir, corrupt_copy):
    def corrupt(copy_dir, package):
        def fn(cues):
            cues[-1].start_s += 2.0
            cues[-1].end_s += 2.0
            return cues

        _rewrite_srt(copy_dir, package, fn)

    check = _subtitles_check(built_package, test_channels_dir, corrupt_copy, corrupt)
    assert check["ok"] is False
    assert "outside its SRT cue" in check["note"]


def test_subtitles_check_fails_when_a_cue_end_covers_later_cues(built_package, test_channels_dir, corrupt_copy):
    def corrupt(copy_dir, package):
        def fn(cues):
            cues[0].end_s = cues[-1].end_s
            return cues

        _rewrite_srt(copy_dir, package, fn)

    check = _subtitles_check(built_package, test_channels_dir, corrupt_copy, corrupt)
    assert check["ok"] is False
    assert "overlaps" in check["note"]


def test_subtitles_check_fails_when_timeline_carries_the_pronunciation(built_package, test_channels_dir, corrupt_copy):
    def corrupt(copy_dir, package):
        path = copy_dir / package["files"]["timeline"]["path"]
        data = json.loads(path.read_text())
        for scene in data["scenes"]:
            for word in scene["words"]:
                word["text"] = word["text"].replace("Garcetti", "gar-SET-ee")
        path.write_text(json.dumps(data))

    check = _subtitles_check(built_package, test_channels_dir, corrupt_copy, corrupt)
    assert check["ok"] is False
    assert "gar-SET-ee's" in check["note"]


# --- duration: measured from video.mp4, not taken from package.json ---


def _cut_video(copy_dir, package, seconds):
    import subprocess

    path = copy_dir / package["files"]["video"]["path"]
    tmp = path.with_name("cut.mp4")
    subprocess.run(["ffmpeg", "-y", "-v", "error", "-i", str(path), "-t", str(seconds), "-c", "copy", str(tmp)], check=True)
    tmp.replace(path)


def _duration_check(built_package, test_channels_dir, corrupt_copy, corrupt):
    copy_dir = corrupt_copy(built_package, corrupt)
    package = json.loads((copy_dir / "package.json").read_text())
    result = run_checks(copy_dir, package, _channel_profile(test_channels_dir))
    return next(c for c in result["checks"] if c["id"] == "duration")


def test_duration_check_fails_when_video_is_shorter_than_package_says(built_package, test_channels_dir, corrupt_copy):
    declared = built_package["package"]["duration_s"]
    check = _duration_check(built_package, test_channels_dir, corrupt_copy, lambda d, p: _cut_video(d, p, 11))
    assert check["ok"] is False
    assert 10.5 < check["value"] < 11.5  # still inside the test channel's 10-30s range
    assert "differs from package.duration_s" in check["note"]
    assert f"{declared:.2f}s" in check["note"] and f"{check['value']:.2f}s" in check["note"]


def test_duration_check_fails_on_measured_length_outside_range(built_package, test_channels_dir, corrupt_copy):
    from studio.video import probe_duration_s

    def corrupt(copy_dir, package):
        _cut_video(copy_dir, package, 5)
        package["duration_s"] = probe_duration_s(copy_dir / package["files"]["video"]["path"])

    check = _duration_check(built_package, test_channels_dir, corrupt_copy, corrupt)
    assert check["ok"] is False
    assert "outside channel range" in check["note"]
    assert "differs" not in check["note"]


def test_subtitles_check_fails_when_a_scene_is_missing(built_package, test_channels_dir, corrupt_copy):
    def corrupt(copy_dir, package):
        path = copy_dir / package["files"]["timeline"]["path"]
        data = json.loads(path.read_text())
        dropped = data["scenes"].pop(1)
        path.write_text(json.dumps(data))
        words = {w["text"] for w in dropped["words"]}

        def fn(cues):  # drop that scene's cues too, so text and timing still line up
            return [c for c in cues if not set(" ".join(c.lines).split()) <= words]

        _rewrite_srt(copy_dir, package, fn)

    check = _subtitles_check(built_package, test_channels_dir, corrupt_copy, corrupt)
    assert check["ok"] is False
    assert "timeline.json scenes" in check["note"]
