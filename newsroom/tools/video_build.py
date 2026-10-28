#!/usr/bin/env python3
"""Собирает ролик по сценарию и сдаёт его в хранилище — вся работа команды Video одной командой:

    python3 newsroom/tools/video_build.py --script-artifact <id> --story-brief-artifact <id> --out /tmp/video

1. Сценарий и `story_brief` — из артефактов PolyMaths (`pm_artifact.py`), или файлами (`--script`, `--story-brief`).
2. Последние ролики канала — из hub (`releases_recent`): по ним студия выбирает шаблон, ведущего и трек так, чтобы
   соседние ролики не повторялись (`studio pick`); `--exclude anchor=m1` и т. п. убирает варианты, на которые были
   замечания.
3. Трек — из хранилища через hub (`media_download_url`).
4. Студия собирается из `studio/` (Docker) и строит ролик: голос ведущего, картинки сцен от генератора с проверкой
   кадра (`--images openrouter`), музыка, субтитры, обложка, ключевые кадры; затем `studio validate`.
5. Файлы пакета уходят в хранилище через hub (`media_upload_url`); в stdout — `video_package` (`package.json` студии,
   ключи файлов в `media`, выбор в `pick`), его агент сдаёт результатом; копия — `<out>/video_package.json`.

Код выхода — итог, он же в `<out>/video_build.json` (шаг `done`) и в `<out>/exit_code`:
- 0 — ролик собран, проверки студии прошли, файлы в хранилище;
- 3 — ролик собран и загружен, но часть проверок студии не прошла (`failed_checks`);
- 4 — сценарий в таком виде не собирается (текст не влезает в плашку или обложку, сценарий не по контракту):
  это работа Script, пакета нет, причина — `script_problem`;
- 1 — сбой (hub, хранилище, Docker, генератор): пакета нет, причина — `error`; повтор с `--upload-only` догружает
  уже собранный пакет, если сбой был на загрузке;
- 2 — неверные аргументы.

Docker в песочнице — отдельный сайдкар, поэтому файлы туда и обратно едут через `docker cp`, а не монтированием;
контейнеры помечены меткой, и новый запуск убирает оставшиеся от прерванного. Ключи: `HUB_URL`, `HUB_API_KEY`,
`STUDIO_OPENROUTER_API_KEY` — `hublib.load_keys` (env или файлы skills проекта `hub-access` и `studio-video`);
ключ генератора уходит в контейнер через окружение, а не в аргументах команды. `PM_READ_KEY` — как у `pm_artifact.py`.
"""
from __future__ import annotations

import argparse
import json
import os
import pathlib
import re
import shlex
import shutil
import subprocess
import sys
import tarfile
import tempfile
import time
import traceback

ROOT = pathlib.Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT / "newsroom" / "tools"))

from hublib import Hub, artifact_with_id, load_keys  # noqa: E402

IMAGE = "8bitnews-studio:video"
LABEL = "8bitnews.video_build=1"
# What the studio reads from the repository besides its own folder (STUDIO_REPO_ROOT).
REPO_PARTS = ("channels", "countries", "newsroom/contracts", "music/catalog.yaml")
# Package files that go to the store one by one: what QA, Legal and the publisher open.
UPLOAD = ("video", "video_no_music", "subtitles", "cover", "timeline", "script")
# `studio build` errors that only a different script fixes (BuildError texts, studio/studio/build.py, cover_title.py).
SCRIPT_PROBLEMS = ("does not fit its plate", "cover text", "failed script.schema.json validation",
                   "does not match any scene id", "is not valid JSON")
EXIT_OK, EXIT_ERROR, EXIT_CHECKS, EXIT_SCRIPT = 0, 1, 3, 4


class Failure(Exception):
    def __init__(self, code: int, message: str):
        super().__init__(message)
        self.code = code


def log(msg: str) -> None:
    print(f"[video_build] {msg}", file=sys.stderr, flush=True)


def docker(*args: str, check: bool = True, capture: bool = False, env: dict | None = None) -> subprocess.CompletedProcess:
    """`docker …`; whatever it prints goes to stderr unless captured: stdout is the video_package alone. A failure
    names only the subcommand, never the arguments (they could carry a path or a value from the script)."""
    proc = subprocess.run(["docker", *args], text=True, stdout=subprocess.PIPE if capture else sys.stderr,
                          stderr=subprocess.PIPE if capture else None, env=env)
    if check and proc.returncode != 0:
        detail = (proc.stderr or "").strip().splitlines()[-1:] if capture else []
        raise Failure(EXIT_ERROR, f"docker {args[0]} failed ({proc.returncode}){': ' + detail[0] if detail else ''}")
    return proc


