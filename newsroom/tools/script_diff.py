#!/usr/bin/env python3
"""Что изменилось в сценарии после прошлой проверки Legal и хватит ли проверки по изменениям:

    python3 newsroom/tools/script_diff.py --old <id script> --new <id script> --review <id legal_review> --out /tmp/escalate.json

Решение принимает код: `must_escalate: true` значит, что правка вышла за рамки точечной (новые люди, цифры, факты, сцены,
больше трёх изменённых сцен или неясно, о какой версии было ревью), и сценарий проверяет вся команда. Тогда в `--out`
лежит готовый `legal_review` для сдачи, а `summary` в выводе — его сводка. В сомнительном случае инструмент требует всю
команду, а не наоборот.
"""
from __future__ import annotations

import argparse
import json
import pathlib
import re
import sys

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parent))

MAX_CHANGED_SCENES = 3
ESCALATE_PREFIX = "ПОЛНАЯ ПРОВЕРКА:"
ESCALATION_ISSUE = "X0"
SCENE_TEXT = ("narration", "on_screen_text", "source_plate", "visual")
SCENE_OTHER = ("shot", "blocks", "fact_ids", "real_people")
TOP_LEVEL = ("hook_type", "serious_mode", "music", "pronunciations", "language", "channel_id")
WORD = re.compile(r"[^\W\d_][\w’'\-]*")
NUMBER = re.compile(r"\d(?:[\d.,:/%\-]*\d)?%?")
SENTENCE_END = re.compile(r"(^|[.!?:;«»\"“”…]\s*)$")


def public_text(script: dict) -> list[tuple[str, str, str]]:
    """Every (where, field kind, text) a viewer or a reader of the listing sees or the picture is drawn from."""
    out = []
    for scene in script.get("scenes", []):
        for field in SCENE_TEXT:
            if scene.get(field):
                out.append((f"{scene['id']}.{field}", field, str(scene[field])))
    cover = script.get("cover") or {}
    for field in ("text", "visual"):
        if cover.get(field):
            out.append((f"cover.{field}", "visual" if field == "visual" else "text", str(cover[field])))
    for platform, meta in sorted((script.get("metadata") or {}).items()):
        for field in ("title", "summary"):
            if meta.get(field):
                out.append((f"metadata.{platform}.{field}", "text", str(meta[field])))
        for field in ("hashtags", "tags"):
            if meta.get(field):
                out.append((f"metadata.{platform}.{field}", "text", " ".join(map(str, meta[field]))))
    return out


def vocabulary(script: dict) -> tuple[set, set]:
    words, numbers = set(), set()
    for _, _, text in public_text(script):
        words |= {w.lower() for w in WORD.findall(text)}
        numbers |= set(NUMBER.findall(text))
    return words, numbers


def new_names(text: str, kind: str, old_words: set) -> set:
    """Capitalised words the old script never used. A sentence-opening word counts unless the same text also uses it in
    lower case; in a picture prompt it never counts, since prompts carry no names and `real_people` covers people."""
    lowered = {m.group(0) for m in WORD.finditer(text) if not m.group(0)[0].isupper()}
    found = set()
    for match in WORD.finditer(text):
        word = match.group(0)
        if not word[0].isupper() or word.lower() in old_words:
            continue
        opening = SENTENCE_END.search(text[:match.start()])
        if opening and (kind == "visual" or word.lower() in lowered):
            continue
        found.add(word)
    return found


def compare(old: dict, new: dict) -> dict:
    changes, changed_scenes, reasons = [], [], []
    old_scenes = {s["id"]: s for s in old.get("scenes", [])}
    new_scenes = {s["id"]: s for s in new.get("scenes", [])}
    added = [s for s in new_scenes if s not in old_scenes]
    removed = [s for s in old_scenes if s not in new_scenes]
    if added:
        reasons.append(f"новые сцены: {', '.join(added)}")
    if removed:
        reasons.append(f"убраны сцены: {', '.join(removed)} (без них остальной текст может читаться иначе)")
    if [s for s in new_scenes if s in old_scenes] != [s for s in old_scenes if s in new_scenes]:
        reasons.append("изменён порядок сцен: обвинение могло оказаться рядом с другим человеком")
    old_facts = {f for s in old_scenes.values() for f in s.get("fact_ids") or []}
    for scene_id, scene in new_scenes.items():
        before = old_scenes.get(scene_id, {})
        touched = False
        for field in SCENE_TEXT + SCENE_OTHER:
            if scene.get(field) != before.get(field):
                changes.append({"where": f"{scene_id}.{field}", "before": before.get(field), "after": scene.get(field)})
                touched = True
        if touched:
            changed_scenes.append(scene_id)
        new_people = set(map(str, scene.get("real_people") or [])) - set(map(str, before.get("real_people") or []))
        if new_people:
            reasons.append(f"{scene_id}: новые люди в кадре: {', '.join(sorted(new_people))}")
        fresh_facts = set(scene.get("fact_ids") or []) - old_facts
        if fresh_facts:
            reasons.append(f"{scene_id}: новые факты досье: {', '.join(sorted(fresh_facts))}")
    if len(changed_scenes) > MAX_CHANGED_SCENES:
        reasons.append(f"изменено сцен: {len(changed_scenes)}, больше {MAX_CHANGED_SCENES}")
    for field in TOP_LEVEL:
        if new.get(field) != old.get(field):
            changes.append({"where": field, "before": old.get(field), "after": new.get(field)})
    if new.get("dossier") != old.get("dossier"):
        reasons.append("сценарий написан по другому досье")
    if new.get("serious_mode") != old.get("serious_mode"):
        reasons.append("изменён серьёзный режим")
    for field in ("cover", "metadata"):
        before, after = old.get(field) or {}, new.get(field) or {}
        for key in sorted(set(before) | set(after)):
            if before.get(key) != after.get(key):
                changes.append({"where": f"{field}.{key}", "before": before.get(key), "after": after.get(key)})

    old_words, old_numbers = vocabulary(old)
    previous = {where: text for where, _, text in public_text(old)}
    names, numbers = set(), set()
    for where, kind, text in public_text(new):
        if previous.get(where) == text:
            continue
        names |= new_names(text, kind, old_words)
        numbers |= set(NUMBER.findall(text)) - old_numbers
    if names:
        reasons.append(f"новые имена и названия: {', '.join(sorted(names))}")
    if numbers:
        reasons.append(f"новые цифры: {', '.join(sorted(numbers))}")
    return {"changed_scenes": changed_scenes, "removed_scenes": removed, "changes": changes, "reasons": reasons}


