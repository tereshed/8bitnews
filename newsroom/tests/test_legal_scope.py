import json
import sys
from pathlib import Path

import pytest

ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT / "newsroom" / "tools"))
from kb_section import section, slug  # noqa: E402
from legal_final_scope import decide as final_scope  # noqa: E402
from script_diff import ESCALATE_PREFIX, decide, escalation  # noqa: E402


def scene(scene_id="S1", **fields):
    return {"id": scene_id, "shot": "scene", "blocks": ["known"], "visual": "A quiet street at dusk, no people.",
            "fact_ids": ["F1"], "narration": "The council voted on Monday.", "real_people": [],
            "on_screen_text": "Council vote", "source_plate": "Source: AP · Nov 3", "duration_s": 5, "word_count": 5, **fields}


def script(*scenes, **fields):
    return {"channel_id": "8bitnews-us", "language": "en", "dossier": "d1", "hook_type": "new_fact", "serious_mode": False,
            "scenes": list(scenes) or [scene()], "cover": {"frame": "S1", "visual": "x", "text": "Council vote"},
            "metadata": {"youtube": {"title": "Council vote", "summary": "The council voted.", "hashtags": ["#8BitNews"], "tags": []}},
            **fields}


REVIEW = {"mode": "script", "channel_id": "8bitnews-us", "country": "US", "verdict": "pass",
          "platforms": {"youtube": {"verdict": "pass"}}, "issues": [{"id": "R1", "severity": "low", "fix_owner": "script"}],
          "required_disclosures": [], "publish_notes": ["СТОП: проверь исход голосования"], "final_checks": ["AI label"]}
OLD_META = {"id": "a", "created_at": "2026-11-09T10:00:00Z"}
REVIEW_META = {"id": "r", "created_at": "2026-11-09T11:00:00Z"}
NEW_META = {"id": "b", "created_at": "2026-11-09T12:00:00Z"}


def run(old, new, review=REVIEW, old_meta=OLD_META, new_meta=NEW_META, review_meta=REVIEW_META):
    return decide(old, new, review, old_meta, new_meta, review_meta)


def test_a_point_fix_needs_no_full_review_and_keeps_the_publish_conditions():
    result = run(script(), script(scene(narration="The council voted on Monday, officials said.")))
    assert result["must_escalate"] is False, result["reasons"]
    assert [c["where"] for c in result["changes"]] == ["S1.narration"]
    assert result["carry_over"]["publish_notes"] == REVIEW["publish_notes"]
    assert result["previous_issues"][0]["id"] == "R1"


def test_changing_only_a_picture_prompt_needs_no_full_review():
    assert run(script(), script(scene(visual="Empty chairs in a hall, no flags.")))["must_escalate"] is False


@pytest.mark.parametrize("change,reason", [
    (lambda: script(scene(narration="Mayor Novartis voted on Monday.")), "новые имена"),
    (lambda: script(scene(narration="The council voted 7 to 2 on Monday.")), "новые цифры"),
    (lambda: script(scene(fact_ids=["F1", "F9"])), "новые факты"),
    (lambda: script(scene(real_people=["Jane Doe"])), "новые люди"),
    (lambda: script(scene(), scene("S2")), "новые сцены"),
    (lambda: script(dossier="d2"), "другому досье"),
    (lambda: script(serious_mode=True), "серьёзный режим"),
    (lambda: script(scene(), metadata={"youtube": {"title": "Council vote in Boston", "summary": "The council voted."}}),
     "новые имена"),
])
def test_anything_beyond_a_point_fix_goes_to_the_whole_team(change, reason):
    result = run(script(), change())
    assert result["must_escalate"] is True
    assert any(reason in r for r in result["reasons"]), result["reasons"]


def test_more_than_three_changed_scenes_go_to_the_whole_team():
    ids = [f"S{i}" for i in range(1, 6)]
    old = script(*[scene(i) for i in ids])
    new = script(*[scene(i, on_screen_text="Council votes") for i in ids])
    result = run(old, new)
    assert result["must_escalate"] is True and any("изменено сцен" in r for r in result["reasons"])


@pytest.mark.parametrize("old_meta,new_meta,review_meta,review", [
    (OLD_META, OLD_META, REVIEW_META, REVIEW),  # the same script twice: a zero diff must not pass as reviewed
    (NEW_META, OLD_META, REVIEW_META, REVIEW),  # versions swapped
    ({"id": "a", "created_at": "2026-11-09T11:30:00Z"}, NEW_META, REVIEW_META, REVIEW),  # review predates the "old" script
    ({"id": "a"}, NEW_META, REVIEW_META, REVIEW),  # no dates
    (OLD_META, NEW_META, REVIEW_META, {**REVIEW, "mode": "final"}),
    (OLD_META, NEW_META, REVIEW_META, {**REVIEW, "verdict": "kill"}),
])
def test_doubt_about_which_version_was_reviewed_goes_to_the_whole_team(old_meta, new_meta, review_meta, review):
    assert run(script(), script(), review, old_meta, new_meta, review_meta)["must_escalate"] is True


