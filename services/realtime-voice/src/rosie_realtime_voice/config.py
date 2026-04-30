from dataclasses import dataclass
import os


@dataclass(frozen=True)
class Settings:
    host: str
    port: int
    reload: bool
    prewarm_enabled: bool
    business_api_url: str | None
    business_result_enabled: bool
    business_auto_dispatch_enabled: bool
    business_api_timeout_seconds: float
    business_result_retry_max_attempts: int
    pipeline_provider: str
    pipecat_url: str | None
    pipecat_timeout_seconds: float
    ai_agent_url: str | None
    agent_enabled: bool
    agent_timeout_seconds: float
    stt_provider: str
    stt_url: str | None
    stt_model: str
    stt_vad_model: str | None
    stt_language: str
    stt_timeout_seconds: float
    stt_min_audio_bytes: int
    tts_provider: str
    tts_url: str | None
    tts_voice: str
    tts_rate: str
    tts_pitch: str
    tts_ffmpeg_path: str
    tts_sapi_voice: str | None
    tts_sapi_rate: int
    tts_sherpa_model_type: str
    tts_sherpa_model: str | None
    tts_sherpa_tokens: str | None
    tts_sherpa_lexicon: str | None
    tts_sherpa_data_dir: str | None
    tts_sherpa_rule_fsts: str | None
    tts_sherpa_provider: str
    tts_sherpa_num_threads: int
    tts_sherpa_sid: int
    tts_sherpa_speed: float
    tts_cache_enabled: bool
    tts_cache_max_entries: int
    tts_timeout_seconds: float


def _optional_env(name: str) -> str | None:
    value = os.getenv(name, "").strip()
    return value or None


def get_settings() -> Settings:
    return Settings(
        host=os.getenv("ROSIE_REALTIME_HOST", "127.0.0.1"),
        port=int(os.getenv("ROSIE_REALTIME_PORT", "8020")),
        reload=os.getenv("ROSIE_REALTIME_RELOAD", "false").lower() == "true",
        prewarm_enabled=os.getenv("ROSIE_REALTIME_PREWARM_ENABLED", "true").lower() == "true",
        business_api_url=_optional_env("ROSIE_BUSINESS_API_URL"),
        business_result_enabled=os.getenv("ROSIE_REALTIME_RESULT_ENABLED", "true").lower() == "true",
        business_auto_dispatch_enabled=os.getenv("ROSIE_BUSINESS_AUTO_DISPATCH_ENABLED", "false").lower() == "true",
        business_api_timeout_seconds=float(os.getenv("ROSIE_BUSINESS_API_TIMEOUT_SECONDS", "5")),
        business_result_retry_max_attempts=int(os.getenv("ROSIE_BUSINESS_RESULT_RETRY_MAX_ATTEMPTS", "5")),
        pipeline_provider=os.getenv("ROSIE_REALTIME_PIPELINE_PROVIDER", "native").strip().lower(),
        pipecat_url=_optional_env("ROSIE_PIPECAT_AGENT_URL"),
        pipecat_timeout_seconds=float(os.getenv("ROSIE_PIPECAT_TIMEOUT_SECONDS", "15")),
        ai_agent_url=_optional_env("ROSIE_AI_AGENT_URL"),
        agent_enabled=os.getenv("ROSIE_REALTIME_AGENT_ENABLED", "true").lower() == "true",
        agent_timeout_seconds=float(os.getenv("ROSIE_REALTIME_AGENT_TIMEOUT_SECONDS", "10")),
        stt_provider=os.getenv("ROSIE_STT_PROVIDER", "none").strip().lower(),
        stt_url=_optional_env("ROSIE_STT_URL"),
        stt_model=os.getenv("ROSIE_STT_MODEL", "iic/SenseVoiceSmall").strip(),
        stt_vad_model=_optional_env("ROSIE_STT_VAD_MODEL"),
        stt_language=os.getenv("ROSIE_STT_LANGUAGE", "zh").strip(),
        stt_timeout_seconds=float(os.getenv("ROSIE_STT_TIMEOUT_SECONDS", "10")),
        stt_min_audio_bytes=int(os.getenv("ROSIE_STT_MIN_AUDIO_BYTES", "32000")),
        tts_provider=os.getenv("ROSIE_TTS_PROVIDER", "none").strip().lower(),
        tts_url=_optional_env("ROSIE_TTS_URL"),
        tts_voice=os.getenv("ROSIE_TTS_VOICE", "zh-CN-XiaoxiaoNeural").strip(),
        tts_rate=os.getenv("ROSIE_TTS_RATE", "+0%").strip(),
        tts_pitch=os.getenv("ROSIE_TTS_PITCH", "+0Hz").strip(),
        tts_ffmpeg_path=os.getenv("ROSIE_TTS_FFMPEG_PATH", "ffmpeg").strip(),
        tts_sapi_voice=_optional_env("ROSIE_TTS_SAPI_VOICE"),
        tts_sapi_rate=int(os.getenv("ROSIE_TTS_SAPI_RATE", "0")),
        tts_sherpa_model_type=os.getenv("ROSIE_TTS_SHERPA_MODEL_TYPE", "vits").strip().lower(),
        tts_sherpa_model=_optional_env("ROSIE_TTS_SHERPA_MODEL"),
        tts_sherpa_tokens=_optional_env("ROSIE_TTS_SHERPA_TOKENS"),
        tts_sherpa_lexicon=_optional_env("ROSIE_TTS_SHERPA_LEXICON"),
        tts_sherpa_data_dir=_optional_env("ROSIE_TTS_SHERPA_DATA_DIR"),
        tts_sherpa_rule_fsts=_optional_env("ROSIE_TTS_SHERPA_RULE_FSTS"),
        tts_sherpa_provider=os.getenv("ROSIE_TTS_SHERPA_PROVIDER", "cpu").strip().lower(),
        tts_sherpa_num_threads=int(os.getenv("ROSIE_TTS_SHERPA_NUM_THREADS", "2")),
        tts_sherpa_sid=int(os.getenv("ROSIE_TTS_SHERPA_SID", "0")),
        tts_sherpa_speed=float(os.getenv("ROSIE_TTS_SHERPA_SPEED", "1.0")),
        tts_cache_enabled=os.getenv("ROSIE_TTS_CACHE_ENABLED", "true").lower() == "true",
        tts_cache_max_entries=int(os.getenv("ROSIE_TTS_CACHE_MAX_ENTRIES", "128")),
        tts_timeout_seconds=float(os.getenv("ROSIE_TTS_TIMEOUT_SECONDS", "10")),
    )
