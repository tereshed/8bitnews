#!/usr/bin/env python3
"""Собирает ролик по сценарию и сдаёт его в хранилище — вся работа команды Video одной командой:

    python3 newsroom/tools/video_build.py --script-artifact <id> --story-brief-artifact <id> --out /tmp/video

1. Сценарий и `story_brief` — из артефактов PolyMaths (`pm_artifact.py`), или файлами (`--script`, `--story-brief`).
2. Последние ролики канала — из hub (`releases_recent`): по ним студия выбирает шаблон, ведущего и трек так, чтобы
   соседние ролики не повторялись, и место заголовка на обложке — по очереди внизу и вверху (`studio pick`);
   `--exclude anchor=m1` и т. п. убирает варианты, на которые были замечания.
3. Трек — из хранилища через hub (`media_download_url`).
4. Студия собирается из `studio/` (Docker) и строит ролик: голос ведущего, картинки сцен от генератора с проверкой
   кадра (`--images openrouter`), музыка, субтитры, обложка, ключевые кадры; затем `studio validate`.
5. Файлы пакета уходят в хранилище через hub (`media_upload_url`); в stdout — `video_package` (`package.json` студии,
   ключи файлов в `media`, выбор в `pick`), его агент сдаёт результатом; копия — `<out>/video_package.json`.

Доработка — `--rework-report <id validation_report или legal_review>`: инструмент сам берёт из отчёта прошлый пакет и картинки,
которые отвергли, потому что повторно показать проверяющим отвергнутую картинку нельзя, а человек после них ролик не смотрит.
Замечание Video, которое не привязать к сцене, значит полную пересборку, если оно не про `--exclude`. Без отчёта (причина в
проверке студии) — `--reuse-package <id video_package или путь> [--redraw S3,S7,cover]`. Сборку, которая ничего не изменит,
инструмент отказывается запускать (код 2). Тот же ролик, кроме того, о чём просили:
Принятые картинки берутся из прошлого пакета (`media.package_archive`) без вызова генератора, кроме названных в `--redraw`
и тех, чей промпт изменился (`studio/studio/scene_images.py`); `pick` остаётся прошлым, кроме видов из `--exclude` и того,
что больше не годится (`studio/studio/pick.py`, заметка в `notes`). Прошлый пакет не читается — обычная полная сборка с
заметкой в `notes`, а не сбой. Без `--reuse-package` всё как обычно.

Код выхода — итог, он же в `<out>/video_build.json` (шаг `done`) и в `<out>/exit_code`:
- 0 — ролик собран, проверки студии прошли, файлы в хранилище;
- 3 — ролик собран и загружен, но часть проверок студии не прошла (`failed_checks`);
- 4 — сценарий в таком виде не собирается (текст не влезает в плашку или обложку, «AI text» в заголовке обложки,
  у `cover.frame` нет картинки, сценарий не по контракту): это работа Script, пакета нет, причина — `script_problem`,
  а весь финальный ответ агента, дословно, — в `<out>/final_message.txt`;
- 1 — сбой (hub, хранилище, Docker, генератор): пакета нет, причина — `error`; повтор с `--upload-only` догружает
  уже собранный пакет, если сбой был на загрузке;
- 2 — неверные аргументы (причина — `error`).

Docker в песочнице — отдельный сайдкар, поэтому файлы туда и обратно едут через `docker cp`, а не монтированием;
контейнеры и образы принадлежат своему run_id; каждый запуск удаляет только свои контейнеры. Таймер внутри
контейнера ограничивает работу даже при потере клиента; кеш изображений живёт в отдельном volume.
Ключи: `HUB_URL`, `HUB_API_KEY`,
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
import uuid

ROOT = pathlib.Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT / "newsroom" / "tools"))

from hublib import SCRIPT_PROBLEM_PREFIX, Hub, artifact_details, artifact_with_id, load_keys  # noqa: E402

RUN_ID = uuid.uuid4().hex
IMAGE = f"8bitnews-studio:video-{RUN_ID}"
BASE_IMAGE = f"8bitnews-studio:base-{RUN_ID}"
LABEL = "8bitnews.video_build=1"
CACHE_VOLUME = os.environ.get("STUDIO_IMAGE_CACHE_VOLUME", "8bitnews-studio-image-cache-v1")
# What the studio reads from the repository besides its own folder (STUDIO_REPO_ROOT).
REPO_PARTS = ("channels", "countries", "newsroom/contracts", "music/catalog.yaml")
# Package files that go to the store one by one: what QA, Legal and the publisher open.
UPLOAD = ("video", "video_no_music", "subtitles", "cover", "timeline", "script")
# `studio build` errors that only a different script fixes (BuildError texts, studio/studio/build.py, cover_title.py).
SCRIPT_PROBLEMS = ("does not fit its plate", "cover text", "failed script.schema.json validation",
                   "does not match any scene id", "no picture for the cover", "is not valid JSON")
EXIT_OK, EXIT_ERROR, EXIT_ARGS, EXIT_CHECKS, EXIT_SCRIPT = 0, 1, 2, 3, 4
# `--redraw` items: a scene id, a keyframe name (what QA's findings carry), or the cover.
REDRAW_ITEM = re.compile(r"(S[0-9]+)(?:_(?:first|mid))?|cover")
FINDING_SCENE = re.compile(r"(?<![A-Za-z0-9])(S[0-9]+)(?![0-9])")
FINDING_COVER = re.compile(r"\bcover\b|обложк", re.IGNORECASE)
# What a rebuild takes from an earlier package's archive; everything else in it is ignored, whatever it claims to be.
PICTURES_IN_ARCHIVE = re.compile(r"package/(package\.json|images/(?:attempts/)?[A-Za-z0-9_-]+\.png)")
MAX_PICTURES_BYTES = 512 * 1024 * 1024


class Failure(Exception):
    def __init__(self, code: int, message: str):
        super().__init__(message)
        self.code = code


def log(msg: str) -> None:
    print(f"[video_build] {msg}", file=sys.stderr, flush=True)


def docker(*args: str, check: bool = True, capture: bool = False, env: dict | None = None) -> subprocess.CompletedProcess:
    """`docker …`; whatever it prints goes to stderr unless captured: stdout is the video_package alone. A failure
    names only the subcommand, never the arguments (they could carry a path or a value from the script)."""
    try:
        proc = subprocess.run(["docker", *args], text=True, stdout=subprocess.PIPE if capture else sys.stderr,
                              stderr=subprocess.PIPE if capture else None, env=env,
                              timeout=float(os.environ.get("STUDIO_COMMAND_TIMEOUT_S", "1800")))
    except subprocess.TimeoutExpired:
        raise Failure(EXIT_ERROR, f"docker {args[0]} exceeded STUDIO_COMMAND_TIMEOUT_S") from None
    if check and proc.returncode != 0:
        detail = (proc.stderr or "").strip().splitlines()[-1:] if capture else []
        raise Failure(EXIT_ERROR, f"docker {args[0]} failed ({proc.returncode}){': ' + detail[0] if detail else ''}")
    return proc


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
    docker("build", "-q", "-t", BASE_IMAGE, str(ctx / "studio"), capture=True)
    (ctx / "Dockerfile").write_text(f"FROM {BASE_IMAGE}\nCOPY repo /repo\nENV STUDIO_REPO_ROOT=/repo\n")
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
    deadline = os.environ.get("STUDIO_COMMAND_TIMEOUT_S", "1800")
    cid = docker("create", "--label", LABEL, "--label", f"8bitnews.run_id={RUN_ID}",
                 "--mount", f"type=volume,src={CACHE_VOLUME},dst=/image-cache",
                 "-e", "STUDIO_IMAGE_CACHE=/image-cache", *env_args, "--entrypoint", "timeout", IMAGE,
                 "--signal=TERM", "--kill-after=10s", deadline, "sh", "-c", script,
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


def parse_redraw(value: str | None) -> list[str]:
    """`S3,S7_mid,cover` -> ["S3", "S7", "cover"]."""
    names = []
    for item in filter(None, (part.strip() for part in (value or "").split(","))):
        found = REDRAW_ITEM.fullmatch(item)
        if not found:
            raise Failure(EXIT_ARGS, f"--redraw {item!r}: ожидается id сцены (S3), ключевой кадр (S3_mid) или cover")
        names.append(found.group(1) or item)
    return list(dict.fromkeys(names))


class Earlier:
    """What a rebuild could get of `--reuse-package`: `package` for its pick, `pictures` (a directory of
    package.json and images/) for the studio, `notes` on what it couldn't."""

    def __init__(self, package_id: str | None = None, package: dict | None = None, notes: list[str] | None = None):
        self.package_id, self.package, self.notes = package_id, package, notes or []
        self.pictures: pathlib.Path | None = None


