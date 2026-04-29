from __future__ import annotations

from contextlib import asynccontextmanager
import json
import logging
from typing import Any

from fastapi import FastAPI, Query, Request
from pydantic import BaseModel, Field

from .ai_agent import generate_greeting
from .config import get_settings
from .db import Database
from .jambonz import extract_call, unknown_number_verbs, welcome_text_verbs, welcome_verbs
from .notifier import notify_wecom


logging.basicConfig(level=logging.INFO)
logger = logging.getLogger(__name__)

settings = get_settings()
db = Database(settings.db_path)


@asynccontextmanager
async def lifespan(app: FastAPI):
    db.init()
    db.upsert_default_merchant(
        merchant_id=settings.default_merchant_id,
        merchant_name=settings.default_merchant_name,
        access_number=settings.default_access_number,
        transfer_phone=settings.default_transfer_phone,
    )
    logger.info("rosie call webhook started with db=%s", settings.db_path)
    yield


app = FastAPI(title="Rosie Call Webhook MVP", version="0.1.0", lifespan=lifespan)


class MerchantIn(BaseModel):
    merchant_id: str = Field(min_length=1)
    merchant_name: str = Field(min_length=1)
    access_number: str = Field(min_length=1)
    original_number: str | None = None
    transfer_phone: str | None = None
    enabled: bool = True


async def _json_payload(request: Request) -> dict[str, Any]:
    try:
        payload = await request.json()
    except json.JSONDecodeError:
        payload = {}
    if not isinstance(payload, dict):
        return {}
    return payload


@app.get("/health")
def health() -> dict[str, str]:
    return {"status": "ok"}


@app.get("/merchants")
def list_merchants() -> dict[str, Any]:
    return {"items": db.list_merchants()}


@app.post("/merchants")
def upsert_merchant(merchant: MerchantIn) -> dict[str, str]:
    db.upsert_merchant(merchant.model_dump())
    return {"status": "ok", "merchant_id": merchant.merchant_id}


@app.post("/webhooks/jambonz/call")
async def inbound_call(request: Request) -> list[dict[str, Any]]:
    payload = await _json_payload(request)
    call = extract_call(payload)
    merchant = db.find_merchant_by_access_number(call["to_number"])

    call["merchant_id"] = merchant["merchant_id"] if merchant else None
    call["raw_payload"] = json.dumps(payload, ensure_ascii=False)
    db.insert_call(call)

    if not merchant:
        logger.warning("unknown access number: %s", call["to_number"])
        return unknown_number_verbs()

    notify_wecom(
        settings.wecom_webhook_url,
        (
            f"Rosie MVP 收到来电\n"
            f"商家：{merchant['merchant_name']}\n"
            f"主叫：{call['from_number'] or '未知'}\n"
            f"幕后接入号：{call['to_number'] or '未知'}"
        ),
    )

    if settings.use_ai_greeting:
        reply = await generate_greeting(
            settings.ai_agent_url,
            merchant["merchant_name"],
            call["from_number"],
            settings.ai_timeout_seconds,
        )
        if reply:
            return welcome_text_verbs(reply)

    return welcome_verbs(merchant["merchant_name"])


@app.post("/webhooks/jambonz/status")
async def call_status(request: Request) -> dict[str, str]:
    payload = await _json_payload(request)
    call = extract_call(payload)
    db.insert_event(
        {
            "call_sid": call["call_sid"],
            "event_type": "jambonz_status",
            "call_status": call["call_status"],
            "raw_payload": json.dumps(payload, ensure_ascii=False),
        }
    )
    return {"status": "ok"}


@app.get("/calls")
def list_calls(limit: int = Query(default=50, ge=1, le=200)) -> dict[str, Any]:
    return {"items": db.list_calls(limit=limit)}