def clean_orphans() -> None:
    """Containers a killed earlier run left behind (the client died, the sidecar kept them running)."""
    ids = docker("ps", "-aq", "--filter", f"label={LABEL}", capture=True, check=False).stdout.split()
    if ids:
        log(f"removing {len(ids)} container(s) of an interrupted earlier run")
        docker("rm", "-f", *ids, capture=True, check=False)


def build_image(work: pathlib.Path) -> None:
    """The studio image (studio/Dockerfile: Kokoro and its voices, sha256-checked) plus the repository files it
    reads, all inside the image: the Docker sidecar can't see the sandbox's files."""
    ctx = work / "image"
    shutil.copytree(ROOT / "studio", ctx / "studio",
                    ignore=shutil.ignore_patterns("__pycache__", ".pytest_cache", "tests", "out"))
    for part in REPO_PARTS:
        src, dst = ROOT / part, ctx / "repo" / part
        dst.parent.mkdir(parents=True, exist_ok=True)
        (shutil.copytree if src.is_dir() else shutil.copy2)(src, dst)
    log("building the studio image (the first build downloads the voice model, about 5 minutes)")
    docker("build", "-q", "-t", "8bitnews-studio:base", str(ctx / "studio"), capture=True)
    (ctx / "Dockerfile").write_text("FROM 8bitnews-studio:base\nCOPY repo /repo\nENV STUDIO_REPO_ROOT=/repo\n")
    docker("build", "-q", "-t", IMAGE, str(ctx), capture=True)


def run_studio(argv: list[list[str]], inputs: dict[str, pathlib.Path], outputs: dict[str, pathlib.Path],
               env_names: dict[str, str] | None = None) -> int:
    """Runs the studio commands `argv` (each a list of arguments, joined by `;` with the first one's exit code
    kept) in a fresh container: `inputs` copied in first (container path -> local path), `outputs` copied out
    after, whatever the exit code. `env_names` reach the container through the environment of `docker create`
    (`-e NAME` without a value), so no secret is ever in a command line."""
    script = "; ".join(" ".join(shlex.quote(a) for a in cmd) for cmd in argv[:1])
    script += "; code=$?" + "".join("; " + " ".join(shlex.quote(a) for a in cmd) for cmd in argv[1:]) + "; exit $code"
    env = dict(os.environ, **(env_names or {}))
    env_args = [a for name in (env_names or {}) for a in ("-e", name)]
    cid = docker("create", "--label", LABEL, *env_args, "--entrypoint", "sh", IMAGE, "-c", script,
                 capture=True, env=env).stdout.strip()
    staged = pathlib.Path(tempfile.mkdtemp(prefix="video_build-in-"))
    try:
        for target, src in inputs.items():
            dst = staged / pathlib.PurePosixPath(target).relative_to("/")
            dst.parent.mkdir(parents=True, exist_ok=True)
            (shutil.copytree if src.is_dir() else shutil.copy2)(src, dst)
        if inputs:
            docker("cp", f"{staged}/.", f"{cid}:/", capture=True)
        code = docker("start", "-a", cid, check=False).returncode
        for target, dst in outputs.items():
            docker("cp", f"{cid}:{target}", str(dst), check=False, capture=True)
        return code
    finally:
        docker("rm", "-f", cid, check=False, capture=True)
        shutil.rmtree(staged, ignore_errors=True)


def retry(what: str, fn, attempts: int = 4):
    for i in range(attempts):
        try:
            return fn()
        except SystemExit as e:  # hublib turns HTTP errors into SystemExit
            err = e
        except OSError as e:  # URLError, timeouts, connection resets
            err = e
        if i < attempts - 1:
            log(f"{what}: {str(err)[:200]}; again in {5 * (i + 1)} s")
            time.sleep(5 * (i + 1))
    raise Failure(EXIT_ERROR, f"{what}: {str(err)[:300]}")


def upload_package(hub: Hub, development_id: str, out: pathlib.Path, package: dict, work: pathlib.Path) -> dict:
    pkg_dir = out / "package"
    media: dict = {}
    for name in UPLOAD:
        entry = package["files"].get(name)
        if entry:
            path = pkg_dir / entry["path"]
            media[name] = retry(f"upload {name}", lambda p=path: hub.upload(development_id, p, p.name))
    media["keyframes"] = {}
    for scene in package["scenes"]:
        for kf in scene["keyframes"]:
            path = pkg_dir / kf["path"]
            media["keyframes"][path.stem] = retry(f"upload {path.stem}", lambda p=path: hub.upload(development_id, p, p.name))
    archive = work / "package.tar.gz"
    with tarfile.open(archive, "w:gz") as tar:
        tar.add(pkg_dir, arcname="package")
    media["package_archive"] = retry("upload package.tar.gz", lambda: hub.upload(development_id, archive, "package.tar.gz"))
    return media


