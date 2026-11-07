#!/usr/bin/env python3
"""Checks source links the way hub's release_submit does, before they reach a dossier.

    python3 newsroom/tools/source_check.py /tmp/research_dossier.json [--script /tmp/script.json]
    python3 newsroom/tools/source_check.py --url <url> [--url <url> ...]

hub refuses a release when a source it checks is neither live for its bot nor archived
(«broken sources with no archive copy», internal/service/release.go). This tool repeats
internal/sources: the same User-Agent and statuses, the same challenge, consent-wall and
soft-404 signs, and the same Wayback CDX lookup of an existing 200 snapshot.

The input is any Research report (facts_report, local_press_report, world_press_report,
research_dossier). Every `url` field in it is checked. With --script, only the links hub
itself takes are checked: those of the facts, groups and clusters the script refers to.

One line per link: `ok`, `archived <snapshot>` (bot-blocked or gone, but a good Wayback
copy exists) or `bad <reason>`. Exit 0 when every link passes, 1 when some are `bad`, 2 on
bad input. hub also tries Save Page Now for a bot-blocked link at submit time. That can
fail (some sites block the archive too), so a link with no snapshot counts as `bad` here.
"""
from __future__ import annotations

import argparse
import json
import os
import re
import sys
import threading
import time
import urllib.error
import urllib.parse
import urllib.request
from concurrent.futures import ThreadPoolExecutor

UA = "8bitnews-sources-bot/1.0 (+https://github.com/tereshed/8bitnews)"  # internal/sources/checker.go
MAX_BODY = 64 * 1024
TIMEOUT = float(os.environ.get("SOURCE_CHECK_TIMEOUT_S", "15"))
WAYBACK = os.environ.get("SOURCE_CHECK_WAYBACK_URL", "https://web.archive.org").rstrip("/")

CHALLENGE_MARKERS = ["cf-browser-verification", "just a moment", "attention required", "verify you are human",
                     "checking your browser", "cf-chl", "challenge-platform", 'name="js_challenge"']
CHALLENGE_HEADINGS = ["pardon our interruption", "are you a robot"]
SOFT404_MARKERS = ["page not found", "404 not found", "we couldn't find", "could not be found",
                   "content is no longer available", "doesn't exist", "page no longer exists"]
CONSENT_HOSTS = {"consent.yahoo.com", "guce.yahoo.com", "guce.oath.com", "consent.aol.com", "guce.aol.com"}
# Python has no public-suffix list; these second levels cover the outlets we cite. Elsewhere the last two labels.
SECOND_LEVELS = {"co.uk", "org.uk", "ac.uk", "gov.uk", "com.au", "net.au", "org.au", "gov.au", "co.nz", "com.br",
                 "com.mx", "com.ar", "co.jp", "co.kr", "co.in", "com.cn", "com.hk", "com.sg", "co.za", "com.tr",
                 "com.co", "gob.es", "gob.mx"}
WAYBACK_LOCK = threading.Lock()  # one archive request at a time: parallel lookups get refused
HEADING = re.compile(r"<title[^>]*>(.*?)</title>|<h1[^>]*>(.*?)</h1>", re.I | re.S)


def heading_text(body: str) -> str:
    return " ".join(a or b for a, b in HEADING.findall(body)).lower()


def looks_like_challenge(body: str) -> bool:
    text = body.lower()
    return any(m in text for m in CHALLENGE_MARKERS) or any(m in heading_text(body) for m in CHALLENGE_HEADINGS)


def looks_like_soft404(body: str) -> bool:
    return any(m in heading_text(body) for m in SOFT404_MARKERS)


def registrable(host: str) -> str:
    labels = host.lower().rstrip(".").split(".")
    n = 3 if len(labels) > 2 and ".".join(labels[-2:]) in SECOND_LEVELS else 2
    return ".".join(labels[-n:])


def redirected_away(original: str, final: str) -> bool:
    o, f = urllib.parse.urlsplit(original), urllib.parse.urlsplit(final)
    if not o.path.strip("/"):
        return False
    return registrable(o.hostname or "") != registrable(f.hostname or "") or not f.path.strip("/")


def fetch(url: str) -> tuple[int, str, str]:
    """(HTTP status, final URL, first 64 KB of the body). Status 0 when the site can't be reached; the body then
    holds the reason."""
    request = urllib.request.Request(url, headers={"User-Agent": UA})
    try:
        with urllib.request.urlopen(request, timeout=TIMEOUT) as resp:
            return resp.status, resp.geturl(), resp.read(MAX_BODY).decode("utf-8", "replace")
    except urllib.error.HTTPError as e:
        return e.code, e.geturl() or url, ""
    except Exception as e:  # DNS, TLS, timeout, connection reset
        return 0, url, f"{type(e).__name__}: {getattr(e, 'reason', e)}"


