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
    FONT_PX,
    FPS,
    LOGICAL_H,
    LOGICAL_W,
    MAX_SUBTITLE_CHARS_PER_LINE,
    MAX_SUBTITLE_LINES,
)
from .palette import Palette, hex_to_rgb, quantize_image
from .quantize import quantize
from .subtitles import cue_lines, wrap_words
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
    they don't collide with a template's top-area plates (source, scene
    title)."""
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
    """Fitting a real scene asset (e.g. a 1024x1792 generated image) to the
    grid is the expensive step (~0.17s observed); every frame of a scene
    re-requests the exact same (path, palette, size) while the scene plays,
    so caching it by those pure inputs is safe and cuts render time without
    affecting determinism. An empty `palette_colors` keeps the picture's own
    colors. Callers never mutate the returned Image in place (they only
    .crop/.convert/.paste it, all of which return new images)."""
    palette = Palette(palette_colors) if palette_colors else None
    return quantize(Image.open(path_str), palette, target_w=w, target_h=h).convert("RGBA")


def scene_palette(template: TemplatePackage, serious: bool, channel_id: str) -> Palette | None:
    """The colors a scene picture snaps to, or None to keep its own. In
    normal mode the picture keeps the generator's colors (owner, 29.10: the
    whole NES palette turned courtrooms brown and grey), unless the template
    sets `palette.scene_colors`; it is still fitted to the 270x480 grid. In
    serious mode the template's muted colors, as before."""
    if serious:
        return template.palette(serious=True)
    colors = template.data["palette"].get("scene_colors")
    return Palette(colors) if colors else None


def frame_palette(template: TemplatePackage, serious: bool, channel_id: str) -> Palette:
    """The template's colors for everything it draws itself, carrying
    `.scene` (`scene_palette`) for the scene pictures."""
    palette = template.palette(serious=serious)
    palette.scene = scene_palette(template, serious, channel_id)
    return palette


def _render_scene_base(
    scene: dict, assets_dir: Path | None, palette: Palette, font, w: int, h: int
) -> Image.Image:
    """Renders scene["id"]'s image (or a placeholder) straight at the
    requested logical size: a picture fitted to the grid, snapped to
    `palette.scene` when there is one (`frame_palette`), the placeholder card
    snapped to `palette` itself. Used both
    for the main `shot: scene` frame and for the anchor's over-the-shoulder
    screen, and -- at oversized `w`/`h` -- as the source for pan/zoom motion,
    so panning/zooming is a pure crop with no secondary resize."""
    path = _scene_image_path(assets_dir, scene["id"])
    if path is not None:
        scene_colors = getattr(palette, "scene", palette)
        return _quantize_asset_cached(str(path), tuple(scene_colors.hex_colors) if scene_colors else (), w, h)
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
    layers = list(enumerate(template.data["studio"]["layers"]))

    def draw_layers(front: bool) -> None:
        for i, layer in layers:
            if bool(layer.get("front")) == front:
                img = quantize_image(template.load_asset_image(layer["file"]), palette)
                dx = _parallax_shift(template, i, scene_timing, t_abs)
                frame.alpha_composite(img, (layer["x"] + dx, layer["y"]))

    draw_layers(front=False)
    screen = template.data["studio"]["screen"]
    # The anchor scene's own picture when it has one (its `visual` says what the screen shows: the hook's screen
    # is what Legal reviewed on it and on an S1 cover), else the next scene shot's, else the channel logo.
    own = _scene_image_path(assets_dir, scene["id"]) is not None and _next_scene_shot(script, scene["id"]) is not None
    screen_scene = scene if own else _next_scene_shot(script, scene["id"])
    if screen_scene is not None:
        content = _render_scene_base(screen_scene, assets_dir, palette, font, screen["w"], screen["h"])
    else:
        content = _logo_card(assets.logo, screen["w"], screen["h"], hex_to_rgb(screen.get("border_color", "#000000")))
    if screen.get("border_color"):
        b = SCREEN_BORDER_PX
        ImageDraw.Draw(frame).rectangle(
            [screen["x"] - b, screen["y"] - b, screen["x"] + screen["w"] + b - 1, screen["y"] + screen["h"] + b - 1],
            fill=(*hex_to_rgb(screen["border_color"]), 255))
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
    draw_layers(front=True)  # the desk in front of the anchor
    return frame


