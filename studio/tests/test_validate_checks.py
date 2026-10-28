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
    assert ids == {
        "format", "duration", "file_size", "subtitles_cover_words",
        "package_schema", "template_valid", "keyframes_grid_palette", "ai_plates", "face_clear",
        "video_matches_keyframes", "no_black_frames", "disclosures",
        "loudness", "true_peak", "silence", "music_under_voice", "video_no_music",
    }
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



# --- E2a-2b checks: one negative test each ---


def _check(built_package, test_channels_dir, corrupt_copy, check_id, corrupt, rehash=True):
    copy_dir = corrupt_copy(built_package, corrupt, rehash=rehash)
    package = json.loads((copy_dir / "package.json").read_text())
    result = run_checks(copy_dir, package, _channel_profile(test_channels_dir))
    return next(c for c in result["checks"] if c["id"] == check_id)


def _paint(copy_dir, rel, rect, color):
    """Fills a logical-coordinate rect of a 1080x1920 image with one color."""
    from PIL import Image, ImageDraw

    path = copy_dir / rel
    img = Image.open(path).convert("RGB")
    x, y, w, h = (rect[k] * 4 for k in ("x", "y", "w", "h"))
    ImageDraw.Draw(img).rectangle([x, y, x + w - 1, y + h - 1], fill=color)
    img.save(path)


def _template(copy_dir):
    return json.loads((copy_dir / "template" / "template.json").read_text())


def test_package_schema_fails_on_a_file_changed_after_hashing(built_package, test_channels_dir, corrupt_copy):
    def corrupt(copy_dir, package):
        from PIL import Image

        path = copy_dir / "keyframes" / "S3_mid.png"
        img = Image.open(path).convert("RGB")
        img.putpixel((0, 0), (0, 0, 0) if img.getpixel((0, 0)) != (0, 0, 0) else (252, 252, 252))
        img.save(path)

    check = _check(built_package, test_channels_dir, corrupt_copy, "package_schema", corrupt, rehash=False)
    assert check["ok"] is False
    assert "keyframes/S3_mid.png" in check["note"]


def test_package_schema_fails_on_a_schema_violation(built_package, test_channels_dir, corrupt_copy):
    check = _check(built_package, test_channels_dir, corrupt_copy, "package_schema", lambda d, p: p.pop("transcript"))
    assert check["ok"] is False
    assert "schema" in check["note"] and "transcript" in check["note"]


def test_template_valid_fails_on_a_broken_template_copy(built_package, test_channels_dir, corrupt_copy):
    def corrupt(copy_dir, package):
        path = copy_dir / "template" / "template.json"
        data = json.loads(path.read_text())
        data["plates"]["source_plate"]["rect"]["h"] = 20
        path.write_text(json.dumps(data))

    check = _check(built_package, test_channels_dir, corrupt_copy, "template_valid", corrupt)
    assert check["ok"] is False
    assert "plates.source_plate" in check["note"]


def test_keyframes_grid_palette_fails_on_an_off_palette_color(built_package, test_channels_dir, corrupt_copy):
    def corrupt(copy_dir, package):
        _paint(copy_dir, "keyframes/S2_mid.png", {"x": 100, "y": 200, "w": 4, "h": 4}, "#123456")

    check = _check(built_package, test_channels_dir, corrupt_copy, "keyframes_grid_palette", corrupt)
    assert check["ok"] is False
    assert "S2_mid.png" in check["note"] and "#123456" in check["note"]


def test_keyframes_grid_palette_fails_off_the_4x4_grid(built_package, test_channels_dir, corrupt_copy):
    def corrupt(copy_dir, package):
        from PIL import Image

        path = copy_dir / "keyframes" / "S1_mid.png"
        img = Image.open(path).convert("RGB")
        img.putpixel((401, 801), (0, 0, 0) if img.getpixel((401, 801)) != (0, 0, 0) else (252, 252, 252))
        img.save(path)

    check = _check(built_package, test_channels_dir, corrupt_copy, "keyframes_grid_palette", corrupt)
    assert check["ok"] is False
    assert "S1_mid.png: 1 of" in check["note"] and "cells are not one color" in check["note"]


def test_keyframes_grid_palette_fails_on_wrong_size(built_package, test_channels_dir, corrupt_copy):
    def corrupt(copy_dir, package):
        from PIL import Image

        path = copy_dir / package["files"]["cover"]["path"]
        Image.open(path).resize((540, 960)).save(path)

    check = _check(built_package, test_channels_dir, corrupt_copy, "keyframes_grid_palette", corrupt)
    assert check["ok"] is False
    assert "cover.png: 540x960" in check["note"]


