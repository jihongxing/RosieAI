import os
import tempfile
from dataclasses import replace

os.environ["ROSIE_DB_PATH"] = tempfile.NamedTemporaryFile(delete=True).name
os.environ["ROSIE_DEFAULT_ACCESS_NUMBER"] = "8613736849910"
os.environ["ROSIE_DEFAULT_MERCHANT_NAME"] = "测试理发店"
os.environ["ROSIE_BUSINESS_API_URL"] = ""
os.environ["ROSIE_USE_AI_GREETING"] = "false"
os.environ["ROSIE_USE_AI_EXTRACT"] = "false"
os.environ["ROSIE_REALTIME_LISTEN_ENABLED"] = "false"

from fastapi.testclient import TestClient  # noqa: E402
import pytest  # noqa: E402

from rosie_call_webhook.app import app  # noqa: E402
import rosie_call_webhook.app as app_module  # noqa: E402


def test_inbound_call_returns_jambonz_verbs():
    with TestClient(app) as client:
        response = client.post(
            "/webhooks/jambonz/call",
            json={
                "callSid": "call-1",
                "callId": "sip-call-1",
                "from": "+8613811112222",
                "to": "8613736849910",
                "direction": "inbound",
                "callStatus": "trying",
            },
        )

        assert response.status_code == 200
        body = response.json()
        assert body[0]["verb"] == "say"
        assert "测试理发店" in body[0]["text"]
        assert body[-1]["verb"] == "hangup"


def test_calls_are_persisted():
    with TestClient(app) as client:
        client.post(
            "/webhooks/jambonz/call",
            json={
                "callSid": "call-2",
                "from": "+8613811112222",
                "to": "8613736849910",
                "callStatus": "trying",
            },
        )
        response = client.get("/calls")

        assert response.status_code == 200
        assert response.json()["items"][0]["call_sid"] == "call-2"


def test_can_add_merchant_and_route_by_access_number():
    with TestClient(app) as client:
        merchant = {
            "merchant_id": "merchant-001",
            "merchant_name": "张三理发店",
            "access_number": "+8617000000001",
            "original_number": "+8613812345678",
            "transfer_phone": "+8613812345678",
            "enabled": True,
        }
        response = client.post("/merchants", json=merchant)
        assert response.status_code == 200

        response = client.post(
            "/webhooks/jambonz/call",
            json={
                "callSid": "call-3",
                "from": "+8613811112222",
                "to": "+8617000000001",
                "callStatus": "trying",
            },
        )

        assert response.status_code == 200
        body = response.json()
        assert "张三理发店" in body[0]["text"]


def test_access_number_matches_with_or_without_plus_prefix():
    with TestClient(app) as client:
        merchant = {
            "merchant_id": "merchant-002",
            "merchant_name": "李四花店",
            "access_number": "+8613736849911",
            "enabled": True,
        }
        response = client.post("/merchants", json=merchant)
        assert response.status_code == 200

        response = client.post(
            "/webhooks/jambonz/call",
            json={
                "callSid": "call-4",
                "from": "+8613811112222",
                "to": "8613736849911",
                "callStatus": "trying",
            },
        )

        assert response.status_code == 200
        body = response.json()
        assert "李四花店" in body[0]["text"]


def test_realtime_listen_metadata_carries_system_prompt(monkeypatch: pytest.MonkeyPatch):
    async def fake_context(merchant):
        return {
            "merchant": merchant,
            "profile": {"industry": "hair_salon"},
            "template": {"key": "hair_salon"},
            "system_prompt": "服务项目：剪发、烫染、护理",
    }

    monkeypatch.setattr(app_module, "_merchant_ai_context", fake_context)
    monkeypatch.setattr(
        app_module,
        "settings",
        replace(
            app_module.settings,
            realtime_listen_enabled=True,
            realtime_ws_url="ws://127.0.0.1:8020/ws/jambonz/audio",
            realtime_action_hook="http://127.0.0.1:8000/listen-complete",
        ),
    )

    with TestClient(app) as client:
        response = client.post(
            "/webhooks/jambonz/call",
            json={
                "callSid": "call-realtime-metadata",
                "from": "+8613811112222",
                "to": "8613736849910",
                "callStatus": "trying",
            },
        )

    assert response.status_code == 200
    body = response.json()
    listen_verb = next(item for item in body if item["verb"] == "listen")
    metadata = listen_verb["metadata"]
    assert metadata["system_prompt"] == "服务项目：剪发、烫染、护理"
    assert metadata["merchant_profile"]["industry"] == "hair_salon"
    assert metadata["industry_template"]["key"] == "hair_salon"


