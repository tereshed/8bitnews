"""newsroom/tools/video_build.py: studio pick's cover layout reaches studio build."""
import argparse
import io
import json
import sys
import tarfile
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


# --- a rebuild from an earlier package --------------------------------------------

PNG = b"\x89PNG picture bytes"
ARCHIVE_KEY = "releases/x/package.tar.gz"


def _earlier_package(**over):
    scenes = [{"id": "S2", "file": "images/S2.png", "fallback": False, "accepted": "images/attempts/S2-0.png",
               "attempts": [{"n": 0, "prompt": "p", "file": "images/attempts/S2-0.png", "found": []}]}]
    return {"channel_id": "8bitnews-us", "template": "nes-desk", "anchor": {"id": "m1", "chosen_by": "argument"},
            "music": {"enabled": False}, "cover": {"frame": "S2", "layout": "top", "picture": "S2"},
            "scenes": [{"id": "S1"}, {"id": "S2"}],
            "images": {"provider": "openrouter", "model": "m", "check_model": "c", "cost_usd": 0.1, "scenes": scenes},
            "media": {"package_archive": ARCHIVE_KEY},
            "pick": {"template": "nes-desk", "anchor": "m1", "music_track": None, "music_key": None, "cover_layout": "top"},
            **over}


def _archive(package, extra=()) -> bytes:
    """The package as video_build uploads it: everything under package/."""
    built = {k: v for k, v in package.items() if k not in ("media", "pick")}
    buf = io.BytesIO()
    with tarfile.open(fileobj=buf, mode="w:gz") as tar:
        def add(name, data):
            info = tarfile.TarInfo(name)
            info.size = len(data)
            tar.addfile(info, io.BytesIO(data))
        add("package/package.json", json.dumps(built).encode())
        add("package/images/S2.png", PNG)
        add("package/images/attempts/S2-0.png", PNG)
        add("package/video.mp4", b"not wanted")
        for member, data in extra:
            if data is None:
                tar.addfile(member)
            else:
                member.size = len(data)
                tar.addfile(member, io.BytesIO(data))
    return buf.getvalue()


class FakeHub:
    def __init__(self, archive=None, *args):
        self.archive = archive
        self.downloads = []

    def recent(self, channel_id):
        return {"releases": []}

    def download(self, key, dest):
        self.downloads.append(key)
        if isinstance(self.archive, BaseException):
            raise self.archive
        dest = Path(dest)
        dest.parent.mkdir(parents=True, exist_ok=True)
        dest.write_bytes(self.archive)
        return dest


@pytest.fixture
def no_waiting(monkeypatch):
    monkeypatch.setattr(video_build.time, "sleep", lambda s: None)


def _package_file(tmp_path, package=None):
    path = tmp_path / "vp.json"
    path.write_text(json.dumps(_earlier_package() if package is None else package))
    return str(path)


def test_fetch_earlier_unpacks_the_pictures_and_nothing_else(tmp_path, no_waiting):
    package = _earlier_package()
    hub = FakeHub(_archive(package))
    earlier = video_build.fetch_earlier(hub, _package_file(tmp_path, package), tmp_path / "e", "8bitnews-us")
    assert hub.downloads == [ARCHIVE_KEY] and earlier.notes == []
    assert (earlier.pictures / "images/S2.png").read_bytes() == PNG
    assert (earlier.pictures / "images/attempts/S2-0.png").read_bytes() == PNG
    assert (earlier.pictures / "package.json").is_file() and not (earlier.pictures / "video.mp4").exists()


def test_a_hostile_archive_writes_nothing_outside_the_pictures(tmp_path, no_waiting):
    link = tarfile.TarInfo("package/images/S9.png")
    link.type, link.linkname = tarfile.SYMTYPE, "/etc/passwd"
    extra = [(tarfile.TarInfo("../../evil.png"), b"x"), (tarfile.TarInfo("package/images/../../../evil.png"), b"x"),
             (tarfile.TarInfo("/tmp/evil.png"), b"x"), (link, None)]
    package = _earlier_package()
    earlier = video_build.fetch_earlier(FakeHub(_archive(package, extra)), _package_file(tmp_path, package), tmp_path / "e", "8bitnews-us")
    assert earlier.pictures is not None
    assert not (earlier.pictures / "images/S9.png").exists()
    assert not list(tmp_path.rglob("evil.png"))


