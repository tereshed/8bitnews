"""Locates the repository root and the channel/contract files `studio build`
and `studio validate` read from it (studio/README.md). This is separate from
`studio.paths.CHANNELS_DIR` (studio/channels/<id>/, the font/palette/anchor
assets shared by every template for a channel): STUDIO_REPO_ROOT points at
the *whole* 8bitnews repo, mounted read-only, so `studio build` can read the
channel's publishing profile (channels/<id>/channel.yaml) and the contract
schemas (newsroom/contracts/*.schema.json) without that repo being part of
this package's own image."""
from __future__ import annotations

import json
import os
from functools import lru_cache
from pathlib import Path

import yaml


class RepoRootNotSet(Exception):
    """STUDIO_REPO_ROOT is required for anything that reads the repo's
    channel profile or contract schemas. Raised (not skipped) so a container
    run without the repo root mounted fails loudly instead of silently
    falling back to stale or missing data."""


def repo_root() -> Path:
    value = os.environ.get("STUDIO_REPO_ROOT")
    if not value:
        raise RepoRootNotSet(
            "STUDIO_REPO_ROOT is not set -- mount the 8bitnews repo root read-only and set "
            "STUDIO_REPO_ROOT to it, e.g. `-v $(pwd):/repo:ro -e STUDIO_REPO_ROOT=/repo`"
        )
    root = Path(value)
    if not root.is_dir():
        raise RepoRootNotSet(f"STUDIO_REPO_ROOT={value!r} is not a directory")
    return root


def default_channels_dir() -> Path:
    return repo_root() / "channels"


def contracts_dir() -> Path:
    return repo_root() / "newsroom" / "contracts"


def load_channel_profile(channel_id: str, channels_dir: Path | None = None) -> dict:
    base = Path(channels_dir) if channels_dir is not None else default_channels_dir()
    path = base / channel_id / "channel.yaml"
    if not path.is_file():
        raise FileNotFoundError(f"channel profile not found: {path}")
    with open(path) as f:
        return yaml.safe_load(f)


@lru_cache(maxsize=16)
def _cached_schema(path_str: str) -> dict:
    with open(path_str) as f:
        return json.load(f)


def load_contract_schema(name: str) -> dict:
    """`name` is a filename under newsroom/contracts/, e.g. 'script.schema.json'."""
    return _cached_schema(str(contracts_dir() / name))
