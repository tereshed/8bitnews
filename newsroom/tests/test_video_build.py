"""newsroom/tools/video_build.py: studio pick's cover layout reaches studio build."""
import argparse
import json
import sys
from pathlib import Path

import pytest

ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT / "newsroom/tools"))
import video_build  # noqa: E402


@pytest.mark.parametrize("picked,passed", [("top", "top"), ("bottom", "bottom"), (None, "bottom")])
def test_the_build_gets_the_cover_layout_studio_pick_chose(tmp_path, monkeypatch, picked, passed):
    """None: a pick.json from before the field, which means the bottom."""
    (tmp_path / "script.json").write_text(json.dumps({"channel_id": "8bitnews-us", "scenes": []}))
    (tmp_path / "story_brief.json").write_text("{}")
    pick = {"template": "nes-desk", "anchor": "m1", "music_track": None, "music_key": None, "reasons": {}}
    if picked:
        pick["cover_layout"] = picked
    runs = []

    class Hub:
        def __init__(self, *args):
            pass

        def recent(self, channel_id):
            return {"releases": []}

    def run_studio(argv, inputs, outputs, env_names=None):
        """The two containers, without Docker: studio pick, then studio build and validate."""
        runs.append(argv)
        if "/work/pick.json" in outputs:
            outputs["/work/pick.json"].write_text(json.dumps(pick))
        else:
            outputs["/work/out"].mkdir(parents=True)
            (outputs["/work/out"] / "package.json").write_text(json.dumps(
                {"duration_s": 60.0, "files": {}, "scenes": [], "validate": {"passed": True, "checks": []}}))
        return 0

    monkeypatch.setattr(video_build, "Hub", Hub)
    monkeypatch.setattr(video_build, "build_image", lambda work: None)
    monkeypatch.setattr(video_build, "run_studio", run_studio)
    out, work = tmp_path / "out", tmp_path / "work"
    out.mkdir()
    work.mkdir()
    args = argparse.Namespace(script_artifact=None, script=str(tmp_path / "script.json"), story_brief_artifact=None,
                              story_brief=str(tmp_path / "story_brief.json"), development_id=None,
                              upload_only=False, no_upload=True)
    steps = {}
    keys = {"HUB_URL": "http://hub.invalid", "HUB_API_KEY": "k", "STUDIO_OPENROUTER_API_KEY": "k"}
    code = video_build.build(args, {}, keys, out, work, lambda name, **info: steps.update({name: info}), {})

    assert code == video_build.EXIT_OK
    build_cmd = runs[1][0]
    assert "build" in build_cmd and build_cmd[build_cmd.index("--cover-layout") + 1] == passed
    assert steps["pick"]["cover_layout"] == passed
