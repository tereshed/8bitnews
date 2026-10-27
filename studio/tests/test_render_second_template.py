"""A template is a data package, not code (docs/architecture.md §5): the
same engine must render a second, differently-styled package without any
template-specific branches in studio/engine.py."""
import numpy as np

from studio.builtin_script import BUILTIN_SCRIPT, builtin_timeline
from studio.channel_assets import load_channel_assets
from studio.constants import LOGICAL_H, LOGICAL_W
from studio.engine import render_frame
from studio.template_pkg import load_template


def test_second_template_renders_same_shape_and_valid_palette(night_report_dir):
    from studio.palette import channel_palette_set

    template = load_template(night_report_dir)
    assets = load_channel_assets("8bitnews-us")
    timeline = builtin_timeline()
    palette_set = channel_palette_set("8bitnews-us")

    for t in [0.0, timeline.duration_s / 2, timeline.duration_s - 0.01]:
        frame = render_frame(BUILTIN_SCRIPT, timeline, template, assets, t)
        assert frame.size == (LOGICAL_W, LOGICAL_H)
        colors = frame.getcolors(maxcolors=1_000_000)
        assert colors is not None
        for _count, rgb in colors:
            assert ("#%02x%02x%02x" % rgb) in palette_set


def test_two_templates_produce_visibly_different_frames(nes_desk_dir, night_report_dir):
    nes_desk = load_template(nes_desk_dir)
    night_report = load_template(night_report_dir)
    assets = load_channel_assets("8bitnews-us")
    timeline = builtin_timeline()

    frame_a = render_frame(BUILTIN_SCRIPT, timeline, nes_desk, assets, 0.0)
    frame_b = render_frame(BUILTIN_SCRIPT, timeline, night_report, assets, 0.0)
    assert not np.array_equal(np.array(frame_a), np.array(frame_b))
