"""hub из песочницы агента: REST-клиент и ключи (общая часть инструментов `newsroom/tools`).

Ключи — из env или из файлов skills проекта (PolyMaths подставляет секреты `${secret:…}` только в файлы skills
и конфиги MCP, не в файлы песочницы):
- `hub-access/hub.env` — `HUB_URL`, `HUB_API_KEY`: Video, QA, Legal, editor;
- `studio-video/video.env` — `STUDIO_OPENROUTER_API_KEY`: только Video (генератор картинок).
"""
from __future__ import annotations

import json
import os
import pathlib
import shutil
import ssl
import sys
import urllib.error
import urllib.parse
import urllib.request

try:  # python.org-сборки Python на macOS не видят системные сертификаты
    import certifi
    SSL_CONTEXT = ssl.create_default_context(cafile=certifi.where())
except ImportError:
    SSL_CONTEXT = ssl.create_default_context()

SKILL_FILES = ("skills/hub-access/hub.env", "skills/studio-video/video.env")


def load_keys(*names: str) -> dict:
    keys = {k: os.environ[k] for k in names if os.environ.get(k)}
    roots = {pathlib.Path.home(), pathlib.Path("/home/sandbox"), pathlib.Path("/root")}
    for root in roots:
        for rel in SKILL_FILES:
            for path in list(root.glob(f".*/{rel}")) + list(root.glob(rel)):
                for line in path.read_text().splitlines():
                    name, _, value = line.partition("=")
                    name, value = name.strip(), value.strip()
                    if name in names and name not in keys and value and not value.startswith("${"):
                        keys[name] = value
    missing = [k for k in names if k not in keys]
    if missing:
        sys.exit(f"нет ключей {', '.join(missing)}: задайте env или подключите агенту skills проекта "
                 f"hub-access / studio-video (~/.claude/{SKILL_FILES[0]})")
    return keys


class Hub:
    def __init__(self, url: str | None = None, key: str | None = None):
        if url is None or key is None:
            keys = load_keys("HUB_URL", "HUB_API_KEY")
            url, key = keys["HUB_URL"], keys["HUB_API_KEY"]
        self.url, self.key = url.rstrip("/"), key

    def call(self, method: str, path: str, body: dict | None = None) -> dict:
        req = urllib.request.Request(self.url + path, method=method,
                                     data=json.dumps(body).encode() if body is not None else None,
                                     headers={"Authorization": f"Bearer {self.key}", "Content-Type": "application/json"})
        try:
            with urllib.request.urlopen(req, timeout=120, context=SSL_CONTEXT) as resp:
                return json.loads(resp.read())
        except urllib.error.HTTPError as e:
            raise SystemExit(f"hub {method} {path}: HTTP {e.code} {e.read().decode(errors='replace')[:500]}")

    def recent(self, channel_id: str, n: int = 20) -> dict:
        return self.call("GET", f"/api/v1/channels/{urllib.parse.quote(channel_id)}/releases/recent?n={n}")

    def download(self, key: str, dest: pathlib.Path) -> pathlib.Path:
        url = self.call("POST", "/api/v1/media/download-url", {"object_key": key})["download_url"]
        dest = pathlib.Path(dest)
        dest.parent.mkdir(parents=True, exist_ok=True)
        with urllib.request.urlopen(url, timeout=300, context=SSL_CONTEXT) as resp, open(dest, "wb") as f:
            shutil.copyfileobj(resp, f)
        return dest

    def upload(self, development_id: str, path: pathlib.Path, name: str) -> str:
        import mimetypes

        ctype = mimetypes.guess_type(name)[0] or "application/octet-stream"
        out = self.call("POST", "/api/v1/media/upload-url",
                        {"development_id": development_id, "filename": name, "content_type": ctype})
        req = urllib.request.Request(out["upload_url"], method="PUT", data=pathlib.Path(path).read_bytes(),
                                     headers={"Content-Type": ctype})
        with urllib.request.urlopen(req, timeout=600, context=SSL_CONTEXT) as resp:
            if resp.status // 100 != 2:
                raise SystemExit(f"upload {name}: HTTP {resp.status}")
        return out["object_key"]

    def release_submit(self, manifest: dict) -> dict:
        return self.call("POST", "/api/v1/releases", manifest)


def artifact(artifact_id: str, kind: str | None = None):
    """Content of a PolyMaths artifact (or the latest artifact of `kind` in a delegation_result), via pm_artifact;
    a path to a JSON file is read as is (tests, hand runs)."""
    return artifact_with_id(artifact_id, kind)[0]


def artifact_with_id(artifact_id: str, kind: str | None = None):
    """(content, id of the artifact actually read): for a delegation_result that is the artifact found in it."""
    if pathlib.Path(artifact_id).is_file():
        return json.loads(pathlib.Path(artifact_id).read_text()), artifact_id
    sys.path.insert(0, str(pathlib.Path(__file__).resolve().parent))
    import pm_artifact

    s = pm_artifact.Session(pm_artifact.api_key())
    art = pm_artifact.fetch(s, artifact_id)
    if kind and art.get("kind") != kind:
        if art.get("kind") != "delegation_result":
            sys.exit(f"artifact {artifact_id} is {art.get('kind')!r}, not {kind!r} and not a delegation_result")
        found = [a for a in (art.get("content") or {}).get("artifacts", []) if a.get("kind") == kind]
        if not found:
            sys.exit(f"delegation_result {artifact_id} has no {kind!r} artifact")
        art = pm_artifact.fetch(s, found[-1]["id"])
    return art["content"], art.get("id") or artifact_id
