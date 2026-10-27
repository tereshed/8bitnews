"""The deterministic frame engine: given a script, a timeline and a
template package, render_frame produces one logical 270x480 RGB frame.
Same inputs -> same output bytes every time (no randomness, no wall-clock
reads; every visual choice is a pure function of (script, timeline,
template, channel_assets, t_abs))."""
from __future__ import annotations

from pathlib import Path

from PIL import Image, ImageDraw

from .channel_assets import ChannelAssets
from .constants import AI_VOICE_PLATE_S, FONT_PX, LOGICAL_H, LOGICAL_W
from .palette import Palette, channel_palette_colors, hex_to_rgb, quantize_image
from .quantize import quantize
from .subtitles import wrap_words
from .template_pkg import TemplatePackage
from .text_render import draw_text, text_size
from .timeline import SceneTiming, Timeline


def _scene_data(script: dict, scene_id: str) -> dict:
    for scene in script["scenes"]:
        if scene["id"] == scene_id:
            return scene
    raise KeyError(f"script has no scene {scene_id!r}")


def _get_preset(template: TemplatePackage, preset_type: str) -> dict | None:
    for preset in template.data.get("motion_presets", []):
        if preset["type"] == preset_type:
            return preset
    return None


def _scene_image_path(assets_dir: Path | None, scene_id: str) -> Path | None:
    if assets_dir is None:
        return None
    path = Path(assets_dir) / f"{scene_id}.png"
    return path if path.is_file() else None


def _draw_placeholder_scene(
    scene_id: str, visual: str, palette_colors: list[str], font
) -> Image.Image:
    """No asset file for this scene: a flat palette-colored card. The debug
    label and wrapped `visual` text sit in the lower half of the frame so
    they don't collide with a template's top-area plates (AI voice/icon,
    scene title, source)."""
    idx = sum(ord(c) for c in scene_id) % len(palette_colors)
    bg = hex_to_rgb(palette_colors[idx])
    brightness = 0.299 * bg[0] + 0.587 * bg[1] + 0.114 * bg[2]
    fg = (0, 0, 0) if brightness > 140 else (252, 252, 252)
    img = Image.new("RGBA", (LOGICAL_W, LOGICAL_H), (*bg, 255))
    text_top = LOGICAL_H // 2
    draw_text(img, (12, text_top), scene_id, font, fg)
    lines = wrap_words(visual.split(), 30)[:6]
    for i, line in enumerate(lines):
        draw_text(img, (12, text_top + (i + 1) * (FONT_PX + 4)), line, font, fg)
    return img


def _zoom_crop(img: Image.Image, progress: float, max_px: int) -> Image.Image:
    """Integer-pixel zoom ('зум целыми пикселями'): crop a shrinking border
    and scale back up with NEAREST, so no new colors are introduced and the
    pixel grid stays crisp."""
    step = int(round(max_px * max(0.0, min(progress, 1.0))))
    if step <= 0:
        return img
    step_y = max(1, round(step * LOGICAL_H / LOGICAL_W))
    box = (step, step_y, LOGICAL_W - step, LOGICAL_H - step_y)
    if box[2] <= box[0] or box[3] <= box[1]:
        return img
    return img.crop(box).resize((LOGICAL_W, LOGICAL_H), Image.NEAREST)


def _render_scene_shot(
    script: dict,
    scene: dict,
    scene_timing: SceneTiming,
    template: TemplatePackage,
    t_abs: float,
    assets_dir: Path | None,
    master_palette_colors: list[str],
    font,
) -> Image.Image:
    path = _scene_image_path(assets_dir, scene["id"])
    if path is not None:
        frame = quantize(Image.open(path), Palette(master_palette_colors)).convert("RGBA")
    else:
        frame = _draw_placeholder_scene(scene["id"], scene["visual"], master_palette_colors, font)

    zoom = _get_preset(template, "zoom")
    if zoom is not None:
        elapsed = t_abs - scene_timing.start_s
        duration = zoom["params"].get("duration_s", scene_timing.duration_s)
        progress = elapsed / duration if duration > 0 else 0.0
        frame = _zoom_crop(frame, progress, zoom["params"].get("max_px", 0))
    return frame


