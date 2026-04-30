import pytest
import respx
from httpx import Response

from rosie_call_webhook.ai_agent import extract_call_summary, generate_greeting
from rosie_call_webhook.business_api import fetch_merchant_ai_context


@pytest.mark.asyncio
@respx.mock
async def test_generate_greeting_from_ai_agent():
    route = respx.post("http://ai-agent:8010/chat").mock(
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
        "服务项目：剪发、烫染、护理",
    )

    assert reply is not None
    assert "AI 前台 Rosie" in reply
    assert route.calls.last.request is not None
    payload = route.calls.last.request.content.decode("utf-8")
    assert "服务项目：剪发、烫染、护理" in payload


@pytest.mark.asyncio
async def test_generate_greeting_without_url_returns_none():
    reply = await generate_greeting(None, "张三理发店", "+8613811112222", 5)
    assert reply is None


@pytest.mark.asyncio
@respx.mock
async def test_extract_call_summary_sends_system_prompt():
    route = respx.post("http://ai-agent:8010/extract").mock(
        return_value=Response(
            200,
            json={
                "model": "deepseek-chat",
                "result": '{"summary":"客户想预约剪发","intent":"appointment"}',
            },
        )
    )

    result = await extract_call_summary(
        "http://ai-agent:8010",
        "张三理发店",
        "客户想预约明天下午剪发",
        5,
        "服务项目：剪发、烫染、护理",
    )

    assert result is not None
    assert "appointment" in result
    payload = route.calls.last.request.content.decode("utf-8")
    assert "服务项目：剪发、烫染、护理" in payload


@pytest.mark.asyncio
@respx.mock
async def test_fetch_merchant_ai_context_from_business_api():
    respx.get("http://api-go:8030/merchant-profile").mock(
        return_value=Response(
            200,
            json={
                "merchant": {"merchant_id": "demo-merchant", "merchant_name": "张三理发店"},
                "profile": {"industry": "hair_salon"},
                "template": {"key": "hair_salon"},
                "system_prompt": "行业模板：理发店\n服务项目：剪发、烫染、护理",
            },
        )
    )

    context = await fetch_merchant_ai_context("http://api-go:8030", "demo-merchant", 5)

    assert context is not None
    assert context["template"]["key"] == "hair_salon"
    assert "剪发、烫染、护理" in context["system_prompt"]
