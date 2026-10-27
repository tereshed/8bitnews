"""Review follow-up (cycle 1): every field in template.schema.json must
actually influence a rendered frame, or it should be removed from the
schema. These tests exercise motion preset selection, transitions and the
palette-subset/anchor-protection split in studio/engine.py directly."""
from __future__ import annotations

import numpy as np
from PIL import Image

from studio import engine
from studio.builtin_script import BUILTIN_SCRIPT, builtin_timeline
from studio.channel_assets import load_channel_assets
from studio.constants import LOGICAL_H, LOGICAL_W
from studio.engine import render_frame
from studio.template_pkg import load_template


def test_scene_motion_presets_are_selected_round_robin_by_scene_index(nes_desk_dir):
    template = load_template(nes_desk_dir)
    presets = engine._scene_motion_presets(template)
    assert len(presets) >= 2  # nes-desk ships pan-right and zoom-in

    chosen_ids = [engine._select_scene_motion(template, i)["id"] for i in range(5)]
    expected = [presets[i % len(presets)]["id"] for i in range(5)]
    assert chosen_ids == expected


def test_zoom_progress_past_half_crops_an_oversized_render_not_a_resize(nes_desk_dir):
    """The zoom preset ('zoom-in', duration_s=3.0): before half its duration
    the scene shot is the plain frame; after half, a 2x-oversized render is
    cropped (never resized) to the same logical size -- so the two halves
    must differ in content but never in size."""
    template = load_template(nes_desk_dir)
    scene = BUILTIN_SCRIPT["scenes"][1]  # S2, a `shot: scene` scene
    timeline = builtin_timeline()
    scene_timing = next(s for s in timeline.scenes if s.scene_id == "S2")

    early = engine._render_scene_shot(BUILTIN_SCRIPT, scene, scene_timing, template, scene_timing.start_s, None, template.palette(), load_channel_assets().font(8))
    late = engine._render_scene_shot(BUILTIN_SCRIPT, scene, scene_timing, template, scene_timing.start_s + 2.9, None, template.palette(), load_channel_assets().font(8))

    assert early.size == (LOGICAL_W, LOGICAL_H)
    assert late.size == (LOGICAL_W, LOGICAL_H)
    assert not np.array_equal(np.array(early), np.array(late))


def test_pan_shifts_crop_window_by_integer_pixels(nes_desk_dir):
    """S3's selected preset (index 2 % 2 == 0) is pan-right, dx_px=24: the
    window shifts right as progress advances, so frames at different
    progress must differ, and both stay the exact logical size (no resize
    is ever used for a pan)."""
    template = load_template(nes_desk_dir)
    scene = BUILTIN_SCRIPT["scenes"][2]  # S3
    timeline = builtin_timeline()
    scene_timing = next(s for s in timeline.scenes if s.scene_id == "S3")
    font = load_channel_assets().font(8)

    start = engine._render_scene_shot(BUILTIN_SCRIPT, scene, scene_timing, template, scene_timing.start_s, None, template.palette(), font)
    mid = engine._render_scene_shot(BUILTIN_SCRIPT, scene, scene_timing, template, scene_timing.start_s + 1.5, None, template.palette(), font)

    assert start.size == (LOGICAL_W, LOGICAL_H) == mid.size
    assert not np.array_equal(np.array(start), np.array(mid))


def test_apply_transition_cut_is_an_instant_switch():
    prev = Image.new("RGB", (LOGICAL_W, LOGICAL_H), (10, 10, 10))
    curr = Image.new("RGB", (LOGICAL_W, LOGICAL_H), (200, 200, 200))
    out = engine._apply_transition("cut", prev, curr, 0.3)
    assert np.array_equal(np.array(out), np.array(curr))


def test_apply_transition_wipe_reveals_current_on_the_left_only():
    prev = Image.new("RGB", (LOGICAL_W, LOGICAL_H), (10, 10, 10))
    curr = Image.new("RGB", (LOGICAL_W, LOGICAL_H), (200, 200, 200))
    out = np.array(engine._apply_transition("wipe", prev, curr, 0.5))
    x = round(LOGICAL_W * 0.5)
    assert (out[:, :x] == 200).all()
    assert (out[:, x + 2 :] == 10).all()


def test_apply_transition_slide_moves_both_frames_by_integer_offset():
    prev = Image.new("RGB", (LOGICAL_W, LOGICAL_H), (10, 10, 10))
    curr = Image.new("RGB", (LOGICAL_W, LOGICAL_H), (200, 200, 200))
    out = np.array(engine._apply_transition("slide", prev, curr, 0.25))
    offset = round(LOGICAL_W * 0.25)
    # prev has slid left by `offset`; curr enters from the right at the same offset.
    assert (out[:, LOGICAL_W - offset :] == 200).all()
    assert (out[:, : LOGICAL_W - offset - 2] == 10).all()


