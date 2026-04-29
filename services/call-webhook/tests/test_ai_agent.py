import pytest
import respx
from httpx import Response

from rosie_call_webhook.ai_agent import generate_greeting


@pytest.mark.asyncio
@respx.mock
async def test_generate_greeting_from_ai_agent():
    respx.post("http://ai-agent:8010/chat").mock(
        return_value=Response(
            200,
            json={
                "model": "deepseek-chat",
                "reply": "您好，我是张三理发店的 AI 前台 Rosie。请问您来电是咨询还是预约？",
            },
        )
    )

    reply = await generate_greeting(
        "http://ai-agent:8010",
        "张三理发店",
        "+8613811112222",
        5,
    )

    assert reply is not None
    assert "AI 前台 Rosie" in reply


@pytest.mark.asyncio
async def test_generate_greeting_without_url_returns_none():
    reply = await generate_greeting(None, "张三理发店", "+8613811112222", 5)
    assert reply is None

