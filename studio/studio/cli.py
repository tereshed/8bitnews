from __future__ import annotations

import argparse
import sys
from pathlib import Path

from .errors import TemplateValidationError
from .preview import render_preview
from .validate import validate_template


def _cmd_template_validate(args: argparse.Namespace) -> int:
    try:
        validate_template(Path(args.dir))
    except TemplateValidationError as e:
        print(str(e), file=sys.stderr)
        return 1
    print(f"{args.dir}: OK")
    return 0


def _cmd_template_preview(args: argparse.Namespace) -> int:
    try:
        result = render_preview(Path(args.dir), Path(args.out))
    except Exception as e:  # noqa: BLE001 - surface any failure as a readable CLI error
        print(f"preview failed: {e}", file=sys.stderr)
        return 1
    print(f"wrote {result['mp4']} ({result['duration_s']:.1f}s, {result['n_frames']} frames)")
    print(f"wrote {result['contact_sheet']}")
    return 0


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(prog="studio")
    sub = parser.add_subparsers(dest="command", required=True)

    template = sub.add_parser("template", help="template package commands")
    template_sub = template.add_subparsers(dest="template_command", required=True)

    p_validate = template_sub.add_parser("validate", help="validate a template package")
    p_validate.add_argument("dir")
    p_validate.set_defaults(func=_cmd_template_validate)

    p_preview = template_sub.add_parser("preview", help="render a short preview of a template package")
    p_preview.add_argument("dir")
    p_preview.add_argument("--out", required=True)
    p_preview.set_defaults(func=_cmd_template_preview)

    return parser


def main(argv: list[str] | None = None) -> int:
    parser = build_parser()
    args = parser.parse_args(argv)
    return args.func(args)
