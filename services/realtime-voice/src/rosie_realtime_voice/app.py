from __future__ import annotations

from contextlib import asynccontextmanager
import json
import logging
import time
from typing import Any

from fastapi import FastAPI, WebSocket, WebSocketDisconnect

from .agent import RealtimeAgent
from .business_api import BusinessAPIClient
from .config import get_settings
from .pipecat_pipeline import PipecatTurnPipeline
from .pipeline import RealtimePipeline, RealtimeTurn
from .session import build_session_context, extract_customer_text
from .speech import create_stt_provider, create_tts_provider


logging.basicConfig(level=logging.INFO)
logger = logging.getLogger(__name__)

settings = get_settings()
agent = RealtimeAgent(
    ai_agent_url=settings.ai_agent_url,
    timeout_seconds=settings.agent_timeout_seconds,
    enabled=settings.agent_enabled,
)
business_api = BusinessAPIClient(
    base_url=settings.business_api_url,
    timeout_seconds=settings.business_api_timeout_seconds,
    enabled=settings.business_result_enabled,
)
if settings.pipeline_provider == "pipecat_http" and settings.pipecat_url:
    pipeline = PipecatTurnPipeline(
        url=settings.pipecat_url,
        timeout_seconds=settings.pipecat_timeout_seconds,
    )
else:
    pipeline = RealtimePipeline(
        stt=create_stt_provider(settings),
        agent=agent,
        tts=create_tts_provider(settings),
    )

sessions: dict[str, dict[str, Any]] = {}
business_result_retry_queue: dict[str, dict[str, Any]] = {}
runtime_state: dict[str, Any] = {
    "prewarm_enabled": settings.prewarm_enabled,
    "prewarmed": not settings.prewarm_enabled,
    "prewarm_ms": 0,
    "prewarm_error": "",
}


@asynccontextmanager
async def lifespan(app: FastAPI):
    await _prewarm()
    try:
        yield
    finally:
        close = getattr(pipeline, "close", None)
        if close:
            await close()
        await business_api.close()


async def _prewarm() -> None:
    if not settings.prewarm_enabled:
        return
    started = time.perf_counter()
    try:
        await pipeline.warmup()
    except Exception as exc:  # pragma: no cover - depends on optional model runtime
        runtime_state.update(
            {
                "prewarmed": False,
                "prewarm_ms": round((time.perf_counter() - started) * 1000),
                "prewarm_error": str(exc),
            }
        )
        logger.exception("realtime voice prewarm failed")
        return
    runtime_state.update(
        {
            "prewarmed": True,
            "prewarm_ms": round((time.perf_counter() - started) * 1000),
            "prewarm_error": "",
        }
    )
    logger.info("realtime voice prewarm completed in %sms", runtime_state["prewarm_ms"])


app = FastAPI(title="Rosie Realtime Voice MVP", version="0.1.0", lifespan=lifespan)


@app.get("/health")
def health() -> dict[str, str]:
    return {
        "status": "ok",
        "ready": str(bool(runtime_state["prewarmed"])).lower(),
        "prewarm_enabled": str(bool(runtime_state["prewarm_enabled"])).lower(),
        "prewarm_ms": str(runtime_state["prewarm_ms"]),
        "prewarm_error": str(runtime_state["prewarm_error"]),
        "business_result_enabled": str(settings.business_result_enabled).lower(),
        "business_auto_dispatch_enabled": str(settings.business_auto_dispatch_enabled).lower(),
        "business_api_configured": str(bool(settings.business_api_url)).lower(),
        "pipeline_provider": settings.pipeline_provider,
        "pipecat_configured": str(bool(settings.pipecat_url)).lower(),
        "agent_enabled": str(settings.agent_enabled).lower(),
        "agent_configured": str(bool(settings.ai_agent_url)).lower(),
        "stt_provider": settings.stt_provider,
        "tts_provider": settings.tts_provider,
        "stt_model": settings.stt_model if settings.stt_provider == "funasr" else "",
        "tts_voice": settings.tts_voice if settings.tts_provider == "edge" else "",
    }


@app.get("/sessions")
def list_sessions() -> dict[str, Any]:
    return {"items": list(sessions.values())}


