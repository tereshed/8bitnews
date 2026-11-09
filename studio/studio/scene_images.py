"""Scene pictures from the image provider (`studio build --images openrouter`).

For every `shot: scene` (and a `custom` cover): the prompt (`scene_prompt`)
-> the image model -> a vision check of the picture (JSON: text, logos,
flags or seals, game elements, photorealism, gore, people the scene doesn't
describe). A picture that fails is drawn again with the prohibition it broke
stated harder, at most twice; if none passes, a neutral card (the channel
logo on the template's colors) stands in, the package's notes say so, and the
package fails its `picture_fallback` check (checks.py), so it goes back to
Video without paying for QA. Extra people alone don't make a card: the first picture with
nothing else wrong is kept, with a note. Every picture
drawn, passed or not, and its check are kept in the package (`images/`).
The frame engine snaps the accepted picture to the 270x480 grid and the
template's NES palette like any scene asset.

A rebuild (`--reuse-from`, newsroom/tools/video_build.py --reuse-package) takes
a picture from the earlier package instead of paying for it when it was
accepted, isn't named in `--redraw`, and the prompt it would be drawn from is
word for word the prompt the earlier one was (`_earlier_matches`): that prompt
holds everything that feeds the picture (the channel's style, the template's
look, serious mode, the cleaned visual, the shot's frame), so scene ids may
shift and anything else that changed means a redraw. Its bytes must still
decode (`_reusable_picture`); the package's own checks run on it as on any.
"""
from __future__ import annotations

import hashlib
import json
import shutil
from collections.abc import Collection
from dataclasses import dataclass, field
from pathlib import Path

from PIL import Image

from .channel_assets import load_channel_logo
from .constants import LOGICAL_H, LOGICAL_W
from .errors import BuildError
from .openrouter import NoImageError, OpenRouterClient
from .palette import hex_to_rgb
from .paths import channel_dir
from .scene_prompt import PLAIN_FLAG, clean_visual, leaked_names, screen_content
from .template_pkg import TemplatePackage

DEFAULT_MODEL = "google/gemini-3.1-flash-image"
DEFAULT_CHECK_MODEL = "google/gemini-3.5-flash-lite"
MAX_REDRAWS = 2
MIN_PICTURE_PX = 256  # shortest side of a usable picture (the frame is 270 wide)


def _image_size(png: bytes) -> tuple[int, int] | None:
    import io

    try:
        with Image.open(io.BytesIO(png)) as img:
            img.verify()
        with Image.open(io.BytesIO(png)) as img:
            return img.size
    except Exception:  # noqa: BLE001 - any undecodable reply is unusable
        return None

CHECK_PROMPT = (
    "You check one illustration for a news video before it is published. Answer with JSON only, one boolean per "
    "key, true only for what you can actually see in the picture: {\"text\": any letters, words, numbers or "
    "text-like glyphs anywhere, \"logos\": any logo, brand mark or trademark, \"flags_or_seals\": a flag whose "
    "design tells which country, state or organization it belongs to (stars, stripes, a cross, an emblem, a pattern "
    "of colors), a coat of arms, a government seal or an official emblem -- a flag of one or two plain colors with "
    "no design on it is false, \"game_elements\": anything from a known video game (a \"?\" block, "
    "pipes, coins, power-ups, health bars, game characters), \"photorealistic\": it looks like a photograph rather "
    "than pixel art, \"gore\": blood, wounds, dead bodies or a moment of violence, \"extra_people\": a person, "
    "human figure, face or hand that the scene description below does not ask for (any person at all when it "
    "describes none)}. The scene description: "
)
FINDINGS = {
    "text": "Absolutely no text: every sign, screen, paper, label, calendar and badge is blank, with no letters, "
            "words, numbers or symbols.",
    "logos": "No logos or brand marks of any kind.",
    "flags_or_seals": "No flags of real countries, states or organizations and no flag the scene doesn't name, no "
                      "coats of arms, no seals, no emblems: plain shapes and plain colors only.",
    "game_elements": "Nothing from any video game: no blocks, pipes, coins, power-ups, health bars or game characters.",
    "photorealistic": "Strictly flat NES 8-bit pixel art with big visible pixels, not a photograph, not realistic.",
    "gore": "No blood, wounds, bodies or violence.",
    "extra_people": "Only the people the scene describes, and none at all if it describes none: no figures, faces "
                    "or hands beyond them.",
}
# Worth a redraw, not a neutral card: a stylized stranger is a QA remark, text or a seal is a rule broken.
SOFT_FINDINGS = {"extra_people"}
# The frame each aspect asks for, in the style's "{frame}".
FRAMES = {"9:16": "vertical 9:16", "4:5": "portrait 4:5", "1:1": "square 1:1", "5:4": "landscape 5:4"}