def test_ai_plates_fails_when_ai_illustration_is_painted_over(built_package, test_channels_dir, corrupt_copy):
    def corrupt(copy_dir, package):
        _paint(copy_dir, "keyframes/S2_mid.png", _template(copy_dir)["plates"]["ai_illustration"]["rect"], "#0058f8")

    check = _check(built_package, test_channels_dir, corrupt_copy, "ai_plates", corrupt)
    assert check["ok"] is False
    assert "S2_mid.png (S2" in check["note"] and "ai_illustration plate missing" in check["note"]


def test_ai_plates_fails_when_ai_voice_is_painted_over(built_package, test_channels_dir, corrupt_copy):
    def corrupt(copy_dir, package):
        _paint(copy_dir, "keyframes/S1_first.png", _template(copy_dir)["plates"]["ai_voice"]["rect"], "#0058f8")

    check = _check(built_package, test_channels_dir, corrupt_copy, "ai_plates", corrupt)
    assert check["ok"] is False
    assert "S1_first.png" in check["note"] and "ai_voice plate missing" in check["note"]


def test_ai_plates_fails_when_the_cover_loses_ai_illustration(built_package, test_channels_dir, corrupt_copy):
    def corrupt(copy_dir, package):
        _paint(copy_dir, "cover.png", _template(copy_dir)["plates"]["ai_illustration"]["rect"], "#0058f8")

    check = _check(built_package, test_channels_dir, corrupt_copy, "ai_plates", corrupt)
    assert check["ok"] is False
    assert "cover.png" in check["note"]


def test_face_clear_fails_when_the_face_is_covered(built_package, test_channels_dir, corrupt_copy):
    from studio.channel_assets import load_channel_assets

    def corrupt(copy_dir, package):
        anchor = load_channel_assets("8bitnews-us", package["anchor"]["id"]).anchor
        face = anchor.face_rect_in_frame(_template(copy_dir)["studio"]["anchor"])
        _paint(copy_dir, "keyframes/S1_mid.png", {"x": face["x"], "y": face["y"] + 20, "w": face["w"], "h": 12}, "#000000")

    check = _check(built_package, test_channels_dir, corrupt_copy, "face_clear", corrupt)
    assert check["ok"] is False
    assert "S1_mid.png (S1" in check["note"] and "face pixels differ" in check["note"]


def test_video_matches_keyframes_fails_on_a_swapped_keyframe(built_package, test_channels_dir, corrupt_copy):
    def corrupt(copy_dir, package):
        shutil.copyfile(copy_dir / "keyframes" / "S3_mid.png", copy_dir / "keyframes" / "S2_mid.png")

    check = _check(built_package, test_channels_dir, corrupt_copy, "video_matches_keyframes", corrupt)
    assert check["ok"] is False
    assert "S2_mid.png" in check["note"] and "4x4 cell(s) differ" in check["note"]


def test_no_black_frames_fails_on_a_black_stretch(built_package, test_channels_dir, corrupt_copy):
    import subprocess

    def corrupt(copy_dir, package):
        path = copy_dir / package["files"]["video"]["path"]
        tmp = path.with_name("black.mp4")
        subprocess.run(["ffmpeg", "-y", "-v", "error", "-i", str(path), "-vf",
                        "drawbox=enable='between(t,2,2.3)':color=black:t=fill", "-c:a", "copy", str(tmp)], check=True)
        tmp.replace(path)

    check = _check(built_package, test_channels_dir, corrupt_copy, "no_black_frames", corrupt)
    assert check["ok"] is False
    assert "black from 2.0" in check["note"]


def test_disclosures_fails_without_ai_voice(built_package, test_channels_dir, corrupt_copy):
    def corrupt(copy_dir, package):
        package["disclosures"]["ai_voice"] = False

    check = _check(built_package, test_channels_dir, corrupt_copy, "disclosures", corrupt)
    assert check["ok"] is False
    assert "disclosures.ai_voice" in check["note"]


def test_disclosures_fails_when_an_enabled_platform_has_no_ai_label(built_package, test_channels_dir, corrupt_copy):
    def corrupt(copy_dir, package):
        del package["disclosures"]["platforms"]["tiktok"]

    check = _check(built_package, test_channels_dir, corrupt_copy, "disclosures", corrupt)
    assert check["ok"] is False
    assert "platforms.tiktok.ai_label" in check["note"]



