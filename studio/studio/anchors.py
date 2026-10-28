"""The channel's anchors: a roster of adult anchors, each with a fixed look
and its own voice, one of them per video (format.md, 'Ведущие';
docs/architecture.md §5).

Drawing a new anchor is a one-off, not part of a video build:
  `studio anchor candidates` -- one picture per look of the channel's brief
      (studio/channels/<id>/anchors/brief.json), each turned into a sprite and
      shown in the studio, for a person to choose from;
  `studio anchor build` -- the frames of a chosen candidate (blink, mouth),
      drawn by editing the same picture, into an anchor sprite set.
"""
from __future__ import annotations

import io
import json
import math
import shutil
from dataclasses import dataclass
from pathlib import Path

import numpy as np
from PIL import Image

from .channel_assets import anchors_dir
from .errors import BuildError
from .openrouter import NoImageError, OpenRouterClient
from .palette import Palette, channel_palette_colors, quantize_image
from .sprite import SpriteFit, render_sprite, sprite_from_picture
from .template_pkg import TemplatePackage

DEFAULT_IMAGE_MODEL = "google/gemini-3.1-flash-image"
PREVIEW_ZOOM = 3


def load_brief(channel_id: str) -> dict:
    with open(anchors_dir(channel_id) / "brief.json") as f:
        return json.load(f)


def brief_canvas(brief: dict) -> tuple[int, int]:
    return brief["canvas"]["w"], brief["canvas"]["h"]


def candidate_prompt(brief: dict, look: dict) -> str:
    return brief["prompt"].format(who=look["who"], outfit=look["outfit"])


@dataclass(frozen=True)
class Candidate:
    id: str
    dir: Path
    cost_usd: float
    sprite: Image.Image


def studio_preview(sprite: Image.Image, template: TemplatePackage, logo: Image.Image) -> Image.Image:
    """The sprite at the template's anchor place in its studio, as an anchor
    shot shows it (the screen shows the channel logo)."""
    from .constants import LOGICAL_H, LOGICAL_W

    palette = template.palette()
    frame = Image.new("RGBA", (LOGICAL_W, LOGICAL_H), (0, 0, 0, 255))
    for layer in template.data["studio"]["layers"]:
        frame.alpha_composite(quantize_image(template.load_asset_image(layer["file"]), palette), (layer["x"], layer["y"]))
    screen = template.data["studio"]["screen"]
    frame.paste(logo.resize((screen["w"], screen["h"]), Image.NEAREST).convert("RGBA"), (screen["x"], screen["y"]))
    cfg = template.data["studio"]["anchor"]
    scale = cfg["scale"]
    frame.alpha_composite(sprite.resize((sprite.width * scale, sprite.height * scale), Image.NEAREST), (cfg["x"], cfg["y"]))
    return frame.convert("RGB")


def make_candidates(
    channel_id: str,
    out_dir: Path,
    client: OpenRouterClient,
    template: TemplatePackage,
    logo: Image.Image,
    looks: list[str] | None = None,
    n: int | None = None,
    model: str = DEFAULT_IMAGE_MODEL,
    attempts: int = 2,
) -> list[Candidate]:
    """One candidate per look (all of the brief's, or `looks`, or the first
    `n`): <out>/<look>/{picture.png, sprite.png, studio.png, candidate.json},
    plus <out>/candidates.png, every studio preview side by side."""
    brief = load_brief(channel_id)
    canvas = brief_canvas(brief)
    palette = Palette(channel_palette_colors(channel_id))
    chosen = [lk for lk in brief["looks"] if looks is None or lk["id"] in looks]
    if looks is not None and len(chosen) != len(set(looks)):
        unknown = sorted(set(looks) - {lk["id"] for lk in brief["looks"]})
        raise BuildError(f"unknown look id(s) {unknown}; the brief has {[lk['id'] for lk in brief['looks']]}")
    if n is not None:
        chosen = chosen[:n]
    out_dir = Path(out_dir)
    result = []
    for look in chosen:
        prompt = candidate_prompt(brief, look)
        cost, reply, sprite, err = 0.0, None, None, None
        for variant in range(attempts):
            try:
                reply = client.generate_image(model, prompt, brief["aspect_ratio"], variant=variant)
            except NoImageError as e:
                err = str(e)
                cost += e.cost_usd
                continue
            cost += reply.cost_usd
            picture = Image.open(io.BytesIO(reply.png))
            try:
                sprite, fit, _ = sprite_from_picture(picture, canvas, palette)
            except ValueError as e:
                err = str(e)
                continue
            if fit.crop[1] == 0:
                err = "the figure touches the top edge (head cut off)"
                sprite = None
                continue
            break
        if sprite is None:
            print(f"{look['id']}: no usable picture ({err})")
            continue
        d = out_dir / look["id"]
        d.mkdir(parents=True, exist_ok=True)
        picture.save(d / "picture.png")
        sprite.save(d / "sprite.png")
        studio_preview(sprite, template, logo).save(d / "studio.png")
        (d / "candidate.json").write_text(json.dumps(
            {"look": look, "prompt": prompt, "model": model, "variant": variant, "cost_usd": cost,
             "canvas": {"w": canvas[0], "h": canvas[1]}}, indent=2))
        result.append(Candidate(look["id"], d, cost, sprite))
    if result:
        sheet = Image.new("RGB", (len(result) * (270 + 6) - 6, 480), (40, 40, 40))
        for i, c in enumerate(result):
            sheet.paste(Image.open(c.dir / "studio.png"), (i * 276, 0))
        sheet.resize((sheet.width * 2, sheet.height * 2), Image.NEAREST).save(out_dir / "candidates.png")
    return result


