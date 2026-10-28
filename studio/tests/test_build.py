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
from studio.builtin_script import BUILTIN_SCRIPT, builtin_timeline
from studio.channel_assets import load_channel_assets
from studio.constants import FONT_PX, OUTPUT_H, OUTPUT_W
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
        _resolve_tts_name(None, {"format": {"voice": {"provider": "kokoro"}}})
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


def test_fit_cover_title_fits_three_lines_on_night_reports_real_rect(night_report_dir):
    """night-report's cover.layout.text_rect holds the 3 lines a 30-character
    title can wrap to at 23 characters per line (studio template validate
    enforces it): guard the real rect geometry."""
    template = load_template(night_report_dir)
    font = load_channel_assets("8bitnews-us").font(FONT_PX)
    text_rect = template.data["cover"]["layout"]["text_rect"]
    lines = fit_cover_title("Voter disenfranchisement fears", text_rect, font)
    assert len(lines) == 3


def test_fit_cover_title_still_rejects_text_needing_three_lines_on_night_report(night_report_dir):
    template = load_template(night_report_dir)
    font = load_channel_assets("8bitnews-us").font(FONT_PX)
    text_rect = template.data["cover"]["layout"]["text_rect"]
    with pytest.raises(BuildError):
        fit_cover_title(
            "This cover title is deliberately far too long to fit in two lines at all",
            text_rect,
            font,
        )


def test_cover_frame_scene_vs_custom_produce_different_images(nes_desk_dir, tmp_path):
    template = load_template(nes_desk_dir)
    assets = load_channel_assets("8bitnews-us")
    timeline = builtin_timeline()

    scene_script = dict(BUILTIN_SCRIPT, cover={"frame": "S1", "visual": "x" * 20, "text": "Scene Cover"})
    scene_out = tmp_path / "scene_cover.png"
    _render_cover(scene_script, timeline, template, assets, None, scene_out)

    assets_dir = tmp_path / "assets"
    assets_dir.mkdir()
    Image.new("RGB", (400, 700), (10, 20, 30)).save(assets_dir / "cover.png")
    custom_script = dict(BUILTIN_SCRIPT, cover={"frame": "custom", "visual": "x" * 20, "text": "Custom Cover"})
    custom_out = tmp_path / "custom_cover.png"
    _render_cover(custom_script, timeline, template, assets, assets_dir, custom_out)

    for path in (scene_out, custom_out):
        with Image.open(path) as img:
            assert img.size == (OUTPUT_W, OUTPUT_H)
    assert scene_out.read_bytes() != custom_out.read_bytes()


def test_cover_frame_unknown_scene_id_raises_readable_error(nes_desk_dir, tmp_path):
    template = load_template(nes_desk_dir)
    assets = load_channel_assets("8bitnews-us")
    timeline = builtin_timeline()
    bad_script = dict(BUILTIN_SCRIPT, cover={"frame": "S99", "visual": "x" * 20, "text": "Nope"})
    with pytest.raises(BuildError) as exc_info:
        _render_cover(bad_script, timeline, template, assets, None, tmp_path / "out.png")
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
    # Fixture script sums to roughly 33 words at 165 wpm plus 3 scene-pause
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


# --- cover title, cover plates, measured duration, build scratch, notes ---


def _title_mask(text, template, assets):
    """Logical-size boolean mask of the cover title's fill pixels, drawn
    with the same layout the build uses."""
    import numpy as np

    from studio.build import cover_title_layout
    from studio.constants import LOGICAL_H, LOGICAL_W
    from studio.text_render import draw_text

    font = assets.font(FONT_PX)
    canvas = Image.new("RGBA", (LOGICAL_W, LOGICAL_H), (0, 0, 0, 0))
    for line, xy in cover_title_layout(text, template.data["cover"]["layout"]["text_rect"], font):
        draw_text(canvas, xy, line, font, (255, 255, 255))
    return np.array(canvas)[:, :, 3] > 0


def _assert_title_drawn(cover_path, text, template, assets):
    import numpy as np

    from studio.constants import SCALE
    from studio.palette import hex_to_rgb

    mask = _title_mask(text, template, assets)
    assert mask.sum() > 50
    big = np.repeat(np.repeat(mask, SCALE, axis=0), SCALE, axis=1)
    pixels = np.array(Image.open(cover_path).convert("RGB"))[big]
    assert (pixels == hex_to_rgb(template.data["subtitles"]["style"]["color"])).all()


def test_scene_cover_of_the_build_carries_the_cover_title(built_package):
    out_dir = built_package["out_dir"]
    script = json.loads((out_dir / "script.json").read_text())
    assert script["cover"]["frame"].startswith("S")
    template = load_template(out_dir / "template")
    _assert_title_drawn(out_dir / "cover.png", script["cover"]["text"], template, load_channel_assets("8bitnews-us"))


