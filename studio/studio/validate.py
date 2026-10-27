"""`studio template validate <dir>`: schema, asset colors, safe zones, text
contrast, required plates, no external URLs. Every failure is collected
into one readable message (not just the first); `cli.py` turns a non-empty
result into a non-zero exit."""
from __future__ import annotations

import json
from pathlib import Path

import jsonschema

from .channel_assets import load_channel_assets
from .constants import FONT_PX
from .errors import TemplateValidationError
from .palette import channel_palette_set, image_colors_in_palette
from .paths import TEMPLATE_SCHEMA_PATH
from .safe_zones import common_safe_rect, rect_within, rects_intersect
from .text_render import text_size

_MIN_CONTRAST_RATIO = 4.5  # WCAG AA for normal text, used as a simple readability floor
_PLATE_PAD = 2  # matches engine.py:_draw_plate's padding

# Fixed strings the engine always draws into these plates (engine.py:render_frame);
# a plate must be large enough to hold its own text at the channel's FONT_PX.
_FIXED_PLATE_TEXT = {
    "ai_voice": "AI · AI voice",
    "ai_icon": "AI",
    "ai_illustration": "AI illustration",
}
# scene_title/source_plate hold scene-dependent text; validate only that the
# rect can fit at least one line of *some* text, per the review ("хотя бы одну строку").
_MIN_LINE_PROBE_TEXT = "W"


def _srgb_to_linear(c: float) -> float:
    c = c / 255.0
    return c / 12.92 if c <= 0.03928 else ((c + 0.055) / 1.055) ** 2.4


def _luminance(rgb: tuple[int, int, int]) -> float:
    r, g, b = (_srgb_to_linear(c) for c in rgb)
    return 0.2126 * r + 0.7152 * g + 0.0722 * b


def _contrast_ratio(a: tuple[int, int, int], b: tuple[int, int, int]) -> float:
    la, lb = _luminance(a), _luminance(b)
    lighter, darker = max(la, lb), min(la, lb)
    return (lighter + 0.05) / (darker + 0.05)


def _hex_to_rgb(h: str) -> tuple[int, int, int]:
    h = h.lstrip("#")
    return (int(h[0:2], 16), int(h[2:4], 16), int(h[4:6], 16))


def _find_urls(data, path: str = "$") -> list[str]:
    hits = []
    if isinstance(data, str):
        if "://" in data:
            hits.append(f"{path} = {data!r}")
    elif isinstance(data, dict):
        for k, v in data.items():
            hits.extend(_find_urls(v, f"{path}.{k}"))
    elif isinstance(data, list):
        for i, v in enumerate(data):
            hits.extend(_find_urls(v, f"{path}[{i}]"))
    return hits


def _referenced_files(data: dict) -> list[str]:
    """Every file path a template package can reference: declared `assets[]`
    plus the studio background/foreground layers and the cover background --
    the review found these last two were never checked against the palette
    or against path traversal, only the (redundant) `assets[]` declaration."""
    files: list[str] = list(data.get("assets", []))
    for layer in data.get("studio", {}).get("layers", []):
        files.append(layer["file"])
    cover_bg = data.get("cover", {}).get("layout", {}).get("background")
    if cover_bg:
        files.append(cover_bg)
    seen: set[str] = set()
    ordered: list[str] = []
    for f in files:
        if f not in seen:
            seen.add(f)
            ordered.append(f)
    return ordered


def _min_plate_size(plate_name: str, font) -> tuple[int, int]:
    text = _FIXED_PLATE_TEXT.get(plate_name, _MIN_LINE_PROBE_TEXT)
    tw, th = text_size(text, font)
    return tw + 2 * _PLATE_PAD, th + 2 * _PLATE_PAD


