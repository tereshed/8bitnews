"""studio.openrouter without the network: caching, cost, retries, refusals."""
import base64
import io
import json

import pytest
from PIL import Image

from studio.errors import BuildError
from studio.openrouter import NoImageError, OpenRouterClient, TransientError


def _png() -> bytes:
    buf = io.BytesIO()
    Image.new("RGB", (4, 4), (0, 255, 0)).save(buf, "PNG")
    return buf.getvalue()


def _image_reply(cost=0.067):
    url = "data:image/png;base64," + base64.b64encode(_png()).decode()
    return {"choices": [{"message": {"content": "", "images": [{"image_url": {"url": url}}]}}], "usage": {"cost": cost}}


class Transport:
    def __init__(self, *replies):
        self.replies = list(replies)
        self.calls = []

    def __call__(self, url, headers, body, timeout_s):
        self.calls.append((url, headers, body))
        r = self.replies.pop(0)
        if isinstance(r, Exception):
            raise r
        return r


def _client(tmp_path, transport, key="sk-test-key"):
    return OpenRouterClient(api_key=key, cache_dir=tmp_path, transport=transport, backoff_s=0)


def test_image_is_cached_and_costs_once(tmp_path):
    t = Transport(_image_reply())
    c = _client(tmp_path, t)
    first = c.generate_image("m", "a desk", "3:4")
    again = c.generate_image("m", "a desk", "3:4")
    assert first.png == again.png == _png()
    assert (first.cost_usd, first.cached) == (0.067, False)
    assert (again.cost_usd, again.cached) == (0.0, True)
    assert len(t.calls) == 1


def test_another_variant_or_prompt_asks_again(tmp_path):
    t = Transport(_image_reply(), _image_reply(), _image_reply())
    c = _client(tmp_path, t)
    c.generate_image("m", "a desk", "3:4")
    c.generate_image("m", "a desk", "3:4", variant=1)
    c.generate_image("m", "a desk, no text", "3:4")
    assert len(t.calls) == 3


def test_the_key_goes_in_the_header_and_never_into_the_cache(tmp_path):
    t = Transport(_image_reply())
    _client(tmp_path, t).generate_image("m", "a desk", "3:4")
    assert t.calls[0][1]["Authorization"] == "Bearer sk-test-key"
    for f in tmp_path.rglob("*"):
        if f.is_file():
            assert "sk-test-key" not in f.read_text()


def test_input_images_are_sent(tmp_path):
    t = Transport(_image_reply())
    _client(tmp_path, t).generate_image("m", "close the eyes", "3:4", images=(_png(),))
    content = t.calls[0][2]["messages"][0]["content"]
    assert content[0] == {"type": "text", "text": "close the eyes"}
    assert content[1]["image_url"]["url"].startswith("data:image/png;base64,")


def test_transient_errors_are_retried(tmp_path):
    t = Transport(TransientError("timeout"), TransientError("HTTP 502"), _image_reply())
    assert _client(tmp_path, t).generate_image("m", "a desk", "3:4").png == _png()
    assert len(t.calls) == 3


def test_gives_up_after_the_retries(tmp_path):
    t = Transport(TransientError("timeout"), TransientError("timeout"), TransientError("timeout"))
    with pytest.raises(BuildError, match="after 3 attempts"):
        _client(tmp_path, t).generate_image("m", "a desk", "3:4")


def test_a_reply_without_an_image_is_a_refusal_and_is_asked_again(tmp_path):
    refusal = {"choices": [{"message": {"content": "I can't draw that person."}}], "usage": {"cost": 0.002}}
    t = Transport(refusal, _image_reply())
    c = _client(tmp_path, t)
    with pytest.raises(NoImageError, match="can't draw") as e:
        c.generate_image("m", "a senator", "3:4")
    assert e.value.cost_usd == 0.002  # billed all the same
    assert c.generate_image("m", "a senator", "3:4").png == _png()  # not served from the cache
    assert len(t.calls) == 2


def test_a_reply_with_no_choices_fails_readably_and_is_not_cached(tmp_path):
    t = Transport({"choices": []}, _image_reply())
    c = _client(tmp_path, t)
    with pytest.raises(BuildError, match="no message"):
        c.generate_image("m", "a desk", "3:4")
    assert c.generate_image("m", "a desk", "3:4").png == _png()


def test_without_a_key_it_says_which_variable(tmp_path, monkeypatch):
    monkeypatch.delenv("STUDIO_OPENROUTER_API_KEY", raising=False)
    with pytest.raises(BuildError, match="STUDIO_OPENROUTER_API_KEY"):
        OpenRouterClient(cache_dir=tmp_path, transport=Transport()).generate_image("m", "a desk", "3:4")


def test_ask_json_reads_a_fenced_answer(tmp_path):
    t = Transport({"choices": [{"message": {"content": "```json\n{\"text\": false}\n```"}}], "usage": {"cost": 0.001}})
    reply = _client(tmp_path, t).ask_json("v", "any text?", (_png(),))
    assert reply.data == {"text": False} and reply.cost_usd == 0.001
    assert t.calls[0][2]["response_format"] == {"type": "json_object"}


def test_ask_json_rejects_prose_and_asks_again_next_time(tmp_path):
    t = Transport({"choices": [{"message": {"content": "There is no text."}}]},
                  {"choices": [{"message": {"content": "{\"text\": false}"}}]})
    c = _client(tmp_path, t)
    with pytest.raises(BuildError, match="JSON"):
        c.ask_json("v", "any text?")
    assert c.ask_json("v", "any text?").data == {"text": False}


def test_ask_json_variant_asks_again(tmp_path):
    t = Transport({"choices": [{"message": {"content": "{\"a\": 1}"}}]}, {"choices": [{"message": {"content": "{\"a\": 2}"}}]})
    c = _client(tmp_path, t)
    assert c.ask_json("v", "q").data == {"a": 1} and c.ask_json("v", "q", variant=1).data == {"a": 2}
    assert c.ask_json("v", "q").cached


def test_api_error_body_fails(tmp_path):
    t = Transport({"error": {"message": "insufficient credits"}})
    with pytest.raises(BuildError, match="insufficient credits"):
        _client(tmp_path, t).generate_image("m", "a desk", "3:4")
    assert not list(tmp_path.rglob("*.json"))  # an error is never cached
