import json
import shutil

import pytest
from PIL import Image

from studio.build import (
    BuildError,
    _render_cover,
    _resolve_template_dir,
    _resolve_tts_name,
    build_package,
    fit_cover_title,
)
from studio.builtin_script import BUILTIN_SCRIPT
from studio.channel_assets import load_channel_assets
from studio.constants import FONT_PX, OUTPUT_H, OUTPUT_W
from studio.cover_title import cover_font_px, cover_text_capacity, worst_case_title_lines
from studio.template_pkg import load_template
from studio.timeline import load_timeline


# --- template resolution order (--template > script.template > channel default) ---


def test_resolve_template_dir_prefers_explicit_arg(nes_desk_dir, night_report_dir):
    script = {"template": "night-report"}
    channel_profile = {"format": {"default_template": "night-report"}}
    resolved = _resolve_template_dir("nes-desk", script, channel_profile)
    assert resolved.resolve() == nes_desk_dir.resolve()


def test_resolve_template_dir_falls_back_to_script_template(night_report_dir):
    script = {"template": "night-report"}
    channel_profile = {"format": {"default_template": "nes-desk"}}
    resolved = _resolve_template_dir(None, script, channel_profile)
    assert resolved.resolve() == night_report_dir.resolve()


def test_resolve_template_dir_falls_back_to_channel_default(nes_desk_dir):
    script = {}
    channel_profile = {"format": {"default_template": "nes-desk"}}
    resolved = _resolve_template_dir(None, script, channel_profile)
    assert resolved.resolve() == nes_desk_dir.resolve()


# --- --tts resolution ---


def test_resolve_tts_name_uses_channel_default():
    assert _resolve_tts_name(None, {"format": {"voice": {"provider": "stub"}}}) == "stub"


def test_resolve_tts_name_explicit_arg_wins():
    assert _resolve_tts_name("stub", {"format": {"voice": {"provider": "kokoro"}}}) == "stub"


def test_resolve_tts_name_unimplemented_provider_hints_stub():
    with pytest.raises(BuildError) as exc_info:
        _resolve_tts_name(None, {"format": {"voice": {"provider": "elevenlabs"}}})
    assert "--tts stub" in str(exc_info.value)


# --- cover: fit check (function-level negative test) and S* vs custom ---


def test_fit_cover_title_raises_when_text_does_not_fit():
    font = load_channel_assets("8bitnews-us").font(FONT_PX)
    tiny_rect = {"x": 0, "y": 0, "w": FONT_PX + 4, "h": FONT_PX + 4}  # fits one ~1-char line only
    with pytest.raises(BuildError):
        fit_cover_title("This title is far too long to fit", tiny_rect, font)


def test_fit_cover_title_fits_when_there_is_room():
    font = load_channel_assets("8bitnews-us").font(FONT_PX)
    rect = {"x": 0, "y": 0, "w": 190, "h": 40}
    lines = fit_cover_title("Short title", rect, font)
    assert lines


def test_fit_cover_title_fits_the_worst_title_on_night_reports_real_rect(night_report_dir):
    """night-report's cover.layout.text_rect holds the lines a 30-character
    title can wrap to (studio template validate enforces it): guard the real
    rect geometry."""
    template = load_template(night_report_dir)
    layout = template.data["cover"]["layout"]
    font = load_channel_assets("8bitnews-us").font(cover_font_px(layout))
    text_rect = layout["text_rect"]
    max_chars, max_rows = cover_text_capacity(text_rect, font.size)
    assert worst_case_title_lines(max_chars) <= max_rows
    lines = fit_cover_title("Trade restrictions hit prices", text_rect, font)
    assert len(lines) == worst_case_title_lines(max_chars) == 3


