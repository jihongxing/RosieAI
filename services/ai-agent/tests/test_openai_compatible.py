import respx
from httpx import Response
import pytest

from rosie_ai_agent.openai_compatible import OpenAICompatibleClient


@pytest.mark.asyncio
@respx.mock
async def test_openai_compatible_generate():
    respx.post("https://api.deepseek.com/chat/completions").mock(
        return_value=Response(
            200,
            json={
                "choices": [
                    {
                        "message": {
                            "content": "您好，可以的。请问您想预约几点？"
                        }
                    }
                ]
            },
        )
    )

    client = OpenAICompatibleClient("https://api.deepseek.com", "test-key", 10)
    reply = await client.generate("deepseek-chat", "客户想预约", system="你是 Rosie")

    assert reply == "您好，可以的。请问您想预约几点？"

