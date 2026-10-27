"""`studio template validate <dir>`: schema, asset colors, safe zones, text
contrast, required plates, no external URLs. Every failure is collected
into one readable message (not just the first); `cli.py` turns a non-empty
result into a non-zero exit."""
from __future__ import annotations

import json
from pathlib import Path

import jsonschema

from .errors import TemplateValidationError
from .palette import channel_palette_set, image_colors_in_palette
from .paths import TEMPLATE_SCHEMA_PATH
from .safe_zones import common_safe_rect, rect_within

_MIN_CONTRAST_RATIO = 4.5  # WCAG AA for normal text, used as a simple readability floor


def _luminance(rgb: tuple[int, int, int]) -> float:
    r, g, b = (c / 255.0 for c in rgb)
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

    palette_set = channel_palette_set(channel_id)
    for rel_asset in data.get("assets", []):
        asset_path = template_dir / rel_asset
        if not asset_path.is_file():
            problems.append(f"assets: {rel_asset} is listed but the file does not exist at {asset_path}")
            continue
        if asset_path.suffix.lower() == ".png":
            bad_colors = image_colors_in_palette(asset_path, palette_set)
            if bad_colors:
                problems.append(
                    f"assets: {rel_asset} has colors outside the NES palette "
                    f"(channels/{channel_id}/palette.json): {', '.join(bad_colors)}"
                )

    safe_rect = common_safe_rect(channel_id)
    for plate_name, plate in data.get("plates", {}).items():
        if not rect_within(plate["rect"], safe_rect):
            problems.append(
                f"plates.{plate_name}.rect {plate['rect']} is outside the common safe zone {safe_rect} "
                f"(channels/{channel_id}/safe_zones.json)"
            )
        ratio = _contrast_ratio(_hex_to_rgb(plate["bg_color"]), _hex_to_rgb(plate["text_color"]))
        if ratio < _MIN_CONTRAST_RATIO:
            problems.append(
                f"plates.{plate_name}: contrast ratio {ratio:.2f} between bg_color "
                f"{plate['bg_color']} and text_color {plate['text_color']} is below {_MIN_CONTRAST_RATIO}"
            )

    sub_rect = data.get("subtitles", {}).get("rect")
    if sub_rect is not None and not rect_within(sub_rect, safe_rect):
        problems.append(f"subtitles.rect {sub_rect} is outside the common safe zone {safe_rect}")
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

    if problems:
        raise TemplateValidationError(_format(template_dir, problems))


def _format(template_dir: Path, problems: list[str]) -> str:
    lines = [f"Template package {template_dir} failed validation ({len(problems)} problem(s)):"]
    lines.extend(f"  - {p}" for p in problems)
    return "\n".join(lines)
