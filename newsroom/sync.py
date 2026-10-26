#!/usr/bin/env python3
"""Синхронизирует команду PolyMaths с её описанием в newsroom/teams/<team>/team.json.

Создаёт недостающих агентов, обновляет промпты и описания ролей, выключает лишних агентов,
обновляет описание команды и контракты артефактов. Источник правды — репозиторий.

    python3 newsroom/sync.py research [--dry-run]

Ключ API: env POLYMATHS_API_KEY, иначе берётся из .mcp.json (сервер wibe).
"""
import argparse
import json
import os
import pathlib
import ssl
import sys
import urllib.error
import urllib.request

try:  # python.org-сборки Python на macOS не видят системные сертификаты
    import certifi
    SSL_CONTEXT = ssl.create_default_context(cafile=certifi.where())
except ImportError:
    SSL_CONTEXT = ssl.create_default_context()

ROOT = pathlib.Path(__file__).resolve().parent
BASE = os.environ.get("POLYMATHS_URL", "https://polymaths.work/api/v1")
PROJECT_ID = os.environ.get("POLYMATHS_PROJECT_ID", "dc720749-e93f-4078-b7fc-d5a3193eefc3")


def api_key():
    if os.environ.get("POLYMATHS_API_KEY"):
        return os.environ["POLYMATHS_API_KEY"]
    cfg = json.loads((ROOT.parent / ".mcp.json").read_text())
    return cfg["mcpServers"]["wibe"]["headers"]["X-API-Key"]


def call(method, path, body=None, dry_run=False):
    if dry_run and method != "GET":
        print(f"  [dry-run] {method} {path}")
        return None
    req = urllib.request.Request(
        BASE + path,
        method=method,
        data=None if body is None else json.dumps(body).encode(),
        headers={"X-API-Key": api_key(), "Content-Type": "application/json"},
    )
    try:
        with urllib.request.urlopen(req, timeout=30, context=SSL_CONTEXT) as resp:
            raw = resp.read()
            return json.loads(raw) if raw else None
    except urllib.error.HTTPError as e:
        sys.exit(f"{method} {path}: HTTP {e.code}: {e.read().decode()}")


def prompt_text(team_dir, agent):
    text = (team_dir / agent["prompt"]).read_text().strip()
    if agent.get("common", True):
        text += "\n\n" + (team_dir / "_common.md").read_text().strip()
    return text + "\n"


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("team")
    ap.add_argument("--dry-run", action="store_true")
    args = ap.parse_args()

    team_dir = ROOT / "teams" / args.team
    spec = json.loads((team_dir / "team.json").read_text())
    team_id = spec["team_id"]

    teams = call("GET", f"/projects/{PROJECT_ID}/teams")
    teams = teams.get("teams", teams) if isinstance(teams, dict) else teams
    team = next((t for t in teams if t["id"] == team_id), None)
    if team is None:
        sys.exit(f"team {team_id} not found in project {PROJECT_ID}")
    existing = {a["name"]: a for a in team["agents"]}

    print(f"team {team['name']} ({team_id})")
    call("PATCH", f"/projects/{PROJECT_ID}/teams/{team_id}",
         {"description": spec["description"], "accepts_delegation": spec["accepts_delegation"]}, args.dry_run)

    for agent in spec["agents"]:
        body = {"system_prompt": prompt_text(team_dir, agent)}
        if agent.get("role_description"):
            body["role_description"] = agent["role_description"]
        if agent.get("model"):  # без поля модель агента не трогаем: остаётся та, что задана в PolyMaths
            body["model"] = agent["model"]
        if agent["name"] in existing:
            print(f"  update {agent['name']}")
            call("PATCH", f"/projects/{PROJECT_ID}/team/agents/{existing[agent['name']]['id']}",
                 dict(body, is_active=True), args.dry_run)
        else:
            print(f"  create {agent['name']}")
            call("POST", f"/projects/{PROJECT_ID}/teams/{team_id}/agents",
                 dict(body, name=agent["name"], role=agent["role"], execution_kind="sandbox"), args.dry_run)

    for name in spec.get("deactivate", []):
        if name in existing and existing[name]["is_active"]:
            print(f"  deactivate {name}")
            call("PATCH", f"/projects/{PROJECT_ID}/team/agents/{existing[name]['id']}",
                 {"is_active": False}, args.dry_run)

    for c in spec.get("contracts", []):
        print(f"  contract {c['kind']}")
        schema = json.loads((ROOT / "contracts" / c["schema"]).read_text())
        call("PUT", f"/projects/{PROJECT_ID}/teams/{team_id}/contracts/{c['kind']}",
             {"json_schema": schema, "description": c["description"], "audience": c["audience"],
              "is_enabled": True}, args.dry_run)


if __name__ == "__main__":
    main()
