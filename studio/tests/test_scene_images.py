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
from studio.scene_images import (
    FINDINGS,
    MAX_REDRAWS,
    EarlierPictures,
    check_findings,
    generate_scene_images,
    load_earlier_pictures,
    load_style,
)
from studio.template_pkg import load_template

CLEAN = (FIXTURES_DIR / "images" / "clean.png").read_bytes()
WITH_TEXT = (FIXTURES_DIR / "images" / "with_text.png").read_bytes()
WITH_PEOPLE = CLEAN + b"\0"  # the same picture; the fake check finds people the scene doesn't ask for in it
WITH_BOTH = CLEAN + b"\0\0"  # ... and text too
PASS = {k: False for k in FINDINGS}


class Client:
    """Draws `pictures` in turn (bytes or an exception); the check finds text
    exactly in with_text.png."""

    def __init__(self, pictures):
        self.pictures = list(pictures)
        self.prompts, self.aspects, self.check_prompts, self.checked = [], [], [], 0

    def generate_image(self, model, prompt, aspect_ratio, images=(), variant=0):
        self.prompts.append(prompt)
        self.aspects.append(aspect_ratio)
        p = self.pictures.pop(0) if self.pictures else CLEAN
        if isinstance(p, Exception):
            raise p
        return ImageReply(p, 0.067, False, model)

    def ask_json(self, model, prompt, images=()):
        self.checked += 1
        self.check_prompts.append(prompt)
        return JsonReply(dict(PASS, text=images[0] in (WITH_TEXT, WITH_BOTH),
                              extra_people=images[0] in (WITH_PEOPLE, WITH_BOTH)), 0.001, False, model)


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
    for part in (style["style"].replace("{frame}", "vertical 9:16"), style["people"], style["prohibitions"],
                 template.data["image_style"]):
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


def _skin_picture(color=(240, 208, 176)) -> bytes:
    """A usable picture in a color outside nes-desk's own 13 (#f0d0b0, skin)."""
    import io

    img = Image.new("RGB", (288, 512), color)
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
    for s in package["images"]["scenes"]:
        assert (generated_build["out_dir"] / s["file"]).is_file()


def test_the_drawn_picture_is_what_the_video_shows_in_its_own_colors(generated_build):
    """S3's keyframes carry the picture's skin color, which nes-desk's own 13
    colors lack: the picture is used, in its own colors."""
    out_dir = generated_build["out_dir"]
    colors = {"#%02x%02x%02x" % c for _n, c in Image.open(out_dir / "keyframes" / "S3_mid.png").convert("RGB").getcolors(1 << 20)}
    template = load_template(TEMPLATES_DIR / "nes-desk")
    assert "#f0d0b0" in colors and "#f0d0b0" not in template.palette().hex_colors


def test_a_drawn_cover_carries_the_cover_ai_line(generated_build, corrupt_copy, test_channels_dir):
    from studio.checks import run_checks

    assert generated_build["package"]["cover"] == {"frame": "custom", "layout": "bottom", "picture": "custom"}
    check = next(c for c in generated_build["package"]["validate"]["checks"] if c["id"] == "ai_label")
    assert check["ok"]

    def cover_without_plate(copy_dir, package):
        y = load_template(copy_dir / "template").plate("ai_label")["y"]
        img = Image.open(copy_dir / "cover.png").convert("RGB")
        img.paste((240, 208, 176), (0, (y - 1) * 4, 270 * 4, (y + 9) * 4))
        img.save(copy_dir / "cover.png")

    import yaml

    copy = corrupt_copy({"out_dir": generated_build["out_dir"], "package": generated_build["package"]}, cover_without_plate)
    package = json.loads((copy / "package.json").read_text())
    profile = yaml.safe_load((test_channels_dir / "8bitnews-us" / "channel.yaml").read_text())
    result = next(c for c in run_checks(copy, package, profile)["checks"] if c["id"] == "ai_label")
    assert not result["ok"] and "cover.png (the cover): no 'AI illustration · AI text' line" in result["note"]


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

