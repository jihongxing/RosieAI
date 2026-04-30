from dataclasses import replace

from fastapi.testclient import TestClient
import pytest
import respx
from httpx import Response

import rosie_realtime_voice.app as app_module
import rosie_realtime_voice.speech as speech_module
from rosie_realtime_voice.app import app
from rosie_realtime_voice.pipecat_pipeline import PipecatTurnPipeline
from rosie_realtime_voice.pipeline import RealtimePipeline, RealtimeTurn
from rosie_realtime_voice.session import build_session_context
from rosie_realtime_voice.speech import (
    EdgeTextToSpeech,
    FunASRSpeechToText,
    HTTPSpeechToText,
    HTTPTextToSpeech,
    SherpaOnnxTextToSpeech,
    WindowsSapiTextToSpeech,
)


def test_health():
    with TestClient(app) as client:
        response = client.get("/health")
        assert response.status_code == 200
        assert response.json()["status"] == "ok"
        assert "ready" in response.json()


def test_jambonz_websocket_counts_audio_bytes():
    with TestClient(app) as client:
        with client.websocket_connect("/ws/jambonz/audio") as websocket:
            websocket.send_json(
                {
                    "callSid": "call-1",
                    "sampleRate": 16000,
                    "merchant_id": "demo-merchant",
                    "merchant_name": "测试理发店",
                    "system_prompt": "服务项目：剪发、烫染、护理",
                    "industry_template": {"key": "hair_salon"},
                }
            )
            websocket.send_bytes(b"\x00\x01" * 160)

        response = client.get("/sessions")
        items = response.json()["items"]
        assert items
        assert items[-1]["session_id"] == "call-1"
        assert items[-1]["audio_bytes"] >= 320
        assert items[-1]["context"]["merchant_id"] == "demo-merchant"
        assert "剪发、烫染、护理" in items[-1]["context"]["system_prompt"]


def test_jambonz_websocket_replies_to_transcript_text():
    with TestClient(app) as client:
        with client.websocket_connect("/ws/jambonz/audio") as websocket:
            websocket.send_json(
                {
                    "callSid": "call-agent-1",
                    "merchant_name": "测试理发店",
                    "system_prompt": "服务项目：剪发、烫染、护理",
                }
            )
            websocket.send_json({"transcript": "你好，我想预约明天下午剪头发"})
            reply = websocket.receive_json()

        assert reply["type"] == "agent_reply"
        assert reply["source"] in {"local_fallback", "fallback_after_error", "ai_agent"}
        assert "Rosie" in reply["reply"]

        response = client.get("/sessions")
        item = [row for row in response.json()["items"] if row["session_id"] == "call-agent-1"][-1]
        assert item["transcript_turns"] == 1
        assert item["agent_turns"] == 1
        assert item["last_customer_text"] == "你好，我想预约明天下午剪头发"


def test_jambonz_websocket_posts_business_result(monkeypatch: pytest.MonkeyPatch):
    class FakePipeline:
        async def warmup(self):
            return None

        async def process_audio(self, context, audio, history):
            return None

        async def process_text(self, context, customer_text, history):
            return RealtimeTurn(
                transcript=customer_text,
                reply={"type": "agent_reply", "source": "test_agent", "reply": "可以，请问怎么称呼？"},
                audio=None,
                stt_source="text_frame",
                tts_source="test_tts",
                timings_ms={"agent_ms": 7, "tts_ms": 30, "total_ms": 40},
            )

    class FakeBusinessAPI:
        def __init__(self):
            self.payloads = []

        async def post_realtime_call_result(self, payload):
            self.payloads.append(payload)
            return {"status": "ok", "call_sid": payload["call_sid"]}

        async def close(self):
            return None

    fake_business_api = FakeBusinessAPI()
    monkeypatch.setattr(app_module, "pipeline", FakePipeline())
    monkeypatch.setattr(
        app_module,
        "settings",
        replace(
            app_module.settings,
            business_api_url="http://business.local",
            business_result_enabled=True,
            business_api_timeout_seconds=5,
        ),
    )
    monkeypatch.setattr(app_module, "business_api", fake_business_api)

    with TestClient(app) as client:
        with client.websocket_connect("/ws/jambonz/audio") as websocket:
            websocket.send_json(
                {
                    "callSid": "call-business",
                    "call_id": "provider-call-1",
                    "merchant_id": "demo-merchant",
                    "from": "+8613811112222",
                    "to": "8613736849910",
                }
            )
            websocket.send_json({"transcript": "你好，我想预约明天下午剪头发"})
            websocket.receive_json()

    assert len(fake_business_api.payloads) == 1
    payload = fake_business_api.payloads[0]
    assert payload["call_sid"] == "call-business"
    assert payload["call_id"] == "provider-call-1"
    assert payload["merchant_id"] == "demo-merchant"
    assert payload["transcript"] == "客户：你好，我想预约明天下午剪头发\nRosie：可以，请问怎么称呼？"
    assert payload["turns"][0]["agent_source"] == "test_agent"
    assert payload["timings_ms"]["total_ms"] == 40

    item = [row for row in app_module.sessions.values() if row["session_id"] == "call-business"][-1]
    assert item["business_result_status"] == "sent"
    assert item["business_result_response"]["call_sid"] == "call-business"


