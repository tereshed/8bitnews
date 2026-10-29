"""The frame layout the owner asked for on 29.10: centered plates as wide
as their text (the source above the title), big centered subtitles on a
plate, the desk in front of the anchor, and what `studio template
validate` holds the templates to."""
import json
import shutil
from pathlib import Path

import numpy as np
import pytest
from PIL import Image

from studio.builtin_script import BUILTIN_SCRIPT, builtin_timeline
from studio.channel_assets import load_channel_assets
from studio.constants import FONT_PX, LOGICAL_W
from studio.engine import content_plate_boxes, render_frame, subtitle_box, subtitle_layout
from studio.errors import TemplateValidationError
from studio.template_pkg import load_template
from studio.validate import validate_template

FONT = load_channel_assets("8bitnews-us").font(FONT_PX)
SCENE = {"id": "S9", "shot": "scene", "on_screen_text": "JUDGE WEIGHS OUTSIDE PROBE",
         "source_plate": "Source: AP · Oct 27 hearing"}


def _problems(src: Path, tmp_path: Path, edit) -> str:
    d = tmp_path / src.name
    shutil.copytree(src, d)
    data = json.loads((d / "template.json").read_text())
    edit(data)
    (d / "template.json").write_text(json.dumps(data))
    with pytest.raises(TemplateValidationError) as e:
        validate_template(d)
    return str(e.value)


# --- plates ---


def test_the_source_sits_above_the_title(nes_desk_dir):
    boxes = content_plate_boxes(SCENE, {"scene_title", "source_plate"}, load_template(nes_desk_dir), FONT)
    source, title = boxes["source_plate"][0], boxes["scene_title"][0]
    assert title["y"] == source["y"] + source["h"] + 2


def test_the_title_alone_goes_to_the_top(nes_desk_dir):
    template = load_template(nes_desk_dir)
    alone = content_plate_boxes(SCENE, {"scene_title"}, template, FONT)["scene_title"][0]
    assert alone["y"] == template.plate("source_plate")["rect"]["y"]


@pytest.mark.parametrize("name", ["scene_title", "source_plate"])
def test_a_plate_is_centered_and_as_wide_as_its_text(nes_desk_dir, name):
    box, lines = content_plate_boxes(SCENE, {"scene_title", "source_plate"}, load_template(nes_desk_dir), FONT)[name]
    assert box["w"] == max(len(line) for line in lines) * FONT_PX + 6
    assert abs((box["x"] + box["w"] / 2) - LOGICAL_W / 2) <= 1


# --- subtitles ---


def test_subtitles_are_big_and_centered(nes_desk_dir):
    template = load_template(nes_desk_dir)
    assets = load_channel_assets("8bitnews-us")
    box, origins, scale = subtitle_box(template, assets, "DOJ's own lawyer said")
    assert scale == 2 and [line for line, _ in origins] == ["DOJ's own", "lawyer said"]
    assert abs((box["x"] + box["w"] / 2) - LOGICAL_W / 2) <= 1


def test_a_word_longer_than_a_line_drops_the_cue_to_the_small_font(nes_desk_dir):
    """Through the build's own cue split: the word stays whole on screen and in the SRT."""
    from studio.subtitles import split_cues
    from studio.timeline import Word

    template = load_template(nes_desk_dir)
    style = template.data["subtitles"]["style"]
    texts = "the administration said the investigation continues".split()
    words = [Word(t, i * 0.4, i * 0.4 + 0.35) for i, t in enumerate(texts)]
    cues = split_cues(words, style["max_lines"], style["max_chars_per_line"], style["font_scale"])
    assert [w for c in cues for line in c.lines for w in line.split()] == texts  # no word broken
    for cue in cues:
        scale, lines = subtitle_layout(template, cue.text)
        assert lines == list(cue.lines)  # the screen shows the SRT's lines
        assert scale == (1 if any(len(w) > style["max_chars_per_line"] for w in cue.text.split()) else 2)
    assert any(subtitle_layout(template, c.text)[0] == 1 for c in cues)


def test_the_preview_timeline_splits_cues_with_the_templates_caps(nes_desk_dir):
    """Otherwise the engine's 2-line cap would drop words from a 23-character cue."""
    template = load_template(nes_desk_dir)
    timeline = builtin_timeline(subtitle_style=template.data["subtitles"]["style"])
    for scene in timeline.scenes:
        shown = [w for cue in scene.subtitles for w in " ".join(subtitle_layout(template, cue.text)[1]).split()]
        assert shown == [w.text for w in scene.words]


def test_a_plate_with_blank_text_is_not_shown(nes_desk_dir):
    from studio.engine import active_plates

    script = json.loads(json.dumps(BUILTIN_SCRIPT))
    script["scenes"][0].update(on_screen_text=" ", source_plate="  ")
    timeline = builtin_timeline()
    s1 = timeline.scenes[0]
    assert not {"scene_title", "source_plate"} & active_plates(script, s1, load_template(nes_desk_dir), s1.start_s + 0.5)


# --- the AI line ---