def unpack_pictures(archive: pathlib.Path, dest: pathlib.Path) -> None:
    """Only `PICTURES_IN_ARCHIVE`, as regular files, to names made here: the archive was uploaded by an agent."""
    total = 0
    with tarfile.open(archive, "r:gz") as tar:
        for member in tar:
            if not (member.isreg() and PICTURES_IN_ARCHIVE.fullmatch(member.name)):
                continue
            total += member.size
            if total > MAX_PICTURES_BYTES:
                raise ValueError(f"more than {MAX_PICTURES_BYTES} bytes of pictures")
            target = dest / member.name.removeprefix("package/")
            target.parent.mkdir(parents=True, exist_ok=True)
            with tar.extractfile(member) as src, open(target, "wb") as out:
                shutil.copyfileobj(src, out)


def fetch_earlier(hub: Hub, ref: str, dest: pathlib.Path, channel_id: str) -> Earlier:
    """The package `--reuse-package` names and its pictures. Whatever goes wrong costs a normal rebuild and a
    note, never the build: nothing here raises."""
    try:
        package, package_id = retry("fetch the earlier video_package", lambda: artifact_with_id(ref, "video_package"))
        if not isinstance(package, dict):
            raise ValueError("it is not a JSON object")
        if package.get("channel_id") != channel_id:
            raise ValueError(f"it belongs to channel {package.get('channel_id')!r}, not {channel_id!r}")
    except Exception as e:  # noqa: BLE001 - Failure from retry, a broken file, anything: see the docstring
        return Earlier(notes=[f"--reuse-package {ref}: the earlier package can't be read ({e}): a full rebuild, with "
                              "a fresh pick and every picture drawn"])
    earlier = Earlier(package_id=package_id, package=package)
    try:
        scenes = (package.get("images") or {}).get("scenes")
        if not scenes:
            raise ValueError("it has no drawn pictures")
        key = (package.get("media") or {}).get("package_archive")
        if not key:
            raise ValueError("it has no media.package_archive")
        archive = retry("download the earlier package", lambda: hub.download(key, dest / "package.tar.gz"))
        unpack_pictures(archive, dest / "pictures")
        archive.unlink()
        built = json.loads((dest / "pictures" / "package.json").read_text())
        if (built.get("images") or {}).get("scenes") != scenes:
            raise ValueError("the pictures in its archive are not the ones the package records")
        earlier.pictures = dest / "pictures"
    except Exception as e:  # noqa: BLE001 - as above; tarfile, gzip, zlib and PIL each have errors of their own
        earlier.notes.append(f"--reuse-package {package_id}: its pictures can't be reused ({e}): every picture is drawn")
    return earlier


