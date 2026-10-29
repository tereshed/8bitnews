"""Scene pictures from the image provider (`studio build --images openrouter`).

For every `shot: scene` (and a `custom` cover): the prompt (`scene_prompt`)
-> the image model -> a vision check of the picture (JSON: text, logos,
flags or seals, game elements, photorealism, gore). A picture that fails is
drawn again with the prohibition it broke stated harder, at most twice; if
none passes, a neutral card (the channel logo on the template's colors)
stands in and the package's notes say so, for QA and Legal. Every picture
drawn, passed or not, and its check are kept in the package (`images/`).
The frame engine snaps the accepted picture to the 270x480 grid and the
template's NES palette like any scene asset.
"""
from __future__ import annotations

import json
import shutil
from dataclasses import dataclass, field
from pathlib import Path

from PIL import Image

from .channel_assets import load_channel_logo
from .constants import LOGICAL_H, LOGICAL_W
from .errors import BuildError
from .openrouter import NoImageError, OpenRouterClient
from .palette import hex_to_rgb
from .paths import channel_dir
from .scene_prompt import clean_visual, leaked_names, screen_content
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
    "text-like glyphs anywhere, \"logos\": any logo, brand mark or trademark, \"flags_or_seals\": the flag of a real "
    "country, state or organization (a plain one-color flag is fine), a coat of arms, government seal or official "
    "emblem, \"game_elements\": anything from a known video game (a \"?\" block, "
    "pipes, coins, power-ups, health bars, game characters), \"photorealistic\": it looks like a photograph rather "
    "than pixel art, \"gore\": blood, wounds, dead bodies or a moment of violence}."
)
FINDINGS = {
    "text": "Absolutely no text: every sign, screen, paper, label, calendar and badge is blank, with no letters, "
            "words, numbers or symbols.",
    "logos": "No logos or brand marks of any kind.",
    "flags_or_seals": "No flags of real countries, states or organizations (a plain one-color flag at most), no "
                      "coats of arms, no seals, no emblems: plain shapes and plain colors only.",
    "game_elements": "Nothing from any video game: no blocks, pipes, coins, power-ups, health bars or game characters.",
    "photorealistic": "Strictly flat NES 8-bit pixel art with big visible pixels, not a photograph, not realistic.",
    "gore": "No blood, wounds, bodies or violence.",
}


def load_style(channel_id: str) -> dict:
    with open(channel_dir(channel_id) / "images.json") as f:
        return json.load(f)


SCREEN_PROMPT = ("This picture fills the TV screen behind a news anchor: show only what is on the screen, filling the "
                 "whole frame; no anchor, no desk, no studio, no TV frame around it.")


def scene_prompt(style: dict, template: TemplatePackage, visual: str, stronger: list[str] = (), serious: bool = False,
                 screen: bool = False) -> str:
    """In serious mode the channel's serious look replaces the template's
    (nes-desk's "bright, saturated" day has no place in a tragedy); a
    `screen` picture is what an anchor shot shows behind the anchor."""
    parts = [style["style"]]
    if serious:
        parts.append(style["serious"])
    elif template.data.get("image_style"):
        parts.append(template.data["image_style"])
    if screen:
        parts.append(SCREEN_PROMPT)
    parts.append("Scene: " + visual)
    parts.append(style["people"])
    parts.append(style["prohibitions"])
    parts.extend(stronger)
    return " ".join(p.strip() for p in parts if p.strip())


def check_findings(answer: dict) -> list[str]:
    """What the check found; a key missing or not a boolean counts as found
    (a check that can't be read is not a pass)."""
    return [k for k in FINDINGS if answer.get(k) is not False]


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


