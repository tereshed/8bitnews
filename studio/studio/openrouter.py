"""OpenRouter client for the studio's pictures (docs/architecture.md §12):
image generation and editing (Gemini Flash Image by default) and a vision
question answered in JSON (the frame check, the anchor's face landmarks).

The key comes from env STUDIO_OPENROUTER_API_KEY only. Every reply is cached
on disk under the sha256 of its request (env STUDIO_IMAGE_CACHE, default
~/.cache/8bitnews-studio/openrouter), so building the same video again pays
nothing; each reply carries what OpenRouter charged for it (0 when cached).
Only a usable reply is cached: a refusal or an answer that isn't JSON is
asked again next time. Network errors, timeouts, 429 and 5xx are retried
`retries` times; any other HTTP error and a reply without an image fail at
once.
"""
from __future__ import annotations

import base64
import hashlib
import http.client
import io
import json
import os
import ssl
import time
import urllib.error
import urllib.request
from dataclasses import dataclass
from pathlib import Path
from typing import Any, Callable

from .errors import BuildError

API_URL = "https://openrouter.ai/api/v1/chat/completions"
KEY_ENV = "STUDIO_OPENROUTER_API_KEY"
CACHE_ENV = "STUDIO_IMAGE_CACHE"

# (url, headers, body, timeout_s) -> parsed JSON reply; tests pass a fake.
Transport = Callable[[str, dict, dict, float], dict]


class TransientError(Exception):
    """A failure worth retrying: network, timeout, 429, 5xx."""


class NoImageError(BuildError):
    """The model answered without an image (usually a refusal); `cost_usd`
    is what that answer was billed."""

    def __init__(self, message: str, cost_usd: float = 0.0):
        super().__init__(message)
        self.cost_usd = cost_usd


@dataclass(frozen=True)
class ImageReply:
    png: bytes
    cost_usd: float
    cached: bool
    model: str


@dataclass(frozen=True)
class JsonReply:
    data: dict
    cost_usd: float
    cached: bool
    model: str


def default_cache_dir() -> Path:
    env = os.environ.get(CACHE_ENV)
    return Path(env) if env else Path.home() / ".cache" / "8bitnews-studio" / "openrouter"


def _urllib_transport(url: str, headers: dict, body: dict, timeout_s: float) -> dict:
    req = urllib.request.Request(url, data=json.dumps(body).encode(), method="POST",
                                 headers={"Content-Type": "application/json", **headers})
    try:
        with urllib.request.urlopen(req, timeout=timeout_s, context=ssl.create_default_context()) as resp:
            return json.loads(resp.read())
    except urllib.error.HTTPError as e:
        detail = e.read().decode(errors="replace")[:300]
        if e.code == 429 or e.code >= 500:
            raise TransientError(f"HTTP {e.code}: {detail}") from e
        raise BuildError(f"OpenRouter HTTP {e.code}: {detail}") from e
    except (urllib.error.URLError, TimeoutError, ConnectionError, http.client.IncompleteRead,
            json.JSONDecodeError) as e:
        raise TransientError(str(e)) from e


def _data_url(png: bytes) -> str:
    return "data:image/png;base64," + base64.b64encode(png).decode()


def _png_from_data_url(url: str) -> bytes:
    if not url.startswith("data:"):
        raise BuildError(f"OpenRouter returned an image URL, expected inline data: {url[:80]}")
    return base64.b64decode(url.split(",", 1)[1])


def _decodes(data: bytes) -> bool:
    from PIL import Image

    try:
        with Image.open(io.BytesIO(data)) as img:
            img.verify()
        return True
    except Exception:  # noqa: BLE001 - whatever PIL can't read is not an image
        return False


def _parse_json_text(text: str) -> dict:
    """The model's text as JSON, tolerating a ```json fence around it."""
    s = text.strip()
    if s.startswith("```"):
        s = s.split("\n", 1)[1] if "\n" in s else ""
        s = s.rsplit("```", 1)[0]
    try:
        data = json.loads(s)
    except json.JSONDecodeError as e:
        raise BuildError(f"vision model did not answer in JSON: {text[:200]!r}") from e
    if not isinstance(data, dict):
        raise BuildError(f"vision model answered {type(data).__name__}, expected a JSON object")
    return data


