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
- 基于原始来电上下文的手动回拨审计已建立：小程序点击回拨前先写入 `callback_requests`，后端不主动外呼。
- 回拨审计状态流转和通话处理动作已建立：可记录已拨出 / 失败 / 取消，并可把来电标记为已处理、归档或恢复待处理。
- 小程序通知页已接入微信订阅消息授权，主要页面已补齐页面级错误态和重试入口。
- 运营侧试点价值看板已建立：按本月 / 近 7 天 / 近 30 天展示来电、有效线索、预约、骚扰过滤、回拨和预估节省时间。
- 运营侧试点管理后台基础版已建立：Go API 提供 `/admin` 页面、`/admin/pilots` 聚合接口和试点运营动作入口，可做筛选、详情查看、开通试用、接入号分配 / 释放、失败通知重派和失败上报重放。
- 平台虚拟号码池已建立：Go API / Postgres 管理 `access_numbers`，支持运营导入 Rosie 接入号、分配给商家、释放回收；商家开通试用时若未绑定接入号会自动分配。
- 号码池路由校验已建立：后台页面可展示 route-check 结果，可导入 jambonz 配置导出，也可通过 jambonz REST API 自动同步 applications / phone numbers；未指向 Rosie call hook 的号码不会被自动分配给试点商家。
- 试点开通 / 套餐价值展示已建立：小程序可查看 30 元/月基础版、14 天试用、开通步骤、条件呼叫转移引导和 +5 元/月老板音色增值服务占位。
- 续费订单闭环已建立：可创建待支付订单、查看订单列表，并在支付成功后延长商家服务期；微信支付配置完整时走 JSAPI 下单和回调验签，缺失时明确返回待配置状态，不伪造扣款。
- realtime-voice 已提供 `/latency-report`，可按最近 turns 汇总 STT / Agent / TTS / 端到端 p50、p95、max，用于 Phase 3 真实语音联调验收。
- Phase 3 本地真实语音基线已建立：FunASR / SenseVoice + ai-agent + sherpa-onnx 通过 `ops/phase3-latency-baseline.ps1` 跑到 `total_ms.p95=1151ms`，低于 1.5s 目标。
- call-webhook 已内置试点电话告知：默认在欢迎语前说明“由 AI 接待并生成文字摘要”，并把告知文案写入 realtime listen metadata 便于审计。
- 试点许可证检查清单和本地依赖扫描脚本已建立：`docs/试点许可证检查清单.md`、`ops/check-third-party-licenses.ps1`。
- 真实 PSTN 入站仍等待 SIP Trunk / DID / IMS 线路资源，当前不阻塞业务层开发。

下一步研发目标：

- Go 通话详情接口 `GET /calls/{call_sid}` 已完成，可展示原始通话、转写、摘要和处理状态。
- Go 微信订阅消息发送器 `/internal/notifications/dispatch` 已完成，可消费 `notification_logs` 并回写 `sent` / `failed` 状态。
- Go 小程序登录接口 `/auth/wechat-login` 已完成，可通过 `jscode2session` 绑定商家 openid。
- 商家配置生成的 `system_prompt` 已接入 Python `ai-agent` 和 `call-webhook`，后续实时语音链路可从 listen metadata 复用。
- `realtime-voice` 已能消费 listen metadata，建立带 `system_prompt` 的实时会话上下文，并建立 STT -> `ai-agent` -> TTS turn pipeline；TTS 音频可通过 WebSocket 回写电话侧。
- `realtime-voice` 已新增真实语音 provider 接入：`ROSIE_STT_PROVIDER=funasr` 可接 FunASR / SenseVoice，`ROSIE_TTS_PROVIDER=edge` 可把 Edge TTS 转成电话侧 PCM，`ROSIE_REALTIME_PIPELINE_PROVIDER=pipecat_http` 可把同一份 session context 转交给 Pipecat worker。
- 用微信开发者工具联调 `apps/miniprogram`，填入真实订阅消息模板 ID 并验证授权弹窗、openid 绑定和发送链路。
- 下一步把同一套 `/latency-report` 基线接到真实 jambonz 媒体流和 SIP Trunk 入站链路上。

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

## 端到端链路脚本

一条命令启动 Go API、ai-agent 和 realtime-voice，模拟一通 WebSocket 电话，并检查通话详情、AI 摘要、收件箱、通知日志和失败上报 retry 队列：

```powershell
powershell -ExecutionPolicy Bypass -File .\ops\e2e-local-chain.ps1
```

默认使用 `local_fallback` ai-agent、文本帧模拟 STT、关闭 TTS，适合快速验证业务闭环。如果传入 PostgreSQL 连接串，脚本会先自动执行 `services/api-go/migrations/`：

```powershell
powershell -ExecutionPolicy Bypass -File .\ops\e2e-local-chain.ps1 `
  -DatabaseUrl "postgres://rosie:rosie_dev_password@127.0.0.1:15433/rosie_test?sslmode=disable"
```

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
curl http://127.0.0.1:8030/value-metrics
curl http://127.0.0.1:8030/service-status
curl -X POST http://127.0.0.1:8030/service-status/activate-trial \
  -H "Content-Type: application/json" \
  -d '{"plan_code": "pilot_basic"}'
curl -X POST http://127.0.0.1:8030/payment-orders \
  -H "Content-Type: application/json" \
  -d '{"order_type": "renewal", "plan_code": "pilot_basic"}'
curl http://127.0.0.1:8030/payment-orders
curl http://127.0.0.1:8030/calls/demo-summary-1
curl -X PATCH http://127.0.0.1:8030/calls/demo-summary-1/inbox-item \
  -H "Content-Type: application/json" \
  -d '{"status": "handled"}'
curl -X POST http://127.0.0.1:8030/calls/demo-summary-1/callback-requests \
  -H "Content-Type: application/json" \
  -d '{
    "target_number": "+8613811112222",
    "requested_by": "miniprogram",
    "reason": "merchant_manual_call_detail"
  }'
curl http://127.0.0.1:8030/digests/preview
curl -X POST http://127.0.0.1:8030/digests/generate
curl http://127.0.0.1:8030/digests
curl -X POST "http://127.0.0.1:8030/internal/digest-tick?now=20:00"
curl -X POST http://127.0.0.1:8030/internal/notifications/dispatch
curl http://127.0.0.1:8030/notification-logs
```

同一个 `call_sid` 重复提交会更新原收件箱条目，不会重复生成待处理事项。`/digests/preview` 会返回 `digest_text`，可作为后续每日汇总通知的正文雏形。`/digests/generate` 会生成正式汇总，并把本批 inbox items 标记为 `digested`，避免重复汇总。

配置 `ROSIE_WECHAT_PAY_*` 商户号、商户私钥、APIv3 Key、平台序列号和平台公钥后，`POST /payment-orders` 会调用微信支付 JSAPI 下单并返回小程序 `wx.requestPayment` 参数；微信支付回调地址为 `POST /internal/wechat-pay/notify`，验签和解密成功后会把订单标记为已支付并续期。

手动回拨仍遵守产品边界：Go API 只记录 `callback_requests` 审计事件，并绑定原始入站通话；小程序随后调用本机 `wx.makePhoneCall`，不由后端发起主动外呼。

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
