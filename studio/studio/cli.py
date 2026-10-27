from __future__ import annotations

import argparse
import json
import sys
from pathlib import Path

from . import repo
from .build import build_package
from .checks import run_checks
from .errors import BuildError, TemplateValidationError
from .preview import render_preview
from .repo import RepoRootNotSet
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


def _cmd_build(args: argparse.Namespace) -> int:
    try:
        package_path = build_package(
            script_path=Path(args.script),
            channel_id=args.channel,
            out_dir=Path(args.out),
            channels_dir=Path(args.channels_dir) if args.channels_dir else None,
            template_dir_arg=args.template,
            assets_dir=Path(args.assets) if args.assets else None,
            tts_name=args.tts,
        )
    except (BuildError, RepoRootNotSet, FileNotFoundError) as e:
        print(f"build failed: {e}", file=sys.stderr)
        return 1

    package = json.loads(package_path.read_text())
    print(f"wrote {package_path} ({package['duration_s']:.1f}s)")
    if not package["validate"]["passed"]:
        print("studio validate found problems in the freshly built package:", file=sys.stderr)
        for check in package["validate"]["checks"]:
            if not check["ok"]:
                print(f"  - {check['id']}: {check.get('note', '')}", file=sys.stderr)
        return 1
    return 0


def _cmd_validate(args: argparse.Namespace) -> int:
    package_path = Path(args.package)
    try:
        package = json.loads(package_path.read_text())
        channel_id = args.channel or package["channel_id"]
        channel_profile = repo.load_channel_profile(
            channel_id, channels_dir=Path(args.channels_dir) if args.channels_dir else None
        )
    except (RepoRootNotSet, FileNotFoundError, json.JSONDecodeError) as e:
        print(f"validate failed: {e}", file=sys.stderr)
        return 1

    result = run_checks(package_path.parent, package, channel_profile)
    for check in result["checks"]:
        status = "ok" if check["ok"] else "FAIL"
        print(f"[{status}] {check['id']}: {check.get('note', '')}")
    return 0 if result["passed"] else 1


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

    p_build = sub.add_parser("build", help="build a video_package from a script")
    p_build.add_argument("--script", required=True)
    p_build.add_argument("--channel", required=True)
    p_build.add_argument("--channels-dir", default=None)
    p_build.add_argument("--template", default=None)
    p_build.add_argument("--assets", default=None)
    p_build.add_argument("--tts", default=None)
    p_build.add_argument("--out", required=True)
    p_build.set_defaults(func=_cmd_build)

    p_pkg_validate = sub.add_parser("validate", help="run studio validate's checks against a built package")
    p_pkg_validate.add_argument("--package", required=True)
    p_pkg_validate.add_argument("--channels-dir", default=None)
    p_pkg_validate.add_argument("--channel", default=None)
    p_pkg_validate.set_defaults(func=_cmd_validate)

    return parser


def main(argv: list[str] | None = None) -> int:
    parser = build_parser()
    args = parser.parse_args(argv)
    return args.func(args)
