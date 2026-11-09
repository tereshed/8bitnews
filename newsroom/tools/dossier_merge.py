#!/usr/bin/env python3
"""Сводит три отчёта Research в основу `research_dossier` кодом, чтобы analyst их не перепечатывал:

    python3 newsroom/tools/dossier_merge.py --facts <id> --local <id> --world <id> --out /tmp/dossier.json

Переносит без изменений `facts`, `timeline`, `unknowns`, `as_of` (самый ранний), `groups` → `local_groups`, `polls`,
`clusters` → `world_clusters`. `story`, `channel_id`, `language`, `review` и `risks` пишет analyst: без них файл не
пройдёт контракт. В stdout — сводка и `coverage_notes` мирового отчёта: решить, что из них перенести в `unknowns`.
"""
from __future__ import annotations

import argparse
import json
import pathlib
import sys

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parent))
from hublib import artifact  # noqa: E402


def merge(facts: dict, local: dict, world: dict) -> dict:
    as_of = sorted(report["as_of"] for report in (facts, local, world) if report.get("as_of"))
    dossier = {
        "facts": facts["facts"],
        "timeline": facts.get("timeline", []),
        "local_groups": local["groups"],
        "polls": local.get("polls", []),
        "world_clusters": world.get("clusters", []),
        "unknowns": facts.get("unknowns", []),
    }
    if as_of:
        dossier["as_of"] = as_of[0]
    return dossier


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__.split("\n\n")[0])
    ap.add_argument("--facts", required=True, help="id facts_report (или delegation_result, или путь к JSON)")
    ap.add_argument("--local", required=True, help="id local_press_report")
    ap.add_argument("--world", required=True, help="id world_press_report")
    ap.add_argument("--out", required=True)
    args = ap.parse_args()
    facts = artifact(args.facts, "facts_report")
    local = artifact(args.local, "local_press_report")
    world = artifact(args.world, "world_press_report")
    dossier = merge(facts, local, world)
    pathlib.Path(args.out).write_text(json.dumps(dossier, indent=2, ensure_ascii=False))
    print(json.dumps({
        "out": args.out,
        "facts": [item.get("id") for item in dossier["facts"]],
        "local_groups": [item.get("id") for item in dossier["local_groups"]],
        "world_clusters": [item.get("id") for item in dossier["world_clusters"]],
        "coverage_notes": world.get("coverage_notes", []),
        "to_write": ["story", "channel_id", "language", "review", "risks", "world_clusters[].contrast_with_local"],
    }, indent=1, ensure_ascii=False))
    return 0


if __name__ == "__main__":
    sys.exit(main())
