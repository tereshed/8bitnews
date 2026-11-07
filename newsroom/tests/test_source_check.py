import gzip
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
CHALLENGE = "<html><title>Just a moment...</title><script src='/cdn-cgi/challenge-platform/x'></script>"
PAGES = {
    "/ok": (200, ARTICLE),
    "/forbidden": (403, "denied"),
    "/forbidden-archived": (403, "denied"),
    "/forbidden-stub": (403, "denied"),
    "/forbidden-soft404-copy": (403, "denied"),
    "/forbidden-copy-500": (403, "denied"),
    "/forbidden-other-path": (403, "denied"),
    "/forbidden-archive-down": (403, "denied"),
    "/gone": (404, "<title>Not here</title>"),
    "/gone-archived": (410, ""),
    "/teapot": (418, ""),
    "/challenge": (200, CHALLENGE),
    "/robot-heading": (200, "<html><h1>Pardon Our Interruption</h1></html>"),
    "/robot-in-text": (200, "<html><title>Bots</title><p>He asked: are you a robot?</p></html>"),
    "/soft": (200, "<html><title>Page not found | Daily</title></html>"),
    "/quotes-404": (200, "<html><title>Why the old model is gone</title><p>That page no longer exists, he said.</p>"),
    "/espa%C3%B1a-noticia": (200, ARTICLE),
}
# CDX rows per cited path: (timestamp, snapshot kind, captured path or None for the same path).
CAPTURES = {
    "/forbidden-archived": [("20261106120000", "good", None)],
    "/gone-archived": [("20261101000000", "good", None)],
    "/forbidden-stub": [("20261107101318", "stub", None)],
    "/forbidden-soft404-copy": [("20261105000000", "soft404", None)],
    "/forbidden-copy-500": [("20261104000000", "error", None)],
    "/forbidden-other-path": [("20261103000000", "good", "/forbidden-other-path/amp")],
}
SNAPSHOTS = {"good": (200, ARTICLE), "stub": (200, CHALLENGE), "soft404": (200, "<title>Page not found</title>"),
             "error": (500, "")}


class Handler(BaseHTTPRequestHandler):
    def log_message(self, *args):
        pass

    def send(self, status, body, headers=(), raw=None):
        self.send_response(status)
        for key, value in headers:
            self.send_header(key, value)
        self.end_headers()
        self.wfile.write(raw if raw is not None else body.encode())

    def do_GET(self):
        parts = urllib.parse.urlsplit(self.path)
        if self.headers.get("User-Agent") != source_check.UA:
            return self.send(400, "wrong user agent")
        if parts.path == "/cdx/search/cdx":
            q = urllib.parse.parse_qs(parts.query)
            assert q["filter"] == ["statuscode:200"] and q["matchType"] == ["exact"]
            cited = urllib.parse.urlsplit(q["url"][0])
            if cited.path == "/forbidden-archive-down":
                return self.send(503, "busy")
            base = q["url"][0][: -len(cited.path)]
            rows = [["timestamp", "original"]] + [[ts, base + (path or cited.path)]
                                                  for ts, _, path in CAPTURES.get(cited.path, [])]
            return self.send(200, json.dumps(rows if len(rows) > 1 else []))
        if parts.path.startswith("/web/"):
            ts = parts.path.split("/")[2].removesuffix("id_")
            kind = next(k for caps in CAPTURES.values() for t, k, _ in caps if t == ts)
            return self.send(*SNAPSHOTS[kind])
        if parts.path == "/moved-home":
            return self.send(302, "", [("Location", "/")])
        if parts.path == "/moved-away":
            return self.send(302, "", [("Location", "http://localhost:1/elsewhere")])
        if parts.path == "/consent":
            return self.send(302, "", [("Location", "http://consent.localhost:1/collect")])
        if parts.path.startswith("/hop"):
            n = int(parts.path.removeprefix("/hop") or 0)
            if n == 0:
                return self.send(200, ARTICLE)
            return self.send(302, "", [("Location", f"/hop{n - 1}")])
        if parts.path == "/gzip-challenge":
            # Sent compressed whatever the request asked for: a reader that doesn't decompress misses the challenge.
            return self.send(200, "", [("Content-Encoding", "gzip")], raw=gzip.compress(CHALLENGE.encode()))
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
    source_check.ARCHIVE_PAUSE = 0
    yield base
    server.shutdown()


@pytest.mark.parametrize("path,passed,word", [
    ("/ok", True, "ok"),
    ("/forbidden", False, "bad"),                  # bot-blocked, no copy: hub refuses
    ("/forbidden-archived", True, "archived"),     # bot-blocked with a real copy: hub accepts
    ("/gone-archived", True, "archived"),          # gone, but archived before
    ("/forbidden-stub", False, "bad"),             # the only copy is a challenge page (El País on 07.11)
    ("/forbidden-soft404-copy", False, "bad"),     # the only copy is a "page not found"
    ("/forbidden-copy-500", False, "bad"),         # the copy can't be read
    ("/forbidden-other-path", False, "bad"),       # the capture is of another page
    ("/gone", False, "bad"),
    ("/teapot", False, "bad"),                     # any other status counts as dead in hub
    ("/challenge", False, "bad"),
    ("/gzip-challenge", False, "bad"),             # the challenge is found inside a gzip body
    ("/robot-heading", False, "bad"),              # challenge words in a heading
    ("/robot-in-text", True, "ok"),                # the same words in the text are fine
    ("/soft", False, "bad"),
    ("/quotes-404", True, "ok"),                   # 404 words in the article text, not its title, are fine
    ("/moved-home", False, "bad"),                 # redirect to the home page is a soft 404
    ("/hop4", True, "ok"),                         # hub follows 4 redirects…
    ("/hop5", False, "bad"),                       # …and refuses the 5th
    ("/españa-noticia", True, "ok"),               # non-ASCII path is percent-encoded on the wire
])
def test_check_matches_hub_outcome(site, path, passed, word):
    ok, line = source_check.check(site + path)
    assert ok is passed and line.split()[0] == word, line


