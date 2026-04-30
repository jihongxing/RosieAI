import pytest
import json

from rosie_ai_agent.local_fallback import LocalFallbackClient


@pytest.mark.asyncio
async def test_local_fallback_generates_short_phone_reply():
    client = LocalFallbackClient()

    reply = await client.generate(
        "local-fallback",
        "客户刚才说：\n你好，我想预约明天下午剪头发\n\n请给出 Rosie 下一句电话回复。",
    )

    assert reply == "可以的，请问您想预约哪一天、几点到店？"


@pytest.mark.asyncio
async def test_local_fallback_streams_reply():
    client = LocalFallbackClient()

    chunks = [
        item
        async for item in client.stream_generate(
            "local-fallback",
            "客户刚才说：\n请给我回个电话",
        )
    ]

    assert chunks == ["好的，请您留下姓名和联系电话，我帮您记录。"]


@pytest.mark.asyncio
async def test_local_fallback_extracts_structured_summary():
    client = LocalFallbackClient()

    result = await client.generate(
        "local-fallback",
        """
你是 Rosie 的通话整理助手。请输出 JSON。
字段包括 summary、customer_name、customer_phone、intent、appointment_time、service、priority、need_human_followup。
通话转写：
客户：你好，我姓王，电话是13811112222，想预约明天下午三点剪头发。
""",
    )

    data = json.loads(result)
    assert data["intent"] == "appointment"
    assert data["customer_name"] == "王"
    assert data["customer_phone"] == "13811112222"
    assert data["priority"] == "high"