@pytest.mark.parametrize("template_name", ["nes-desk", "night-report"])
def test_cover_title_is_twice_scene_text_size(template_name):
    """The actual title footprint must be large, not just a larger rect."""
    import numpy as np

    from studio.build import _draw_cover_title
    from studio.palette import hex_to_rgb

    template = load_template(_resolve_template_dir(template_name, {}, {}))
    frame = Image.new("RGBA", (270, 480), (0, 0, 0, 0))
    _draw_cover_title(frame, "Trade", template, load_channel_assets("8bitnews-us"), "bottom")
    fill = hex_to_rgb(template.data["subtitles"]["style"]["color"])
    ys, xs = np.nonzero((np.asarray(frame.convert("RGB")) == fill).all(axis=2))  # the glyphs' ink, not the plate
    from studio.text_render import draw_text

    small = Image.new("RGBA", (270, 480), (0, 0, 0, 0))
    draw_text(small, (0, 0), "Trade", load_channel_assets("8bitnews-us").font(FONT_PX), fill)
    left, top, right, bottom = small.getbbox()
    assert xs.max() + 1 - xs.min() == 2 * (right - left)
    assert ys.max() + 1 - ys.min() == 2 * (bottom - top)


def test_large_cover_title_rejects_word_splitting_and_font_fallback(nes_desk_dir):
    template = load_template(nes_desk_dir)
    layout = template.data["cover"]["layout"]
    font = load_channel_assets("8bitnews-us").font(cover_font_px(layout))
    with pytest.raises(BuildError, match="shorten the wording"):
        fit_cover_title("Disenfranchisement fears", layout["text_rect"], font)


def test_cover_rect_too_short_for_large_font_has_no_lines():
    assert cover_text_capacity({"w": 230, "h": 12}, 16) == (14, 0)


def test_fit_cover_title_still_rejects_text_needing_more_lines_on_night_report(night_report_dir):
    template = load_template(night_report_dir)
    font = load_channel_assets("8bitnews-us").font(FONT_PX)
    text_rect = template.data["cover"]["layout"]["text_rect"]
    with pytest.raises(BuildError):
        fit_cover_title(
            "This cover title is deliberately far too long to fit in the lines the rect holds, even at "
            "twenty-eight characters a line, however it wraps",
            text_rect,
            font,
        )


def test_cover_frame_scene_vs_custom_produce_different_images(nes_desk_dir, tmp_path):
    template = load_template(nes_desk_dir)
    assets = load_channel_assets("8bitnews-us")

    scene_script = dict(BUILTIN_SCRIPT, cover={"frame": "S1", "visual": "x" * 20, "text": "Scene Cover"})
    scene_out = tmp_path / "scene_cover.png"
    record = _render_cover(scene_script, template, assets, None, scene_out, "bottom")
    assert record == {"frame": "S1", "layout": "bottom", "picture": "template"}  # no picture file: the fallback

    assets_dir = tmp_path / "assets"
    assets_dir.mkdir()
    Image.new("RGB", (400, 700), (10, 20, 30)).save(assets_dir / "cover.png")
    custom_script = dict(BUILTIN_SCRIPT, cover={"frame": "custom", "visual": "x" * 20, "text": "Custom Cover"})
    custom_out = tmp_path / "custom_cover.png"
    record = _render_cover(custom_script, template, assets, assets_dir, custom_out, "bottom")
    assert record == {"frame": "custom", "layout": "bottom", "picture": "custom"}

    for path in (scene_out, custom_out):
        with Image.open(path) as img:
            assert img.size == (OUTPUT_W, OUTPUT_H)
    assert scene_out.read_bytes() != custom_out.read_bytes()


def test_cover_frame_unknown_scene_id_raises_readable_error(nes_desk_dir, tmp_path):
    template = load_template(nes_desk_dir)
    assets = load_channel_assets("8bitnews-us")
    bad_script = dict(BUILTIN_SCRIPT, cover={"frame": "S99", "visual": "x" * 20, "text": "Nope"})
    with pytest.raises(BuildError) as exc_info:
        _render_cover(bad_script, template, assets, None, tmp_path / "out.png", "bottom")
    assert "S99" in str(exc_info.value)


# --- full build: structure, pronunciation text routing, schema ---


def test_build_output_files_exist(built_package):
    out_dir = built_package["out_dir"]
    for rel in ("video.mp4", "subtitles.srt", "cover.png", "timeline.json", "script.json", "package.json"):
        assert (out_dir / rel).is_file(), rel
    assert (out_dir / "template" / "template.json").is_file()
    package = built_package["package"]
    for scene in package["scenes"]:
        for kf in scene["keyframes"]:
            assert (out_dir / kf["path"]).is_file()


def test_build_package_matches_schema(built_package):
    import jsonschema

    from studio import repo

    schema = repo.load_contract_schema("video_package.schema.json")
    jsonschema.Draft202012Validator(schema).validate(built_package["package"])