def earlier_choices(package: dict) -> dict:
    """What `studio pick --keep` takes: the earlier package's `pick`, or what it says it was made with."""
    pick = package.get("pick") or {}
    music = package.get("music") or {}
    return {"template": pick.get("template") or package.get("template"),
            "anchor": pick.get("anchor") or (package.get("anchor") or {}).get("id"),
            "music_track": pick["music_track"] if "music_track" in pick else (music.get("track_id") if music.get("enabled") else None),
            "cover_layout": pick.get("cover_layout") or (package.get("cover") or {}).get("layout"),
            "package_archive": (package.get("media") or {}).get("package_archive")}


def redraw_from_report(report: dict) -> tuple[list[str], list[str]]:
    """(the pictures a QA or Legal report rejects for Video, the ids of its Video findings no picture can be read from)."""
    names, unbound = [], []
    for issue in report.get("issues") or []:
        if issue.get("fix_owner") != "video" or issue.get("resolved_by_exclusion") is True \
                or issue.get("severity") not in ("blocker", "major", "high"):
            continue
        text = " ".join(str(issue.get(field) or "") for field in ("scene_id", "keyframe", "where"))
        found = FINDING_SCENE.findall(text) + (["cover"] if FINDING_COVER.search(text) else [])
        if found:
            names += found
        else:
            unbound.append(str(issue.get("id")))
    return list(dict.fromkeys(names)), unbound


