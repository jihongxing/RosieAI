import os
import tempfile

os.environ["ROSIE_DB_PATH"] = tempfile.NamedTemporaryFile(delete=True).name
os.environ["ROSIE_DEFAULT_ACCESS_NUMBER"] = "+8617000000000"
os.environ["ROSIE_DEFAULT_MERCHANT_NAME"] = "测试理发店"
os.environ["ROSIE_USE_AI_GREETING"] = "false"

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
                "to": "+8617000000000",
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
                "to": "+8617000000000",
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