def _compose_anchor_sprite(assets: ChannelAssets, pose: str, eyes_state: str, mouth_level: int) -> Image.Image:
    anchor = assets.anchor
    canvas = Image.new("RGBA", anchor.canvas, (0, 0, 0, 0))
    canvas.alpha_composite(anchor.bodies[pose])
    canvas.alpha_composite(anchor.eyes[eyes_state], anchor.eyes_offset())
    canvas.alpha_composite(anchor.mouths[str(mouth_level)], anchor.mouth_offset())
    return canvas


def _render_anchor_shot(
    script: dict,
    scene: dict,
    scene_timing: SceneTiming,
    timeline: Timeline,
    template: TemplatePackage,
    assets: ChannelAssets,
    t_abs: float,
    assets_dir: Path | None,
    master_palette_colors: list[str],
    font,
) -> Image.Image:
    frame = Image.new("RGBA", (LOGICAL_W, LOGICAL_H), (0, 0, 0, 255))
    for layer in template.data["studio"]["layers"]:
        img = template.load_asset_image(layer["file"])
        frame.alpha_composite(img, (layer["x"], layer["y"]))

    screen = template.data["studio"]["screen"]
    screen_scene = _next_scene_shot(script, scene["id"])
    if screen_scene is not None:
        screen_path = _scene_image_path(assets_dir, screen_scene["id"])
        if screen_path is not None:
            content = quantize(Image.open(screen_path), Palette(master_palette_colors))
        else:
            content = _draw_placeholder_scene(
                screen_scene["id"], screen_scene["visual"], master_palette_colors, font
            ).convert("RGB")
        content = content.resize((screen["w"], screen["h"]), Image.NEAREST)
    else:
        content = assets.logo.resize((screen["w"], screen["h"]), Image.NEAREST)
    frame.paste(content.convert("RGBA"), (screen["x"], screen["y"]))

    serious = bool(script.get("serious_mode"))
    pose = "serious" if serious else "normal"
    eyes_state = "closed" if assets.anchor.is_blinking(t_abs) else "open"
    volume = scene_timing.volume_at(t_abs)
    mouth_level = assets.anchor.mouth_level_for_volume(volume)
    sprite = _compose_anchor_sprite(assets, pose, eyes_state, mouth_level)

    anchor_cfg = template.data["studio"]["anchor"]
    scale = anchor_cfg["scale"]
    scaled = sprite.resize(
        (round(sprite.width * scale), round(sprite.height * scale)), Image.NEAREST
    )
    frame.alpha_composite(scaled, (anchor_cfg["x"], anchor_cfg["y"]))
    return frame


def _next_scene_shot(script: dict, after_scene_id: str) -> dict | None:
    scenes = script["scenes"]
    idx = next(i for i, s in enumerate(scenes) if s["id"] == after_scene_id)
    for s in scenes[idx + 1 :]:
        if s["shot"] == "scene":
            return s
    return None


