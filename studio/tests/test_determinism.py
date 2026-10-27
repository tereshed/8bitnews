import numpy as np

from studio.builtin_script import BUILTIN_SCRIPT, builtin_timeline
from studio.channel_assets import load_channel_assets
from studio.engine import render_frame
from studio.template_pkg import load_template


def test_render_frame_is_deterministic(nes_desk_dir):
    template = load_template(nes_desk_dir)
    assets = load_channel_assets("8bitnews-us")
    timeline = builtin_timeline()

    for t in [0.0, 1.3, 2.9, 3.0, 3.1, 6.7, timeline.duration_s - 0.01]:
        frame_a = render_frame(BUILTIN_SCRIPT, timeline, template, assets, t)
        frame_b = render_frame(BUILTIN_SCRIPT, timeline, template, assets, t)
        assert np.array_equal(np.array(frame_a), np.array(frame_b)), f"frame at t={t} is not deterministic"


def test_full_timeline_render_is_deterministic_across_runs(nes_desk_dir):
    template = load_template(nes_desk_dir)
    assets = load_channel_assets("8bitnews-us")
    timeline = builtin_timeline()
    sample_times = [i * 0.37 for i in range(int(timeline.duration_s / 0.37))]

    def render_all():
        return [np.array(render_frame(BUILTIN_SCRIPT, timeline, template, assets, t)) for t in sample_times]

    run1 = render_all()
    run2 = render_all()
    assert len(run1) == len(run2)
    for a, b in zip(run1, run2):
        assert np.array_equal(a, b)


def test_uniform_timeline_is_deterministic():
    t1 = builtin_timeline()
    t2 = builtin_timeline()
    assert t1.duration_s == t2.duration_s
    for s1, s2 in zip(t1.scenes, t2.scenes):
        assert s1.start_s == s2.start_s
        assert s1.end_s == s2.end_s
        assert [w.text for w in s1.words] == [w.text for w in s2.words]
        assert [(w.start_s, w.end_s) for w in s1.words] == [(w.start_s, w.end_s) for w in s2.words]
