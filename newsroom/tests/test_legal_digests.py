import re
import sys
from pathlib import Path

import pytest

ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT / "newsroom" / "tools"))
from kb_section import headings, section  # noqa: E402

DIGESTS = {
    "countries/us/legal.md": "countries/us/legal_digest.md",
    "countries/es/legal.md": "countries/es/legal_digest.md",
    "operator/legal.md": "operator/legal_digest.md",
    "platforms/youtube/rules.md": "platforms/youtube/rules_digest.md",
}
# Every section past Legal reports cited (2026-11-04..08, 321 reports): a digest that lost one would have missed a finding.
CITED = {
    "countries/us/legal.md": ["defamation-quotes-parody", "copyright-trademarks", "defamation-standards",
                              "defamation-false-light", "defamation-republication"],
    "countries/es/legal.md": ["veracity", "music", "reportaje-neutral", "required-disclosures"],
    "operator/legal.md": ["ai-act-art50", "ai-act-our-case"],
    "platforms/youtube/rules.md": ["4-монетизация-и-право-на-рекомендации", "7-метаданные-заголовки-обложки-хэштеги",
                                   "9-чек-лист-красные-флаги-для-ревьюера", "6-копирайт-и-музыка"],
}
PAIRS = sorted(DIGESTS.items())


def anchors(digest: str) -> list[str]:
    return re.findall(r"\[#([^\]\s]+)\]", digest)


def covered_lines(source: str, digest: str) -> set[int]:
    """Lines of the source headings the digest names, directly or through a section containing them."""
    items = headings(source)
    named = {h["line"] for h in items if h["anchors"] & set(anchors(digest))}
    lines = set()
    for i, h in enumerate(items):
        if h["line"] in named:
            lines.add(h["line"])
            for sub in items[i + 1:]:
                if sub["level"] <= h["level"]:
                    break
                lines.add(sub["line"])
    return lines


@pytest.mark.parametrize("source,digest", PAIRS)
def test_every_anchor_opens_a_section(source, digest):
    text, kb = (ROOT / digest).read_text(), (ROOT / source).read_text()
    assert anchors(text), digest
    for anchor in anchors(text):
        assert section(kb, anchor), f"{digest}: [#{anchor}] opens nothing in {source}"


@pytest.mark.parametrize("source,digest", PAIRS)
def test_every_section_of_the_base_is_in_the_digest(source, digest):
    text, kb = (ROOT / digest).read_text(), (ROOT / source).read_text()
    lines = covered_lines(kb, text)
    missing = [h["title"] for h in headings(kb) if h["level"] in (2, 3) and h["line"] not in lines]
    assert not missing, f"{digest} misses {missing}"


@pytest.mark.parametrize("source,digest", PAIRS)
def test_digest_states_its_source_and_date_and_the_duty_to_open_the_section(source, digest):
    head = "\n".join((ROOT / digest).read_text().splitlines()[:6])
    date = re.search(r"verified_at:\s*(\S+)", (ROOT / source).read_text())
    assert source in head and "kb_section.py" in head
    assert f"verified_at: {date.group(1) if date else 'not declared'}" in head


@pytest.mark.parametrize("source,digest", PAIRS)
def test_digest_is_much_smaller_than_the_base(source, digest):
    assert (ROOT / digest).stat().st_size < 0.45 * (ROOT / source).stat().st_size


@pytest.mark.parametrize("source,anchor", [(s, a) for s, refs in sorted(CITED.items()) for a in refs])
def test_sections_past_reports_cited_stay_in_the_digest(source, anchor):
    kb, text = (ROOT / source).read_text(), (ROOT / DIGESTS[source]).read_text()
    heading = next((h for h in headings(kb) if anchor in h["anchors"]), None)
    assert heading, f"{source}#{anchor} no longer exists"
    assert heading["line"] in covered_lines(kb, text), f"{DIGESTS[source]} lost {anchor}"
