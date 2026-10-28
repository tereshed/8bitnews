"""studio.scene_images without the network: prompt, check, redraws and the
neutral card (fixtures/images/with_text.png stands for a picture the check
finds text in)."""
import json
from pathlib import Path

import pytest
from PIL import Image

from conftest import FIXTURES_DIR
from studio.build import build_package
from studio.openrouter import ImageReply, JsonReply, NoImageError
from studio.paths import TEMPLATES_DIR
from studio.scene_images import FINDINGS, check_findings, generate_scene_images, load_style
from studio.template_pkg import load_template

CLEAN = (FIXTURES_DIR / "images" / "clean.png").read_bytes()
WITH_TEXT = (FIXTURES_DIR / "images" / "with_text.png").read_bytes()
PASS = {k: False for k in FINDINGS}


class Client:
    """Draws `pictures` in turn (bytes or an exception); the check finds text
    exactly in with_text.png."""

    def __init__(self, pictures):
        self.pictures = list(pictures)
        self.prompts, self.aspects, self.checked = [], [], 0

    def generate_image(self, model, prompt, aspect_ratio, images=(), variant=0):
        self.prompts.append(prompt)
        self.aspects.append(aspect_ratio)
        p = self.pictures.pop(0) if self.pictures else CLEAN
        if isinstance(p, Exception):
            raise p
        return ImageReply(p, 0.067, False, model)

    def ask_json(self, model, prompt, images=()):
        self.checked += 1
        return JsonReply(dict(PASS, text=images[0] == WITH_TEXT), 0.001, False, model)


def _script(**scene_overrides):
    script = json.loads((FIXTURES_DIR / "scripts" / "short.json").read_text())
    for s in script["scenes"]:
        if s["shot"] == "scene":
            s.update(scene_overrides)
    return script


@pytest.fixture
def template():
    return load_template(TEMPLATES_DIR / "nes-desk")


def _scene_ids(script):
    return [s["id"] for s in script["scenes"] if s["shot"] == "scene"]


def test_a_passing_picture_becomes_the_scene_asset(tmp_path, template):
    script = _script()
    images_dir, report = generate_scene_images(script, template, "8bitnews-us", tmp_path, Client([]))
    for sid in _scene_ids(script):
        assert (images_dir / f"{sid}.png").read_bytes() == CLEAN
    assert not report.notes and not any(s["fallback"] for s in report.scenes)
    assert report.cost_usd == pytest.approx(0.068 * len(_scene_ids(script)))


def test_the_prompt_carries_style_template_look_and_prohibitions(tmp_path, template):
    script = _script(visual="Secretary Weber holds a sign marked 'VOTE' under a question mark",
                     real_people=["Shirley Weber"])
    client = Client([])
    generate_scene_images(script, template, "8bitnews-us", tmp_path, client)
    prompt = client.prompts[0]
    style = load_style("8bitnews-us")
    for part in (style["style"], style["people"], style["prohibitions"], template.data["image_style"]):
        assert part in prompt
    assert "Weber" not in prompt and "VOTE" not in prompt and "question" not in prompt.split("Scene:")[1].split(".")[0]


def test_text_in_the_picture_means_a_redraw_with_the_prohibition_stated_harder(tmp_path, template):
    script = _script()
    first = _scene_ids(script)[0]
    client = Client([WITH_TEXT, CLEAN])
    images_dir, report = generate_scene_images(script, template, "8bitnews-us", tmp_path, client)
    assert FINDINGS["text"] not in client.prompts[0] and FINDINGS["text"] in client.prompts[1]
    scene = next(s for s in report.scenes if s["id"] == first)
    assert [a["found"] for a in scene["attempts"]] == [["text"], []]
    assert (images_dir / f"{first}.png").read_bytes() == CLEAN
    assert (tmp_path / scene["attempts"][0]["file"]).read_bytes() == WITH_TEXT  # the rejected one is kept


def test_no_passing_picture_leaves_a_neutral_card_and_a_note(tmp_path, template):
    script = _script()
    first = _scene_ids(script)[0]
    client = Client([WITH_TEXT, NoImageError("refused"), WITH_TEXT])
    images_dir, report = generate_scene_images(script, template, "8bitnews-us", tmp_path, client)
    scene = next(s for s in report.scenes if s["id"] == first)
    assert scene["fallback"] and scene["accepted"] is None and len(scene["attempts"]) == 3
    card = Image.open(images_dir / f"{first}.png")
    assert card.size == (270, 480)
    assert any(n.startswith(f"{first}: no generated picture passed") and "text" in n for n in report.notes)


