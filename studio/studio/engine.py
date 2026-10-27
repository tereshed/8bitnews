"""The deterministic frame engine: given a script, a timeline and a
template package, render_frame produces one logical 270x480 RGB frame.
Same inputs -> same output bytes every time (no randomness, no wall-clock
reads; every visual choice is a pure function of (script, timeline,
template, channel_assets, t_abs))."""
from __future__ import annotations

from functools import lru_cache
from pathlib import Path

from PIL import Image, ImageDraw

from .channel_assets import ChannelAssets
from .constants import (
    AI_VOICE_PLATE_S,
    FONT_PX,
    FPS,
    LOGICAL_H,
    LOGICAL_W,
    MAX_SUBTITLE_CHARS_PER_LINE,
    MAX_SUBTITLE_LINES,
)
from .palette import Palette, hex_to_rgb, quantize_image
from .quantize import quantize
from .subtitles import wrap_words
from .template_pkg import TemplatePackage
from .text_render import draw_text, text_size
from .timeline import SceneTiming, Timeline

_SCENE_MOTION_TYPES = ("pan", "zoom")


def _scene_data(script: dict, scene_id: str) -> dict:
    for scene in script["scenes"]:
        if scene["id"] == scene_id:
            return scene
    raise KeyError(f"script has no scene {scene_id!r}")


def _scene_index(script: dict, scene_id: str) -> int:
    return next(i for i, s in enumerate(script["scenes"]) if s["id"] == scene_id)


def _get_preset(template: TemplatePackage, preset_type: str) -> dict | None:
    for preset in template.data.get("motion_presets", []):
        if preset["type"] == preset_type:
            return preset
    return None


def _scene_motion_presets(template: TemplatePackage) -> list[dict]:
    return [p for p in template.data.get("motion_presets", []) if p["type"] in _SCENE_MOTION_TYPES]


def _select_scene_motion(template: TemplatePackage, scene_index: int) -> dict | None:
    """Deterministic, round-robin pick of a `shot: scene` motion preset by
    the scene's ordinal position in the script ('по порядковому номеру
    сцены по кругу'). `studio template validate` requires at least one
    pan/zoom preset to exist, so templates can't ship with silent no-motion
    scenes; this still tolerates an empty list defensively."""
    presets = _scene_motion_presets(template)
    if not presets:
        return None
    return presets[scene_index % len(presets)]


def _scene_image_path(assets_dir: Path | None, scene_id: str) -> Path | None:
    if assets_dir is None:
        return None
    path = Path(assets_dir) / f"{scene_id}.png"
    return path if path.is_file() else None


