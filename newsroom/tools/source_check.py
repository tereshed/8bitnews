#!/usr/bin/env python3
"""Checks source links nearly the way hub's release_submit does, before they reach a dossier.

    python3 newsroom/tools/source_check.py /tmp/research_dossier.json [--script /tmp/script.json]
    python3 newsroom/tools/source_check.py --url <url> [--url <url> ...]

hub refuses a release when a source it checks is neither live for its bot nor archived
(«broken sources with no archive copy», internal/service/release.go). This tool repeats
internal/sources: the same User-Agent, redirect limit and statuses, the same challenge,
consent-wall and soft-404 signs, and the same Wayback CDX lookup of a 200 snapshot.

The input is a research_dossier, facts_report, local_press_report or world_press_report.
The links checked are the ones hub takes: sources of facts, and representatives of groups
and clusters. Polls and outlets_reviewed are not checked, because hub doesn't check them.
With --script, only the facts, groups and clusters that the script refers to are checked,
as in hub.

One line per link, printed as soon as it is known:
- `ok`: hub's bot gets the page;
- `archived <snapshot>`: bot-blocked or gone, but a good Wayback copy exists;
- `bad <reason>`: hub will refuse it, or the archive didn't answer («archive unknown»).

Exit codes: 0 when every link passes, 1 when some are `bad`, 2 on bad input.

`ok` is not a promise. hub runs on another IP and speaks HTTP/2. At submit time it also
tries Save Page Now, which may rescue a `bad` link, but often fails (some sites block the
archive too), so this tool doesn't count on it.
"""
from __future__ import annotations

import argparse
import gzip
import json
import os
import re
import sys
import threading
import time
import urllib.error
import urllib.parse
import urllib.request
import zlib
from concurrent.futures import ThreadPoolExecutor, as_completed

UA = "8bitnews-sources-bot/1.0 (+https://github.com/tereshed/8bitnews)"  # internal/sources/checker.go
MAX_BODY = 64 * 1024
TIMEOUT = float(os.environ.get("SOURCE_CHECK_TIMEOUT_S", "15"))         # hub: 15 s for the whole request
BUDGET = float(os.environ.get("SOURCE_CHECK_BUDGET_S", "100"))          # whole run, so an agent's shell call ends
ARCHIVE_TRIES = 3
ARCHIVE_PAUSE = 3.0
WAYBACK = os.environ.get("SOURCE_CHECK_WAYBACK_URL", "https://web.archive.org").rstrip("/")

CHALLENGE_MARKERS = ["cf-browser-verification", "just a moment", "attention required", "verify you are human",
                     "checking your browser", "cf-chl", "challenge-platform", 'name="js_challenge"']
CHALLENGE_HEADINGS = ["pardon our interruption", "are you a robot"]
SOFT404_MARKERS = ["page not found", "404 not found", "we couldn't find", "could not be found",
                   "content is no longer available", "doesn't exist", "page no longer exists"]
CONSENT_HOSTS = {"consent.yahoo.com", "guce.yahoo.com", "guce.oath.com", "consent.aol.com", "guce.aol.com"}
# Python has no public-suffix list (hub uses one). These multi-label suffixes cover the countries and outlets we
# cite; elsewhere the registrable domain is the last two labels.
SECOND_LEVELS = {
    "co.uk", "org.uk", "ac.uk", "gov.uk", "me.uk", "ltd.uk", "plc.uk", "net.uk", "sch.uk", "nhs.uk", "police.uk",
    "com.au", "net.au", "org.au", "edu.au", "gov.au", "asn.au", "id.au", "co.nz", "org.nz", "govt.nz", "ac.nz",
    "com.br", "gov.br", "leg.br", "jus.br", "org.br", "net.br", "edu.br", "art.br", "blog.br",
    "com.mx", "gob.mx", "org.mx", "edu.mx", "net.mx", "com.ar", "gob.ar", "gov.ar", "org.ar", "net.ar", "edu.ar",
    "com.co", "gov.co", "org.co", "edu.co", "com.pe", "gob.pe", "org.pe", "gob.cl", "com.ve", "gob.ve",
    "co.jp", "or.jp", "ne.jp", "go.jp", "ac.jp", "ad.jp", "ed.jp", "gr.jp", "lg.jp",
    "co.kr", "or.kr", "go.kr", "ac.kr", "re.kr", "ne.kr", "co.in", "gov.in", "nic.in", "org.in", "net.in", "ac.in",
    "com.cn", "gov.cn", "org.cn", "net.cn", "edu.cn", "com.hk", "gov.hk", "org.hk", "com.tw", "gov.tw", "org.tw",
    "com.sg", "gov.sg", "edu.sg", "org.sg", "com.my", "gov.my", "co.id", "go.id", "or.id", "co.th", "go.th",
    "com.ph", "gov.ph", "com.vn", "gov.vn", "com.pk", "gov.pk", "com.bd", "gov.bd",
    "co.za", "gov.za", "org.za", "ac.za", "co.ke", "go.ke", "or.ke", "com.ng", "gov.ng", "com.eg", "gov.eg",
    "co.il", "gov.il", "org.il", "ac.il", "com.tr", "gov.tr", "org.tr", "edu.tr", "bel.tr", "k12.tr",
    "com.sa", "gov.sa", "com.qa", "gov.qa", "gov.ae", "co.ae", "com.ua", "gov.ua", "org.ua", "in.ua",
    "com.es", "org.es", "edu.es", "nom.es", "gob.es", "com.pt", "gov.pt", "org.pt", "com.pl", "gov.pl", "org.pl",
    "gv.at", "co.at", "or.at", "ac.at", "com.gr", "gov.gr", "com.ru", "gov.ru",
}
WAYBACK_LOCK = threading.Lock()  # one archive request at a time: parallel lookups get refused
HEADING = re.compile(r"<title[^>]*>(.*?)</title>|<h1[^>]*>(.*?)</h1>", re.I | re.S)
ID = re.compile(r"^[FLW][0-9]+$")
SKIP_KEYS = {"polls", "outlets_reviewed"}  # links hub never checks
START = time.monotonic()


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