def _logo_card(logo: Image.Image, w: int, h: int, bg: tuple[int, int, int]) -> Image.Image:
    """The channel logo on the screen of the last anchor shot: whole-pixel
    scaled, centered on a plain card, with a margin -- never stretched."""
    card = Image.new("RGBA", (w, h), (*bg, 255))
    scale = max(1, min((w - 16) // logo.width, (h - 16) // logo.height))
    big = logo.resize((logo.width * scale, logo.height * scale), Image.NEAREST)
    card.alpha_composite(big, ((w - big.width) // 2, (h - big.height) // 2))
    return card


def _next_scene_shot(script: dict, after_scene_id: str) -> dict | None:
    scenes = script["scenes"]
    idx = next(i for i, s in enumerate(scenes) if s["id"] == after_scene_id)
    for s in scenes[idx + 1 :]:
        if s["shot"] == "scene":
            return s
    return None


def _max_chars_for_rect(rect_w: int, pad: int = 3) -> int:  # pad: PLATE_PAD_X, the box's own
    """How many FONT_PX-wide monospace glyphs fit on one line of a plate
    rect, after padding on both sides."""
    return max(1, (rect_w - 2 * pad) // FONT_PX)


DEFAULT_PLATE_MAX_LINES = 2  # template.schema.json plate_style.max_lines default


def plate_lines(template: TemplatePackage, plate_name: str, text: str) -> list[str]:
    """Scene text (`on_screen_text`, `source_plate`) word-wrapped to its
    plate's width, never truncated: `studio build` refuses a script whose
    text needs more than `plate_max_lines` before anything is rendered."""
    return wrap_words(text.split(), _max_chars_for_rect(template.plate(plate_name)["rect"]["w"]))


def plate_max_lines(template: TemplatePackage, plate_name: str) -> int:
    return template.plate(plate_name).get("max_lines", DEFAULT_PLATE_MAX_LINES)


PLATE_PAD_X, PLATE_PAD_Y = 3, 2
PLATE_GAP = 2  # between stacked plates (`below`)
SCREEN_BORDER_PX = 2


def plate_box(plate_cfg: dict, lines: list[str], font, y: int | None = None) -> dict:
    """The box a text plate draws: as wide as its longest line, centered in
    its rect, one 10-px row per line -- at `y`, or at the rect's own top."""
    rect = plate_cfg["rect"]
    w = max(text_size(line, font)[0] for line in lines) + 2 * PLATE_PAD_X
    return {"x": rect["x"] + (rect["w"] - w) // 2, "y": rect["y"] if y is None else y, "w": w,
            "h": len(lines) * (FONT_PX + 2) + PLATE_PAD_Y}


def _draw_plate(frame: Image.Image, plate_cfg: dict, lines: list[str], font, y: int | None = None) -> dict:
    """Draws the plate (`plate_box`), each line centered in it; returns the box."""
    box = plate_box(plate_cfg, lines, font, y)
    bg = hex_to_rgb(plate_cfg["bg_color"])
    fg = hex_to_rgb(plate_cfg["text_color"])
    ImageDraw.Draw(frame).rectangle(
        [box["x"], box["y"], box["x"] + box["w"] - 1, box["y"] + box["h"] - 1], fill=(*bg, 255))
    ty = box["y"] + PLATE_PAD_Y
    for line in lines:
        draw_text(frame, (box["x"] + (box["w"] - text_size(line, font)[0]) // 2, ty), line, font, fg)
        ty += FONT_PX + 2
    return box


AI_VOICE_LABEL = "AI voice"
AI_ILLUSTRATION_LABEL = "AI illustration"


def ai_label_text(plates: set[str], cover: bool = False) -> str | None:
    """The one AI line (format.md 'Текст в кадре'): "AI voice" on every
    frame, joined by "AI illustration" while a real person is in frame. A
    still cover has no voice: only "AI illustration", when it applies."""
    illustration = "ai_illustration" in plates
    if cover:
        return AI_ILLUSTRATION_LABEL if illustration else None
    return f"{AI_VOICE_LABEL} · {AI_ILLUSTRATION_LABEL}" if illustration else AI_VOICE_LABEL


def ai_label_origin(template: TemplatePackage, text: str, font) -> tuple[int, int]:
    """Top-left of the AI line: centered in the frame at `plates.ai_label.y`."""
    return (LOGICAL_W - text_size(text, font)[0]) // 2, template.plate("ai_label")["y"]


def draw_ai_label(frame: Image.Image, template: TemplatePackage, text: str | None, font) -> None:
    """The AI line: small text with an outline, no plate behind it."""
    if not text:
        return
    cfg = template.plate("ai_label")
    draw_text(frame, ai_label_origin(template, text, font), text, font, hex_to_rgb(cfg["text_color"]),
              outline=hex_to_rgb(cfg["outline_color"]))


def active_plates(script: dict, scene_timing: SceneTiming, template: TemplatePackage, t_abs: float) -> set[str]:
    """Which of scene_title, source_plate and ai_illustration (the AI line's
    "AI illustration", `ai_label_text`) are visible at `t_abs`. A pure
    function of its inputs, so the rules (format.md: AI illustration while a
    real person is in frame) can be tested without rendering any pixels.
    """
    scene = _scene_data(script, scene_timing.scene_id)
    plates: set[str] = set()

    element_delay = _get_preset(template, "element_appear")
    title_ready = True
    if element_delay is not None and element_delay["params"].get("element") == "scene_title":
        title_ready = (t_abs - scene_timing.start_s) >= element_delay["params"].get("delay_s", 0.0)

    if title_ready and (scene.get("on_screen_text") or "").strip():
        plates.add("scene_title")
    if (scene.get("source_plate") or "").strip():
        plates.add("source_plate")

    # ai_illustration must stay up for as long as a real person's likeness is
    # anywhere in frame -- including an anchor shot's over-the-shoulder screen,
    # which _render_anchor_shot fills with the anchor scene's own picture (its
    # real_people) or the *next* scene-shot's image (_next_scene_shot): both count.
    real_person_in_frame = bool(scene.get("real_people"))
    if scene["shot"] == "anchor":
        screen_scene = _next_scene_shot(script, scene["id"])
        if screen_scene is not None and screen_scene.get("real_people"):
            real_person_in_frame = True
    if real_person_in_frame:
        plates.add("ai_illustration")
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


_TEXT_PLATE_FIELDS = {"source_plate": "source_plate", "scene_title": "on_screen_text"}


def content_plate_boxes(scene: dict, plates: set[str], template: TemplatePackage, font) -> dict[str, tuple[dict, list[str]]]:
    """Where the source and title plates go and what they say. A plate with
    `below` sits under that plate when it is shown, else at its own rect's
    top: the source above the title (owner, 29.10), the title up top alone."""
    out: dict[str, tuple[dict, list[str]]] = {}
    for name in sorted(_TEXT_PLATE_FIELDS, key=lambda n: bool(template.plate(n).get("below"))):
        if name not in plates:
            continue
        # [:max_lines] only guards `template preview`'s built-in script; `studio
        # build` has already rejected text that doesn't fit (build.check_plate_text).
        lines = plate_lines(template, name, scene[_TEXT_PLATE_FIELDS[name]])[: plate_max_lines(template, name)]
        above = out.get(template.plate(name).get("below"))
        y = above[0]["y"] + above[0]["h"] + PLATE_GAP if above else None
        out[name] = (plate_box(template.plate(name), lines, font, y), lines)
    return out


def _draw_content_plates(frame: Image.Image, scene: dict, plates: set[str], template: TemplatePackage, font) -> None:
    """Source and title plates -- shared by the timed frame render
    (`_render_core`) and the untimed cover render (`render_cover_frame`)."""
    for name, (box, lines) in content_plate_boxes(scene, plates, template, font).items():
        _draw_plate(frame, template.plate(name), lines, font, box["y"])


def cover_plates(script: dict, scene_timing: SceneTiming, template: TemplatePackage, t_abs: float) -> set[str]:
    """Plates on a `cover.frame: S*` cover: only ai_illustration, by the same
    rule as the timed frame (a real person in the scene or on the screen
    behind the anchor). The cover's own title replaces scene_title and
    source_plate, and a still cover has no voice to label."""
    return active_plates(script, scene_timing, template, t_abs) & {"ai_illustration"}


def render_cover_frame(
    script: dict,
    timeline: Timeline,
    template: TemplatePackage,
    assets: ChannelAssets,
    t_abs: float,
    assets_dir: Path | None = None,
) -> Image.Image:
    """A still frame for `cover.frame: S*`: the same scene pixels as the
    timed render at `t_abs` with `cover_plates` only and no subtitles; the
    caller draws `cover.text` on top."""
    scene_timing = timeline.scene_at(t_abs)
    scene = _scene_data(script, scene_timing.scene_id)
    serious = bool(script.get("serious_mode"))
    palette = frame_palette(template, serious, assets.channel_id)
    font = assets.font(FONT_PX)
    frame = _render_scene_pixels(script, scene, scene_timing, template, assets, t_abs, assets_dir, palette, font)
    plates = cover_plates(script, scene_timing, template, t_abs)
    _draw_content_plates(frame, scene, plates, template, font)
    draw_ai_label(frame, template, ai_label_text(plates, cover=True), font)
    return frame.convert("RGB")


def _select_transition(template: TemplatePackage, scene_index: int) -> dict | None:
    if scene_index == 0:
        return None
    transitions = template.data.get("transitions", [])
    if not transitions:
        return None
    return transitions[scene_index % len(transitions)]


def transition_sound(template: TemplatePackage, scene_index: int) -> Path | None:
    """The sound the scene at `scene_index` enters with (its transition's
    `sound`), or None. `studio build` drops it in serious mode."""
    transition = _select_transition(template, scene_index)
    if transition is None or not transition.get("sound"):
        return None
    return template.asset_path(transition["sound"])


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

    cue = next((c for c in scene_timing.subtitles if c.start_s <= t_abs < c.end_s), None)
    if cue is not None:
        _draw_subtitle(frame, template, assets, cue.text)
    return frame.convert("RGB")


def subtitle_layout(template: TemplatePackage, text: str) -> tuple[int, list[str]]:
    """(font scale, lines) for a cue: re-wrapped at render time with *this*
    template's caps (`subtitles.style`), by the same `cue_lines` the build
    split the cues with -- a cue with a word longer than a line
    ("administration") is shown in the small font, not broken."""
    style = template.data["subtitles"]["style"]
    scale, lines = cue_lines(text.split(), style.get("max_chars_per_line", MAX_SUBTITLE_CHARS_PER_LINE),
                             style.get("font_scale", 1))
    return scale, lines[: style.get("max_lines", MAX_SUBTITLE_LINES)]


def subtitle_box(template: TemplatePackage, assets: ChannelAssets, text: str) -> tuple[dict, list[tuple[str, tuple[int, int]]], int]:
    """The cue's plate box (bottom-aligned in `subtitles.rect`, centered,
    as wide as its longest line) and each line with its origin, and the scale."""
    rect = template.data["subtitles"]["rect"]
    scale, lines = subtitle_layout(template, text)
    font = assets.font(FONT_PX * scale)
    step = (FONT_PX + 2) * scale
    pad_x, pad_y = PLATE_PAD_X * scale, PLATE_PAD_Y * scale
    w = max(text_size(line, font)[0] for line in lines) + 2 * pad_x
    h = len(lines) * step + pad_y
    box = {"x": rect["x"] + (rect["w"] - w) // 2, "y": rect["y"] + rect["h"] - h, "w": w, "h": h}
    origins = []
    for i, line in enumerate(lines):
        origins.append((line, (box["x"] + (w - text_size(line, font)[0]) // 2, box["y"] + pad_y + i * step)))
    return box, origins, scale


def _draw_subtitle(frame: Image.Image, template: TemplatePackage, assets: ChannelAssets, text: str) -> None:
    """Subtitles centered, on a plate (`style.bg_color`) or outlined text."""
    style = template.data["subtitles"]["style"]
    box, origins, scale = subtitle_box(template, assets, text)
    font = assets.font(FONT_PX * scale)
    fg = hex_to_rgb(style["color"])
    if style.get("bg_color"):
        ImageDraw.Draw(frame).rectangle([box["x"], box["y"], box["x"] + box["w"] - 1, box["y"] + box["h"] - 1],
                                        fill=(*hex_to_rgb(style["bg_color"]), 255))
        outline = None
    else:
        outline = hex_to_rgb(style["outline_color"])
    for line, xy in origins:
        draw_text(frame, xy, line, font, fg, outline=outline)


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
    # its backgrounds are snapped to, scene pictures to `palette.scene`; the
    # anchor sprite is composited afterwards and is never touched by this
    # (see _render_anchor_shot).
    palette = frame_palette(template, serious, assets.channel_id)
    font = assets.font(FONT_PX)

    scene_index = timeline.scenes.index(scene_timing)
    frame = _render_core(script, template, assets, t_abs, assets_dir, palette, font, scene_timing)
    plates = active_plates(script, scene_timing, template, t_abs)

    transition = _select_transition(template, scene_index) if scene_index > 0 else None
    duration = transition.get("duration_s", 0.0) if transition else 0.0
    elapsed = t_abs - scene_timing.start_s
    if transition is not None and transition["type"] != "cut" and duration > 0 and elapsed < duration:
        prev_timing = timeline.scenes[scene_index - 1]
        prev_t = max(prev_timing.start_s, prev_timing.end_s - 1.0 / (2 * FPS))
        prev_frame = _render_core(script, template, assets, prev_t, assets_dir, palette, font, prev_timing)
        frame = _apply_transition(transition["type"], prev_frame, frame, elapsed / duration)
        # mid-transition both scenes are in frame: either one's real person counts
        plates |= active_plates(script, prev_timing, template, prev_t) & {"ai_illustration"}

    # The AI line goes on last, outside the transition: it holds still on every frame.
    frame = frame.convert("RGBA")
    draw_ai_label(frame, template, ai_label_text(plates), font)
    return frame.convert("RGB")
