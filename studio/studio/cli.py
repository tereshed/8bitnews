from __future__ import annotations

import argparse
import json
import sys
from pathlib import Path

from . import repo
from .build import build_package
from .checks import run_checks
from .errors import BuildError, TemplateValidationError
from .music import synth_test_track
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
            keep_work=args.keep_work,
            music_file=Path(args.music_file) if args.music_file else None,
            music_track=args.music_track,
            anchor_id=args.anchor,
            images=args.images,
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


def _cmd_music_test_track(args: argparse.Namespace) -> int:
    path = synth_test_track(Path(args.out))
    print(f"wrote {path}")
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


def _cmd_anchor_candidates(args: argparse.Namespace) -> int:
    from .anchors import DEFAULT_IMAGE_MODEL, make_candidates
    from .channel_assets import load_channel_logo
    from .openrouter import OpenRouterClient
    from .template_pkg import load_template

    try:
        profile = repo.load_channel_profile(args.channel)
        template = load_template(Path(args.template) if args.template else
                                 Path(__file__).resolve().parent.parent / "templates" / profile["format"]["default_template"])
        made = make_candidates(
            args.channel, Path(args.out), OpenRouterClient(), template, load_channel_logo(args.channel),
            looks=args.looks.split(",") if args.looks else None, n=args.n, model=args.model or DEFAULT_IMAGE_MODEL,
        )
    except (BuildError, RepoRootNotSet, FileNotFoundError) as e:
        print(f"anchor candidates failed: {e}", file=sys.stderr)
        return 1
    for c in made:
        print(f"{c.id}: {c.dir} (${c.cost_usd:.3f})")
    print(f"{len(made)} candidate(s), ${sum(c.cost_usd for c in made):.2f}; side by side: {Path(args.out) / 'candidates.png'}")
    return 0 if made else 1


def _cmd_anchor_build(args: argparse.Namespace) -> int:
    from .anchors import DEFAULT_IMAGE_MODEL, DEFAULT_VISION_MODEL, build_anchor
    from .channel_assets import anchors_dir
    from .openrouter import OpenRouterClient

    out = Path(args.out) if args.out else anchors_dir(args.channel) / args.id
    try:
        manifest = build_anchor(
            args.channel, Path(args.from_dir), args.id, args.voice, out, OpenRouterClient(),
            image_model=args.model or DEFAULT_IMAGE_MODEL, vision_model=args.vision_model or DEFAULT_VISION_MODEL,
        )
    except (BuildError, FileNotFoundError) as e:
        print(f"anchor build failed: {e}", file=sys.stderr)
        return 1
    print(f"wrote {out} (voice {manifest['voice_id']}, ${manifest['_cost_usd']:.2f}); "
          f"frames: {Path(args.from_dir) / ('build-' + args.id) / 'frames.png'}")
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

    p_build = sub.add_parser("build", help="build a video_package from a script")
    p_build.add_argument("--script", required=True)
    p_build.add_argument("--channel", required=True)
    p_build.add_argument("--channels-dir", default=None)
    p_build.add_argument("--template", default=None)
    p_build.add_argument("--assets", default=None)
    p_build.add_argument("--tts", default=None)
    p_build.add_argument("--out", required=True)
    p_build.add_argument("--keep-work", action="store_true", help="keep frames/ after the build, for debugging")
    p_build.add_argument("--music-file", default=None,
                         help="music audio to use; alone it is recorded as unverified (demo/test), with "
                              "--music-track it must be that catalog track's audio")
    p_build.add_argument("--music-track", default=None,
                         help="catalog track id (hub music_pick) instead of picking by script.music")
    p_build.add_argument("--anchor", default=None,
                         help="which of the channel's anchors presents it (hub anchor_pick); default: one picked "
                              "by the script's content")
    p_build.add_argument("--images", default=None, choices=["openrouter"],
                         help="draw the scene pictures with the image provider (env STUDIO_OPENROUTER_API_KEY) "
                              "instead of reading them from --assets")
    p_build.set_defaults(func=_cmd_build)

    music = sub.add_parser("music", help="music helpers")
    music_sub = music.add_subparsers(dest="music_command", required=True)
    p_track = music_sub.add_parser("test-track", help="write the synthesized 8-bit test loop (own work; a build still records any "
                                   "--music-file without --music-track as unverified)")
    p_track.add_argument("--out", required=True)
    p_track.set_defaults(func=_cmd_music_test_track)

    anchor = sub.add_parser("anchor", help="draw the channel's anchors (a one-off, not part of a video build)")
    anchor_sub = anchor.add_subparsers(dest="anchor_command", required=True)
    p_cand = anchor_sub.add_parser("candidates", help="one candidate per look of the channel's anchors/brief.json, "
                                   "as a sprite in the studio, for a person to choose from")
    p_cand.add_argument("--channel", required=True)
    p_cand.add_argument("--out", required=True)
    p_cand.add_argument("--n", type=int, default=None, help="only the first N looks")
    p_cand.add_argument("--looks", default=None, help="comma-separated look ids from the brief")
    p_cand.add_argument("--template", default=None, help="studio to show them in (default: the channel's default template)")
    p_cand.add_argument("--model", default=None)
    p_cand.set_defaults(func=_cmd_anchor_candidates)

    p_abuild = anchor_sub.add_parser("build", help="a chosen candidate -> the anchor's sprite set (blink and mouth "
                                     "frames drawn by editing the same picture)")
    p_abuild.add_argument("--channel", required=True)
    p_abuild.add_argument("--from", dest="from_dir", required=True, help="the candidate's directory")
    p_abuild.add_argument("--id", required=True, help="anchor id (directory name under the channel's anchors/)")
    p_abuild.add_argument("--voice", required=True, help="the anchor's Kokoro voice id")
    p_abuild.add_argument("--out", default=None, help="default: studio/channels/<channel>/anchors/<id>")
    p_abuild.add_argument("--model", default=None)
    p_abuild.add_argument("--vision-model", default=None)
    p_abuild.set_defaults(func=_cmd_anchor_build)

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