def validate_template(template_dir: Path, channel_id: str = "8bitnews-us") -> None:
    """Raises TemplateValidationError with every problem found, or returns
    None if the package is valid."""
    template_dir = Path(template_dir)
    problems: list[str] = []

    template_json_path = template_dir / "template.json"
    if not template_json_path.is_file():
        raise TemplateValidationError(f"{template_json_path} does not exist")

    try:
        data = json.loads(template_json_path.read_text())
    except json.JSONDecodeError as e:
        raise TemplateValidationError(f"{template_json_path} is not valid JSON: {e}") from e

    with open(TEMPLATE_SCHEMA_PATH) as f:
        schema = json.load(f)
    validator = jsonschema.Draft202012Validator(schema)
    for error in sorted(validator.iter_errors(data), key=lambda e: list(e.path)):
        loc = "/".join(str(p) for p in error.path) or "<root>"
        problems.append(f"schema: {loc}: {error.message}")

    if problems:
        # Every later check assumes a schema-valid document; stop here.
        raise TemplateValidationError(_format(template_dir, problems))

    url_hits = _find_urls(data)
    if url_hits:
        problems.append("external URLs are not allowed in a template package: " + "; ".join(url_hits))

    if data.get("assets") and not data.get("license_note", "").strip():
        problems.append(
            "license_note is required when assets[] is non-empty (template.schema.json)"
        )

    palette_set = channel_palette_set(channel_id)
    template_dir_resolved = template_dir.resolve()
    for rel in _referenced_files(data):
        if Path(rel).is_absolute():
            problems.append(f"referenced file {rel!r} must be a relative path inside the template package")
            continue
        if ".." in Path(rel).parts:
            problems.append(f"referenced file {rel!r} must not escape the template package directory ('..' is not allowed)")
            continue
        asset_path = template_dir / rel
        try:
            if template_dir_resolved not in (asset_path.resolve(), *asset_path.resolve().parents):
                problems.append(f"referenced file {rel!r} resolves outside the template package directory")
                continue
        except OSError:
            pass
        if not asset_path.is_file():
            problems.append(f"referenced file {rel!r} is referenced but does not exist at {asset_path}")
            continue
        if asset_path.suffix.lower() == ".png":
            bad_colors = image_colors_in_palette(asset_path, palette_set)
            if bad_colors:
                problems.append(
                    f"{rel}: has colors outside the NES palette "
                    f"(channels/{channel_id}/palette.json): {', '.join(bad_colors)}"
                )

    assets = load_channel_assets(channel_id)
    font = assets.font(FONT_PX)
    face_rect = assets.anchor.face_rect_in_frame(data["studio"]["anchor"])

    safe_rect = common_safe_rect(channel_id)
    for plate_name, plate in data.get("plates", {}).items():
        rect = plate["rect"]
        if not rect_within(rect, safe_rect):
            problems.append(
                f"plates.{plate_name}.rect {rect} is outside the common safe zone {safe_rect} "
                f"(channels/{channel_id}/safe_zones.json)"
            )
        if rects_intersect(rect, face_rect):
            problems.append(
                f"plates.{plate_name}.rect {rect} overlaps the anchor's face {face_rect} "
                f"(channels/{channel_id}/anchor/manifest.json face_rect, scaled/offset by studio.anchor)"
            )
        min_w, min_h = _min_plate_size(plate_name, font)
        if rect["w"] < min_w or rect["h"] < min_h:
            problems.append(
                f"plates.{plate_name}.rect {rect['w']}x{rect['h']} is too small to fit its text at "
                f"{FONT_PX}px (needs at least {min_w}x{min_h})"
            )
        ratio = _contrast_ratio(_hex_to_rgb(plate["bg_color"]), _hex_to_rgb(plate["text_color"]))
        if ratio < _MIN_CONTRAST_RATIO:
            problems.append(
                f"plates.{plate_name}: contrast ratio {ratio:.2f} between bg_color "
                f"{plate['bg_color']} and text_color {plate['text_color']} is below {_MIN_CONTRAST_RATIO}"
            )

    sub_rect = data.get("subtitles", {}).get("rect")
    if sub_rect is not None:
        if not rect_within(sub_rect, safe_rect):
            problems.append(f"subtitles.rect {sub_rect} is outside the common safe zone {safe_rect}")
        if rects_intersect(sub_rect, face_rect):
            problems.append(f"subtitles.rect {sub_rect} overlaps the anchor's face {face_rect}")
    sub_style = data.get("subtitles", {}).get("style", {})
    if sub_style:
        ratio = _contrast_ratio(_hex_to_rgb(sub_style["color"]), _hex_to_rgb(sub_style["outline_color"]))
        if ratio < _MIN_CONTRAST_RATIO:
            problems.append(
                f"subtitles.style: contrast ratio {ratio:.2f} between color {sub_style['color']} "
                f"and outline_color {sub_style['outline_color']} is below {_MIN_CONTRAST_RATIO}"
            )

    cover_rect = data.get("cover", {}).get("layout", {}).get("text_rect")
    if cover_rect is not None and not rect_within(cover_rect, safe_rect):
        problems.append(f"cover.layout.text_rect {cover_rect} is outside the common safe zone {safe_rect}")

    for color in data.get("palette", {}).get("colors", []):
        if color.lower() not in palette_set:
            problems.append(f"palette.colors: {color} is not in the NES palette (channels/{channel_id}/palette.json)")
    for color in data.get("palette", {}).get("serious_colors", []):
        if color.lower() not in palette_set:
            problems.append(
                f"palette.serious_colors: {color} is not in the NES palette (channels/{channel_id}/palette.json)"
            )

    motion_types = {p.get("type") for p in data.get("motion_presets", [])}
    if not ({"pan", "zoom"} & motion_types):
        problems.append(
            "motion_presets: at least one preset of type 'pan' or 'zoom' is required "
            "(every shot: scene must move, docs/architecture.md §5)"
        )

    if problems:
        raise TemplateValidationError(_format(template_dir, problems))


def _format(template_dir: Path, problems: list[str]) -> str:
    lines = [f"Template package {template_dir} failed validation ({len(problems)} problem(s)):"]
    lines.extend(f"  - {p}" for p in problems)
    return "\n".join(lines)