def _scene_frame(tmp_path, serious: bool, with_picture: bool, color=(240, 208, 176)) -> set[str]:
    from studio.builtin_script import BUILTIN_SCRIPT, builtin_timeline
    from studio.channel_assets import load_channel_assets
    from studio.engine import render_frame

    assets_dir = tmp_path / "assets"
    assets_dir.mkdir(exist_ok=True)
    if with_picture:
        (assets_dir / "S3.png").write_bytes(_skin_picture(color))
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
    keys = ("bg_color", "text_color", "outline_color", "color")
    drawn_by_template = {p[k].lower() for p in template.data["plates"].values() for k in keys if k in p}
    drawn_by_template |= {v.lower() for k, v in template.data["subtitles"]["style"].items() if k in keys}
    assert "#f0d0b0" not in colors and colors <= set(template.palette(serious=True).hex_colors) | drawn_by_template


def test_the_placeholder_card_keeps_the_template_colors(tmp_path):
    template = load_template(TEMPLATES_DIR / "nes-desk")
    assert _scene_frame(tmp_path, serious=False, with_picture=False) <= set(template.palette().hex_colors)


def test_a_picture_keeps_its_own_colors_in_normal_mode(tmp_path):
    """Owner, 29.10: the NES palette turned pictures brown and grey."""
    from studio.palette import channel_palette_set

    assert "#7b2d43" in _scene_frame(tmp_path, serious=False, with_picture=True, color=(0x7b, 0x2d, 0x43))
    assert "#7b2d43" not in channel_palette_set("8bitnews-us")


def test_scene_colors_narrow_what_pictures_snap_to(tmp_path, monkeypatch):
    from studio.engine import scene_palette

    template = load_template(TEMPLATES_DIR / "nes-desk")
    assert scene_palette(template, False, "8bitnews-us") is None  # the picture's own colors
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
    assert aspect == "5:4"  # nes-desk's screen is 170x136
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


def test_extra_people_are_redrawn_and_then_kept_not_carded(tmp_path, template):
    script = _script(visual="A conveyor belt of envelopes beside a ballot box.", real_people=[])
    first = _scene_ids(script)[0]
    client = Client([WITH_PEOPLE] * 3)  # the first scene shot is drawn first (short.json's S1 has no screen text)
    images_dir, report = generate_scene_images(script, template, "8bitnews-us", tmp_path, client)
    scene = next(s for s in report.scenes if s["id"] == first)
    assert len(scene["attempts"]) == 3 and not scene["fallback"] and scene["accepted"] == f"images/attempts/{first}-0.png"
    assert scene["kept_with"] == ["extra_people"]
    assert "no figures, faces or hands" in client.prompts[1]  # the redraw says it harder
    assert client.check_prompts[0].endswith("The scene description: A conveyor belt of envelopes beside a ballot box.")


@pytest.mark.parametrize("pictures,kept", [
    ([WITH_TEXT, WITH_PEOPLE, WITH_TEXT], 1),  # after a hard failure
    ([NoImageError("refused", 0.01), WITH_TEXT, WITH_PEOPLE], 2),  # a refusal has no findings to go by
])
def test_the_kept_picture_is_the_first_with_only_extra_people(tmp_path, template, pictures, kept):
    script = _script(real_people=[])
    first = _scene_ids(script)[0]
    images_dir, report = generate_scene_images(script, template, "8bitnews-us", tmp_path, Client(pictures))
    scene = next(s for s in report.scenes if s["id"] == first)
    assert scene["accepted"] == f"images/attempts/{first}-{kept}.png" and not scene["fallback"]
    assert (images_dir / f"{first}.png").read_bytes() == WITH_PEOPLE
    note = next(n for n in report.notes if n.startswith(f"{first}:"))
    assert "no generated picture passed the check after 3 attempt(s)" in note and "text" in note
    assert f"images/attempts/{first}-{kept}.png is kept: its only finding is people" in note


def test_extra_people_with_text_is_still_a_card(tmp_path, template):
    script = _script(real_people=[])
    first = _scene_ids(script)[0]
    images_dir, report = generate_scene_images(script, template, "8bitnews-us", tmp_path, Client([WITH_BOTH] * 3))
    scene = next(s for s in report.scenes if s["id"] == first)
    assert scene["fallback"] and "kept_with" not in scene
    assert (images_dir / f"{first}.png").read_bytes() != WITH_BOTH


def test_the_fixed_wording_never_counts_as_a_name(tmp_path, template):
    """"power-ups" in the prohibitions, "hands" in the extra-people redraw: not Samantha Power or Guy Hands."""
    script = _script(visual="A diplomat speaks at a podium.", real_people=["Samantha Power", "Guy Hands"])
    generate_scene_images(script, template, "8bitnews-us", tmp_path, Client([WITH_PEOPLE] * 3))


