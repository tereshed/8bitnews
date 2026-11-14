#!/usr/bin/env python3
"""Extract stage verdicts from structured artifacts and exact review bindings."""
from __future__ import annotations

import argparse
import json
from pathlib import Path

from hublib import SCRIPT_PROBLEM_PREFIX, artifact, artifact_details

# Studio checks only a change to the script can pass: the words don't fit the channel's length, or a picture
# named in --redraw became a neutral card again. Any other failed check is Video's.
SCRIPT_CHECKS = ("duration", "picture_fallback_redrawn")
NOTES_IN_SUMMARY = 1500  # characters: the router sees only the summary, and rule 8 hands these notes to Script


def build_report(result_id: str, delegation: dict, fetch=artifact, fetch_details=artifact_details) -> dict:
    team = delegation["team"]
    if team not in ("Research", "Script", "Video", "QA", "Legal"):
        raise ValueError("Unsupported content team")
    ids = {item["kind"]: item["id"] for item in delegation.get("artifacts", [])}
    report = {"team": team, "state": delegation["state"], "delegation_result_id": result_id,
              "artifacts": ids, "verdict": "missing", "fix_owners": {}, "notes": []}
    if report["state"] != "done":
        return report
    kind = {"Research": "research_dossier", "Script": "script", "Video": "video_package",
            "QA": "validation_report", "Legal": "legal_review"}[team]
    if kind not in ids:
        if team == "Video" and delegation.get("summary", "").strip().lstrip("\"'«`* ").startswith(SCRIPT_PROBLEM_PREFIX):
            report.update(verdict="fail", fix_owners={"script": ["studio"]}, notes=[delegation["summary"]])
        return report
    if team in ("Research", "Script"):
        report["verdict"] = "not_approved"
        # fact_checker's review reaches PolyMaths under either kind, with the same content.
        names = ("review", "research_review") if team == "Research" else ("review",)
        reviews = [fetch_details(ids[name], name) for name in names if name in ids]
        current = [review for review in reviews if review.get("parent_id") == ids[kind]]
        # A review bound to nothing could be the newer one, so it can't be set aside as stale.
        unbound = any(not review.get("parent_id") for review in reviews)
        if current and not unbound and all(review["content"].get("decision") == "approved" for review in current):
            report["verdict"] = "approved"
        elif unbound or (reviews and not current):
            report["notes"].append("Review is not bound to the latest output artifact")
        return report
    doc = fetch(ids[kind], kind)

    def fix(owner, issue_id):
        if owner not in ("script", "research", "video", "editor", "none") or not issue_id:
            raise ValueError("Invalid fix owner or missing issue ID")
        report["fix_owners"].setdefault(owner, []).append(issue_id)

    if team == "Video":
        validation = doc["validate"]
        failed = [check for check in validation["checks"] if not check["ok"]]
        report["verdict"] = "built" if validation["passed"] and not failed and validation["checks"] else "fail"
        for check in failed:
            fix("script" if check["id"] in SCRIPT_CHECKS else "video", check["id"])
            report["notes"].append(f"{check['id']}: {check.get('note', '')}")
        if report["verdict"] == "fail" and not failed:
            fix("video", "inconsistent_validation")
    else:
        report["verdict"] = doc["verdict"]
        if team == "Legal":
            report["notes"].append(f"mode: {doc['mode']}")
            report["platforms"] = {name: info["verdict"] for name, info in doc["platforms"].items()}
        for issue in doc.get("issues", []):
            relevant = (issue.get("severity") in ("major", "blocker") if team == "QA"
                        else issue.get("resolved_by_exclusion") is not True)
            if relevant:
                fix(issue["fix_owner"], issue["id"])
                if report["verdict"] == "pass" and (team == "QA" or issue.get("severity") in ("blocker", "high")):
                    report["verdict"] = "fail" if team == "QA" else "fix_required"
        if team == "Legal" and report["verdict"] == "pass" and "pass" not in report["platforms"].values():
            # hub refuses a release with nowhere to go: stop before Video, QA and Legal final are spent on it
            report["verdict"] = "fix_required"
            fix("editor", "no_platform")
            report["notes"].append("Legal passed the video but left no platform to publish on")
        if team == "QA" and (not doc["technical"]["passed"] or doc["technical"]["failed_checks"]):
            report["verdict"] = "fail"
            if not report["fix_owners"]:
                fix("video", "technical")
    return report


def summary(report: dict) -> str:
    mode = next((n.removeprefix("mode: ") for n in report["notes"] if n.startswith("mode: ")), "")
    team = report["team"] + (f" {mode}" if mode else "")
    ids = ", ".join(f"{kind}={value}" for kind, value in sorted(report["artifacts"].items()))
    fixes = ", ".join(f"{owner}=[{','.join(ids)}]" for owner, ids in sorted(report["fix_owners"].items()))
    platforms = ", ".join(f"{name}={verdict}" for name, verdict in sorted(report.get("platforms", {}).items()))
    line = f"{team} {report['state']}: {report['verdict']}. ids: {ids}. fix: {fixes}. platforms: {platforms}"
    if report["team"] == "Video" and report["verdict"] == "fail" and report["notes"]:
        line += f". notes: {' | '.join(report['notes'])[:NOTES_IN_SUMMARY]}"
    return line


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("delegation_result_id")
    parser.add_argument("--out", required=True)
    args = parser.parse_args()
    report = build_report(args.delegation_result_id, artifact(args.delegation_result_id, "delegation_result"))
    Path(args.out).write_text(json.dumps(report, ensure_ascii=False, indent=2))
    Path(args.out + ".summary.txt").write_text(summary(report))
    print(summary(report))


if __name__ == "__main__":
    main()
