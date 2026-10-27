import sys
from pathlib import Path

STUDIO_DIR = Path(__file__).resolve().parent.parent
if str(STUDIO_DIR) not in sys.path:
    sys.path.insert(0, str(STUDIO_DIR))

import pytest  # noqa: E402

from studio.paths import TEMPLATES_DIR  # noqa: E402

FIXTURES_DIR = Path(__file__).resolve().parent / "fixtures"


@pytest.fixture
def nes_desk_dir() -> Path:
    return TEMPLATES_DIR / "nes-desk"


@pytest.fixture
def night_report_dir() -> Path:
    return TEMPLATES_DIR / "night-report"


@pytest.fixture
def bad_safezone_dir() -> Path:
    return FIXTURES_DIR / "bad-safezone"


@pytest.fixture
def bad_palette_dir() -> Path:
    return FIXTURES_DIR / "bad-palette"


@pytest.fixture
def missing_license_note_dir() -> Path:
    return FIXTURES_DIR / "missing-license-note"