@pytest.mark.parametrize("path,status", [("/forbidden", "bot_protected"), ("/gone", "dead"), ("/teapot", "dead"),
                                         ("/soft", "soft_404"), ("/challenge", "bot_protected")])
def test_bad_line_names_hubs_status(site, path, status):
    assert source_check.check(site + path)[1].startswith(f"bad {status}")


def test_redirect_to_another_domain_is_soft_404():
    assert source_check.redirected_away("https://www.elpais.com/a/b", "https://elpais.com/a/b") is False
    assert source_check.redirected_away("https://www.bbc.co.uk/news/1", "https://news.bbc.co.uk/news/1") is False
    assert source_check.redirected_away("https://www.nhk.or.jp/a", "https://www3.nhk.or.jp/a") is False
    assert source_check.redirected_away("https://a.example.com/story", "https://other.com/story") is True
    assert source_check.redirected_away("https://a.example.com/story", "https://a.example.com/") is True


def test_consent_wall_is_bot_protected(monkeypatch):
    monkeypatch.setattr(source_check, "fetch", lambda url: (200, "https://consent.yahoo.com/v2/collect", ARTICLE))
    assert source_check.live_status("https://finance.yahoo.com/news/x") == "bot_protected"


def test_archive_down_is_unknown_not_missing(site):
    ok, line = source_check.check(site + "/forbidden-archive-down")
    assert not ok and "archive unknown" in line and "no archive copy" not in line


def test_unreachable_site_names_the_reason(site):
    ok, line = source_check.check("http://127.0.0.1:9/nothing-listens-here")
    assert not ok and "unreachable (" in line


def test_non_http_link_is_bad_without_archive_lookup(site):
    ok, line = source_check.check("ftp://example.com/file")
    assert not ok and "blocked" in line


def test_spent_budget_stops_checking(site, monkeypatch):
    monkeypatch.setattr(source_check, "START", source_check.time.monotonic() - source_check.BUDGET - 1)
    ok, line = source_check.check(site + "/ok")
    assert not ok and "time budget" in line


def test_same_page_compares_decoded_paths():
    assert source_check.same_page("https://x.es/espa%C3%B1a", "http://x.es/espa%c3%b1a/")
    assert source_check.same_page("https://x.es/españa", "https://x.es/espa%C3%B1a")
    assert not source_check.same_page("https://x.com/watch?v=dQw4", "https://x.com/watch?v=DQW4")


DOSSIER = {
    "facts": [
        {"id": "F1", "primary_source": {"url": "https://a/1"}, "sources": [{"url": "https://a/2"}]},
        {"id": "F2", "sources": [{"url": "https://b/1"}]},
    ],
    "local_groups": [{"id": "L1", "representatives": [{"url": "https://c/1"}]},
                     {"id": "L2", "representatives": [{"url": "https://c/2"}]}],
    "world_clusters": [{"id": "W1", "representatives": [{"url": "https://d/1"}]}],
    "polls": [{"url": "https://poll/1"}],
}


def test_hub_urls_takes_only_what_the_script_uses():
    script = {"scenes": [{"fact_ids": ["F1", "L2"]}], "notes": "W1 mentioned in prose is not a reference"}
    assert source_check.hub_urls(DOSSIER, script) == ["https://a/1", "https://a/2", "https://c/2"]
    # Ids used only as keys are not references, as in hub's collectIDs: the script refers to nothing, so all links.
    assert len(source_check.hub_urls(DOSSIER, {"F2": "x", "scenes": []})) == 6
    assert len(source_check.hub_urls(DOSSIER)) == 6


def test_polls_are_never_checked():
    assert "https://poll/1" not in source_check.hub_urls(DOSSIER)
    report = {"groups": [{"id": "L1", "representatives": [{"url": "https://x/1"}]}],
              "polls": [{"url": "https://poll/2"}], "outlets_reviewed": [{"url": "https://o/1"}]}
    assert source_check.hub_urls(report) == ["https://x/1"]
    assert source_check.other_urls({"misc": [{"url": "https://x/2"}], "polls": [{"url": "https://poll/3"}]}) == \
        ["https://x/2"]


def test_malformed_sources_are_skipped():
    report = {"facts": [{"id": "F1", "sources": ["https://not-an-object", {"url": None}, {"url": "https://ok/1"}]}]}
    assert source_check.hub_urls(report) == ["https://ok/1"]


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


def test_main_with_script_checks_only_referenced_links(site, tmp_path, capsys, monkeypatch):
    dossier = tmp_path / "dossier.json"
    dossier.write_text(json.dumps({"facts": [{"id": "F1", "sources": [{"url": site + "/ok"}]},
                                             {"id": "F2", "sources": [{"url": site + "/forbidden"}]}]}))
    script = tmp_path / "script.json"
    script.write_text(json.dumps({"scenes": [{"fact_ids": ["F1"]}]}))
    monkeypatch.setattr(sys, "argv", ["source_check.py", str(dossier), "--script", str(script)])
    assert source_check.main() == 0
    assert "/forbidden" not in capsys.readouterr().out