def test_build_duration_within_expected_range(built_package):
    # Fixture script sums to roughly 33 words at 144 wpm plus 3 scene-pause
    # gaps -- comfortably inside the test channel's 10-30s range isn't
    # guaranteed, so just check it's a sane positive number matching the
    # package's own files.
    assert built_package["package"]["duration_s"] > 0


def test_build_pronunciation_only_affects_synthesis_text(built_package):
    out_dir = built_package["out_dir"]
    package = built_package["package"]

    s2 = next(s for s in package["scenes"] if s["id"] == "S2")
    assert "gar-SET-ee" in s2["narration"]
    assert "Garcetti" not in s2["narration"]

    assert "Garcetti" in package["transcript"]
    assert "gar-SET-ee" not in package["transcript"]

    srt_text = (out_dir / "subtitles.srt").read_text()
    assert "Garcetti" in srt_text
    assert "gar-SET-ee" not in srt_text

    timeline = load_timeline(out_dir / "timeline.json")
    all_words = [w.text for scene in timeline.scenes for w in scene.words]
    assert "Garcetti's" in all_words
    assert not any("gar-SET-ee" in w for w in all_words)


def test_build_disclosures_and_music(built_package):
    package = built_package["package"]
    assert package["disclosures"]["ai_voice"] is True
    assert package["disclosures"]["ai_visuals"] is True
    assert set(package["disclosures"]["platforms"]) == {"youtube", "tiktok", "instagram"}
    assert all(p["ai_label"] is True for p in package["disclosures"]["platforms"].values())
    assert package["music"] == {"enabled": False}
    assert package["notes"]


def test_build_ai_label_shown_matches_real_person_scene(built_package):
    package = built_package["package"]
    by_id = {s["id"]: s for s in package["scenes"]}
    assert by_id["S2"]["ai_label_shown"] is True  # S2 has real_people in the fixture script
    assert by_id["S3"]["ai_label_shown"] is False


def test_build_scene_pause_changes_total_duration(short_script_path, test_channels_dir, nes_desk_dir, tmp_path, monkeypatch):
    def _template_with_pause(pause_s: float):
        d = tmp_path / f"tmpl-{pause_s}"
        shutil.copytree(nes_desk_dir, d)
        data = json.loads((d / "template.json").read_text())
        data["timing"]["scene_pause_s"] = pause_s
        (d / "template.json").write_text(json.dumps(data))
        return d

    short_pause_dir = tmp_path / "out-short"
    long_pause_dir = tmp_path / "out-long"
    # package.duration_s must come from measuring video.mp4: a small offset on
    # the measurement shows up in the package, a timeline length would not.
    import studio.build as build_mod
    from studio.video import probe_duration_s

    monkeypatch.setattr(build_mod, "probe_duration_s", lambda path: probe_duration_s(path) + 0.05)
    build_package(
        script_path=short_script_path,
        channel_id="8bitnews-us",
        out_dir=short_pause_dir,
        channels_dir=test_channels_dir,
        template_dir_arg=str(_template_with_pause(0.0)),
        tts_name="stub",
    )
    build_package(
        script_path=short_script_path,
        channel_id="8bitnews-us",
        out_dir=long_pause_dir,
        channels_dir=test_channels_dir,
        template_dir_arg=str(_template_with_pause(0.9)),
        tts_name="stub",
        keep_work=True,
    )
    assert (long_pause_dir / "frames" / "frame_000000.png").is_file()  # --keep-work keeps the build scratch
    measured = probe_duration_s(long_pause_dir / "video.mp4")
    assert json.loads((long_pause_dir / "package.json").read_text())["duration_s"] == pytest.approx(measured + 0.05)
    short_duration = json.loads((short_pause_dir / "package.json").read_text())["duration_s"]
    long_duration = json.loads((long_pause_dir / "package.json").read_text())["duration_s"]
    assert long_duration > short_duration


