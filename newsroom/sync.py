#!/usr/bin/env python3
"""Синхронизирует команду PolyMaths с её описанием в newsroom/teams/<team>/team.json.

Создаёт команду, если у неё ещё нет `team_id` (и записывает id в team.json), создаёт недостающих агентов,
обновляет промпты и описания ролей, настройки песочницы агентов (`settings`: Docker, лимиты), выключает лишних
агентов, обновляет описание команды и контракты артефактов. Потом — skills проекта из newsroom/skills/<name>/:
файлы skill'а и список агентов, которым он выдан (поле `skills` агентов во всех team.json). Источник правды —
репозиторий.

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
    if not team_id:
        if args.dry_run:
            print(f"  [dry-run] create team {spec['name']} ({spec['type']})")
            return
        same_name = [t for t in list_teams() if t["name"] == spec["name"]]
        if same_name:  # a run that created it but whose team.json edit was lost
            sys.exit(f"team {spec['name']!r} already exists ({same_name[0]['id']}): put its id into team.json")
        created = call("POST", f"/projects/{PROJECT_ID}/teams",
                       {"name": spec["name"], "type": spec["type"], "description": spec["description"],
                        "accepts_delegation": spec["accepts_delegation"]})
        team_id = (created.get("team") or created)["id"]
        spec["team_id"] = team_id
        (team_dir / "team.json").write_text(json.dumps(spec, ensure_ascii=False, indent=2) + "\n")
        print(f"created team {spec['name']} {team_id} (written to team.json)")

    teams = list_teams()
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
            agent_id = existing[agent["name"]]["id"]
            call("PATCH", f"/projects/{PROJECT_ID}/team/agents/{agent_id}", dict(body, is_active=True), args.dry_run)
        else:
            print(f"  create {agent['name']}")
            # A router is an LLM call without a sandbox; every other role runs in one.
            kind = "llm" if agent["role"] == "router" else "sandbox"
            call("POST", f"/projects/{PROJECT_ID}/teams/{team_id}/agents",
                 dict(body, name=agent["name"], role=agent["role"], execution_kind=kind), args.dry_run)
            agent_id = None if args.dry_run else next(
                (a["id"] for t in list_teams() if t["id"] == team_id for a in t["agents"] if a["name"] == agent["name"]), None)
            if agent_id is None and not args.dry_run:
                sys.exit(f"agent {agent['name']} was created but is not listed in team {team_id}")
        if agent.get("settings"):
            print(f"  settings {agent['name']}: {agent['settings']}")
            if agent_id:
                call("PUT", f"/agents/{agent_id}/settings", agent["settings"], args.dry_run)

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

    sync_skills(args.dry_run)


def list_teams():
    teams = call("GET", f"/projects/{PROJECT_ID}/teams")
    return teams.get("teams", teams) if isinstance(teams, dict) else teams


def sync_skills(dry_run):
    """Each newsroom/skills/<name>/ → the project skill <name> with exactly those files, given to the agents
    whose `skills` (in any team.json) name it. A skill no agent names is left as it is."""
    teams = {t["id"]: t for t in list_teams()}
    wanted: dict[str, list[str]] = {}
    for spec_path in sorted((ROOT / "teams").glob("*/team.json")):
        spec = json.loads(spec_path.read_text())
        team = teams.get(spec.get("team_id"))
        if team is None:
            continue
        ids = {a["name"]: a["id"] for a in team["agents"]}
        for agent in spec["agents"]:
            for skill in agent.get("skills", []):
                if agent["name"] in ids:
                    wanted.setdefault(skill, []).append(ids[agent["name"]])
    current = call("GET", f"/projects/{PROJECT_ID}/agent-skills")
    current = {s["name"]: s for s in (current.get("skills", current) if isinstance(current, dict) else current)}
    for name, agent_ids in sorted(wanted.items()):
        skill_dir = ROOT / "skills" / name
        if not skill_dir.is_dir():
            sys.exit(f"skill {name!r} is named in a team.json but newsroom/skills/{name}/ does not exist")
        files = {f.name: f.read_text() for f in sorted(skill_dir.iterdir()) if f.is_file()}
        description = next((line.split(":", 1)[1].strip() for line in files.get("SKILL.md", "").splitlines()
                            if line.startswith("description:")), name)
        if name in current:
            print(f"  skill {name}: update files, {len(agent_ids)} agent(s)")
            call("PUT", f"/projects/{PROJECT_ID}/agent-skills/{current[name]['id']}",
                 {"name": name, "description": description, "files": files, "is_active": True}, dry_run)
            call("PUT", f"/projects/{PROJECT_ID}/agent-skills/{current[name]['id']}/agents",
                 {"agent_ids": sorted(set(agent_ids)), "applies_to_all": False}, dry_run)
        else:
            print(f"  skill {name}: create, {len(agent_ids)} agent(s)")
            call("POST", f"/projects/{PROJECT_ID}/agent-skills",
                 {"name": name, "description": description, "files": files, "is_active": True, "source": "path",
                  "agent_ids": sorted(set(agent_ids)), "applies_to_all_agents": False}, dry_run)


if __name__ == "__main__":
    main()
