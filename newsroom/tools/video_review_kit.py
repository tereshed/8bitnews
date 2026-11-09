#!/usr/bin/env python3
"""Скачивает собранный ролик для просмотра — QA и Legal (режим final) смотрят его по кадрам:

    python3 newsroom/tools/video_review_kit.py <id video_package или delegation_result> --out /tmp/review

В `<out>`: `video_package.json` (без служебной истории картинок, полный пакет — `video_package.full.json`), ключевые кадры
`keyframes/S1_first.png …` (кадр сцены ровно таким, каким он стоит в ролике: плашки, субтитры, ведущий), `cover.png`,
`cover_160.png` (копия шириной 160 px), `subtitles.srt`, `timeline.json`, а с `--video` ещё `video.mp4` и
`video_no_music.mp4`. Файлы берутся из хранилища через hub (`media_download_url`).
Кадры и обложка уменьшены вдвое, до 540×960: сетка кадра 270×480, поэтому ни один её пиксель не теряется, а токенов на
просмотр нужно вчетверо меньше. Инструмент проверяет это на каждом кадре; кадр вне сетки остаётся как есть
(`full_size_images` в сводке).
В stdout — `video_package_id` (id артефакта, который ты смотришь: впиши его в свой отчёт), список скачанного и короткая
сводка пакета: длительность, шаблон, ведущий, трек, проверки студии, заметки. Папка `--out` очищается перед скачиванием.
Открывай картинки инструментом чтения файлов: так ты их видишь.
"""
from __future__ import annotations

import argparse
import json
import pathlib
import shutil
import sys
import zlib

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parent))
from hublib import Hub, artifact_with_id  # noqa: E402
from pngshrink import Unsupported, halve, thumbnail  # noqa: E402

EXT = {"cover": ".png", "subtitles": ".srt", "timeline": ".json", "script": ".json", "video": ".mp4",
       "video_no_music": ".mp4"}


def compact(package: dict) -> dict:
    """The package without the pictures' prompts and rejected attempts, which reviewers don't read."""
    images = package.get("images")
    if not isinstance(images, dict):
        return package
    scenes = [{"id": s.get("id"), "file": s.get("file"), "accepted": s.get("accepted"),
               "attempts": len(s.get("attempts") or []),
               "found": ((s.get("attempts") or [{}])[-1]).get("found", [])} for s in images.get("scenes") or []]
    return {**package, "images": {**{k: v for k, v in images.items() if k != "scenes"}, "scenes": scenes}}


def shrink(out: pathlib.Path) -> list[str]:
    """Make cover_160.png, halve the frames and the cover. Returns what had to stay full size."""
    cover = out / "cover.png"
    kept = []
    if cover.exists():
        try:
            thumbnail(cover, out / "cover_160.png")
        except (Unsupported, OSError, zlib.error) as err:
            kept.append(f"cover_160.png: {err}")
    for path in sorted((out / "keyframes").glob("*.png")) + ([cover] if cover.exists() else []):
        try:
            if not halve(path):
                kept.append(f"{path.name}: вне сетки")
        except (Unsupported, OSError, zlib.error) as err:
            kept.append(f"{path.name}: {err}")
    return kept


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__.split("\n\n")[0])
    ap.add_argument("video_package", help="artifact id (or a delegation_result id), or a path to video_package.json")
    ap.add_argument("--out", default="/tmp/review")
    ap.add_argument("--video", action="store_true", help="also download video.mp4 and video_no_music.mp4")
    args = ap.parse_args()

    package, package_id = artifact_with_id(args.video_package, "video_package")
    out = pathlib.Path(args.out)
    shutil.rmtree(out, ignore_errors=True)  # nothing from an earlier video may be mistaken for this one
    out.mkdir(parents=True)
    (out / "video_package.json").write_text(json.dumps(compact(package), indent=2, ensure_ascii=False))
    (out / "video_package.full.json").write_text(json.dumps(package, indent=2, ensure_ascii=False))
    media = package.get("media") or {}
    if not media:
        sys.exit("у video_package нет media: ролик не загружен в хранилище, смотреть нечего")
    expected = {pathlib.Path(k["path"]).stem for s in package.get("scenes", []) for k in s.get("keyframes", [])}
    missing = sorted(expected - set((media.get("keyframes") or {})))
    if missing or not expected:
        sys.exit(f"в хранилище нет ключевых кадров {', '.join(missing) or '(в пакете нет сцен)'}: ролик загружен не целиком")
    hub = Hub()
    got = []
    for name in ("cover", "subtitles", "timeline") + (("video", "video_no_music") if args.video else ()):
        if media.get(name):
            got.append(hub.download(media[name], out / f"{name}{EXT[name]}"))
    for name, key in sorted((media.get("keyframes") or {}).items()):
        got.append(hub.download(key, out / "keyframes" / f"{name}.png"))
    full_size = shrink(out)
    failed = [f"{c['id']}: {c.get('note', '')}" for c in package.get("validate", {}).get("checks", []) if not c.get("ok")]
    print(json.dumps({
        "video_package_id": package_id,
        "downloaded": [str(p) for p in got],
        "full_size_images": full_size,
        "duration_s": package.get("duration_s"),
        "template": package.get("template"),
        "anchor": (package.get("anchor") or {}).get("id"),
        "music": package.get("music"),
        "studio_checks_passed": package.get("validate", {}).get("passed"),
        "failed_checks": failed,
        "notes": package.get("notes", []),
        "scenes": [{"id": s["id"], "keyframes": [k["path"] for k in s.get("keyframes", [])],
                    "real_people": s.get("real_people", []), "ai_label_shown": s.get("ai_label_shown")}
                   for s in package.get("scenes", [])],
    }, indent=1, ensure_ascii=False))
    return 0


if __name__ == "__main__":
    sys.exit(main())
