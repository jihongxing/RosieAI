from __future__ import annotations

import asyncio
import base64
import logging
import math
import os
import re
import tempfile
import wave
from collections import OrderedDict
from dataclasses import dataclass
from typing import Any

import httpx

from .config import Settings
from .session import RealtimeSessionContext


logger = logging.getLogger(__name__)


@dataclass
class STTResult:
    transcript: str
    is_final: bool = True
    source: str = "http_stt"


@dataclass
class TTSResult:
    audio: bytes
    content_type: str = "audio/L16;rate=16000"
    source: str = "http_tts"


class SpeechToText:
    async def warmup(self) -> None:
        return None

    async def transcribe(
        self,
        context: RealtimeSessionContext,
        audio: bytes,
    ) -> STTResult | None:
        return None


class TextToSpeech:
    async def warmup(self) -> None:
        return None

    async def synthesize(
        self,
        context: RealtimeSessionContext,
        text: str,
    ) -> TTSResult | None:
        return None


class HTTPSpeechToText(SpeechToText):
    def __init__(self, url: str, timeout_seconds: float) -> None:
        self.url = url.rstrip("/")
        self.timeout_seconds = timeout_seconds

    async def transcribe(
        self,
        context: RealtimeSessionContext,
        audio: bytes,
    ) -> STTResult | None:
        if not audio:
            return None
        headers = {
            "Content-Type": "audio/L16",
            "X-Rosie-Session-ID": context.session_id,
            "X-Rosie-Merchant-ID": context.merchant_id,
        }
        params = {"sample_rate": context.sample_rate}
        try:
            async with httpx.AsyncClient(timeout=httpx.Timeout(self.timeout_seconds)) as client:
                response = await client.post(self.url, params=params, content=audio, headers=headers)
                response.raise_for_status()
        except httpx.HTTPError as exc:
            logger.warning("stt request failed: %s", exc)
            return None

        content_type = response.headers.get("content-type", "")
        if "application/json" in content_type:
            payload = response.json()
            text = _string_value(payload, "transcript", "text", "utterance")
            is_final = bool(payload.get("is_final", payload.get("final", True)))
        else:
            text = response.text.strip()
            is_final = True
        if not text:
            return None
        return STTResult(transcript=text, is_final=is_final)


class FunASRSpeechToText(SpeechToText):
    def __init__(
        self,
        model_name: str,
        language: str,
        timeout_seconds: float,
        vad_model: str | None = None,
    ) -> None:
        self.model_name = model_name
        self.language = language
        self.timeout_seconds = timeout_seconds
        self.vad_model = vad_model
        self._model: Any | None = None

    async def warmup(self) -> None:
        await asyncio.wait_for(
            asyncio.to_thread(self._get_model),
            timeout=self.timeout_seconds,
        )

    async def transcribe(
        self,
        context: RealtimeSessionContext,
        audio: bytes,
    ) -> STTResult | None:
        if not audio:
            return None
        path = _write_pcm_wav(audio, context.sample_rate)
        try:
            result = await asyncio.wait_for(
                asyncio.to_thread(self._generate, path),
                timeout=self.timeout_seconds,
            )
        except TimeoutError:
            logger.warning("funasr stt timed out after %ss", self.timeout_seconds)
            return None
        except Exception as exc:  # pragma: no cover - depends on optional model runtime
            logger.warning("funasr stt failed: %s", exc)
            return None
        finally:
            try:
                os.remove(path)
            except OSError:
                pass

        text = _extract_funasr_text(result)
        if not text:
            return None
        return STTResult(transcript=text, is_final=True, source="funasr_stt")

    def _generate(self, wav_path: str) -> Any:
        model = self._get_model()
        return model.generate(
            input=wav_path,
            language=self.language,
            use_itn=True,
            batch_size_s=60,
        )

    def _get_model(self) -> Any:
        if self._model is None:
            try:
                from funasr import AutoModel
            except ImportError as exc:  # pragma: no cover - optional dependency
                raise RuntimeError(
                    "ROSIE_STT_PROVIDER=funasr requires installing funasr/modelscope runtime"
                ) from exc

            kwargs: dict[str, Any] = {
                "model": self.model_name,
                "disable_update": True,
                "trust_remote_code": True,
            }
            if self.vad_model:
                kwargs["vad_model"] = self.vad_model
                kwargs["vad_kwargs"] = {"max_single_segment_time": 30000}
            self._model = AutoModel(**kwargs)
        return self._model