def test_the_style_names_the_pictures_frame(template):
    style = load_style("8bitnews-us")
    from studio.scene_images import scene_prompt
    assert "vertical 9:16" in scene_prompt(style, template, "a ballot box")
    screen = scene_prompt(style, template, "a ballot box", screen=True, aspect="4:5")
    assert "portrait 4:5" in screen and "9:16" not in screen and "{frame}" not in screen
    assert "Scene: a ballot box. " in screen
    assert "Scene: Who will win? " in scene_prompt(style, template, "Who will win?")


def test_scene_and_screen_pictures_leave_room_for_the_plates(template):
    """The plate stack covers the top of a scene shot and, with a source, the top of the screen behind the anchor."""
    from studio.scene_images import scene_prompt

    style = load_style("8bitnews-us")
    assert style["top_room"] in scene_prompt(style, template, "a ballot box")
    assert style["top_room"] in scene_prompt(style, template, "a ballot box", screen=True, aspect="5:4")


def test_a_neutral_card_fails_the_package_and_nothing_else_does(generated_build):
    """S2 never passes in this build: QA would only fail it for $0.78, so it stops at the studio's checks."""
    failed = [c for c in generated_build["package"]["validate"]["checks"] if not c["ok"]]
    assert [c["id"] for c in failed] == ["picture_fallback"]
    assert "S2 (3 attempt(s): text)" in failed[0]["note"]


# --- a rebuild takes the accepted pictures of the earlier package -----------------

class NoCalls(Client):
    """A rebuild that reuses everything must not reach the provider, not even for the check."""

    def __init__(self):
        super().__init__([])

    def generate_image(self, *args, **kwargs):
        raise AssertionError("a paid call to draw a picture")

    def ask_json(self, *args, **kwargs):
        raise AssertionError("a paid call to check a picture")


class Recording(Client):
    def __init__(self, pictures=()):
        super().__init__(pictures)
        self.variants = []

    def generate_image(self, model, prompt, aspect_ratio, images=(), variant=0):
        self.variants.append(variant)
        return super().generate_image(model, prompt, aspect_ratio, images, variant)


def _picture(color) -> bytes:
    """A usable picture of its own color: tells which build drew it."""
    return _skin_picture(color)


def _earlier(tmp_path, template, script, pictures=(), package_id="pkg-1", name="earlier") -> EarlierPictures:
    """An earlier build as `studio build` leaves it: images/ and package.json with its images report."""
    out = tmp_path / name
    out.mkdir()
    _, report = generate_scene_images(script, template, "8bitnews-us", out, Client(pictures))
    (out / "package.json").write_text(json.dumps({"images": report.to_package()}))
    return EarlierPictures(out, package_id, report.to_package()["scenes"])


def _rebuild(tmp_path, template, script, earlier, client=None, redraw=(), name="rebuilt"):
    out = tmp_path / name
    out.mkdir()
    images_dir, report = generate_scene_images(script, template, "8bitnews-us", out, client or NoCalls(), earlier=earlier,
                                               redraw=redraw)
    return images_dir, report, {s["id"]: s for s in report.scenes}


def _two_scene_pictures(script):
    ids = _scene_ids(script)
    return ids, {sid: _picture((10 * (i + 1), 80, 120)) for i, sid in enumerate(ids)}


def test_a_rebuild_with_nothing_changed_reuses_every_picture_and_pays_nothing(tmp_path, template):
    script = _script()
    ids, pictures = _two_scene_pictures(script)
    earlier = _earlier(tmp_path, template, script, [pictures[sid] for sid in ids])
    images_dir, report, scenes = _rebuild(tmp_path, template, script, earlier)
    assert report.cost_usd == 0 and report.to_package()["cost_usd"] == 0
    for sid in ids:
        assert (images_dir / f"{sid}.png").read_bytes() == pictures[sid]
        assert scenes[sid]["reused_from"]["package"] == "pkg-1" and scenes[sid]["reused_from"]["scene"] == sid
        assert scenes[sid]["attempts"] == [] and scenes[sid]["fallback"] is False and "redraw_requested" not in scenes[sid]
    assert f"{len(ids)} of {len(ids)} picture(s) reused from pkg-1" in report.notes[-1] and "drawn again: none" in report.notes[-1]


