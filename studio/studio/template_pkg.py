"""Loads a template package (template.json + its PNG assets) from a
directory. A template is data, not code: the engine renders any package
that satisfies studio/template.schema.json (docs/architecture.md §5)."""
from __future__ import annotations

import json
from dataclasses import dataclass
from functools import lru_cache
from pathlib import Path

from PIL import Image

from .palette import Palette


@dataclass(frozen=True)
class TemplatePackage:
    dir: Path
    data: dict

    @property
    def id(self) -> str:
        return self.data["id"]

    def asset_path(self, relative: str) -> Path:
        return self.dir / relative

    def load_asset_image(self, relative: str) -> Image.Image:
        return Image.open(self.asset_path(relative)).convert("RGBA")

    def palette(self, serious: bool = False) -> Palette:
        colors = self.data["palette"]["colors"]
        if serious:
            colors = self.data["palette"].get("serious_colors", colors)
        return Palette(colors)

    def plate(self, name: str) -> dict:
        return self.data["plates"][name]

    def rect(self, name: str) -> dict:
        return self.plate(name)["rect"]


def load_template(template_dir: Path | str) -> TemplatePackage:
    template_dir = Path(template_dir)
    with open(template_dir / "template.json") as f:
        data = json.load(f)
    return TemplatePackage(dir=template_dir, data=data)


@lru_cache(maxsize=8)
def _cached(template_dir_str: str) -> TemplatePackage:
    return load_template(Path(template_dir_str))


def load_template_cached(template_dir: Path | str) -> TemplatePackage:
    return _cached(str(Path(template_dir).resolve()))