# --- studio anchor build ---------------------------------------------------

DEFAULT_VISION_MODEL = "google/gemini-3.5-flash-lite"
LANDMARKS_PROMPT = (
    "The image is a pixel-art portrait of a TV news anchor on a flat green background. Give bounding boxes as "
    "[ymin, xmin, ymax, xmax], each number normalized to 0-1000 of the image height or width, for: \"head\" (the whole "
    "head with the hair, down to the chin), \"eyes\" (one box around both eyes with the eyelids, without the "
    "eyebrows), \"mouth\" (the lips). Answer with JSON only: {\"head\": [...], \"eyes\": [...], \"mouth\": [...]}."
)
EDIT_PROMPT = (
    "Edit this image. Show the same person with {change}. Change nothing else: keep exactly the same pose, framing, "
    "size and position in the image, the same face, hair, clothing, colors, pixel-art style and the same flat green "
    "background. No text."
)
EDITS = {
    "eyes_closed": ("eyes", "both eyes fully closed, as in a natural blink"),
    "mouth_1": ("mouth", "the mouth slightly open, as when speaking, lips just apart"),
    "mouth_2": ("mouth", "the mouth open wider, as in the middle of saying 'ah', the teeth visible"),
}
MAX_FACE_DRIFT = 0.15  # share of face pixels (outside eyes and mouth) an edit may change
REGISTER_SEARCH_PX = 16  # how far an edited picture may have shifted


def _box(raw, name: str) -> tuple[float, float, float, float]:
    if (not isinstance(raw, list) or len(raw) != 4 or not all(isinstance(v, (int, float)) for v in raw)
            or not (0 <= raw[0] < raw[2] <= 1000 and 0 <= raw[1] < raw[3] <= 1000)):
        raise BuildError(f"vision model gave no usable {name!r} box: {raw!r}")
    return tuple(float(v) for v in raw)


def _picture_box(box: tuple, size: tuple[int, int]) -> tuple[int, int, int, int]:
    """[ymin, xmin, ymax, xmax] in 0-1000 -> (x0, y0, x1, y1) picture pixels."""
    w, h = size
    return (round(box[1] / 1000 * w), round(box[0] / 1000 * h), round(box[3] / 1000 * w), round(box[2] / 1000 * h))


def _sprite_box(pbox: tuple[int, int, int, int], fit: SpriteFit, pad: int) -> tuple[int, int, int, int]:
    cx0, cy0, cx1, cy1 = fit.crop
    sx, sy = fit.size[0] / (cx1 - cx0), fit.size[1] / (cy1 - cy0)
    x0 = fit.offset[0] + math.floor((pbox[0] - cx0) * sx) - pad
    y0 = fit.offset[1] + math.floor((pbox[1] - cy0) * sy) - pad
    x1 = fit.offset[0] + math.ceil((pbox[2] - cx0) * sx) + pad
    y1 = fit.offset[1] + math.ceil((pbox[3] - cy0) * sy) + pad
    cw, ch = fit.canvas
    return max(0, x0), max(0, y0), min(cw, x1), min(ch, y1)