def test_jambonz_websocket_auto_dispatches_realtime_notification(monkeypatch: pytest.MonkeyPatch):
    class FakePipeline:
        async def warmup(self):
            return None

        async def process_audio(self, context, audio, history):
            return None

        async def process_text(self, context, customer_text, history):
            return RealtimeTurn(
                transcript=customer_text,
                reply={"type": "agent_reply", "source": "test_agent", "reply": "好的，已经记录。"},
                audio=None,
                stt_source="text_frame",
                timings_ms={"total_ms": 25},
            )

    class FakeBusinessAPI:
        def __init__(self):
            self.dispatched_keys = []

        async def post_realtime_call_result(self, payload):
            return {
                "status": "ok",
                "call_sid": payload["call_sid"],
                "realtime_notification": {
                    "status": "queued",
                    "idempotency_key": "realtime_call:demo-merchant:call-dispatch",
                },
            }

        async def dispatch_notification(self, idempotency_key):
            self.dispatched_keys.append(idempotency_key)
            return {"status": "ok", "total": 1, "results": [{"status": "sent"}]}

        async def close(self):
            return None

    fake_business_api = FakeBusinessAPI()
    monkeypatch.setattr(app_module, "pipeline", FakePipeline())
    monkeypatch.setattr(
        app_module,
        "settings",
        replace(
            app_module.settings,
            business_api_url="http://business.local",
            business_result_enabled=True,
            business_auto_dispatch_enabled=True,
        ),
    )
    monkeypatch.setattr(app_module, "business_api", fake_business_api)

    with TestClient(app) as client:
        with client.websocket_connect("/ws/jambonz/audio") as websocket:
            websocket.send_json({"callSid": "call-dispatch", "merchant_id": "demo-merchant"})
            websocket.send_json({"transcript": "你好，我想预约明天下午剪头发"})
            websocket.receive_json()

    assert fake_business_api.dispatched_keys == ["realtime_call:demo-merchant:call-dispatch"]
    item = [row for row in app_module.sessions.values() if row["session_id"] == "call-dispatch"][-1]
    assert item["business_dispatch_status"] == "sent"
    assert item["business_dispatch_response"]["total"] == 1


