#!/usr/bin/env python3
"""Plan/apply channel schedules, or inspect progress using read-only PolyMaths calls."""
from __future__ import annotations

import argparse
import json
import sys
from datetime import datetime, timezone
from pathlib import Path

import yaml

import sync

ROOT = Path(__file__).resolve().parent.parent
sys.path.insert(0, str(ROOT / "newsroom/tools"))
import pm_artifact  # noqa: E402


def schedules(root=ROOT):
    team = json.loads((root / "newsroom/teams/newsroom/team.json").read_text())["team_id"]
    desired = []
    for path in sorted((root / "channels").glob("*/channel.yaml")):
        channel = yaml.safe_load(path.read_text())
        for index, cron in enumerate(channel.get("schedule", {}).get("runs", [])):
            desired.append({"project_id": sync.PROJECT_ID, "name": f"8bitnews/{channel['id']}/{index + 1}",
                            "team_id": team, "cron_expression": cron, "timezone": channel["timezone"],
                            "work_intent": "explanation", "is_active": True,
                            "description": f"channel_id: {channel['id']}\nОдин запуск = один новый поворот сюжета. "
                            "Пройди Newsroom до release_receipt. Сначала проверь доступ к hub и историю канала. "
                            "Если hub недоступен или подходящей новости нет — не запускай Research и Video. "
                            "Публикация после одобрения модератора; самостоятельно не публикуй."})
    return desired


def apply_schedules(session, desired):
    response = session.tool("scheduled_task_list", {"project_id": sync.PROJECT_ID})
    existing = response["scheduled_tasks"] if isinstance(response, dict) else response
    if isinstance(response, dict) and response.get("total", len(existing)) != len(existing):
        raise ValueError("Incomplete schedule listing; refusing to create possible duplicates")
    for wanted in desired:
        matches = [s for s in existing if s["name"] == wanted["name"]]
        if len(matches) > 1:
            raise ValueError(f"Duplicate managed schedule: {wanted['name']}")
        if matches:
            if any(matches[0].get(k) != wanted[k] for k in ("cron_expression", "timezone", "team_id", "is_active", "work_intent", "description")):
                raise ValueError(f"Existing schedule differs: {wanted['name']}; review it in PolyMaths before changing")
            print(f"unchanged: {wanted['name']}")
        else:
            session.tool("scheduled_task_create", wanted)
            print(f"created: {wanted['name']}")


def progress(task, artifacts, now, stale_minutes):
    stamps = [task["created_at"]] + [a["created_at"] for a in artifacts if a.get("created_at")]
    latest = max(datetime.fromisoformat(stamp.replace("Z", "+00:00")) for stamp in stamps)
    kinds = {a.get("kind") for a in artifacts}
    status = task.get("status", task.get("state"))
    finished = status in ("done", "completed", "failed", "cancelled", "needs_human", "paused", "expired")
    return {"task_id": task["id"], "status": status, "ready_for_moderation": "release_receipt" in kinds,
            "minutes_without_artifact": round((now - latest).total_seconds() / 60, 1),
            "stalled": not finished and (now - latest).total_seconds() > stale_minutes * 60,
            "artifact_kinds": sorted(k for k in kinds if k)}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("command", choices=("schedules", "status"))
    parser.add_argument("--apply", action="store_true", help="create missing schedules; default only prints the local plan")
    parser.add_argument("--stale-minutes", type=int, default=30)
    parser.add_argument("--task-id", help="inspect a specific task instead of the latest root tasks")
    args = parser.parse_args()
    if args.command == "schedules" and not args.apply:
        print(json.dumps(schedules(), ensure_ascii=False, indent=2))
        return
    if args.command == "status" and args.apply:
        parser.error("status is read-only")
    session = pm_artifact.Session(sync.api_key())
    if args.command == "schedules":
        apply_schedules(session, schedules())
        return
    response = (session.tool("task_get", {"task_id": args.task_id}) if args.task_id else
                session.tool("task_list", {"project_id": sync.PROJECT_ID, "root_only": True,
                                          "limit": 20, "order_by": "created_at", "order_dir": "desc"}))
    tasks = [response.get("task", response)] if args.task_id else response["tasks"]
    newsroom = json.loads((ROOT / "newsroom/teams/newsroom/team.json").read_text())["team_id"]
    reports = []
    for task in tasks:
        if not args.task_id and task.get("team_id") != newsroom:
            continue
        result = session.tool("artifact_list", {"task_id": task["id"], "only_ready": True})
        artifacts = result["items"] if isinstance(result, dict) else result
        reports.append(progress(task, artifacts, datetime.now(timezone.utc), args.stale_minutes))
    print(json.dumps(reports, ensure_ascii=False, indent=2))
    if any(r["stalled"] or r["status"] in ("failed", "needs_human") for r in reports):
        raise SystemExit(1)


if __name__ == "__main__":
    main()
