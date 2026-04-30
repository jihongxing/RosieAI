import pytest

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