def parse_excludes(values: list[str]) -> dict:
    out: dict[str, list[str]] = {}
    for v in values:
        kind, _, value = v.partition("=")
        if kind not in ("anchor", "template", "music_track") or not value:
            raise SystemExit(f"--exclude {v!r}: ожидается anchor=<id>, template=<id> или music_track=<id>")
        out.setdefault(kind, []).append(value)
    return out


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__.split("\n\n")[0])
    src = ap.add_mutually_exclusive_group(required=True)
    src.add_argument("--script-artifact")
    src.add_argument("--script")
    brief = ap.add_mutually_exclusive_group(required=True)
    brief.add_argument("--story-brief-artifact")
    brief.add_argument("--story-brief")
    ap.add_argument("--development-id", help="hub development id (default: story_brief.development.hub_development_id)")
    ap.add_argument("--out", default="/tmp/video")
    ap.add_argument("--exclude", action="append", default=[], metavar="KIND=ID",
                    help="leave a choice out of studio pick: anchor=<id>, template=<id>, music_track=<id> (repeatable)")
    ap.add_argument("--upload-only", action="store_true",
                    help="upload the package an earlier run already built in --out (after a failed upload)")
    ap.add_argument("--no-upload", action="store_true", help="build and check only (testing)")
    args = ap.parse_args()
    excludes = parse_excludes(args.exclude)

    out = pathlib.Path(args.out)
    if not args.upload_only:
        shutil.rmtree(out, ignore_errors=True)
    out.mkdir(parents=True, exist_ok=True)
    work = pathlib.Path(tempfile.mkdtemp(prefix="video_build-"))
    report: dict = {"ok": False, "steps": []}

    def step(name: str, **info) -> None:
        report["steps"].append({"step": name, **info})
        (out / "video_build.json").write_text(json.dumps(report, indent=2, ensure_ascii=False))
        log(f"{name}: {json.dumps(info, ensure_ascii=False)[:300]}")

    keys = {}
    try:
        keys = load_keys("HUB_URL", "HUB_API_KEY", "STUDIO_OPENROUTER_API_KEY")
        code = build(args, excludes, keys, out, work, step, report)
    except Failure as e:
        code = e.code
        report["error" if code == EXIT_ERROR else "script_problem"] = redact(str(e), keys)
    except SystemExit as e:  # load_keys, hub HTTP errors, artifact fetch
        code = EXIT_ERROR
        report["error"] = redact(str(e.code), keys)
    except Exception as e:  # noqa: BLE001 - report anything, the agent reads video_build.json
        code = EXIT_ERROR
        report["error"] = redact(f"{type(e).__name__}: {e}", keys)
        traceback.print_exc(file=sys.stderr)
    finally:
        shutil.rmtree(work, ignore_errors=True)
    report["ok"] = code == EXIT_OK
    step("done", exit_code=code, **{k: report[k] for k in ("error", "script_problem") if k in report})
    (out / "exit_code").write_text(f"{code}\n")
    return code


def redact(text: str, keys: dict) -> str:
    for value in keys.values():
        if value:
            text = text.replace(value, "***")
    return re.sub(r"(sk-or-[A-Za-z0-9-]{6})[A-Za-z0-9-]+", r"\1***", text)


