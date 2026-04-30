from dataclasses import dataclass
import os
from pathlib import Path


@dataclass(frozen=True)
class Settings:
    host: str
    port: int
    reload: bool
    db_path: Path
    default_access_number: str
    default_merchant_id: str
    default_merchant_name: str
    default_transfer_phone: str | None
    public_base_url: str | None
    wecom_webhook_url: str | None
    business_api_url: str | None
    ai_agent_url: str | None
    use_ai_greeting: bool
    use_ai_extract: bool
    ai_timeout_seconds: float
    realtime_listen_enabled: bool
    realtime_ws_url: str | None
    realtime_action_hook: str | None


def _optional_env(name: str) -> str | None:
    value = os.getenv(name, "").strip()
    return value or None


def get_settings() -> Settings:
    return Settings(
        host=os.getenv("ROSIE_HOST", "127.0.0.1"),
        port=int(os.getenv("ROSIE_PORT", "8000")),
        reload=os.getenv("ROSIE_RELOAD", "false").lower() == "true",
        db_path=Path(os.getenv("ROSIE_DB_PATH", "./data/rosie_mvp.sqlite3")),
        default_access_number=os.getenv("ROSIE_DEFAULT_ACCESS_NUMBER", "8613736849910"),
        default_merchant_id=os.getenv("ROSIE_DEFAULT_MERCHANT_ID", "demo-merchant"),
        default_merchant_name=os.getenv("ROSIE_DEFAULT_MERCHANT_NAME", "测试商家"),
        default_transfer_phone=_optional_env("ROSIE_DEFAULT_TRANSFER_PHONE"),
        public_base_url=_optional_env("ROSIE_PUBLIC_BASE_URL"),
        wecom_webhook_url=_optional_env("ROSIE_WECOM_WEBHOOK_URL"),
        business_api_url=_optional_env("ROSIE_BUSINESS_API_URL"),
        ai_agent_url=_optional_env("ROSIE_AI_AGENT_URL"),
        use_ai_greeting=os.getenv("ROSIE_USE_AI_GREETING", "false").lower() == "true",
        use_ai_extract=os.getenv("ROSIE_USE_AI_EXTRACT", "false").lower() == "true",
        ai_timeout_seconds=float(os.getenv("ROSIE_AI_TIMEOUT_SECONDS", "10")),
        realtime_listen_enabled=os.getenv("ROSIE_REALTIME_LISTEN_ENABLED", "false").lower() == "true",
        realtime_ws_url=_optional_env("ROSIE_REALTIME_WS_URL"),
        realtime_action_hook=_optional_env("ROSIE_REALTIME_ACTION_HOOK"),
    )
