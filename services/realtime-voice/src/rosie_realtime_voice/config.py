from dataclasses import dataclass
import os


@dataclass(frozen=True)
class Settings:
    host: str
    port: int
    reload: bool


def get_settings() -> Settings:
    return Settings(
        host=os.getenv("ROSIE_REALTIME_HOST", "127.0.0.1"),
        port=int(os.getenv("ROSIE_REALTIME_PORT", "8020")),
        reload=os.getenv("ROSIE_REALTIME_RELOAD", "false").lower() == "true",
    )

