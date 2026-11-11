#!/usr/bin/env python3
"""Checks that a research_dossier names the state media it rests on, before fact_checker does.

    python3 newsroom/tools/state_media_check.py /tmp/research_dossier.json [--country ES]

State media are the outlets with `ownership: state_funded` or `state_controlled` in
countries/<cc>/sources.yaml; the country comes from the dossier's channel. A source or a
representative is theirs as hub decides it for the video's source list
(internal/publish/sources.go): its URL is on their domain or a subdomain, or its `outlet` goes
by their name or an `aka` name, alone or in a reprint's form («EFE (via Infobae)», «Infobae (EFE)»,
«EFE vía Infobae»). hub marks those links «(medio estatal)» by itself. What hub can't mark is the
text, and that is what analyst.md asks for:
- the `note` of a fact citing state media says so;
- the `position` or `label` of a world cluster with a representative from state media says so;
- each sentence of a cluster's `label`, `position` or `on_development` that names a state outlet says so
  in that sentence, link or no link;
- each `review` sentence that names a state outlet says so in that sentence. A one-word name is matched
  as written («EFE», «Lusa»: not «la prensa lusa»), a longer one ignoring case;
- one line of `risks` says so.

"Says so" is a word for it: «estatal», «state-funded», «state media», «state-run», «government-run»…
A world_press_report is checked the same way, for its clusters only.

One line per problem. Exit codes: 0 when nothing is missing, 1 when something is, 2 on bad input.
Sandboxes have no PyYAML, so sources.yaml is read by a small parser that knows its one-line form.
"""
from __future__ import annotations

import argparse
import json
import os
import re
import sys
import urllib.parse

ROOT = os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
STATE = ("state_funded", "state_controlled")
# A word that says an outlet belongs to a state, in the channels' languages.
MARK = re.compile(
    r"estatal|(?:del|por el) Estado|gubernamental|oficialista"
    r"|state[- ](?:funded|controlled|owned|run|media|agency|news agency|broadcaster|television|tv)"
    r"|government[- ](?:funded|controlled|owned|run)|госуд",
    re.I,
)


def parse_inline(body: str) -> dict:
    """The inside of a one-line YAML mapping: `outlet: EFE, aka: [EFE, Agencia EFE], note: "a, b"`."""
    out, key, buf, depth, quote = {}, None, "", 0, ""
    pairs = []
    for ch in body + ",":
        if quote:
            if ch == quote:
                quote = ""
            buf += ch
        elif ch in "\"'":
            quote = ch
            buf += ch
        elif ch == "[":
            depth += 1
            buf += ch
        elif ch == "]":
            depth -= 1
            buf += ch
        elif ch == "," and depth == 0:
            pairs.append(buf)
            buf = ""
        else:
            buf += ch
    for pair in pairs:
        if ":" not in pair:
            continue
        key, value = pair.split(":", 1)
        out[key.strip()] = scalar(value.strip())
    return out


def scalar(value: str):
    if value.startswith("[") and value.endswith("]"):
        return [scalar(v) for v in split_list(value[1:-1])]
    if len(value) >= 2 and value[0] == value[-1] and value[0] in "\"'":
        return value[1:-1]
    if value in ("true", "false"):
        return value == "true"
    return value


def split_list(inner: str) -> list[str]:
    items, buf, quote = [], "", ""
    for ch in inner + ",":
        if quote:
            if ch == quote:
                quote = ""
            buf += ch
        elif ch in "\"'":
            quote = ch
            buf += ch
        elif ch == ",":
            if buf.strip():
                items.append(buf.strip())
            buf = ""
        else:
            buf += ch
    return items


def outlets(text: str) -> list[dict]:
    """Every outlet in a sources.yaml, local and world: the lines `- { outlet: …, domain: … }`."""
    found = []
    for line in text.splitlines():
        m = re.match(r"^\s*-\s*\{(.*)\}\s*(?:#.*)?$", line)
        if m:
            o = parse_inline(m.group(1))
            if o.get("outlet") and o.get("domain"):
                found.append(o)
    return found


def country_of(channel_id: str) -> str:
    with open(os.path.join(ROOT, "channels", channel_id, "channel.yaml"), encoding="utf-8") as f:
        m = re.search(r"^country:\s*([A-Za-z]{2})\s*$", f.read(), re.M)
    if not m:
        raise ValueError(f"channels/{channel_id}/channel.yaml has no country")
    return m.group(1).lower()


