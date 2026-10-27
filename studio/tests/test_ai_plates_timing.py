from studio.builtin_script import BUILTIN_SCRIPT, builtin_timeline
from studio.constants import AI_VOICE_PLATE_S
from studio.engine import active_plates
from studio.template_pkg import load_template


def test_ai_voice_plate_only_in_first_three_seconds(nes_desk_dir):
    template = load_template(nes_desk_dir)
    timeline = builtin_timeline()

    before = active_plates(BUILTIN_SCRIPT, timeline.scene_at(0.0), template, 0.0)
    assert "ai_voice" in before
    assert "ai_icon" not in before

    just_before = active_plates(BUILTIN_SCRIPT, timeline.scene_at(AI_VOICE_PLATE_S - 0.1), template, AI_VOICE_PLATE_S - 0.1)
    assert "ai_voice" in just_before

    after = active_plates(BUILTIN_SCRIPT, timeline.scene_at(AI_VOICE_PLATE_S + 0.1), template, AI_VOICE_PLATE_S + 0.1)
    assert "ai_icon" in after
    assert "ai_voice" not in after


def test_ai_voice_and_ai_icon_are_mutually_exclusive(nes_desk_dir):
    template = load_template(nes_desk_dir)
    timeline = builtin_timeline()
    for t in [0.0, 1.0, 2.9, 3.0, 3.1, 5.0, timeline.duration_s - 0.01]:
        plates = active_plates(BUILTIN_SCRIPT, timeline.scene_at(t), template, t)
        assert ("ai_voice" in plates) != ("ai_icon" in plates)


def test_ai_illustration_shows_whenever_a_real_person_is_in_frame(nes_desk_dir):
    """Not just for a scene's own `real_people`: an anchor shot (S1) puts the
    *next* scene-shot's image on its over-the-shoulder screen
    (engine._next_scene_shot), so S1 must also carry the disclosure for as
    long as that next scene (S2) has a real person on it."""
    template = load_template(nes_desk_dir)
    timeline = builtin_timeline()

    by_scene_id = {s["id"]: s for s in BUILTIN_SCRIPT["scenes"]}
    expect_plate = {"S1": True, "S2": True, "S3": False}  # S1's screen shows S2
    for scene_timing in timeline.scenes:
        scene = by_scene_id[scene_timing.scene_id]
        mid_t = (scene_timing.start_s + scene_timing.end_s) / 2
        plates = active_plates(BUILTIN_SCRIPT, scene_timing, template, mid_t)
        if expect_plate[scene["id"]]:
            assert "ai_illustration" in plates, scene["id"]
        else:
            assert "ai_illustration" not in plates, scene["id"]


def test_ai_illustration_holds_for_the_whole_scene(nes_desk_dir):
    """format.md: the plate stays up the entire time a real person is on
    screen, not just at one instant."""
    template = load_template(nes_desk_dir)
    timeline = builtin_timeline()
    s2 = next(s for s in timeline.scenes if s.scene_id == "S2")
    assert s2.end_s > s2.start_s
    samples = [s2.start_s, (s2.start_s + s2.end_s) / 2, s2.end_s - 0.01]
    for t in samples:
        assert "ai_illustration" in active_plates(BUILTIN_SCRIPT, s2, template, t)
