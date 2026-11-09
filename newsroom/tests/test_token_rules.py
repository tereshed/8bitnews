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


def test_review_kit_reports_the_attempt_it_kept_not_the_last_one():
    import sys
    sys.path.insert(0, str(TEAMS.parent / "tools"))
    from video_review_kit import compact

    package = {"images": {"cost_usd": 1, "scenes": [{
        "id": "S1", "file": "images/S1.png", "accepted": "a/0.png", "kept_with": ["extra_people"], "fallback": False,
        "attempts": [{"file": "a/0.png", "found": ["extra_people"], "prompt": "p"},
                     {"file": "a/1.png", "found": ["real_person_likeness"], "prompt": "p"}]}]}}
    scene = compact(package)["images"]["scenes"][0]
    assert scene["found"] == ["extra_people"] and scene["kept_with"] == ["extra_people"] and scene["attempts"] == 2
    assert compact(package)["images"]["cost_usd"] == 1 and "prompt" not in str(scene)

