from __future__ import annotations

from collections.abc import AsyncIterator
import json
import re
from typing import Any


class LocalFallbackClient:
    async def health(self) -> dict[str, Any]:
        return {
            "provider": "local_fallback",
            "model_loaded": True,
        }

    async def generate(self, model: str, prompt: str, system: str | None = None) -> str:
        if _is_extract_prompt(prompt):
            return json.dumps(_extract_summary(prompt), ensure_ascii=False)
        text = _extract_customer_text(prompt)
        if any(word in text for word in ("预约", "剪头发", "理发", "到店")):
            return "可以的，请问您想预约哪一天、几点到店？"
        if any(word in text for word in ("投诉", "生气", "急", "马上")):
            return "我已经记录为紧急事项，会提醒店主尽快处理。"
        if any(word in text for word in ("电话", "联系", "回电")):
            return "好的，请您留下姓名和联系电话，我帮您记录。"
        return "好的，我先帮您记录，请继续说。"

    async def stream_generate(self, model: str, prompt: str, system: str | None = None) -> AsyncIterator[str]:
        yield await self.generate(model, prompt, system)


def _extract_customer_text(prompt: str) -> str:
    marker = "客户刚才说："
    if marker not in prompt:
        return prompt
    for line in prompt.split(marker, 1)[1].splitlines():
        text = line.strip()
        if text:
            return text
    return ""


def _is_extract_prompt(prompt: str) -> bool:
    return "通话整理助手" in prompt and "字段包括" in prompt and "通话转写：" in prompt


def _extract_summary(prompt: str) -> dict[str, Any]:
    transcript = prompt.split("通话转写：", 1)[-1].strip()
    text = _customer_only_text(transcript) or transcript
    intent = "inquiry"
    priority = "normal"
    need_human_followup = True
    if any(word in transcript for word in ("贷款", "pos", "发票", "代开", "推广", "营销", "信用卡")):
        intent = "spam"
        priority = "low"
        need_human_followup = False
    elif any(word in transcript for word in ("投诉", "紧急", "急", "马上", "尽快", "严重")):
        intent = "urgent"
        priority = "urgent"
    elif any(word in transcript for word in ("预约", "约", "明天", "今天", "下午", "上午", "晚上", "几点")):
        intent = "appointment"
        priority = "high"
    return {
        "summary": text[:120],
        "customer_name": _first_match(r"(?:我姓|姓)([\u4e00-\u9fa5A-Za-z]{1,8})", transcript),
        "customer_phone": _first_match(r"1[3-9]\d{9}", transcript),
        "intent": intent,
        "appointment_time": _first_match(r"(?:今天|明天|后天)?\s*(?:上午|下午|晚上)?\s*(?:\d{1,2}|十一|十二|十|一|二|两|三|四|五|六|七|八|九)\s*(?:点|:|：)\s*(?:\d{1,2}分?|半)?", transcript),
        "service": _service_hint(transcript),
        "priority": priority,
        "need_human_followup": need_human_followup,
    }


def _customer_only_text(text: str) -> str:
    lines: list[str] = []
    for line in text.splitlines():
        line = line.strip()
        for prefix in ("客户：", "客户:", "Customer:", "customer:"):
            if line.startswith(prefix):
                lines.append(line.removeprefix(prefix).strip())
                break
    return "\n".join(lines)


def _first_match(pattern: str, text: str) -> str | None:
    match = re.search(pattern, text)
    if not match:
        return None
    if match.lastindex:
        return match.group(1).strip()
    return match.group(0).strip()


def _service_hint(text: str) -> str | None:
    for item in ("剪发", "剪头发", "理发", "烫染", "护理", "洗头", "染发", "烫发"):
        if item in text:
            return "剪发" if item in ("剪头发", "理发") else item
    return None