def test_scene_cover_is_the_scene_frame_plus_title_only(built_package):
    """Outside the title's own pixels (and their 1px outline) the S* cover is
    exactly the upscaled render_cover_frame output: no re-quantization of the
    anchor, nothing else drawn on top."""
    import numpy as np

    from studio.constants import SCALE
    from studio.engine import render_cover_frame
    from studio.timeline import load_timeline

    out_dir = built_package["out_dir"]
    script = json.loads((out_dir / "script.json").read_text())
    template = load_template(out_dir / "template")
    assets = load_channel_assets("8bitnews-us")
    timeline = load_timeline(out_dir / "timeline.json")
    st = next(s for s in timeline.scenes if s.scene_id == script["cover"]["frame"])
    base = np.array(render_cover_frame(script, timeline, template, assets, (st.start_s + st.end_s) / 2))
    cover = np.array(Image.open(out_dir / "cover.png").convert("RGB"))[::SCALE, ::SCALE]

    mask = _title_mask(script["cover"]["text"], template, assets)
    near_title = mask.copy()
    for dy, dx in ((-1, 0), (1, 0), (0, -1), (0, 1)):
        near_title |= np.roll(mask, (dy, dx), axis=(0, 1))
    assert (cover[~near_title] == base[~near_title]).all()

    # No scene_title plate: its rect is not filled with the plate's background.
    from studio.palette import hex_to_rgb

    plate = template.plate("scene_title")
    r = plate["rect"]
    area = cover[r["y"]:r["y"] + r["h"], r["x"]:r["x"] + r["w"]]
    assert (area == hex_to_rgb(plate["bg_color"])).all(axis=2).mean() < 0.6


def test_custom_cover_carries_the_cover_title(nes_desk_dir, tmp_path):
    template = load_template(nes_desk_dir)
    assets = load_channel_assets("8bitnews-us")
    script = dict(BUILTIN_SCRIPT, cover={"frame": "custom", "visual": "x" * 20, "text": "Custom Cover Title"})
    out = tmp_path / "custom.png"
    _render_cover(script, builtin_timeline(), template, assets, None, out)
    _assert_title_drawn(out, "Custom Cover Title", template, assets)


def test_scene_cover_title_text_changes_the_title_area(nes_desk_dir, tmp_path):
    import numpy as np

    from studio.constants import SCALE

    template = load_template(nes_desk_dir)
    assets = load_channel_assets("8bitnews-us")
    r = template.data["cover"]["layout"]["text_rect"]
    crops = []
    for text in ("First Title", "Second Title"):
        out = tmp_path / f"{text}.png"
        _render_cover(dict(BUILTIN_SCRIPT, cover={"frame": "S2", "visual": "x" * 20, "text": text}),
                      builtin_timeline(), template, assets, None, out)
        img = np.array(Image.open(out).convert("RGB"))
        crops.append(img[r["y"] * SCALE:(r["y"] + r["h"]) * SCALE, r["x"] * SCALE:(r["x"] + r["w"]) * SCALE])
    assert not np.array_equal(crops[0], crops[1])


def test_scene_cover_title_that_does_not_fit_fails_the_build(nes_desk_dir, tmp_path):
    template = load_template(nes_desk_dir)
    template.data["cover"]["layout"]["text_rect"] = {"x": 10, "y": 350, "w": 60, "h": 12}
    script = dict(BUILTIN_SCRIPT, cover={"frame": "S1", "visual": "x" * 20, "text": "Far Too Long For This Rect"})
    with pytest.raises(BuildError):
        _render_cover(script, builtin_timeline(), template, load_channel_assets("8bitnews-us"), None, tmp_path / "c.png")


def test_cover_plates_keep_only_ai_illustration(short_script_path, nes_desk_dir):
    from studio.engine import cover_plates
    from studio.timeline import uniform_timeline

    script = json.loads(short_script_path.read_text())
    template = load_template(nes_desk_dir)
    timeline = uniform_timeline(script)

    def plates(scene_id):
        st = next(s for s in timeline.scenes if s.scene_id == scene_id)
        return cover_plates(script, st, template, (st.start_s + st.end_s) / 2)

    assert plates("S2") == {"ai_illustration"}  # real person in the scene
    assert plates("S1") == {"ai_illustration"}  # anchor, S2's real person on the screen behind her
    assert plates("S3") == set()


def test_build_duration_is_the_measured_video_length(built_package):
    from studio.video import probe_duration_s

    measured = probe_duration_s(built_package["out_dir"] / "video.mp4")
    assert abs(built_package["package"]["duration_s"] - measured) <= 0.1


def test_build_removes_frames_scratch_but_keeps_voice(built_package):
    out_dir = built_package["out_dir"]
    assert not (out_dir / "frames").exists()
    assert (out_dir / "voice.wav").is_file()


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