class HTTPTextToSpeech(TextToSpeech):
    def __init__(self, url: str, timeout_seconds: float) -> None:
        self.url = url.rstrip("/")
        self.timeout_seconds = timeout_seconds

    async def synthesize(
        self,
        context: RealtimeSessionContext,
        text: str,
    ) -> TTSResult | None:
        if not text:
            return None
        payload = {
            "text": text,
            "sample_rate": context.sample_rate,
            "session": context.to_dict(),
        }
        try:
            async with httpx.AsyncClient(timeout=httpx.Timeout(self.timeout_seconds)) as client:
                response = await client.post(self.url, json=payload)
                response.raise_for_status()
        except httpx.HTTPError as exc:
            logger.warning("tts request failed: %s", exc)
            return None

        content_type = response.headers.get("content-type", "")
        if content_type.startswith("audio/") or content_type == "application/octet-stream":
            return TTSResult(audio=response.content, content_type=content_type or "application/octet-stream")

        payload = response.json()
        encoded = _string_value(payload, "audio_base64", "audio")
        if not encoded:
            return None
        audio = base64.b64decode(encoded)
        return TTSResult(
            audio=audio,
            content_type=str(payload.get("content_type") or "audio/L16;rate=16000"),
        )


class EdgeTextToSpeech(TextToSpeech):
    def __init__(
        self,
        voice: str,
        rate: str,
        pitch: str,
        ffmpeg_path: str,
        timeout_seconds: float,
        cache_enabled: bool = True,
        cache_max_entries: int = 128,
    ) -> None:
        self.voice = voice
        self.rate = rate
        self.pitch = pitch
        self.ffmpeg_path = ffmpeg_path
        self.timeout_seconds = timeout_seconds
        self.cache_enabled = cache_enabled
        self.cache_max_entries = max(0, cache_max_entries)
        self._cache: OrderedDict[tuple[str, str, str, int, str], TTSResult] = OrderedDict()

    async def synthesize(
        self,
        context: RealtimeSessionContext,
        text: str,
    ) -> TTSResult | None:
        if not text:
            return None
        cache_key = (self.voice, self.rate, self.pitch, context.sample_rate, text)
        if self.cache_enabled and cache_key in self._cache:
            cached = self._cache.pop(cache_key)
            self._cache[cache_key] = cached
            return TTSResult(
                audio=cached.audio,
                content_type=cached.content_type,
                source="edge_tts_cache",
            )

        try:
            mp3 = await asyncio.wait_for(
                _edge_synthesize_mp3(text, self.voice, self.rate, self.pitch),
                timeout=self.timeout_seconds,
            )
            pcm = await asyncio.wait_for(
                _transcode_mp3_to_pcm(mp3, context.sample_rate, self.ffmpeg_path),
                timeout=self.timeout_seconds,
            )
        except TimeoutError:
            logger.warning("edge tts timed out after %ss", self.timeout_seconds)
            return None
        except Exception as exc:  # pragma: no cover - depends on optional edge/ffmpeg runtime
            logger.warning("edge tts failed: %s", exc)
            return None

        if not pcm:
            return None
        result = TTSResult(
            audio=pcm,
            content_type=f"audio/L16;rate={context.sample_rate}",
            source="edge_tts",
        )
        if self.cache_enabled and self.cache_max_entries > 0:
            self._cache[cache_key] = result
            while len(self._cache) > self.cache_max_entries:
                self._cache.popitem(last=False)
        return result


