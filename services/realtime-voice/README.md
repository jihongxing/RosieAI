# Rosie Realtime Voice MVP

这是 Phase 3 的第一块：实时语音链路底座。

它不是语音留言箱。目标是验证：

```text
jambonz listen
  -> WebSocket
  -> Rosie realtime-voice
  -> 收到实时 PCM 音频帧
  -> 读取 call-webhook metadata 中的商家 system_prompt
  -> STT 转写客户语音
  -> 调用 ai-agent 生成下一句回复
  -> TTS 合成电话侧音频并通过 WebSocket 发回 jambonz
```

后续会在这个服务内接入：

- STT：FunASR / SenseVoice 或临时云 STT
- LLM：ai-agent
- TTS：Fun-CosyVoice / 临时云 TTS
- Pipecat：实时语音 Agent pipeline

当前已经建立 Pipecat-ready session context：`call-webhook` 写入 listen metadata 的 `system_prompt`、`merchant_profile`、`industry_template` 会被实时服务解析并保存在 `/sessions`。服务已支持 STT / ai-agent / TTS turn pipeline：收到 PCM 音频达到阈值后调用 STT，拿到最终转写后调用 `ai-agent /chat`，再调用 TTS，把生成的电话侧 PCM bytes 通过同一个 WebSocket 发回 jambonz。

当前支持三种运行方式：

- `native`：在 `realtime-voice` 内直接编排 STT -> `ai-agent` -> TTS。
- `pipecat_http`：把同一份 session context、history 和音频转交给外部 Pipecat worker，由 Pipecat worker 返回 transcript / reply / audio。
- 文本帧模拟：真实音频服务未就绪时仍可用文本帧验证 AI 回复和 TTS 回写。

## 启动

```bash
cd services/realtime-voice
cp .env.example .env
docker compose up -d --build
```

构建带 FunASR / Edge TTS / Pipecat 依赖的语音 worker 镜像：

```bash
docker build --build-arg INSTALL_VOICE_MODELS=true -t rosie-realtime-voice:voice .
```

健康检查：

```bash
curl http://127.0.0.1:8020/health
curl http://127.0.0.1:8020/sessions
```

`/health` 中的 `ready=true` 表示模型预热完成。默认 `ROSIE_REALTIME_PREWARM_ENABLED=true`，服务启动时会加载 STT / TTS 模型；生产环境应等待 `ready=true` 后再接入电话流量。

连接 `ai-agent`：

```env
ROSIE_REALTIME_AGENT_ENABLED=true
ROSIE_AI_AGENT_URL=http://127.0.0.1:8010
ROSIE_REALTIME_AGENT_TIMEOUT_SECONDS=10
```

连接 STT / TTS HTTP 服务：

```env
ROSIE_REALTIME_PIPELINE_PROVIDER=native

ROSIE_STT_PROVIDER=http
ROSIE_STT_URL=http://127.0.0.1:8050/transcribe
ROSIE_STT_TIMEOUT_SECONDS=10
ROSIE_STT_MIN_AUDIO_BYTES=32000

ROSIE_TTS_PROVIDER=http
ROSIE_TTS_URL=http://127.0.0.1:8060/synthesize
ROSIE_TTS_TIMEOUT_SECONDS=10
```

`ROSIE_STT_MIN_AUDIO_BYTES` 默认 32000，约等于 16kHz 16-bit mono PCM 的 1 秒音频。真实 Pipecat / VAD 接入后可以由端点事件触发 flush，而不是固定按字节阈值切段。

本地 FunASR / SenseVoice STT：

```powershell
cd services\realtime-voice
..\..\.venv\Scripts\python -m pip install -r requirements-voice-models.txt
$env:ROSIE_STT_PROVIDER="funasr"
$env:ROSIE_STT_MODEL="iic/SenseVoiceSmall"
$env:ROSIE_STT_LANGUAGE="zh"
```

本地 Edge TTS 到电话侧 PCM：

```powershell
cd services\realtime-voice
..\..\.venv\Scripts\python -m pip install -r requirements-voice-models.txt
$env:ROSIE_TTS_PROVIDER="edge"
$env:ROSIE_TTS_VOICE="zh-CN-XiaoxiaoNeural"
$env:ROSIE_TTS_FFMPEG_PATH="ffmpeg"
```

`edge-tts` 生成的是压缩音频，服务会调用 ffmpeg 转成 jambonz 电话侧使用的 16-bit mono PCM。部署机器需要能执行 `ffmpeg`，或通过 `ROSIE_TTS_FFMPEG_PATH` 指向完整路径。

本机 Windows SAPI 低延迟动态 TTS：

```powershell
.\scripts\stop-local-voice-worker.ps1
.\scripts\start-local-voice-worker.ps1 -TtsProvider sapi -SapiVoice "Microsoft Huihui Desktop" -SapiRate 1
.\scripts\smoke-local-voice.ps1 -CallSid sapi-smoke -Turns 4
```