@app.get("/business-result-retries")
def list_business_result_retries() -> dict[str, Any]:
    return {"items": list(business_result_retry_queue.values())}


@app.post("/internal/business-result-retries/flush")
async def flush_business_result_retries() -> dict[str, Any]:
    results: list[dict[str, Any]] = []
    for session_id, job in list(business_result_retry_queue.items()):
        if int(job.get("attempt_count") or 0) >= settings.business_result_retry_max_attempts:
            job["status"] = "exhausted"
            results.append(
                {
                    "session_id": session_id,
                    "status": "exhausted",
                    "attempt_count": job["attempt_count"],
                }
            )
            continue
        try:
            response = await business_api.post_realtime_call_result(job["payload"])
        except Exception as exc:  # pragma: no cover - network failures are environment dependent
            job["attempt_count"] = int(job.get("attempt_count") or 0) + 1
            job["last_error"] = str(exc)
            job["last_attempt_at"] = time.time()
            job["status"] = "failed"
            results.append(
                {
                    "session_id": session_id,
                    "status": "failed",
                    "attempt_count": job["attempt_count"],
                    "last_error": job["last_error"],
                }
            )
            continue

        business_result_retry_queue.pop(session_id, None)
        if session_id in sessions:
            sessions[session_id]["business_result_status"] = "sent"
            sessions[session_id]["business_result_error"] = ""
            sessions[session_id]["business_result_response"] = response or {}
            sessions[session_id]["business_result_retry_queued"] = False
            sessions[session_id]["updated_at"] = time.time()
            await _dispatch_business_notification(session_id, response or {})
        results.append(
            {
                "session_id": session_id,
                "status": "sent",
                "attempt_count": int(job.get("attempt_count") or 0) + 1,
            }
        )
    return {
        "status": "ok",
        "total": len(results),
        "remaining": len(business_result_retry_queue),
        "results": results,
    }


@app.websocket("/ws/jambonz/audio")
async def jambonz_audio(websocket: WebSocket) -> None:
    await websocket.accept()
    session_id = f"session-{int(time.time() * 1000)}"
    started_at = time.time()
    metadata: dict[str, Any] = {}
    history: list[dict[str, str]] = []
    pending_audio = bytearray()
    audio_bytes = 0
    audio_frames = 0

    try:
        first = await websocket.receive()
        if first.get("type") == "websocket.disconnect":
            return
        if "text" in first and first["text"]:
            try:
                metadata = json.loads(first["text"])
            except json.JSONDecodeError:
                metadata = {"raw": first["text"]}
        elif "bytes" in first and first["bytes"]:
            audio_bytes += len(first["bytes"])
            audio_frames += 1

        context = build_session_context(session_id, metadata)
        session_id = context.session_id
        sessions[session_id] = {
            "session_id": session_id,
            "metadata": metadata,
            "context": context.to_dict(),
            "audio_bytes": audio_bytes,
            "audio_frames": audio_frames,
            "transcript_turns": 0,
            "agent_turns": 0,
            "stt_turns": 0,
            "tts_turns": 0,
            "tts_audio_bytes": 0,
            "last_customer_text": "",
            "last_agent_reply": "",
            "agent_source": "",
            "stt_source": "",
            "tts_source": "",
            "last_turn_timings_ms": {},
            "turns": [],
            "business_result_status": "pending",
            "business_result_error": "",
            "business_result_response": {},
            "business_result_retry_queued": False,
            "business_dispatch_status": "pending",
            "business_dispatch_error": "",
            "business_dispatch_response": {},
            "started_at": started_at,
            "updated_at": time.time(),
            "status": "connected",
        }
        logger.info("jambonz audio websocket connected: %s", session_id)

        while True:
            message = await websocket.receive()
            if message.get("type") == "websocket.disconnect":
                break
            if "bytes" in message and message["bytes"] is not None:
                chunk = message["bytes"]
                audio_bytes += len(chunk)
                audio_frames += 1
                pending_audio.extend(chunk)
                sessions[session_id].update(
                    {
                        "audio_bytes": audio_bytes,
                        "audio_frames": audio_frames,
                        "updated_at": time.time(),
                    }
                )
                if len(pending_audio) >= settings.stt_min_audio_bytes:
                    turn = await pipeline.process_audio(context, bytes(pending_audio), history)
                    pending_audio.clear()
                    if turn:
                        await _send_turn(websocket, session_id, turn)
            elif "text" in message and message["text"]:
                logger.info("jambonz text frame for %s: %s", session_id, message["text"])
                try:
                    text_payload = json.loads(message["text"])
                except json.JSONDecodeError:
                    text_payload = {"text": message["text"]}
                if _should_flush_audio(text_payload) and pending_audio:
                    turn = await pipeline.process_audio(context, bytes(pending_audio), history)
                    pending_audio.clear()
                    if turn:
                        await _send_turn(websocket, session_id, turn)
                    continue
                customer_text = extract_customer_text(text_payload)
                if not customer_text:
                    continue
                turn = await pipeline.process_text(context, customer_text, history)
                if turn:
                    await _send_turn(websocket, session_id, turn)
    except WebSocketDisconnect:
        logger.info("jambonz audio websocket disconnected: %s", session_id)
    except RuntimeError as exc:
        # TestClient and some ASGI servers raise RuntimeError if receive() is
        # called after a disconnect frame has already been consumed.
        logger.info("jambonz audio websocket closed: %s (%s)", session_id, exc)
    finally:
        if session_id in sessions:
            sessions[session_id]["status"] = "disconnected"
            sessions[session_id]["updated_at"] = time.time()
            await _post_business_result(session_id)