def _max_chars_for_rect(rect_w: int, pad: int = 2) -> int:
    """How many FONT_PX-wide monospace glyphs fit on one line of a plate
    rect, after padding on both sides."""
    return max(1, (rect_w - 2 * pad) // FONT_PX)


def _draw_plate(frame: Image.Image, plate_cfg: dict, lines: list[str], font) -> None:
    rect = plate_cfg["rect"]
    bg = hex_to_rgb(plate_cfg["bg_color"])
    fg = hex_to_rgb(plate_cfg["text_color"])
    draw = ImageDraw.Draw(frame)
    draw.rectangle(
        [rect["x"], rect["y"], rect["x"] + rect["w"] - 1, rect["y"] + rect["h"] - 1],
        fill=(*bg, 255),
    )
    pad = 2
    y = rect["y"] + pad
    for line in lines:
        draw_text(frame, (rect["x"] + pad, y), line, font, fg)
        y += FONT_PX + 2


def active_plates(script: dict, scene_timing: SceneTiming, template: TemplatePackage, t_abs: float) -> set[str]:
    """Which named plates (docs: AI voice, AI icon, AI illustration, scene
    title, source plate) should be visible at `t_abs`. A pure function of
    its inputs so timing rules (format.md: AI voice plate for the video's
    first 3s, then a persistent icon; AI illustration while real_people is
    non-empty) can be tested without rendering any pixels.
    """
    scene = _scene_data(script, scene_timing.scene_id)
    plates: set[str] = set()

    element_delay = _get_preset(template, "element_appear")
    title_ready = True
    if element_delay is not None and element_delay["params"].get("element") == "scene_title":
        title_ready = (t_abs - scene_timing.start_s) >= element_delay["params"].get("delay_s", 0.0)

    if title_ready and scene.get("on_screen_text"):
        plates.add("scene_title")
    if scene.get("source_plate"):
        plates.add("source_plate")
    if scene.get("real_people"):
        plates.add("ai_illustration")
    plates.add("ai_voice" if t_abs < AI_VOICE_PLATE_S else "ai_icon")
    return plates


def render_frame(
    script: dict,
    timeline: Timeline,
    template: TemplatePackage,
    assets: ChannelAssets,
    t_abs: float,
    assets_dir: Path | None = None,
) -> Image.Image:
    scene_timing = timeline.scene_at(t_abs)
    if scene_timing is None:
        return Image.new("RGB", (LOGICAL_W, LOGICAL_H), (0, 0, 0))
    scene = _scene_data(script, scene_timing.scene_id)
    serious = bool(script.get("serious_mode"))
    full_palette_colors = channel_palette_colors(assets.channel_id)
    font = assets.font(FONT_PX)

    if scene["shot"] == "anchor":
        frame = _render_anchor_shot(
            script, scene, scene_timing, timeline, template, assets, t_abs, assets_dir, full_palette_colors, font
        )
    else:
        frame = _render_scene_shot(
            script, scene, scene_timing, template, t_abs, assets_dir, full_palette_colors, font
        ).convert("RGBA")

    plates = active_plates(script, scene_timing, template, t_abs)

    if "scene_title" in plates:
        title_cfg = template.plate("scene_title")
        max_chars = _max_chars_for_rect(title_cfg["rect"]["w"])
        lines = wrap_words(scene["on_screen_text"].split(), max_chars)[:2]
        _draw_plate(frame, title_cfg, lines, font)

    if "source_plate" in plates:
        source_cfg = template.plate("source_plate")
        max_chars = _max_chars_for_rect(source_cfg["rect"]["w"])
        lines = wrap_words(scene["source_plate"].split(), max_chars)[:2]
        _draw_plate(frame, source_cfg, lines, font)

    if "ai_illustration" in plates:
        _draw_plate(frame, template.plate("ai_illustration"), ["AI illustration"], font)

    if "ai_voice" in plates:
        _draw_plate(frame, template.plate("ai_voice"), ["AI · AI voice"], font)
    else:
        _draw_plate(frame, template.plate("ai_icon"), ["AI"], font)

    cue = next((c for c in scene_timing.subtitles if c.start_s <= t_abs < c.end_s), None)
    if cue is not None:
        sub = template.data["subtitles"]
        rect = sub["rect"]
        style = sub["style"]
        fg = hex_to_rgb(style["color"])
        outline = hex_to_rgb(style["outline_color"])
        n_lines = len(cue.lines)
        total_h = n_lines * (FONT_PX + 2)
        y = rect["y"] + rect["h"] - total_h
        for line in cue.lines:
            w, _h = text_size(line, font)
            x = rect["x"] + max(0, (rect["w"] - w) // 2)
            draw_text(frame, (x, y), line, font, fg, outline=outline)
            y += FONT_PX + 2

    if serious:
        frame = quantize_image(frame, template.palette(serious=True))
    return frame.convert("RGB")
