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
risk_issue = {
    "type": "object",
    "required": ["id", "family", "severity", "scope", "where", "excerpt", "rule", "scenario", "fix", "fix_owner"],
    "properties": {
        "id": {"type": "string", "pattern": "^[RPX][0-9]+$",
               "description": "R* from law_report, P* from platform_report, X* added by legal_lead"},
        "family": {"enum": ["defamation", "privacy", "minors", "criminal_case", "likeness", "election", "copyright",
                            "music", "ai_disclosure", "regulatory", "platform_guidelines", "monetization", "reach",
                            "metadata", "other"]},
        "severity": {"enum": ["blocker", "high", "medium", "low"]},
        "scope": {"type": "string", "minLength": 2, "description": "Jurisdiction (US, US-CA, ES, EU) or platform (youtube, tiktok, instagram)"},
        "where": {"type": "string", "minLength": 1, "description": "Scene id, script or package field, or timecode"},
        "excerpt": {"type": "string", "minLength": 1, "description": "Exact text, or a description of the visual or audio element"},
        "rule": {"type": "object", "required": ["ref", "summary"], "properties": {
            "ref": {"type": "string", "minLength": 3, "description": "Knowledge base section (countries/us/legal.md#defamation) or official URL"},
            "summary": {"type": "string", "minLength": 10}}},
        "scenario": {"type": "string", "minLength": 10, "description": "Who could do what: lawsuit, fine, strike, removal, demonetization, reach limit"},
        "fix": {"type": "string", "minLength": 5, "description": "Concrete rewording or action; 'none' only for low severity"},
        "fix_owner": {"enum": ["script", "video", "editor", "none"]},
    },
}
disclosure = {
    "type": "object",
    "required": ["scope", "what"],
    "properties": {
        "scope": {"type": "string", "minLength": 2, "description": "Platform or jurisdiction that requires it"},
        "what": {"type": "string", "minLength": 5, "description": "Flag, label or text, and where it goes"},
        "ref": {"type": "string"},
    },
}
review_mode = {"enum": ["script", "final"], "description": "script: before render; final: rendered video_package"}
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
def risk_report(title, desc, prefix):
    return schema(title, desc,
        ["mode", "checked_against", "issues", "required_disclosures"],
        {"mode": review_mode,
         "checked_against": {"type": "array", "minItems": 1, "items": str1,
                             "description": "Knowledge base files read, with their verified_at"},
         "issues": {"type": "array", "items": {"allOf": [{"$ref": "#/$defs/risk_issue"},
                    {"properties": {"id": {"pattern": f"^{prefix}[0-9]+$"}}}]}},
         "required_disclosures": {"type": "array", "items": {"$ref": "#/$defs/disclosure"}},
         "notes": {"type": "array", "items": str1}},
        {"risk_issue": risk_issue, "disclosure": disclosure})

schemas["law_report"] = risk_report("law_report",
    "legal_counsel output: legal risks under the country's law and the channel's policy.", "R")
schemas["platform_report"] = risk_report("platform_report",
    "platform_reviewer output: risks of sanctions on each enabled platform.", "P")
lr = schema("legal_review",
    "legal_lead output and final result of the Legal team.",
    ["mode", "channel_id", "country", "verdict", "platforms", "issues", "required_disclosures", "summary_ru", "checked_against"],
    {"mode": review_mode,
     "channel_id": str1,
     "country": {"type": "string", "pattern": "^[A-Z]{2}$"},
     "verdict": {"enum": ["pass", "fix_required", "kill"]},
     "platforms": {"type": "object", "minProperties": 1, "additionalProperties": {
         "type": "object", "required": ["verdict"], "properties": {
             "verdict": {"enum": ["pass", "fix_required", "exclude"]},
             "reason": {"type": "string"}}},
         "description": "Per enabled platform; exclude = do not publish there"},
     "issues": {"type": "array", "items": {"allOf": [{"$ref": "#/$defs/risk_issue"}, {"properties": {
         "resolved_by_exclusion": {"type": "boolean",
                                   "description": "The risk exists only on platforms marked exclude"}}}]}},
     "required_disclosures": {"type": "array", "items": {"$ref": "#/$defs/disclosure"}},
     "kill_reason": {"type": "string", "minLength": 10},
     "summary_ru": {"type": "string", "minLength": 20, "description": "For the human moderator, Russian, <= 80 words"},
     "checked_against": {"type": "array", "minItems": 1, "items": str1}},
    {"risk_issue": risk_issue, "disclosure": disclosure})