class WindowsSapiTextToSpeech(TextToSpeech):
    def __init__(
        self,
        voice: str | None,
        rate: int,
        ffmpeg_path: str,
        timeout_seconds: float,
    ) -> None:
        self.voice = voice
        self.rate = rate
        self.ffmpeg_path = ffmpeg_path
        self.timeout_seconds = timeout_seconds

    async def synthesize(
        self,
        context: RealtimeSessionContext,
        text: str,
    ) -> TTSResult | None:
        if not text:
            return None
        wav_path = tempfile.NamedTemporaryFile(delete=False, suffix=".wav").name
        try:
            await asyncio.wait_for(
                _sapi_synthesize_wav(text, wav_path, self.voice, self.rate),
                timeout=self.timeout_seconds,
            )
            pcm = await asyncio.wait_for(
                _transcode_file_to_pcm(wav_path, context.sample_rate, self.ffmpeg_path),
                timeout=self.timeout_seconds,
            )
        except TimeoutError:
            logger.warning("windows sapi tts timed out after %ss", self.timeout_seconds)
            return None
        except Exception as exc:  # pragma: no cover - depends on local Windows voices
            logger.warning("windows sapi tts failed: %s", exc)
            return None
        finally:
            try:
                os.remove(wav_path)
            except OSError:
                pass

        if not pcm:
            return None
        return TTSResult(
            audio=pcm,
            content_type=f"audio/L16;rate={context.sample_rate}",
            source="sapi_tts",
        )


class SherpaOnnxTextToSpeech(TextToSpeech):
    def __init__(
        self,
        model_type: str,
        model: str | None,
        tokens: str | None,
        lexicon: str | None,
        data_dir: str | None,
        rule_fsts: str | None,
        provider: str,
        num_threads: int,
        sid: int,
        speed: float,
        ffmpeg_path: str,
        timeout_seconds: float,
    ) -> None:
        self.model_type = model_type
        self.model = model
        self.tokens = tokens
        self.lexicon = lexicon
        self.data_dir = data_dir
        self.rule_fsts = rule_fsts
        self.provider = provider
        self.num_threads = num_threads
        self.sid = sid
        self.speed = speed
        self.ffmpeg_path = ffmpeg_path
        self.timeout_seconds = timeout_seconds
        self._tts: Any | None = None

    async def warmup(self) -> None:
        await asyncio.wait_for(
            asyncio.to_thread(self._get_tts),
            timeout=self.timeout_seconds,
        )

    async def synthesize(
        self,
        context: RealtimeSessionContext,
        text: str,
    ) -> TTSResult | None:
        if not text:
            return None
        try:
            generated = await asyncio.wait_for(
                asyncio.to_thread(self._generate, text),
                timeout=self.timeout_seconds,
            )
            pcm = await asyncio.wait_for(
                _float_audio_to_pcm(
                    generated.samples,
                    generated.sample_rate,
                    context.sample_rate,
                    self.ffmpeg_path,
                ),
                timeout=self.timeout_seconds,
            )
        except TimeoutError:
            logger.warning("sherpa-onnx tts timed out after %ss", self.timeout_seconds)
            return None
        except Exception as exc:  # pragma: no cover - depends on optional model runtime
            logger.warning("sherpa-onnx tts failed: %s", exc)
            return None

        if not pcm:
            return None
        return TTSResult(
            audio=pcm,
            content_type=f"audio/L16;rate={context.sample_rate}",
            source="sherpa_onnx_tts",
        )

    def _generate(self, text: str) -> Any:
        tts = self._get_tts()
        return tts.generate(text, sid=self.sid, speed=self.speed)

    def _get_tts(self) -> Any:
        if self._tts is not None:
            return self._tts
        try:
            import sherpa_onnx
        except ImportError as exc:  # pragma: no cover - optional dependency
            raise RuntimeError("ROSIE_TTS_PROVIDER=sherpa_onnx requires installing sherpa-onnx") from exc

        if self.model_type != "vits":
            raise RuntimeError(f"unsupported sherpa-onnx tts model type: {self.model_type}")
        if not self.model or not self.tokens:
            raise RuntimeError("ROSIE_TTS_PROVIDER=sherpa_onnx requires ROSIE_TTS_SHERPA_MODEL and TOKENS")

        vits = sherpa_onnx.OfflineTtsVitsModelConfig(
            model=self.model,
            lexicon=self.lexicon or "",
            tokens=self.tokens,
            data_dir=self.data_dir or "",
        )
        if self.rule_fsts:
            vits.rule_fsts = self.rule_fsts
        model_config = sherpa_onnx.OfflineTtsModelConfig(
            vits=vits,
            provider=self.provider,
            debug=False,
            num_threads=self.num_threads,
        )
        config = sherpa_onnx.OfflineTtsConfig(model=model_config)
        if not config.validate():
            raise RuntimeError("invalid sherpa-onnx tts config")
        self._tts = sherpa_onnx.OfflineTts(config)
        return self._tts