def generate_scene_images(
    script: dict,
    template: TemplatePackage,
    channel_id: str,
    out_dir: Path,
    client: OpenRouterClient,
    model: str = DEFAULT_MODEL,
    check_model: str = DEFAULT_CHECK_MODEL,
) -> tuple[Path, ImagesReport]:
    """Writes <out>/images/<scene>.png for every scene shot (and cover.png for
    a custom cover) -- the directory `build` then reads scene art from -- plus
    every attempt under images/attempts/. Returns that directory and the
    report for package.json."""
    style = load_style(channel_id)
    images_dir = Path(out_dir) / "images"
    if images_dir.exists():
        shutil.rmtree(images_dir)
    (images_dir / "attempts").mkdir(parents=True)
    report = ImagesReport(model, check_model)
    # (name, visual, kind): every scene shot; the screen of an anchor shot whose `visual` says what it shows,
    # except the last one, which shows the channel logo (engine._render_anchor_shot); a custom cover.
    jobs = []
    for i, s in enumerate(script["scenes"]):
        if s["shot"] == "scene":
            jobs.append((s["id"], s["visual"], "scene"))
        elif any(later["shot"] == "scene" for later in script["scenes"][i + 1:]) and screen_content(s["visual"]):
            jobs.append((s["id"], screen_content(s["visual"]), "screen"))
    if script["cover"]["frame"] == "custom":
        jobs.append(("cover", script["cover"]["visual"], "scene"))
    logo = load_channel_logo(channel_id)
    serious = bool(script.get("serious_mode"))
    # Every scene is cleaned of everyone in the script: `real_people` lists
    # who a scene shows, and a scene can still mention someone else.
    everyone = sorted({p for s in script["scenes"] for p in s.get("real_people", [])})
    screen = template.data["studio"]["screen"]
    screen_aspect = "4:5" if screen["w"] / screen["h"] <= 0.9 else ("1:1" if screen["w"] / screen["h"] <= 1.15 else "5:4")
    for name, visual, kind in jobs:
        cleaned = clean_visual(visual, everyone)
        stronger: list[str] = []
        attempts: list[dict] = []
        accepted = None
        for n in range(1 + MAX_REDRAWS):
            prompt = scene_prompt(style, template, cleaned, stronger, serious, screen=kind == "screen")
            leaked = leaked_names(prompt, everyone)
            if leaked:  # clean_visual's job; this is the last line before the network
                raise BuildError(f"{name}: the image prompt still names {', '.join(leaked)}; not sent")
            try:
                reply = client.generate_image(model, prompt, screen_aspect if kind == "screen" else "9:16", variant=n)
            except NoImageError as e:
                attempts.append({"n": n, "prompt": prompt, "refused": str(e)[:300], "cost_usd": round(e.cost_usd, 4)})
                continue
            rel = f"images/attempts/{name}-{n}.png"
            (Path(out_dir) / rel).write_bytes(reply.png)
            size = _image_size(reply.png)
            if size is None or min(size) < MIN_PICTURE_PX:
                attempts.append({"n": n, "prompt": prompt, "file": rel, "unusable": f"image {size}",
                                 "cost_usd": round(reply.cost_usd, 4)})
                continue
            check = client.ask_json(check_model, CHECK_PROMPT, (reply.png,))
            found = check_findings(check.data)
            attempts.append({"n": n, "prompt": prompt, "file": rel, "check": check.data, "found": found,
                             "cost_usd": round(reply.cost_usd + check.cost_usd, 4)})
            if not found:
                accepted = rel
                break
            stronger = list(dict.fromkeys(stronger + [FINDINGS[k] for k in found]))
        target = images_dir / f"{name}.png"
        if accepted:
            shutil.copyfile(Path(out_dir) / accepted, target)
        else:
            if kind == "screen":  # sized to the screen, so the logo isn't cropped down to a few pixels
                fallback_card(template, logo, (screen["w"], screen["h"]), scale=1).save(target)
            else:
                fallback_card(template, logo).save(target)
            found_all = sorted({k for a in attempts for k in a.get("found", [])}
                               | {"refused" for a in attempts if "refused" in a}
                               | {"unusable image" for a in attempts if "unusable" in a})
            what = f"{name} screen behind the anchor" if kind == "screen" else name
            report.notes.append(
                f"{what}: no generated picture passed the check after {len(attempts)} attempt(s) "
                f"({', '.join(found_all)}); a neutral card with the logo stands in -- see images/attempts/")
        report.scenes.append({"id": name, "file": f"images/{name}.png", "fallback": accepted is None,
                              "accepted": accepted, "attempts": attempts})
    return images_dir, report