def merged_excludes(excludes: dict, earlier_pick: dict) -> dict:
    """This rebuild's excludes and those of the build it continues: what a reviewer rejected must not come back."""
    merged = {kind: list(values) for kind, values in (earlier_pick.get("excluded") or {}).items()}
    for kind, values in excludes.items():
        merged[kind] = list(dict.fromkeys(merged.get(kind, []) + list(values)))
    return merged


def check_redraw_names(redraw: list[str], script: dict, earlier: Earlier) -> None:
    """A wrong name in `--redraw` would keep the very picture it was meant to replace, and QA pays again to find
    that out. A name may be from the earlier package: that is what the findings it answers were about. Without
    the earlier package every picture is drawn anyway."""
    if earlier.package is None:
        return
    known = {"cover"} | {s.get("id") for s in script.get("scenes", [])} | {s.get("id") for s in earlier.package.get("scenes", [])}
    unknown = [name for name in redraw if name not in known]
    if unknown:
        raise Failure(EXIT_ARGS, f"--redraw {', '.join(unknown)}: таких сцен нет ни в сценарии, ни в прошлом пакете")


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
    ap.add_argument("--reuse-package", metavar="ID_OR_PATH",
                    help="rebuild: keep this earlier video_package's pick and accepted pictures")
    ap.add_argument("--rework-report", metavar="ID",
                    help="rebuild after this validation_report or legal_review: finds the earlier package and the rejected pictures")
    ap.add_argument("--redraw", metavar="S3,S7,cover",
                    help="with --reuse-package: the pictures QA or Legal rejected, to draw again")
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
    report: dict = {"ok": False, "run_id": RUN_ID, "steps": []}

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
        report["script_problem" if code == EXIT_SCRIPT else "error"] = redact(str(e), keys)
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
    if code == EXIT_SCRIPT:  # before exit_code: the agent reads it as soon as it sees the build has ended
        (out / "final_message.txt").write_text(final_message(report["script_problem"]) + "\n")
    (out / "exit_code").write_text(f"{code}\n")
    return code


def final_message(script_problem: str) -> str:
    """The agent's whole final answer on exit 4, one line: Newsroom sends the script back to Script only if it starts
    with exactly this prefix, and the agent's own wording has missed it before."""
    return f"{SCRIPT_PROBLEM_PREFIX} {' '.join(script_problem.split())}"


def redact(text: str, keys: dict) -> str:
    for value in keys.values():
        if value:
            text = text.replace(value, "***")
    return re.sub(r"(sk-or-[A-Za-z0-9-]{6})[A-Za-z0-9-]+", r"\1***", text)