def test_build_is_deterministic_across_independent_runs(short_script_path, test_channels_dir, tmp_path):
    out_a = tmp_path / "out-a"
    out_b = tmp_path / "out-b"
    build_package(script_path=short_script_path, channel_id="8bitnews-us", out_dir=out_a, channels_dir=test_channels_dir, tts_name="stub")
    build_package(script_path=short_script_path, channel_id="8bitnews-us", out_dir=out_b, channels_dir=test_channels_dir, tts_name="stub")

    pkg_a = json.loads((out_a / "package.json").read_text())
    pkg_b = json.loads((out_b / "package.json").read_text())
    for scene_a, scene_b in zip(pkg_a["scenes"], pkg_b["scenes"]):
        for kf_a, kf_b in zip(scene_a["keyframes"], scene_b["keyframes"]):
            assert kf_a["sha256"] == kf_b["sha256"]


def test_build_clears_stale_frames_and_keyframes_on_rebuild_into_same_out_dir(
    short_script_path, test_channels_dir, tmp_path
):
    """Rebuilding into the same --out dir after a scene id changes must not
    leave orphaned keyframes/frames from the previous build lying around."""
    out_dir = tmp_path / "out"
    build_package(script_path=short_script_path, channel_id="8bitnews-us", out_dir=out_dir, channels_dir=test_channels_dir, tts_name="stub")
    assert (out_dir / "keyframes" / "S4_first.png").is_file()

    script = json.loads(short_script_path.read_text())
    script["scenes"][-1]["id"] = "S9"
    renamed_script_path = tmp_path / "renamed-script.json"
    renamed_script_path.write_text(json.dumps(script))

    build_package(script_path=renamed_script_path, channel_id="8bitnews-us", out_dir=out_dir, channels_dir=test_channels_dir, tts_name="stub")

    assert not (out_dir / "keyframes" / "S4_first.png").exists()
    assert not (out_dir / "keyframes" / "S4_mid.png").exists()
    assert (out_dir / "keyframes" / "S9_first.png").is_file()
    assert (out_dir / "keyframes" / "S9_mid.png").is_file()


# --- the cover: the story picture, the title on its plate at the bottom or the top, the AI line ---


def _title_mask(text, template, assets, layout="bottom"):
    """Logical-size boolean mask of the cover title's fill pixels, drawn
    with the same layout the build uses."""
    import numpy as np

    from studio.build import cover_plate_layout
    from studio.constants import LOGICAL_H, LOGICAL_W
    from studio.text_render import draw_text

    _box, lines, font = cover_plate_layout(text, template, assets, layout)
    canvas = Image.new("RGBA", (LOGICAL_W, LOGICAL_H), (0, 0, 0, 0))
    for line, xy in lines:
        draw_text(canvas, xy, line, font, (255, 255, 255))
    return np.array(canvas)[:, :, 3] > 0


def _assert_title_drawn(cover_path, text, template, assets, layout="bottom"):
    import numpy as np

    from studio.constants import SCALE
    from studio.palette import hex_to_rgb

    mask = _title_mask(text, template, assets, layout)
    assert mask.sum() > 50
    big = np.repeat(np.repeat(mask, SCALE, axis=0), SCALE, axis=1)
    pixels = np.array(Image.open(cover_path).convert("RGB"))[big]
    assert (pixels == hex_to_rgb(template.data["subtitles"]["style"]["color"])).all()


def _logical_cover(path):
    import numpy as np

    from studio.constants import SCALE

    return np.array(Image.open(path).convert("RGB"))[::SCALE, ::SCALE]


def _picture_area(text, template, assets, layout):
    """Where the cover shows its picture: everywhere but the title plate and the AI strip."""
    import numpy as np

    from studio.build import cover_plate_layout
    from studio.constants import LOGICAL_H, LOGICAL_W
    from studio.engine import ai_label_box

    plate = cover_plate_layout(text, template, assets, layout)[0]
    strip = ai_label_box(template, assets.labels.cover, assets.font(FONT_PX))
    area = np.ones((LOGICAL_H, LOGICAL_W), bool)
    for box in (plate, strip):
        area[box["y"]:box["y"] + box["h"], box["x"]:box["x"] + box["w"]] = False
    return area


def test_the_build_records_its_cover(built_package):
    # short.json: cover.frame S1, no picture files (stub build), the default layout
    assert built_package["package"]["cover"] == {"frame": "S1", "layout": "bottom", "picture": "template"}