@pytest.mark.parametrize("make_hub,package,why", [
    (lambda p: FakeHub(OSError("no such key")), None, "no such key"),
    (lambda p: FakeHub(SystemExit("hub POST: HTTP 404")), None, "HTTP 404"),
    (lambda p: FakeHub(b"this is not a gzip archive"), None, "can't be reused"),
    (lambda p: FakeHub(_archive(p)[:200]), None, "can't be reused"),
    (lambda p: FakeHub(_archive(dict(p, images={"scenes": [{"id": "S7"}]}))), None, "not the ones the package records"),
    (lambda p: FakeHub(b""), {"media": {}}, "no media.package_archive"),
    (lambda p: FakeHub(b""), {"media": None}, "no media.package_archive"),
    (lambda p: FakeHub(b""), {"images": None}, "no drawn pictures"),
])
def test_pictures_that_cant_be_had_cost_a_note_but_the_pick_stays(tmp_path, no_waiting, make_hub, package, why):
    earlier_package = _earlier_package(**(package or {}))
    earlier = video_build.fetch_earlier(make_hub(earlier_package), _package_file(tmp_path, earlier_package), tmp_path / "e",
                                        "8bitnews-us")
    assert earlier.pictures is None and earlier.package == earlier_package
    assert why in earlier.notes[0] and "every picture is drawn" in earlier.notes[0]


@pytest.mark.parametrize("content", ["not json", "[1]", json.dumps(_earlier_package(channel_id="8bitnews-es"))])
def test_a_package_that_cant_be_had_means_a_fresh_full_rebuild(tmp_path, no_waiting, content):
    path = tmp_path / "vp.json"
    path.write_text(content)
    earlier = video_build.fetch_earlier(FakeHub(_archive(_earlier_package())), str(path), tmp_path / "e", "8bitnews-us")
    assert earlier.package is None and earlier.pictures is None and "a full rebuild" in earlier.notes[0]


def test_a_package_id_that_cant_be_fetched_means_a_fresh_full_rebuild(tmp_path, no_waiting, monkeypatch):
    def refuse(ref, kind):
        raise SystemExit("artifact is not a video_package")
    monkeypatch.setattr(video_build, "artifact_with_id", refuse)
    earlier = video_build.fetch_earlier(FakeHub(b""), "some-id", tmp_path / "e", "8bitnews-us")
    assert earlier.package is None and "not a video_package" in earlier.notes[0]


def test_earlier_choices_come_from_the_pick_or_from_what_was_made():
    assert video_build.earlier_choices(_earlier_package()) == {
        "template": "nes-desk", "anchor": "m1", "music_track": None, "cover_layout": "top", "package_archive": ARCHIVE_KEY}
    older = _earlier_package(pick=None, music={"enabled": True, "track_id": "t1"})
    assert video_build.earlier_choices(older)["anchor"] == "m1" and video_build.earlier_choices(older)["music_track"] == "t1"
    assert video_build.earlier_choices(older)["cover_layout"] == "top"


@pytest.mark.parametrize("value,names", [
    (None, []), ("", []), ("S3", ["S3"]), ("S3,S7,cover", ["S3", "S7", "cover"]), (" S3 , S3_mid,S7_first ", ["S3", "S7"]),
])
def test_parse_redraw(value, names):
    assert video_build.parse_redraw(value) == names


@pytest.mark.parametrize("value", ["s3", "S3_last", "scene3", "S", "cover2", "S3;rm"])
def test_parse_redraw_refuses_what_it_cant_tie_to_a_picture(value):
    with pytest.raises(video_build.Failure) as e:
        video_build.parse_redraw(value)
    assert e.value.code == video_build.EXIT_ARGS