lr["allOf"] = [
    {"if": {"properties": {"verdict": {"const": "kill"}}}, "then": {"required": ["kill_reason"]}},
    {"if": {"required": ["issues"], "properties": {"issues": {"contains": {
         "properties": {"severity": {"enum": ["blocker", "high"]}},
         "not": {"required": ["resolved_by_exclusion"], "properties": {"resolved_by_exclusion": {"const": True}}}}}}},
     "then": {"properties": {"verdict": {"enum": ["fix_required", "kill"]}}},
     "description": "An open blocker or high issue forbids pass"},
]
schemas["legal_review"] = lr

blocks = ["hook", "whats_new", "known", "local", "world", "unknown", "outro"]
moods = ["neutral", "curious", "tense", "urgent", "somber", "hopeful"]
def refs_for(block, field, prefix):
    return {"if": {"properties": {"blocks": {"contains": {"const": block}}}},
            "then": {"required": [field], "properties": {field: {"minItems": 1}}},
            "description": f"A '{block}' scene references at least one {prefix}* item"}
scene = {
    "type": "object",
    "required": ["id", "blocks", "shot", "duration_s", "narration", "word_count", "on_screen_text", "visual",
                 "real_people", "fact_ids"],
    "properties": {
        "id": {"type": "string", "pattern": "^S[0-9]+$"},
        "blocks": {"type": "array", "minItems": 1, "uniqueItems": True, "items": {"enum": blocks},
                   "description": "Structure blocks from format.md this scene covers; merged blocks list several"},
        "shot": {"enum": ["anchor", "scene"],
                 "description": "anchor: the host at the studio desk; scene: a full-screen story scene"},
        "duration_s": {"type": "number", "exclusiveMinimum": 0},
        "narration": {"type": "string", "minLength": 1, "description": "Voiceover, written for the ear"},
        "word_count": {"type": "integer", "minimum": 1, "description": "Whitespace-separated words of narration"},
        "on_screen_text": {"type": "string", "maxLength": 60},
        "source_plate": {"type": "string", "description": "Source label under a number or quote: 'Source: X · Oct 21'"},
        "visual": {"type": "string", "minLength": 20, "description": "What is on screen and what moves, English"},
        "real_people": {"type": "array", "items": str1, "description": "Real people shown; the template adds the 'AI illustration' plate"},
        "fact_ids": {"type": "array", "items": {"type": "string", "pattern": "^F[0-9]+$"}},
        "group_ids": {"type": "array", "items": {"type": "string", "pattern": "^L[0-9]+$"}},
        "cluster_ids": {"type": "array", "items": {"type": "string", "pattern": "^W[0-9]+$"}},
    },
    "allOf": [refs_for("whats_new", "fact_ids", "F"), refs_for("known", "fact_ids", "F"),
              refs_for("local", "group_ids", "L"), refs_for("world", "cluster_ids", "W")],
}
platform_meta = {
    "type": "object",
    "required": ["title", "summary", "hashtags"],
    "properties": {
        "title": {"type": "string", "minLength": 10, "maxLength": 100},
        "summary": {"type": "string", "minLength": 20, "description": "1-2 sentences from review.whats_new and review.known"},
        "hashtags": {"type": "array", "minItems": 1, "uniqueItems": True,
                     "items": {"type": "string", "pattern": "^#[A-Za-z0-9_]+$"}},
        "tags": {"type": "array", "uniqueItems": True, "items": str1, "description": "YouTube only"},
    },
}
music = {
    "type": "object",
    "required": ["enabled"],
    "properties": {
        "enabled": {"type": "boolean"},
        "mood": {"enum": moods, "description": "Mood from music/catalog.yaml"},
        "energy": {"type": "integer", "minimum": 1, "maximum": 5},
        "cue": {"type": "string", "minLength": 10, "description": "Where the track enters, ducks, swells and fades"},
    },
    "if": {"properties": {"enabled": {"const": True}}},
    "then": {"required": ["mood", "energy", "cue"]},
}
sc = schema("script",
    "scriptwriter output and final result of the Script team: the video script for Video, QA and Legal.",
    ["channel_id", "language", "duration_s", "word_count", "serious_mode", "hook_type", "music",
     "scenes", "metadata", "cover"],
    {"channel_id": str1,
     "language": str1,
     "dossier": {"type": "string", "description": "research_dossier artifact id or repository path"},
     "template": {"type": "string", "minLength": 1,
                  "description": "Only when the experiment variant pins a template; otherwise omitted and Video takes "
                                 "the template from hub template_pick (docs/architecture.md, Templates)"},
     "duration_s": {"type": "integer", "minimum": 15, "maximum": 180,
                    "description": "Planned length; the channel range is in channel.yaml"},
     "word_count": {"type": "integer", "minimum": 1, "description": "Sum of scene word_count"},
     "serious_mode": {"type": "boolean", "description": "Deaths, violence, terror, disasters: see format.md"},
     "serious_reason": {"type": "string"},
     "hook_type": {"enum": ["new_fact", "number", "quote", "contrast"]},
     "music": {"$ref": "#/$defs/music"},
     "pronunciations": {"type": "array", "items": {"type": "object", "required": ["text", "say"], "properties": {
         "text": str1, "say": {"type": "string", "minLength": 1, "description": "Respelling, e.g. 'ess-EYE-lee'"}}}},
     "scenes": {"type": "array", "minItems": 4, "maxItems": 14, "items": {"$ref": "#/$defs/scene"},
                "prefixItems": [{"properties": {"blocks": {"contains": {"const": "hook"}}}}],
                "description": "In order; the first scene is the hook"},
     "metadata": {"type": "object", "minProperties": 1,
                  "propertyNames": {"enum": ["youtube", "tiktok", "instagram"]},
                  "additionalProperties": {"$ref": "#/$defs/platform_meta"},
                  "description": "Per enabled platform, by channels/<id>/metadata.md"},
     "cover": {"type": "object", "required": ["frame", "visual", "text"], "properties": {
         "frame": {"type": "string", "pattern": "^(custom|S[0-9]+)$"},
         "visual": {"type": "string", "minLength": 10},
         "text": {"type": "string", "minLength": 3, "maxLength": 30}}},
     "notes": {"type": "array", "items": str1}},
    {"scene": scene, "platform_meta": platform_meta, "music": music})
