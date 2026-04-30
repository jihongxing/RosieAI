from __future__ import annotations

import logging
from typing import Any

import httpx


logger = logging.getLogger(__name__)


class BusinessAPIClient:
    def __init__(
        self,
        base_url: str | None,
        timeout_seconds: float,
        enabled: bool = True,
    ) -> None:
        self.base_url = base_url.rstrip("/") if base_url else None
        self.timeout_seconds = timeout_seconds
        self.enabled = enabled
        self._client: httpx.AsyncClient | None = None

    async def post_realtime_call_result(self, payload: dict[str, Any]) -> dict[str, Any] | None:
        if not self.enabled or not self.base_url:
            return None
        try:
            response = await self._http_client().post(
                f"{self.base_url}/internal/realtime-call-result",
                json=payload,
            )
            response.raise_for_status()
            data = response.json()
        except httpx.HTTPError as exc:
            logger.warning("business api realtime call result failed: %s", exc)
            raise

        return data if isinstance(data, dict) else {}

    async def dispatch_notification(self, idempotency_key: str, status: str = "queued") -> dict[str, Any] | None:
        if not self.enabled or not self.base_url or not idempotency_key:
            return None
        try:
            response = await self._http_client().post(
                f"{self.base_url}/internal/notifications/dispatch",
                params={"idempotency_key": idempotency_key, "status": status},
            )
            response.raise_for_status()
            data = response.json()
        except httpx.HTTPError as exc:
            logger.warning("business api notification dispatch failed: %s", exc)
            raise

        return data if isinstance(data, dict) else {}

    async def close(self) -> None:
        if self._client:
            await self._client.aclose()
            self._client = None

    def _http_client(self) -> httpx.AsyncClient:
        if self._client is None:
            self._client = httpx.AsyncClient(timeout=httpx.Timeout(self.timeout_seconds))
        return self._client
