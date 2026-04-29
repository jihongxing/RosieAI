from __future__ import annotations

from contextlib import asynccontextmanager
import json
import logging
import re
from typing import Any

from fastapi import FastAPI, HTTPException, Query, Request
from pydantic import BaseModel, Field

from .ai_agent import extract_call_summary, generate_greeting
from .config import get_settings
from .db import Database
from .jambonz import (
    extract_call,
    realtime_listen_verbs,
    unknown_number_verbs,
    welcome_text_verbs,
    welcome_verbs,
)
from .notifier import notify_wecom
from .summary import build_digest_text, fallback_summary, inbox_status, parse_summary_result


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


class SimulatedCallResultIn(BaseModel):
    call_sid: str = Field(min_length=1)
    from_number: str = Field(default="")
    to_number: str = Field(default="")
    transcript: str = Field(min_length=1)


class NotificationPreferencesIn(BaseModel):
    digest_mode: str = Field(default="daily")
    digest_times: list[str] = Field(default_factory=lambda: ["20:00"])
    realtime_enabled: bool = False
    urgent_realtime_enabled: bool = True
    team_wecom_enabled: bool = False
    sms_fallback_enabled: bool = False
    quiet_hours_start: str | None = None
    quiet_hours_end: str | None = None


async def _json_payload(request: Request) -> dict[str, Any]:
    try:
        payload = await request.json()
    except json.JSONDecodeError:
        payload = {}
    if not isinstance(payload, dict):
        return {}
    return payload


def _normalize_notification_preferences(row: dict[str, Any]) -> dict[str, Any]:
    return {
        "merchant_id": row["merchant_id"],
        "digest_mode": row["digest_mode"],
        "digest_times": json.loads(row["digest_times"]),
        "realtime_enabled": bool(row["realtime_enabled"]),
        "urgent_realtime_enabled": bool(row["urgent_realtime_enabled"]),
        "team_wecom_enabled": bool(row["team_wecom_enabled"]),
        "sms_fallback_enabled": bool(row["sms_fallback_enabled"]),
        "quiet_hours_start": row.get("quiet_hours_start"),
        "quiet_hours_end": row.get("quiet_hours_end"),
        "created_at": row["created_at"],
        "updated_at": row["updated_at"],
    }


def _validate_notification_preferences(preferences: NotificationPreferencesIn) -> None:
    if preferences.digest_mode not in {"daily", "twice_daily", "hourly", "manual"}:
        raise HTTPException(status_code=400, detail="invalid digest_mode")

    if not preferences.digest_times:
        raise HTTPException(status_code=400, detail="digest_times cannot be empty")

    time_pattern = re.compile(r"^\d{2}:\d{2}$")
    for item in preferences.digest_times:
        if not time_pattern.match(item):
            raise HTTPException(status_code=400, detail=f"invalid digest time: {item}")
        hour, minute = item.split(":")
        if int(hour) > 23 or int(minute) > 59:
            raise HTTPException(status_code=400, detail=f"invalid digest time: {item}")

    for item in (preferences.quiet_hours_start, preferences.quiet_hours_end):
        if item is not None and not time_pattern.match(item):
            raise HTTPException(status_code=400, detail=f"invalid quiet hour: {item}")


def _inbox_title(summary: dict[str, Any]) -> str:
    intent = summary.get("intent")
    if intent == "spam":
        return "疑似骚扰来电"
    if intent == "appointment":
        return "预约意向"
    if intent == "urgent":
        return "紧急事项"
    return "有效来电"


async def _store_call_result(
    call_sid: str,
    merchant: dict[str, Any],
    transcript: str,
    source: str,
) -> dict[str, Any]:
    db.insert_transcript(
        {
            "call_sid": call_sid,
            "merchant_id": merchant["merchant_id"],
            "transcript": transcript,
            "source": source,
        }
    )

    raw_extract = None
    if settings.use_ai_extract:
        raw_extract = await extract_call_summary(
            settings.ai_agent_url,
            merchant["merchant_name"],
            transcript,
            settings.ai_timeout_seconds,
        )

    summary = parse_summary_result(raw_extract, transcript)
    summary["call_sid"] = call_sid
    summary["merchant_id"] = merchant["merchant_id"]
    summary["raw_result"] = raw_extract or json.dumps(fallback_summary(transcript), ensure_ascii=False)
    summary_id = db.insert_summary(summary)

    title = _inbox_title(summary)
    status = inbox_status(summary)
    inbox_id = db.insert_inbox_item(
        {
            "merchant_id": merchant["merchant_id"],
            "call_sid": call_sid,
            "title": title,
            "body": summary.get("summary") or transcript[:120],
            "priority": summary.get("priority", "normal"),
            "status": status,
            "need_human_followup": summary.get("need_human_followup", False),
        }
    )

    return {
        "summary_id": summary_id,
        "inbox_item_id": inbox_id,
        "summary": summary,
        "inbox": {
            "title": title,
            "status": status,
            "priority": summary.get("priority", "normal"),
            "need_human_followup": summary.get("need_human_followup", False),
        },
    }


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


@app.get("/notification-preferences")
def get_notification_preferences(merchant_id: str | None = None) -> dict[str, Any]:
    target_merchant_id = merchant_id or settings.default_merchant_id
    merchant = db.find_merchant_by_id(target_merchant_id)
    if not merchant:
        raise HTTPException(status_code=404, detail="unknown merchant")
    row = db.ensure_notification_preferences(target_merchant_id)
    return _normalize_notification_preferences(row)


