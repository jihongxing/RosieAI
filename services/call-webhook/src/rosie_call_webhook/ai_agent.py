from __future__ import annotations

import logging

import httpx


logger = logging.getLogger(__name__)


async def generate_greeting(
    ai_agent_url: str | None,
    merchant_name: str,
    caller_number: str,
    timeout_seconds: float,
    system_prompt: str | None = None,
) -> str | None:
    if not ai_agent_url:
        return None

    customer_text = (
        "客户刚刚来电，但机主当前无应答、忙线或不可及。"
        "请用一句适合电话播放的中文回复，明确你是 AI 前台 Rosie，"
        "说明会帮忙记录，并询问对方来电目的是咨询还是预约。"
    )
    payload = {
        "merchant_name": merchant_name,
        "system_prompt": system_prompt,
        "customer_text": customer_text,
        "history": [
            {
                "role": "system",
                "content": f"主叫号码：{caller_number or '未知'}",
            }
        ],
    }

    try:
        async with httpx.AsyncClient(timeout=httpx.Timeout(timeout_seconds)) as client:
            response = await client.post(f"{ai_agent_url.rstrip('/')}/chat", json=payload)
            response.raise_for_status()
            data = response.json()
    except httpx.HTTPError as exc:
        logger.warning("ai greeting failed: %s", exc)
        return None

    reply = str(data.get("reply", "")).strip()
    return reply or None


async def extract_call_summary(
    ai_agent_url: str | None,
    merchant_name: str,
    transcript: str,
    timeout_seconds: float,
    system_prompt: str | None = None,
) -> str | None:
    if not ai_agent_url:
        return None

    payload = {
        "merchant_name": merchant_name,
        "system_prompt": system_prompt,
        "transcript": transcript,
    }

    try:
        async with httpx.AsyncClient(timeout=httpx.Timeout(timeout_seconds)) as client:
            response = await client.post(f"{ai_agent_url.rstrip('/')}/extract", json=payload)
            response.raise_for_status()
            data = response.json()
    except httpx.HTTPError as exc:
        logger.warning("ai extract failed: %s", exc)
        return None

    result = str(data.get("result", "")).strip()
    return result or None