async def _send_turn(websocket: WebSocket, session_id: str, turn: RealtimeTurn) -> None:
    session = sessions[session_id]
    reply_text = str((turn.reply or {}).get("reply") or "")
    agent_source = str((turn.reply or {}).get("source") or "")
    turn_record = {
        "customer_text": turn.transcript,
        "agent_reply": reply_text,
        "stt_source": turn.stt_source,
        "agent_source": agent_source,
        "tts_source": turn.tts_source,
        "timings_ms": turn.timings_ms or {},
    }
    session["turns"].append(turn_record)
    session.update(
        {
            "transcript_turns": session["transcript_turns"] + 1,
            "last_customer_text": turn.transcript,
            "stt_source": turn.stt_source,
            "last_turn_timings_ms": turn.timings_ms or {},
            "updated_at": time.time(),
        }
    )
    if turn.stt_source != "text_frame":
        session["stt_turns"] = session["stt_turns"] + 1
    if turn.reply:
        session.update(
            {
                "agent_turns": session["agent_turns"] + 1,
                "last_agent_reply": reply_text,
                "agent_source": agent_source,
                "updated_at": time.time(),
            }
        )
        await websocket.send_json(turn.reply)
    if turn.audio:
        session.update(
            {
                "tts_turns": session["tts_turns"] + 1,
                "tts_audio_bytes": session["tts_audio_bytes"] + len(turn.audio),
                "tts_source": turn.tts_source,
                "updated_at": time.time(),
            }
        )
        await websocket.send_bytes(turn.audio)


async def _post_business_result(session_id: str) -> None:
    session = sessions[session_id]
    if not settings.business_result_enabled or not settings.business_api_url:
        session["business_result_status"] = "disabled"
        return

    payload = _business_result_payload(session)
    if not payload["transcript"]:
        session["business_result_status"] = "skipped_empty_transcript"
        return

    try:
        response = await business_api.post_realtime_call_result(payload)
    except Exception as exc:  # pragma: no cover - network failures are environment dependent
        session["business_result_status"] = "failed"
        session["business_result_error"] = str(exc)
        _queue_business_result_retry(session_id, payload, exc)
        session["updated_at"] = time.time()
        return

    business_result_retry_queue.pop(session_id, None)
    session["business_result_status"] = "sent"
    session["business_result_error"] = ""
    session["business_result_response"] = response or {}
    session["business_result_retry_queued"] = False
    session["updated_at"] = time.time()
    await _dispatch_business_notification(session_id, response or {})


