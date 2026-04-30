from __future__ import annotations

import json
from collections.abc import AsyncIterator
from typing import Any

import httpx


class OpenAICompatibleClient:
    def __init__(self, base_url: str, api_key: str | None, timeout_seconds: float) -> None:
        self.base_url = base_url.rstrip("/")
        self.api_key = api_key
        self.timeout = httpx.Timeout(timeout_seconds)

    def _headers(self) -> dict[str, str]:
        headers = {"Content-Type": "application/json"}
        if self.api_key:
            headers["Authorization"] = f"Bearer {self.api_key}"
        return headers

    async def health(self) -> dict[str, Any]:
        # Many OpenAI-compatible providers either do not expose /models or require
        # account-specific permissions. Treat configured credentials as enough for
        # service readiness; /chat is the real integration test.
        return {
            "base_url": self.base_url,
            "api_key_configured": bool(self.api_key),
        }

    async def generate(self, model: str, prompt: str, system: str | None = None) -> str:
        messages = []
        if system:
            messages.append({"role": "system", "content": system})
        messages.append({"role": "user", "content": prompt})

        payload = {
            "model": model,
            "messages": messages,
            "temperature": 0.3,
            "max_tokens": 512,
            "stream": False,
        }

        async with httpx.AsyncClient(timeout=self.timeout) as client:
            response = await client.post(
                f"{self.base_url}/chat/completions",
                headers=self._headers(),
                json=payload,
            )
            response.raise_for_status()
            data = response.json()

        choices = data.get("choices") or []
        if not choices:
            return ""
        message = choices[0].get("message") or {}
        return str(message.get("content", "")).strip()

    async def stream_generate(self, model: str, prompt: str, system: str | None = None) -> AsyncIterator[str]:
        messages = []
        if system:
            messages.append({"role": "system", "content": system})
        messages.append({"role": "user", "content": prompt})

        payload = {
            "model": model,
            "messages": messages,
            "temperature": 0.3,
            "max_tokens": 160,
            "stream": True,
        }

        async with httpx.AsyncClient(timeout=self.timeout) as client:
            async with client.stream(
                "POST",
                f"{self.base_url}/chat/completions",
                headers=self._headers(),
                json=payload,
            ) as response:
                response.raise_for_status()
                async for line in response.aiter_lines():
                    if not line or not line.startswith("data:"):
                        continue
                    data_line = line.removeprefix("data:").strip()
                    if data_line == "[DONE]":
                        break
                    data = json.loads(data_line)
                    choices = data.get("choices") or []
                    if not choices:
                        continue
                    delta = choices[0].get("delta") or {}
                    token = str(delta.get("content") or "")
                    if token:
                        yield token