def build(args, excludes: dict, keys: dict, out: pathlib.Path, work: pathlib.Path, step, report: dict) -> int:
    hub = Hub(keys["HUB_URL"], keys["HUB_API_KEY"])
    if args.script_artifact:
        script, script_id = artifact_with_id(args.script_artifact, "script")
    else:
        script, script_id = json.loads(pathlib.Path(args.script).read_text()), str(args.script)
    story_brief = (artifact_with_id(args.story_brief_artifact, "story_brief")[0] if args.story_brief_artifact
                   else json.loads(pathlib.Path(args.story_brief).read_text()))
    development_id = args.development_id or (story_brief.get("development") or {}).get("hub_development_id")
    if not development_id and not args.no_upload:
        raise Failure(EXIT_ERROR, "нет hub development id: его пишет editor в story_brief.development.hub_development_id "
                                  "(или --development-id)")
    channel_id = str(script.get("channel_id", ""))
    step("inputs", channel=channel_id, development_id=development_id, script=script_id,
         scenes=len(script.get("scenes", [])), excludes=excludes)

    if args.upload_only:
        package_path = out / "package" / "package.json"
        if not package_path.is_file():
            raise Failure(EXIT_ERROR, f"--upload-only: нет собранного пакета в {package_path}")
        package = json.loads(package_path.read_text())
        pick = json.loads((out / "pick.json").read_text())
    else:
        clean_orphans()
        recent = retry("hub releases_recent", lambda: hub.recent(channel_id))
        (work / "script.json").write_text(json.dumps(script, ensure_ascii=False, indent=2))
        (work / "recent.json").write_text(json.dumps(recent))
        (work / "exclude.json").write_text(json.dumps(excludes))
        step("recent", videos=len(recent.get("releases", [])))

        build_image(work)
        step("image", image=IMAGE)

        code = run_studio([["sh", "-c", "python -m studio pick --script /work/script.json --channel \"$0\" "
                                        "--recent /work/recent.json --exclude /work/exclude.json > /work/pick.json "
                                        "2> /work/pick.err", channel_id]],
                          {f"/work/{n}": work / n for n in ("script.json", "recent.json", "exclude.json")},
                          {"/work/pick.json": work / "pick.json", "/work/pick.err": work / "pick.err"})
        if code != 0:
            err = (work / "pick.err").read_text().strip() if (work / "pick.err").is_file() else ""
            raise Failure(EXIT_ERROR, f"studio pick failed: {err[-500:]}")
        pick = json.loads((work / "pick.json").read_text())
        shutil.copy2(work / "pick.json", out / "pick.json")
        step("pick", **{k: pick[k] for k in ("template", "anchor", "music_track")}, reasons=pick["reasons"])

        inputs = {"/work/script.json": work / "script.json"}
        build_cmd = ["python", "-m", "studio", "build", "--script", "/work/script.json", "--channel", channel_id,
                     "--template", pick["template"], "--anchor", pick["anchor"], "--images", "openrouter", "--out", "/work/out"]
        if pick["music_track"]:
            key = pick["music_key"]
            retry("download the track", lambda: hub.download(key, work / "music" / key))
            inputs["/music"] = work / "music"
            build_cmd += ["--music-track", pick["music_track"]]
            step("music", track=pick["music_track"], key=key)

        code = run_studio([["sh", "-c", '"$@" 2> /work/build.err; c=$?; cat /work/build.err >&2; exit $c', "build", *build_cmd],
                           ["sh", "-c", "python -m studio validate --package /work/out/package.json > /work/validate.txt 2>&1"]],
                          inputs, {"/work/out": out / "package", "/work/validate.txt": out / "validate.txt",
                                   "/work/build.err": out / "build.err"},
                          env_names={"STUDIO_OPENROUTER_API_KEY": keys["STUDIO_OPENROUTER_API_KEY"], "STUDIO_MUSIC_DIR": "/music"})
        package_path = out / "package" / "package.json"
        if not package_path.is_file():
            err = (out / "build.err").read_text() if (out / "build.err").is_file() else ""
            reason = next((line for line in reversed(err.splitlines()) if "build failed:" in line), err.strip()[-500:])
            tail = err[err.find("build failed:"):][:1500] if "build failed:" in err else reason
            if any(p in err for p in SCRIPT_PROBLEMS):
                raise Failure(EXIT_SCRIPT, redact(tail, keys))
            raise Failure(EXIT_ERROR, redact(f"studio build exited {code} without a package: {tail}", keys))
        package = json.loads(package_path.read_text())

    failed = [c for c in package["validate"]["checks"] if not c["ok"]]
    step("build", duration_s=package["duration_s"], images_cost_usd=(package.get("images") or {}).get("cost_usd"),
         failed_checks=[f"{c['id']}: {c.get('note', '')}" for c in failed], notes=package.get("notes", []))

    media = {}
    if not args.no_upload:
        media = upload_package(hub, development_id, out, package, work)
        step("upload", files=len(media["keyframes"]) + len(media) - 1)

    package["script"] = script_id  # the studio saw a copy at /work/script.json
    package["media"] = media
    package["pick"] = pick
    (out / "video_package.json").write_text(json.dumps(package, indent=2, ensure_ascii=False))
    print(json.dumps(package, ensure_ascii=False))
    return EXIT_CHECKS if failed else EXIT_OK


if __name__ == "__main__":
    sys.exit(main())