class OpenRouterClient:
    def __init__(
        self,
        api_key: str | None = None,
        cache_dir: Path | None = None,
        timeout_s: float = 180.0,
        retries: int = 2,
        transport: Transport | None = None,
        backoff_s: float = 2.0,
    ):
        self.api_key = api_key if api_key is not None else os.environ.get(KEY_ENV, "")
        self.cache_dir = Path(cache_dir) if cache_dir else default_cache_dir()
        self.timeout_s = timeout_s
        self.retries = retries
        self.transport = transport or _urllib_transport
        self.backoff_s = backoff_s

    def _post(self, body: dict, variant: int, parse: Callable[[dict], Any]) -> tuple[Any, float, bool]:
        """(parse(reply), cost, cached). `parse` raises for a reply that can't
        be used, and only one that parses is cached; `variant` asks again for
        the same request without reusing the cached answer."""
        key = hashlib.sha256(json.dumps([body, variant], sort_keys=True).encode()).hexdigest()
        path = self.cache_dir / key[:2] / f"{key}.json"
        if path.is_file():
            return parse(json.loads(path.read_text())), 0.0, True
        if not self.api_key:
            raise BuildError(f"no OpenRouter key: set env {KEY_ENV} (or build without generated pictures)")
        headers = {"Authorization": f"Bearer {self.api_key}"}
        for attempt in range(self.retries + 1):
            try:
                reply = self.transport(API_URL, headers, body, self.timeout_s)
                break
            except TransientError as e:
                if attempt == self.retries:
                    raise BuildError(f"OpenRouter failed after {self.retries + 1} attempts: {e}") from e
                time.sleep(self.backoff_s * (attempt + 1))
        if "error" in reply:
            raise BuildError(f"OpenRouter error: {json.dumps(reply['error'])[:300]}")
        cost = float((reply.get("usage") or {}).get("cost") or 0.0)
        try:
            value = parse(reply)
        except NoImageError as e:
            e.cost_usd = cost
            raise
        path.parent.mkdir(parents=True, exist_ok=True)
        tmp = path.with_suffix(".part")
        tmp.write_text(json.dumps(reply))
        tmp.rename(path)
        return value, cost, False

    @staticmethod
    def _message(reply: dict) -> dict:
        choices = reply.get("choices") or []
        if not choices or not isinstance(choices[0].get("message"), dict):
            raise BuildError(f"OpenRouter reply has no message: {json.dumps(reply)[:200]}")
        return choices[0]["message"]

    def generate_image(
        self, model: str, prompt: str, aspect_ratio: str, images: tuple[bytes, ...] = (), variant: int = 0
    ) -> ImageReply:
        """One image from `prompt`; `images` (PNG bytes) are given to the
        model to edit or follow; another `variant` draws it again."""
        content: list[dict] = [{"type": "text", "text": prompt}]
        content += [{"type": "image_url", "image_url": {"url": _data_url(png)}} for png in images]
        body = {
            "model": model,
            "modalities": ["image", "text"],
            "image_config": {"aspect_ratio": aspect_ratio},
            "messages": [{"role": "user", "content": content}],
        }
        def parse(reply: dict) -> bytes:
            message = self._message(reply)
            found = message.get("images") or []
            if not found:
                raise NoImageError(f"{model} returned no image: {(message.get('content') or '')[:300]!r}")
            png = _png_from_data_url(found[0]["image_url"]["url"])
            if not _decodes(png):
                raise NoImageError(f"{model} returned an image that does not decode")
            return png

        png, cost, cached = self._post(body, variant, parse)
        return ImageReply(png, cost, cached, model)

    def ask_json(self, model: str, prompt: str, images: tuple[bytes, ...] = (), variant: int = 0) -> JsonReply:
        """A question about `images`, answered as one JSON object; another
        `variant` asks again."""
        content: list[dict] = [{"type": "text", "text": prompt}]
        content += [{"type": "image_url", "image_url": {"url": _data_url(png)}} for png in images]
        body = {
            "model": model,
            "response_format": {"type": "json_object"},
            "temperature": 0,
            "messages": [{"role": "user", "content": content}],
        }
        data, cost, cached = self._post(body, variant, lambda r: _parse_json_text(self._message(r).get("content") or ""))
        return JsonReply(data, cost, cached, model)
