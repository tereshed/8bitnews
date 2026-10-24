#!/usr/bin/env python3
"""Генерирует newsroom/contracts/*.schema.json.

Общие определения (источник, факт, группа мнений…) повторяются в нескольких схемах — правь их здесь, а не в JSON.

    python3 newsroom/gen_contracts.py
"""
import json
import pathlib

out = pathlib.Path(__file__).resolve().parent / "contracts"
S = "https://json-schema.org/draft/2020-12/schema"
str1 = {"type": "string", "minLength": 1}

source = {
    "type": "object",
    "required": ["url", "outlet", "published_at"],
    "properties": {
        "url": {"type": "string", "pattern": "^https?://"},
        "outlet": str1,
        "title": {"type": "string"},
        "published_at": {"type": "string", "description": "ISO 8601, UTC if known; 'unknown' if the page has no date"},
        "kind": {"enum": ["primary", "wire", "press", "other"]},
    },
}
fact = {
    "type": "object",
    "required": ["id", "claim", "status", "sources"],
    "properties": {
        "id": {"type": "string", "pattern": "^F[0-9]+$"},
        "claim": {"type": "string", "minLength": 10, "description": "One verifiable statement, English"},
        "status": {"enum": ["confirmed", "disputed", "unverified", "false"]},
        "primary_source": {"$ref": "#/$defs/source"},
        "sources": {"type": "array", "minItems": 1, "items": {"$ref": "#/$defs/source"}},
        "is_new": {"type": "boolean", "description": "True if the fact is part of the latest development (story_brief.development), false for background"},
        "note": {"type": "string"},
    },
    "if": {"properties": {"status": {"const": "confirmed"}}},
    "then": {"anyOf": [
        {"required": ["primary_source"]},
        {"properties": {"sources": {"minItems": 2}}},
    ]},
}
quote_rep = {
    "type": "object",
    "required": ["outlet", "url"],
    "properties": {
        "outlet": str1,
        "author": {"type": "string"},
        "url": {"type": "string", "pattern": "^https?://"},
        "published_at": {"type": "string"},
        "quote": {"type": "string", "description": "Verbatim, <= 40 words"},
        "quote_en": {"type": "string", "description": "English translation if the quote is not in English"},
        "language": {"type": "string"},
    },
}
group = {
    "type": "object",
    "required": ["id", "label", "position", "arguments", "share_estimate", "representatives"],
    "properties": {
        "id": {"type": "string", "pattern": "^L[0-9]+$"},
        "label": {"type": "string", "minLength": 3, "description": "Neutral, descriptive"},
        "position": {"type": "string", "minLength": 20},
        "arguments": {"type": "array", "minItems": 1, "items": str1},
        "share_estimate": {"enum": ["dominant", "significant", "marginal", "unknown"]},
        "share_basis": {"type": "string", "description": "Poll or other evidence behind share_estimate"},
        "on_development": {"type": "string", "description": "How this group reacted to the latest development, or 'no reaction found'"},
        "representatives": {"type": "array", "minItems": 1, "items": {"$ref": "#/$defs/rep"}},
    },
}
cluster = {
    "type": "object",
    "required": ["id", "label", "regions", "position", "contrast_with_local", "representatives"],
    "properties": {
        "id": {"type": "string", "pattern": "^W[0-9]+$"},
        "label": {"type": "string", "minLength": 3},
        "regions": {"type": "array", "minItems": 1, "items": str1},
        "position": {"type": "string", "minLength": 20},
        "contrast_with_local": {"type": "string", "minLength": 10},
        "on_development": {"type": "string", "description": "How this group reacted to the latest development, or 'no reaction found'"},
        "representatives": {"type": "array", "minItems": 1, "items": {"$ref": "#/$defs/rep"}},
    },
}
poll = {
    "type": "object",
    "required": ["pollster", "url", "fieldwork", "finding"],
    "properties": {
        "pollster": str1, "url": {"type": "string", "pattern": "^https?://"},
        "fieldwork": str1, "sample": {"type": "string"}, "finding": str1,
    },
}
outlet_local = {
    "type": "object",
    "required": ["outlet", "leaning", "url"],
    "properties": {
        "outlet": str1,
        "leaning": {"enum": ["left", "lean_left", "center", "lean_right", "right", "unknown"]},
        "url": {"type": "string", "pattern": "^https?://"},
    },
}
outlet_world = {
    "type": "object",
    "required": ["outlet", "region", "ownership", "url"],
    "properties": {
        "outlet": str1, "region": str1,
        "ownership": {"enum": ["private", "public_broadcaster", "state_funded", "state_controlled"]},
        "language": {"type": "string"},
        "url": {"type": "string", "pattern": "^https?://"},
    },
}
timeline_item = {
    "type": "object",
    "required": ["time", "event", "fact_ids"],
    "properties": {
        "time": str1, "event": str1,
        "fact_ids": {"type": "array", "items": {"type": "string", "pattern": "^F[0-9]+$"}},
    },
}
review = {
    "type": "object",
    "required": ["language", "whats_new", "known", "local_opinions", "world_opinions", "unknown"],
    "properties": {
        "language": {"type": "string", "description": "BCP 47, equals channel language"},
        "whats_new": {"type": "string", "minLength": 20},
        "known": {"type": "string", "minLength": 40},
        "local_opinions": {"type": "string", "minLength": 40},
        "world_opinions": {"type": "string", "minLength": 40},
        "unknown": {"type": "string", "minLength": 10},
    },
    "description": "Written in the channel language. Every sentence ends with refs like [F1], [L2], [W1].",
}
new_facts = {
    "type": "array", "minItems": 3, "items": {"$ref": "#/$defs/fact"},
    "contains": {"required": ["is_new"], "properties": {"is_new": {"const": True}}},
    "description": "At least one fact has is_new: true",
}
prior = {
    "type": "object",
    "required": ["published_at", "covered"],
    "properties": {
        "release_id": {"type": ["string", "null"]},
        "published_at": str1,
        "covered": {"type": "string", "minLength": 10, "description": "What that video already told, one or two sentences"},
    },
}
variant = {
    "type": "object",
    "required": ["arm_id", "params"],
    "properties": {
        "experiment_id": {"type": ["string", "null"]},
        "arm_id": str1,
        "params": {"type": "object"},
    },
}

