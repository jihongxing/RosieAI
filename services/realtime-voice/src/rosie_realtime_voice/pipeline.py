from __future__ import annotations

from dataclasses import dataclass
import time
from typing import Any

from .agent import RealtimeAgent
from .session import RealtimeSessionContext
from .speech import SpeechToText, TextToSpeech


@dataclass
class RealtimeTurn:
    transcript: str
    reply: dict[str, Any] | None
    audio: bytes | None
    tts_content_type: str = ""
    stt_source: str = ""
    tts_source: str = ""
    timings_ms: dict[str, int] | None = None


class RealtimePipeline:
    def __init__(
        self,
        stt: SpeechToText,
        agent: RealtimeAgent,
        tts: TextToSpeech,
    ) -> None:
        self.stt = stt
        self.agent = agent
        self.tts = tts

    async def warmup(self) -> None:
        await self.stt.warmup()
        warmup = getattr(self.agent, "warmup", None)
        if warmup:
            await warmup()
        await self.tts.warmup()

    async def close(self) -> None:
        close = getattr(self.agent, "close", None)
        if close:
            await close()

    async def process_audio(
        self,
        context: RealtimeSessionContext,
        audio: bytes,
        history: list[dict[str, str]],
    ) -> RealtimeTurn | None:
        started = time.perf_counter()
        stt_started = time.perf_counter()
        stt_result = await self.stt.transcribe(context, audio)
        stt_ms = _elapsed_ms(stt_started)
        if not stt_result or not stt_result.is_final:
            return None
        turn = await self.process_text(
            context,
            stt_result.transcript,
            history,
            stt_source=stt_result.source,
            inherited_timings_ms={"stt_ms": stt_ms},
        )
        if turn:
            timings = turn.timings_ms or {}
            timings["total_ms"] = _elapsed_ms(started)
            turn.timings_ms = timings
        return turn

    async def process_text(
        self,
        context: RealtimeSessionContext,
        customer_text: str,
        history: list[dict[str, str]],
        stt_source: str = "text_frame",
        inherited_timings_ms: dict[str, int] | None = None,
    ) -> RealtimeTurn | None:
        if not customer_text:
            return None
        started = time.perf_counter()
        timings = dict(inherited_timings_ms or {})
        history.append({"role": "user", "content": customer_text})
        agent_started = time.perf_counter()
        reply = await self.agent.reply(context, customer_text, history)
        timings["agent_ms"] = _elapsed_ms(agent_started)
        if not reply:
            timings["total_ms"] = _elapsed_ms(started)
            return RealtimeTurn(
                transcript=customer_text,
                reply=None,
                audio=None,
                stt_source=stt_source,
                timings_ms=timings,
            )
        history.append({"role": "assistant", "content": reply["reply"]})

        tts_started = time.perf_counter()
        tts_result = await self.tts.synthesize(context, reply["reply"])
        timings["tts_ms"] = _elapsed_ms(tts_started)
        timings["total_ms"] = _elapsed_ms(started)
        return RealtimeTurn(
            transcript=customer_text,
            reply=reply,
            audio=tts_result.audio if tts_result else None,
            tts_content_type=tts_result.content_type if tts_result else "",
            stt_source=stt_source,
            tts_source=tts_result.source if tts_result else "",
            timings_ms=timings,
        )


def _elapsed_ms(started: float) -> int:
    return round((time.perf_counter() - started) * 1000)
