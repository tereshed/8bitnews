"""The sandbox wrapper must never remove another concurrently rendering video."""
import importlib.util
import subprocess
from pathlib import Path

import pytest


def wrapper():
    path = Path(__file__).resolve().parents[2] / "newsroom/tools/video_build.py"
    spec = importlib.util.spec_from_file_location("isolated_video_wrapper", path)
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)
    return mod


def test_two_runs_have_distinct_container_ownership_and_images(tmp_path, monkeypatch):
    first, second = wrapper(), wrapper()
    assert first.RUN_ID != second.RUN_ID
    assert first.IMAGE != second.IMAGE and first.BASE_IMAGE != second.BASE_IMAGE
    calls = []

    def docker(*args, **kwargs):
        calls.append(args)
        if args[0] == "create":
            return subprocess.CompletedProcess(args, 0, stdout="owned-container\n")
        return subprocess.CompletedProcess(args, 0, stdout="")

    monkeypatch.setattr(first, "docker", docker)
    first.run_studio([["true"]], {}, {})
    create = calls[0]
    assert f"8bitnews.run_id={first.RUN_ID}" in create
    assert f"8bitnews.run_id={second.RUN_ID}" not in create
    assert any("dst=/image-cache" in arg for arg in create)
    assert [call for call in calls if call[0] == "rm"] == [("rm", "-f", "owned-container")]
    assert not any(call[0] == "ps" for call in calls)


def test_a_stalled_render_is_bounded_and_its_own_container_is_removed(monkeypatch):
    mod = wrapper()
    calls = []

    def run(argv, **kwargs):
        calls.append(argv)
        assert kwargs["timeout"] == 5.0
        if argv[1] == "create":
            return subprocess.CompletedProcess(argv, 0, stdout="owned\n")
        if argv[1] == "start":
            raise subprocess.TimeoutExpired(argv, 5)
        return subprocess.CompletedProcess(argv, 0, stdout="")

    monkeypatch.setenv("STUDIO_COMMAND_TIMEOUT_S", "5")
    monkeypatch.setattr(mod.subprocess, "run", run)
    with pytest.raises(mod.Failure, match="exceeded"):
        mod.run_studio([["true"]], {}, {})
    assert calls[-1] == ["docker", "rm", "-f", "owned"]