class StateMedia:
    def __init__(self, sources_yaml: str):
        state = [o for o in outlets(sources_yaml) if o.get("ownership") in STATE]
        self.domains = {o["domain"].lower(): o["outlet"] for o in state}
        self.names = {}
        self.prose = []  # (pattern, outlet) for names in running text
        for o in state:
            for name in [o["outlet"], *(o.get("aka") or [])]:
                self.names[name.lower()] = o["outlet"]
                # A one-word name is matched as written: «EFE», «Lusa», but not «efe» or «la prensa lusa»
                # (Portuguese). Longer names ignore case: «al jazeera».
                flags = re.I if " " in name.strip() else 0
                self.prose.append((re.compile(r"(?<!\w)" + re.escape(name.strip()) + r"(?!\w)", flags), o["outlet"]))

    def by_url(self, url: str) -> str | None:
        host = (urllib.parse.urlparse(url or "").hostname or "").lower()
        while host:
            if host in self.domains:
                return self.domains[host]
            host = host.partition(".")[2]
        return None

    def by_outlet(self, outlet: str) -> str | None:
        """hub's goesBy: the whole name, or a part of «X (via Y)», «Y (X)», «X vía Y», «Y / X»."""
        lower = (outlet or "").lower().replace("vía", "via")
        for part in re.split(r"[(),/]", lower):
            part = part.strip()
            part = part[4:].strip() if part.startswith("via ") else part
            before, via, after = part.partition(" via ")
            for candidate in (before.strip(), after.strip() if via else ""):
                if candidate in self.names:
                    return self.names[candidate]
        return None

    def of(self, item: dict) -> str | None:
        return self.by_url(item.get("url", "")) or self.by_outlet(item.get("outlet", ""))

    def named_in(self, sentence: str) -> list[str]:
        return sorted({outlet for pattern, outlet in self.prose if pattern.search(sentence)})


def sentences(text: str) -> list[str]:
    return [s for s in re.split(r"(?<=[.!?])\s+(?=\S)", text or "") if s.strip()]


def check(doc: dict, media: StateMedia) -> list[str]:
    problems, used = [], set()
    for fact in doc.get("facts") or []:
        cited = sorted({o for o in (media.of(s) for s in [*(fact.get("sources") or []), fact.get("primary_source") or {}]) if o})
        used.update(cited)
        if cited and not MARK.search(fact.get("note") or ""):
            problems.append(f"{fact.get('id')}: cites {', '.join(cited)}; say it is state media in note")
    for cluster in doc.get("world_clusters") or doc.get("clusters") or []:
        cited = sorted({o for o in (media.of(r) for r in cluster.get("representatives") or []) if o})
        used.update(cited)
        if cited and not MARK.search((cluster.get("position") or "") + " " + (cluster.get("label") or "")):
            problems.append(f"{cluster.get('id')}: a representative is from {', '.join(cited)}; say it is state media in position")
        # An outlet the cluster's text leans on without a representative (11.11: «DW (public broadcaster) …»).
        # Each sentence that names one carries the mark itself, as in review.
        for field in ("label", "position", "on_development"):
            for sentence in sentences(cluster.get(field) or ""):
                named = sorted(set(media.named_in(sentence)) - set(cited))
                used.update(named)
                if named and not MARK.search(sentence):
                    problems.append(f"{cluster.get('id')}.{field}: «{sentence[:90]}» names {', '.join(named)} "
                                    "without saying it is state media")
    for group in doc.get("local_groups") or []:
        used.update(o for o in (media.of(r) for r in group.get("representatives") or []) if o)
    review = doc.get("review")
    if isinstance(review, dict):
        for field, text in review.items():
            if field == "language" or not isinstance(text, str):
                continue
            for sentence in sentences(text):
                named = media.named_in(sentence)
                if named and not MARK.search(sentence):
                    problems.append(f"review.{field}: «{sentence[:90]}» names {', '.join(named)} without saying it is state media")
    if "risks" in doc and used and not any(MARK.search(r if isinstance(r, str) else json.dumps(r, ensure_ascii=False))
                                           for r in doc.get("risks") or []):
        problems.append(f"risks: the dossier rests on {', '.join(sorted(used))}; add a line that it is state media")
    return problems


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__.split("\n\n")[0])
    ap.add_argument("file", help="research_dossier or world_press_report JSON")
    ap.add_argument("--country", help="ES, US…; by default the country of the dossier's channel")
    args = ap.parse_args()
    try:
        with open(args.file, encoding="utf-8") as f:
            doc = json.load(f)
        if not isinstance(doc, dict):
            raise ValueError("not a JSON object")
        doc = doc.get("content", doc) if isinstance(doc.get("content"), dict) else doc
        cc = (args.country or country_of(doc["channel_id"])).lower()
        with open(os.path.join(ROOT, "countries", cc, "sources.yaml"), encoding="utf-8") as f:
            media = StateMedia(f.read())
    except (OSError, ValueError, KeyError, json.JSONDecodeError) as e:
        print(f"state_media_check: {e}", file=sys.stderr)
        return 2
    problems = check(doc, media)
    for p in problems:
        print(p)
    if not problems:
        print("ok: state media are named as such")
    return 1 if problems else 0


if __name__ == "__main__":
    sys.exit(main())