def schema(title, desc, required, props, defs):
    d = {"$schema": S, "title": title, "description": desc, "type": "object",
         "required": required, "properties": props}
    if defs:
        d["$defs"] = defs
    return d

schemas = {
 "story_brief": schema("story_brief",
   "Newsroom editor output: one story for one channel run. Input for Research, Script and Video.",
   ["channel_id", "country", "language", "story", "development", "why_now", "seed_sources", "variant"],
   {"channel_id": str1,
    "country": {"type": "string", "pattern": "^[A-Z]{2}$"},
    "language": str1,
    "story": {"type": "object", "required": ["title", "summary"],
      "description": "The developing story as a whole",
      "properties": {
        "title": {"type": "string", "minLength": 5}, "summary": {"type": "string", "minLength": 20},
        "started_at": {"type": "string", "description": "When the story began, approximate"},
        "hub_story_id": {"type": ["string", "null"]}}},
    "development": {"type": "object", "required": ["summary", "reported_at"],
      "description": "What is new: the turn of the story this video is about",
      "properties": {
        "summary": {"type": "string", "minLength": 20},
        "reported_at": {"type": "string", "description": "When the development was first reported, UTC ISO 8601"}}},
    "prior_coverage": {"type": "array", "items": {"$ref": "#/$defs/prior"},
      "description": "Earlier videos of this channel on the same story"},
    "why_now": {"type": "string", "minLength": 10},
    "seed_sources": {"type": "array", "minItems": 1, "items": {"$ref": "#/$defs/source"}},
    "variant": {"$ref": "#/$defs/variant"}},
   {"source": source, "prior": prior, "variant": variant}),
 "facts_report": schema("facts_report",
   "fact_researcher output: what is known as of now, with sources.",
   ["as_of", "facts", "timeline", "unknowns"],
   {"as_of": {"type": "string", "description": "UTC ISO 8601"},
    "facts": new_facts,
    "timeline": {"type": "array", "items": {"$ref": "#/$defs/timeline_item"}},
    "unknowns": {"type": "array", "items": str1}},
   {"source": source, "fact": fact, "timeline_item": timeline_item}),
 "local_press_report": schema("local_press_report",
   "local_press_analyst output: how opinions split inside the country.",
   ["outlets_reviewed", "groups"],
   {"outlets_reviewed": {"type": "array", "minItems": 6, "items": {"$ref": "#/$defs/outlet"}},
    "groups": {"type": "array", "minItems": 2, "maxItems": 5, "items": {"$ref": "#/$defs/group"}},
    "polls": {"type": "array", "items": {"$ref": "#/$defs/poll"}}},
   {"outlet": outlet_local, "group": group, "rep": quote_rep, "poll": poll}),
 "world_press_report": schema("world_press_report",
   "world_press_analyst output: clusters of opinion in the world press.",
   ["outlets_reviewed", "clusters"],
   {"outlets_reviewed": {"type": "array", "minItems": 6, "items": {"$ref": "#/$defs/outlet"}},
    "clusters": {"type": "array", "minItems": 2, "maxItems": 5, "items": {"$ref": "#/$defs/cluster"}},
    "coverage_notes": {"type": "array", "items": str1,
      "description": "Findings that are not clusters: silent regions or outlets, wire-only coverage, no reactions yet"}},
   {"outlet": outlet_world, "cluster": cluster, "rep": quote_rep}),
 "research_dossier": schema("research_dossier",
   "analyst output and final result of the Research team.",
   ["channel_id", "language", "as_of", "story", "facts", "local_groups", "world_clusters", "unknowns", "review", "risks"],
   {"channel_id": str1, "language": str1,
    "as_of": {"type": "string", "description": "UTC ISO 8601"},
    "story": {"type": "object", "required": ["title", "summary", "development"], "properties": {
        "title": {"type": "string", "minLength": 5}, "summary": {"type": "string", "minLength": 20},
        "development": {"type": "string", "minLength": 20, "description": "What is new, English"}}},
    "facts": new_facts,
    "timeline": {"type": "array", "items": {"$ref": "#/$defs/timeline_item"}},
    "local_groups": {"type": "array", "minItems": 2, "items": {"$ref": "#/$defs/group"}},
    "polls": {"type": "array", "items": {"$ref": "#/$defs/poll"}},
    "world_clusters": {"type": "array", "minItems": 2, "items": {"$ref": "#/$defs/cluster"}},
    "unknowns": {"type": "array", "items": str1},
    "review": {"$ref": "#/$defs/review"},
    "risks": {"type": "array", "items": str1, "description": "Legal and editorial risks; empty array if none"}},
   {"source": source, "fact": fact, "timeline_item": timeline_item, "group": group,
    "cluster": cluster, "rep": quote_rep, "poll": poll, "review": review}),
}
for name, sch in schemas.items():
    with open(out / f"{name}.schema.json", "w", encoding="utf-8") as f:
        json.dump(sch, f, ensure_ascii=False, indent=2)
        f.write("\n")
print("written:", ", ".join(schemas))