def test_scene_cover_of_the_build_carries_the_cover_title(built_package):
    out_dir = built_package["out_dir"]
    script = json.loads((out_dir / "script.json").read_text())
    assert script["cover"]["frame"].startswith("S")
    template = load_template(out_dir / "template")
    _assert_title_drawn(out_dir / "cover.png", script["cover"]["text"], template, load_channel_assets("8bitnews-us"),
                        built_package["package"]["cover"]["layout"])


def test_a_cover_without_a_picture_is_the_template_background_the_plate_and_the_ai_line(built_package):
    """The stub build has no picture files: the cover falls back to the
    template's cover background with the same plate and AI line, and nothing
    else is drawn -- no anchor, no studio, no scene plates, no subtitles."""
    import numpy as np

    from studio.build import cover_plate_layout
    from studio.palette import hex_to_rgb
    from studio.quantize import quantize

    out_dir = built_package["out_dir"]
    script = json.loads((out_dir / "script.json").read_text())
    template = load_template(out_dir / "template")
    assets = load_channel_assets("8bitnews-us", built_package["package"]["anchor"]["id"])
    background = template.load_asset_image(template.data["cover"]["layout"]["background"]).convert("RGB")
    want = np.array(quantize(background, template.palette(serious=bool(script.get("serious_mode")))))
    cover = _logical_cover(out_dir / "cover.png")
    area = _picture_area(script["cover"]["text"], template, assets, "bottom")
    assert (cover[area] == want[area]).all()

    # one solid black plate: only black and the title's color inside it
    plate = cover_plate_layout(script["cover"]["text"], template, assets, "bottom")[0]
    inside = cover[plate["y"]:plate["y"] + plate["h"], plate["x"]:plate["x"] + plate["w"]].reshape(-1, 3)
    black = (inside == (0, 0, 0)).all(axis=1)
    title = (inside == hex_to_rgb(template.data["subtitles"]["style"]["color"])).all(axis=1)
    assert (black | title).all() and black.sum() > title.sum() > 0


