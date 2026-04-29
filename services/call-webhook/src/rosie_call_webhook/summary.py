from __future__ import annotations

import json
import re
from typing import Any


SPAM_KEYWORDS = ("贷款", "pos", "POS", "发票", "代开", "推广", "营销", "信用卡")
APPOINTMENT_KEYWORDS = ("预约", "约", "明天", "今天", "下午", "上午", "晚上", "几点")
URGENT_KEYWORDS = ("投诉", "紧急", "急", "马上", "尽快", "严重")


def fallback_summary(transcript: str) -> dict[str, Any]:
    text = transcript.strip()
    lower_text = text.lower()
    is_spam = any(keyword.lower() in lower_text for keyword in SPAM_KEYWORDS)
    is_urgent = any(keyword in text for keyword in URGENT_KEYWORDS)
    is_appointment = any(keyword in text for keyword in APPOINTMENT_KEYWORDS)

    if is_spam:
        intent = "spam"
        priority = "low"
        need_human_followup = False
    elif is_urgent:
        intent = "urgent"
        priority = "urgent"
        need_human_followup = True
    elif is_appointment:
        intent = "appointment"
        priority = "high"
        need_human_followup = True
    else:
        intent = "inquiry"
        priority = "normal"
        need_human_followup = True

    return {
        "summary": text[:120] or None,
        "customer_name": None,
        "customer_phone": _extract_phone(text),
        "intent": intent,
        "appointment_time": _extract_time_hint(text),
        "service": None,
        "priority": priority,
        "need_human_followup": need_human_followup,
    }


def parse_summary_result(result: str | None, transcript: str) -> dict[str, Any]:
    if not result:
        return fallback_summary(transcript)

    try:
        data = json.loads(result)
    except json.JSONDecodeError:
        return fallback_summary(transcript)

    if not isinstance(data, dict):
        return fallback_summary(transcript)

    fallback = fallback_summary(transcript)
    return {
        "summary": data.get("summary") or fallback["summary"],
        "customer_name": data.get("customer_name"),
        "customer_phone": data.get("customer_phone") or fallback["customer_phone"],
        "intent": data.get("intent") or fallback["intent"],
        "appointment_time": data.get("appointment_time") or fallback["appointment_time"],
        "service": data.get("service"),
        "priority": data.get("priority") or fallback["priority"],
        "need_human_followup": bool(data.get("need_human_followup", fallback["need_human_followup"])),
    }


def inbox_status(summary: dict[str, Any]) -> str:
    if summary.get("intent") == "spam":
        return "filtered"
    if summary.get("need_human_followup"):
        return "needs_review"
    return "archived"


def _extract_phone(text: str) -> str | None:
    match = re.search(r"1[3-9]\d{9}", text)
    return match.group(0) if match else None


def _extract_time_hint(text: str) -> str | None:
    match = re.search(r"(今天|明天|后天)?\s*(上午|下午|晚上)?\s*\d{1,2}\s*[点:：]\s*(\d{1,2}分?)?", text)
    return match.group(0).strip() if match else None
