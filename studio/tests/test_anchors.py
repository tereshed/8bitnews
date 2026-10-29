"""studio anchor candidates / build without the network (anchor_fakes), and
the channel's roster as committed."""
import json
from pathlib import Path

import numpy as np
import pytest
from PIL import Image

from anchor_fakes import FakeClient, as_image, no_image, picture
from studio.anchors import anchor_problems, build_anchor, edit_problem, make_candidates, roster_problems
from studio.channel_assets import anchor_ids, anchors_dir, load_anchor, load_channel_logo, pick_anchor
from studio.errors import BuildError
from studio.paths import TEMPLATES_DIR
from studio.template_pkg import load_template

CHANNEL = "8bitnews-us"
FIXTURES = Path(__file__).resolve().parent / "fixtures"


@pytest.fixture
def template():
    return load_template(TEMPLATES_DIR / "nes-desk")


def _candidates(tmp_path, client, template, looks=("w1",)):
    return make_candidates(CHANNEL, tmp_path / "cand", client, template, load_channel_logo(CHANNEL), looks=list(looks))


def test_candidates_write_a_sprite_and_a_studio_preview(tmp_path, template):
    made = _candidates(tmp_path, FakeClient([picture(), picture()]), template, looks=("w1", "m1"))
    assert [c.id for c in made] == ["w1", "m1"]
    d = tmp_path / "cand" / "w1"
    assert Image.open(d / "sprite.png").size == (120, 160)
    assert Image.open(d / "studio.png").size == (270, 480)
    meta = json.loads((d / "candidate.json").read_text())
    assert "a woman in her late 30s" in meta["prompt"] and meta["look"]["id"] == "w1"
    assert (tmp_path / "cand" / "candidates.png").is_file()


def test_candidate_prompt_keeps_the_format_rules(tmp_path, template):
    client = FakeClient([picture()])
    _candidates(tmp_path, client, template)
    prompt = client.calls[0]["prompt"]
    for rule in ("not chibi", "does not resemble any real person", "No text", "no logos", "flat solid pure green"):
        assert rule in prompt


def test_a_cut_off_head_or_a_refusal_is_drawn_again(tmp_path, template):
    client = FakeClient([no_image(), picture(head_top=0), picture()])
    made = _candidates(tmp_path, client, template)
    assert len(made) == 0  # two attempts per look by default: refusal, then a head cut by the top edge
    client = FakeClient([picture(head_top=0), picture()])
    made = _candidates(tmp_path, client, template)
    assert len(made) == 1 and [c["variant"] for c in client.calls] == [0, 1]


@pytest.fixture
def candidate(tmp_path, template):
    _candidates(tmp_path, FakeClient([picture()]), template)
    return tmp_path / "cand" / "w1"


def _build(tmp_path, candidate, images, landmarks=None, **kw):
    """Rebuilds w1 (voice af_sarah) into a temporary directory: every pinned
    and measured voice belongs to an anchor of the roster."""
    client = FakeClient(images, landmarks)
    manifest = build_anchor(CHANNEL, candidate, "w1", "af_sarah", tmp_path / "anchor", client, **kw)
    return manifest, client


def test_build_cuts_blink_and_mouth_frames_from_edits(tmp_path, candidate):
    manifest, client = _build(tmp_path, candidate, [picture(eyes_closed=True), picture(mouth=1), picture(mouth=2)])
    out = tmp_path / "anchor"
    assert anchor_problems(CHANNEL, out) == []
    assert manifest["voice_id"] == "af_sarah" and manifest["status"] == "active"
    assert [c["inputs"] for c in client.calls if c["kind"] == "image"] == [1, 1, 1]  # edits of the same picture
    body = np.asarray(Image.open(out / "body.png"))
    for frame, part in (("eyes_closed", "eyes"), ("mouth_1", "mouth"), ("mouth_2", "mouth")):
        patch = np.asarray(Image.open(out / f"{frame}.png"))
        o = manifest[part]["offset"]
        under = body[o["y"]:o["y"] + patch.shape[0], o["x"]:o["x"] + patch.shape[1]]
        assert (patch != under).any(-1).sum() >= 2, frame
    eyes_open = np.asarray(Image.open(out / "eyes_open.png"))
    o = manifest["eyes"]["offset"]
    assert (eyes_open == body[o["y"]:o["y"] + eyes_open.shape[0], o["x"]:o["x"] + eyes_open.shape[1]]).all()


def test_an_edit_drawn_a_few_pixels_off_is_lined_up(tmp_path, candidate):
    manifest, _ = _build(tmp_path, candidate, [picture(eyes_closed=True, shift=(7, -5)), picture(mouth=1, shift=(-6, 4)),
                                               picture(mouth=2)])
    assert anchor_problems(CHANNEL, tmp_path / "anchor") == []
    assert manifest["source"]["edits"]["eyes_closed"]["variant"] == 0