sc["allOf"] = [
    {"if": {"properties": {"serious_mode": {"const": True}}},
     "then": {"required": ["serious_reason"], "properties": {"music": {"anyOf": [
         {"properties": {"enabled": {"const": False}}},
         {"properties": {"mood": {"enum": ["somber", "neutral"]}, "energy": {"maximum": 2}}}]}}},
     "description": "Serious mode: no music, or a somber or neutral track at energy 1-2"},
]
schemas["script"] = sc

media = {
    "type": "object",
    "required": ["sha256", "bytes"],
    "properties": {
        "key": {"type": "string", "minLength": 1, "description": "S3 object key from hub media_upload_url"},
        "path": {"type": "string", "minLength": 1, "description": "Local path, for studio runs without hub"},
        "sha256": {"type": "string", "pattern": "^[0-9a-f]{64}$"},
        "bytes": {"type": "integer", "minimum": 1},
        "content_type": {"type": "string"},
    },
    "anyOf": [{"required": ["key"]}, {"required": ["path"]}],
}
vscene = {
    "type": "object",
    "required": ["id", "start_s", "end_s", "narration", "on_screen_text", "keyframes", "real_people", "ai_label_shown"],
    "properties": {
        "id": {"type": "string", "pattern": "^S[0-9]+$", "description": "Scene id from the script"},
        "start_s": {"type": "number", "minimum": 0},
        "end_s": {"type": "number", "exclusiveMinimum": 0},
        "narration": {"type": "string", "description": "Text as voiced; differs from the script only by pronunciations"},
        "on_screen_text": {"type": "string"},
        "keyframes": {"type": "array", "minItems": 1, "items": {"$ref": "#/$defs/media"},
                      "description": "PNG frames for QA and Legal: at least the scene's first frame"},
        "real_people": {"type": "array", "items": str1},
        "ai_label_shown": {"type": "boolean", "description": "The 'AI illustration' plate is on screen in this scene"},
    },
    "if": {"properties": {"real_people": {"minItems": 1}}},
    "then": {"properties": {"ai_label_shown": {"const": True}}},
}
vmusic = {
    "type": "object",
    "required": ["enabled"],
    "properties": {
        "enabled": {"type": "boolean"},
        "track_id": {"type": "string", "description": "Track id from music/catalog.yaml"},
        "license": {"type": "string", "description": "License as in the catalog, e.g. CC0-1.0"},
        "attribution": {"type": "string", "description": "Required credit line, if the license asks for one"},
        "duck_db": {"type": "number", "maximum": 0, "description": "Music gain under the voice"},
    },
    "if": {"properties": {"enabled": {"const": True}}},
    "then": {"required": ["track_id", "license"]},
}
vp = schema("video_package",
    "video_producer output and final result of the Video team: the rendered video and everything QA and Legal "
    "need without rewatching it in full.",
    ["channel_id", "script", "template", "duration_s", "files", "render", "voice", "transcript", "scenes", "music",
     "disclosures", "validate"],
    {"channel_id": str1,
     "script": {"type": "string", "minLength": 1, "description": "script artifact id or repository path"},
     "template": str1,
     "duration_s": {"type": "number", "exclusiveMinimum": 0, "description": "Measured length of the rendered video"},
     "files": {"type": "object", "required": ["video", "subtitles", "cover"], "properties": {
         "video": {"$ref": "#/$defs/media"},
         "video_no_music": {"$ref": "#/$defs/media", "description": "Same video without music, for YouTube"},
         "subtitles": {"$ref": "#/$defs/media", "description": "SRT with the burned-in subtitle text and timings"},
         "cover": {"$ref": "#/$defs/media"}}},
     "render": {"type": "object", "required": ["width", "height", "fps", "video_codec", "audio_codec", "loudness_lufs"],
                "properties": {
                    "width": {"type": "integer"}, "height": {"type": "integer"}, "fps": {"type": "number"},
                    "video_codec": str1, "audio_codec": str1,
                    "loudness_lufs": {"type": "number", "description": "Integrated loudness, target about -14"},
                    "true_peak_dbtp": {"type": "number"},
                    "engine": {"type": "string", "description": "Render engine and version"}}},
     "voice": {"type": "object", "required": ["provider", "voice_id"], "properties": {
         "provider": str1, "voice_id": str1, "model": {"type": "string"}, "speed": {"type": "number"},
         "wpm": {"type": "number", "description": "Measured words per minute of the narration"}}},
     "transcript": {"type": "string", "minLength": 1, "description": "Full narration as voiced, scenes joined in order"},
     "scenes": {"type": "array", "minItems": 1, "items": {"$ref": "#/$defs/vscene"}},
     "music": {"$ref": "#/$defs/vmusic"},
     "disclosures": {"type": "object", "required": ["ai_voice", "ai_visuals", "platforms"], "properties": {
         "ai_voice": {"type": "boolean"},
         "ai_visuals": {"type": "boolean"},
         "platforms": {"type": "object", "minProperties": 1,
                       "propertyNames": {"enum": ["youtube", "tiktok", "instagram"]},
                       "additionalProperties": {"type": "object", "required": ["ai_label"], "properties": {
                           "ai_label": {"type": "boolean", "description": "The platform's AI-content flag set on upload"}}},
                       "description": "Per enabled platform of the channel"}}},
     "validate": {"type": "object", "required": ["passed", "checks"], "properties": {
         "passed": {"type": "boolean"},
         "checks": {"type": "array", "minItems": 1, "items": {"type": "object", "required": ["id", "ok"], "properties": {
             "id": str1, "ok": {"type": "boolean"}, "value": {}, "expected": {}, "note": {"type": "string"}}}}},
         "description": "Result of studio validate on this package"},
     "notes": {"type": "array", "items": str1}},
    {"media": media, "vscene": vscene, "vmusic": vmusic})
