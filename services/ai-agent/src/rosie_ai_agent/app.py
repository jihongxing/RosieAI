from __future__ import annotations

from typing import Any

import httpx
from fastapi import FastAPI, HTTPException
from pydantic import BaseModel, Field

from .config import get_settings
from .llm import create_llm_client
from .prompts import chat_system_prompt, chat_user_prompt, extract_prompt


settings = get_settings()
llm = create_llm_client(settings)

app = FastAPI(title="Rosie AI Agent MVP", version="0.1.0")


class ChatMessage(BaseModel):
    role: str
    content: str


class ChatRequest(BaseModel):
    merchant_name: str = Field(default="测试商家")
    merchant_profile: str | None = None
    customer_text: str = Field(min_length=1)
    history: list[ChatMessage] = Field(default_factory=list)


class ChatResponse(BaseModel):
    model: str
    reply: str


class ExtractRequest(BaseModel):
    merchant_name: str = Field(default="测试商家")
    transcript: str = Field(min_length=1)


class ExtractResponse(BaseModel):
    model: str
    result: str


@app.get("/health")
def health() -> dict[str, str]:
    return {"status": "ok", "provider": settings.provider, "model": settings.model}


@app.get("/health/llm")
async def llm_health() -> dict[str, Any]:
    try:
        data = await llm.health()
    except httpx.HTTPError as exc:
        raise HTTPException(status_code=503, detail=f"llm unavailable: {exc}") from exc
    return {"status": "ok", "provider": settings.provider, "llm": data}


@app.post("/chat", response_model=ChatResponse)
async def chat(request: ChatRequest) -> ChatResponse:
    history_text = "\n".join(f"{item.role}: {item.content}" for item in request.history[-8:])
    try:
        reply = await llm.generate(
            model=settings.model,
            system=chat_system_prompt(request.merchant_name, request.merchant_profile),
            prompt=chat_user_prompt(request.customer_text, history_text),
        )
    except httpx.HTTPError as exc:
        raise HTTPException(status_code=503, detail=f"llm request failed: {exc}") from exc
    return ChatResponse(model=settings.model, reply=reply)


@app.post("/extract", response_model=ExtractResponse)
async def extract(request: ExtractRequest) -> ExtractResponse:
    try:
        result = await llm.generate(
            model=settings.model,
            prompt=extract_prompt(request.merchant_name, request.transcript),
        )
    except httpx.HTTPError as exc:
        raise HTTPException(status_code=503, detail=f"llm request failed: {exc}") from exc
    return ExtractResponse(model=settings.model, result=result)
