#!/usr/bin/env python3
"""Печатает раздел базы знаний Legal целиком (с подразделами):

    python3 newsroom/tools/kb_section.py countries/us/legal.md defamation-false-light
    python3 newsroom/tools/kb_section.py platforms/youtube/rules.md 4
    python3 newsroom/tools/kb_section.py countries/us/legal.md          # оглавление

Якорь — id из `<a id="…">` в заголовке, slug заголовка (`4-монетизация-и-право-на-рекомендации`) или его номер (`4`).
"""
from __future__ import annotations

import re
import sys
from pathlib import Path

HEADING = re.compile(r"^(#{1,6})\s+(.*\S)\s*$")
ANCHOR_ID = re.compile(r'<a\s+id="([^"]+)"\s*>\s*</a>')
TAG = re.compile(r"<[^>]+>")


def slug(title: str) -> str:
    text = TAG.sub("", title).strip().lower()
    text = re.sub(r"[^\w\s-]", "", text)
    return re.sub(r"\s", "-", text)


def headings(text: str) -> list[dict]:
    """Headings outside code fences: level, title, line number, the anchors they answer to."""
    found, fenced = [], False
    for number, line in enumerate(text.splitlines()):
        if line.lstrip().startswith("```"):
            fenced = not fenced
        match = None if fenced else HEADING.match(line)
        if match:
            title = match.group(2)
            anchors = set(ANCHOR_ID.findall(title)) | {slug(title)}
            prefix = re.match(r"(\d+(?:\.\d+)*)[.\s]", TAG.sub("", title).strip())
            if prefix:
                anchors.add(prefix.group(1))
            found.append({"level": len(match.group(1)), "title": TAG.sub("", title).strip(), "line": number,
                          "anchors": anchors, "id": next(iter(ANCHOR_ID.findall(title)), None)})
    return found


def section(text: str, anchor: str) -> str | None:
    """The section the anchor names, down to the next heading of its level or higher; None if there is none."""
    anchor = anchor.lstrip("#").strip()
    items = headings(text)
    for i, item in enumerate(items):
        if anchor in item["anchors"]:
            end = next((h["line"] for h in items[i + 1:] if h["level"] <= item["level"]), None)
            lines = text.splitlines()
            return "\n".join(lines[item["line"]:end]).strip()
    return None


def main() -> int:
    if len(sys.argv) not in (2, 3):
        print(__doc__)
        return 2
    text = Path(sys.argv[1]).read_text()
    if len(sys.argv) == 2:
        for item in headings(text):
            print(f"{'  ' * (item['level'] - 2)}{item['title']}  [#{item['id'] or slug(item['title'])}]")
        return 0
    found = section(text, sys.argv[2])
    if found is None:
        print(f"нет раздела {sys.argv[2]!r} в {sys.argv[1]}; оглавление: python3 newsroom/tools/kb_section.py {sys.argv[1]}",
              file=sys.stderr)
        return 1
    print(found)
    return 0


if __name__ == "__main__":
    sys.exit(main())