@app.put("/notification-preferences")
def update_notification_preferences(
    preferences: NotificationPreferencesIn,
    merchant_id: str | None = None,
) -> dict[str, Any]:
    target_merchant_id = merchant_id or settings.default_merchant_id
    merchant = db.find_merchant_by_id(target_merchant_id)
    if not merchant:
        raise HTTPException(status_code=404, detail="unknown merchant")

    _validate_notification_preferences(preferences)
    row = db.update_notification_preferences(
        target_merchant_id,
        {
            **preferences.model_dump(),
            "digest_times": json.dumps(preferences.digest_times),
        },
    )
    return _normalize_notification_preferences(row)


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

    reply = None
    if settings.use_ai_greeting:
        reply = await generate_greeting(
            settings.ai_agent_url,
            merchant["merchant_name"],
            call["from_number"],
            settings.ai_timeout_seconds,
        )

    if settings.realtime_listen_enabled and settings.realtime_ws_url:
        greeting = reply or (
            f"您好，我是{merchant['merchant_name']}的 AI 前台 Rosie。"
            "机主现在不方便接电话，请您直接说来电事项。"
        )
        return realtime_listen_verbs(
            greeting,
            settings.realtime_ws_url,
            settings.realtime_action_hook,
            metadata={
                "merchant_id": merchant["merchant_id"],
                "merchant_name": merchant["merchant_name"],
                "caller_number": call["from_number"],
                "access_number": call["to_number"],
                "call_sid": call["call_sid"],
            },
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


@app.post("/webhooks/jambonz/listen-complete")
async def listen_complete(request: Request) -> dict[str, str]:
    payload = await _json_payload(request)
    call = extract_call(payload)
    db.insert_event(
        {
            "call_sid": call["call_sid"],
            "event_type": "jambonz_listen_complete",
            "call_status": call["call_status"],
            "raw_payload": json.dumps(payload, ensure_ascii=False),
        }
    )
    return {"status": "ok"}


@app.get("/calls")
def list_calls(limit: int = Query(default=50, ge=1, le=200)) -> dict[str, Any]:
    return {"items": db.list_calls(limit=limit)}


@app.post("/simulate/call-result")
async def simulate_call_result(payload: SimulatedCallResultIn) -> dict[str, Any]:
    to_number = payload.to_number or settings.default_access_number
    merchant = db.find_merchant_by_access_number(to_number)
    if not merchant:
        raise HTTPException(status_code=404, detail="unknown access number")

    call = {
        "call_sid": payload.call_sid,
        "call_id": f"sim-{payload.call_sid}",
        "merchant_id": merchant["merchant_id"],
        "from_number": payload.from_number,
        "to_number": to_number,
        "call_status": "completed",
        "direction": "inbound",
        "raw_payload": json.dumps(payload.model_dump(), ensure_ascii=False),
    }
    db.insert_call(call)
    result = await _store_call_result(payload.call_sid, merchant, payload.transcript, "simulated")
    return {"status": "ok", "call_sid": payload.call_sid, **result}


@app.get("/inbox")
def list_inbox(
    merchant_id: str | None = None,
    limit: int = Query(default=50, ge=1, le=200),
) -> dict[str, Any]:
    target_merchant_id = merchant_id or settings.default_merchant_id
    return {"items": db.list_inbox_items(target_merchant_id, limit=limit)}


@app.get("/digests/preview")
def digest_preview(
    merchant_id: str | None = None,
    limit: int = Query(default=100, ge=1, le=500),
) -> dict[str, Any]:
    target_merchant_id = merchant_id or settings.default_merchant_id
    items = db.list_pending_digest_items(target_merchant_id, limit=limit)
    urgent_count = sum(1 for item in items if item["priority"] == "urgent")
    followup_count = sum(1 for item in items if item["need_human_followup"])
    spam_count = sum(1 for item in items if item["status"] == "filtered")
    return {
        "merchant_id": target_merchant_id,
        "total": len(items),
        "urgent_count": urgent_count,
        "followup_count": followup_count,
        "spam_count": spam_count,
        "digest_text": build_digest_text(items),
        "items": items,
    }


@app.post("/digests/generate")
def generate_digest(
    merchant_id: str | None = None,
    digest_type: str = "daily",
    limit: int = Query(default=100, ge=1, le=500),
) -> dict[str, Any]:
    target_merchant_id = merchant_id or settings.default_merchant_id
    items = db.list_pending_digest_items(target_merchant_id, limit=limit)
    urgent_count = sum(1 for item in items if item["priority"] == "urgent")
    followup_count = sum(1 for item in items if item["need_human_followup"])
    spam_count = sum(1 for item in items if item["status"] == "filtered")
    item_ids = [int(item["id"]) for item in items]
    digest_text = build_digest_text(items)
    digest_id = db.insert_digest(
        {
            "merchant_id": target_merchant_id,
            "digest_type": digest_type,
            "item_count": len(items),
            "urgent_count": urgent_count,
            "followup_count": followup_count,
            "spam_count": spam_count,
            "digest_text": digest_text,
            "item_ids": json.dumps(item_ids),
        }
    )
    db.mark_inbox_items_digested(item_ids)
    return {
        "status": "ok",
        "digest_id": digest_id,
        "merchant_id": target_merchant_id,
        "digest_type": digest_type,
        "total": len(items),
        "urgent_count": urgent_count,
        "followup_count": followup_count,
        "spam_count": spam_count,
        "digest_text": digest_text,
        "item_ids": item_ids,
    }


@app.get("/digests")
def list_digests(
    merchant_id: str | None = None,
    limit: int = Query(default=20, ge=1, le=100),
) -> dict[str, Any]:
    target_merchant_id = merchant_id or settings.default_merchant_id
    return {"items": db.list_digests(target_merchant_id, limit=limit)}
