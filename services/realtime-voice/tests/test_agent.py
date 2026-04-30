import respx
import pytest
from httpx import Response

from rosie_realtime_voice.agent import RealtimeAgent
from rosie_realtime_voice.session import build_session_context


@pytest.mark.asyncio
@respx.mock
async def test_realtime_agent_reuses_http_client():
    respx.post("http://ai.local/chat").mock(
        return_value=Response(200, json={"reply": "好的，我帮您记录。", "model": "test"})
    )
    agent = RealtimeAgent("http://ai.local", 5, enabled=True)
    context = build_session_context("s1", {"callSid": "s1", "merchant_name": "测试商家"})

    await agent.reply(context, "你好", [])
    first_client = agent._client
    await agent.reply(context, "我想预约", [])

    assert agent._client is first_client
    await agent.close()


@pytest.mark.asyncio
@respx.mock
async def test_realtime_agent_warmup_opens_health_connection():
    respx.get("http://ai.local/health").mock(
        return_value=Response(200, json={"status": "ok"})
    )
    agent = RealtimeAgent("http://ai.local", 5, enabled=True)

    await agent.warmup()

    assert agent._client is not None
    await agent.close()
