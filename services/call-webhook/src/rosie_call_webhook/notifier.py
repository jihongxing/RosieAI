from __future__ import annotations

import json
import logging
from urllib import request
from urllib.error import URLError


logger = logging.getLogger(__name__)


def notify_wecom(webhook_url: str | None, text: str) -> None:
    if not webhook_url:
        return

    payload = json.dumps({"msgtype": "text", "text": {"content": text}}).encode("utf-8")
    req = request.Request(
        webhook_url,
        data=payload,
        headers={"Content-Type": "application/json"},
        method="POST",
    )
    try:
        request.urlopen(req, timeout=3).read()
    except URLError as exc:
        logger.warning("failed to send wecom notification: %s", exc)