def _rebuild_run(tmp_path, monkeypatch, *, reuse=None, redraw=None, hub=None, built=None, pick_extra=None, rework=None, excludes=None):
    """video_build.build without Docker. Returns (exit code, the steps, the argv of each studio run, their inputs)."""
    (tmp_path / "script.json").write_text(json.dumps(
        {"channel_id": "8bitnews-us", "scenes": [{"id": "S1"}, {"id": "S2"}, {"id": "S3"}]}))
    (tmp_path / "story_brief.json").write_text("{}")
    pick = {"template": "nes-desk", "anchor": "m1", "music_track": None, "music_key": None, "reasons": {},
            "cover_layout": "top", **(pick_extra or {})}
    runs, inputs_seen = [], []

    def run_studio(argv, inputs, outputs, env_names=None):
        runs.append(argv)
        inputs_seen.append(dict(inputs))
        if "/work/pick.json" in outputs:
            outputs["/work/pick.json"].write_text(json.dumps(pick))
        else:
            outputs["/work/out"].mkdir(parents=True)
            (outputs["/work/out"] / "package.json").write_text(json.dumps(built or {
                "duration_s": 60.0, "files": {}, "scenes": [], "notes": [], "validate": {"passed": True, "checks": []}}))
        return 0

    monkeypatch.setattr(video_build, "Hub", lambda *a: hub or FakeHub())
    monkeypatch.setattr(video_build, "build_image", lambda work: None)
    monkeypatch.setattr(video_build, "run_studio", run_studio)
    monkeypatch.setattr(video_build.time, "sleep", lambda s: None)
    out, work = tmp_path / "out", tmp_path / "work"
    out.mkdir()
    work.mkdir()
    args = argparse.Namespace(script_artifact=None, script=str(tmp_path / "script.json"), story_brief_artifact=None,
                              story_brief=str(tmp_path / "story_brief.json"), development_id=None, upload_only=False,
                              no_upload=True, reuse_package=reuse, redraw=redraw, rework_report=rework)
    steps = {}
    keys = {"HUB_URL": "http://hub.invalid", "HUB_API_KEY": "k", "STUDIO_OPENROUTER_API_KEY": "k"}
    code = video_build.build(args, {}, keys, out, work, lambda name, **info: steps.update({name: info}), {})
    return code, steps, runs, inputs_seen


def test_without_reuse_package_the_commands_are_todays(tmp_path, monkeypatch):
    code, _, runs, inputs = _rebuild_run(tmp_path, monkeypatch)
    assert code == video_build.EXIT_OK
    assert runs[0][0][2] == ('python -m studio pick --script /work/script.json --channel "$0" --recent /work/recent.json '
                             '--exclude /work/exclude.json > /work/pick.json 2> /work/pick.err')
    assert set(inputs[0]) == {"/work/script.json", "/work/recent.json", "/work/exclude.json"}
    build_cmd = runs[1][0]
    assert not {"--reuse-from", "--reuse-package-id", "--redraw", "--note"} & set(build_cmd)
    assert set(inputs[1]) == {"/work/script.json"}


def test_a_rebuild_hands_the_studio_the_pick_the_pictures_and_the_redraw(tmp_path, monkeypatch):
    package = _earlier_package()
    code, steps, runs, inputs = _rebuild_run(tmp_path, monkeypatch, reuse=_package_file(tmp_path, package), redraw="S2,cover",
                                             hub=FakeHub(_archive(package)), pick_extra={"notes": ["anchor changed"]})
    assert code == video_build.EXIT_OK
    assert "--keep /work/keep.json" in runs[0][0][2] and "/work/keep.json" in inputs[0]
    keep = json.loads(inputs[0]["/work/keep.json"].read_text())
    assert keep["anchor"] == "m1" and keep["package_archive"] == ARCHIVE_KEY
    build_cmd = runs[1][0]
    assert build_cmd[build_cmd.index("--redraw") + 1] == "S2,cover"
    assert build_cmd[build_cmd.index("--reuse-from") + 1] == "/work/reuse"
    assert inputs[1]["/work/reuse"].name == "pictures"
    assert build_cmd[build_cmd.index("--note") + 1] == "anchor changed"
    assert steps["reuse"]["pictures"] is True and steps["pick"]["notes"] == ["anchor changed"]
    assert build_cmd[build_cmd.index("--anchor") + 1] == "m1"


