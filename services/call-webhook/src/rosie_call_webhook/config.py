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


def _optional_env(name: str) -> str | None:
    value = os.getenv(name, "").strip()
    return value or None


def get_settings() -> Settings:
    return Settings(
        host=os.getenv("ROSIE_HOST", "127.0.0.1"),
        port=int(os.getenv("ROSIE_PORT", "8000")),
        reload=os.getenv("ROSIE_RELOAD", "false").lower() == "true",
        db_path=Path(os.getenv("ROSIE_DB_PATH", "./data/rosie_mvp.sqlite3")),
        default_access_number=os.getenv("ROSIE_DEFAULT_ACCESS_NUMBER", "+8617000000000"),
        default_merchant_id=os.getenv("ROSIE_DEFAULT_MERCHANT_ID", "demo-merchant"),
        default_merchant_name=os.getenv("ROSIE_DEFAULT_MERCHANT_NAME", "测试商家"),
        default_transfer_phone=_optional_env("ROSIE_DEFAULT_TRANSFER_PHONE"),
        public_base_url=_optional_env("ROSIE_PUBLIC_BASE_URL"),
        wecom_webhook_url=_optional_env("ROSIE_WECOM_WEBHOOK_URL"),
    )

