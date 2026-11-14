"""stage_report.py sends an approved research_dossier to hub to archive its sources, and nothing that goes wrong
there touches the report."""
import json
import sys
import threading
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path

import pytest

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "tools"))
import stage_report  # noqa: E402

DOSSIER = {"facts": [{"id": "F1", "primary_source": {"url": "https://www.rtve.es/noticias/x"}}]}
APPROVED = {"team": "Research", "state": "done", "delegation_result_id": "d",
            "artifacts": {"research_dossier": "dossier-id", "review": "review-id"},
            "verdict": "approved", "fix_owners": {}, "notes": []}


@pytest.fixture
def hub(monkeypatch):
    """A stand-in hub on a local port: records each request and answers as `reply` says."""
    calls, reply = [], {"status": 202, "body": {"queued": 1, "skipped_recent": 0}, "delay": 0}

    class Handler(BaseHTTPRequestHandler):
        def do_POST(self):
            body = json.loads(self.rfile.read(int(self.headers["Content-Length"])))
            calls.append({"path": self.path, "auth": self.headers["Authorization"], "body": body})
            time.sleep(reply["delay"])
            data = json.dumps(reply["body"]).encode()
            try:
                self.send_response(reply["status"])
                self.send_header("Content-Type", "application/json")
                self.send_header("Content-Length", str(len(data)))
                self.end_headers()
                self.wfile.write(data)
            except OSError:  # the client gave up waiting
                pass

        def log_message(self, *args):
            pass

    server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
    threading.Thread(target=server.serve_forever, kwargs={"poll_interval": 0.01}, daemon=True).start()
    monkeypatch.setenv("HUB_URL", f"http://127.0.0.1:{server.server_port}")
    monkeypatch.setenv("HUB_API_KEY", "pipeline-key")
    monkeypatch.setattr(stage_report, "artifact", lambda artifact_id, kind: {
        ("d", "delegation_result"): {}, ("dossier-id", "research_dossier"): DOSSIER}[(artifact_id, kind)])
    yield calls, reply
    server.shutdown()
    server.server_close()


def test_an_approved_dossier_goes_to_hub(hub, capsys):
    calls, _ = hub
    assert stage_report.archive_sources(dict(APPROVED)) == {"queued": 1, "skipped_recent": 0}
    assert calls == [{"path": "/api/v1/sources/archive", "auth": "Bearer pipeline-key",
                      "body": {"research_dossier": DOSSIER}}]
    assert capsys.readouterr().err == ""


@pytest.mark.parametrize("report", [
    dict(APPROVED, verdict="not_approved"),
    dict(APPROVED, team="Script", artifacts={"script": "s"}),
    dict(APPROVED, state="failed", verdict="missing"),
])
def test_nothing_else_goes_to_hub(hub, report):
    calls, _ = hub
    assert stage_report.archive_sources(report) is None
    assert calls == []


@pytest.mark.parametrize("trouble", ["http_error", "timeout", "no_keys", "dossier_unreadable"])
def test_a_failure_is_one_warning_line(hub, monkeypatch, capsys, trouble):
    _, reply = hub
    if trouble == "http_error":
        reply.update(status=500, body={"error": "internal error"})
    elif trouble == "timeout":
        monkeypatch.setattr(stage_report, "ARCHIVE_TIMEOUT", 0.2)
        reply["delay"] = 1
    elif trouble == "no_keys":
        monkeypatch.delenv("HUB_API_KEY")
        monkeypatch.setenv("HOME", str(Path(__file__).parent))  # no skills there
    else:
        def unreadable(*_):
            sys.exit("artifact dossier-id: not found")
        monkeypatch.setattr(stage_report, "artifact", unreadable)
    result = stage_report.archive_sources(dict(APPROVED))
    assert set(result) == {"error"} and result["error"]
    err = capsys.readouterr().err
    assert err.count("\n") == 1 and err.startswith("stage_report: ")


def _run_main(monkeypatch, tmp_path, *flags):
    monkeypatch.setattr(stage_report, "build_report", lambda result_id, delegation: dict(APPROVED))
    out = tmp_path / "stage_report.json"
    monkeypatch.setattr(sys, "argv", ["stage_report.py", "d", "--out", str(out), *flags])
    stage_report.main()  # a SystemExit here would be a non-zero exit code
    return json.loads(out.read_text()), Path(str(out) + ".summary.txt").read_text()


def test_main_records_the_outcome_outside_the_summary(hub, monkeypatch, tmp_path, capsys):
    calls, reply = hub
    report, line = _run_main(monkeypatch, tmp_path)
    assert report == dict(APPROVED, archive={"queued": 1, "skipped_recent": 0})
    assert line == stage_report.summary(APPROVED) and "archive" not in line

    reply.update(status=503, body={"error": "down"})
    failed, failed_line = _run_main(monkeypatch, tmp_path)
    assert failed["verdict"] == "approved" and set(failed["archive"]) == {"error"}
    assert failed_line == line
    assert capsys.readouterr().out.splitlines() == [line, line]
    assert len(calls) == 2


def test_no_archive_flag(hub, monkeypatch, tmp_path):
    calls, _ = hub
    report, _ = _run_main(monkeypatch, tmp_path, "--no-archive")
    assert report == APPROVED and calls == []
