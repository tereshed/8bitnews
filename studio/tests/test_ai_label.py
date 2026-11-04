"""The one AI line (format.md 'Текст в кадре'): "AI voice" on every frame,
joined by "AI illustration" while a real person is in frame."""
from studio.builtin_script import BUILTIN_SCRIPT, builtin_timeline
from studio.channel_assets import AiLabels, load_ai_labels
from studio.engine import active_plates, ai_label_text
from studio.template_pkg import load_template

LABELS = AiLabels(voice="AI voice", illustration="AI illustration")


def test_every_frame_says_ai_voice(nes_desk_dir):
    template = load_template(nes_desk_dir)
    timeline = builtin_timeline()
    for t in [0.0, 2.9, 3.1, 5.0, timeline.duration_s - 0.01]:
        assert ai_label_text(active_plates(BUILTIN_SCRIPT, timeline.scene_at(t), template, t), LABELS).startswith("AI voice")


def test_the_line_names_the_illustration_only_when_it_applies():
    assert ai_label_text(set(), LABELS) == "AI voice"
    assert ai_label_text({"ai_illustration"}, LABELS) == "AI voice · AI illustration"
    # a still cover has no voice to label
    assert ai_label_text(set(), LABELS, cover=True) is None
    assert ai_label_text({"ai_illustration"}, LABELS, cover=True) == "AI illustration"


def test_each_channel_labels_in_its_own_language():
    """format.md of each channel: a disclosure the audience can read."""
    assert load_ai_labels("8bitnews-us").widest == "AI voice · AI illustration"
    assert load_ai_labels("8bitnews-es").widest == "Voz IA · Ilustración IA"
    es = load_ai_labels("8bitnews-es")
    assert ai_label_text(set(), es) == "Voz IA"
    assert ai_label_text({"ai_illustration"}, es, cover=True) == "Ilustración IA"


def test_ai_illustration_shows_whenever_a_real_person_is_in_frame(nes_desk_dir):
    """Not just for a scene's own `real_people`: an anchor shot (S1) puts the
    *next* scene-shot's image on the screen behind the anchor when it has no
    picture of its own (engine._next_scene_shot), so S1 must also carry the
    disclosure for as long as that next scene (S2) has a real person on it."""
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
    """format.md: "AI illustration" stays up the entire time a real person
    is on screen, not just at one instant."""
    template = load_template(nes_desk_dir)
    timeline = builtin_timeline()
    s2 = next(s for s in timeline.scenes if s.scene_id == "S2")
    assert s2.end_s > s2.start_s
    samples = [s2.start_s, (s2.start_s + s2.end_s) / 2, s2.end_s - 0.01]
    for t in samples:
        assert "ai_illustration" in active_plates(BUILTIN_SCRIPT, s2, template, t)


def test_the_spanish_channel_is_complete():
    """Every anchor speaks Spanish at a set speed, and its AI line fits the templates."""
    from studio.channel_assets import anchor_ids, load_anchor, load_channel_assets
    from studio.config import tts_settings
    from studio.kokoro import _language_of_voice
    from studio.paths import TEMPLATES_DIR
    from studio.validate import validate_template

    ids = anchor_ids("8bitnews-es")
    assert len(ids) == 3
    voices = [load_anchor("8bitnews-es", a).voice_id for a in ids]
    assert len(set(voices)) == 3, "one face, one voice"
    settings = tts_settings("kokoro")["voices"]
    for voice in voices:
        assert _language_of_voice(voice) == "es"
        assert settings[voice]["speed"] == 1.0
    assert load_channel_assets("8bitnews-es").labels.voice == "Voz IA"
    for template in ("nes-desk", "night-report"):
        validate_template(TEMPLATES_DIR / template, "8bitnews-es")
