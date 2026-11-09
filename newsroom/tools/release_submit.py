#!/usr/bin/env python3
"""Сдаёт ролик в hub (`release_submit`) — последний шаг editor команды Newsroom:

    python3 newsroom/tools/release_submit.py --story-brief <id> --research-dossier <id> --script <id> \\
        --video-package <id> --validation-report <id> --legal-review <id>

Каждый id — артефакт нужного вида или `delegation_result` команды, в котором он лежит. Скрипт скачивает артефакты,
собирает манифест (development_id из `story_brief.development.hub_development_id`, медиа — из
`video_package.media`) и отправляет его в hub. Сначала проверяет, что все артефакты про один ролик: `video_package`
собран по этому `script`, а QA (`validation_report.video_package`) и Legal final (`legal_review.video_package`) смотрели
именно этот `video_package`. hub сам проверяет: файлы в хранилище, QA `pass`, Legal `final` и `pass`;
собирает и архивирует источники. В stdout — ответ hub (`release_id`, статус, допущенные площадки, источники).
С `--dry-run` манифест печатается, но не отправляется.
"""
from __future__ import annotations

import argparse
import json
import pathlib
import sys

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parent))
from hublib import Hub, artifact, artifact_details, artifact_with_id, artifact_with_origin  # noqa: E402


def reports_problems(legal_review: dict, legal_task, fetch) -> list[str]:
    """Nobody watches the video after this. Both reviewers must have looked at it, in the Legal task of this review."""
    problems = []
    for kind in ("law_report", "platform_report"):
        report_id = legal_review.get(kind)
        if not report_id:
            problems.append(f"legal_review (final) не называет {kind}: готовый ролик смотрел не вся команда")
            continue
        report = fetch(report_id, kind)
        if (report.get("content") or {}).get("mode") != "final":
            problems.append(f"{kind} {report_id} не о готовом ролике")
        if not legal_task or report.get("task_id") != legal_task:
            problems.append(f"{kind} {report_id} из другой задачи Legal")
    return problems


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__.split("\n\n")[0])
    for name in ("story-brief", "research-dossier", "script", "video-package", "validation-report", "legal-review"):
        ap.add_argument(f"--{name}", required=True)
    ap.add_argument("--dry-run", action="store_true")
    args = ap.parse_args()

    story_brief, _, task_id = artifact_with_origin(args.story_brief, "story_brief")
    research_dossier, dossier_id = artifact_with_id(args.research_dossier, "research_dossier")
    script, script_id = artifact_with_id(args.script, "script")
    video_package, video_id = artifact_with_id(args.video_package, "video_package")
    validation_report = artifact(args.validation_report, "validation_report")
    legal_details = artifact_details(args.legal_review, "legal_review")
    legal_review = legal_details["content"]
    if legal_review.get("mode") != "final":
        sys.exit(f"legal_review {args.legal_review} — режим {legal_review.get('mode')!r}: нужен челлендж готового ролика (final)")
    # The artifacts must be about one and the same video: a QA pass or a Legal pass for an earlier build says
    # nothing about this one.
    mismatch = []
    if script.get("dossier") != dossier_id:
        mismatch.append("script.dossier does not match the submitted research dossier")
    if video_package.get("script") != script_id:
        mismatch.append(f"video_package собран по сценарию {video_package.get('script')}, а сдаётся {script_id}")
    if validation_report.get("video_package") != video_id:
        mismatch.append(f"validation_report проверял {validation_report.get('video_package')}, а сдаётся {video_id}")
    if legal_review.get("video_package") != video_id:
        mismatch.append(f"legal_review (final) смотрел {legal_review.get('video_package')}, а сдаётся {video_id}")
    if legal_review.get("script") != script_id:
        mismatch.append(f"legal_review (final) был о сценарии {legal_review.get('script')}, а сдаётся {script_id}")
    mismatch += reports_problems(legal_review, legal_details.get("task_id"), artifact_details)
    if mismatch:
        sys.exit("артефакты не про один ролик:\n" + "\n".join(f"  - {m}" for m in mismatch))
    development_id = (story_brief.get("development") or {}).get("hub_development_id")
    if not development_id:
        sys.exit("в story_brief нет development.hub_development_id: editor не зарегистрировал поворот в hub")
    media = video_package.get("media") or {}
    missing = [k for k in ("video", "subtitles", "cover") if not media.get(k)]
    if missing:
        sys.exit(f"в video_package.media нет {', '.join(missing)}: ролик не загружен в хранилище")
    manifest = {
        "research_dossier_artifact_id": dossier_id,
        "script_artifact_id": script_id,
        "video_package_artifact_id": video_id,
        "development_id": development_id,
        "polymaths_task_id": task_id or "",
        "channel_id": video_package["channel_id"],
        "media": {"video": media["video"], "subtitles": media["subtitles"], "cover": media["cover"]},
        "research_dossier": research_dossier,
        "script": script,
        "video_package": video_package,
        "validation_report": validation_report,
        "legal_review": legal_review,
    }
    if args.dry_run:
        print(json.dumps(manifest, indent=1, ensure_ascii=False))
        return 0
    print(json.dumps(Hub().release_submit(manifest), indent=1, ensure_ascii=False))
    return 0


if __name__ == "__main__":
    sys.exit(main())
