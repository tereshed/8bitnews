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


def test_duration_check_fails_outside_channel_range(built_package, test_channels_dir, corrupt_copy):
    def corrupt(copy_dir, package):
        package["duration_s"] = 9999.0

    copy_dir = corrupt_copy(built_package, corrupt)
    package = json.loads((copy_dir / "package.json").read_text())
    result = run_checks(copy_dir, package, _channel_profile(test_channels_dir))
    check = next(c for c in result["checks"] if c["id"] == "duration")
    assert check["ok"] is False
    assert "9999" in check["note"] or check["value"] == 9999.0


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
