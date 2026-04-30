# RosieAI

RosieAI 是一个面向中国中小商家和高频电话业务人士的 AI 电话前台项目。

## 产品边界

RosieAI 只做接电话，不做主动外拨电话，不做电销系统。

- 默认只处理客户主动打进来的电话，通过无应答 / 忙线 / 不可及条件呼叫转移接入 Rosie。
- 可以支持“有上下文的回电话”，例如客户刚来电后由老板在小程序手动点击回拨，或商家明确授权的单次服务回拨。
- 坚决不做陌生号码营销、批量外呼、电话销售 CRM、自动获客外呼。
- 产品价值是“帮你不错过客户、少被骚扰”，不是“替你骚扰客户”。

当前仓库包含：

- `docs/`：需求文档、技术方案、项目实施计划。
- `apps/miniprogram/`：微信小程序前端雏形，包含收件箱、通话详情、汇总历史和通知偏好页面。
- `services/api-go/`：正式业务后端起点，承接商家、通话、收件箱、汇总、通知日志和小程序 API。
- `services/call-webhook/`：Phase 1 最小通信 webhook，接收 jambonz 呼入回调。
- `services/ai-agent/`：Phase 2 LLM MVP，当前服务器默认走 DeepSeek / OpenAI-compatible API，本地 Qwen 作为后续资源充足后的降本方向。
- `services/realtime-voice/`：Phase 3 实时音频 WebSocket 骨架，当前用于验证 jambonz `listen` 音频入口。

## 架构决策

2026-04-30 决策：MVP 验证已通过，正式业务后端开始切换到 Go。

- Go 负责正式业务后端：商家、号码、通话、收件箱、汇总、通知日志、小程序 API、支付、审计、后台。
- Python 暂时保留 AI / 语音相关能力：`ai-agent`、后续 STT / TTS / Pipecat、实验性推理链路。
- Python `call-webhook` 保留为已验证 MVP 行为参考和通信联调工具，不再继续承接正式业务功能扩展。
- `services/api-go` 已具备内存 store 和 PostgreSQL store；设置 `ROSIE_DATABASE_URL` 后切到正式数据库。

## 当前推进阶段

Phase 1 通信技术链路已完成：

- jambonz 呼入 webhook。
- SQLite 通话记录。
- 多商家幕后接入号映射。
- Docker 部署。
- 公网 `call-webhook -> ai-agent -> realtime listen verbs` 已验证。
- 真实 PSTN 入站依赖 SIP Trunk / DID / 运营商线路资源，当前归类为商务前置项，不再阻塞业务层 MVP 开发。

2026-04-30 当前检查点：

- 业务层 MVP 已跑通模拟链路：模拟转写文本 -> AI 结构化摘要 -> 小程序收件箱数据 -> 汇总预览 -> 正式汇总生成。
- `call_sid` 幂等已完成，同一通电话重复提交会更新原摘要和收件箱条目，不会重复进入待办。
- 通知偏好 API 已完成，默认策略为每日 20:00 汇总、实时通知关闭、紧急实时提醒开启。
- 定时汇总触发器 `/internal/digest-tick` 已完成，可由 cron 每分钟调用；到点后会幂等生成汇总并写入通知发送日志。
- 通知发送日志 `/notification-logs` 已完成，微信订阅消息发送器已接入，可消费待发送记录并回写发送状态。
- 小程序页面雏形已建立：收件箱、通话详情、汇总历史、通知偏好，默认对接 Go API `http://127.0.0.1:8030`。
- 商家配置接口与小程序页面已建立：店名、号码、地址、营业时间、服务项目、FAQ、预约规则。
- 第一版行业话术模板已建立：理发店 / 美发店、本地生活服务，可生成 AI 系统提示词。
- 真实 PSTN 入站仍等待 SIP Trunk / DID / IMS 线路资源，当前不阻塞业务层开发。

下一步研发目标：

- Go 通话详情接口 `GET /calls/{call_sid}` 已完成，可展示原始通话、转写、摘要和处理状态。
- Go 微信订阅消息发送器 `/internal/notifications/dispatch` 已完成，可消费 `notification_logs` 并回写 `sent` / `failed` 状态。
- Go 小程序登录接口 `/auth/wechat-login` 已完成，可通过 `jscode2session` 绑定商家 openid。
- 商家配置生成的 `system_prompt` 已接入 Python `ai-agent` 和 `call-webhook`，后续实时语音链路可从 listen metadata 复用。
- `realtime-voice` 已能消费 listen metadata，建立带 `system_prompt` 的实时会话上下文，并建立 STT -> `ai-agent` -> TTS turn pipeline；TTS 音频可通过 WebSocket 回写电话侧。
- `realtime-voice` 已新增真实语音 provider 接入：`ROSIE_STT_PROVIDER=funasr` 可接 FunASR / SenseVoice，`ROSIE_TTS_PROVIDER=edge` 可把 Edge TTS 转成电话侧 PCM，`ROSIE_REALTIME_PIPELINE_PROVIDER=pipecat_http` 可把同一份 session context 转交给 Pipecat worker。
- 用微信开发者工具联调 `apps/miniprogram`，补齐页面级错误态、真实订阅消息授权和处理动作。
- 继续建设基于通话上下文的手动回拨审计、真实小程序订阅授权和页面错误态。
- 下一步部署 FunASR / SenseVoice、TTS worker 或 Pipecat worker，做真实电话侧端到端延迟测试。

