import os

from rosie_ai_agent.config import get_settings


def test_deepseek_config_from_env(monkeypatch):
    monkeypatch.setenv("ROSIE_LLM_PROVIDER", "openai_compatible")
    monkeypatch.setenv("ROSIE_OPENAI_BASE_URL", "https://api.deepseek.com")
    monkeypatch.setenv("ROSIE_OPENAI_API_KEY", "test-key")
    monkeypatch.setenv("ROSIE_LLM_MODEL", "deepseek-chat")

    settings = get_settings()

    assert settings.provider == "openai_compatible"
    assert settings.openai_base_url == "https://api.deepseek.com"
    assert settings.openai_api_key == "test-key"
    assert settings.model == "deepseek-chat"

