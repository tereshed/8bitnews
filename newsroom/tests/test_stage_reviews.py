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


@pytest.mark.parametrize("parent,decision,expected", [
    ("current", "approved", "approved"),
    ("previous", "approved", "not_approved"),
    ("current", "changes_requested", "not_approved"),
])
def test_research_review_under_its_other_kind_counts_the_same(parent, decision, expected):
    delegation = {"team": "Research", "state": "done", "artifacts": [
        {"kind": "research_dossier", "id": "current"}, {"kind": "research_review", "id": "r"},
    ]}
    asked = []

    def details(artifact_id, kind):
        asked.append(kind)
        return {"parent_id": parent, "content": {"decision": decision}}

    assert build_report("d", delegation, fetch_details=details)["verdict"] == expected
    assert asked == ["research_review"]


def test_a_second_review_of_the_same_dossier_that_disagrees_stops_it():
    delegation = {"team": "Research", "state": "done", "artifacts": [
        {"kind": "research_dossier", "id": "current"}, {"kind": "review", "id": "a"}, {"kind": "research_review", "id": "b"},
    ]}
    decisions = {"a": "approved", "b": "changes_requested"}
    report = build_report("d", delegation, fetch_details=lambda i, _: {
        "parent_id": "current", "content": {"decision": decisions[i]},
    })
    assert report["verdict"] == "not_approved"


def test_a_stale_review_under_the_other_kind_doesnt_block_the_current_one():
    delegation = {"team": "Research", "state": "done", "artifacts": [
        {"kind": "research_dossier", "id": "current"}, {"kind": "review", "id": "new"}, {"kind": "research_review", "id": "old"},
    ]}
    parents = {"new": "current", "old": "previous"}
    decisions = {"new": "approved", "old": "changes_requested"}
    report = build_report("d", delegation, fetch_details=lambda i, _: {
        "parent_id": parents[i], "content": {"decision": decisions[i]},
    })
    assert report["verdict"] == "approved"


def test_script_team_ignores_the_research_kind():
    delegation = {"team": "Script", "state": "done", "artifacts": [
        {"kind": "script", "id": "current"}, {"kind": "research_review", "id": "r"},
    ]}
    report = build_report("d", delegation, fetch_details=lambda *_: pytest.fail("not a Script review"))
    assert report["verdict"] == "not_approved"


@pytest.mark.parametrize("platforms,verdict,owners", [
    ({"youtube": {"verdict": "exclude"}}, "fix_required", {"editor": ["no_platform"]}),
    ({"youtube": {"verdict": "pass"}, "tiktok": {"verdict": "exclude"}}, "pass", {}),
])
def test_a_legal_pass_needs_a_platform_to_publish_on(platforms, verdict, owners):
    delegation = {"team": "Legal", "state": "done", "artifacts": [{"kind": "legal_review", "id": "r"}]}
    doc = {"mode": "script", "verdict": "pass", "platforms": platforms, "issues": []}
    report = build_report("d", delegation, fetch=lambda _id, _kind: doc)
    assert report["verdict"] == verdict and report["fix_owners"] == owners