def test_an_edit_that_redraws_the_face_is_drawn_again(tmp_path, candidate):
    redrawn = picture(eyes_closed=True, face_color=(252, 160, 68))
    manifest, client = _build(tmp_path, candidate, [redrawn, picture(eyes_closed=True), picture(mouth=1), picture(mouth=2)])
    assert manifest["source"]["edits"]["eyes_closed"]["variant"] == 1


def test_no_usable_edit_fails_the_build(tmp_path, candidate):
    redrawn = picture(eyes_closed=True, face_color=(252, 160, 68))
    with pytest.raises(BuildError, match="no usable eyes_closed edit"):
        _build(tmp_path, candidate, [redrawn, no_image(), picture()])  # third: unchanged eyes


BAD_LANDMARKS = {"head": [100, 300, 400, 700], "eyes": [500, 300, 520, 700], "mouth": [300, 400, 320, 600]}


def test_landmarks_that_make_no_face_are_asked_again(tmp_path, candidate):
    from anchor_fakes import LANDMARKS

    manifest, client = _build(tmp_path, candidate, [picture(eyes_closed=True), picture(mouth=1), picture(mouth=2)],
                              landmarks=[BAD_LANDMARKS, LANDMARKS])
    assert [c["variant"] for c in client.calls if c["kind"] == "json"] == [0, 1]
    assert anchor_problems(CHANNEL, tmp_path / "anchor") == []


def test_landmarks_that_never_make_a_face_fail(tmp_path, candidate):
    with pytest.raises(BuildError, match="no usable head, eyes and mouth boxes"):
        _build(tmp_path, candidate, [], landmarks=BAD_LANDMARKS)


def test_the_voice_is_checked_before_anything_is_paid_for(tmp_path, candidate):
    for voice, why in (("am_adam", "not a Kokoro voice pinned"), ("af_heart", "already the voice of w2")):
        client = FakeClient([picture()])
        with pytest.raises(BuildError, match=why):
            build_anchor(CHANNEL, candidate, "w9", voice, tmp_path / "w9", client)
        assert client.calls == [] and not (tmp_path / "w9").exists()


def test_a_failed_build_leaves_the_existing_set_alone(tmp_path, candidate):
    out = tmp_path / "anchor"
    out.mkdir()
    (out / "manifest.json").write_text("{}")
    with pytest.raises(BuildError):
        _build(tmp_path, candidate, [picture(), picture(), picture()])  # edits that change nothing
    assert (out / "manifest.json").read_text() == "{}" and not (out / "body.png").exists()


def test_edit_problem_names_a_redrawn_face():
    base = Image.new("RGBA", (20, 20), (240, 208, 176, 255))
    edit = Image.new("RGBA", (20, 20), (252, 160, 68, 255))
    assert "redrew the rest of the face" in edit_problem(base, edit, (5, 5, 10, 8), (0, 0, 20, 20), [(5, 5, 10, 8)])
    assert "did not change" in edit_problem(base, base.copy(), (5, 5, 10, 8), (0, 0, 20, 20), [(5, 5, 10, 8)])


def test_anchor_problems_catch_a_broken_set(tmp_path, candidate):
    _build(tmp_path, candidate, [picture(eyes_closed=True), picture(mouth=1), picture(mouth=2)])
    out = tmp_path / "anchor"
    m = json.loads((out / "manifest.json").read_text())
    m["voice_id"] = "am_nobody"
    m["canvas"] = {"w": 64, "h": 96}
    (out / "manifest.json").write_text(json.dumps(m))
    Image.new("RGBA", (3, 3), (1, 2, 3, 255)).save(out / "mouth_2.png")
    problems = "\n".join(anchor_problems(CHANNEL, out))
    assert "not a Kokoro voice pinned" in problems and "no calibrated speed" in problems
    assert "canvas" in problems and "outside the channel palette" in problems and "differ in size" in problems


# --- the committed roster ----------------------------------------------------

@pytest.fixture
def channel_copy(tmp_path, monkeypatch):
    """The channel's studio assets copied, so anchors can be retired or
    removed without touching the repository."""
    import shutil

    import studio.channel_assets as ca
    from studio.paths import channel_dir

    dest = tmp_path / "channels" / CHANNEL
    shutil.copytree(channel_dir(CHANNEL), dest)
    monkeypatch.setattr(ca, "channel_dir", lambda channel_id: tmp_path / "channels" / channel_id)
    ca.load_anchor.cache_clear()
    ca.load_channel_assets.cache_clear()
    yield dest / "anchors"
    ca.load_anchor.cache_clear()
    ca.load_channel_assets.cache_clear()


def _retire(anchors: Path, anchor_id: str) -> None:
    path = anchors / anchor_id / "manifest.json"
    m = json.loads(path.read_text())
    m["status"] = "retired"
    path.write_text(json.dumps(m))


