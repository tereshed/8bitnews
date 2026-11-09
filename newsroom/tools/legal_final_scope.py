#!/usr/bin/env python3
"""Какой объём проверки нужен Legal в режиме final:

    python3 newsroom/tools/legal_final_scope.py --script <id> --legal-review <id legal_review по сценарию> --video-package <id>

`scope: new_only` — ролик собран из того самого сценария, что прошёл Legal, открытых замечаний к тексту нет: текст заново
не разбираем, смотрим то, чего в сценарии не было видно. `scope: full` — причины в `reasons`, проверяем и текст. Если что-то
не удалось установить, инструмент отвечает `full`.
"""
from __future__ import annotations

import argparse
import json
import pathlib
import sys

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parent))


def decide(script_meta: dict, review_meta: dict, package: dict) -> dict:
    review = review_meta.get("content") or {}
    reasons = []
    if review.get("mode") != "script":
        reasons.append(f"это не ревью сценария (mode: {review.get('mode')})")
    if review.get("verdict") not in ("pass", "fix_required"):
        reasons.append(f"вердикт ревью: {review.get('verdict')}")
    if not script_meta.get("id") or package.get("script") != script_meta.get("id"):
        reasons.append(f"ролик собран из другого сценария: {package.get('script')}, а не {script_meta.get('id')}")
    script_at, review_at = script_meta.get("created_at"), review_meta.get("created_at")
    if not script_at or not review_at:
        reasons.append("нет дат артефактов")
    elif review_at <= script_at:
        reasons.append("сценарий новее ревью: эту версию Legal не видел")
    for issue in review.get("issues", []):
        if issue.get("severity") in ("blocker", "high") and issue.get("fix_owner") in ("script", "editor") \
                and issue.get("resolved_by_exclusion") is not True:
            reasons.append(f"в ревью открыто замечание к тексту: {issue.get('id')}")
    return {"scope": "full" if reasons else "new_only", "reasons": reasons}


def main() -> int:
    from hublib import artifact, artifact_details

    ap = argparse.ArgumentParser(description=__doc__.split("\n\n")[0])
    ap.add_argument("--script", required=True)
    ap.add_argument("--legal-review", required=True)
    ap.add_argument("--video-package", required=True)
    args = ap.parse_args()
    result = decide(artifact_details(args.script, "script"), artifact_details(args.legal_review, "legal_review"),
                    artifact(args.video_package, "video_package"))
    print(json.dumps(result, ensure_ascii=False))
    return 0


if __name__ == "__main__":
    sys.exit(main())