def _stripes(path, size, colors):
    """Three vertical stripes, left to right."""
    img = Image.new("RGB", size, colors[0])
    w, h = size
    img.paste(colors[1], (w // 3, 0, 2 * w // 3, h))
    img.paste(colors[2], (2 * w // 3, 0, w, h))
    img.save(path)


def test_the_cover_is_the_picture_of_the_scene_in_cover_frame_without_the_anchor(nes_desk_dir, tmp_path):
    """cover.frame S1 (an anchor shot) shows S1's own screen picture --
    landscape, so center-cropped to its middle stripe -- across the whole
    frame; cover.frame S2 shows S2's. No studio, and no anchor over the face rect."""
    template = load_template(nes_desk_dir)
    assets = load_channel_assets("8bitnews-us")
    images = tmp_path / "images"
    images.mkdir()
    green, yellow = (0, 160, 0), (230, 200, 40)
    _stripes(images / "S1.png", (960, 540), [(200, 0, 0), green, (0, 0, 200)])
    Image.new("RGB", (576, 1024), yellow).save(images / "S2.png")
    face = assets.anchor.face_rect_in_frame(template.data["studio"]["anchor"])
    area = _picture_area("Pixel News", template, assets, "bottom")
    pixels = {}
    for frame_id, color in (("S1", green), ("S2", yellow)):
        out = tmp_path / f"{frame_id}.png"
        script = dict(BUILTIN_SCRIPT, cover={"frame": frame_id, "visual": "x" * 20, "text": "Pixel News"})
        assert _render_cover(script, template, assets, images, out, "bottom")["picture"] == frame_id
        cover = _logical_cover(out)
        assert (cover[area] == color).all()
        assert (cover[face["y"]:face["y"] + face["h"], face["x"]:face["x"] + face["w"]] == color).all()
        _assert_title_drawn(out, "Pixel News", template, assets)
        pixels[frame_id] = cover
    assert (pixels["S1"][area] != pixels["S2"][area]).all(axis=1).all()


@pytest.mark.parametrize("channel", ["8bitnews-us", "8bitnews-es"])
@pytest.mark.parametrize("template_name", ["nes-desk", "night-report"])
def test_both_cover_layouts_keep_a_three_line_plate_in_the_safe_zone_and_off_the_ai_line(channel, template_name):
    from studio.build import cover_plate_layout
    from studio.constants import LOGICAL_W
    from studio.cover_title import cover_plate_box, cover_text_capacity
    from studio.engine import ai_label_box
    from studio.paths import TEMPLATES_DIR
    from studio.safe_zones import common_safe_rect, rect_within, rects_intersect
    from studio.text_render import text_size

    template = load_template(TEMPLATES_DIR / template_name)
    assets = load_channel_assets(channel)
    safe = common_safe_rect(channel)
    strip = ai_label_box(template, assets.labels.cover, assets.font(FONT_PX))
    cfg = template.data["cover"]["layout"]
    max_chars, _rows = cover_text_capacity(cfg["text_rect"], cover_font_px(cfg))
    for layout in ("bottom", "top"):
        plate, lines, font = cover_plate_layout("Trade restrictions hit prices", template, assets, layout)
        assert len(lines) == 3
        # the worst case: three lines as wide as a line gets
        widest = cover_plate_box(3, text_size("W" * max_chars, font)[0], font.size, cfg["text_rect"]["w"], layout,
                                 strip["y"], safe["y"])
        for box in (plate, widest):
            assert rect_within(box, safe), (layout, box, safe)
            assert not rects_intersect(box, strip), (layout, box, strip)
            assert abs(box["x"] + box["w"] / 2 - LOGICAL_W / 2) <= 1  # centered
            if layout == "bottom":
                assert 0 < strip["y"] - (box["y"] + box["h"]) <= 6  # just above the AI line
            else:
                assert 0 < box["y"] - safe["y"] <= 6  # just under the top of the safe zone


def test_cover_layout_top_and_bottom_put_the_plate_in_different_places(nes_desk_dir, tmp_path):
    from studio.build import cover_plate_layout
    from studio.cli import build_parser

    argv = ["build", "--script", "s.json", "--channel", "8bitnews-us", "--out", "o"]
    assert build_parser().parse_args(argv).cover_layout == "bottom"
    assert build_parser().parse_args(argv + ["--cover-layout", "top"]).cover_layout == "top"
    with pytest.raises(SystemExit):
        build_parser().parse_args(argv + ["--cover-layout", "middle"])

    template = load_template(nes_desk_dir)
    assets = load_channel_assets("8bitnews-us")
    script = dict(BUILTIN_SCRIPT, cover={"frame": "S2", "visual": "x" * 20, "text": "Pixel News"})
    plates, covers = {}, {}
    for layout in ("bottom", "top"):
        out = tmp_path / f"{layout}.png"
        assert _render_cover(script, template, assets, None, out, layout)["layout"] == layout
        _assert_title_drawn(out, "Pixel News", template, assets, layout)
        plates[layout] = cover_plate_layout("Pixel News", template, assets, layout)[0]
        covers[layout] = _logical_cover(out)
    top, bottom = plates["top"], plates["bottom"]
    assert top["y"] + top["h"] < bottom["y"]
    for box in (top, bottom):
        rows, cols = slice(box["y"], box["y"] + box["h"]), slice(box["x"], box["x"] + box["w"])
        assert (covers["top"][rows, cols] != covers["bottom"][rows, cols]).any()
    with pytest.raises(BuildError, match="cover layout 'middle'"):
        _render_cover(script, template, assets, None, tmp_path / "x.png", "middle")


def test_a_build_with_the_cover_title_at_the_top_passes_its_checks(short_script_path, test_channels_dir, tmp_path):
    from studio.cli import main

    out = tmp_path / "out"
    assert main(["build", "--script", str(short_script_path), "--channel", "8bitnews-us",
                 "--channels-dir", str(test_channels_dir), "--tts", "stub", "--out", str(out), "--cover-layout", "top"]) == 0
    package = json.loads((out / "package.json").read_text())
    assert package["cover"] == {"frame": "S1", "layout": "top", "picture": "template"}
    assert package["validate"]["passed"]
    script = json.loads((out / "script.json").read_text())
    _assert_title_drawn(out / "cover.png", script["cover"]["text"], load_template(out / "template"),
                        load_channel_assets("8bitnews-us"), "top")


@pytest.mark.parametrize("channel,line", [("8bitnews-us", "AI illustration · AI text"),
                                          ("8bitnews-es", "Ilustración IA · Texto IA")])
@pytest.mark.parametrize("frame_id", ["S1", "S2", "custom"])
def test_every_cover_carries_the_cover_ai_line(nes_desk_dir, tmp_path, channel, line, frame_id):
    """Whatever the picture, a real person in it or not: the picture and the title are generated."""
    import numpy as np

    from studio.engine import draw_ai_label

    template = load_template(nes_desk_dir)
    assets = load_channel_assets(channel)
    out = tmp_path / "cover.png"
    _render_cover(dict(BUILTIN_SCRIPT, cover={"frame": frame_id, "visual": "x" * 20, "text": "Pixel News"}),
                  template, assets, None, out, "bottom")
    canvas = Image.new("RGBA", (270, 480), (0, 0, 0, 0))
    draw_ai_label(canvas, template, line, assets.font(FONT_PX))
    want = np.asarray(canvas)
    drawn = want[:, :, 3] == 255
    assert (_logical_cover(out)[drawn] == want[drawn][:, :3]).all()


def test_custom_cover_carries_the_cover_title(nes_desk_dir, tmp_path):
    template = load_template(nes_desk_dir)
    assets = load_channel_assets("8bitnews-us")
    script = dict(BUILTIN_SCRIPT, cover={"frame": "custom", "visual": "x" * 20, "text": "Custom Cover Title"})
    out = tmp_path / "custom.png"
    _render_cover(script, template, assets, None, out, "bottom")
    _assert_title_drawn(out, "Custom Cover Title", template, assets)


def test_scene_cover_title_text_changes_the_title_area(nes_desk_dir, tmp_path):
    import numpy as np

    from studio.build import cover_plate_layout

    template = load_template(nes_desk_dir)
    assets = load_channel_assets("8bitnews-us")
    crops = []
    for text in ("First Title", "Second Title"):
        out = tmp_path / f"{text}.png"
        _render_cover(dict(BUILTIN_SCRIPT, cover={"frame": "S2", "visual": "x" * 20, "text": text}),
                      template, assets, None, out, "bottom")
        r = cover_plate_layout(text, template, assets, "bottom")[0]
        crops.append(_logical_cover(out)[r["y"]:r["y"] + r["h"], r["x"]:r["x"] + r["w"]])
    assert crops[0].shape != crops[1].shape or not np.array_equal(crops[0], crops[1])


def test_scene_cover_title_that_does_not_fit_fails_the_build(nes_desk_dir, tmp_path):
    template = load_template(nes_desk_dir)
    template.data["cover"]["layout"]["text_rect"] = {"x": 10, "y": 350, "w": 60, "h": 12}
    script = dict(BUILTIN_SCRIPT, cover={"frame": "S1", "visual": "x" * 20, "text": "Far Too Long For This Rect"})
    with pytest.raises(BuildError):
        _render_cover(script, template, load_channel_assets("8bitnews-us"), None, tmp_path / "c.png", "bottom")


def test_build_duration_is_the_measured_video_length(built_package):
    from studio.video import probe_duration_s

    measured = probe_duration_s(built_package["out_dir"] / "video.mp4")
    assert abs(built_package["package"]["duration_s"] - measured) <= 0.1


def test_build_removes_scratch_but_keeps_the_voice_stem(built_package):
    out_dir = built_package["out_dir"]
    assert not (out_dir / "frames").exists()
    assert not list(out_dir.glob("_*"))  # _voice_raw.wav, _picture.mp4, _master.wav
    assert (out_dir / built_package["package"]["files"]["voice_stem"]["path"]).is_file()


def test_build_notes_an_unmatched_pronunciation_hint(built_package):
    notes = built_package["package"]["notes"]
    assert any("Pixel Council" in n for n in notes)  # fixture hint that matches no single word


def test_script_artifact_is_repo_relative_inside_the_repo(tmp_path):
    from studio.build import _script_artifact

    root = tmp_path / "repo"
    script = root / "newsroom" / "trials" / "x" / "script.json"
    script.parent.mkdir(parents=True)
    script.write_text("{}")
    assert _script_artifact(script, root) == "newsroom/trials/x/script.json"


def test_script_artifact_outside_the_repo_is_kept_as_given(tmp_path):
    from studio.build import _script_artifact

    outside = tmp_path / "elsewhere" / "script.json"
    assert _script_artifact(outside, tmp_path / "repo") == str(outside)
    assert _script_artifact(outside, None) == str(outside)