def version_problems(old_meta: dict, new_meta: dict, review_meta: dict, review: dict) -> list[str]:
    """Is this review about the old version, and did the new version come after it? Unknown counts as no."""
    problems = []
    if old_meta.get("id") and old_meta.get("id") == new_meta.get("id"):
        problems.append("старая и новая версии — один и тот же сценарий")
    if review.get("mode") != "script":
        problems.append(f"прошлое ревью не по сценарию (mode: {review.get('mode')})")
    if review.get("verdict") == "kill":
        problems.append("прошлое ревью сняло ролик (kill)")
    if any(issue.get("id") == ESCALATION_ISSUE for issue in review.get("issues", [])):
        problems.append("прошлое ревью — запрос полной проверки, а не сама проверка")
    if not old_meta.get("id") or review.get("script") != old_meta.get("id"):
        problems.append(f"прошлое ревью было о сценарии {review.get('script')}, а не о {old_meta.get('id')}")
    stamps = [old_meta.get("created_at"), review_meta.get("created_at"), new_meta.get("created_at")]
    if not all(stamps):
        problems.append("нет дат артефактов: не понять, о какой версии было ревью")
    elif not stamps[0] <= stamps[1] < stamps[2]:
        problems.append("порядок версий не сходится: ожидается старый сценарий, ревью, новый сценарий")
    return problems


def escalation(review: dict, reasons: list[str], script_id: str) -> dict:
    """A valid legal_review that holds the video and sends the script to the whole team.

    If a router closes the task on it, `fix_owner: editor` stops the run for a human: the script reaches neither Video
    nor another round of diff review."""
    text = "; ".join(reasons)
    return {
        "mode": "script",
        "channel_id": review.get("channel_id"),
        "country": review.get("country"),
        "script": script_id,
        "verdict": "fix_required",
        "platforms": {name: {"verdict": "fix_required", "reason": "Full review required"}
                      for name in (review.get("platforms") or {"youtube": None})},
        "issues": [{
            "id": ESCALATION_ISSUE, "family": "other", "severity": "high", "scope": review.get("country") or "unknown",
            "where": "script", "excerpt": text, "rule": {"ref": "newsroom/teams/legal/legal_lead.md", "summary": text},
            "scenario": "The script changed beyond a point fix, so the earlier review does not cover it.",
            "fix": "None yet: the Legal router runs the whole team on this script.", "fix_owner": "editor"}],
        "required_disclosures": review.get("required_disclosures") or [],
        "publish_notes": review.get("publish_notes") or [],
        "final_checks": review.get("final_checks") or [],
        "summary_ru": f"{ESCALATE_PREFIX} {text}"[:400],
        "checked_against": ["newsroom/tools/script_diff.py"],
    }


def decide(old: dict, new: dict, review: dict, old_meta: dict, new_meta: dict, review_meta: dict) -> dict:
    result = compare(old, new)
    reasons = version_problems(old_meta, new_meta, review_meta, review) + result.pop("reasons")
    return {"must_escalate": bool(reasons), "reasons": reasons, **result,
            "previous_issues": [{k: issue.get(k) for k in ("id", "severity", "fix_owner", "where", "fix")}
                                for issue in review.get("issues", [])],
            "carry_over": {"platforms": review.get("platforms"), "required_disclosures": review.get("required_disclosures"),
                           "publish_notes": review.get("publish_notes"), "final_checks": review.get("final_checks")}}


def main() -> int:
    from hublib import artifact_details

    ap = argparse.ArgumentParser(description=__doc__.split("\n\n")[0])
    ap.add_argument("--old", required=True, help="id сценария, который проверял Legal")
    ap.add_argument("--new", required=True, help="id нового сценария")
    ap.add_argument("--review", required=True, help="id legal_review по старому сценарию")
    ap.add_argument("--out", default="/tmp/legal_review_escalate.json")
    args = ap.parse_args()
    old = artifact_details(args.old, "script")
    new = artifact_details(args.new, "script")
    review = artifact_details(args.review, "legal_review")
    result = decide(old["content"], new["content"], review["content"], old, new, review)
    if result["must_escalate"]:
        pathlib.Path(args.out).write_text(json.dumps(escalation(review["content"], result["reasons"], new.get("id") or args.new), indent=2,
                                                     ensure_ascii=False))
        result["escalation_file"] = args.out
        result["summary"] = f"{ESCALATE_PREFIX} {'; '.join(result['reasons'])}"[:500]
    print(json.dumps(result, indent=1, ensure_ascii=False))
    return 0


if __name__ == "__main__":
    sys.exit(main())
