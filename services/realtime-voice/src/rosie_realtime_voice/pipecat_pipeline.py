from __future__ import annotations

import base64
import logging
from typing import Any

import httpx

from .pipeline import RealtimeTurn
from .session import RealtimeSessionContext


logger = logging.getLogger(__name__)


class PipecatTurnPipeline:
    def __init__(self, url: str, timeout_seconds: float) -> None:
        self.url = url.rstrip("/")
        self.timeout_seconds = timeout_seconds

    async def warmup(self) -> None:
        return None

    async def close(self) -> None:
        return None

    async def process_audio(
        self,
        context: RealtimeSessionContext,
        audio: bytes,
        history: list[dict[str, str]],
    ) -> RealtimeTurn | None:
        if not audio:
            return None
        payload = {
            "input_type": "audio",
            "audio_base64": base64.b64encode(audio).decode("ascii"),
            "sample_rate": context.sample_rate,
            "session": context.to_dict(),
            "history": history[-8:],
        }
        return await self._request_turn(payload, history, default_stt_source="pipecat_stt")

    async def process_text(
        self,
        context: RealtimeSessionContext,
        customer_text: str,
        history: list[dict[str, str]],
    ) -> RealtimeTurn | None:
        if not customer_text:
            return None
        payload = {
            "input_type": "text",
            "text": customer_text,
            "sample_rate": context.sample_rate,
            "session": context.to_dict(),
            "history": history[-8:],
        }
        return await self._request_turn(payload, history, default_stt_source="text_frame")

    async def _request_turn(
        self,
        payload: dict[str, Any],
        history: list[dict[str, str]],
        default_stt_source: str,
    ) -> RealtimeTurn | None:
        try:
            async with httpx.AsyncClient(timeout=httpx.Timeout(self.timeout_seconds)) as client:
                response = await client.post(f"{self.url}/turn", json=payload)
                response.raise_for_status()
                data = response.json()
        except httpx.HTTPError as exc:
            logger.warning("pipecat turn request failed: %s", exc)
            return None

        turn = _turn_from_payload(data, default_stt_source)
        if not turn:
            return None
        if turn.transcript:
            history.append({"role": "user", "content": turn.transcript})
        if turn.reply and turn.reply.get("reply"):
            history.append({"role": "assistant", "content": str(turn.reply["reply"])})
        return turn


def _turn_from_payload(payload: dict[str, Any], default_stt_source: str) -> RealtimeTurn | None:
    transcript = _string_value(payload, "transcript", "text", "customer_text")
    reply = _reply_payload(payload)
    encoded_audio = _string_value(payload, "audio_base64", "audio")
    audio = base64.b64decode(encoded_audio) if encoded_audio else None
    if not transcript and not reply and not audio:
        return None
    return RealtimeTurn(
        transcript=transcript,
        reply=reply,
        audio=audio,
        tts_content_type=_string_value(payload, "content_type", "tts_content_type"),
        stt_source=_string_value(payload, "stt_source") or default_stt_source,
        tts_source=_string_value(payload, "tts_source") or ("pipecat_tts" if audio else ""),
    )


def _reply_payload(payload: dict[str, Any]) -> dict[str, Any] | None:
    reply = payload.get("reply")
    if isinstance(reply, dict):
        text = _string_value(reply, "reply", "text", "content")
        if not text:
            return None
        return {
            "type": str(reply.get("type") or "agent_reply"),
            "source": str(reply.get("source") or "pipecat_agent"),
            "model": reply.get("model"),
            "reply": text,
        }
    text = _string_value(payload, "reply", "agent_reply")
    if not text:
        return None
    return {
        "type": "agent_reply",
        "source": _string_value(payload, "agent_source") or "pipecat_agent",
        "model": payload.get("model"),
        "reply": text,
    }


def _string_value(payload: dict[str, Any], *keys: str) -> str:
    for key in keys:
        value = payload.get(key)
        if value not in (None, ""):
            return str(value).strip()
    return ""