```env
ROSIE_TTS_PROVIDER=sapi
ROSIE_TTS_SAPI_VOICE=Microsoft Huihui Desktop
ROSIE_TTS_SAPI_RATE=1
```

SAPI 是本机动态 TTS，不依赖预测缓存，适合验证“未知文本也要低延迟”的链路。它的音质不如 Edge / CosyVoice，但本机 smoke 中任意中文短句 TTS 约 0.45-0.93s；整条 FunASR STT -> fallback agent -> SAPI TTS 的 warm turn 约 1.25-1.49s。

本机 sherpa-onnx 中文 VITS TTS：

```powershell
.\scripts\download-sherpa-onnx-zh-tts.ps1
.\scripts\stop-local-voice-worker.ps1
.\scripts\start-local-voice-worker.ps1 `
  -TtsProvider sherpa_onnx `
  -SherpaModelDir "D:\codeSpace\RosieAI\data\models\tts\vits-icefall-zh-aishell3" `
  -SherpaSpeakerId 0 `
  -SherpaSpeed 1.0
.\scripts\smoke-local-voice.ps1 -CallSid sherpa-smoke -Turns 4
```

```env
ROSIE_TTS_PROVIDER=sherpa_onnx
ROSIE_TTS_SHERPA_MODEL_TYPE=vits
ROSIE_TTS_SHERPA_MODEL=D:\codeSpace\RosieAI\data\models\tts\vits-icefall-zh-aishell3\model.onnx
ROSIE_TTS_SHERPA_TOKENS=D:\codeSpace\RosieAI\data\models\tts\vits-icefall-zh-aishell3\tokens.txt
ROSIE_TTS_SHERPA_LEXICON=D:\codeSpace\RosieAI\data\models\tts\vits-icefall-zh-aishell3\lexicon.txt
ROSIE_TTS_SHERPA_PROVIDER=cpu
ROSIE_TTS_SHERPA_NUM_THREADS=2
ROSIE_TTS_SHERPA_SID=0
ROSIE_TTS_SHERPA_SPEED=1.0
```

sherpa-onnx 是当前本地动态 TTS 主线。它不依赖预测缓存，本机使用 `vits-icefall-zh-aishell3` 模型，动态短句 TTS warm 约 0.32-0.37s；整条 FunASR STT -> fallback agent -> sherpa-onnx TTS 的 warm turn 约 0.95-1.08s。

接入真实 `ai-agent` HTTP 链路：

```powershell
.\services\ai-agent\scripts\start-local-ai-agent.ps1 -Provider local_fallback -Model local-fallback
.\services\realtime-voice\scripts\stop-local-voice-worker.ps1
.\services\realtime-voice\scripts\start-local-voice-worker.ps1 `
  -TtsProvider sherpa_onnx `
  -SherpaModelDir "D:\codeSpace\RosieAI\data\models\tts\vits-icefall-zh-aishell3" `
  -AiAgentUrl "http://127.0.0.1:8010"
.\services\realtime-voice\scripts\smoke-local-voice.ps1 -CallSid ai-agent-smoke -Turns 3
```

如果本机已运行 Ollama / Qwen，可把第一行改成：

```powershell
.\services\ai-agent\scripts\start-local-ai-agent.ps1 -Provider ollama -Model qwen3:4b
```

2026-04-30 ai-agent HTTP 基线：预热 ai-agent HTTP client 后，`local_fallback` provider 的 `agent_ms` 为 7-10ms，整条 FunASR -> ai-agent HTTP -> sherpa-onnx TTS 链路为 0.88-1.08s。这个数字只代表 ai-agent HTTP 链路和规则兜底，不代表真实 Qwen 推理耗时；接入 Ollama / Qwen 后主要观察 `agent_ms`。

本机启动真实语音 worker：

```powershell
.\scripts\start-local-voice-worker.ps1
curl http://127.0.0.1:8020/health
```

本机端到端 smoke test：

```powershell
.\scripts\smoke-local-voice.ps1 -CallSid smoke-001 -Turns 3 -MaxTotalMs 1500
curl http://127.0.0.1:8020/sessions
curl "http://127.0.0.1:8020/latency-report?max_total_ms=1500"
```

一条命令生成 Phase 3 延迟基线：

```powershell
powershell -ExecutionPolicy Bypass -File ..\..\ops\phase3-latency-baseline.ps1 `
  -TtsProvider sherpa_onnx `
  -Turns 2 `
  -MaxTotalMs 1500
```

`/latency-report` 会聚合最近 turns 的 `stt_ms`、`agent_ms`、`tts_ms` 和 `total_ms`，返回 p50、p95、max、慢 turn 数量以及各 provider source 分布。`status=ok` 表示 `total_ms.p95` 没超过 `max_total_ms`；`status=degraded` 表示真实电话侧联调需要继续拆分 STT / Agent / TTS 瓶颈。`smoke-local-voice.ps1` 会在发送音频后自动读取同一份报告，可用 `-ReportPath data\runtime\voice-latency-report.json` 落盘。