def test_an_unreadable_check_is_not_a_pass():
    assert check_findings(PASS) == []
    assert check_findings({}) == list(FINDINGS)
    assert check_findings(dict(PASS, gore="no")) == ["gore"]


def _skin_picture() -> bytes:
    """A usable picture in a color outside nes-desk's own 13 (#f0d0b0, skin)."""
    import io

    img = Image.new("RGB", (288, 512), (240, 208, 176))
    buf = io.BytesIO()
    img.save(buf, "PNG")
    return buf.getvalue()


@pytest.fixture(scope="module")
def generated_build(tmp_path_factory):
    """short.json with a custom cover, built with drawn pictures: the first
    scene never passes the check, the rest and the cover are skin-colored."""
    tmp_path = tmp_path_factory.mktemp("generated")
    script = json.loads((FIXTURES_DIR / "scripts" / "short.json").read_text())
    script["cover"] = {"frame": "custom", "visual": "A county office with a ballot box on the desk", "text": script["cover"]["text"]}
    script_path = tmp_path / "script.json"
    script_path.write_text(json.dumps(script))
    client = Client([WITH_TEXT, WITH_TEXT, WITH_TEXT] + [_skin_picture()] * 10)
    package_path = build_package(
        script_path=script_path, channel_id="8bitnews-us", out_dir=tmp_path / "out",
        channels_dir=FIXTURES_DIR / "channels", tts_name="stub", images="openrouter", image_client=client,
    )
    return {"out_dir": tmp_path / "out", "package": json.loads(package_path.read_text()), "script": script}


def test_build_with_generated_pictures_records_them(generated_build):
    package = generated_build["package"]
    assert package["images"]["provider"] == "openrouter" and package["images"]["cost_usd"] > 0
    assert [s["id"] for s in package["images"]["scenes"]] == ["S2", "S3", "cover"]
    assert any(n.startswith("S2: no generated picture passed") for n in package["notes"])
    assert package["validate"]["passed"], [c for c in package["validate"]["checks"] if not c["ok"]]
    for s in package["images"]["scenes"]:
        assert (generated_build["out_dir"] / s["file"]).is_file()


def test_the_drawn_picture_is_what_the_video_shows_in_the_full_palette(generated_build):
    """S3's keyframes carry the picture's skin color, which nes-desk's own 13
    colors lack: the picture is used, and snapped to the whole NES palette."""
    out_dir = generated_build["out_dir"]
    colors = {"#%02x%02x%02x" % c for _n, c in Image.open(out_dir / "keyframes" / "S3_mid.png").convert("RGB").getcolors(1 << 20)}
    template = load_template(TEMPLATES_DIR / "nes-desk")
    assert "#f0d0b0" in colors and "#f0d0b0" not in template.palette().hex_colors


def test_a_drawn_cover_carries_the_ai_illustration_plate(generated_build, corrupt_copy, test_channels_dir):
    from studio.checks import run_checks

    check = next(c for c in generated_build["package"]["validate"]["checks"] if c["id"] == "ai_plates")
    assert check["ok"]

    def cover_without_plate(copy_dir, package):
        template = load_template(copy_dir / "template")
        r = template.plate("ai_illustration")["rect"]
        img = Image.open(copy_dir / "cover.png").convert("RGB")
        img.paste((240, 208, 176), (r["x"] * 4, r["y"] * 4, (r["x"] + r["w"]) * 4, (r["y"] + r["h"]) * 4))
        img.save(copy_dir / "cover.png")

    import yaml

    copy = corrupt_copy({"out_dir": generated_build["out_dir"], "package": generated_build["package"]}, cover_without_plate)
    package = json.loads((copy / "package.json").read_text())
    profile = yaml.safe_load((test_channels_dir / "8bitnews-us" / "channel.yaml").read_text())
    result = next(c for c in run_checks(copy, package, profile)["checks"] if c["id"] == "ai_plates")
    assert not result["ok"] and "a cover drawn by the image model" in result["note"]