def _logical_edit(copy_dir, rel, edit):
    """Edits a keyframe at 270x480 (edit(img) draws on an RGBA image) and
    writes it back at 1080x1920 on the 4x4 grid."""
    from PIL import Image

    path = copy_dir / rel
    big = Image.open(path).convert("RGBA")
    small = big.resize((big.width // 4, big.height // 4), Image.NEAREST)
    edit(small)
    small.convert("RGB").resize(big.size, Image.NEAREST).save(path)


def test_ai_plates_fails_on_a_plate_with_the_wrong_label(built_package, test_channels_dir, corrupt_copy):
    """The AI-voice plate drawn with the icon's "AI" label: right colors, wrong text."""
    from studio.channel_assets import load_channel_assets
    from studio.engine import _draw_plate

    def corrupt(copy_dir, package):
        cfg = _template(copy_dir)["plates"]["ai_voice"]
        font = load_channel_assets("8bitnews-us").font(8)
        _logical_edit(copy_dir, "keyframes/S1_first.png", lambda img: _draw_plate(img, cfg, ["AI"], font))

    check = _check(built_package, test_channels_dir, corrupt_copy, "ai_plates", corrupt)
    assert check["ok"] is False
    assert "S1_first.png" in check["note"] and "'AI · AI voice' plate" in check["note"]


def test_ai_plates_fails_when_the_engine_leaves_a_plate_out(built_package, test_channels_dir, corrupt_copy, monkeypatch):
    import studio.checks as checks_mod

    real = checks_mod.active_plates
    monkeypatch.setattr(checks_mod, "active_plates", lambda *a: real(*a) - {"ai_illustration"})
    check = _check(built_package, test_channels_dir, corrupt_copy, "ai_plates", lambda d, p: None)
    assert check["ok"] is False
    # S1 is the anchor with S2's real person on the screen behind her
    assert "(S1, " in check["note"] and "does not put ai_illustration" in check["note"]


def test_video_matches_keyframes_fails_on_a_small_local_change(built_package, test_channels_dir, corrupt_copy):
    def corrupt(copy_dir, package):
        def edit(img):
            from PIL import ImageDraw

            old = img.getpixel((100, 200))[:3]
            color = (0, 0, 0) if sum(old) > 300 else (252, 252, 252)
            ImageDraw.Draw(img).rectangle([100, 200, 107, 207], fill=(*color, 255))

        _logical_edit(copy_dir, "keyframes/S2_mid.png", edit)

    check = _check(built_package, test_channels_dir, corrupt_copy, "video_matches_keyframes", corrupt)
    assert check["ok"] is False
    assert "S2_mid.png" in check["note"] and "4x4 cell(s) differ" in check["note"]


def test_no_black_frames_fails_when_the_video_is_unreadable(built_package, test_channels_dir, corrupt_copy):
    def corrupt(copy_dir, package):
        (copy_dir / package["files"]["video"]["path"]).write_bytes(b"not a video")

    check = _check(built_package, test_channels_dir, corrupt_copy, "no_black_frames", corrupt)
    assert check["ok"] is False
    assert "could not read" in check["note"]


def test_no_black_frames_passes_a_dark_frame_with_light_lettering(built_package, test_channels_dir, corrupt_copy):
    """A night scene: almost all black, but the AI plate's letters are light."""
    import subprocess

    def corrupt(copy_dir, package):
        path = copy_dir / package["files"]["video"]["path"]
        subprocess.run(["ffmpeg", "-y", "-v", "error", "-f", "lavfi", "-i",
                        "color=c=black:s=1080x1920:d=2:r=30,drawbox=x=40:y=100:w=60:h=32:color=0xf8f8f8:t=fill",
                        "-c:v", "libx264", "-pix_fmt", "yuv420p", str(path)], check=True)

    check = _check(built_package, test_channels_dir, corrupt_copy, "no_black_frames", corrupt)
    assert check["ok"] is True, check["note"]


def test_package_schema_fails_on_a_keyframe_missing_from_the_timeline(built_package, test_channels_dir, corrupt_copy):
    def corrupt(copy_dir, package):
        path = copy_dir / package["files"]["timeline"]["path"]
        data = json.loads(path.read_text())
        data["keyframes"] = [kf for kf in data["keyframes"] if kf["path"] != "keyframes/S2_mid.png"]
        path.write_text(json.dumps(data))

    check = _check(built_package, test_channels_dir, corrupt_copy, "package_schema", corrupt)
    assert check["ok"] is False
    assert "keyframes/S2_mid.png: listed for S2 but has no timeline.json keyframe record" in check["note"]


def test_package_schema_fails_on_a_path_outside_the_package(built_package, test_channels_dir, corrupt_copy):
    def corrupt(copy_dir, package):
        shutil.copyfile(copy_dir / "subtitles.srt", copy_dir.parent / "outside.srt")
        package["files"]["subtitles"]["path"] = "../outside.srt"

    check = _check(built_package, test_channels_dir, corrupt_copy, "package_schema", corrupt)
    assert check["ok"] is False
    assert "points outside the package directory" in check["note"]


def test_template_valid_fails_when_the_copy_is_another_template(built_package, test_channels_dir, corrupt_copy):
    def corrupt(copy_dir, package):
        package["template"] = "night-report"

    check = _check(built_package, test_channels_dir, corrupt_copy, "template_valid", corrupt)
    assert check["ok"] is False
    assert "package says 'night-report'" in check["note"]
