from __future__ import annotations

import re
from typing import Any


def normalize_number(value: Any) -> str:
    if value is None:
        return ""
    text = str(value).strip()
    if not text:
        return ""
    if text.startswith("+"):
        return "+" + re.sub(r"\D", "", text[1:])
    return re.sub(r"\D", "", text)


def get_payload_value(payload: dict[str, Any], *names: str) -> Any:
    for name in names:
        if name in payload and payload[name] not in (None, ""):
            return payload[name]
    return None


def extract_call(payload: dict[str, Any]) -> dict[str, str]:
    from_number = normalize_number(get_payload_value(payload, "from", "caller", "callerId"))
    to_number = normalize_number(get_payload_value(payload, "to", "called", "destination"))
    return {
        "call_sid": str(get_payload_value(payload, "callSid", "call_sid", "sid") or ""),
        "call_id": str(get_payload_value(payload, "callId", "sipCallId", "call_id") or ""),
        "from_number": from_number,
        "to_number": to_number,
        "call_status": str(get_payload_value(payload, "callStatus", "call_status", "sipStatus") or ""),
        "direction": str(get_payload_value(payload, "direction") or "inbound"),
    }


def say(text: str) -> dict[str, Any]:
    return {"verb": "say", "text": text}


def pause(length: int = 1) -> dict[str, Any]:
    return {"verb": "pause", "length": length}


def hangup() -> dict[str, str]:
    return {"verb": "hangup"}


def listen(
    url: str,
    action_hook: str | None = None,
    metadata: dict[str, Any] | None = None,
) -> dict[str, Any]:
    verb: dict[str, Any] = {
        "verb": "listen",
        "url": url,
        "sampleRate": 16000,
        "mixType": "mono",
        "playBeep": False,
        "timeout": 30,
        "maxLength": 300,
        "bidirectionalAudio": {
            "enabled": True,
            "streaming": True,
            "sampleRate": 16000,
        },
    }
    if action_hook:
        verb["actionHook"] = action_hook
    if metadata:
        verb["metadata"] = metadata
    return verb


def welcome_verbs(merchant_name: str) -> list[dict[str, Any]]:
    return welcome_text_verbs(
        f"您好，这里是{merchant_name}的 AI 接线员 Rosie。"
        "机主现在不方便接电话，我已经帮您记录本次来电。"
        "这是最小验证版本，稍后会将来电信息通知机主。"
    )


def welcome_text_verbs(text: str) -> list[dict[str, Any]]:
    return [
        say(text),
        pause(1),
        hangup(),
    ]


def realtime_listen_verbs(
    greeting_text: str,
    websocket_url: str,
    action_hook: str | None,
    metadata: dict[str, Any],
) -> list[dict[str, Any]]:
    return [
        say(greeting_text),
        listen(websocket_url, action_hook=action_hook, metadata=metadata),
        hangup(),
    ]


def unknown_number_verbs() -> list[dict[str, Any]]:
    return [
        say("您好，Rosie 暂时无法识别这个接入号码，请稍后再试。"),
        pause(1),
        hangup(),
    ]