def test_every_scene_is_cleaned_of_everyone_in_the_script(tmp_path, template):
    script = _script()
    scenes = [s for s in script["scenes"] if s["shot"] == "scene"]
    scenes[0]["real_people"] = ["Shirley Weber"]
    scenes[1]["real_people"] = []
    scenes[1]["visual"] = "A letter from Weber's office lands on a clerk's desk"
    client = Client([])
    generate_scene_images(script, template, "8bitnews-us", tmp_path, client)
    assert all("Weber" not in p for p in client.prompts)
    assert any("a person's office" in p for p in client.prompts)


def test_a_prompt_that_still_names_someone_is_never_sent(tmp_path, template, monkeypatch):
    import studio.scene_images as si
    from studio.errors import BuildError

    monkeypatch.setattr(si, "clean_visual", lambda visual, people: visual)
    script = _script(visual="Secretary Weber signs a letter", real_people=["Shirley Weber"])
    client = Client([])
    with pytest.raises(BuildError, match="still names Weber"):
        generate_scene_images(script, template, "8bitnews-us", tmp_path, client)
    assert client.prompts == []


def test_refusals_and_unusable_pictures_are_billed_and_drawn_again(tmp_path, template):
    import io

    tiny = io.BytesIO()
    Image.new("RGB", (64, 114), (0, 0, 0)).save(tiny, "PNG")
    script = _script()
    first = _scene_ids(script)[0]
    client = Client([NoImageError("refused", cost_usd=0.002), tiny.getvalue(), CLEAN])
    _, report = generate_scene_images(script, template, "8bitnews-us", tmp_path, client)
    scene = next(s for s in report.scenes if s["id"] == first)
    assert [("refused" in a, "unusable" in a, a.get("found")) for a in scene["attempts"]] == [
        (True, False, None), (False, True, None), (False, False, [])]
    assert scene["attempts"][0]["cost_usd"] == 0.002 and not scene["fallback"]
    assert report.cost_usd == pytest.approx(0.002 + 0.067 + 0.068 + 0.068 * (len(_scene_ids(script)) - 1))


def test_serious_mode_asks_for_the_serious_look(tmp_path, template):
    script = _script()
    script["serious_mode"] = True
    client = Client([])
    generate_scene_images(script, template, "8bitnews-us", tmp_path, client)
    assert load_style("8bitnews-us")["serious"] in client.prompts[0]
    assert template.data["image_style"] not in client.prompts[0]


def test_images_and_assets_together_are_refused(tmp_path):
    from studio.errors import BuildError
    with pytest.raises(BuildError, match="pass one"):
        build_package(script_path=FIXTURES_DIR / "scripts" / "short.json", channel_id="8bitnews-us", out_dir=tmp_path,
                      channels_dir=FIXTURES_DIR / "channels", tts_name="stub", images="openrouter",
                      assets_dir=tmp_path, image_client=Client([]))


# --- what scene pictures snap to (engine.scene_palette) ------------------------

def _scene_frame(tmp_path, serious: bool, with_picture: bool) -> set[str]:
    from studio.builtin_script import BUILTIN_SCRIPT, builtin_timeline
    from studio.channel_assets import load_channel_assets
    from studio.engine import render_frame

    assets_dir = tmp_path / "assets"
    assets_dir.mkdir(exist_ok=True)
    if with_picture:
        (assets_dir / "S3.png").write_bytes(_skin_picture())
    script = dict(BUILTIN_SCRIPT, serious_mode=serious)
    timeline = builtin_timeline()
    s3 = next(s for s in timeline.scenes if s.scene_id == "S3")
    frame = render_frame(script, timeline, load_template(TEMPLATES_DIR / "nes-desk"), load_channel_assets("8bitnews-us"),
                         (s3.start_s + s3.end_s) / 2, assets_dir)
    return {"#%02x%02x%02x" % c for _n, c in frame.getcolors(1 << 20)}


def test_a_picture_keeps_skin_tones_in_normal_mode(tmp_path):
    assert "#f0d0b0" in _scene_frame(tmp_path, serious=False, with_picture=True)