def _register(base: Image.Image, edit: Image.Image, region: tuple[int, int, int, int],
              search: int = REGISTER_SEARCH_PX) -> Image.Image:
    """`edit` shifted by the whole-pixel offset that best lines its `region`
    up with `base` (image models redraw an edit a few pixels off)."""
    b = np.asarray(base.convert("L"), dtype=np.int16)
    e = np.asarray(edit.convert("L"), dtype=np.int16)
    x0, y0, x1, y1 = region
    x0, y0 = max(x0, search), max(y0, search)
    x1, y1 = min(x1, b.shape[1] - search), min(y1, b.shape[0] - search)
    if x1 - x0 < 4 or y1 - y0 < 4:
        return edit.convert("RGB")  # the region hugs the edge: nothing to line up by
    ref = b[y0:y1:2, x0:x1:2]
    best, best_d = (0, 0), None
    for dy in range(-search, search + 1, 2):
        for dx in range(-search, search + 1, 2):
            d = np.abs(e[y0 + dy:y1 + dy:2, x0 + dx:x1 + dx:2] - ref).mean()
            if best_d is None or d < best_d:
                best, best_d = (dx, dy), d
    for dy in range(best[1] - 1, best[1] + 2):  # refine to one pixel
        for dx in range(best[0] - 1, best[0] + 2):
            if abs(dx) > search or abs(dy) > search:
                continue
            d = np.abs(e[y0 + dy:y1 + dy:2, x0 + dx:x1 + dx:2] - ref).mean()
            if d < best_d:
                best, best_d = (dx, dy), d
    dx, dy = best
    shifted = Image.new("RGB", edit.size, edit.getpixel((0, 0)))
    shifted.paste(edit.convert("RGB"), (-dx, -dy))
    return shifted


def edit_problem(base_sprite: Image.Image, edit_sprite: Image.Image, target: tuple, face: tuple, keep_out: list[tuple]) -> str | None:
    """Why an edited sprite can't give its patch (None: it can). The patch
    must have changed, and the rest of the face must not have: the patch is
    pasted onto the base body, so a redrawn face would not fit around it."""
    diff = (np.asarray(base_sprite) != np.asarray(edit_sprite)).any(-1)
    tx0, ty0, tx1, ty1 = target
    if diff[ty0:ty1, tx0:tx1].sum() < 2:
        return "the edit did not change that part of the face"
    fx0, fy0, fx1, fy1 = face
    mask = np.zeros_like(diff)
    mask[fy0:fy1, fx0:fx1] = True
    for x0, y0, x1, y1 in keep_out:
        mask[y0:y1, x0:x1] = False
    if mask.sum() == 0:
        return None
    drift = diff[mask].mean()
    if drift > MAX_FACE_DRIFT:
        return f"the edit redrew the rest of the face ({drift:.0%} of its pixels differ, limit {MAX_FACE_DRIFT:.0%})"
    return None


def _landmarks(answer: dict, size: tuple[int, int], fit: SpriteFit):
    """The vision model's boxes -> (0-1000 boxes, picture boxes, the face and
    the eyes/mouth regions on the sprite); BuildError if they don't make a
    face (eyes or mouth outside the head, eyes not above the mouth)."""
    boxes = {name: _box(answer.get(name), name) for name in ("head", "eyes", "mouth")}
    pboxes = {name: _picture_box(b, size) for name, b in boxes.items()}
    face = _sprite_box(pboxes["head"], fit, pad=0)
    regions = {"eyes": _sprite_box(pboxes["eyes"], fit, pad=1), "mouth": _sprite_box(pboxes["mouth"], fit, pad=1)}
    for name, (x0, y0, x1, y1) in regions.items():
        if not (face[0] <= x0 and face[1] <= y0 and x1 <= face[2] and y1 <= face[3]):
            raise BuildError(f"{name} box {regions[name]} is not inside the head {face}")
    if regions["eyes"][3] > regions["mouth"][1]:
        raise BuildError(f"eyes {regions['eyes']} are not above the mouth {regions['mouth']}")
    return boxes, pboxes, face, regions


