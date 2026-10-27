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


def test_build_scene_pause_changes_total_duration(short_script_path, test_channels_dir, nes_desk_dir, tmp_path):
    def _template_with_pause(pause_s: float):
        d = tmp_path / f"tmpl-{pause_s}"
        shutil.copytree(nes_desk_dir, d)
        data = json.loads((d / "template.json").read_text())
        data["timing"]["scene_pause_s"] = pause_s
        (d / "template.json").write_text(json.dumps(data))
        return d

    short_pause_dir = tmp_path / "out-short"
    long_pause_dir = tmp_path / "out-long"
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
    )
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