def test_simulated_call_result_creates_inbox_item():
    with TestClient(app) as client:
        response = client.post(
            "/simulate/call-result",
            json={
                "call_sid": "sim-call-1",
                "from_number": "+8613811112222",
                "to_number": "8613736849910",
                "transcript": "你好，我想预约明天下午三点剪头发，我姓王。",
            },
        )

        assert response.status_code == 200
        body = response.json()
        assert body["summary"]["intent"] == "appointment"
        assert body["summary"]["appointment_time"] == "明天下午三点"
        assert body["inbox"]["status"] == "needs_review"

        inbox_response = client.get("/inbox")
        assert inbox_response.status_code == 200
        items = inbox_response.json()["items"]
        assert items[0]["call_sid"] == "sim-call-1"
        assert items[0]["title"] == "预约意向"


def test_digest_preview_counts_pending_items():
    with TestClient(app) as client:
        client.post(
            "/simulate/call-result",
            json={
                "call_sid": "sim-call-2",
                "from_number": "+8613811113333",
                "to_number": "8613736849910",
                "transcript": "我们这里可以代开发票，还能办理POS机。",
            },
        )

        response = client.get("/digests/preview")

        assert response.status_code == 200
        body = response.json()
        assert body["total"] >= 1
        assert body["spam_count"] >= 1
        assert "Rosie 今日帮你整理了" in body["digest_text"]


def test_simulated_call_result_is_idempotent_by_call_sid():
    with TestClient(app) as client:
        payload = {
            "call_sid": "sim-call-idempotent",
            "from_number": "+8613811115555",
            "to_number": "8613736849910",
            "transcript": "你好，我想预约明天下午三点剪头发。",
        }
        first_response = client.post("/simulate/call-result", json=payload)
        second_response = client.post(
            "/simulate/call-result",
            json={**payload, "transcript": "你好，我想预约明天下午四点剪头发。"},
        )

        assert first_response.status_code == 200
        assert second_response.status_code == 200
        assert first_response.json()["inbox_item_id"] == second_response.json()["inbox_item_id"]

        inbox_response = client.get("/inbox")
        items = [
            item
            for item in inbox_response.json()["items"]
            if item["call_sid"] == "sim-call-idempotent"
        ]
        assert len(items) == 1
        assert "四点" in items[0]["body"]


def test_generate_digest_marks_items_as_digested():
    with TestClient(app) as client:
        client.post(
            "/simulate/call-result",
            json={
                "call_sid": "sim-call-digest-generate",
                "from_number": "+8613811116666",
                "to_number": "8613736849910",
                "transcript": "你好，我想预约明天下午三点剪头发。",
            },
        )

        response = client.post("/digests/generate")

        assert response.status_code == 200
        body = response.json()
        assert body["total"] >= 1
        assert "sim-call-digest-generate" in str(body["item_ids"]) or body["item_ids"]

        preview_response = client.get("/digests/preview")
        assert preview_response.status_code == 200
        pending_ids = [item["id"] for item in preview_response.json()["items"]]
        assert not any(item_id in pending_ids for item_id in body["item_ids"])

        digests_response = client.get("/digests")
        assert digests_response.status_code == 200
        digests = digests_response.json()["items"]
        assert digests[0]["id"] == body["digest_id"]
        assert "Rosie 今日帮你整理了" in digests[0]["digest_text"]