def test_jambonz_websocket_queues_failed_business_result_and_flushes(monkeypatch: pytest.MonkeyPatch):
    class FakePipeline:
        async def warmup(self):
            return None

        async def process_audio(self, context, audio, history):
            return None

        async def process_text(self, context, customer_text, history):
            return RealtimeTurn(
                transcript=customer_text,
                reply={"type": "agent_reply", "source": "test_agent", "reply": "我会记录下来。"},
                audio=None,
                stt_source="text_frame",
                timings_ms={"total_ms": 20},
            )

    class FlakyBusinessAPI:
        def __init__(self):
            self.calls = 0

        async def post_realtime_call_result(self, payload):
            self.calls += 1
            if self.calls == 1:
                raise RuntimeError("business api unavailable")
            return {"status": "ok", "call_sid": payload["call_sid"]}

        async def close(self):
            return None

    flaky_business_api = FlakyBusinessAPI()
    app_module.business_result_retry_queue.clear()
    monkeypatch.setattr(app_module, "pipeline", FakePipeline())
    monkeypatch.setattr(
        app_module,
        "settings",
        replace(
            app_module.settings,
            business_api_url="http://business.local",
            business_result_enabled=True,
            business_result_retry_max_attempts=5,
        ),
    )
    monkeypatch.setattr(app_module, "business_api", flaky_business_api)

    with TestClient(app) as client:
        with client.websocket_connect("/ws/jambonz/audio") as websocket:
            websocket.send_json({"callSid": "call-retry", "merchant_id": "demo-merchant"})
            websocket.send_json({"transcript": "请帮我记录一个问题"})
            websocket.receive_json()

        retries = client.get("/business-result-retries").json()["items"]
        assert len(retries) == 1
        assert retries[0]["session_id"] == "call-retry"
        assert retries[0]["attempt_count"] == 1

        flushed = client.post("/internal/business-result-retries/flush").json()
        assert flushed["remaining"] == 0
        assert flushed["results"][0]["status"] == "sent"

    item = [row for row in app_module.sessions.values() if row["session_id"] == "call-retry"][-1]
    assert item["business_result_status"] == "sent"
    assert item["business_result_retry_queued"] is False


def test_jambonz_websocket_sends_tts_audio_after_stt(monkeypatch: pytest.MonkeyPatch):
    class FakePipeline:
        async def process_audio(self, context, audio, history):
            return RealtimeTurn(
                transcript="客户想预约剪发",
                reply={"type": "agent_reply", "source": "test_agent", "reply": "请问您几点到店？"},
                audio=b"\x01\x02\x03\x04",
                stt_source="test_stt",
                tts_source="test_tts",
            )

        async def process_text(self, context, customer_text, history):
            return None

    monkeypatch.setattr(app_module, "pipeline", FakePipeline())
    monkeypatch.setattr(
        app_module,
        "settings",
        replace(app_module.settings, stt_min_audio_bytes=4),
    )

    with TestClient(app) as client:
        with client.websocket_connect("/ws/jambonz/audio") as websocket:
            websocket.send_json({"callSid": "call-audio-out", "merchant_name": "测试理发店"})
            websocket.send_bytes(b"\x00\x01\x00\x01")
            reply = websocket.receive_json()
            audio = websocket.receive_bytes()

        assert reply["reply"] == "请问您几点到店？"
        assert audio == b"\x01\x02\x03\x04"
        response = client.get("/sessions")
        item = [row for row in response.json()["items"] if row["session_id"] == "call-audio-out"][-1]
        assert item["stt_turns"] == 1
        assert item["tts_turns"] == 1
        assert item["tts_audio_bytes"] == 4
        assert item["stt_source"] == "test_stt"
        assert item["tts_source"] == "test_tts"


@pytest.mark.asyncio
@respx.mock
async def test_http_stt_provider_transcribes_audio():
    respx.post("http://stt.local/transcribe").mock(
        return_value=Response(
            200,
            json={"transcript": "客户想预约剪发", "is_final": True},
        )
    )
    stt = HTTPSpeechToText("http://stt.local/transcribe", 5)
    context = build_session_context("s1", {"callSid": "s1", "merchant_id": "demo"})

    result = await stt.transcribe(context, b"\x00\x01" * 160)

    assert result is not None
    assert result.transcript == "客户想预约剪发"


@pytest.mark.asyncio
@respx.mock
async def test_http_tts_provider_returns_audio_bytes():
    respx.post("http://tts.local/synthesize").mock(
        return_value=Response(
            200,
            content=b"\x01\x02",
            headers={"content-type": "audio/L16"},
        )
    )
    tts = HTTPTextToSpeech("http://tts.local/synthesize", 5)
    context = build_session_context("s1", {"callSid": "s1", "merchant_id": "demo"})

    result = await tts.synthesize(context, "请问您几点到店？")

    assert result is not None
    assert result.audio == b"\x01\x02"
    assert result.content_type == "audio/L16"


