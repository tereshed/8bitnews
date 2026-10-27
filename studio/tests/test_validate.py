import subprocess
import sys

import pytest

from studio.errors import TemplateValidationError
from studio.validate import validate_template


def test_nes_desk_is_valid(nes_desk_dir):
    validate_template(nes_desk_dir)  # must not raise


def test_night_report_is_valid(night_report_dir):
    validate_template(night_report_dir)  # must not raise


def test_bad_safezone_fails_with_readable_reason(bad_safezone_dir):
    with pytest.raises(TemplateValidationError) as exc_info:
        validate_template(bad_safezone_dir)
    message = str(exc_info.value)
    assert "safe zone" in message
    assert "ai_voice" in message


def test_bad_palette_fails_with_readable_reason(bad_palette_dir):
    with pytest.raises(TemplateValidationError) as exc_info:
        validate_template(bad_palette_dir)
    message = str(exc_info.value)
    assert "palette" in message
    assert "#ff0000" in message


def test_missing_license_note_fails_with_readable_reason(missing_license_note_dir):
    with pytest.raises(TemplateValidationError) as exc_info:
        validate_template(missing_license_note_dir)
    message = str(exc_info.value)
    assert "license_note" in message


@pytest.mark.parametrize("fixture_name", ["nes-desk", "night-report"])
def test_cli_exits_zero_on_valid_template(fixture_name, nes_desk_dir, night_report_dir):
    template_dir = nes_desk_dir if fixture_name == "nes-desk" else night_report_dir
    result = subprocess.run(
        [sys.executable, "-m", "studio", "template", "validate", str(template_dir)],
        cwd=str(template_dir.parent.parent),
        capture_output=True,
        text=True,
    )
    assert result.returncode == 0, result.stdout + result.stderr


@pytest.mark.parametrize("fixture_name", ["bad-safezone", "bad-palette"])
def test_cli_exits_nonzero_on_invalid_template(fixture_name, bad_safezone_dir, bad_palette_dir):
    template_dir = bad_safezone_dir if fixture_name == "bad-safezone" else bad_palette_dir
    result = subprocess.run(
        [sys.executable, "-m", "studio", "template", "validate", str(template_dir)],
        cwd=str(template_dir.parent.parent.parent),
        capture_output=True,
        text=True,
    )
    assert result.returncode != 0
    assert result.stderr.strip() != ""
