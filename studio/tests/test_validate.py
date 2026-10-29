import subprocess
import sys

import pytest

from studio.errors import TemplateValidationError
from studio.validate import _contrast_ratio, _hex_to_rgb, validate_template


def test_nes_desk_is_valid(nes_desk_dir):
    validate_template(nes_desk_dir)  # must not raise


def test_night_report_is_valid(night_report_dir):
    validate_template(night_report_dir)  # must not raise


def test_bad_safezone_fails_with_readable_reason(bad_safezone_dir):
    with pytest.raises(TemplateValidationError) as exc_info:
        validate_template(bad_safezone_dir)
    message = str(exc_info.value)
    assert "safe zone" in message
    assert "ai_label" in message


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


def test_bad_face_overlap_fails_with_readable_reason(bad_face_overlap_dir):
    with pytest.raises(TemplateValidationError) as exc_info:
        validate_template(bad_face_overlap_dir)
    message = str(exc_info.value)
    assert "face" in message
    assert "ai_label" in message


def test_bad_plate_too_small_fails_with_readable_reason(bad_plate_too_small_dir):
    with pytest.raises(TemplateValidationError) as exc_info:
        validate_template(bad_plate_too_small_dir)
    message = str(exc_info.value)
    assert "too small" in message
    assert "scene_title" in message


def test_bad_referenced_file_fails_with_readable_reason(bad_referenced_file_dir):
    """A studio.layers[].file that isn't also declared in assets[] must still
    be checked against the palette (review: the old code only checked
    assets[], so an off-palette studio layer slipped through)."""
    with pytest.raises(TemplateValidationError) as exc_info:
        validate_template(bad_referenced_file_dir)
    message = str(exc_info.value)
    assert "palette" in message
    assert "#ff00ff" in message


def test_contrast_ratio_uses_wcag_srgb_linearization_not_naive_luma():
    """#505050 text on #000000 looks borderline-readable under the naive
    (non-linearized) luma formula the old code used (~7.3, a false pass
    against the 4.5 AA floor); the correct WCAG relative-luminance ratio is
    ~2.6, which must fail."""
    ratio = _contrast_ratio(_hex_to_rgb("#505050"), _hex_to_rgb("#000000"))
    assert ratio < 4.5
    assert 2.0 < ratio < 3.0


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