def voice_setup_problems(voice_id: str) -> list[str]:
    """A voice an anchor can speak with: pinned in the image and measured."""
    from .config import tts_settings
    from .paths import STUDIO_ROOT

    problems = []
    pinned = {f["path"] for f in json.loads((STUDIO_ROOT / "models.json").read_text())["kokoro"]["files"]}
    if f"voices/{voice_id}.bin" not in pinned:
        problems.append(f"voice_id {voice_id!r} is not a Kokoro voice pinned in studio/models.json")
    measured = tts_settings("kokoro").get("voices", {}).get(voice_id)
    if not isinstance(measured, dict) or not all(isinstance(measured.get(k), (int, float)) for k in ("speed", "serious_speed")):
        problems.append(f"voice_id {voice_id!r} has no calibrated speed and serious_speed in config.json tts.kokoro.voices")
    return problems


def voice_problems(channel_id: str, voice_id: str, anchor_id: str) -> list[str]:
    """`voice_setup_problems`, and the voice not taken by another active anchor."""
    from .channel_assets import anchor_ids, load_anchor

    problems = voice_setup_problems(voice_id)
    taken = [other for other in anchor_ids(channel_id) if other != anchor_id and load_anchor(channel_id, other).voice_id == voice_id]
    if taken:
        problems.append(f"voice {voice_id!r} is already the voice of {', '.join(taken)}: each anchor speaks with its own")
    return problems


