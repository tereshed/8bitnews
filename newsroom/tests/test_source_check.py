import json
import sys
import threading
import urllib.parse
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path

import pytest

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "tools"))
import source_check  # noqa: E402

ARTICLE = "<html><title>Jobs report beats forecasts</title><body>Payrolls rose by 165,000.</body></html>"
PAGES = {
    "/ok": (200, ARTICLE),
    "/forbidden": (403, "denied"),
    "/forbidden-archived": (403, "denied"),
    "/forbidden-stub": (403, "denied"),
    "/gone": (404, "<title>Not here</title>"),
    "/teapot": (418, ""),
    "/challenge": (200, "<html><title>Just a moment...</title><script src='/cdn-cgi/challenge-platform/x'></script>"),
    "/soft": (200, "<html><title>Page not found | Daily</title></html>"),
    "/quotes-404": (200, "<html><title>Why the old model is gone</title><p>That page no longer exists, he said.</p>"),
}
# CDX rows per cited path; a snapshot body per timestamp.
CAPTURES = {"/forbidden-archived": [("20261106120000", "good")], "/forbidden-stub": [("20261107101318", "stub")]}
SNAPSHOTS = {"good": ARTICLE, "stub": "<html><title>Just a moment...</title></html>"}


class Handler(BaseHTTPRequestHandler):
    def log_message(self, *args):
        pass

    def send(self, status, body, headers=()):
        self.send_response(status)
        for key, value in headers:
            self.send_header(key, value)
        self.end_headers()
        self.wfile.write(body.encode())

    def do_GET(self):
        parts = urllib.parse.urlsplit(self.path)
        if self.headers.get("User-Agent") != source_check.UA:
            return self.send(400, "wrong user agent")
        if parts.path == "/cdx/search/cdx":
            q = urllib.parse.parse_qs(parts.query)
            assert q["filter"] == ["statuscode:200"] and q["matchType"] == ["exact"]
            cited = urllib.parse.urlsplit(q["url"][0])
            rows = [["timestamp", "original"]] + [[ts, q["url"][0]] for ts, _ in CAPTURES.get(cited.path, [])]
            return self.send(200, json.dumps(rows if len(rows) > 1 else []))
        if parts.path.startswith("/web/"):
            ts = parts.path.split("/")[2].removesuffix("id_")
            kind = next(k for caps in CAPTURES.values() for t, k in caps if t == ts)
            return self.send(200, SNAPSHOTS[kind])
        if parts.path == "/moved-home":
            return self.send(302, "", [("Location", "/")])
        if parts.path == "/":
            return self.send(200, "<title>Home</title>")
        status, body = PAGES.get(parts.path, (404, ""))
        self.send(status, body)


@pytest.fixture(scope="module")
def site():
    server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
    threading.Thread(target=server.serve_forever, daemon=True).start()
    base = f"http://127.0.0.1:{server.server_port}"
    source_check.WAYBACK = base
    yield base
    server.shutdown()


@pytest.mark.parametrize("path,passed,word", [
    ("/ok", True, "ok"),
    ("/forbidden", False, "bad"),                 # bot-blocked, no copy: hub refuses
    ("/forbidden-archived", True, "archived"),    # bot-blocked with a real copy: hub accepts
    ("/forbidden-stub", False, "bad"),            # the only copy is a challenge page (El País on 07.11)
    ("/gone", False, "bad"),
    ("/teapot", False, "bad"),                    # any other status counts as dead in hub
    ("/challenge", False, "bad"),
    ("/soft", False, "bad"),
    ("/moved-home", False, "bad"),                # redirect to the home page is a soft 404
    ("/quotes-404", True, "ok"),                  # 404 words in the article text, not its title, are fine
])
def test_check_matches_hub_outcome(site, path, passed, word):
    ok, line = source_check.check(site + path)
    assert ok is passed and line.split()[0] == word, line


def test_unreachable_site_names_the_reason(site):
    ok, line = source_check.check("http://127.0.0.1:9/nothing-listens-here")
    assert not ok and "unreachable (" in line


def test_non_http_link_is_bad_without_archive_lookup(site):
    ok, line = source_check.check("ftp://example.com/file")
    assert not ok and "blocked" in line


def test_hub_urls_takes_only_what_the_script_uses():
    dossier = {
        "facts": [
            {"id": "F1", "primary_source": {"url": "https://a/1"}, "sources": [{"url": "https://a/2"}]},
            {"id": "F2", "sources": [{"url": "https://b/1"}]},
        ],
        "local_groups": [{"id": "L1", "representatives": [{"url": "https://c/1"}]},
                         {"id": "L2", "representatives": [{"url": "https://c/2"}]}],
        "world_clusters": [{"id": "W1", "representatives": [{"url": "https://d/1"}]}],
    }
    script = {"scenes": [{"fact_ids": ["F1", "L2"]}], "notes": "W1 mentioned in prose is not a reference"}
    assert source_check.hub_urls(dossier, script) == ["https://a/1", "https://a/2", "https://c/2"]
    # A script that refers to nothing makes hub take every link, as extract.go does.
    assert len(source_check.hub_urls(dossier, {"scenes": []})) == 6


def test_all_urls_reads_any_report_shape():
    report = {"groups": [{"representatives": [{"url": "https://x/1", "quote": "q"}]}],
              "facts": [{"sources": [{"url": "https://x/2"}], "note": "see https://not-a-field"}]}
    assert source_check.all_urls(report) == ["https://x/1", "https://x/2"]


def test_main_exit_codes(site, tmp_path, capsys, monkeypatch):
    good = tmp_path / "good.json"
    good.write_text(json.dumps({"facts": [{"id": "F1", "sources": [{"url": site + "/ok"}]}]}))
    bad = tmp_path / "bad.json"
    bad.write_text(json.dumps({"facts": [{"id": "F1", "sources": [{"url": site + "/ok"}, {"url": site + "/forbidden"}]}]}))
    for path, code in ((good, 0), (bad, 1), (tmp_path / "missing.json", 2)):
        monkeypatch.setattr(sys, "argv", ["source_check.py", str(path)])
        assert source_check.main() == code
    out = capsys.readouterr().out
    assert f"bad      {site}/forbidden" in out and f"ok       {site}/ok" in out
