from __future__ import annotations

import logging
from typing import Any

import httpx


logger = logging.getLogger(__name__)


async def fetch_merchant_ai_context(
    business_api_url: str | None,
    merchant_id: str,
    timeout_seconds: float,
) -> dict[str, Any] | None:
    if not business_api_url or not merchant_id:
        return None

    try:
        async with httpx.AsyncClient(timeout=httpx.Timeout(timeout_seconds)) as client:
            response = await client.get(
                f"{business_api_url.rstrip('/')}/merchant-profile",
                params={"merchant_id": merchant_id},
            )
            response.raise_for_status()
            data = response.json()
    except httpx.HTTPError as exc:
        logger.warning("business api merchant profile failed: %s", exc)
        return None

    if not isinstance(data, dict):
        return None
    return {
        "merchant": data.get("merchant") or {},
        "profile": data.get("profile") or {},
        "template": data.get("template") or {},
        "system_prompt": str(data.get("system_prompt") or "").strip(),
    }