def load_style(channel_id: str) -> dict:
    with open(channel_dir(channel_id) / "images.json") as f:
        return json.load(f)


# Trial 3: a plain grey flag on a stand came back rolled up, a grey cone that QA and Legal read as a missile.
PLAIN_FLAG_LOOK = "The plain light grey flag is a simple cloth with no design; on a pole it hangs open, not rolled up."
SCREEN_PROMPT = ("This picture fills the TV screen behind a news anchor: show only what is on the screen, filling the "
                 "whole frame; no anchor, no desk, no studio, no TV frame around it.")


def scene_prompt(style: dict, template: TemplatePackage, visual: str, stronger: list[str] = (), serious: bool = False,
                 screen: bool = False, aspect: str = "9:16") -> str:
    """In serious mode the channel's serious look replaces the template's
    (nes-desk's "bright, saturated" day has no place in a tragedy); a
    `screen` picture is what an anchor shot shows behind the anchor."""
    parts = [style["style"].replace("{frame}", FRAMES[aspect])]
    if serious:
        parts.append(style["serious"])
    elif template.data.get("image_style"):
        parts.append(template.data["image_style"])
    if screen:
        parts.append(SCREEN_PROMPT)
    # the source and title plates sit at the top of a scene shot, and reach the top of the screen behind the anchor
    parts.append(style.get("top_room", ""))
    parts.append("Scene: " + _sentence(visual))
    if PLAIN_FLAG in visual:
        parts.append(PLAIN_FLAG_LOOK)
    parts.append(style["people"])
    parts.append(style["prohibitions"])
    parts.extend(stronger)
    return " ".join(p.strip() for p in parts if p.strip())


def _sentence(text: str) -> str:
    text = text.strip()
    return text if text.endswith((".", "?", "!")) else text + "."


def check_findings(answer: dict) -> list[str]:
    """What the check found; a key missing or not a boolean counts as found
    (a check that can't be read is not a pass)."""
    return [k for k in FINDINGS if answer.get(k) is not False]


def attempt_findings(attempts: list[dict]) -> list[str]:
    """Why none of a picture's attempts passed: what the checks found, a refusal, an unusable image."""
    return sorted({k for a in attempts for k in a.get("found", [])}
                  | {"refused" for a in attempts if "refused" in a}
                  | {"unusable image" for a in attempts if "unusable" in a})


@dataclass
class ImagesReport:
    model: str
    check_model: str
    scenes: list[dict] = field(default_factory=list)
    notes: list[str] = field(default_factory=list)

    @property
    def cost_usd(self) -> float:
        return round(sum(a.get("cost_usd", 0.0) for s in self.scenes for a in s["attempts"]), 4)

    def to_package(self) -> dict:
        return {"provider": "openrouter", "model": self.model, "check_model": self.check_model,
                "cost_usd": self.cost_usd, "scenes": self.scenes}


