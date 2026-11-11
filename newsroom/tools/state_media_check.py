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
- each sentence of a cluster's `label`, `position` or `on_development`, and of `review`, that names a state
  outlet says so next to the name: just before it, just after it, or in a parenthesis right after it
  («DW (servicio público)» is not marked, «Al Jazeera (medio estatal) y DW» marks only Al Jazeera).
  Each word of a name keeps its first letter as written («EFE», «Efe», «Lusa», «Prensa Latina»; not «efe»,
  «la prensa lusa», «la prensa latina»);
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
                self.prose.append((name_pattern(name), o["outlet"]))

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

    def unmarked_in(self, sentence: str) -> list[str]:
        """State outlets the sentence names with no mark next to any mention of them."""
        sentence = protect(sentence)  # same length: positions stay
        named, marked = set(), set()
        for pattern, outlet in self.prose:
            for m in pattern.finditer(sentence):
                named.add(outlet)
                if marked_next_to(sentence, m.start(), m.end()):
                    marked.add(outlet)
        return sorted(named - marked)


def name_pattern(name: str) -> re.Pattern:
    """Each word keeps its first letter as written and ignores case in the rest: «EFE», «Efe», «LUSA», «Lusa»,
    «Prensa Latina», but not «efe» (the letter), «la prensa lusa» (Portuguese) or «la prensa latina»."""
    def word(w: str) -> str:
        return re.escape(w[0]) + (r"(?i:" + re.escape(w[1:]) + ")" if len(w) > 1 else "")
    return re.compile(r"(?<!\w)" + r"\s+".join(word(w) for w in name.split()) + r"(?!\w)")


def marked_next_to(sentence: str, start: int, end: int) -> bool:
    """The mark belongs to this name: a parenthesis right after the name decides («DW (servicio público)» is not
    marked); otherwise it stands just before the name or in the words right after it, up to the second comma or
    the end of the clause («DW, funded from the federal budget (state-funded), …»). Marks inside earlier
    parentheses belong to other names («Al Jazeera (medio estatal) y DW»)."""
    before = re.sub(r"\([^()]*\)", " ", sentence[max(0, start - 40):start])[-30:]
    if MARK.search(before):  # «state-funded DW (11 Nov)», «los medios estatales Al Jazeera y Anadolu»
        return True
    after = sentence[end:]
    if after.lstrip().startswith("("):
        inner = after.lstrip()[1:]
        return bool(MARK.search(inner[:inner.find(")")] if ")" in inner else inner))
    tail = after[:80]
    stop = re.search(r"[.;!?]", tail)
    commas = [i for i, ch in enumerate(tail) if ch == ","]
    cut = min([len(tail)] + ([stop.start()] if stop else []) + (commas[1:2]))
    return bool(MARK.search(tail[:cut]))


# Abbreviations whose dots don't end a sentence: «EE. UU.», «U.S.», «Sr.».
ABBREV = re.compile(r"\b(?:EE\. ?UU|U\.S|U\.K|Sra?|Dra?|núm|p\. ?ej)\.")


def protect(text: str) -> str:
    """The abbreviations' dots as one-dot leaders, same length."""
    return ABBREV.sub(lambda m: m.group(0).replace(".", "․"), text or "")


def sentences(text: str) -> list[str]:
    return [s.replace("․", ".") for s in re.split(r"(?<=[.!?])\s+(?=\S)", protect(text)) if s.strip()]


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
        text = " ".join(cluster.get(k) or "" for k in ("label", "position", "on_development"))
        silent = [o for o in cited if o not in media.named_in(text)]  # named ones are held to the sentence rule below
        if silent and not MARK.search((cluster.get("position") or "") + " " + (cluster.get("label") or "")):
            problems.append(f"{cluster.get('id')}: a representative is from {', '.join(silent)}; say it is state media in position")
        # A state outlet the cluster's text names, with or without a representative from it, is marked next to
        # the name at least once in that text (11.11: «DW (public broadcaster) likewise centres rents»).
        named, marked = set(), set()
        for field in ("label", "position", "on_development"):
            for sentence in sentences(cluster.get(field) or ""):
                here = set(media.named_in(sentence))
                named |= here
                marked |= here - set(media.unmarked_in(sentence))
        used.update(named)
        if named - marked:
            problems.append(f"{cluster.get('id')}: the text names {', '.join(sorted(named - marked))} and never says "
                            "it is state media next to the name; say it once in position")
    for group in doc.get("local_groups") or []:
        used.update(o for o in (media.of(r) for r in group.get("representatives") or []) if o)
    review = doc.get("review")
    if isinstance(review, dict):
        for field, text in review.items():
            if field == "language" or not isinstance(text, str):
                continue
            for sentence in sentences(text):
                unmarked = media.unmarked_in(sentence)
                if unmarked:
                    problems.append(f"review.{field}: «{sentence[:90]}» names {', '.join(unmarked)} without saying it is state media")
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
