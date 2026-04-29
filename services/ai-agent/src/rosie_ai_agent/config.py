from dataclasses import dataclass
import os


@dataclass(frozen=True)
class Settings:
    host: str
    port: int
    reload: bool
    ollama_base_url: str
    model: str
    timeout_seconds: float


def get_settings() -> Settings:
    return Settings(
        host=os.getenv("ROSIE_AI_HOST", "127.0.0.1"),
        port=int(os.getenv("ROSIE_AI_PORT", "8010")),
        reload=os.getenv("ROSIE_AI_RELOAD", "false").lower() == "true",
        ollama_base_url=os.getenv("ROSIE_OLLAMA_BASE_URL", "http://127.0.0.1:11434").rstrip("/"),
        model=os.getenv("ROSIE_LLM_MODEL", "qwen3:8b"),
        timeout_seconds=float(os.getenv("ROSIE_LLM_TIMEOUT_SECONDS", "60")),
    )