def test_digest_tick_queues_notification_and_is_idempotent():
    with TestClient(app) as client:
        merchant = {
            "merchant_id": "merchant-digest-tick",
            "merchant_name": "定时汇总测试店",
            "access_number": "+8617000000200",
            "enabled": True,
        }
        assert client.post("/merchants", json=merchant).status_code == 200
        client.post(
            "/simulate/call-result",
            json={
                "call_sid": "sim-call-digest-tick",
                "from_number": "+8613811117777",
                "to_number": "+8617000000200",
                "transcript": "你好，我想预约明天下午三点剪头发。",
            },
        )

        response = client.post("/internal/digest-tick?now=2026-04-30T20:00:00")

        assert response.status_code == 200
        result = next(
            item
            for item in response.json()["results"]
            if item["merchant_id"] == "merchant-digest-tick"
        )
        assert result["status"] == "queued"
        assert result["total"] == 1
        assert result["digest_id"]
        assert result["notification_log_id"]

        duplicate_response = client.post("/internal/digest-tick?now=2026-04-30T20:00:00")
        duplicate_result = next(
            item
            for item in duplicate_response.json()["results"]
            if item["merchant_id"] == "merchant-digest-tick"
        )
        assert duplicate_result["status"] == "duplicate"
        assert duplicate_result["notification_log_id"] == result["notification_log_id"]

        logs_response = client.get("/notification-logs?merchant_id=merchant-digest-tick")
        assert logs_response.status_code == 200
        logs = logs_response.json()["items"]
        assert len(logs) == 1
        assert logs[0]["status"] == "queued"
        assert logs[0]["channel"] == "wechat_subscription"
        assert logs[0]["related_digest_id"] == result["digest_id"]


def test_digest_tick_skips_when_not_due():
    with TestClient(app) as client:
        response = client.post("/internal/digest-tick?now=2026-04-30T19:59:00")

        assert response.status_code == 200
        assert all(item["status"] == "not_due" for item in response.json()["results"])


def test_digest_tick_records_empty_due_run():
    with TestClient(app) as client:
        merchant = {
            "merchant_id": "merchant-empty-digest-tick",
            "merchant_name": "空汇总测试店",
            "access_number": "+8617000000201",
            "enabled": True,
        }
        assert client.post("/merchants", json=merchant).status_code == 200

        response = client.post("/internal/digest-tick?now=2026-05-01T20:00:00")

        assert response.status_code == 200
        result = next(
            item
            for item in response.json()["results"]
            if item["merchant_id"] == "merchant-empty-digest-tick"
        )
        assert result["status"] == "skipped_no_pending_items"
        assert result["notification_log_id"]

        logs_response = client.get("/notification-logs?merchant_id=merchant-empty-digest-tick")
        assert logs_response.status_code == 200
        logs = logs_response.json()["items"]
        assert len(logs) == 1
        assert logs[0]["status"] == "skipped"
        assert logs[0]["related_digest_id"] is None


def test_notification_preferences_have_product_defaults():
    with TestClient(app) as client:
        response = client.get("/notification-preferences")

        assert response.status_code == 200
        body = response.json()
        assert body["digest_mode"] == "daily"
        assert body["digest_times"] == ["20:00"]
        assert body["realtime_enabled"] is False
        assert body["urgent_realtime_enabled"] is True
        assert body["team_wecom_enabled"] is False
        assert body["sms_fallback_enabled"] is False


def test_can_update_notification_preferences():
    with TestClient(app) as client:
        response = client.put(
            "/notification-preferences",
            json={
                "digest_mode": "twice_daily",
                "digest_times": ["12:00", "20:00"],
                "realtime_enabled": False,
                "urgent_realtime_enabled": True,
                "team_wecom_enabled": True,
                "sms_fallback_enabled": True,
                "quiet_hours_start": "22:00",
                "quiet_hours_end": "08:00",
            },
        )

        assert response.status_code == 200
        body = response.json()
        assert body["digest_mode"] == "twice_daily"
        assert body["digest_times"] == ["12:00", "20:00"]
        assert body["team_wecom_enabled"] is True
        assert body["sms_fallback_enabled"] is True
        assert body["quiet_hours_start"] == "22:00"


def test_rejects_invalid_notification_digest_time():
    with TestClient(app) as client:
        response = client.put(
            "/notification-preferences",
            json={
                "digest_mode": "daily",
                "digest_times": ["25:00"],
            },
        )

        assert response.status_code == 400
