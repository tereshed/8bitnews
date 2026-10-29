"""Plates hold up to max_lines of scene text and never cut it (E2a-2b);
template validation of plate heights and the cover title rect."""
import json
import os
import shutil
from pathlib import Path

import pytest

from studio.build import BuildError, check_plate_text
from studio.engine import plate_lines, plate_max_lines
from studio.errors import TemplateValidationError
from studio.template_pkg import load_template
from studio.timeline import SceneVoice, Word, timeline_from_voice
from studio.validate import validate_template

REPO = Path(os.environ["STUDIO_REPO_ROOT"])
NINTH = REPO / "newsroom" / "trials" / "2026-10-26-ninth-circuit" / "script.json"
CA_BALLOTS = REPO / "newsroom" / "trials" / "2026-10-24-ca-ballots" / "script_team.json"


def _edited_template(src: Path, tmp_path: Path, edit) -> Path:
    d = tmp_path / src.name
    shutil.copytree(src, d)
    data = json.loads((d / "template.json").read_text())
    edit(data)
    (d / "template.json").write_text(json.dumps(data))
    return d


def _problems(template_dir: Path) -> str:
    with pytest.raises(TemplateValidationError) as e:
        validate_template(template_dir)
    return str(e.value)


# --- the trial scripts' source dates come out whole ---


def test_ninth_circuit_source_plates_keep_their_dates(nes_desk_dir):
    template = load_template(nes_desk_dir)
    scenes = {s["id"]: s for s in json.loads(NINTH.read_text())["scenes"]}
    for scene_id, date in (("S3", "· Oct 13-15"), ("S7", "· Oct 20-24")):
        lines = plate_lines(template, "source_plate", scenes[scene_id]["source_plate"])
        assert len(lines) <= plate_max_lines(template, "source_plate")
        assert lines[-1].endswith(date), lines


@pytest.mark.parametrize("path", [NINTH, CA_BALLOTS])
@pytest.mark.parametrize("template_name", ["nes-desk", "night-report"])
def test_trial_scripts_fit_every_plate(path, template_name):
    from studio.paths import TEMPLATES_DIR

    data = json.loads(path.read_text())
    check_plate_text(data.get("script", data), load_template(TEMPLATES_DIR / template_name))


# --- build refuses text that needs more lines than the plate holds ---


def _script_with(field, text):
    return {"scenes": [{"id": "S4", field: text}]}


def test_build_rejects_source_plate_over_three_lines(nes_desk_dir):
    text = "Source: one two three four five six seven eight nine ten eleven twelve thirteen fourteen · Oct 1"
    with pytest.raises(BuildError) as e:
        check_plate_text(_script_with("source_plate", text), load_template(nes_desk_dir))
    msg = str(e.value)
    assert "scene S4" in msg and "source_plate" in msg and "needs 5 lines" in msg and "holds 3" in msg


def test_build_rejects_on_screen_text_over_two_lines(nes_desk_dir):
    # format.md: scene title up to 2 lines
    with pytest.raises(BuildError) as e:
        check_plate_text(_script_with("on_screen_text", "Officials call it intimidation, counties push back"),
                         load_template(nes_desk_dir))
    assert "on_screen_text" in str(e.value) and "needs 3 lines" in str(e.value) and "holds 2" in str(e.value)


def test_build_checks_plate_text_before_any_tts(short_script_path, test_channels_dir, tmp_path, monkeypatch):
    import studio.build as build_mod

    class _NoTts:
        def __init__(self, **kwargs):
            pass

        def synthesize(self, text, voice_id):
            raise AssertionError("TTS ran before the plate text check")

    monkeypatch.setitem(build_mod.ADAPTERS, "stub", _NoTts)
    script = json.loads(short_script_path.read_text())
    script["scenes"][2]["on_screen_text"] = "Officials call it intimidation, counties push back"
    path = tmp_path / "script.json"
    path.write_text(json.dumps(script))
    with pytest.raises(BuildError) as e:
        build_mod.build_package(script_path=path, channel_id="8bitnews-us", out_dir=tmp_path / "out",
                                channels_dir=test_channels_dir, tts_name="stub")
    assert "scene S3: on_screen_text" in str(e.value)


def test_plate_max_lines_defaults_to_two(nes_desk_dir, tmp_path):
    d = _edited_template(nes_desk_dir, tmp_path, lambda d: d["plates"]["source_plate"].pop("max_lines"))
    assert plate_max_lines(load_template(d), "source_plate") == 2


# --- template validate: plate height, cover title rect ---


def test_validate_rejects_plate_too_low_for_its_max_lines(nes_desk_dir, tmp_path):
    d = _edited_template(nes_desk_dir, tmp_path, lambda d: d["plates"]["source_plate"]["rect"].update(h=28))
    assert "plates.source_plate.rect h=28 is too low for max_lines=3: needs at least 32" in _problems(d)


def test_validate_rejects_cover_rect_too_small_for_a_30_char_title(night_report_dir, tmp_path):
    # 23 characters a line, h=22 holds 2 lines; "Voter disenfranchisement fears" needs 3
    d = _edited_template(night_report_dir, tmp_path, lambda d: d["cover"]["layout"]["text_rect"].update(w=188, h=22))
    assert "fits 2 line(s) of 23 characters; a 30-character title can need 3" in _problems(d)


def test_worst_case_title_lines_matches_a_real_three_line_title():
    from studio.cover_title import worst_case_title_lines
    from studio.subtitles import wrap_words

    assert len(wrap_words("Voter disenfranchisement fears".split(), 23)) == 3
    assert worst_case_title_lines(23) == 3  # nes-desk / night-report width
    assert worst_case_title_lines(10) == 5


def test_worst_case_title_lines_agrees_with_brute_force():
    from studio.cover_title import worst_case_title_lines
    from studio.subtitles import wrap_words

    def brute(max_chars, max_len):
        best = 0

        def rec(lengths, used):
            nonlocal best
            if lengths:
                best = max(best, len(wrap_words(["W" * n for n in lengths], max_chars)))
            for n in range(1, max_chars + 1):
                total = used + n + (1 if lengths else 0)
                if total > max_len:
                    break
                rec(lengths + [n], total)

        rec([], 0)
        return best

    for max_chars in (4, 5, 7, 9):
        assert worst_case_title_lines(max_chars, 14) == brute(max_chars, 14)


def test_validate_rejects_cover_rect_over_the_face(nes_desk_dir, tmp_path):
    d = _edited_template(nes_desk_dir, tmp_path, lambda d: d["cover"]["layout"]["text_rect"].update(y=170))
    assert "cover.layout.text_rect" in _problems(d) and "overlaps the face of anchor" in _problems(d)


def test_validate_rejects_cover_rect_over_the_ai_line(nes_desk_dir, tmp_path):
    d = _edited_template(nes_desk_dir, tmp_path, lambda d: d["cover"]["layout"]["text_rect"].update(y=340))
    assert "overlaps the AI line" in _problems(d)


# --- pauses between scenes hold the previous scene, not a black frame ---


def test_scene_at_holds_the_previous_scene_during_the_pause():
    voices = [
        SceneVoice("S1", "anchor", (Word("one", 0.0, 0.5), Word("two", 0.5, 1.0))),
        SceneVoice("S2", "scene", (Word("three", 0.0, 0.5),)),
    ]
    timeline = timeline_from_voice(voices, 0.3, 2, 23)
    assert timeline.scene_at(1.15).scene_id == "S1"  # inside the 1.0-1.3 s pause
    assert timeline.scene_at(1.3).scene_id == "S2"
    assert timeline.scene_at(99).scene_id == "S2"
