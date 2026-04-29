# Rosie Realtime Voice MVP

这是 Phase 3 的第一块：实时语音链路底座。

它不是语音留言箱。目标是验证：

```text
jambonz listen
  -> WebSocket
  -> Rosie realtime-voice
  -> 收到实时 PCM 音频帧
```

后续会在这个服务内接入：

- STT：FunASR / SenseVoice 或临时云 STT
- LLM：ai-agent
- TTS：Fun-CosyVoice / 临时云 TTS
- Pipecat：实时语音 Agent pipeline

## 启动

```bash
cd services/realtime-voice
cp .env.example .env
docker compose up -d --build
```

健康检查：

```bash
curl http://127.0.0.1:8020/health
curl http://127.0.0.1:8020/sessions
```

## jambonz listen 配置

`call-webhook` 打开：

```env
ROSIE_REALTIME_LISTEN_ENABLED=true
ROSIE_REALTIME_WS_URL=ws://服务器IP:8020/ws/jambonz/audio
ROSIE_REALTIME_ACTION_HOOK=http://服务器IP:8000/webhooks/jambonz/listen-complete
```

公网联调时需要把 `ws://服务器IP:8020` 改成 jambonz 可访问的地址。