def test_only_the_scene_named_in_redraw_is_drawn_again(tmp_path, template):
    script = _script()
    ids, pictures = _two_scene_pictures(script)
    earlier = _earlier(tmp_path, template, script, [pictures[sid] for sid in ids])
    new = _picture((200, 10, 10))
    client = Recording([new])
    images_dir, report, scenes = _rebuild(tmp_path, template, script, earlier, client, redraw={ids[0]})
    assert len(client.prompts) == 1 and client.checked == 1
    assert (images_dir / f"{ids[0]}.png").read_bytes() == new
    assert scenes[ids[0]]["redraw_requested"] is True and "reused_from" not in scenes[ids[0]]
    assert report.cost_usd == pytest.approx(0.068)
    for sid in ids[1:]:
        assert (images_dir / f"{sid}.png").read_bytes() == pictures[sid] and "reused_from" in scenes[sid]
    assert f"{ids[0]} (named in --redraw)" in report.notes[-1]


def test_a_redraw_never_gets_the_rejected_picture_back_from_the_providers_cache(tmp_path, template):
    """The cache is keyed by request and variant: the same prompt at variant 0 is the same picture."""
    script = _script()
    ids, pictures = _two_scene_pictures(script)
    earlier = _earlier(tmp_path, template, script, [WITH_TEXT, _picture((1, 2, 3))] + [pictures[sid] for sid in ids[1:]])
    client = Recording([])
    _, _, scenes = _rebuild(tmp_path, template, script, earlier, client, redraw={ids[0]})
    assert client.variants == [2]  # its earlier attempts used 0 and 1
    assert scenes[ids[0]]["attempts"][0]["variant"] == 2


def test_a_changed_visual_is_drawn_again_and_the_rest_stays(tmp_path, template):
    script = _script()
    ids, pictures = _two_scene_pictures(script)
    earlier = _earlier(tmp_path, template, script, [pictures[sid] for sid in ids])
    changed = json.loads(json.dumps(script))
    next(s for s in changed["scenes"] if s["id"] == ids[0])["visual"] = "An empty stadium at night, rows of plain seats."
    client = Recording([])
    _, report, scenes = _rebuild(tmp_path, template, changed, earlier, client)
    assert client.variants == [0] and "empty stadium" in client.prompts[0]
    assert "reused_from" not in scenes[ids[0]] and "redraw_requested" not in scenes[ids[0]]
    assert all("reused_from" in scenes[sid] for sid in ids[1:])
    assert f"{ids[0]} (its prompt changed)" in report.notes[-1]


def test_scene_ids_can_shift_the_picture_follows_its_content(tmp_path, template):
    script = _script()
    ids, pictures = _two_scene_pictures(script)
    earlier = _earlier(tmp_path, template, script, [pictures[sid] for sid in ids])
    shifted = json.loads(json.dumps(script))
    for s in reversed(shifted["scenes"]):
        s["id"] = f"S{int(s['id'][1:]) + 1}"
    shifted["cover"]["frame"] = f"S{int(shifted['cover']['frame'][1:]) + 1}"
    images_dir, _, scenes = _rebuild(tmp_path, template, shifted, earlier)
    for sid in ids:
        new_id = f"S{int(sid[1:]) + 1}"
        assert scenes[new_id]["reused_from"]["scene"] == sid
        assert (images_dir / f"{new_id}.png").read_bytes() == pictures[sid]


def test_redraw_names_the_earlier_scene_even_when_ids_shifted(tmp_path, template):
    """QA's finding is about the package it looked at: its S3 may be S4 by now."""
    script = _script()
    ids, pictures = _two_scene_pictures(script)
    earlier = _earlier(tmp_path, template, script, [pictures[sid] for sid in ids])
    shifted = json.loads(json.dumps(script))
    for s in reversed(shifted["scenes"]):
        s["id"] = f"S{int(s['id'][1:]) + 1}"
    shifted["cover"]["frame"] = f"S{int(shifted['cover']['frame'][1:]) + 1}"
    last = ids[-1]
    _, _, scenes = _rebuild(tmp_path, template, shifted, earlier, Recording([]), redraw={last})
    moved = f"S{int(last[1:]) + 1}"
    assert "reused_from" not in scenes[moved] and scenes[moved]["redraw_requested"] is True


