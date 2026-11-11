import json
import subprocess
import sys
from pathlib import Path

import pytest

ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT / "newsroom" / "tools"))
import state_media_check as smc  # noqa: E402

TOOL = ROOT / "newsroom" / "tools" / "state_media_check.py"
ES = smc.StateMedia((ROOT / "countries" / "es" / "sources.yaml").read_text(encoding="utf-8"))


@pytest.mark.parametrize("cc", ["es", "us"])
def test_reads_sources_yaml_as_pyyaml_does(cc):
    yaml = pytest.importorskip("yaml")
    text = (ROOT / "countries" / cc / "sources.yaml").read_text(encoding="utf-8")
    doc = yaml.safe_load(text)
    want = {o["domain"]: o for o in doc["local"] + [o for r in doc["world"] for o in r["outlets"]]}
    got = {o["domain"]: o for o in smc.outlets(text)}
    assert got.keys() == want.keys()
    for domain, o in want.items():
        for key in ("outlet", "ownership", "aka"):
            assert got[domain].get(key) == o.get(key), (domain, key)


def test_parses_quotes_and_lists_with_commas():
    o = smc.parse_inline('outlet: A, domain: a.es, aka: [A, "B, C"], note: "x, y: z", paywall: true')
    assert o == {"outlet": "A", "domain": "a.es", "aka": ["A", "B, C"], "note": "x, y: z", "paywall": True}


@pytest.mark.parametrize("url, outlet", [
    ("https://www.rtve.es/noticias/x", "RTVE"),
    ("https://efe.com/espana/x", "Agencia EFE"),
    ("https://efeverde.com/x", "EFE Verde"),
    ("https://www.aljazeera.com/news/x", "Al Jazeera"),
    ("https://www.elpais.com/x", None),
    ("https://notefe.com/x", None),
    # foreign state broadcasters and agencies (state budget or state ownership, 11.11.2026)
    ("https://www.dw.com/es/x", "Deutsche Welle"),
    ("https://www.france24.com/es/x", "France 24"),
    ("https://www.rfi.fr/es/x", "RFI"),
    ("https://www.rferl.org/a/x", "Radio Free Europe/Radio Liberty"),
    ("https://www.aa.com.tr/en/x", "Anadolu Agency"),
    ("https://www.lusa.pt/x", "Lusa"),
    ("https://www.cgtn.com/x", "CGTN"),
    ("https://www.svoboda.org/a/x", "Radio Svoboda"),
    ("https://www.hispantv.com/x", "HispanTV"),
    ("https://www.prensa-latina.cu/x", "Prensa Latina"),
    ("https://www.telesurtv.net/x", "teleSUR"),
    ("https://www.bbc.com/news/x", None),  # licence fee: public_broadcaster, no mark
])
def test_matches_a_domain_and_its_subdomains(url, outlet):
    assert ES.by_url(url) == outlet


@pytest.mark.parametrize("name, outlet", [
    ("EFE (via Infobae)", "Agencia EFE"),
    ("Infobae (EFE)", "Agencia EFE"),
    ("EFE vía Infobae", "Agencia EFE"),
    ("Infobae / EFE", "Agencia EFE"),
    ("RTVE via Yahoo", "RTVE"),
    ("Al Jazeera", "Al Jazeera"),
    ("EFE EME", None),  # a music magazine
    ("Público", None),
    ("elDiario.es", None),
])
def test_matches_a_reprint_as_hub_does(name, outlet):
    assert ES.by_outlet(name) == outlet


def dossier(**over):
    doc = {
        "channel_id": "8bitnews-es",
        "facts": [{"id": "F1", "claim": "x" * 10, "status": "confirmed",
                   "sources": [{"url": "https://www.rtve.es/a", "outlet": "RTVE", "published_at": "unknown"}],
                   "note": "RTVE is state-funded (medio estatal)."}],
        "local_groups": [],
        "world_clusters": [{"id": "W1", "label": "Gulf outlets", "position": "State-funded Al Jazeera argues…",
                            "representatives": [{"outlet": "Al Jazeera", "url": "https://www.aljazeera.com/a"}]}],
        "review": {"language": "es-ES", "whats_new": "Según RTVE (medio estatal), algo pasó [F1].",
                   "known": "Nada más [F1].", "local_opinions": "x [F1].", "world_opinions": "y [W1].", "unknown": "z."},
        "risks": ["RTVE and Al Jazeera are state media: mark them (medio estatal)."],
    }
    doc.update(over)
    return doc


def test_a_dossier_that_names_its_state_media_passes():
    assert smc.check(dossier(), ES) == []


def test_each_missing_mark_is_a_line():
    doc = dossier(risks=["Nothing else."])
    doc["facts"][0]["note"] = "From RTVE's live page."
    doc["world_clusters"][0]["position"] = "Al Jazeera argues…"
    doc["review"]["known"] = "Según RTVE, nada más [F1]. Otra frase sin medios [F1]."
    problems = smc.check(doc, ES)
    assert [p.split(":")[0] for p in problems] == ["F1", "W1", "review.known", "risks"]


def test_a_reprint_counts_and_a_spanish_mark_does_too():
    doc = dossier()
    doc["facts"][0]["sources"] = [{"url": "https://www.infobae.com/a", "outlet": "EFE (via Infobae)", "published_at": "unknown"}]
    doc["facts"][0]["note"] = "Teletipo de EFE, agencia financiada por el Estado."
    doc["review"]["known"] = "Según EFE, agencia estatal, nada más [F1]."
    assert smc.check(doc, ES) == []
    doc["facts"][0]["note"] = "Teletipo de EFE."
    assert [p.split(":")[0] for p in smc.check(doc, ES)] == ["F1"]


