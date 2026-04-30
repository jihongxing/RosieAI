from __future__ import annotations

from collections.abc import AsyncIterator
from typing import Protocol, Any

from .config import Settings
from .local_fallback import LocalFallbackClient
from .ollama import OllamaClient
from .openai_compatible import OpenAICompatibleClient


class LLMClient(Protocol):
    async def health(self) -> dict[str, Any]:
        ...

    async def generate(self, model: str, prompt: str, system: str | None = None) -> str:
        ...

    async def stream_generate(self, model: str, prompt: str, system: str | None = None) -> AsyncIterator[str]:
        ...


def create_llm_client(settings: Settings) -> LLMClient:
    if settings.provider in {"local", "local_fallback"}:
        return LocalFallbackClient()
    if settings.provider == "ollama":
        return OllamaClient(settings.ollama_base_url, settings.timeout_seconds)
    if settings.provider in {"openai", "openai_compatible", "deepseek"}:
        return OpenAICompatibleClient(
            settings.openai_base_url,
            settings.openai_api_key,
            settings.timeout_seconds,
        )
    raise ValueError(f"unsupported ROSIE_LLM_PROVIDER: {settings.provider}")