def test_a_retired_anchor_is_never_picked(channel_copy):
    _retire(channel_copy, "w2")
    assert "w2" not in anchor_ids(CHANNEL) and "w2" in anchor_ids(CHANNEL, include_retired=True)
    base = json.loads((FIXTURES / "scripts" / "demo.json").read_text())
    picks = {pick_anchor(CHANNEL, dict(base, cover=dict(base["cover"], text=f"V{i}"))) for i in range(200)}
    assert "w2" not in picks and len(picks) == 10


def test_retiring_an_anchor_moves_only_its_own_scripts(channel_copy):
    base = json.loads((FIXTURES / "scripts" / "demo.json").read_text())
    scripts = [dict(base, cover=dict(base["cover"], text=f"V{i}")) for i in range(100)]
    before = [pick_anchor(CHANNEL, s) for s in scripts]
    _retire(channel_copy, "m3")
    after = [pick_anchor(CHANNEL, s) for s in scripts]
    assert all(b == a for a, b in zip(before, after) if a != "m3")  # only m3's scripts move
    assert "m3" in before
    assert all(a != "m3" for a in after)


def test_template_validate_checks_every_anchors_face(tmp_path):
    """A plate over the face of one anchor only -- not the first in the roster."""
    import shutil

    from studio.errors import TemplateValidationError
    from studio.validate import validate_template

    template = load_template(TEMPLATES_DIR / "nes-desk")
    cfg = template.data["studio"]["anchor"]
    faces = {a: load_anchor(CHANNEL, a).face_rect_in_frame(cfg) for a in anchor_ids(CHANNEL)}
    first = anchor_ids(CHANNEL)[0]

    def inside(x, y, r):
        return r["x"] <= x < r["x"] + r["w"] and r["y"] <= y < r["y"] + r["h"]

    spot = next(((a, x, y) for a, r in faces.items() if a != first for y in range(r["y"], r["y"] + r["h"])
                 for x in range(r["x"], r["x"] + r["w"])
                 if not any(inside(x, y, other) for b, other in faces.items() if b != a)), None)
    assert spot, "every face pixel is shared with another face"
    anchor_id, x, y = spot
    d = tmp_path / "t"
    shutil.copytree(TEMPLATES_DIR / "nes-desk", d)
    data = json.loads((d / "template.json").read_text())
    data["plates"]["source_plate"]["rect"] = {"x": x, "y": y, "w": 1, "h": 1}
    (d / "template.json").write_text(json.dumps(data))
    with pytest.raises(TemplateValidationError) as e:
        validate_template(d)
    assert f"overlaps the face of anchor {anchor_id} " in str(e.value)


def test_every_anchor_of_the_channel_passes():
    ids = anchor_ids(CHANNEL, include_retired=True)
    assert ids
    for anchor_id in ids:
        assert anchor_problems(CHANNEL, anchors_dir(CHANNEL) / anchor_id) == []
    assert roster_problems(CHANNEL) == []


def test_pick_is_stable_per_script_and_spreads_over_the_roster():
    base = json.loads((FIXTURES / "scripts" / "demo.json").read_text())
    assert pick_anchor(CHANNEL, base) == pick_anchor(CHANNEL, json.loads(json.dumps(base)))
    picks = set()
    for i in range(200):
        variant = dict(base, cover=dict(base["cover"], text=f"Variant {i}"))
        picks.add(pick_anchor(CHANNEL, variant))
    assert picks == set(anchor_ids(CHANNEL))


def test_anchor_voice_comes_from_its_manifest():
    for anchor_id in anchor_ids(CHANNEL):
        assert load_anchor(CHANNEL, anchor_id).voice_id == json.loads(
            (anchors_dir(CHANNEL) / anchor_id / "manifest.json").read_text())["voice_id"]


# --- studio build --anchor ---------------------------------------------------

def test_build_takes_the_given_anchor_and_its_voice(tmp_path, channel_copy):
    from studio.build import build_package

    _retire(channel_copy, "w2")
    package = json.loads(build_package(
        script_path=FIXTURES / "scripts" / "short.json", channel_id=CHANNEL, out_dir=tmp_path / "out",
        channels_dir=FIXTURES / "channels", tts_name="stub", anchor_id="w2",
    ).read_text())
    assert package["anchor"] == {"id": "w2", "chosen_by": "argument"}
    assert package["voice"]["voice_id"] == "af_heart"
    assert any("anchor 'w2' is retired" in n for n in package["notes"])
    assert package["validate"]["passed"], [c for c in package["validate"]["checks"] if not c["ok"]]


def test_build_refuses_an_unknown_anchor(tmp_path):
    from studio.build import build_package

    with pytest.raises(BuildError, match="'nobody' is not an anchor"):
        build_package(script_path=FIXTURES / "scripts" / "short.json", channel_id=CHANNEL, out_dir=tmp_path,
                      channels_dir=FIXTURES / "channels", tts_name="stub", anchor_id="nobody")