def test_a_broken_archive_rebuilds_in_full_keeping_the_pick_with_a_note(tmp_path, monkeypatch):
    package = _earlier_package()
    code, steps, runs, inputs = _rebuild_run(tmp_path, monkeypatch, reuse=_package_file(tmp_path, package), redraw="S2",
                                             hub=FakeHub(b"not a gzip archive"))
    assert code == video_build.EXIT_OK
    assert "--keep /work/keep.json" in runs[0][0][2]
    build_cmd = runs[1][0]
    assert "--reuse-from" not in build_cmd and "/work/reuse" not in inputs[1]
    assert build_cmd[build_cmd.index("--redraw") + 1] == "S2"  # a card on S2 is still Script's to fix
    assert "every picture is drawn" in build_cmd[build_cmd.index("--note") + 1]


def test_a_package_that_cant_be_read_rebuilds_as_today_with_a_note(tmp_path, monkeypatch):
    path = tmp_path / "vp.json"
    path.write_text("not json")
    code, _, runs, inputs = _rebuild_run(tmp_path, monkeypatch, reuse=str(path))
    assert code == video_build.EXIT_OK
    assert "--keep" not in runs[0][0][2] and "/work/keep.json" not in inputs[0]
    build_cmd = runs[1][0]
    assert "--reuse-from" not in build_cmd
    assert "a full rebuild" in build_cmd[build_cmd.index("--note") + 1]


def test_a_failed_picture_check_exits_3(tmp_path, monkeypatch):
    built = {"duration_s": 60.0, "files": {}, "scenes": [], "notes": [],
             "validate": {"passed": False, "checks": [{"id": "picture_fallback", "ok": False, "note": "S2 (3 attempt(s): text)"}]}}
    code, steps, _, _ = _rebuild_run(tmp_path, monkeypatch, built=built)
    assert code == video_build.EXIT_CHECKS
    assert steps["build"]["failed_checks"] == ["picture_fallback: S2 (3 attempt(s): text)"]


def test_a_redraw_name_found_nowhere_is_a_wrong_argument(tmp_path, monkeypatch):
    package = _earlier_package()
    with pytest.raises(video_build.Failure) as e:
        _rebuild_run(tmp_path, monkeypatch, reuse=_package_file(tmp_path, package), redraw="S2,S99", hub=FakeHub(_archive(package)))
    assert e.value.code == video_build.EXIT_ARGS and "S99" in str(e.value)


def test_a_redraw_name_from_the_earlier_package_is_fine(tmp_path, monkeypatch):
    package = _earlier_package(scenes=[{"id": "S1"}, {"id": "S9"}])
    code, _, runs, _ = _rebuild_run(tmp_path, monkeypatch, reuse=_package_file(tmp_path, package), redraw="S9",
                                    hub=FakeHub(_archive(package)))
    assert code == video_build.EXIT_OK and runs[1][0][runs[1][0].index("--redraw") + 1] == "S9"


def test_redraw_without_reuse_package_is_a_wrong_argument(tmp_path, monkeypatch):
    with pytest.raises(video_build.Failure) as e:
        _rebuild_run(tmp_path, monkeypatch, redraw="S2")
    assert e.value.code == video_build.EXIT_ARGS


def test_a_wrong_argument_reaches_the_agent_through_exit_code_and_error(tmp_path, monkeypatch, capsys):
    (tmp_path / "script.json").write_text(json.dumps({"channel_id": "8bitnews-us", "scenes": []}))
    (tmp_path / "story_brief.json").write_text("{}")
    out = tmp_path / "out"
    monkeypatch.setattr(sys, "argv", ["video_build", "--script", str(tmp_path / "script.json"), "--story-brief",
                                      str(tmp_path / "story_brief.json"), "--no-upload", "--out", str(out), "--redraw", "scene3"])
    monkeypatch.setattr(video_build, "load_keys", lambda *names: {n: "k" for n in names})
    assert video_build.main() == video_build.EXIT_ARGS
    assert (out / "exit_code").read_text().strip() == "2"
    report = json.loads((out / "video_build.json").read_text())
    assert "scene3" in report["error"] and "script_problem" not in report