def ascii_url(url: str) -> str:
    """The URL as it goes on the wire: IDN host in punycode, non-ASCII path and query percent-encoded."""
    p = urllib.parse.urlsplit(url)
    host = p.hostname or ""
    try:
        host = host.encode("idna").decode("ascii")
    except UnicodeError:
        pass
    netloc = host + (f":{p.port}" if p.port else "")
    quote = lambda s: urllib.parse.quote(s, safe="/%:@!$&'()*+,;=-._~?")
    return urllib.parse.urlunsplit((p.scheme, netloc, quote(p.path), quote(p.query), ""))


class Redirects(urllib.request.HTTPRedirectHandler):
    max_redirections = 4  # hub (safehttp.go, maxRedirects 5) refuses the 5th redirect


OPENER = urllib.request.build_opener(Redirects)


def fetch(url: str) -> tuple[int, str, str]:
    """(HTTP status, final URL, first 64 KB of the body). Status 0 when the site can't be reached in TIMEOUT;
    the body then holds the reason."""
    request = urllib.request.Request(ascii_url(url), headers={"User-Agent": UA, "Accept-Encoding": "gzip"})
    deadline = time.monotonic() + TIMEOUT
    try:
        with OPENER.open(request, timeout=TIMEOUT) as resp:
            raw, decoder = b"", None
            if resp.headers.get("Content-Encoding", "").lower() == "gzip":
                decoder = zlib.decompressobj(16 + zlib.MAX_WBITS)
            while len(raw) < MAX_BODY:
                if time.monotonic() > deadline:
                    return 0, url, "timeout reading the page"
                chunk = resp.read(16 * 1024)
                if not chunk:
                    break
                raw += decoder.decompress(chunk) if decoder else chunk
            return resp.status, resp.geturl(), raw[:MAX_BODY].decode("utf-8", "replace")
    except urllib.error.HTTPError as e:
        if 300 <= e.code < 400:  # redirect limit hit: hub's client errors out, so hub can't reach it
            return 0, url, "too many redirects"
        return e.code, e.geturl() or url, ""
    except (zlib.error, gzip.BadGzipFile) as e:
        return 0, url, f"broken gzip: {e}"
    except Exception as e:  # DNS, TLS, timeout, connection reset
        return 0, url, f"{type(e).__name__}: {getattr(e, 'reason', e)}"


def live_status(url: str) -> str:
    """hub's CheckStatus for the live page: ok, bot_protected, dead, soft_404, unreachable (…), blocked."""
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
        for attempt in range(ARCHIVE_TRIES):
            status, final, body = fetch(url)
            if status != 0 and status != 429 and status < 500:
                break
            if attempt + 1 < ARCHIVE_TRIES and time.monotonic() - START < BUDGET:
                time.sleep(ARCHIVE_PAUSE * (attempt + 1))
        return status, final, body