def test_apply_transition_pixelate_reveals_more_blocks_as_progress_grows():
    prev = Image.new("RGB", (LOGICAL_W, LOGICAL_H), (10, 10, 10))
    curr = Image.new("RGB", (LOGICAL_W, LOGICAL_H), (200, 200, 200))
    early = np.array(engine._apply_transition("pixelate", prev, curr, 0.1))
    late = np.array(engine._apply_transition("pixelate", prev, curr, 0.9))
    assert (early == 200).sum() < (late == 200).sum()


def test_transition_at_scene_boundary_blends_into_the_first_frame(nes_desk_dir):
    """render_frame (not the internal helper) must actually use the boundary
    transition: nes-desk's transitions round-robin to 'pixel-wipe' (a
    pixelate, duration_s=0.4) at the S1->S2 boundary. Two points inside that
    0.4s window (both still before the 3s AI-voice/-icon cutover, so the
    plates drawn on top are identical either way) must differ, because the
    pixelate reveal fraction grows with elapsed time."""
    template = load_template(nes_desk_dir)
    assets = load_channel_assets("8bitnews-us")
    timeline = builtin_timeline()
    s2 = next(s for s in timeline.scenes if s.scene_id == "S2")
    assert s2.start_s + 0.39 < 3.0  # stay on the same side of the ai_voice/ai_icon cutover

    just_inside = render_frame(BUILTIN_SCRIPT, timeline, template, assets, s2.start_s + 0.01)
    near_end_of_transition = render_frame(BUILTIN_SCRIPT, timeline, template, assets, s2.start_s + 0.39)

    assert just_inside.size == (LOGICAL_W, LOGICAL_H)
    assert not np.array_equal(np.array(just_inside), np.array(near_end_of_transition))


def test_scene_shot_colors_are_restricted_to_template_palette_subset(night_report_dir):
    """Regression for the review's 'pink placeholder in night-report preview'
    bug: a `shot: scene` frame (background + plates + subtitles, no anchor)
    must only use colors from the *template's* palette.colors, not the full
    54-color channel master palette."""
    template = load_template(night_report_dir)
    assets = load_channel_assets("8bitnews-us")
    timeline = builtin_timeline()
    subset = {c.lower() for c in template.data["palette"]["colors"]}

    s3 = next(s for s in timeline.scenes if s.scene_id == "S3")
    mid_t = (s3.start_s + s3.end_s) / 2
    frame = render_frame(BUILTIN_SCRIPT, timeline, template, assets, mid_t)
    for _count, rgb in frame.getcolors(maxcolors=1_000_000):
        assert ("#%02x%02x%02x" % rgb) in subset


def test_anchor_shot_is_not_restricted_to_template_palette_subset(night_report_dir):
    """The anchor's own sprite colors (skin, hair, suit) must survive
    untouched even though night-report's palette subset doesn't include
    them -- the review's point 2: only the studio/scene art gets muted, not
    the anchor."""
    template = load_template(night_report_dir)
    assets = load_channel_assets("8bitnews-us")
    timeline = builtin_timeline()
    subset = {c.lower() for c in template.data["palette"]["colors"]}

    s1 = next(s for s in timeline.scenes if s.scene_id == "S1")
    mid_t = (s1.start_s + s1.end_s) / 2
    frame = render_frame(BUILTIN_SCRIPT, timeline, template, assets, mid_t)
    colors = {("#%02x%02x%02x" % rgb) for _count, rgb in frame.getcolors(maxcolors=1_000_000)}
    assert colors - subset, "expected at least one anchor-sprite color outside the template's muted subset"


def test_serious_mode_does_not_recolor_the_anchor(nes_desk_dir):
    """Rendering the same anchor scene in serious_mode must keep the exact
    same anchor skin/hair colors (only the pose sprite may change, not the
    palette the sprite is drawn in); regression for the review's point 2
    (the old code re-quantized the *whole* frame to serious_colors)."""
    template = load_template(nes_desk_dir)
    assets = load_channel_assets("8bitnews-us")

    normal_script = dict(BUILTIN_SCRIPT, serious_mode=False)
    serious_script = dict(BUILTIN_SCRIPT, serious_mode=True)
    timeline = builtin_timeline()
    s1 = next(s for s in timeline.scenes if s.scene_id == "S1")
    mid_t = (s1.start_s + s1.end_s) / 2

    normal_frame = render_frame(normal_script, timeline, template, assets, mid_t)
    serious_frame = render_frame(serious_script, timeline, template, assets, mid_t)

    skin = "#fce0a8"
    normal_colors = {("#%02x%02x%02x" % rgb) for _c, rgb in normal_frame.getcolors(1_000_000)}
    serious_colors = {("#%02x%02x%02x" % rgb) for _c, rgb in serious_frame.getcolors(1_000_000)}
    assert skin in normal_colors
    assert skin in serious_colors
