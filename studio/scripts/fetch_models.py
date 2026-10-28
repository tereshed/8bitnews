"""Downloads the model files listed in studio/models.json into a directory,
checking each against its sha256 -- run once by `docker build`, so a build
of a video later needs no network.

    python3 scripts/fetch_models.py models.json /opt/kokoro
"""
from __future__ import annotations

import hashlib
import json
import sys
import urllib.request
from pathlib import Path


def fetch(manifest_path: Path, dest: Path) -> None:
    manifest = json.loads(manifest_path.read_text())
    for entry in manifest["kokoro"]["files"]:
        out = dest / entry["path"]
        out.parent.mkdir(parents=True, exist_ok=True)
        tmp = out.with_suffix(out.suffix + ".part")
        h = hashlib.sha256()
        with urllib.request.urlopen(entry["url"], timeout=120) as resp, open(tmp, "wb") as f:
            for chunk in iter(lambda: resp.read(1 << 20), b""):
                h.update(chunk)
                f.write(chunk)
        if h.hexdigest() != entry["sha256"]:
            tmp.unlink()
            sys.exit(f"{entry['url']}: sha256 {h.hexdigest()} != {entry['sha256']}")
        tmp.rename(out)
        print(f"{entry['path']}: ok")
    (dest / "models.json").write_text(manifest_path.read_text())


if __name__ == "__main__":
    fetch(Path(sys.argv[1]), Path(sys.argv[2]))
