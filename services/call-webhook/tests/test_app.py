import os
import tempfile

os.environ["ROSIE_DB_PATH"] = tempfile.NamedTemporaryFile(delete=True).name
os.environ["ROSIE_DEFAULT_ACCESS_NUMBER"] = "8613736849910"
os.environ["ROSIE_DEFAULT_MERCHANT_NAME"] = "测试理发店"
os.environ["ROSIE_USE_AI_GREETING"] = "false"
os.environ["ROSIE_USE_AI_EXTRACT"] = "false"
os.environ["ROSIE_REALTIME_LISTEN_ENABLED"] = "false"

from fastapi.testclient import TestClient  # noqa: E402

from rosie_call_webhook.app import app  # noqa: E402


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