@pytest.mark.parametrize("sentence, named", [
    ("Según Lusa, el Gobierno portugués no reaccionó [W1].", ["Lusa"]),
    ("Según LUSA, nada [W1].", ["Lusa"]),
    ("La prensa lusa apenas recoge la huelga [W1].", []),  # «lusa» = Portuguese
    ("La letra efe no es una agencia.", []),
    ("Según EFE, nada más [F1].", ["Agencia EFE"]),
    ("Según Efe, hay ocho detenidos [F1].", ["Agencia EFE"]),  # the common Spanish spelling
    ("Según Tass, X.", ["TASS"]),
    ("DW y France 24 lo destacan [W1].", ["Deutsche Welle", "France 24"]),
    ("Según Prensa Latina, X [W1].", ["Prensa Latina"]),
    ("La prensa latina apenas lo recoge [W1].", []),
    ("Radio Liberty y Anadolu lo recogen [W1].", ["Anadolu Agency", "Radio Free Europe/Radio Liberty"]),
])
def test_each_word_keeps_its_first_letter_as_written(sentence, named):
    assert ES.named_in(sentence) == named


@pytest.mark.parametrize("sentence, unmarked", [
    ("Según DW (medio estatal alemán), X [W1].", []),
    ("Según la cadena estatal alemana DW, X [W1].", []),
    ("DW, funded from the German federal budget (state-funded), centres rents.", []),
    ("DW (servicio público alemán) y Al Jazeera (medio estatal) coinciden [W1].", ["Deutsche Welle"]),
    ("Al Jazeera (medio estatal) y DW coinciden [W1].", ["Deutsche Welle"]),
    ("State-funded DW (11 Nov, 09:11 UTC, with AP material) reported X.", []),  # a mark before the name counts
    ("Los medios estatales Al Jazeera (Catar) y Anadolu (Turquía) lo recuerdan [W2].", []),
    ("Según DW, el Gobierno no ha respondido a ninguna de las preguntas de los sindicatos ni a la respuesta gubernamental [W1].",
     ["Deutsche Welle"]),
])
def test_the_mark_stands_next_to_the_name(sentence, unmarked):
    assert ES.unmarked_in(sentence) == unmarked


def test_abbreviations_dont_end_a_sentence():
    text = "Radio Free Europe, financiada por EE. UU. (medio estatal), lo recoge [W1]. Otra frase [W1]."
    assert smc.sentences(text) == ["Radio Free Europe, financiada por EE. UU. (medio estatal), lo recoge [W1].", "Otra frase [W1]."]
    assert ES.unmarked_in(smc.sentences(text)[0]) == []


def test_a_state_outlet_named_in_a_clusters_text_needs_a_mark_too():
    # 11.11.2026, run 843bdf00: W1 leaned on DW's coverage with no DW link and called it a public broadcaster.
    # One mark elsewhere in the text doesn't cover DW: each sentence that names a state outlet carries its own.
    doc = dossier()
    doc["world_clusters"][0]["position"] = "State-funded Al Jazeera argues X. DW (public broadcaster) likewise centres rents."
    assert [p.split(":")[0] for p in smc.check(doc, ES)] == ["W1"]
    doc["world_clusters"][0]["position"] = "State-funded Al Jazeera argues X. DW, funded from the German federal budget (state-funded), centres rents."
    assert smc.check(doc, ES) == []
    # Once marked in the cluster's text, later mentions there needn't repeat it (review sentences still do).
    doc["world_clusters"][0]["position"] = "State-funded Al Jazeera argues X. Al Jazeera adds Y. State-funded DW centres rents."
    assert smc.check(doc, ES) == []
    # A representative's outlet is held to the same rule where the text names it.
    doc["world_clusters"][0]["representatives"].append({"outlet": "DW", "url": "https://www.dw.com/es/a"})
    doc["world_clusters"][0]["position"] = "State-funded Al Jazeera argues X. DW (public broadcaster) likewise centres rents."
    assert [p.split(":")[0] for p in smc.check(doc, ES)] == ["W1"]


def test_no_state_media_asks_for_nothing():
    doc = dossier(facts=[{"id": "F1", "claim": "x" * 10, "status": "unverified",
                          "sources": [{"url": "https://elpais.com/a", "outlet": "El País", "published_at": "unknown"}]}],
                  world_clusters=[], risks=[])
    doc["review"]["whats_new"] = "Según El País, algo pasó [F1]."
    assert smc.check(doc, ES) == []


def test_a_world_report_is_checked_for_its_clusters():
    report = {"clusters": [{"id": "W1", "label": "Gulf", "position": "Al Jazeera argues…",
                            "representatives": [{"outlet": "Al Jazeera", "url": "https://www.aljazeera.com/a"}]}],
              "outlets_reviewed": []}
    assert [p.split(":")[0] for p in smc.check(report, ES)] == ["W1"]


def run(tmp_path, doc, *args):
    f = tmp_path / "d.json"
    f.write_text(json.dumps(doc), encoding="utf-8")
    return subprocess.run([sys.executable, str(TOOL), str(f), *args], capture_output=True, text=True)


def test_exit_codes(tmp_path):
    ok = run(tmp_path, dossier())
    assert ok.returncode == 0 and ok.stdout.startswith("ok"), ok.stdout + ok.stderr
    bad = run(tmp_path, dossier(risks=[]))
    assert bad.returncode == 1 and bad.stdout.startswith("risks:"), bad.stdout
    # The country comes from the channel; --country stands in when there is none.
    report = {"clusters": [], "outlets_reviewed": []}
    assert run(tmp_path, report).returncode == 2
    assert run(tmp_path, report, "--country", "ES").returncode == 0
    assert run(tmp_path, ["not", "an", "object"]).returncode == 2
