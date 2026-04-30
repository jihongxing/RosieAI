# Rosie Call Webhook MVP

最小 MVP 服务，用于 Phase 1 通信 POC。

目标：

- 接收 jambonz inbound call webhook。
- 根据被叫号码匹配 Rosie 幕后接入号。
- 写入 SQLite 通话记录。
- 返回 jambonz JSON verbs，播放固定欢迎语并挂断。
- 接收 jambonz call status webhook。

当前版本仍不在 call-webhook 内直接跑 Pipecat / STT / TTS；它负责呼叫控制、商家路由和 listen metadata。实时音频由 `services/realtime-voice` 承接，并在那里执行 STT -> `ai-agent` -> TTS turn pipeline。

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
| `ROSIE_CALL_NOTICE_ENABLED` | `true` | 是否在入站欢迎语前播放 AI 接待和摘要用途告知 |
| `ROSIE_CALL_NOTICE_TEXT` | `本通话将由 AI 接待并生成文字摘要，用于通知商家跟进。` | 试点合规告知文案；启用 realtime listen 时会同时写入 metadata |
| `ROSIE_BUSINESS_API_URL` | 空 | Go 正式业务后端地址；设置后读取 `/merchant-profile` 的 `system_prompt` |
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
ROSIE_BUSINESS_API_URL=http://host.docker.internal:8030
ROSIE_AI_AGENT_URL=http://host.docker.internal:8010
ROSIE_AI_TIMEOUT_SECONDS=10
```

如果你不用 Docker、直接在宿主机跑 call-webhook：

```env
ROSIE_USE_AI_GREETING=true
ROSIE_BUSINESS_API_URL=http://127.0.0.1:8030
ROSIE_AI_AGENT_URL=http://127.0.0.1:8010
ROSIE_AI_TIMEOUT_SECONDS=10
```

配置 `ROSIE_BUSINESS_API_URL` 后，call-webhook 会按 `merchant_id` 读取 Go API 的 `/merchant-profile`，把返回的 `system_prompt` 传给 `ai-agent /chat`、`ai-agent /extract`，并写入 realtime listen metadata，后续 Pipecat / STT / TTS 链路可直接复用。AI greeting 或 Go API 读取失败时会自动降级为固定欢迎语，不会阻塞电话接听。

## 试点告知

默认开启 `ROSIE_CALL_NOTICE_ENABLED=true`。每通入站电话都会在欢迎语前加入 `ROSIE_CALL_NOTICE_TEXT`，避免 AI greeting 或固定欢迎语绕过身份/摘要用途告知。启用实时语音时，同一文案也会写入 listen metadata 的 `call_notice` 字段，便于之后按 call_sid 审计。

## 接入实时语音 WebSocket

这不是语音留言，而是后续实时对话的底座：jambonz 会通过 `listen` verb 把通话音频实时推到 `realtime-voice`，并允许我们把音频实时送回电话侧。

```env
ROSIE_REALTIME_LISTEN_ENABLED=true
ROSIE_REALTIME_WS_URL=ws://你的服务器:8020/ws/jambonz/audio
ROSIE_REALTIME_ACTION_HOOK=http://你的服务器:8000/webhooks/jambonz/listen-complete
```

`realtime-voice` 已支持消费 listen metadata、读取商家 `system_prompt`、调用 STT / `ai-agent` / TTS，并通过同一个 WebSocket 回写音频。真实 STT / TTS 服务通过 `services/realtime-voice` 的 HTTP provider 配置接入。

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

生成正式汇总：

```bash
curl -X POST http://127.0.0.1:8000/digests/generate
curl http://127.0.0.1:8000/digests
```

`/digests/preview` 会返回 `digest_text`，用于预览每日汇总正文。同一个 `call_sid` 重复提交时会更新原有 transcript、summary 和 inbox item，避免重试造成重复待办。`/digests/generate` 会把本批收件箱条目标记为 `digested`，后续预览不会重复汇总。

定时汇总触发器：

```bash
curl -X POST "http://127.0.0.1:8000/internal/digest-tick"
curl -X POST "http://127.0.0.1:8000/internal/digest-tick?now=2026-04-30T20:00:00"
curl http://127.0.0.1:8000/notification-logs
```

服务器 cron 可以每分钟调用 `/internal/digest-tick`。服务会按商家的通知偏好判断是否到点，到点后生成正式汇总并写入 `notification_logs`。同一商家同一天同一时间重复触发会命中同一条 `idempotency_key`，不会重复生成待发送通知。

当前通知只进入发送日志：

- 默认渠道：`wechat_subscription`
- 启用团队企业微信群偏好时：`wecom_robot`
- 待发送状态：`queued`
- 没有待汇总事项时：`skipped`

## 通知偏好

默认策略：

- `digest_mode`: `daily`
- `digest_times`: `["20:00"]`
- `realtime_enabled`: `false`
- `urgent_realtime_enabled`: `true`
- `team_wecom_enabled`: `false`
- `sms_fallback_enabled`: `false`

查看：

```bash
curl http://127.0.0.1:8000/notification-preferences
```

改成午间 + 晚间两次汇总：

```bash
curl -X PUT http://127.0.0.1:8000/notification-preferences \
  -H "Content-Type: application/json" \
  -d '{
    "digest_mode": "twice_daily",
    "digest_times": ["12:00", "20:00"],
    "realtime_enabled": false,
    "urgent_realtime_enabled": true,
    "team_wecom_enabled": false,
    "sms_fallback_enabled": false,
    "quiet_hours_start": "22:00",
    "quiet_hours_end": "08:00"
  }'
```

如果配置：

```env
ROSIE_USE_AI_EXTRACT=true
ROSIE_BUSINESS_API_URL=http://127.0.0.1:8030
ROSIE_AI_AGENT_URL=http://127.0.0.1:8010
```

则摘要优先走 `ai-agent /extract`，并携带 Go 商家配置生成的 `system_prompt`；失败时自动降级到本地规则。

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