@pytest.mark.asyncio
async def test_funasr_provider_transcribes_pcm(monkeypatch: pytest.MonkeyPatch):
    monkeypatch.setattr(
        FunASRSpeechToText,
        "_generate",
        lambda self, wav_path: [{"text": "<|zh|><|NEUTRAL|><|Speech|><|withitn|>客户想预约剪发"}],
    )
    stt = FunASRSpeechToText(
        model_name="iic/SenseVoiceSmall",
        language="zh",
        timeout_seconds=5,
    )
    context = build_session_context("s1", {"callSid": "s1", "merchant_id": "demo"})

    result = await stt.transcribe(context, b"\x00\x01" * 160)

    assert result is not None
    assert result.transcript == "客户想预约剪发"
    assert result.source == "funasr_stt"


@pytest.mark.asyncio
async def test_edge_tts_provider_returns_phone_pcm(monkeypatch: pytest.MonkeyPatch):
    async def fake_edge_synthesize_mp3(text, voice, rate, pitch):
        assert voice == "zh-CN-XiaoxiaoNeural"
        return b"mp3-bytes"

    async def fake_transcode_mp3_to_pcm(mp3, sample_rate, ffmpeg_path):
        assert mp3 == b"mp3-bytes"
        assert sample_rate == 16000
        return b"\x01\x02\x03\x04"

    monkeypatch.setattr(speech_module, "_edge_synthesize_mp3", fake_edge_synthesize_mp3)
    monkeypatch.setattr(speech_module, "_transcode_mp3_to_pcm", fake_transcode_mp3_to_pcm)
    tts = EdgeTextToSpeech(
        voice="zh-CN-XiaoxiaoNeural",
        rate="+0%",
        pitch="+0Hz",
        ffmpeg_path="ffmpeg",
        timeout_seconds=5,
        cache_enabled=False,
    )
    context = build_session_context("s1", {"callSid": "s1", "merchant_id": "demo"})

    result = await tts.synthesize(context, "请问您几点到店？")

    assert result is not None
    assert result.audio == b"\x01\x02\x03\x04"
    assert result.content_type == "audio/L16;rate=16000"
    assert result.source == "edge_tts"


@pytest.mark.asyncio
async def test_edge_tts_provider_reuses_cached_phone_pcm(monkeypatch: pytest.MonkeyPatch):
    calls = {"edge": 0, "ffmpeg": 0}

    async def fake_edge_synthesize_mp3(text, voice, rate, pitch):
        calls["edge"] += 1
        return b"mp3-bytes"

    async def fake_transcode_mp3_to_pcm(mp3, sample_rate, ffmpeg_path):
        calls["ffmpeg"] += 1
        return b"\x01\x02\x03\x04"

    monkeypatch.setattr(speech_module, "_edge_synthesize_mp3", fake_edge_synthesize_mp3)
    monkeypatch.setattr(speech_module, "_transcode_mp3_to_pcm", fake_transcode_mp3_to_pcm)
    tts = EdgeTextToSpeech(
        voice="zh-CN-XiaoxiaoNeural",
        rate="+0%",
        pitch="+0Hz",
        ffmpeg_path="ffmpeg",
        timeout_seconds=5,
        cache_enabled=True,
    )
    context = build_session_context("s1", {"callSid": "s1", "merchant_id": "demo"})

    first = await tts.synthesize(context, "请问您几点到店？")
    second = await tts.synthesize(context, "请问您几点到店？")

    assert first is not None
    assert second is not None
    assert first.source == "edge_tts"
    assert second.source == "edge_tts_cache"
    assert second.audio == b"\x01\x02\x03\x04"
    assert calls == {"edge": 1, "ffmpeg": 1}