def test_the_ai_line_holds_still_through_a_transition(night_report_dir):
    """night-report slides S2 in: the line is drawn after the slide, exactly in place."""
    from studio.engine import draw_ai_label

    template = load_template(night_report_dir)
    assets = load_channel_assets("8bitnews-us")
    timeline = builtin_timeline()
    s2 = next(s for s in timeline.scenes if s.scene_id == "S2")
    from studio.engine import scene_transition_duration
    assert scene_transition_duration(template, 1) > 0.1  # mid-slide
    frame = np.asarray(render_frame(BUILTIN_SCRIPT, timeline, template, assets, s2.start_s + 0.1))
    canvas = Image.new("RGBA", (270, 480), (0, 0, 0, 0))
    draw_ai_label(canvas, template, "AI voice · AI illustration", FONT)  # S1's screen and S2 both show a real person
    want = np.asarray(canvas)
    drawn = want[..., 3] == 255
    assert (frame[drawn] == want[drawn][:, :3]).all()


# --- the desk in front of the anchor ---


def test_the_desk_is_drawn_over_the_anchor(nes_desk_dir):
    template = load_template(nes_desk_dir)
    assets = load_channel_assets("8bitnews-us")
    timeline = builtin_timeline()
    s1 = next(s for s in timeline.scenes if s.scene_id == "S1")
    frame = np.asarray(render_frame(BUILTIN_SCRIPT, timeline, template, assets, (s1.start_s + s1.end_s) / 2))
    cfg = template.data["studio"]["anchor"]
    body = np.asarray(assets.anchor.bodies["normal"])
    desk = next(layer for layer in template.data["studio"]["layers"] if layer.get("front"))
    desk_px = np.asarray(template.load_asset_image(desk["file"]).convert("RGBA"))
    # sprite pixels behind the desk show the desk's color, not the anchor's
    ys, xs = np.nonzero((body[..., 3] > 0) & (desk_px[cfg["y"]:cfg["y"] + body.shape[0], cfg["x"]:cfg["x"] + body.shape[1], 3] == 255))
    assert len(ys)
    y, x = ys[0] + cfg["y"], xs[0] + cfg["x"]
    assert tuple(frame[y, x]) == tuple(desk_px[y, x, :3])


# --- what validate holds a template to ---


def test_validate_rejects_a_front_layer_over_a_face(nes_desk_dir, tmp_path):
    d = tmp_path / "t"
    shutil.copytree(nes_desk_dir, d)
    desk = Image.open(d / "assets" / "desk.png")
    up = Image.new("RGBA", desk.size, (0, 0, 0, 0))
    up.paste(desk.crop((0, 100, desk.width, desk.height)), (0, 0))  # the desk 100 px higher
    up.save(d / "assets" / "desk.png")
    with pytest.raises(TemplateValidationError) as e:
        validate_template(d)
    assert "drawn in front of the anchor and covers the face of anchor" in str(e.value)


def test_validate_rejects_a_plate_stack_that_reaches_a_face(nes_desk_dir, tmp_path):
    def edit(data):
        data["plates"]["source_plate"]["rect"]["y"] = 160
        data["plates"]["scene_title"]["rect"]["y"] = 160
    assert "plates.scene_title under plates.source_plate can reach" in _problems(nes_desk_dir, tmp_path, edit)


def test_validate_rejects_a_subtitle_rect_too_small_for_the_big_font(nes_desk_dir, tmp_path):
    assert "too small for 2 line(s) of 12 characters at font_scale 2" in _problems(
        nes_desk_dir, tmp_path, lambda d: d["subtitles"]["rect"].update(w=200))


def test_validate_rejects_an_ai_line_outside_the_safe_zone(nes_desk_dir, tmp_path):
    assert "plates.ai_label: the line" in _problems(nes_desk_dir, tmp_path,
                                                    lambda d: d["plates"]["ai_label"].update(y=420))


def test_the_last_anchor_shot_shows_the_logo_unstretched(nes_desk_dir):
    from studio.timeline import uniform_timeline

    template = load_template(nes_desk_dir)
    assets = load_channel_assets("8bitnews-us")
    script = json.loads(json.dumps(BUILTIN_SCRIPT))
    script["scenes"][-1].update(shot="anchor", real_people=[])  # an outro: no scene shot after it
    timeline = uniform_timeline(script)
    last = timeline.scenes[-1]
    frame = render_frame(script, timeline, template, assets, (last.start_s + last.end_s) / 2)
    screen = template.data["studio"]["screen"]
    top = template.data["studio"]["anchor"]["y"] - screen["y"]  # the anchor's head may overlap the screen's bottom
    content = np.asarray(frame.crop((screen["x"], screen["y"], screen["x"] + screen["w"], screen["y"] + screen["h"])))[:top]

    def card(k):  # the logo k times its size, centered on a black card
        c = Image.new("RGBA", (screen["w"], screen["h"]), (0, 0, 0, 255))
        big = assets.logo.resize((assets.logo.width * k, assets.logo.height * k), Image.NEAREST)
        c.alpha_composite(big, ((screen["w"] - big.width) // 2, (screen["h"] - big.height) // 2))
        return np.asarray(c.convert("RGB"))[:top]

    assert any((card(k) == content).all() for k in range(2, 9))


def test_validate_rejects_a_plate_under_itself(nes_desk_dir, tmp_path):
    assert "a plate can't sit under itself" in _problems(
        nes_desk_dir, tmp_path, lambda d: d["plates"]["source_plate"].update(below="scene_title"))


def test_validate_rejects_subtitles_over_the_ai_line(nes_desk_dir, tmp_path):
    assert "a subtitle would cover it" in _problems(nes_desk_dir, tmp_path,
                                                    lambda d: d["subtitles"]["rect"].update(y=340))
