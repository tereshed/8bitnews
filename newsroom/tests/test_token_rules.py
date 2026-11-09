import json
from pathlib import Path

import pytest

TEAMS = Path(__file__).resolve().parents[1] / "teams"


@pytest.mark.parametrize("common", sorted(TEAMS.glob("*/_common.md")), ids=lambda p: p.parent.name)
def test_every_team_is_told_not_to_write_project_notes(common):
    assert "**Заметки проекта.**" in common.read_text()


@pytest.mark.parametrize("manifest", sorted(TEAMS.glob("*/team.json")), ids=lambda p: p.parent.name)
def test_agent_profile_matches_what_the_sandbox_runs(manifest):
    for agent in json.loads(manifest.read_text())["agents"]:
        if agent.get("code_backend") != "claude-code":
            continue
        backend = agent["settings"]["code_backend_settings"]
        assert backend["model"] == agent["model"], agent["name"]
        assert backend["env"]["DEVTEAM_AGENT_REASONING_EFFORT"] == agent["reasoning_effort"], agent["name"]
