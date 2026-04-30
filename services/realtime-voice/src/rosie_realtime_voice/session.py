from __future__ import annotations

from dataclasses import asdict, dataclass, field
from typing import Any


@dataclass
class RealtimeSessionContext:
    session_id: str
    call_sid: str = ""
    merchant_id: str = ""
    merchant_name: str = ""
    caller_number: str = ""
    access_number: str = ""
    system_prompt: str = ""
    merchant_profile: dict[str, Any] = field(default_factory=dict)
    industry_template: dict[str, Any] = field(default_factory=dict)
    sample_rate: int = 16000
    raw_metadata: dict[str, Any] = field(default_factory=dict)

    def to_dict(self) -> dict[str, Any]:
        return asdict(self)


def build_session_context(default_session_id: str, metadata: dict[str, Any]) -> RealtimeSessionContext:
    payload = _unwrap_metadata(metadata)
    call_sid = _string_value(payload, "callSid", "call_sid", "sid")
    sample_rate = _int_value(payload, 16000, "sampleRate", "sample_rate")
    return RealtimeSessionContext(
        session_id=call_sid or default_session_id,
        call_sid=call_sid,
        merchant_id=_string_value(payload, "merchant_id", "merchantId"),
        merchant_name=_string_value(payload, "merchant_name", "merchantName"),
        caller_number=_string_value(payload, "caller_number", "callerNumber", "from"),
        access_number=_string_value(payload, "access_number", "accessNumber", "to"),
        system_prompt=_string_value(payload, "system_prompt", "systemPrompt"),
        merchant_profile=_dict_value(payload, "merchant_profile", "merchantProfile"),
        industry_template=_dict_value(payload, "industry_template", "industryTemplate"),
        sample_rate=sample_rate,
        raw_metadata=payload,
    )


def extract_customer_text(message: dict[str, Any]) -> str:
    payload = _unwrap_metadata(message)
    return _string_value(
        payload,
        "customer_text",
        "customerText",
        "transcript",
        "text",
        "utterance",
    )


def _unwrap_metadata(value: dict[str, Any]) -> dict[str, Any]:
    nested = value.get("metadata")
    if isinstance(nested, dict):
        return {**value, **nested}
    return value


def _string_value(payload: dict[str, Any], *keys: str) -> str:
    for key in keys:
        value = payload.get(key)
        if value not in (None, ""):
            return str(value)
    return ""


def _dict_value(payload: dict[str, Any], *keys: str) -> dict[str, Any]:
    for key in keys:
        value = payload.get(key)
        if isinstance(value, dict):
            return value
    return {}


def _int_value(payload: dict[str, Any], fallback: int, *keys: str) -> int:
    for key in keys:
        value = payload.get(key)
        if value in (None, ""):
            continue
        try:
            return int(value)
        except (TypeError, ValueError):
            return fallback
    return fallback
