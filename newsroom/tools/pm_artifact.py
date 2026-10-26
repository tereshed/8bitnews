#!/usr/bin/env python3
"""Печатает content артефакта PolyMaths по его id — так команды читают артефакты чужих задач.

Делегированная задача видит только текст постановки, а не артефакты задачи-заказчика. Поэтому роутер Newsroom
передаёт командам id артефактов (досье, сценарий, юридический отчёт), а агенты скачивают их этим скриптом:

    python3 newsroom/tools/pm_artifact.py <artifact_id> > /tmp/research_dossier.json

В stdout — content артефакта (JSON), в stderr — вид и summary. Крупный content дочитывается кусками.
Ключ: env PM_READ_KEY, иначе строка PM_READ_KEY=... из файла polymaths.env skill'а проекта `polymaths-artifacts`
(секрет `${secret:PM_READ_KEY}` PolyMaths подставляет только в файлы skills и конфиги MCP, не в файлы песочницы).
Это ключ отдельного участника проекта с ролью viewer: он только читает.
"""
import json
import os
import pathlib
import ssl
import sys
import urllib.request

try:  # python.org-сборки Python на macOS не видят системные сертификаты
    import certifi
    SSL_CONTEXT = ssl.create_default_context(cafile=certifi.where())
except ImportError:
    SSL_CONTEXT = ssl.create_default_context()

URL = os.environ.get("POLYMATHS_MCP_URL", "https://mcp.polymaths.work/mcp")
SKILL_ENV = "skills/polymaths-artifacts/polymaths.env"


def env_files():
    for root in {pathlib.Path.home(), pathlib.Path("/home/sandbox"), pathlib.Path("/root")}:
        yield from root.glob(f".*/{SKILL_ENV}")
        yield from root.glob(SKILL_ENV)


def api_key():
    if os.environ.get("PM_READ_KEY"):
        return os.environ["PM_READ_KEY"]
    for path in env_files():
        for line in path.read_text().splitlines():
            name, _, value = line.partition("=")
            value = value.strip()
            if name.strip() == "PM_READ_KEY" and value and not value.startswith("${"):
                return value
    sys.exit(f"нет ключа: задайте PM_READ_KEY или подключите агенту skill polymaths-artifacts (~/.claude/{SKILL_ENV})")


class Session:
    def __init__(self, key):
        self.key, self.sid, self.next_id = key, None, 1
        self.rpc("initialize", {"protocolVersion": "2025-03-26", "capabilities": {},
                                "clientInfo": {"name": "pm_artifact", "version": "1"}})
        self.post({"jsonrpc": "2.0", "method": "notifications/initialized"})

    def post(self, body):
        headers = {"X-API-Key": self.key, "Content-Type": "application/json",
                   "Accept": "application/json, text/event-stream"}
        if self.sid:
            headers["Mcp-Session-Id"] = self.sid
        req = urllib.request.Request(URL, method="POST", data=json.dumps(body).encode(), headers=headers)
        with urllib.request.urlopen(req, timeout=120, context=SSL_CONTEXT) as resp:
            self.sid = resp.headers.get("Mcp-Session-Id") or self.sid
            return resp.read().decode()

    def rpc(self, method, params):
        self.next_id += 1
        raw = self.post({"jsonrpc": "2.0", "id": self.next_id, "method": method, "params": params})
        # streamable HTTP отвечает либо JSON, либо SSE с одной строкой data:
        lines = [l[5:] for l in raw.splitlines() if l.startswith("data:")] or [raw]
        msg = json.loads(lines[-1])
        if "error" in msg:
            sys.exit(f"{method}: {msg['error']}")
        return msg["result"]

    def tool(self, name, args):
        result = self.rpc("tools/call", {"name": name, "arguments": args})
        text = "".join(c.get("text", "") for c in result.get("content", []))
        if result.get("isError"):
            sys.exit(f"{name}: {text}")
        data = json.loads(text)
        return data.get("data", data) if isinstance(data, dict) else data


def main():
    if len(sys.argv) != 2:
        sys.exit("использование: pm_artifact.py <artifact_id>")
    artifact_id = sys.argv[1]
    s = Session(api_key())
    art = s.tool("artifact_get", {"artifact_id": artifact_id})
    content = art.get("content")
    if art.get("content_truncated"):
        parts, offset = [], 0
        while True:
            chunk = s.tool("text_recall", {"source": "artifact_content", "id": artifact_id, "offset": offset})
            parts.append(chunk["text"])
            if not chunk.get("has_more"):
                break
            offset = chunk["next_offset"]
        content = "".join(parts)
    if isinstance(content, str):
        try:
            content = json.loads(content)
        except json.JSONDecodeError:
            print(content)
            content = None
    if content is not None:
        print(json.dumps(content, ensure_ascii=False, indent=1))
    print(f"{art.get('kind')}: {art.get('summary', '')}", file=sys.stderr)


if __name__ == "__main__":
    main()
