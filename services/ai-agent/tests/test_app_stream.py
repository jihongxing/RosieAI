import json

from fastapi.testclient import TestClient

import rosie_ai_agent.app as app_module
from rosie_ai_agent.app import app
from rosie_ai_agent.local_fallback import LocalFallbackClient


def test_chat_stream_returns_ndjson_tokens(monkeypatch):
    monkeypatch.setattr(app_module, "llm", LocalFallbackClient())
    with TestClient(app) as client:
        with client.stream(
            "POST",
            "/chat/stream",
            json={
                "merchant_name": "测试理发店",
                "customer_text": "你好，我想预约明天下午剪头发",
                "history": [],
            },
        ) as response:
            assert response.status_code == 200
            rows = [json.loads(line) for line in response.iter_lines() if line]

    assert rows[0]["type"] == "token"
    assert rows[-1]["type"] == "done"