def test_serious_mode_mutes_the_picture(tmp_path):
    template = load_template(TEMPLATES_DIR / "nes-desk")
    colors = _scene_frame(tmp_path, serious=True, with_picture=True)
    drawn_by_template = {c.lower() for p in template.data["plates"].values() for c in (p["bg_color"], p["text_color"])}
    drawn_by_template |= {template.data["subtitles"]["style"][k].lower() for k in ("color", "outline_color")}
    assert "#f0d0b0" not in colors and colors <= set(template.palette(serious=True).hex_colors) | drawn_by_template


def test_the_placeholder_card_keeps_the_template_colors(tmp_path):
    template = load_template(TEMPLATES_DIR / "nes-desk")
    assert _scene_frame(tmp_path, serious=False, with_picture=False) <= set(template.palette().hex_colors)


def test_scene_colors_narrow_what_pictures_snap_to(tmp_path, monkeypatch):
    from studio.engine import scene_palette

    template = load_template(TEMPLATES_DIR / "nes-desk")
    assert len(scene_palette(template, False, "8bitnews-us")) == 55
    template.data["palette"]["scene_colors"] = ["#000000", "#fcfcfc", "#f0d0b0"]
    assert scene_palette(template, False, "8bitnews-us").hex_colors == ["#000000", "#fcfcfc", "#f0d0b0"]
    assert scene_palette(template, True, "8bitnews-us").hex_colors == template.palette(serious=True).hex_colors


def test_an_anchor_scene_gets_its_own_screen_picture(tmp_path, template):
    script = _script()
    first = script["scenes"][0]
    assert first["shot"] == "anchor"
    first["visual"] = "The anchor at the studio desk; behind them, the screen shows a courthouse icon over a ledger."
    last = script["scenes"][-1]
    assert last["shot"] == "anchor"
    last["visual"] = "The anchor at the studio desk; the screen behind the anchor shows a stack of ballots."
    client = Client([])
    images_dir, report = generate_scene_images(script, template, "8bitnews-us", tmp_path, client)
    assert (images_dir / f"{first['id']}.png").is_file()
    screens = [(p, a) for p, a in zip(client.prompts, client.aspects) if "TV screen behind a news anchor" in p]
    assert len(screens) == 1  # not the last anchor shot's: the outro shows the logo
    screen_prompt, aspect = screens[0]
    assert "courthouse icon over a ledger" in screen_prompt and "studio desk" not in screen_prompt
    assert aspect == "4:5"  # nes-desk's screen is 80x100
    assert not (images_dir / f"{last['id']}.png").exists()


def test_a_screen_picture_that_fails_the_check_becomes_a_screen_sized_card(tmp_path, template):
    script = _script()
    first = script["scenes"][0]
    first["visual"] = "The anchor at the studio desk; the screen behind the anchor shows a courthouse icon."
    client = Client([WITH_TEXT] * 3)  # the screen is drawn first, three times
    images_dir, report = generate_scene_images(script, template, "8bitnews-us", tmp_path, client)
    screen = template.data["studio"]["screen"]
    assert Image.open(images_dir / f"{first['id']}.png").size == (screen["w"], screen["h"])
    assert any(n.startswith(f"{first['id']} screen behind the anchor: no generated picture") for n in report.notes)


def test_the_screen_shows_the_anchor_scenes_own_picture(tmp_path):
    from studio.builtin_script import BUILTIN_SCRIPT, builtin_timeline
    from studio.channel_assets import load_channel_assets
    from studio.engine import render_frame

    template = load_template(TEMPLATES_DIR / "nes-desk")
    timeline = builtin_timeline()
    s1 = next(s for s in timeline.scenes if s.scene_id == "S1")
    screen = template.data["studio"]["screen"]
    box = (screen["x"] + 4, screen["y"] + 4, screen["x"] + screen["w"] - 4, screen["y"] + screen["h"] - 4)

    def screen_colors(with_own):
        assets = tmp_path / ("own" if with_own else "next")
        assets.mkdir()
        if with_own:
            (assets / "S1.png").write_bytes(_skin_picture())
        frame = render_frame(BUILTIN_SCRIPT, timeline, template, load_channel_assets("8bitnews-us"),
                             (s1.start_s + s1.end_s) / 2, assets)
        counts = {"#%02x%02x%02x" % c: n for n, c in frame.crop(box).getcolors(1 << 20)}
        return counts.get("#f0d0b0", 0) / sum(counts.values())  # the anchor's shoulder may reach into the screen

    assert screen_colors(True) > 0.8
    assert screen_colors(False) == 0