def test_a_neutral_card_is_never_reused_and_draws_past_what_was_tried(tmp_path, template):
    script = _script()
    ids, pictures = _two_scene_pictures(script)
    earlier = _earlier(tmp_path, template, script, [WITH_TEXT] * (1 + MAX_REDRAWS) + [pictures[sid] for sid in ids[1:]])
    assert next(s for s in earlier.scenes if s["id"] == ids[0])["fallback"] is True
    client = Recording([])
    _, report, scenes = _rebuild(tmp_path, template, script, earlier, client)
    assert client.variants == [1 + MAX_REDRAWS]
    assert "reused_from" not in scenes[ids[0]] and not scenes[ids[0]]["fallback"]
    assert f"{ids[0]} (it was a neutral card)" in report.notes[-1]


def test_someone_new_in_the_script_changes_the_prompt_of_a_scene_that_names_them(tmp_path, template):
    """Every scene is cleaned of everyone in the script, so a scene's prompt also hangs on the others'."""
    script = _script()
    ids = _scene_ids(script)
    next(s for s in script["scenes"] if s["id"] == ids[0])["visual"] = "A letter from Weber's office lands on a desk."
    earlier = _earlier(tmp_path, template, script)
    changed = json.loads(json.dumps(script))
    next(s for s in changed["scenes"] if s["id"] == ids[1])["real_people"] = ["Shirley Weber"]
    client = Recording([])
    _, _, scenes = _rebuild(tmp_path, template, changed, earlier, client)
    assert "reused_from" not in scenes[ids[0]] and "Weber" not in client.prompts[0]
    assert "reused_from" in scenes[ids[1]]


def test_serious_mode_changes_every_prompt(tmp_path, template):
    script = _script()
    earlier = _earlier(tmp_path, template, script)
    client = Recording([])
    _, report, scenes = _rebuild(tmp_path, template, dict(script, serious_mode=True), earlier, client)
    assert len(client.prompts) == len(_scene_ids(script)) and not any("reused_from" in s for s in scenes.values())


def test_another_templates_look_changes_the_prompt(tmp_path, template):
    script = _script()
    earlier = _earlier(tmp_path, template, script)
    night = load_template(TEMPLATES_DIR / "night-report")
    client = Recording([])
    _, _, scenes = _rebuild(tmp_path, night, script, earlier, client)
    assert len(client.prompts) == len(_scene_ids(script)) and not any("reused_from" in s for s in scenes.values())


def test_a_picture_kept_with_extra_people_stays_kept_and_says_so(tmp_path, template):
    script = _script(real_people=[])
    first = _scene_ids(script)[0]
    earlier = _earlier(tmp_path, template, script, [WITH_PEOPLE] * 3)
    images_dir, report, scenes = _rebuild(tmp_path, template, script, earlier)
    assert scenes[first]["kept_with"] == ["extra_people"] and (images_dir / f"{first}.png").read_bytes() == WITH_PEOPLE
    assert any(n.startswith(f"{first}: the reused picture was kept") for n in report.notes)
    _, _, scenes = _rebuild(tmp_path, template, script, earlier, Recording([]), redraw={first}, name="redrawn")
    assert "reused_from" not in scenes[first]


@pytest.mark.parametrize("damage,why", [
    (lambda d, s: (d / s["file"]).write_bytes(b"not a png"), "not the accepted attempt"),
    (lambda d, s: ((d / s["file"]).write_bytes(b"not a png"), (d / s["accepted"]).write_bytes(b"not a png")), "unusable"),
    (lambda d, s: (d / s["file"]).unlink(), "missing"),
    (lambda d, s: s.update(file="../../outside.png"), "outside the package"),
    (lambda d, s: s.update(fallback=None), "neutral card"),
    (lambda d, s: s.update(accepted=None), "no accepted picture"),
    (lambda d, s: s["attempts"][0].update(file="images/attempts/other.png"), "not on record"),
    (lambda d, s: s["attempts"][0].update(check={}), "its check found"),
    (lambda d, s: s["attempts"][0]["check"].update(text=True), "its check found text"),
    (lambda d, s: s["attempts"][0]["check"].update(extra_people=True), "its check found extra_people"),
])
def test_a_picture_in_doubt_is_drawn_again(tmp_path, template, damage, why):
    script = _script()
    ids, pictures = _two_scene_pictures(script)
    earlier = _earlier(tmp_path, template, script, [pictures[sid] for sid in ids])
    (tmp_path / "outside.png").write_bytes(pictures[ids[0]])
    damage(earlier.dir, next(s for s in earlier.scenes if s["id"] == ids[0]))
    client = Recording([])
    _, report, scenes = _rebuild(tmp_path, template, script, earlier, client)
    assert len(client.prompts) == 1 and "reused_from" not in scenes[ids[0]]
    assert all("reused_from" in scenes[sid] for sid in ids[1:])
    assert f"{ids[0]} (" in report.notes[-1] and why in report.notes[-1]


