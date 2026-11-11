import sys
from datetime import datetime, timedelta, timezone
from pathlib import Path

import pytest

ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT / "newsroom"))
sys.path.insert(0, str(ROOT / "newsroom/tools"))
import automation  # noqa: E402
from stage_report import build_report, summary  # noqa: E402


class Session:
    def __init__(self):
        self.existing, self.created = [], []

    def tool(self, name, args):
        if name == "scheduled_task_list":
            return {"scheduled_tasks": self.existing, "total": len(self.existing)}
        assert name == "scheduled_task_create"
        self.created.append(args)
        self.existing.append(args)


def test_schedule_creation_uses_channel_timezone_and_is_repeatable():
    desired = automation.schedules(ROOT)
    assert {s["timezone"] for s in desired} == {"Europe/Madrid"}
    assert all(s["work_intent"] == "explanation" and s["team_id"] for s in desired)
    session = Session()
    automation.apply_schedules(session, desired)
    automation.apply_schedules(session, desired)
    # 8bitnews-es: five a day since 11.11.2026, each run told its number and that the others may still be under way
    assert len(session.created) == len(desired) == 5
    assert [s["name"] for s in desired] == [f"8bitnews/8bitnews-es/{i}" for i in range(1, 6)]
    assert "Запуск 3 из 5 за день" in desired[2]["description"]
    session.existing[0] = dict(session.existing[0], cron_expression="0 7 * * *")
    with pytest.raises(ValueError, match="differs"):
        automation.apply_schedules(session, desired)


def test_a_paused_channel_gets_no_schedule_and_its_switched_off_one_is_left_alone():
    desired = automation.schedules(ROOT)
    assert not [s for s in desired if "8bitnews-us" in s["name"]]
    paused = {"name": "8bitnews/8bitnews-us/1", "is_active": False}
    session = Session()
    session.existing.append(paused)
    automation.apply_schedules(session, desired)
    assert paused in session.existing and paused not in session.created


def test_active_without_artifacts_is_stalled_and_receipt_does_not_mean_published():
    now = datetime.now(timezone.utc)
    task = {"id": "task", "status": "active", "created_at": (now - timedelta(hours=1)).isoformat()}
    assert automation.progress(task, [], now, 30)["stalled"]
    report = automation.progress(task, [{"kind": "release_receipt", "created_at": now.isoformat()}], now, 30)
    assert report["ready_for_moderation"] and not report["stalled"]
    assert "published" not in report


@pytest.mark.parametrize("state", ["failed", "needs_human", "expired", "cancelled"])
def test_failed_delegation_never_becomes_pass(state):
    def fetch(*args):
        raise AssertionError("failed stage must not fetch/approve an old artifact")
    result = build_report("delegation", {"team": "QA", "state": state, "artifacts": []}, fetch)
    assert result["verdict"] == "missing"


def test_video_checks_override_an_inconsistent_pass():
    delegation = {"team": "Video", "state": "done", "artifacts": [{"kind": "video_package", "id": "video-id"}]}
    result = build_report("delegation", delegation, lambda *_: {"validate": {"passed": True, "checks": [{"id": "duration", "ok": False, "note": "too long"}]}})
    assert result["verdict"] == "fail" and result["fix_owners"] == {"script": ["duration"]}
    assert "video_package=video-id" in summary(result)


def _video_report(*checks):
    delegation = {"team": "Video", "state": "done", "artifacts": [{"kind": "video_package", "id": "video-id"}]}
    return build_report("delegation", delegation, lambda *_: {"validate": {"passed": False, "checks": list(checks)}})


def test_a_neutral_card_goes_back_to_video_without_qa():
    result = _video_report({"id": "format", "ok": True},
                           {"id": "picture_fallback", "ok": False, "note": "neutral card: S2 (3 attempt(s): text)"})
    assert result["verdict"] == "fail" and result["fix_owners"] == {"video": ["picture_fallback"]}
    assert "fix: video=[picture_fallback]" in summary(result)


def test_a_card_on_a_redrawn_picture_goes_to_script_and_the_router_sees_the_scene_and_the_finding():
    """The router sees the summary alone, and rule 8 hands Script the notes."""
    note = "redrawing did not help: S5 (3 attempt(s): flags_or_seals, extra_people)"
    result = _video_report({"id": "picture_fallback_redrawn", "ok": False, "note": note})
    assert result["verdict"] == "fail" and result["fix_owners"] == {"script": ["picture_fallback_redrawn"]}
    assert "fix: script=[picture_fallback_redrawn]" in summary(result)
    assert "S5 (3 attempt(s): flags_or_seals, extra_people)" in summary(result)


def test_a_built_video_keeps_its_summary_short():
    delegation = {"team": "Video", "state": "done", "artifacts": [{"kind": "video_package", "id": "video-id"}]}
    result = build_report("delegation", delegation, lambda *_: {"validate": {"passed": True, "checks": [{"id": "format", "ok": True, "note": "ok"}]}})
    assert result["verdict"] == "built" and "notes:" not in summary(result)


def test_legal_exclusion_is_not_an_open_fix_and_mode_is_preserved():
    delegation = {"team": "Legal", "state": "done", "artifacts": [{"kind": "legal_review", "id": "legal-id"}]}
    result = build_report("delegation", delegation, lambda *_: {"mode": "final", "verdict": "pass", "platforms": {"youtube": {"verdict": "pass"}}, "issues": [{"id": "P1", "fix_owner": "video", "resolved_by_exclusion": True}]})
    assert result["verdict"] == "pass" and not result["fix_owners"]
    assert summary(result).startswith("Legal final done: pass")
