# Rosie Call Webhook MVP

最小 MVP 服务，用于 Phase 1 通信 POC。

目标：

- 接收 jambonz inbound call webhook。
- 根据被叫号码匹配 Rosie 幕后接入号。
- 写入 SQLite 通话记录。
- 返回 jambonz JSON verbs，播放固定欢迎语并挂断。
- 接收 jambonz call status webhook。

当前版本不包含 Pipecat / STT / LLM / TTS。它只验证真实电话能进入 Rosie 后端。

## 快速启动

```bash
cd services/call-webhook
python -m venv .venv
.venv\Scripts\activate
pip install -r requirements.txt
python -m rosie_call_webhook
```

服务默认监听：

```text
http://127.0.0.1:8000
```

## 环境变量

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `ROSIE_DB_PATH` | `./data/rosie_mvp.sqlite3` | SQLite 数据库路径 |
| `ROSIE_DEFAULT_ACCESS_NUMBER` | `8613736849910` | jambonz 测试号码，对应 payload 里的 `to` |
| `ROSIE_DEFAULT_MERCHANT_ID` | `demo-merchant` | 测试商家 ID |
| `ROSIE_DEFAULT_MERCHANT_NAME` | `测试商家` | 测试商家名称 |
| `ROSIE_DEFAULT_TRANSFER_PHONE` | 空 | 后续转人工使用，当前 MVP 只保存 |
| `ROSIE_PUBLIC_BASE_URL` | 空 | 未来生成 actionHook 使用 |
| `ROSIE_WECOM_WEBHOOK_URL` | 空 | 可选企业微信机器人通知 |
| `ROSIE_USE_AI_GREETING` | `false` | 是否调用 `ai-agent` 生成欢迎语 |
| `ROSIE_USE_AI_EXTRACT` | `false` | 是否调用 `ai-agent /extract` 生成来电摘要，关闭时使用本地规则兜底 |
| `ROSIE_AI_AGENT_URL` | `http://127.0.0.1:8010` | ai-agent 地址 |
| `ROSIE_AI_TIMEOUT_SECONDS` | `10` | 调用 ai-agent 超时时间 |
| `ROSIE_REALTIME_LISTEN_ENABLED` | `false` | 是否启用 jambonz 实时音频 websocket |
| `ROSIE_REALTIME_WS_URL` | 空 | realtime-voice WebSocket 地址 |
| `ROSIE_REALTIME_ACTION_HOOK` | 空 | listen 结束后的回调地址 |

可以复制 `.env.example`：

```powershell
Copy-Item .env.example .env
```

## Docker 启动

```powershell
cd services\call-webhook
Copy-Item .env.example .env
docker compose up --build
```

## 接入 ai-agent

如果 `ai-agent` 已经在宿主机 `8010` 端口运行：

```env
ROSIE_USE_AI_GREETING=true
ROSIE_AI_AGENT_URL=http://host.docker.internal:8010
ROSIE_AI_TIMEOUT_SECONDS=10
```

如果你不用 Docker、直接在宿主机跑 call-webhook：

```env
ROSIE_USE_AI_GREETING=true
ROSIE_AI_AGENT_URL=http://127.0.0.1:8010
ROSIE_AI_TIMEOUT_SECONDS=10
```

AI greeting 失败时会自动降级为固定欢迎语，不会阻塞电话接听。

## 接入实时语音 WebSocket

这不是语音留言，而是后续实时对话的底座：jambonz 会通过 `listen` verb 把通话音频实时推到 `realtime-voice`，并允许我们把音频实时送回电话侧。

```env
ROSIE_REALTIME_LISTEN_ENABLED=true
ROSIE_REALTIME_WS_URL=ws://你的服务器:8020/ws/jambonz/audio
ROSIE_REALTIME_ACTION_HOOK=http://你的服务器:8000/webhooks/jambonz/listen-complete
```

当前第一版只验证实时音频 WebSocket 是否能建立和收到 PCM 音频。下一步会把它接到 STT/TTS/Pipecat。

## jambonz 配置

在 jambonz application 中配置 calling webhook：

```text
POST https://your-domain/webhooks/jambonz/call
```

配置 call status webhook：

```text
POST https://your-domain/webhooks/jambonz/status
```

返回示例：

```json
[
  {
    "verb": "say",
    "text": "您好，这里是测试商家的 AI 接线员 Rosie..."
  },
  {
    "verb": "pause",
    "length": 1
  },
  {
    "verb": "hangup"
  }
]
```

## 本地测试

使用脚本：

```powershell
services\call-webhook\scripts\test-webhook.ps1
```

或者手动调用：

```bash
curl -X POST http://127.0.0.1:8000/webhooks/jambonz/call ^
  -H "Content-Type: application/json" ^
  -d "{\"callSid\":\"call-1\",\"from\":\"+8613811112222\",\"to\":\"8613736849910\",\"callStatus\":\"trying\"}"
```

查看通话记录：

```bash
curl http://127.0.0.1:8000/calls
```

## 业务层 MVP：模拟转写到收件箱

真实 PSTN 线路和 STT 未就绪时，可以用文本模拟一通来电结果。服务会写入：

- `calls`
- `call_transcripts`
- `call_summaries`
- `inbox_items`

```bash
curl -X POST http://127.0.0.1:8000/simulate/call-result \
  -H "Content-Type: application/json" \
  -d '{
    "call_sid": "sim-call-1",
    "from_number": "+8613811112222",
    "to_number": "8613736849910",
    "transcript": "你好，我想预约明天下午三点剪头发，我姓王。"
  }'
```

查看小程序收件箱数据：

```bash
curl http://127.0.0.1:8000/inbox
```

预览待汇总内容：

```bash
curl http://127.0.0.1:8000/digests/preview
```

`/digests/preview` 会返回 `digest_text`，用于预览每日汇总正文。同一个 `call_sid` 重复提交时会更新原有 transcript、summary 和 inbox item，避免重试造成重复待办。

如果配置：

```env
ROSIE_USE_AI_EXTRACT=true
ROSIE_AI_AGENT_URL=http://127.0.0.1:8010
```

则摘要优先走 `ai-agent /extract`；失败时自动降级到本地规则。

## 新增商家幕后接入号

```powershell
$body = @{
  merchant_id = "merchant-001"
  merchant_name = "张三理发店"
  access_number = "+8617000000001"
  original_number = "+8613812345678"
  transfer_phone = "+8613812345678"
  enabled = $true
} | ConvertTo-Json

Invoke-RestMethod `
  -Uri "http://127.0.0.1:8000/merchants" `
  -Method Post `
  -ContentType "application/json" `
  -Body $body
```

查看商家：

```powershell
Invoke-RestMethod -Uri "http://127.0.0.1:8000/merchants"
```

## Phase 1 验收

- 手机能打进 jambonz。
- jambonz 能调用 `/webhooks/jambonz/call`。
- 服务能根据 `to` 字段匹配 Rosie 幕后接入号。
- 电话侧能听到固定欢迎语。
- `/calls` 能看到通话记录。
- `/webhooks/jambonz/status` 能记录状态事件。