@pytest.mark.asyncio
async def test_sapi_tts_provider_returns_dynamic_phone_pcm(monkeypatch: pytest.MonkeyPatch):
    async def fake_sapi_synthesize_wav(text, output_path, voice, rate):
        assert text == "动态回复也要快速合成"
        assert voice == "Microsoft Huihui Desktop"
        assert rate == 1

    async def fake_transcode_file_to_pcm(path, sample_rate, ffmpeg_path):
        assert sample_rate == 16000
        return b"\x05\x06"

    monkeypatch.setattr(speech_module, "_sapi_synthesize_wav", fake_sapi_synthesize_wav)
    monkeypatch.setattr(speech_module, "_transcode_file_to_pcm", fake_transcode_file_to_pcm)
    tts = WindowsSapiTextToSpeech(
        voice="Microsoft Huihui Desktop",
        rate=1,
        ffmpeg_path="ffmpeg",
        timeout_seconds=5,
    )
    context = build_session_context("s1", {"callSid": "s1", "merchant_id": "demo"})

    result = await tts.synthesize(context, "动态回复也要快速合成")

    assert result is not None
    assert result.audio == b"\x05\x06"
    assert result.content_type == "audio/L16;rate=16000"
    assert result.source == "sapi_tts"


@pytest.mark.asyncio
async def test_sherpa_onnx_tts_provider_returns_dynamic_phone_pcm(monkeypatch: pytest.MonkeyPatch):
    class FakeGeneratedAudio:
        samples = [0.0, 0.25, -0.25]
        sample_rate = 24000

    monkeypatch.setattr(
        SherpaOnnxTextToSpeech,
        "_generate",
        lambda self, text: FakeGeneratedAudio(),
    )

    async def fake_float_audio_to_pcm(samples, source_sample_rate, target_sample_rate, ffmpeg_path):
        assert list(samples) == [0.0, 0.25, -0.25]
        assert source_sample_rate == 24000
        assert target_sample_rate == 16000
        return b"\x07\x08"

    monkeypatch.setattr(speech_module, "_float_audio_to_pcm", fake_float_audio_to_pcm)
    tts = SherpaOnnxTextToSpeech(
        model_type="vits",
        model="model.onnx",
        tokens="tokens.txt",
        lexicon=None,
        data_dir=None,
        rule_fsts=None,
        provider="cpu",
        num_threads=2,
        sid=0,
        speed=1.0,
        ffmpeg_path="ffmpeg",
        timeout_seconds=5,
    )
    context = build_session_context("s1", {"callSid": "s1", "merchant_id": "demo"})

    result = await tts.synthesize(context, "动态回复也要快速合成")

    assert result is not None
    assert result.audio == b"\x07\x08"
    assert result.content_type == "audio/L16;rate=16000"
    assert result.source == "sherpa_onnx_tts"


@pytest.mark.asyncio
async def test_realtime_pipeline_warmup_calls_stt_and_tts():
    calls: list[str] = []

    class FakeStt:
        async def warmup(self):
            calls.append("stt")

        async def transcribe(self, context, audio):
            return None

    class FakeTts:
        async def warmup(self):
            calls.append("tts")

        async def synthesize(self, context, text):
            return None

    class FakeAgent:
        async def warmup(self):
            calls.append("agent")

        async def reply(self, context, customer_text, history):
            return None

    pipeline = RealtimePipeline(FakeStt(), FakeAgent(), FakeTts())

    await pipeline.warmup()

    assert calls == ["stt", "agent", "tts"]


@pytest.mark.asyncio
@respx.mock
async def test_pipecat_pipeline_returns_transcript_reply_and_audio():
    respx.post("http://pipecat.local/turn").mock(
        return_value=Response(
            200,
            json={
                "transcript": "客户想预约剪发",
                "reply": "请问您几点到店？",
                "audio_base64": "AQIDBA==",
                "content_type": "audio/L16;rate=16000",
            },
        )
    )
    pipeline = PipecatTurnPipeline("http://pipecat.local", 5)
    context = build_session_context(
        "s1",
        {
            "callSid": "s1",
            "merchant_id": "demo",
            "system_prompt": "服务项目：剪发、烫染、护理",
        },
    )
    history: list[dict[str, str]] = []

    turn = await pipeline.process_audio(context, b"\x00\x01", history)

    assert turn is not None
    assert turn.transcript == "客户想预约剪发"
    assert turn.reply["reply"] == "请问您几点到店？"
    assert turn.audio == b"\x01\x02\x03\x04"
    assert turn.stt_source == "pipecat_stt"
    assert turn.tts_source == "pipecat_tts"
    assert history[-2:] == [
        {"role": "user", "content": "客户想预约剪发"},
        {"role": "assistant", "content": "请问您几点到店？"},
    ]