def test_a_rebuild_of_a_rebuild_reuses_and_redraws_past_all_of_them(tmp_path, template):
    script = _script()
    ids, pictures = _two_scene_pictures(script)
    first = _earlier(tmp_path, template, script, [pictures[sid] for sid in ids])
    _, report, _ = _rebuild(tmp_path, template, script, first)
    second_dir = tmp_path / "rebuilt"
    (second_dir / "package.json").write_text(json.dumps({"images": report.to_package()}))
    second, why = load_earlier_pictures(second_dir, "pkg-2")
    assert why is None
    images_dir, _, scenes = _rebuild(tmp_path, template, script, second, name="third")
    assert all((images_dir / f"{sid}.png").read_bytes() == pictures[sid] for sid in ids)
    assert scenes[ids[0]]["reused_from"]["package"] == "pkg-2"
    client = Recording([])
    _rebuild(tmp_path, template, script, second, client, redraw={ids[0]}, name="fourth")
    assert client.variants == [1]  # pkg-1 used variant 0 and pkg-2 asked nothing


def test_a_custom_cover_is_reused_and_redrawn_like_a_scene(tmp_path, template):
    script = _script()
    script["cover"] = {"frame": "custom", "visual": "A county office with a ballot box on the desk", "text": script["cover"]["text"]}
    earlier = _earlier(tmp_path, template, script)
    assert next(s for s in earlier.scenes if s["id"] == "cover")["accepted"]
    _, _, scenes = _rebuild(tmp_path, template, script, earlier)
    assert scenes["cover"]["reused_from"]["scene"] == "cover"
    client = Recording([])
    _, _, scenes = _rebuild(tmp_path, template, script, earlier, client, redraw={"cover"}, name="redrawn")
    assert len(client.prompts) == 1 and "reused_from" not in scenes["cover"] and scenes["cover"]["redraw_requested"] is True


def test_a_cover_only_matches_a_cover(tmp_path, template):
    script = _script()
    ids = _scene_ids(script)
    visual = next(s for s in script["scenes"] if s["id"] == ids[0])["visual"]
    earlier = _earlier(tmp_path, template, script)
    with_cover = json.loads(json.dumps(script))
    with_cover["cover"] = {"frame": "custom", "visual": visual, "text": script["cover"]["text"]}
    client = Recording([])
    _, _, scenes = _rebuild(tmp_path, template, with_cover, earlier, client)
    assert "reused_from" not in scenes["cover"] and len(client.prompts) == 1


def test_a_prompt_that_still_names_someone_stops_a_rebuild_too(tmp_path, template, monkeypatch):
    import studio.scene_images as si
    from studio.errors import BuildError

    script = _script()
    earlier = _earlier(tmp_path, template, script)
    monkeypatch.setattr(si, "clean_visual", lambda visual, people: visual)
    named = _script(visual="Secretary Weber signs a letter", real_people=["Shirley Weber"])
    with pytest.raises(BuildError, match="still names Weber"):
        _rebuild(tmp_path, template, named, earlier)


@pytest.mark.parametrize("package_json", [None, "not json", "{}", '{"images": {"scenes": [1]}}', '{"images": {"scenes": 3}}'])
def test_an_unreadable_earlier_package_is_reported_not_raised(tmp_path, package_json):
    if package_json is not None:
        (tmp_path / "package.json").write_text(package_json)
    earlier, why = load_earlier_pictures(tmp_path, "pkg")
    assert earlier is None and why


