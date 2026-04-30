from __future__ import annotations

import logging
from typing import Any

import httpx

from .session import RealtimeSessionContext


logger = logging.getLogger(__name__)


class RealtimeAgent:
    def __init__(self, ai_agent_url: str | None, timeout_seconds: float, enabled: bool = True) -> None:
        self.ai_agent_url = ai_agent_url.rstrip("/") if ai_agent_url else None
        self.timeout_seconds = timeout_seconds
        self.enabled = enabled
        self._client: httpx.AsyncClient | None = None

    async def close(self) -> None:
        if self._client:
            await self._client.aclose()
            self._client = None

    async def warmup(self) -> None:
        if not self.enabled or not self.ai_agent_url:
            return
        response = await self._http_client().get(f"{self.ai_agent_url}/health")
        response.raise_for_status()

    async def reply(
        self,
        context: RealtimeSessionContext,
        customer_text: str,
        history: list[dict[str, str]],
    ) -> dict[str, Any] | None:
        if not self.enabled or not customer_text:
            return None
        if not self.ai_agent_url:
            return {
                "type": "agent_reply",
                "source": "local_fallback",
                "reply": self._fallback_reply(context),
            }

        payload = {
            "merchant_name": context.merchant_name or "测试商家",
            "system_prompt": context.system_prompt,
            "customer_text": customer_text,
            "history": history[-8:],
        }
        try:
            response = await self._http_client().post(f"{self.ai_agent_url}/chat", json=payload)
            response.raise_for_status()
            data = response.json()
        except httpx.HTTPError as exc:
            logger.warning("realtime ai-agent chat failed: %s", exc)
            return {
                "type": "agent_reply",
                "source": "fallback_after_error",
                "reply": self._fallback_reply(context),
                "error": str(exc),
            }

        reply = str(data.get("reply", "")).strip()
        if not reply:
            return None
        return {
            "type": "agent_reply",
            "source": "ai_agent",
            "model": data.get("model"),
            "reply": reply,
        }

    def _http_client(self) -> httpx.AsyncClient:
        if self._client is None:
            self._client = httpx.AsyncClient(timeout=httpx.Timeout(self.timeout_seconds))
        return self._client

    def _fallback_reply(self, context: RealtimeSessionContext) -> str:
        merchant_name = context.merchant_name or "商家"
        return f"您好，这里是{merchant_name}的 AI 前台 Rosie。我先帮您记录，请继续说。"