def build(args, excludes: dict, keys: dict, out: pathlib.Path, work: pathlib.Path, step, report: dict) -> int:
    reuse_package = getattr(args, "reuse_package", None)
    redraw = parse_redraw(getattr(args, "redraw", None))
    rework_notes = []
    rework_report = getattr(args, "rework_report", None)
    if rework_report:
        if reuse_package:
            raise Failure(EXIT_ARGS, "--rework-report сам находит прошлый пакет: --reuse-package с ним не нужен")
        try:
            details = artifact_details(rework_report)
        except (Exception, SystemExit) as e:  # noqa: BLE001 - reading it blind would reuse what was rejected
            raise Failure(EXIT_ERROR, f"--rework-report {rework_report}: отчёт не читается ({e})") from e
        if details.get("kind") not in (None, "validation_report", "legal_review"):
            raise Failure(EXIT_ARGS, f"--rework-report {rework_report}: это {details.get('kind')}, нужен validation_report или legal_review")
        found, unbound = redraw_from_report(details.get("content") or {})
        reuse_package = (details.get("content") or {}).get("video_package")
        redraw = list(dict.fromkeys(redraw + found))
        if unbound and not excludes:
            rework_notes.append(f"findings {', '.join(unbound)} name no picture: a full rebuild")
            reuse_package = None
        if not reuse_package:
            redraw = []
    if redraw and not reuse_package:
        raise Failure(EXIT_ARGS, "--redraw нужен вместе с --reuse-package: без него рисуются все картинки")
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
        earlier = Earlier()
        if reuse_package:
            earlier = fetch_earlier(hub, reuse_package, work / "earlier", channel_id)
            earlier.notes = [redact(note, keys) for note in earlier.notes]
            check_redraw_names(redraw, script, earlier)
            if earlier.package is not None:
                earlier_images = earlier.package.get("images") or {}
                # a card is never reused: it is drawn again, and asked for, so a second card sends the visual to Script
                redraw = list(dict.fromkeys(redraw + [s.get("id") for s in earlier_images.get("scenes") or [] if s.get("fallback")]))
                validation = earlier.package.get("validate") or {}
                if not redraw and not excludes and earlier.package.get("script") == script_id and validation.get("passed") is True:
                    raise Failure(EXIT_ARGS, "пересборка ничего не изменит: сценарий тот же, замечаний к картинкам нет, студия прошла. "
                                             "Нужен отчёт с замечаниями для Video (--rework-report) или --redraw/--exclude")
                excludes = merged_excludes(excludes, earlier.package.get("pick") or {})
            step("reuse", package=earlier.package_id, pictures=earlier.pictures is not None, redraw=redraw,
                 notes=earlier.notes)
        recent = retry("hub releases_recent", lambda: hub.recent(channel_id))
        (work / "script.json").write_text(json.dumps(script, ensure_ascii=False, indent=2))
        (work / "recent.json").write_text(json.dumps(recent))
        (work / "exclude.json").write_text(json.dumps(excludes))
        step("recent", videos=len(recent.get("releases", [])))

        build_image(work)
        step("image", image=IMAGE)

        pick_inputs = ["script.json", "recent.json", "exclude.json"]
        keep = ""
        if earlier.package is not None:
            (work / "keep.json").write_text(json.dumps(earlier_choices(earlier.package)))
            pick_inputs.append("keep.json")
            keep = "--keep /work/keep.json "
        code = run_studio([["sh", "-c", "python -m studio pick --script /work/script.json --channel \"$0\" "
                                        f"--recent /work/recent.json --exclude /work/exclude.json {keep}> /work/pick.json "
                                        "2> /work/pick.err", channel_id]],
                          {f"/work/{n}": work / n for n in pick_inputs},
                          {"/work/pick.json": work / "pick.json", "/work/pick.err": work / "pick.err"})
        if code != 0:
            err = (work / "pick.err").read_text().strip() if (work / "pick.err").is_file() else ""
            raise Failure(EXIT_ERROR, f"studio pick failed: {err[-500:]}")
        pick = json.loads((work / "pick.json").read_text())
        shutil.copy2(work / "pick.json", out / "pick.json")
        cover_layout = pick.get("cover_layout") or "bottom"  # a pick.json without the field (older studio): bottom
        step("pick", **{k: pick[k] for k in ("template", "anchor", "music_track")}, cover_layout=cover_layout,
             reasons=pick["reasons"], **({"notes": pick["notes"]} if pick.get("notes") else {}))

        inputs = {"/work/script.json": work / "script.json"}
        build_cmd = ["python", "-m", "studio", "build", "--script", "/work/script.json", "--channel", channel_id,
                     "--template", pick["template"], "--anchor", pick["anchor"], "--cover-layout", cover_layout,
                     "--images", "openrouter", "--out", "/work/out"]
        if redraw:
            build_cmd += ["--redraw", ",".join(redraw)]
        if earlier.pictures is not None:
            inputs["/work/reuse"] = earlier.pictures
            build_cmd += ["--reuse-from", "/work/reuse", "--reuse-package-id", earlier.package_id]
        for note in rework_notes + earlier.notes + pick.get("notes", []):
            build_cmd += ["--note", note]
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
                          env_names={"STUDIO_OPENROUTER_API_KEY": keys["STUDIO_OPENROUTER_API_KEY"], "STUDIO_MUSIC_DIR": "/music",
                                     "STUDIO_IMAGE_BUDGET_USD": os.environ.get("STUDIO_IMAGE_BUDGET_USD", "2")})
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
    package["pick"] = {**pick, "excluded": excludes} if excludes else pick
    (out / "video_package.json").write_text(json.dumps(package, indent=2, ensure_ascii=False))
    print(json.dumps(package, ensure_ascii=False))
    return EXIT_CHECKS if failed else EXIT_OK


if __name__ == "__main__":
    sys.exit(main())
