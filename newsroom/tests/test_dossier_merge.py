import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "tools"))
from dossier_merge import merge  # noqa: E402

FACTS = {"as_of": "2026-11-09T08:00:00Z", "facts": [{"id": "F1", "claim": "x"}], "timeline": [{"date": "2026-11-08"}],
         "unknowns": ["why"], "_run_diagnostics": [{"code": "result_submitted"}]}
LOCAL = {"outlets_reviewed": [], "groups": [{"id": "L1"}], "polls": [{"org": "AP"}]}
WORLD = {"outlets_reviewed": [], "clusters": [{"id": "W1", "contrast_with_local": "draft"}], "coverage_notes": ["thin"]}


def test_merge_moves_the_reports_unchanged():
    dossier = merge(FACTS, LOCAL, WORLD)
    assert dossier == {"as_of": "2026-11-09T08:00:00Z", "facts": FACTS["facts"], "timeline": FACTS["timeline"],
                       "local_groups": LOCAL["groups"], "polls": LOCAL["polls"], "world_clusters": WORLD["clusters"],
                       "unknowns": FACTS["unknowns"]}


def test_merge_leaves_the_analyst_fields_empty_so_the_contract_rejects_a_forgotten_one():
    dossier = merge(FACTS, LOCAL, WORLD)
    assert not {"story", "channel_id", "language", "review", "risks"} & dossier.keys()


def test_a_story_with_no_world_press_keeps_empty_clusters():
    assert merge(FACTS, LOCAL, {"outlets_reviewed": []})["world_clusters"] == []


def test_as_of_is_the_earliest_report():
    assert merge(FACTS, dict(LOCAL, as_of="2026-11-09T07:00:00Z"), WORLD)["as_of"] == "2026-11-09T07:00:00Z"