def fallback_card(template: TemplatePackage, logo: Image.Image, size: tuple[int, int] = (LOGICAL_W, LOGICAL_H),
                  scale: int = 2) -> Image.Image:
    """The neutral card that stands in for a picture no attempt passed: the
    channel logo on the template's darkest color."""
    colors = template.palette().hex_colors
    bg = min((hex_to_rgb(c) for c in colors), key=sum)
    card = Image.new("RGB", size, bg)
    scaled = logo.resize((logo.width * scale, logo.height * scale), Image.NEAREST)
    card.paste(scaled, ((size[0] - scaled.width) // 2, (size[1] - scaled.height) // 2), scaled)
    return card


def picture_jobs(script: dict) -> list[tuple[str, str, str]]:
    """(name, visual, kind) of every picture `generate_scene_images` draws, each
    written to images/<name>.png even when no attempt passes: every scene shot;
    the screen of an anchor shot whose `visual` says what it shows, except the
    last one, which shows the channel logo (engine._render_anchor_shot); a
    custom cover."""
    jobs = []
    for i, s in enumerate(script["scenes"]):
        if s["shot"] == "scene":
            jobs.append((s["id"], s["visual"], "scene"))
        elif any(later["shot"] == "scene" for later in script["scenes"][i + 1:]) and screen_content(s["visual"]):
            jobs.append((s["id"], screen_content(s["visual"]), "screen"))
    if script["cover"]["frame"] == "custom":
        jobs.append(("cover", script["cover"]["visual"], "scene"))
    return jobs


@dataclass
class EarlierPictures:
    """An earlier build of the video, for `--reuse-from`: the directory holding its package.json and
    images/, what the images report calls it, and its `images.scenes`."""
    dir: Path
    package_id: str
    scenes: list[dict]


def load_earlier_pictures(directory: Path, package_id: str) -> tuple[EarlierPictures | None, str | None]:
    """(what can be reused, None), or (None, why nothing can)."""
    try:
        scenes = json.loads((Path(directory) / "package.json").read_text())["images"]["scenes"]
        if not all(isinstance(e, dict) and isinstance(e.get("id"), str) for e in scenes):
            raise ValueError("a malformed images.scenes entry")
    except (OSError, ValueError, KeyError, TypeError) as e:
        return None, f"{type(e).__name__}: {e}"[:200]
    return EarlierPictures(Path(directory), package_id, scenes), None


def _first_prompt(entry: dict) -> str | None:
    """The prompt an earlier picture was first asked for with, before a redraw stated anything harder."""
    if isinstance(entry.get("prompt"), str):  # taken from a build before that one: it has no attempts of its own
        return entry["prompt"]
    attempts = entry.get("attempts")
    first = attempts[0] if isinstance(attempts, list) and attempts else None
    return first.get("prompt") if isinstance(first, dict) else None


def _earlier_matches(earlier: EarlierPictures | None, name: str, prompt: str, claimed: set[str]) -> list[dict]:
    """The earlier pictures drawn from this very `prompt`: the scene's own if its id still holds it, else
    any not yet taken (scene ids shift). A cover only ever matches a cover."""
    if earlier is None:
        return []
    same = [e for e in earlier.scenes if (e["id"] == "cover") == (name == "cover") and _first_prompt(e) == prompt]
    return [e for e in same if e["id"] == name] or [e for e in same if e["id"] not in claimed]


def _reusable_picture(entry: dict, earlier: EarlierPictures) -> tuple[bytes | None, str | None]:
    """(the picture's bytes, None), or (None, why it can't stand in this build). A record that can't be
    read is not a pass: only a picture that passed its check, or was kept with nothing but `SOFT_FINDINGS`,
    is taken, and only the very bytes that were checked."""
    if entry.get("fallback") is not False:
        return None, "it was a neutral card"
    accepted = entry.get("accepted")
    if not isinstance(accepted, str):
        return None, "no accepted picture on record"
    if "reused_from" not in entry:  # one taken from an even earlier build has its check on record there
        attempts = entry.get("attempts")
        attempt = next((a for a in attempts if isinstance(a, dict) and a.get("file") == accepted), None) \
            if isinstance(attempts, list) else None
        if attempt is None:
            return None, "its accepted attempt is not on record"
        check = attempt.get("check")
        found = set(check_findings(check if isinstance(check, dict) else {}))
        kept = entry.get("kept_with")
        if found - (SOFT_FINDINGS & set(kept if isinstance(kept, list) else [])):
            return None, f"its check found {', '.join(sorted(found))}"
    root = earlier.dir.resolve()
    final, source = (root / str(entry.get("file"))).resolve(), (root / accepted).resolve()
    if not (final.is_relative_to(root) and source.is_relative_to(root)):
        return None, "its file is outside the package"
    try:
        data = final.read_bytes()
        if data != source.read_bytes():
            return None, "its file is not the accepted attempt"
    except OSError:
        return None, "its file is missing"
    size = _image_size(data)
    if size is None or min(size) < MIN_PICTURE_PX:
        return None, f"its file is unusable: image {size}"
    return data, None


def _take_earlier(matches: list[dict], earlier: EarlierPictures | None) -> tuple[dict | None, bytes | None, str]:
    """The first of `matches` that can stand in and its bytes, else (None, None, why none can)."""
    why = "its prompt changed"
    for entry in matches:
        data, why = _reusable_picture(entry, earlier)
        if data is not None:
            return entry, data, ""
    return None, None, why


def _reused_entry(name: str, prompt: str, taken: dict, data: bytes, earlier: EarlierPictures, report: "ImagesReport") -> dict:
    """The images report's record of a picture taken from `earlier`: nothing attempted, nothing paid, and
    enough for the next rebuild to take it again or to redraw past it."""
    entry = {"id": name, "file": f"images/{name}.png", "fallback": False, "accepted": f"images/{name}.png",
             "prompt": prompt, "attempts": [],
             "reused_from": {"package": earlier.package_id, "scene": taken["id"],
                             "sha256": hashlib.sha256(data).hexdigest(), "variants_used": _variants_used(taken)}}
    if taken.get("kept_with"):
        entry["kept_with"] = taken["kept_with"]
        report.notes.append(f"{name}: the reused picture was kept in {earlier.package_id} with findings: "
                            f"{', '.join(str(k) for k in taken['kept_with'])}")
    return entry


def _variants_used(entry: dict) -> int:
    """How many generator variants of this prompt the earlier builds used: a redraw goes past them,
    since the provider's cache would hand back the very picture that was rejected."""
    reused = entry.get("reused_from")
    if isinstance(reused, dict) and isinstance(reused.get("variants_used"), int):
        return reused["variants_used"]
    attempts = entry.get("attempts")
    used = [a.get("variant", a.get("n")) for a in attempts if isinstance(a, dict)] if isinstance(attempts, list) else []
    return max((v for v in used if isinstance(v, int)), default=-1) + 1


def generate_scene_images(
    script: dict,
    template: TemplatePackage,
    channel_id: str,
    out_dir: Path,
    client: OpenRouterClient,
    model: str = DEFAULT_MODEL,
    check_model: str = DEFAULT_CHECK_MODEL,
    earlier: EarlierPictures | None = None,
    redraw: Collection[str] = (),
) -> tuple[Path, ImagesReport]:
    """Writes <out>/images/<scene>.png for every scene shot (and cover.png for
    a custom cover) -- the directory `build` then reads scene art from -- plus
    every attempt under images/attempts/. Returns that directory and the
    report for package.json. `earlier`: a package whose accepted pictures
    stand in where nothing that feeds them changed; `redraw` (`--redraw`):
    picture names, in this script or the earlier package, never taken from it."""
    style = load_style(channel_id)
    images_dir = Path(out_dir) / "images"
    if images_dir.exists():
        shutil.rmtree(images_dir)
    (images_dir / "attempts").mkdir(parents=True)
    report = ImagesReport(model, check_model)
    jobs = picture_jobs(script)
    logo = load_channel_logo(channel_id)
    serious = bool(script.get("serious_mode"))
    # Every scene is cleaned of everyone in the script: `real_people` lists
    # who a scene shows, and a scene can still mention someone else.
    everyone = sorted({p for s in script["scenes"] for p in s.get("real_people", [])})
    screen = template.data["studio"]["screen"]
    screen_aspect = "4:5" if screen["w"] / screen["h"] <= 0.9 else ("1:1" if screen["w"] / screen["h"] <= 1.15 else "5:4")
    claimed: set[str] = set()
    reused_names: list[str] = []
    drawn_again: list[str] = []
    for name, visual, kind in jobs:
        cleaned = clean_visual(visual, everyone)
        # clean_visual's job; this is the last line before the network. Only the scene text comes from the
        # script: the fixed wording around it ("power-ups", "hands", "gore") would trip on Samantha Power.
        leaked = leaked_names(cleaned, everyone)
        if leaked:
            raise BuildError(f"{name}: the image prompt still names {', '.join(leaked)}; not sent")
        stronger: list[str] = []
        attempts: list[dict] = []
        accepted = None
        aspect = screen_aspect if kind == "screen" else "9:16"
        first_prompt = scene_prompt(style, template, cleaned, stronger, serious, screen=kind == "screen", aspect=aspect)
        matches = _earlier_matches(earlier, name, first_prompt, claimed)
        requested = name in redraw or any(e["id"] in redraw for e in matches)
        taken, data, why_not = (None, None, "named in --redraw") if requested else _take_earlier(matches, earlier)
        if taken is not None:
            (images_dir / f"{name}.png").write_bytes(data)
            claimed.add(taken["id"])
            reused_names.append(name)
            report.scenes.append(_reused_entry(name, first_prompt, taken, data, earlier, report))
            continue
        if earlier is not None:
            drawn_again.append(f"{name} ({why_not})")
        # 0 for a first build, as ever. A prompt asked before starts past what was asked: past MAX_REDRAWS when
        # the earlier record is gone, so a redraw never gets the rejected picture back from the provider's cache.
        first_variant = max(map(_variants_used, matches)) if matches else (1 + MAX_REDRAWS if requested else 0)
        for n in range(1 + MAX_REDRAWS):
            prompt = scene_prompt(style, template, cleaned, stronger, serious, screen=kind == "screen", aspect=aspect)
            variant = {"variant": first_variant + n} if first_variant else {}
            try:
                reply = client.generate_image(model, prompt, aspect, variant=first_variant + n)
            except NoImageError as e:
                attempts.append({"n": n, **variant, "prompt": prompt, "refused": str(e)[:300], "cost_usd": round(e.cost_usd, 4)})
                continue
            rel = f"images/attempts/{name}-{n}.png"
            (Path(out_dir) / rel).write_bytes(reply.png)
            size = _image_size(reply.png)
            if size is None or min(size) < MIN_PICTURE_PX:
                attempts.append({"n": n, **variant, "prompt": prompt, "file": rel, "unusable": f"image {size}",
                                 "cost_usd": round(reply.cost_usd, 4)})
                continue
            check = client.ask_json(check_model, CHECK_PROMPT + _sentence(cleaned), (reply.png,))
            found = check_findings(check.data)
            attempts.append({"n": n, **variant, "prompt": prompt, "file": rel, "check": check.data, "found": found,
                             "cost_usd": round(reply.cost_usd + check.cost_usd, 4)})
            if not found:
                accepted = rel
                break
            stronger = list(dict.fromkeys(stronger + [FINDINGS[k] for k in found]))
        target = images_dir / f"{name}.png"
        entry = {"id": name, "file": f"images/{name}.png"}
        if requested:
            entry["redraw_requested"] = True
        if not accepted:
            what = f"{name} screen behind the anchor" if kind == "screen" else name
            found_all = attempt_findings(attempts)
            failed = f"{what}: no generated picture passed the check after {len(attempts)} attempt(s) ({', '.join(found_all)})"
            soft = next((a for a in attempts if a.get("found") and set(a["found"]) <= SOFT_FINDINGS), None)
            if soft:
                accepted = soft["file"]
                entry["kept_with"] = soft["found"]
                report.notes.append(f"{failed}; {accepted} is kept: its only finding is people the scene doesn't "
                                    f"describe -- see images/attempts/")
            else:
                report.notes.append(f"{failed}; a neutral card with the logo stands in -- see images/attempts/")
        if accepted:
            shutil.copyfile(Path(out_dir) / accepted, target)
        elif kind == "screen":  # sized to the screen, so the logo isn't cropped down to a few pixels
            fallback_card(template, logo, (screen["w"], screen["h"]), scale=1).save(target)
        else:
            fallback_card(template, logo).save(target)
        report.scenes.append({**entry, "fallback": accepted is None, "accepted": accepted, "attempts": attempts})
    if earlier is not None:
        report.notes.append(f"{len(reused_names)} of {len(jobs)} picture(s) reused from {earlier.package_id}, unchanged, "
                            f"without a new check or cost: {', '.join(reused_names) or 'none'}; drawn again: "
                            f"{', '.join(drawn_again) or 'none'}")
    return images_dir, report