def same_page(target: str, captured: str) -> bool:
    t, c = urllib.parse.urlsplit(target), urllib.parse.urlsplit(captured)
    path = lambda u: urllib.parse.unquote(u.path).rstrip("/")
    query = lambda u: sorted(urllib.parse.parse_qsl(u.query, keep_blank_values=True))
    return path(t) == path(c) and query(t) == query(c)


def snapshot(url: str) -> str | None:
    """The latest Wayback capture (HTTP 200) that shows the page itself, as hub's AvailabilityClient.Closest."""
    query = urllib.parse.urlencode({"url": url, "matchType": "exact", "filter": "statuscode:200", "collapse": "digest",
                                    "fl": "timestamp,original", "limit": "-3", "output": "json"})
    status, _, body = archive_get(f"{WAYBACK}/cdx/search/cdx?{query}")
    if status != 200:
        raise RuntimeError(f"lookup answered {status or body}")
    rows = json.loads(body or "[]")
    for row in reversed(rows[1:]):
        timestamp, original = row[0], row[1]
        if not same_page(url, original):
            continue
        status, _, page = archive_get(f"{WAYBACK}/web/{timestamp}id_/{original}")
        if status == 200 and not looks_like_challenge(page) and not looks_like_soft404(page):
            return f"{WAYBACK}/web/{timestamp}/{original}"
    return None


def check(url: str) -> tuple[bool, str]:
    if time.monotonic() - START > BUDGET:
        return False, "bad archive unknown (time budget spent): run again for this link"
    status = live_status(url)
    if status == "ok":
        return True, "ok"
    if status == "blocked":
        return False, "bad blocked (not an http(s) link)"
    try:
        copy = snapshot(url)
    except Exception as e:
        return False, f"bad {status}; archive unknown ({e})"
    return (True, f"archived {copy} (live: {status})") if copy else (False, f"bad {status}, no archive copy")


def script_ids(node, found: set) -> set:
    """F/L/W ids anywhere among the script's string values (not keys), as hub's collectIDs."""
    if isinstance(node, str) and ID.match(node):
        found.add(node)
    elif isinstance(node, list):
        for item in node:
            script_ids(item, found)
    elif isinstance(node, dict):
        for item in node.values():
            script_ids(item, found)
    return found


def links(items, ids: set, rep_key: str | None) -> list[str]:
    found = []
    for item in items if isinstance(items, list) else []:
        if not isinstance(item, dict) or (ids and item.get("id") not in ids):
            continue
        if rep_key is None:
            sources = [item.get("primary_source")] + (item.get("sources") or [])
        else:
            sources = item.get(rep_key) or []
        found += [s["url"] for s in sources if isinstance(s, dict) and isinstance(s.get("url"), str) and s["url"]]
    return found


def hub_urls(report: dict, script=None) -> list[str]:
    """The links hub checks (internal/sources/extract.go). A script that refers to nothing makes hub take all."""
    ids = script_ids(script, set()) if script is not None else set()
    found = links(report.get("facts"), ids, None)
    for key in ("local_groups", "world_clusters", "groups", "clusters"):  # dossier, then the analysts' reports
        found += links(report.get(key), ids, "representatives")
    return found


def other_urls(node) -> list[str]:
    """`url` fields of a report of an unknown shape, except polls and outlets_reviewed."""
    found = []
    if isinstance(node, dict):
        for key, value in node.items():
            if key in SKIP_KEYS:
                continue
            if key == "url" and isinstance(value, str) and value:
                found.append(value)
            else:
                found.extend(other_urls(value))
    elif isinstance(node, list):
        for item in node:
            found.extend(other_urls(item))
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
            with open(args.report, encoding="utf-8") as f:
                report = json.load(f)
            script = None
            if args.script:
                with open(args.script, encoding="utf-8") as f:
                    script = json.load(f)
            known = hub_urls(report, script) if isinstance(report, dict) else []
            urls += known if known or args.script else other_urls(report)
    except (OSError, ValueError) as e:
        print(f"cannot read input: {e}", file=sys.stderr)
        return 2
    urls = list(dict.fromkeys(urls))
    if not urls:
        print("no links to check", file=sys.stderr)
        return 2
    bad = 0
    with ThreadPoolExecutor(max_workers=6) as pool:
        futures = {pool.submit(check, url): url for url in urls}
        for future in as_completed(futures):
            passed, line = future.result()
            bad += not passed
            word, _, rest = line.partition(" ")
            print(f"{word:<9}{futures[future]}" + (f"  — {rest}" if rest else ""), flush=True)
    print(f"{len(urls)} links: {len(urls) - bad} pass, {bad} bad", file=sys.stderr)
    return 1 if bad else 0


if __name__ == "__main__":
    sys.exit(main())
