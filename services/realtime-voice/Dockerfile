FROM python:3.12-slim

WORKDIR /app

ENV PYTHONDONTWRITEBYTECODE=1
ENV PYTHONUNBUFFERED=1
ENV PYTHONPATH=/app/src

COPY requirements.txt .
RUN pip install --no-cache-dir --disable-pip-version-check -r requirements.txt
COPY requirements-voice-models.txt .
ARG INSTALL_VOICE_MODELS=false
RUN if [ "$INSTALL_VOICE_MODELS" = "true" ]; then \
      apt-get update \
      && apt-get install -y --no-install-recommends ffmpeg \
      && rm -rf /var/lib/apt/lists/* \
      && pip install --no-cache-dir --disable-pip-version-check -r requirements-voice-models.txt; \
    fi

COPY src ./src

EXPOSE 8020

CMD ["python", "-m", "uvicorn", "rosie_realtime_voice.app:app", "--host", "0.0.0.0", "--port", "8020"]