def build_anchor(
    channel_id: str,
    candidate_dir: Path,
    anchor_id: str,
    voice_id: str,
    out_dir: Path,
    client: OpenRouterClient,
    image_model: str = DEFAULT_IMAGE_MODEL,
    vision_model: str = DEFAULT_VISION_MODEL,
    attempts: int = 3,
    work_dir: Path | None = None,
) -> dict:
    """A chosen candidate -> an anchor sprite set in `out_dir` (manifest.json,
    body, eyes and mouth frames). The body is the candidate's sprite as is;
    blink and mouth frames are patches cut from edits of the same picture.
    The candidate's calm, neutral pose is both the normal and the serious pose
    (format.md: in serious mode the anchor neither smiles nor gestures).
    `work_dir` gets the edits and a sheet of every frame, for a person to look at."""
    candidate_dir, out_dir = Path(candidate_dir), Path(out_dir)
    meta = json.loads((candidate_dir / "candidate.json").read_text())
    brief = load_brief(channel_id)
    canvas = brief_canvas(brief)
    palette = Palette(channel_palette_colors(channel_id))
    picture = Image.open(candidate_dir / "picture.png").convert("RGB")
    picture_png = (candidate_dir / "picture.png").read_bytes()
    # Before anything is paid for: the voice must be usable and free.
    problems = voice_problems(channel_id, voice_id, anchor_id)
    if problems:
        raise BuildError(f"{anchor_id}: " + "; ".join(problems))
    sprite, fit, mask = sprite_from_picture(picture, canvas, palette)
    cost = 0.0

    landmark_problems = []
    for variant in range(attempts):
        marks = client.ask_json(vision_model, LANDMARKS_PROMPT, (picture_png,), variant=variant)
        cost += marks.cost_usd
        try:
            boxes, pboxes, face, regions = _landmarks(marks.data, picture.size, fit)
            break
        except BuildError as e:
            landmark_problems.append(f"variant {variant}: {e}")
    else:
        raise BuildError(f"{anchor_id}: no usable head, eyes and mouth boxes from {vision_model}: "
                         + "; ".join(landmark_problems))

    work = Path(work_dir) if work_dir else candidate_dir / f"build-{anchor_id}"
    work.mkdir(parents=True, exist_ok=True)
    patches: dict[str, Image.Image] = {
        "eyes_open": sprite.crop(regions["eyes"]),
        "mouth_0": sprite.crop(regions["mouth"]),
    }
    edits_meta = {}
    for frame, (region_name, change) in EDITS.items():
        prompt = EDIT_PROMPT.format(change=change)
        problems = []
        for variant in range(attempts):
            try:
                reply = client.generate_image(image_model, prompt, brief["aspect_ratio"], images=(picture_png,), variant=variant)
            except NoImageError as e:
                problems.append(str(e))
                cost += e.cost_usd
                continue
            cost += reply.cost_usd
            edited = Image.open(io.BytesIO(reply.png)).convert("RGB")
            if edited.size != picture.size:
                edited = edited.resize(picture.size, Image.LANCZOS)
            edited = _register(picture, edited, pboxes["head"])
            edited.save(work / f"{frame}-{variant}.png")
            edit_sprite = render_sprite(edited, fit, palette, mask)
            problem = edit_problem(sprite, edit_sprite, regions[region_name], face, list(regions.values()))
            if problem is None:
                patches[frame] = edit_sprite.crop(regions[region_name])
                edits_meta[frame] = {"prompt": prompt, "variant": variant}
                break
            problems.append(f"variant {variant}: {problem}")
        if frame not in patches:
            raise BuildError(f"{anchor_id}: no usable {frame} edit after {attempts} attempt(s): " + "; ".join(problems))

    # Built beside the roster and checked there; only a set that passes
    # replaces anything in it.
    staging = out_dir.parent / f".{out_dir.name}.building"
    if staging.exists():
        shutil.rmtree(staging)
    staging.mkdir(parents=True)
    sprite.save(staging / "body.png")
    for name, img in patches.items():
        img.save(staging / f"{name}.png")
    ex, ey = regions["eyes"][:2]
    mx, my = regions["mouth"][:2]
    manifest = {
        "status": "active",
        "voice_id": voice_id,
        "look": meta["look"],
        "canvas": {"w": canvas[0], "h": canvas[1]},
        "face_rect": {"x": face[0], "y": face[1], "w": face[2] - face[0], "h": face[3] - face[1]},
        "poses": {"normal": {"body": "body.png"}, "serious": {"body": "body.png"}},
        "eyes": {"offset": {"x": ex, "y": ey}, "frames": {"open": "eyes_open.png", "closed": "eyes_closed.png"}},
        "mouth": {
            "offset": {"x": mx, "y": my},
            "frames": {"0": "mouth_0.png", "1": "mouth_1.png", "2": "mouth_2.png"},
            "volume_thresholds": {"1": 0.15, "2": 0.5},
        },
        "blink": {"period_s": 4.0, "duration_s": 0.12},
        "source": {
            "image_model": meta["model"],
            "prompt": meta["prompt"],
            "variant": meta["variant"],
            "edits": edits_meta,
            "landmarks": {"model": vision_model, "boxes": {k: list(v) for k, v in boxes.items()}},
        },
        "license": "Drawn for 8bitnews by the image model named in `source` from the prompts there, then cut into "
                   "sprites by `studio anchor build`; no third-party artwork was given to the model.",
    }
    (staging / "manifest.json").write_text(json.dumps(manifest, indent=2) + "\n")
    problems = anchor_problems(channel_id, staging)
    if problems:
        raise BuildError(f"the built anchor does not pass its checks (left in {staging}):\n"
                         + "\n".join(f"  - {x}" for x in problems))
    if out_dir.exists():
        shutil.rmtree(out_dir)
    staging.rename(out_dir)
    _frames_sheet(out_dir, manifest).save(work / "frames.png")
    manifest["_cost_usd"] = cost
    return manifest


def compose(anchor_dir: Path, manifest: dict, pose: str, eyes: str, mouth: str) -> Image.Image:
    img = Image.open(anchor_dir / manifest["poses"][pose]["body"]).convert("RGBA")
    o = manifest["eyes"]["offset"]
    img.alpha_composite(Image.open(anchor_dir / manifest["eyes"]["frames"][eyes]).convert("RGBA"), (o["x"], o["y"]))
    o = manifest["mouth"]["offset"]
    img.alpha_composite(Image.open(anchor_dir / manifest["mouth"]["frames"][mouth]).convert("RGBA"), (o["x"], o["y"]))
    return img


def _frames_sheet(anchor_dir: Path, manifest: dict) -> Image.Image:
    combos = [("open", "0"), ("open", "1"), ("open", "2"), ("closed", "0")]
    w, h = manifest["canvas"]["w"], manifest["canvas"]["h"]
    sheet = Image.new("RGBA", (len(combos) * (w + 4), h), (40, 40, 40, 255))
    for i, (eyes, mouth) in enumerate(combos):
        sheet.alpha_composite(compose(anchor_dir, manifest, "normal", eyes, mouth), (i * (w + 4), 0))
    return sheet.resize((sheet.width * PREVIEW_ZOOM, sheet.height * PREVIEW_ZOOM), Image.NEAREST)