def create_stt_provider(settings: Settings) -> SpeechToText:
    provider = settings.stt_provider
    url = settings.stt_url
    timeout_seconds = settings.stt_timeout_seconds
    if provider == "http" and url:
        return HTTPSpeechToText(url, timeout_seconds)
    if provider == "funasr":
        return FunASRSpeechToText(
            model_name=settings.stt_model,
            language=settings.stt_language,
            timeout_seconds=timeout_seconds,
            vad_model=settings.stt_vad_model,
        )
    return SpeechToText()


def create_tts_provider(settings: Settings) -> TextToSpeech:
    provider = settings.tts_provider
    url = settings.tts_url
    timeout_seconds = settings.tts_timeout_seconds
    if provider == "http" and url:
        return HTTPTextToSpeech(url, timeout_seconds)
    if provider == "edge":
        return EdgeTextToSpeech(
            voice=settings.tts_voice,
            rate=settings.tts_rate,
            pitch=settings.tts_pitch,
            ffmpeg_path=settings.tts_ffmpeg_path,
            timeout_seconds=timeout_seconds,
            cache_enabled=settings.tts_cache_enabled,
            cache_max_entries=settings.tts_cache_max_entries,
        )
    if provider == "sapi":
        return WindowsSapiTextToSpeech(
            voice=settings.tts_sapi_voice,
            rate=settings.tts_sapi_rate,
            ffmpeg_path=settings.tts_ffmpeg_path,
            timeout_seconds=timeout_seconds,
        )
    if provider == "sherpa_onnx":
        return SherpaOnnxTextToSpeech(
            model_type=settings.tts_sherpa_model_type,
            model=settings.tts_sherpa_model,
            tokens=settings.tts_sherpa_tokens,
            lexicon=settings.tts_sherpa_lexicon,
            data_dir=settings.tts_sherpa_data_dir,
            rule_fsts=settings.tts_sherpa_rule_fsts,
            provider=settings.tts_sherpa_provider,
            num_threads=settings.tts_sherpa_num_threads,
            sid=settings.tts_sherpa_sid,
            speed=settings.tts_sherpa_speed,
            ffmpeg_path=settings.tts_ffmpeg_path,
            timeout_seconds=timeout_seconds,
        )
    return TextToSpeech()


def _string_value(payload: dict[str, Any], *keys: str) -> str:
    for key in keys:
        value = payload.get(key)
        if value not in (None, ""):
            return str(value).strip()
    return ""


def _write_pcm_wav(audio: bytes, sample_rate: int) -> str:
    handle = tempfile.NamedTemporaryFile(delete=False, suffix=".wav")
    handle.close()
    with wave.open(handle.name, "wb") as wav:
        wav.setnchannels(1)
        wav.setsampwidth(2)
        wav.setframerate(sample_rate)
        wav.writeframes(audio)
    return handle.name


def _extract_funasr_text(result: Any) -> str:
    if isinstance(result, str):
        return _clean_funasr_text(result)
    if isinstance(result, dict):
        return _clean_funasr_text(_string_value(result, "text", "transcript", "sentence"))
    if isinstance(result, list):
        parts = [_extract_funasr_text(item) for item in result]
        return " ".join(part for part in parts if part).strip()
    return ""


def _clean_funasr_text(text: str) -> str:
    return re.sub(r"<\|[^|]+?\|>", "", text).strip()


async def _edge_synthesize_mp3(text: str, voice: str, rate: str, pitch: str) -> bytes:
    try:
        import edge_tts
    except ImportError as exc:  # pragma: no cover - optional dependency
        raise RuntimeError("ROSIE_TTS_PROVIDER=edge requires installing edge-tts") from exc

    communicate = edge_tts.Communicate(text, voice, rate=rate, pitch=pitch)
    chunks: list[bytes] = []
    async for chunk in communicate.stream():
        if chunk.get("type") == "audio" and chunk.get("data"):
            chunks.append(chunk["data"])
    return b"".join(chunks)


