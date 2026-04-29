from fastapi.testclient import TestClient

from rosie_realtime_voice.app import app


def test_health():
    with TestClient(app) as client:
        response = client.get("/health")
        assert response.status_code == 200
        assert response.json()["status"] == "ok"


def test_jambonz_websocket_counts_audio_bytes():
    with TestClient(app) as client:
        with client.websocket_connect("/ws/jambonz/audio") as websocket:
            websocket.send_json({"callSid": "call-1", "sampleRate": 16000})
            websocket.send_bytes(b"\x00\x01" * 160)

        response = client.get("/sessions")
        items = response.json()["items"]
        assert items
        assert items[-1]["session_id"] == "call-1"
        assert items[-1]["audio_bytes"] >= 320