def anchor_problems(channel_id: str, anchor_dir: Path) -> list[str]:
    """What is wrong with one anchor's sprite set ([] when nothing): run by the
    tests over the whole roster and by `studio anchor build` on what it wrote."""
    d = Path(anchor_dir)
    anchor_id = d.name
    try:
        m = json.loads((d / "manifest.json").read_text())
    except (OSError, json.JSONDecodeError) as e:
        return [f"{anchor_id}: manifest.json unreadable: {e}"]
    problems = []
    p = problems.append
    if m.get("status") not in ("active", "retired"):
        p(f"status {m.get('status')!r} is neither 'active' nor 'retired'")
    canvas = (m["canvas"]["w"], m["canvas"]["h"])
    if canvas != brief_canvas(load_brief(channel_id)):
        p(f"canvas {canvas} differs from the channel's {brief_canvas(load_brief(channel_id))} (anchors/brief.json): "
          "templates place every anchor the same way")
    palette = {c.lower() for c in channel_palette_colors(channel_id)}

    def image(rel: str) -> Image.Image | None:
        if not (d / rel).is_file():
            p(f"{rel} is missing")
            return None
        img = Image.open(d / rel).convert("RGBA")
        bad = {"#%02x%02x%02x" % c[1][:3] for c in img.getcolors(1 << 20) if c[1][3] and "#%02x%02x%02x" % c[1][:3] not in palette}
        if bad:
            p(f"{rel} has colors outside the channel palette: {', '.join(sorted(bad)[:5])}")
        return img

    for pose in ("normal", "serious"):
        if pose not in m["poses"]:
            p(f"pose {pose!r} is missing")
            continue
        body = image(m["poses"][pose]["body"])
        if body is not None and body.size != canvas:
            p(f"{m['poses'][pose]['body']} is {body.size}, the canvas is {canvas}")
    fr = m["face_rect"]
    face = (fr["x"], fr["y"], fr["x"] + fr["w"], fr["y"] + fr["h"])
    if not (0 <= face[0] < face[2] <= canvas[0] and 0 <= face[1] < face[3] <= canvas[1]):
        p(f"face_rect {fr} is not inside the canvas")
    for part, frames in (("eyes", ("open", "closed")), ("mouth", ("0", "1", "2"))):
        o = m[part]["offset"]
        sizes = set()
        for frame in frames:
            rel = m[part]["frames"].get(frame)
            if rel is None:
                p(f"{part} frame {frame!r} is missing")
                continue
            img = image(rel)
            if img is None:
                continue
            sizes.add(img.size)
            if img.getchannel("A").getextrema()[0] < 255:
                p(f"{rel} has transparent pixels: a frame must fully cover the one under it")
        if len(sizes) > 1:
            p(f"{part} frames differ in size: {sorted(sizes)}")
        for w, h in sizes:
            if not (face[0] <= o["x"] and face[1] <= o["y"] and o["x"] + w <= face[2] and o["y"] + h <= face[3]):
                p(f"{part} frames at {o} ({w}x{h}) are not inside face_rect {fr}")
    t = m["mouth"]["volume_thresholds"]
    if not 0 < t["1"] < t["2"] <= 1:
        p(f"mouth.volume_thresholds {t} must rise within (0, 1]")
    if not 0 < m["blink"]["duration_s"] < m["blink"]["period_s"]:
        p(f"blink {m['blink']} must be shorter than its period")

    problems.extend(voice_setup_problems(m.get("voice_id")))
    return [f"{anchor_id}: {x}" for x in problems]


def roster_problems(channel_id: str) -> list[str]:
    """The roster as a whole: someone to pick, and one voice per face."""
    from .channel_assets import anchor_ids, load_anchor

    active = anchor_ids(channel_id)
    if not active:
        return [f"channel {channel_id!r} has no active anchor"]
    by_voice: dict[str, list[str]] = {}
    for anchor_id in active:
        by_voice.setdefault(load_anchor(channel_id, anchor_id).voice_id, []).append(anchor_id)
    return [f"anchors {', '.join(ids)} share the voice {voice!r}: each anchor speaks with its own (format.md)"
            for voice, ids in by_voice.items() if len(ids) > 1]
