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


@pytest.mark.parametrize("platforms,verdict,owners", [
    ({"youtube": {"verdict": "exclude"}}, "fix_required", {"editor": ["no_platform"]}),
    ({"youtube": {"verdict": "pass"}, "tiktok": {"verdict": "exclude"}}, "pass", {}),
])
def test_a_legal_pass_needs_a_platform_to_publish_on(platforms, verdict, owners):
    delegation = {"team": "Legal", "state": "done", "artifacts": [{"kind": "legal_review", "id": "r"}]}
    doc = {"mode": "script", "verdict": "pass", "platforms": platforms, "issues": []}
    report = build_report("d", delegation, fetch=lambda _id, _kind: doc)
    assert report["verdict"] == verdict and report["fix_owners"] == owners