def test_upload_only_with_reuse_package_fetches_nothing(tmp_path, monkeypatch):
    (tmp_path / "script.json").write_text(json.dumps({"channel_id": "8bitnews-us", "scenes": []}))
    (tmp_path / "story_brief.json").write_text("{}")
    out = tmp_path / "out"
    (out / "package").mkdir(parents=True)
    (out / "package" / "package.json").write_text(json.dumps(
        {"duration_s": 60.0, "files": {}, "scenes": [], "validate": {"passed": True, "checks": []}}))
    (out / "pick.json").write_text(json.dumps({"template": "nes-desk", "anchor": "m1", "music_track": None}))
    hub = FakeHub(OSError("must not be called"))
    monkeypatch.setattr(video_build, "Hub", lambda *a: hub)
    monkeypatch.setattr(video_build, "fetch_earlier", lambda *a: pytest.fail("must not fetch"))
    args = argparse.Namespace(script_artifact=None, script=str(tmp_path / "script.json"), story_brief_artifact=None,
                              story_brief=str(tmp_path / "story_brief.json"), development_id=None, upload_only=True,
                              no_upload=True, reuse_package="some-id", redraw="S2")
    keys = {"HUB_URL": "http://hub.invalid", "HUB_API_KEY": "k", "STUDIO_OPENROUTER_API_KEY": "k"}
    assert video_build.build(args, {}, keys, out, tmp_path, lambda name, **info: None, {}) == video_build.EXIT_OK


# --- exit 4: the agent's final answer is written for it ------------------------------

def _main_with_failure(tmp_path, monkeypatch, failure):
    out = tmp_path / "out"
    monkeypatch.setattr(sys, "argv", ["video_build", "--script", "s.json", "--story-brief", "b.json", "--out", str(out)])
    monkeypatch.setattr(video_build, "load_keys", lambda *names: {n: "secret-key" for n in names})

    def build(*args):
        raise failure
    monkeypatch.setattr(video_build, "build", build)
    return video_build.main(), out


def test_exit_4_writes_the_final_answer_that_sends_the_script_back_to_script(tmp_path, monkeypatch):
    problem = "build failed: scene text does not fit its plate:\n  - scene S3: on_screen_text needs 4 lines\n  - key secret-key"
    code, out = _main_with_failure(tmp_path, monkeypatch, video_build.Failure(video_build.EXIT_SCRIPT, problem))
    assert code == video_build.EXIT_SCRIPT and (out / "exit_code").read_text().strip() == "4"
    text = (out / "final_message.txt").read_text()
    assert text == ("Сценарий не собирается: build failed: scene text does not fit its plate: - scene S3: "
                    "on_screen_text needs 4 lines - key ***\n")
    assert json.loads((out / "video_build.json").read_text())["script_problem"].startswith("build failed:")
    # What stage_report makes of it, once the Video router has passed the agent's answer on as its summary.
    from stage_report import build_report

    report = build_report("delegation", {"team": "Video", "state": "done", "artifacts": [], "summary": text},
                          lambda *_: pytest.fail("there is no package to fetch"))
    assert report["verdict"] == "fail" and report["fix_owners"] == {"script": ["studio"]}
    assert report["notes"] == [text]


@pytest.mark.parametrize("code", [video_build.EXIT_ERROR, video_build.EXIT_ARGS])
def test_only_exit_4_writes_a_final_answer(tmp_path, monkeypatch, code):
    got, out = _main_with_failure(tmp_path, monkeypatch, video_build.Failure(code, "hub is down"))
    assert got == code and not (out / "final_message.txt").exists()
    assert json.loads((out / "video_build.json").read_text())["error"] == "hub is down"


def test_a_summary_without_the_prefix_is_not_taken_for_a_script_problem():
    from stage_report import build_report

    report = build_report("delegation", {"team": "Video", "state": "done", "artifacts": [], "summary": "The script does not build"},
                          lambda *_: pytest.fail("there is no package to fetch"))
    assert report["verdict"] == "missing" and not report["fix_owners"]