def test_redraw_names_resolve_to_the_picture_they_show():
    from studio.build import resolve_redraw

    script = {"cover": {"frame": "S2", "text": "x"}, "scenes": [
        {"id": "S1", "shot": "anchor", "visual": "The anchor at the desk."},
        {"id": "S2", "shot": "scene", "visual": "A ballot box."},
        {"id": "S3", "shot": "anchor", "visual": "The anchor at the desk."},
    ]}
    assert resolve_redraw(script, ["S2"]) == ({"S2"}, [])
    names, notes = resolve_redraw(script, ["cover"])
    assert names == {"cover", "S2"} and "it shows S2's picture" in notes[0]
    names, notes = resolve_redraw(script, ["S1"])  # no picture of its own: its screen shows the next scene shot's
    assert names == {"S1", "S2"} and "it shows S2's picture" in notes[0]
    names, notes = resolve_redraw(script, ["S3"])  # the last anchor shot's screen is the channel logo
    assert names == {"S3"} and "nothing to draw again" in notes[0]
    assert resolve_redraw(script, ["S9"]) == ({"S9"}, [])  # may be a scene id of the earlier package


def test_reuse_and_redraw_need_drawn_pictures(tmp_path):
    from studio.errors import BuildError

    with pytest.raises(BuildError, match="need --images"):
        build_package(script_path=FIXTURES_DIR / "scripts" / "short.json", channel_id="8bitnews-us", out_dir=tmp_path,
                      channels_dir=FIXTURES_DIR / "channels", tts_name="stub", redraw=["S2"])


@pytest.fixture(scope="module")
def clean_build(tmp_path_factory):
    """short.json with every picture passing, then a rebuild of it that reuses them all."""
    tmp_path = tmp_path_factory.mktemp("rebuild")
    script = json.loads((FIXTURES_DIR / "scripts" / "short.json").read_text())
    script_path = tmp_path / "script.json"
    script_path.write_text(json.dumps(script))
    common = dict(script_path=script_path, channel_id="8bitnews-us", channels_dir=FIXTURES_DIR / "channels", tts_name="stub",
                  images="openrouter")
    first = build_package(out_dir=tmp_path / "first", image_client=Client([_skin_picture()] * 10), **common)
    return {"tmp": tmp_path, "first": tmp_path / "first", "common": common,
            "first_package": json.loads(first.read_text())}


def test_a_full_reuse_build_makes_the_same_frames_at_no_cost(clean_build):
    built = build_package(out_dir=clean_build["tmp"] / "second", image_client=NoCalls(), reuse_from=clean_build["first"],
                          reuse_package_id="pkg-1", notes=["a note from the pick"], **clean_build["common"])
    package = json.loads(built.read_text())
    assert package["validate"]["passed"], [c for c in package["validate"]["checks"] if not c["ok"]]
    assert package["images"]["cost_usd"] == 0
    assert all(s["reused_from"]["package"] == "pkg-1" for s in package["images"]["scenes"])
    assert "a note from the pick" in package["notes"]
    first_dir, second_dir = clean_build["first"], built.parent
    for scene in clean_build["first_package"]["scenes"]:
        for kf in scene["keyframes"]:
            assert (first_dir / kf["path"]).read_bytes() == (second_dir / kf["path"]).read_bytes(), kf["path"]
    assert (first_dir / "cover.png").read_bytes() == (second_dir / "cover.png").read_bytes()


def test_a_card_on_a_redrawn_picture_is_scripts_to_fix(clean_build):
    sid = clean_build["first_package"]["images"]["scenes"][0]["id"]
    client = Client([WITH_TEXT] * (1 + MAX_REDRAWS))
    built = build_package(out_dir=clean_build["tmp"] / "card", image_client=client, reuse_from=clean_build["first"],
                          reuse_package_id="pkg-1", redraw=[sid], **clean_build["common"])
    package = json.loads(built.read_text())
    failed = [c for c in package["validate"]["checks"] if not c["ok"]]
    assert [c["id"] for c in failed] == ["picture_fallback_redrawn"]
    assert f"{sid} (3 attempt(s): text)" in failed[0]["note"]


def test_an_unreadable_earlier_package_means_a_full_rebuild_with_a_note(clean_build):
    client = Client([_skin_picture()] * 10)
    built = build_package(out_dir=clean_build["tmp"] / "full", image_client=client, reuse_from=clean_build["tmp"] / "nowhere",
                          reuse_package_id="pkg-1", **clean_build["common"])
    package = json.loads(built.read_text())
    assert package["validate"]["passed"] and package["images"]["cost_usd"] > 0
    assert any("the earlier package's pictures can't be reused" in n for n in package["notes"])