async def _dispatch_business_notification(session_id: str, result: dict[str, Any]) -> None:
    session = sessions[session_id]
    if not settings.business_auto_dispatch_enabled:
        session["business_dispatch_status"] = "disabled"
        return
    notification = result.get("realtime_notification") if isinstance(result, dict) else None
    if not isinstance(notification, dict):
        session["business_dispatch_status"] = "skipped_no_notification"
        return
    notification_status = _string_value(notification.get("status"))
    if notification_status != "queued":
        session["business_dispatch_status"] = "skipped_" + (notification_status or "unknown")
        return
    idempotency_key = _string_value(notification.get("idempotency_key"))
    if not idempotency_key:
        session["business_dispatch_status"] = "skipped_missing_idempotency_key"
        return
    try:
        response = await business_api.dispatch_notification(idempotency_key)
    except Exception as exc:  # pragma: no cover - network failures are environment dependent
        session["business_dispatch_status"] = "failed"
        session["business_dispatch_error"] = str(exc)
        session["updated_at"] = time.time()
        return

    session["business_dispatch_status"] = "sent"
    session["business_dispatch_error"] = ""
    session["business_dispatch_response"] = response or {}
    session["updated_at"] = time.time()


def _queue_business_result_retry(session_id: str, payload: dict[str, Any], exc: Exception) -> None:
    existing = business_result_retry_queue.get(session_id)
    now = time.time()
    if existing:
        existing["payload"] = payload
        existing["attempt_count"] = int(existing.get("attempt_count") or 0) + 1
        existing["last_error"] = str(exc)
        existing["updated_at"] = now
        existing["status"] = "failed"
    else:
        business_result_retry_queue[session_id] = {
            "session_id": session_id,
            "payload": payload,
            "attempt_count": 1,
            "last_error": str(exc),
            "created_at": now,
            "updated_at": now,
            "status": "failed",
        }
    if session_id in sessions:
        sessions[session_id]["business_result_retry_queued"] = True


def _business_result_payload(session: dict[str, Any]) -> dict[str, Any]:
    context = session.get("context") or {}
    metadata = session.get("metadata") or {}
    turns = session.get("turns") or []
    return {
        "call_sid": _string_value(context.get("call_sid")) or _string_value(session.get("session_id")),
        "call_id": _string_value(metadata.get("call_id") or metadata.get("callId")),
        "merchant_id": _string_value(context.get("merchant_id")),
        "from_number": _string_value(
            context.get("caller_number") or metadata.get("from_number") or metadata.get("from")
        ),
        "to_number": _string_value(context.get("access_number") or metadata.get("to_number") or metadata.get("to")),
        "transcript": _transcript_from_turns(turns),
        "turns": turns,
        "timings_ms": _aggregate_timings(turns),
        "metadata": {
            "session_id": session.get("session_id"),
            "audio_bytes": session.get("audio_bytes", 0),
            "audio_frames": session.get("audio_frames", 0),
            "tts_audio_bytes": session.get("tts_audio_bytes", 0),
            "pipeline_provider": settings.pipeline_provider,
            "stt_provider": settings.stt_provider,
            "tts_provider": settings.tts_provider,
            "raw": metadata,
            "context": context,
        },
    }


def _transcript_from_turns(turns: list[dict[str, Any]]) -> str:
    lines: list[str] = []
    for turn in turns:
        customer_text = _string_value(turn.get("customer_text"))
        agent_reply = _string_value(turn.get("agent_reply"))
        if customer_text:
            lines.append(f"客户：{customer_text}")
        if agent_reply:
            lines.append(f"Rosie：{agent_reply}")
    return "\n".join(lines)


def _aggregate_timings(turns: list[dict[str, Any]]) -> dict[str, int]:
    totals: dict[str, int] = {"turn_count": len(turns)}
    for turn in turns:
        timings = turn.get("timings_ms")
        if not isinstance(timings, dict):
            continue
        for key, value in timings.items():
            if not isinstance(value, int):
                continue
            totals[key] = totals.get(key, 0) + value
    return totals


def _string_value(value: Any) -> str:
    if value in (None, ""):
        return ""
    return str(value)


def _should_flush_audio(payload: dict[str, Any]) -> bool:
    event = str(payload.get("event") or payload.get("type") or "").lower()
    return event in {"end_of_utterance", "flush_audio", "stt_flush"}