@pytest.mark.parametrize("issue,names,unbound", [
    ({"id": "Q1", "severity": "major", "fix_owner": "video", "scene_id": "S3"}, ["S3"], []),
    ({"id": "Q1", "severity": "major", "fix_owner": "video", "keyframe": "S7_mid"}, ["S7"], []),
    ({"id": "Q1", "severity": "major", "fix_owner": "video", "scene_id": "cover"}, ["cover"], []),
    ({"id": "P1", "severity": "high", "fix_owner": "video", "where": "S2 picture and S10 keyframe"}, ["S2", "S10"], []),
    ({"id": "P1", "severity": "high", "fix_owner": "video", "where": "обложка"}, ["cover"], []),
    ({"id": "P1", "severity": "high", "fix_owner": "video", "where": "S3-S5"}, ["S3", "S4", "S5"], []),
    ({"id": "P1", "severity": "high", "fix_owner": "video", "where": "Scene 3 and S5"}, ["S5", "S3"], []),
    ({"id": "P1", "severity": "high", "fix_owner": "video", "where": "в сцене 7"}, ["S7"], []),
    ({"id": "P1", "severity": "high", "fix_owner": "video", "where": "the opening scene"}, [], ["P1"]),
    ({"id": "Q2", "severity": "major", "fix_owner": "video", "area": "audio"}, [], ["Q2"]),
    ({"id": "Q3", "severity": "minor", "fix_owner": "video", "scene_id": "S3"}, [], []),
    ({"id": "R1", "severity": "low", "fix_owner": "video", "where": "S3"}, [], []),
    ({"id": "Q4", "severity": "major", "fix_owner": "script", "scene_id": "S3"}, [], []),
    ({"id": "P2", "severity": "high", "fix_owner": "video", "where": "S3", "resolved_by_exclusion": True}, [], []),
])
def test_rejected_pictures_are_read_from_the_report_not_from_the_agent(issue, names, unbound):
    assert video_build.redraw_from_report({"issues": [issue]}) == (names, unbound)


def test_a_rejected_anchor_stays_excluded_across_rebuilds():
    assert video_build.merged_excludes({"anchor": ["w1"]}, {"excluded": {"anchor": ["m7"], "template": ["t"]}}) == {
        "anchor": ["m7", "w1"], "template": ["t"]}
    assert video_build.merged_excludes({}, {}) == {}


def _earlier_stub(monkeypatch, script="", passed=True, scenes=(), pick=None):
    package = {"script": script, "pick": pick or {}, "validate": {"passed": passed}, "images": {"scenes": list(scenes)}}
    seen = []

    def fetch(hub, ref, dest, channel_id):
        seen.append(ref)
        return video_build.Earlier(package_id="prev", package=package)

    monkeypatch.setattr(video_build, "fetch_earlier", fetch)
    return seen


def _report_stub(monkeypatch, content, kind="validation_report"):
    monkeypatch.setattr(video_build, "artifact_details", lambda ref, *a: {"content": content, "kind": kind, "id": ref})


FINDING = {"id": "Q1", "severity": "major", "fix_owner": "video", "scene_id": "S2"}


def test_a_rework_report_finds_the_package_and_redraws_what_it_rejects(tmp_path, monkeypatch):
    seen = _earlier_stub(monkeypatch, script=str(tmp_path / "script.json"))
    _report_stub(monkeypatch, {"video_package": "prev", "issues": [FINDING]})
    code, _, runs, _ = _rebuild_run(tmp_path, monkeypatch, rework="report")
    assert code == 0 and seen == ["prev"]
    build = runs[1][0]
    assert build[build.index("--redraw") + 1] == "S2"


def test_a_finding_with_no_picture_draws_every_picture_again_and_keeps_the_excludes(tmp_path, monkeypatch):
    seen = _earlier_stub(monkeypatch, scenes=[{"id": "S1", "fallback": False}, {"id": "S2", "fallback": False}],
                         pick={"excluded": {"anchor": ["m7"]}})
    _report_stub(monkeypatch, {"video_package": "prev", "issues": [{**FINDING, "scene_id": "", "where": "00:23"}]})
    code, _, runs, _ = _rebuild_run(tmp_path, monkeypatch, rework="report")
    assert code == 0 and seen == ["prev"]
    assert runs[1][0][runs[1][0].index("--redraw") + 1] == "S1,S2,cover"