2026-04-30 本机基线：SenseVoiceSmall 首次权重下载约 893MB；模型冷加载后首个 turn 约 13s，其中 STT 模型加载约 10s，Edge TTS 约 2.7s。启用默认 TTS 内存缓存后，同一句回复的 warm turn 约 0.6-0.7s，TTS 来源为 `edge_tts_cache`；STT RTF 约 0.10。当前未连接外部 `ai-agent` 时，回复来源为 `local_fallback`。

2026-04-30 动态 TTS 基线：`ROSIE_TTS_PROVIDER=sapi` 时，不使用 TTS cache，warm turn 为 1.25-1.49s；分段约为 STT 0.70-0.88s，SAPI TTS 0.50-0.58s。

2026-04-30 sherpa-onnx 基线：`ROSIE_TTS_PROVIDER=sherpa_onnx` 时，不使用 TTS cache，warm turn 为 0.95-1.08s；分段约为 STT 0.61-0.71s，sherpa-onnx TTS 0.33-0.37s。启用启动预热后，模型加载约 10.37s 发生在 startup，`ready=true` 后第一通 WebSocket smoke 为 1.08s，后续约 1.11-1.22s。

2026-04-30 Phase 3 基线脚本实测：`ops/phase3-latency-baseline.ps1 -TtsProvider sherpa_onnx -Turns 2 -MaxTotalMs 1500` 返回 `status=ok`，预热 12.41s，`total_ms.p50=1048`、`total_ms.p95=1151`、`total_ms.max=1163`；分段 p95 为 STT 869ms、ai-agent 10ms、sherpa-onnx TTS 275ms。报告落盘到 `data/runtime/phase3-latency-20260430-154545.json`。

TTS 缓存配置：

```env
ROSIE_TTS_CACHE_ENABLED=true
ROSIE_TTS_CACHE_MAX_ENTRIES=128
```

## Pipecat worker 契约

如果要把主线交给 Pipecat worker：

```env
ROSIE_REALTIME_PIPELINE_PROVIDER=pipecat_http
ROSIE_PIPECAT_AGENT_URL=http://127.0.0.1:8070
ROSIE_PIPECAT_TIMEOUT_SECONDS=15
```

`realtime-voice` 会调用：

```text
POST /turn
```

音频请求：

```json
{
  "input_type": "audio",
  "audio_base64": "<raw pcm base64>",
  "sample_rate": 16000,
  "session": {
    "session_id": "call_sid",
    "merchant_id": "demo-merchant",
    "system_prompt": "..."
  },
  "history": []
}
```

文本请求：

```json
{
  "input_type": "text",
  "text": "你好，我想预约明天下午剪头发",
  "sample_rate": 16000,
  "session": {
    "session_id": "call_sid",
    "merchant_id": "demo-merchant",
    "system_prompt": "..."
  },
  "history": []
}
```

Pipecat worker 响应：

```json
{
  "transcript": "客户想预约剪发",
  "reply": "请问您几点到店？",
  "audio_base64": "<16kHz s16le mono pcm base64>",
  "content_type": "audio/L16;rate=16000",
  "stt_source": "pipecat_stt",
  "tts_source": "pipecat_tts"
}
```

返回后 `realtime-voice` 会继续负责 session 统计和 WebSocket 音频回写。

## jambonz listen 配置

`call-webhook` 打开：

```env
ROSIE_REALTIME_LISTEN_ENABLED=true
ROSIE_REALTIME_WS_URL=ws://服务器IP:8020/ws/jambonz/audio
ROSIE_REALTIME_ACTION_HOOK=http://服务器IP:8000/webhooks/jambonz/listen-complete
```

公网联调时需要把 `ws://服务器IP:8020` 改成 jambonz 可访问的地址。

## STT / TTS HTTP 契约

STT 请求：

```text
POST /transcribe?sample_rate=16000
Content-Type: audio/L16
X-Rosie-Session-ID: call_sid
X-Rosie-Merchant-ID: merchant_id

<raw pcm bytes>
```

STT 响应：

```json
{"transcript":"客户想预约剪发","is_final":true}
```

TTS 请求：

```json
{
  "text": "请问您几点到店？",
  "sample_rate": 16000,
  "session": {
    "merchant_id": "demo-merchant",
    "system_prompt": "..."
  }
}
```

TTS 可以直接返回 `audio/*` 或 `application/octet-stream` bytes；也可以返回 JSON：

```json
{"audio_base64":"...","content_type":"audio/L16;rate=16000"}
```

## 本地模拟 STT 文本帧

真实 STT 服务未启动时，可以向 WebSocket 发送：

```json
{"transcript":"你好，我想预约明天下午剪头发"}
```

返回示例：

```json
{
  "type": "agent_reply",
  "source": "ai_agent",
  "model": "deepseek-chat",
  "reply": "您好，我先帮您记录预约，请问您明天下午几点到店？"
}
```

如果已配置 TTS，文本帧也会触发 TTS，并在 JSON 回复后追加发送音频 bytes。