async def _transcode_mp3_to_pcm(mp3: bytes, sample_rate: int, ffmpeg_path: str) -> bytes:
    if not mp3:
        return b""
    process = await asyncio.create_subprocess_exec(
        ffmpeg_path,
        "-hide_banner",
        "-loglevel",
        "error",
        "-i",
        "pipe:0",
        "-f",
        "s16le",
        "-acodec",
        "pcm_s16le",
        "-ac",
        "1",
        "-ar",
        str(sample_rate),
        "pipe:1",
        stdin=asyncio.subprocess.PIPE,
        stdout=asyncio.subprocess.PIPE,
        stderr=asyncio.subprocess.PIPE,
    )
    stdout, stderr = await process.communicate(input=mp3)
    if process.returncode != 0:
        message = stderr.decode("utf-8", errors="ignore").strip()
        raise RuntimeError(f"ffmpeg failed to convert edge tts audio: {message}")
    return stdout


async def _sapi_synthesize_wav(text: str, output_path: str, voice: str | None, rate: int) -> None:
    escaped_text = _powershell_quote(text)
    escaped_output = _powershell_quote(output_path)
    script = (
        "Add-Type -AssemblyName System.Speech; "
        "$s = New-Object System.Speech.Synthesis.SpeechSynthesizer; "
        f"$s.Rate = {max(-10, min(10, rate))}; "
    )
    if voice:
        script += f"$s.SelectVoice({_powershell_quote(voice)}); "
    script += f"$s.SetOutputToWaveFile({escaped_output}); $s.Speak({escaped_text}); $s.Dispose();"

    process = await asyncio.create_subprocess_exec(
        "powershell",
        "-NoProfile",
        "-ExecutionPolicy",
        "Bypass",
        "-Command",
        script,
        stdout=asyncio.subprocess.PIPE,
        stderr=asyncio.subprocess.PIPE,
    )
    _, stderr = await process.communicate()
    if process.returncode != 0:
        message = stderr.decode("utf-8", errors="ignore").strip()
        raise RuntimeError(f"windows sapi powershell failed: {message}")


async def _transcode_file_to_pcm(path: str, sample_rate: int, ffmpeg_path: str) -> bytes:
    process = await asyncio.create_subprocess_exec(
        ffmpeg_path,
        "-hide_banner",
        "-loglevel",
        "error",
        "-i",
        path,
        "-f",
        "s16le",
        "-acodec",
        "pcm_s16le",
        "-ac",
        "1",
        "-ar",
        str(sample_rate),
        "pipe:1",
        stdout=asyncio.subprocess.PIPE,
        stderr=asyncio.subprocess.PIPE,
    )
    stdout, stderr = await process.communicate()
    if process.returncode != 0:
        message = stderr.decode("utf-8", errors="ignore").strip()
        raise RuntimeError(f"ffmpeg failed to convert wav audio: {message}")
    return stdout


def _powershell_quote(value: str) -> str:
    return "'" + value.replace("'", "''") + "'"


async def _float_audio_to_pcm(
    samples: Any,
    source_sample_rate: int,
    target_sample_rate: int,
    ffmpeg_path: str,
) -> bytes:
    wav_path = tempfile.NamedTemporaryFile(delete=False, suffix=".wav").name
    try:
        _write_float_wav(samples, source_sample_rate, wav_path)
        return await _transcode_file_to_pcm(wav_path, target_sample_rate, ffmpeg_path)
    finally:
        try:
            os.remove(wav_path)
        except OSError:
            pass


def _write_float_wav(samples: Any, sample_rate: int, path: str) -> None:
    values = []
    for value in samples:
        if isinstance(value, float) and math.isnan(value):
            value = 0.0
        clipped = max(-1.0, min(1.0, float(value)))
        values.append(int(clipped * 32767))
    with wave.open(path, "wb") as wav:
        wav.setnchannels(1)
        wav.setsampwidth(2)
        wav.setframerate(sample_rate)
        frames = b"".join(int(value).to_bytes(2, "little", signed=True) for value in values)
        wav.writeframes(frames)