def test_a_phone_called_s24_is_no_scene(tmp_path, monkeypatch):
    _earlier_stub(monkeypatch, scenes=[{"id": "S3", "fallback": False}])
    _report_stub(monkeypatch, {"video_package": "prev", "issues": [{**FINDING, "scene_id": "S3", "where": "S3 shows a Galaxy S24"}]})
    code, _, runs, _ = _rebuild_run(tmp_path, monkeypatch, rework="report")
    assert code == 0 and runs[1][0][runs[1][0].index("--redraw") + 1] == "S3"


def test_redraw_all_expands_to_every_picture(tmp_path, monkeypatch):
    _earlier_stub(monkeypatch, scenes=[{"id": "S1", "fallback": False}])
    code, _, runs, _ = _rebuild_run(tmp_path, monkeypatch, reuse="prev", redraw="all")
    assert code == 0 and runs[1][0][runs[1][0].index("--redraw") + 1] == "S1,cover"


def test_a_review_of_the_script_has_no_package_so_the_build_is_a_first_one(tmp_path, monkeypatch):
    seen = _earlier_stub(monkeypatch)
    _report_stub(monkeypatch, {"mode": "script", "issues": [{**FINDING, "where": "S2"}]}, "legal_review")
    code, _, runs, _ = _rebuild_run(tmp_path, monkeypatch, rework="report")
    assert code == 0 and seen == [] and "--redraw" not in runs[1][0]


def test_a_report_that_cannot_be_read_stops_the_build(tmp_path, monkeypatch):
    def broken(*a):
        raise SystemExit("no such artifact")

    monkeypatch.setattr(video_build, "artifact_details", broken)
    with pytest.raises(video_build.Failure) as e:
        _rebuild_run(tmp_path, monkeypatch, rework="report")
    assert e.value.code == video_build.EXIT_ERROR


def test_another_artifact_is_not_a_report(tmp_path, monkeypatch):
    _report_stub(monkeypatch, {}, "video_package")
    with pytest.raises(video_build.Failure) as e:
        _rebuild_run(tmp_path, monkeypatch, rework="report")
    assert e.value.code == video_build.EXIT_ARGS


def test_a_rebuild_that_would_change_nothing_is_refused(tmp_path, monkeypatch):
    _earlier_stub(monkeypatch, script=str(tmp_path / "script.json"))
    with pytest.raises(video_build.Failure) as e:
        _rebuild_run(tmp_path, monkeypatch, reuse="prev")
    assert e.value.code == video_build.EXIT_ARGS and "ничего не изменит" in str(e.value)


def test_a_changed_script_or_a_failed_build_may_rebuild_with_nothing_named(tmp_path, monkeypatch):
    _earlier_stub(monkeypatch, script="another-script")
    assert _rebuild_run(tmp_path, monkeypatch, reuse="prev")[0] == 0


def test_a_rebuild_after_a_failed_check_is_not_refused(tmp_path, monkeypatch):
    _earlier_stub(monkeypatch, script=str(tmp_path / "script.json"), passed=False)
    assert _rebuild_run(tmp_path, monkeypatch, reuse="prev")[0] == 0


def test_an_earlier_card_is_asked_for_again_so_a_second_card_goes_to_script(tmp_path, monkeypatch):
    _earlier_stub(monkeypatch, script=str(tmp_path / "script.json"), passed=False,
                     scenes=[{"id": "S3", "fallback": True}, {"id": "S1", "fallback": False}])
    code, _, runs, _ = _rebuild_run(tmp_path, monkeypatch, reuse="prev")
    build = runs[1][0]
    assert code == 0 and build[build.index("--redraw") + 1] == "S3"



def test_a_voice_outside_the_channels_length_is_the_scripts_to_fix():
    assert "narration length" in video_build.SCRIPT_PROBLEMS