def _draw_placeholder_scene(
    scene_id: str, visual: str, palette_colors: list[str], font, w: int = LOGICAL_W, h: int = LOGICAL_H
) -> Image.Image:
    """No asset file for this scene: a flat palette-colored card. The debug
    label and wrapped `visual` text sit in the lower half of the frame so
    they don't collide with a template's top-area plates (AI voice/icon,
    scene title, source)."""
    idx = sum(ord(c) for c in scene_id) % len(palette_colors)
    bg = hex_to_rgb(palette_colors[idx])
    brightness = 0.299 * bg[0] + 0.587 * bg[1] + 0.114 * bg[2]
    fg = (0, 0, 0) if brightness > 140 else (252, 252, 252)
    img = Image.new("RGBA", (w, h), (*bg, 255))
    text_top = h // 2
    draw_text(img, (12, text_top), scene_id, font, fg)
    lines = wrap_words(visual.split(), max(1, (w - 24) // FONT_PX))[:6]
    for i, line in enumerate(lines):
        draw_text(img, (12, text_top + (i + 1) * (FONT_PX + 4)), line, font, fg)
    return img


@lru_cache(maxsize=64)
def _quantize_asset_cached(path_str: str, palette_colors: tuple[str, ...], w: int, h: int) -> Image.Image:
    """Quantizing a real scene asset (e.g. a 1024x1792 generated image) is the
    expensive step (~0.17s observed); every frame of a scene re-requests the
    exact same (path, palette, size) while the scene plays, so caching it by
    those pure inputs is safe and cuts render time without affecting
    determinism. Callers never mutate the returned Image in place (they only
    .crop/.convert/.paste it, all of which return new images)."""
    return quantize(Image.open(path_str), Palette(palette_colors), target_w=w, target_h=h).convert("RGBA")


def _render_scene_base(
    scene: dict, assets_dir: Path | None, palette: Palette, font, w: int, h: int
) -> Image.Image:
    """Renders scene["id"]'s image (or a placeholder) straight at the
    requested logical size, already snapped to `palette`. Used both for the
    main `shot: scene` frame and for the anchor's over-the-shoulder screen,
    and -- at oversized `w`/`h` -- as the source for pan/zoom motion, so
    panning/zooming is a pure crop with no secondary resize."""
    path = _scene_image_path(assets_dir, scene["id"])
    if path is not None:
        return _quantize_asset_cached(str(path), tuple(palette.hex_colors), w, h)
    placeholder = _draw_placeholder_scene(scene["id"], scene["visual"], palette.hex_colors, font, w, h)
    # The placeholder's debug label is drawn in a fixed white/black (see
    # _draw_placeholder_scene) that isn't necessarily in a template's own
    # palette subset (e.g. night-report uses #f8f8f8, not #fcfcfc) -- snap it
    # too, so a no-asset scene is still fully within `palette`.
    return quantize_image(placeholder, palette)


def _render_scene_shot(
    script: dict,
    scene: dict,
    scene_timing: SceneTiming,
    template: TemplatePackage,
    t_abs: float,
    assets_dir: Path | None,
    palette: Palette,
    font,
) -> Image.Image:
    motion = _select_scene_motion(template, _scene_index(script, scene["id"]))
    if motion is None:
        return _render_scene_base(scene, assets_dir, palette, font, LOGICAL_W, LOGICAL_H)

    duration = motion["params"].get("duration_s", scene_timing.duration_s)
    elapsed = t_abs - scene_timing.start_s
    progress = max(0.0, min(elapsed / duration, 1.0)) if duration > 0 else 0.0

    if motion["type"] == "pan":
        dx = max(0, int(motion["params"].get("dx_px", 0)))
        dy = max(0, int(motion["params"].get("dy_px", 0)))
        if dx == 0 and dy == 0:
            return _render_scene_base(scene, assets_dir, palette, font, LOGICAL_W, LOGICAL_H)
        big = _render_scene_base(scene, assets_dir, palette, font, LOGICAL_W + dx, LOGICAL_H + dy)
        ox, oy = round(dx * progress), round(dy * progress)
        return big.crop((ox, oy, ox + LOGICAL_W, oy + LOGICAL_H))

    # "zoom": целыми кратностями, без нецелого ресайза (see README "Motion") --
    # the first half of the preset's duration shows the plain frame, the second
    # half swaps in an N-times-oversized render (N picked from max_px, an
    # integer >= 2) and crops the same LOGICAL_W x LOGICAL_H window out of its
    # center. Only Image.crop is used: no resize, so every pixel of the
    # result is already an exact NES-grid pixel.
    if progress < 0.5:
        return _render_scene_base(scene, assets_dir, palette, font, LOGICAL_W, LOGICAL_H)
    max_px = motion["params"].get("max_px", 0)
    factor = 3 if max_px > LOGICAL_W / 6 else 2
    big = _render_scene_base(scene, assets_dir, palette, font, LOGICAL_W * factor, LOGICAL_H * factor)
    cx, cy = (LOGICAL_W * factor) // 2, (LOGICAL_H * factor) // 2
    box = (cx - LOGICAL_W // 2, cy - LOGICAL_H // 2, cx + LOGICAL_W // 2, cy + LOGICAL_H // 2)
    return big.crop(box)


def _compose_anchor_sprite(assets: ChannelAssets, pose: str, eyes_state: str, mouth_level: int) -> Image.Image:
    anchor = assets.anchor
    canvas = Image.new("RGBA", anchor.canvas, (0, 0, 0, 0))
    canvas.alpha_composite(anchor.bodies[pose])
    canvas.alpha_composite(anchor.eyes[eyes_state], anchor.eyes_offset())
    canvas.alpha_composite(anchor.mouths[str(mouth_level)], anchor.mouth_offset())
    return canvas


def _parallax_shift(template: TemplatePackage, layer_index: int, scene_timing: SceneTiming, t_abs: float) -> int:
    """Integer-pixel horizontal drift for a studio background layer while an
    anchor shot plays (schema `studio.layers[].parallax` x the preset's
    `shift_px`). The drift is a triangle wave that is exactly 0 at both ends
    of the scene, so consecutive anchor shots never jump at the cut."""
    preset = _get_preset(template, "parallax")
    if preset is None or layer_index not in set(preset["params"].get("layer_indices", [])):
        return 0
    shift_px = preset["params"].get("shift_px", 6)
    duration = scene_timing.duration_s
    progress = max(0.0, min((t_abs - scene_timing.start_s) / duration, 1.0)) if duration > 0 else 0.0
    wave = 1.0 - abs(progress - 0.5) * 2.0
    layer = template.data["studio"]["layers"][layer_index]
    return round(layer.get("parallax", 0.0) * shift_px * wave)


def _render_anchor_shot(
    script: dict,
    scene: dict,
    scene_timing: SceneTiming,
    template: TemplatePackage,
    assets: ChannelAssets,
    t_abs: float,
    assets_dir: Path | None,
    palette: Palette,
    font,
) -> Image.Image:
    frame = Image.new("RGBA", (LOGICAL_W, LOGICAL_H), (0, 0, 0, 255))
    for i, layer in enumerate(template.data["studio"]["layers"]):
        img = quantize_image(template.load_asset_image(layer["file"]), palette)
        dx = _parallax_shift(template, i, scene_timing, t_abs)
        frame.alpha_composite(img, (layer["x"] + dx, layer["y"]))

    screen = template.data["studio"]["screen"]
    screen_scene = _next_scene_shot(script, scene["id"])
    if screen_scene is not None:
        content = _render_scene_base(screen_scene, assets_dir, palette, font, screen["w"], screen["h"])
    else:
        content = assets.logo.resize((screen["w"], screen["h"]), Image.NEAREST)
    frame.paste(content.convert("RGBA"), (screen["x"], screen["y"]))

    # The anchor sprite's own pixels are never run through palette
    # quantization (channel master-palette colors already; the review found
    # that re-quantizing the whole frame to a template's restricted subset,
    # as the old serious-mode pass did, turns skin tones grey/white).
    serious = bool(script.get("serious_mode"))
    pose = "serious" if serious else "normal"
    eyes_state = "closed" if assets.anchor.is_blinking(t_abs) else "open"
    volume = scene_timing.volume_at(t_abs)
    mouth_level = assets.anchor.mouth_level_for_volume(volume)
    sprite = _compose_anchor_sprite(assets, pose, eyes_state, mouth_level)

    anchor_cfg = template.data["studio"]["anchor"]
    scale = anchor_cfg["scale"]  # schema-enforced integer >= 1: exact pixel replication, no fractional resize
    scaled = sprite.resize((sprite.width * scale, sprite.height * scale), Image.NEAREST)
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

    # ai_illustration must stay up for as long as a real person's likeness is
    # anywhere in frame -- including an anchor shot's over-the-shoulder screen,
    # which _render_anchor_shot fills with the *next* scene-shot's image
    # (_next_scene_shot), not the anchor scene's own real_people.
    real_person_in_frame = bool(scene.get("real_people"))
    if scene["shot"] == "anchor":
        screen_scene = _next_scene_shot(script, scene["id"])
        if screen_scene is not None and screen_scene.get("real_people"):
            real_person_in_frame = True
    if real_person_in_frame:
        plates.add("ai_illustration")

    plates.add("ai_voice" if t_abs < AI_VOICE_PLATE_S else "ai_icon")
    return plates


def _render_scene_pixels(
    script: dict,
    scene: dict,
    scene_timing: SceneTiming,
    template: TemplatePackage,
    assets: ChannelAssets,
    t_abs: float,
    assets_dir: Path | None,
    palette: Palette,
    font,
) -> Image.Image:
    if scene["shot"] == "anchor":
        return _render_anchor_shot(script, scene, scene_timing, template, assets, t_abs, assets_dir, palette, font)
    return _render_scene_shot(script, scene, scene_timing, template, t_abs, assets_dir, palette, font)


def _draw_content_plates(frame: Image.Image, scene: dict, plates: set[str], template: TemplatePackage, font) -> None:
    """Scene-title / source / AI-illustration plates -- shared by the timed
    frame render (`_render_core`) and the untimed cover render
    (`render_cover_frame`), which skips the AI-voice/AI-icon plate and
    subtitles a still cover doesn't need."""
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


def render_cover_frame(
    script: dict,
    timeline: Timeline,
    template: TemplatePackage,
    assets: ChannelAssets,
    t_abs: float,
    assets_dir: Path | None = None,
) -> Image.Image:
    """A still frame for `cover.frame: S*`: the same scene pixels as the
    timed render at `t_abs`, but only the scene_title/source_plate/
    ai_illustration plates -- no AI-voice/AI-icon plate (those are timed
    relative to the video's start, which a still cover has none of) and no
    subtitles."""
    scene_timing = timeline.scene_at(t_abs)
    scene = _scene_data(script, scene_timing.scene_id)
    serious = bool(script.get("serious_mode"))
    palette = template.palette(serious=serious)
    font = assets.font(FONT_PX)
    frame = _render_scene_pixels(script, scene, scene_timing, template, assets, t_abs, assets_dir, palette, font)
    plates = active_plates(script, scene_timing, template, t_abs) & {"scene_title", "source_plate", "ai_illustration"}
    _draw_content_plates(frame, scene, plates, template, font)
    return frame.convert("RGB")


def _select_transition(template: TemplatePackage, scene_index: int) -> dict | None:
    if scene_index == 0:
        return None
    transitions = template.data.get("transitions", [])
    if not transitions:
        return None
    return transitions[scene_index % len(transitions)]


def scene_transition_duration(template: TemplatePackage, scene_index: int) -> float:
    """How long (seconds) the scene at `scene_index` spends blending in from
    the previous one -- 0 for the first scene, a `cut`, or a template with
    no transitions. Used by `studio build` to find each scene's first
    keyframe just after this blend ends."""
    transition = _select_transition(template, scene_index)
    if transition is None or transition["type"] == "cut":
        return 0.0
    return transition.get("duration_s", 0.0)


def _render_core(
    script: dict,
    template: TemplatePackage,
    assets: ChannelAssets,
    t_abs: float,
    assets_dir: Path | None,
    palette: Palette,
    font,
    scene_timing: SceneTiming,
) -> Image.Image:
    """Renders one scene's content, plates and subtitles -- everything
    `render_frame` needs both for the frame actually requested and for the
    previous scene's frozen last frame a transition blends from."""
    scene = _scene_data(script, scene_timing.scene_id)
    frame = _render_scene_pixels(script, scene, scene_timing, template, assets, t_abs, assets_dir, palette, font)

    plates = active_plates(script, scene_timing, template, t_abs)
    _draw_content_plates(frame, scene, plates, template, font)

    if "ai_voice" in plates:
        _draw_plate(frame, template.plate("ai_voice"), ["AI · AI voice"], font)
    else:
        _draw_plate(frame, template.plate("ai_icon"), ["AI"], font)

    cue = next((c for c in scene_timing.subtitles if c.start_s <= t_abs < c.end_s), None)
    if cue is not None:
        sub = template.data["subtitles"]
        rect = sub["rect"]
        style = sub["style"]
        # Re-wrap at render time using *this* template's own line/char caps
        # (schema `subtitles.style.max_lines`/`max_chars_per_line`) rather than
        # the timeline's template-independent split, so those fields actually
        # affect the frame.
        max_lines = style.get("max_lines", MAX_SUBTITLE_LINES)
        max_chars = style.get("max_chars_per_line", MAX_SUBTITLE_CHARS_PER_LINE)
        lines = wrap_words(cue.text.split(), max_chars)[:max_lines]
        fg = hex_to_rgb(style["color"])
        outline = hex_to_rgb(style["outline_color"])
        total_h = len(lines) * (FONT_PX + 2)
        y = rect["y"] + rect["h"] - total_h
        for line in lines:
            w, _h = text_size(line, font)
            x = rect["x"] + max(0, (rect["w"] - w) // 2)
            draw_text(frame, (x, y), line, font, fg, outline=outline)
            y += FONT_PX + 2

    return frame.convert("RGB")


def _apply_transition(kind: str, prev: Image.Image, curr: Image.Image, progress: float) -> Image.Image:
    """Blends the previous scene's frozen last frame into the current
    scene's frame using only crop/paste (no resize, no blending of pixel
    values), so every output pixel stays an exact color from one of the two
    source frames -- never a new, off-palette average."""
    progress = max(0.0, min(progress, 1.0))

    if kind == "wipe":
        x = round(LOGICAL_W * progress)
        out = prev.copy()
        if x > 0:
            out.paste(curr.crop((0, 0, x, LOGICAL_H)), (0, 0))
        return out

    if kind == "slide":
        offset = round(LOGICAL_W * progress)
        out = Image.new(prev.mode, (LOGICAL_W, LOGICAL_H))
        out.paste(prev, (-offset, 0))
        out.paste(curr, (LOGICAL_W - offset, 0))
        return out

    if kind == "pixelate":
        block = 10
        cols = LOGICAL_W // block
        rows = LOGICAL_H // block
        reveal = round(cols * rows * progress)
        out = prev.copy()
        for i in range(reveal):
            by, bx = divmod(i, cols)
            box = (bx * block, by * block, bx * block + block, by * block + block)
            out.paste(curr.crop(box), (box[0], box[1]))
        return out

    return curr  # "cut" (or any unrecognized type): an instant switch


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

    serious = bool(script.get("serious_mode"))
    # The template's own (muted, in serious mode) palette subset is what
    # backgrounds/scene art are snapped to; the anchor sprite is composited
    # afterwards and is never touched by this (see _render_anchor_shot).
    palette = template.palette(serious=serious)
    font = assets.font(FONT_PX)

    scene_index = timeline.scenes.index(scene_timing)
    frame = _render_core(script, template, assets, t_abs, assets_dir, palette, font, scene_timing)

    if scene_index == 0:
        return frame

    transition = _select_transition(template, scene_index)
    if transition is None:
        return frame
    duration = transition.get("duration_s", 0.0)
    elapsed = t_abs - scene_timing.start_s
    if transition["type"] == "cut" or duration <= 0 or elapsed >= duration:
        return frame

    prev_timing = timeline.scenes[scene_index - 1]
    prev_t = max(prev_timing.start_s, prev_timing.end_s - 1.0 / (2 * FPS))
    prev_frame = _render_core(script, template, assets, prev_t, assets_dir, palette, font, prev_timing)

    return _apply_transition(transition["type"], prev_frame, frame, elapsed / duration)