def live_status(url: str) -> str:
    """hub's CheckStatus for the live page: ok, bot_protected, dead, soft_404, unreachable, blocked."""
    if urllib.parse.urlsplit(url).scheme not in ("http", "https"):
        return "blocked"
    status, final, body = fetch(url)
    if status == 0:
        return f"unreachable ({body})"
    if status in (401, 403, 429, 451):
        return "bot_protected"
    if status in (404, 410):
        return "dead"
    if 200 <= status < 300:
        host = (urllib.parse.urlsplit(final).hostname or "").lower()
        if host in CONSENT_HOSTS or host.startswith(("consent.", "guce.")) or looks_like_challenge(body):
            return "bot_protected"
        if redirected_away(url, final) or looks_like_soft404(body):
            return "soft_404"
        return "ok"
    return "dead"


def archive_get(url: str) -> tuple[int, str, str]:
    """GET from the archive, again after a pause when it is busy (429, 5xx, refused connection), as hub's get."""
    with WAYBACK_LOCK:
        for attempt in range(3):
            status, final, body = fetch(url)
            if status != 0 and status != 429 and status < 500:
                return status, final, body
            time.sleep(3 * (attempt + 1))
        return status, final, body


def same_page(target: str, captured: str) -> bool:
    t, c = urllib.parse.urlsplit(target), urllib.parse.urlsplit(captured)
    query = lambda u: urllib.parse.urlencode(sorted(urllib.parse.parse_qsl(u.query, keep_blank_values=True)))
    return t.path.rstrip("/") == c.path.rstrip("/") and query(t) == query(c)


def snapshot(url: str) -> str | None:
    """The latest Wayback capture (HTTP 200) that shows the page itself, as hub's AvailabilityClient.Closest."""
    query = urllib.parse.urlencode({"url": url, "matchType": "exact", "filter": "statuscode:200", "collapse": "digest",
                                    "fl": "timestamp,original", "limit": "-3", "output": "json"})
    status, _, body = archive_get(f"{WAYBACK}/cdx/search/cdx?{query}")
    if status != 200:
        raise RuntimeError(f"Wayback lookup answered HTTP {status} {body if status == 0 else ''}".strip())
    rows = json.loads(body or "[]")
    for timestamp, original in reversed(rows[1:]):
        if not same_page(url, original):
            continue
        status, _, page = archive_get(f"{WAYBACK}/web/{timestamp}id_/{original}")
        if status == 200 and not looks_like_challenge(page) and not looks_like_soft404(page):
            return f"{WAYBACK}/web/{timestamp}/{original}"
    return None


def check(url: str) -> tuple[bool, str]:
    status = live_status(url)
    if status == "ok":
        return True, "ok"
    if status == "blocked":
        return False, "bad blocked (not an http(s) link)"
    try:
        copy = snapshot(url)
    except Exception as e:
        return False, f"bad {status}; archive unknown ({e}): retry later or replace"
    return (True, f"archived {copy} (live: {status})") if copy else (False, f"bad {status}, no archive copy")


def all_urls(node) -> list[str]:
    found = []
    if isinstance(node, dict):
        for key, value in node.items():
            if key == "url" and isinstance(value, str) and value:
                found.append(value)
            else:
                found.extend(all_urls(value))
    elif isinstance(node, list):
        for item in node:
            found.extend(all_urls(item))
    return found


def hub_urls(dossier: dict, script) -> list[str]:
    """The links hub checks (internal/sources/extract.go): sources and representatives of what the script uses."""
    ids = set(re.findall(r'"([FLW][0-9]+)"', json.dumps(script)))
    found = []
    for fact in dossier.get("facts") or []:
        if ids and fact.get("id") not in ids:
            continue
        found += [s["url"] for s in [fact.get("primary_source") or {}] + (fact.get("sources") or []) if s.get("url")]
    for key in ("local_groups", "world_clusters"):
        for group in dossier.get(key) or []:
            if ids and group.get("id") not in ids:
                continue
            found += [r["url"] for r in group.get("representatives") or [] if r.get("url")]
    return found


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__.split("\n\n")[0])
    ap.add_argument("report", nargs="?", help="JSON of a Research report or dossier")
    ap.add_argument("--script", help="script JSON: check only the links hub takes for it")
    ap.add_argument("--url", action="append", default=[], help="a link to check; can repeat")
    args = ap.parse_args()
    urls = list(args.url)
    try:
        if args.report:
            report = json.load(open(args.report))
            urls += hub_urls(report, json.load(open(args.script))) if args.script else all_urls(report)
    except (OSError, ValueError) as e:
        print(f"cannot read input: {e}", file=sys.stderr)
        return 2
    urls = list(dict.fromkeys(urls))
    if not urls:
        print("no links to check", file=sys.stderr)
        return 2
    with ThreadPoolExecutor(max_workers=6) as pool:
        results = list(pool.map(check, urls))
    for url, (_, line) in zip(urls, results):
        print(f"{line.split(' ', 1)[0]:<9}{url}" + (f"  — {line.split(' ', 1)[1]}" if " " in line else ""))
    bad = sum(1 for passed, _ in results if not passed)
    print(f"{len(urls)} links: {len(urls) - bad} pass, {bad} bad", file=sys.stderr)
    return 1 if bad else 0


if __name__ == "__main__":
    sys.exit(main())