def test_escalation_review_holds_the_video_and_satisfies_the_contract():
    jsonschema = pytest.importorskip("jsonschema")
    doc = escalation(REVIEW, ["новые цифры: 7"])
    jsonschema.validate(doc, json.loads((ROOT / "newsroom/contracts/legal_review.schema.json").read_text()))
    assert doc["verdict"] == "fix_required"
    assert doc["issues"][0]["fix_owner"] == "script"  # never "none": that route would send the script to Video
    assert doc["publish_notes"] == REVIEW["publish_notes"]
    assert ESCALATE_PREFIX == "ПОЛНАЯ ПРОВЕРКА:"


def test_stage_report_keeps_an_escalation_out_of_video():
    from stage_report import build_report

    delegation = {"team": "Legal", "state": "done", "artifacts": [{"kind": "legal_review", "id": "r2"}]}
    report = build_report("d", delegation, fetch=lambda _id, _kind: escalation(REVIEW, ["x"]))
    assert report["verdict"] == "fix_required" and report["fix_owners"] == {"script": ["X0"]}


PACKAGE = {"script": "a"}
FINAL_REVIEW = {"id": "r", "created_at": REVIEW_META["created_at"], "content": REVIEW}


def test_final_looks_only_at_what_is_new_when_the_reviewed_script_was_built():
    assert final_scope(OLD_META, FINAL_REVIEW, PACKAGE) == {"scope": "new_only", "reasons": []}


def test_video_only_findings_keep_the_narrow_scope():
    review = {**FINAL_REVIEW, "content": {**REVIEW, "verdict": "fix_required", "issues": [
        {"id": "P1", "severity": "high", "fix_owner": "video"}]}}
    assert final_scope(OLD_META, review, PACKAGE)["scope"] == "new_only"


@pytest.mark.parametrize("script_meta,review,package", [
    (NEW_META, FINAL_REVIEW, {"script": "b"}),  # script newer than the review
    (OLD_META, FINAL_REVIEW, {"script": "other"}),  # built from something else
    (OLD_META, FINAL_REVIEW, {}),
    ({"id": "a"}, FINAL_REVIEW, PACKAGE),  # no date
    (OLD_META, {**FINAL_REVIEW, "content": {**REVIEW, "mode": "final"}}, PACKAGE),
    (OLD_META, {**FINAL_REVIEW, "content": {**REVIEW, "verdict": "kill"}}, PACKAGE),
    (OLD_META, {**FINAL_REVIEW, "content": escalation(REVIEW, ["x"])}, PACKAGE),  # an escalation is no pass
    (OLD_META, {"id": "r", "content": REVIEW}, PACKAGE),
])
def test_final_checks_the_text_too_on_any_doubt(script_meta, review, package):
    assert final_scope(script_meta, review, package)["scope"] == "full"


KB = """# Title

## <a id="scope"></a>0. Общие принципы
intro

## 1. Диффамация
### <a id="defamation-standards"></a>1.1 Кто истец
rule one
### 1.2 Мнение и факт
rule two
```
## not a heading
```
## 2. Право на образ
rule three
"""


@pytest.mark.parametrize("anchor,has,lacks", [
    ("scope", "intro", "Диффамация"),
    ("defamation-standards", "rule one", "rule two"),
    ("#defamation-standards", "rule one", "rule two"),
    ("1-диффамация", "rule two", "rule three"),
    ("1", "rule two", "rule three"),
    ("1.2", "not a heading", "rule three"),
    ("2-право-на-образ", "rule three", "rule two"),
])
def test_section_returns_the_whole_section_and_no_more(anchor, has, lacks):
    found = section(KB, anchor)
    assert has in found and lacks not in found


def test_unknown_anchor_is_none_not_a_guess():
    assert section(KB, "nope") is None


def test_slug_matches_the_links_past_reports_use():
    assert slug("4. Монетизация и право на рекомендации") == "4-монетизация-и-право-на-рекомендации"
    assert slug("7. Метаданные: заголовки, обложки, хэштеги") == "7-метаданные-заголовки-обложки-хэштеги"


@pytest.mark.parametrize("anchor", ["4-монетизация-и-право-на-рекомендации", "7-метаданные-заголовки-обложки-хэштеги",
                                    "9-чек-лист-красные-флаги-для-ревьюера", "4"])
def test_past_youtube_references_resolve(anchor):
    assert section((ROOT / "platforms/youtube/rules.md").read_text(), anchor)