vp["allOf"] = [
    {"if": {"required": ["duration_s", "music"],
            "properties": {"duration_s": {"exclusiveMinimum": 60}, "music": {"properties": {"enabled": {"const": True}}}}},
     "then": {"properties": {"files": {"required": ["video_no_music"]}}},
     "description": "Longer than 60 s with music: a YouTube version without music is required"},
]
schemas["video_package"] = vp

uuid = {"type": "string", "pattern": "^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$"}
schemas["stage_report"] = schema("stage_report",
    "Newsroom producer output: what another team returned in a delegation_result, so the router can choose the next "
    "stage. The router sees only artifact summaries, so the summary of this artifact carries the verdict and the ids.",
    ["team", "state", "delegation_result_id", "artifacts", "verdict"],
    {"team": {"enum": ["Research", "Script", "Legal", "Video", "QA"]},
     "state": {"enum": ["done", "failed", "needs_human", "expired", "cancelled"],
               "description": "State of the delegation as reported in delegation_result"},
     "delegation_result_id": uuid,
     "artifacts": {"type": "object", "propertyNames": {"pattern": "^[a-z_]+$"}, "additionalProperties": uuid,
                   "description": "Latest artifact id per kind returned by the team, e.g. {\"research_dossier\": \"…\"}"},
     "verdict": {"enum": ["approved", "not_approved", "pass", "fix_required", "kill", "missing"],
                 "description": "Research/Script: approved by the team's reviewer or not; Legal: legal_review.verdict; "
                                "missing: the expected artifact is absent"},
     "fix_owners": {"type": "object", "propertyNames": {"enum": ["script", "video", "editor", "none"]},
                    "additionalProperties": {"type": "array", "items": str1},
                    "description": "Legal only: open issue ids grouped by fix_owner"},
     "platforms": {"type": "object", "additionalProperties": {"enum": ["pass", "fix_required", "exclude"]},
                   "description": "Legal only: verdict per platform"},
     "notes": {"type": "array", "items": str1}},
    {})

for name, sch in schemas.items():
    with open(out / f"{name}.schema.json", "w", encoding="utf-8") as f:
        json.dump(sch, f, ensure_ascii=False, indent=2)
        f.write("\n")
print("written:", ", ".join(schemas))
