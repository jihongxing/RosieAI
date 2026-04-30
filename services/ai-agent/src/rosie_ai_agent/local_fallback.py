from __future__ import annotations

from collections.abc import AsyncIterator
from typing import Any


class LocalFallbackClient:
    async def health(self) -> dict[str, Any]:
        return {
            "provider": "local_fallback",
            "model_loaded": True,
        }

    async def generate(self, model: str, prompt: str, system: str | None = None) -> str:
        text = _extract_customer_text(prompt)
        if any(word in text for word in ("预约", "剪头发", "理发", "到店")):
            return "可以的，请问您想预约哪一天、几点到店？"
        if any(word in text for word in ("投诉", "生气", "急", "马上")):
            return "我已经记录为紧急事项，会提醒店主尽快处理。"
        if any(word in text for word in ("电话", "联系", "回电")):
            return "好的，请您留下姓名和联系电话，我帮您记录。"
        return "好的，我先帮您记录，请继续说。"

    async def stream_generate(self, model: str, prompt: str, system: str | None = None) -> AsyncIterator[str]:
        yield await self.generate(model, prompt, system)


def _extract_customer_text(prompt: str) -> str:
    marker = "客户刚才说："
    if marker not in prompt:
        return prompt
    for line in prompt.split(marker, 1)[1].splitlines():
        text = line.strip()
        if text:
            return text
    return ""
