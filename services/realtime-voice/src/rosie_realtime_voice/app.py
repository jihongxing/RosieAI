from __future__ import annotations

import json
import logging
import time
from typing import Any

from fastapi import FastAPI, WebSocket, WebSocketDisconnect


logging.basicConfig(level=logging.INFO)
logger = logging.getLogger(__name__)

app = FastAPI(title="Rosie Realtime Voice MVP", version="0.1.0")

sessions: dict[str, dict[str, Any]] = {}


@app.get("/health")
def health() -> dict[str, str]:
    return {"status": "ok"}


@app.get("/sessions")
def list_sessions() -> dict[str, Any]:
    return {"items": list(sessions.values())}


@app.websocket("/ws/jambonz/audio")
async def jambonz_audio(websocket: WebSocket) -> None:
    await websocket.accept()
    session_id = f"session-{int(time.time() * 1000)}"
    started_at = time.time()
    metadata: dict[str, Any] = {}
    audio_bytes = 0
    audio_frames = 0

    try:
        first = await websocket.receive()
        if first.get("type") == "websocket.disconnect":
            return
        if "text" in first and first["text"]:
            try:
                metadata = json.loads(first["text"])
                session_id = str(metadata.get("callSid") or metadata.get("call_sid") or session_id)
            except json.JSONDecodeError:
                metadata = {"raw": first["text"]}
        elif "bytes" in first and first["bytes"]:
            audio_bytes += len(first["bytes"])
            audio_frames += 1

        sessions[session_id] = {
            "session_id": session_id,
            "metadata": metadata,
            "audio_bytes": audio_bytes,
            "audio_frames": audio_frames,
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
                audio_bytes += len(message["bytes"])
                audio_frames += 1
                sessions[session_id].update(
                    {
                        "audio_bytes": audio_bytes,
                        "audio_frames": audio_frames,
                        "updated_at": time.time(),
                    }
                )
            elif "text" in message and message["text"]:
                logger.info("jambonz text frame for %s: %s", session_id, message["text"])
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
