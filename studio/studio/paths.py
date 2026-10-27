from pathlib import Path

STUDIO_ROOT = Path(__file__).resolve().parent.parent
CHANNELS_DIR = STUDIO_ROOT / "channels"
TEMPLATES_DIR = STUDIO_ROOT / "templates"
TEMPLATE_SCHEMA_PATH = STUDIO_ROOT / "template.schema.json"


def channel_dir(channel_id: str) -> Path:
    return CHANNELS_DIR / channel_id


def template_dir(template_id: str) -> Path:
    return TEMPLATES_DIR / template_id