## 通知策略

Rosie 默认不做逐通实时通知，而是把漏接电话整理成小程序收件箱和定时汇总：

- 所有通话记录、摘要、分类和回拨建议进入小程序收件箱。
- 默认每日 20:00 发送一次汇总；可选午间 + 晚间两次汇总。
- 实时通知默认关闭，只对投诉、紧急事项、明确要求回电等例外场景开启。
- 微信订阅消息用于定时汇总和关键提醒。
- 企业微信群机器人只作为团队商家可选通知渠道，不作为默认开通步骤。
- 短信只做紧急兜底。

## 服务端口

| 服务 | 端口 | 说明 |
|------|------|------|
| call-webhook | 8000 | jambonz 呼入 webhook |
| ai-agent | 8010 | 本地 LLM Agent API |
| realtime-voice | 8020 | jambonz 实时音频 WebSocket |
| api-go | 8030 | 正式业务后端 |

## 业务层 MVP 验证

在真实 PSTN 线路就绪前，正式业务后端使用 Go API 验证“漏接电话整理器”链路：

```powershell
cd services\api-go
$env:ROSIE_DATABASE_URL="postgres://rosie:rosie_dev_password@127.0.0.1:15433/rosie_test?sslmode=disable"
go run .\cmd\rosie-api
```

```bash
curl -X POST http://127.0.0.1:8030/simulate/call-result \
  -H "Content-Type: application/json" \
  -d '{
    "call_sid": "demo-summary-1",
    "from_number": "+8613811112222",
    "to_number": "8613736849910",
    "transcript": "你好，我想预约明天下午三点剪头发，我姓王。"
  }'

curl http://127.0.0.1:8030/inbox
curl http://127.0.0.1:8030/calls/demo-summary-1
curl http://127.0.0.1:8030/digests/preview
curl -X POST http://127.0.0.1:8030/digests/generate
curl http://127.0.0.1:8030/digests
curl -X POST "http://127.0.0.1:8030/internal/digest-tick?now=20:00"
curl -X POST http://127.0.0.1:8030/internal/notifications/dispatch
curl http://127.0.0.1:8030/notification-logs
```

同一个 `call_sid` 重复提交会更新原收件箱条目，不会重复生成待处理事项。`/digests/preview` 会返回 `digest_text`，可作为后续每日汇总通知的正文雏形。`/digests/generate` 会生成正式汇总，并把本批 inbox items 标记为 `digested`，避免重复汇总。

`/internal/digest-tick` 是给服务器 cron 调用的内部触发器。它会读取每个商家的通知偏好，只有当前时间命中 `digest_times` 时才会生成汇总；同一天同一商家同一时间重复触发会复用同一条通知日志，避免重复发送。通知进入 `/notification-logs` 后，可由 Go 的 `/internal/notifications/dispatch` 发送微信订阅消息。

查看和更新通知偏好：

```bash
curl http://127.0.0.1:8030/notification-preferences

curl -X PUT http://127.0.0.1:8030/notification-preferences \
  -H "Content-Type: application/json" \
  -d '{
    "digest_mode": "twice_daily",
    "digest_times": ["12:00", "20:00"],
    "realtime_enabled": false,
    "urgent_realtime_enabled": true,
    "team_wecom_enabled": false,
    "sms_fallback_enabled": false
  }'
```

默认通知偏好：

```json
{
  "digest_mode": "daily",
  "digest_times": ["20:00"],
  "realtime_enabled": false,
  "urgent_realtime_enabled": true,
  "team_wecom_enabled": false,
  "sms_fallback_enabled": false
}
```

## 当前 jambonz 测试信息

```text
Application SID: 8c0cec24-7782-4794-abe9-79c02ffcbbff
Phone Number: 8613736849910
```

## Phase 3 外部联调辅助脚本

配置 call-webhook 的 AI greeting 和 realtime listen：

```bash
cd /opt/RosieAI
bash ops/configure-call-webhook-realtime.sh 服务器公网IP 8613736849910
cd services/call-webhook
docker compose up -d --build
```

检查公网端口、webhook verbs 和 realtime sessions：

```bash
cd /opt/RosieAI
bash ops/check-phase3-realtime.sh 服务器公网IP 8613736849910
```

详细配置见：

```text
docs/Rosie AI jambonz 外部联调指南.md
```
