import sys
from pathlib import Path

import pytest

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "tools"))
from stage_report import build_report  # noqa: E402


@pytest.mark.parametrize("team,kind", [("Research", "research_dossier"), ("Script", "script")])
@pytest.mark.parametrize("parent,decision,expected", [
    ("current", "approved", "approved"),
    ("previous", "approved", "not_approved"),
    (None, "approved", "not_approved"),
    ("current", "changes_requested", "not_approved"),
])
def test_only_review_of_current_artifact_can_approve(team, kind, parent, decision, expected):
    delegation = {"team": team, "state": "done", "artifacts": [
        {"kind": kind, "id": "current"}, {"kind": "review", "id": "review"},
    ]}
    report = build_report("delegation", delegation, fetch_details=lambda *_: {
        "parent_id": parent, "content": {"decision": decision},
    })
    assert report["verdict"] == expected
