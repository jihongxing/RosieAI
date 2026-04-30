# Rosie API Go

正式业务后端起点。

## 架构决策

- Go 负责正式业务后端：商家、号码、通话、收件箱、汇总、通知日志、小程序 API、支付、审计、后台。
- Python 暂时保留 AI / 语音相关能力：`ai-agent`、后续 STT / TTS / Pipecat、实验性推理链路。
- Python `call-webhook` 是 MVP 行为参考，后续不再继续堆正式业务功能。

## 当前接口

当前 Go 版先对齐 Python MVP 已验证的业务接口：

- `GET /health`
- `GET /health/deps`
- `GET /admin`
- `GET /admin/access-numbers`
- `GET /admin/access-numbers/route-check`
- `POST /admin/access-numbers`
- `POST /admin/jambonz/config-export`
- `POST /admin/jambonz/sync`
- `GET /admin/pilots`
- `GET /admin/pilots/{merchant_id}`
- `POST /admin/pilots/{merchant_id}/assign-access-number`
- `POST /admin/pilots/{merchant_id}/release-access-number`
- `POST /admin/pilots/{merchant_id}/activate-trial`
- `POST /admin/pilots/{merchant_id}/dispatch-notifications`
- `POST /admin/pilots/{merchant_id}/flush-retries`
- `POST /auth/wechat-login`
- `GET /merchants`
- `POST /merchants`
- `GET /value-metrics`
- `GET /service-status`
- `POST /service-status/activate-trial`
- `GET /payment-orders`
- `POST /payment-orders`
- `POST /internal/payment-orders/{order_no}/mark-paid`
- `POST /internal/wechat-pay/notify`
- `GET /merchant-profile`
- `PUT /merchant-profile`
- `GET /industry-templates`
- `GET /calls`
- `GET /calls/{call_sid}`
- `PATCH /calls/{call_sid}/inbox-item`
- `POST /calls/{call_sid}/callback-requests`
- `PATCH /calls/{call_sid}/callback-requests/{callback_id}`
- `POST /simulate/call-result`
- `GET /inbox`
- `GET /digests/preview`
- `POST /digests/generate`
- `GET /digests`
- `GET /notification-preferences`
- `PUT /notification-preferences`
- `POST /internal/digest-tick`
- `POST /internal/notifications/dispatch`
- `GET /notification-logs`
- `POST /internal/realtime-call-result-retries`
- `GET /internal/realtime-call-result-retries`
- `POST /internal/realtime-call-result-retries/flush`

默认使用内存 store，方便本地迁移接口和测试行为。设置 `ROSIE_DATABASE_URL` 后会切到 PostgreSQL store。PostgreSQL 正式 schema 见 `migrations/`。

## 本地运行

```powershell
cd services\api-go
go test ./...
go run .\cmd\rosie-api
```

默认监听：

```text
http://127.0.0.1:8030
```

部署诊断：

```powershell
curl http://127.0.0.1:8030/health/deps
```

`/health/deps` 会检查数据库、AI 摘要服务、微信通知配置，以及 due 通知队列和失败上报 retry 队列数量；依赖异常时返回 `503` 和 `status=degraded`。

## 环境变量

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `ROSIE_API_ADDR` | `127.0.0.1:8030` | Go API 监听地址 |
| `ROSIE_DATABASE_URL` | 空 | PostgreSQL 连接串；为空时使用内存 store |
| `ROSIE_DEFAULT_ACCESS_NUMBER` | `8613736849910` | 默认幕后接入号 |
| `ROSIE_DEFAULT_MERCHANT_ID` | `demo-merchant` | 默认商家 ID |
| `ROSIE_DEFAULT_MERCHANT_NAME` | `测试商家` | 默认商家名称 |
| `ROSIE_DEFAULT_TRANSFER_PHONE` | 空 | 默认转人工号码 |
| `ROSIE_AI_AGENT_URL` | 空 | ai-agent 地址；配置后可用于结构化通话摘要 |
| `ROSIE_AI_SUMMARY_ENABLED` | `true` | 是否尝试用 ai-agent 生成结构化摘要；失败会回落到规则摘要 |
| `ROSIE_AI_SUMMARY_TIMEOUT_SECONDS` | `3` | AI 摘要请求超时时间 |
| `ROSIE_JAMBONZ_EXPECTED_CALL_HOOK_URL` | 空 | 号码池路由校验时要求 jambonz application 指向的 Rosie call hook |
| `ROSIE_JAMBONZ_EXPECTED_STATUS_HOOK_URL` | 空 | 号码池路由校验时要求 jambonz application 指向的 Rosie status hook |
| `ROSIE_JAMBONZ_API_BASE_URL` | 空 | jambonz REST API base URL，通常形如 `https://jambonz.example.com/v1` |
| `ROSIE_JAMBONZ_API_TOKEN` | 空 | jambonz REST API Bearer token |
| `ROSIE_JAMBONZ_APPLICATIONS_PATH` | `/Applications` | 拉取 jambonz applications 的路径 |
| `ROSIE_JAMBONZ_PHONE_NUMBERS_PATH` | `/PhoneNumbers` | 拉取 jambonz phone numbers 的路径；自托管版本不同可覆盖 |
| `ROSIE_WECHAT_APP_ID` | 空 | 微信小程序 AppID |
| `ROSIE_WECHAT_APP_SECRET` | 空 | 微信小程序 AppSecret |
| `ROSIE_WECHAT_SUBSCRIBE_TEMPLATE_ID` | 空 | 订阅消息模板 ID |
| `ROSIE_WECHAT_DEFAULT_OPENID` | 空 | 本地测试默认接收 openid，正式版应来自用户绑定关系 |
| `ROSIE_WECHAT_SUBSCRIBE_PAGE` | `pages/inbox/index` | 点击订阅消息进入的小程序页面 |
| `ROSIE_WECHAT_MINIPROGRAM_STATE` | `formal` | `formal` / `trial` / `developer` |
| `ROSIE_WECHAT_DIGEST_TITLE_KEY` | `thing1` | 汇总标题模板字段 |
| `ROSIE_WECHAT_DIGEST_SUMMARY_KEY` | `thing2` | 汇总正文模板字段 |
| `ROSIE_WECHAT_DIGEST_TIME_KEY` | `time3` | 发送时间模板字段 |
| `ROSIE_WECHAT_PAY_API_BASE_URL` | `https://api.mch.weixin.qq.com` | 微信支付 API 地址，本地测试可指向 mock server |
| `ROSIE_WECHAT_PAY_MCH_ID` | 空 | 微信支付商户号 |
| `ROSIE_WECHAT_PAY_NOTIFY_URL` | 空 | 微信支付回调地址 |
| `ROSIE_WECHAT_PAY_MERCHANT_SERIAL_NO` | 空 | 微信支付商户证书序列号 |
| `ROSIE_WECHAT_PAY_PRIVATE_KEY_PATH` | 空 | 微信支付商户私钥路径 |
| `ROSIE_WECHAT_PAY_API_V3_KEY` | 空 | 微信支付 APIv3 Key |
| `ROSIE_WECHAT_PAY_PLATFORM_SERIAL_NO` | 空 | 微信支付平台证书序列号，用于回调验签 |
| `ROSIE_WECHAT_PAY_PLATFORM_KEY_PATH` | 空 | 微信支付平台公钥/平台证书路径，用于回调验签 |

## 本地 PostgreSQL

使用 Podman 启动本地测试库：

```powershell
podman run -d --name rosie-postgres `
  -e POSTGRES_USER=rosie `
  -e POSTGRES_PASSWORD=rosie_dev_password `
  -e POSTGRES_DB=rosie_test `
  -p 127.0.0.1:15433:5432 `
  -v rosie-postgres-data:/var/lib/postgresql/data `
  docker.io/library/postgres:16-alpine
```

执行 migration：

```powershell
go run .\cmd\rosie-migrate -database-url "postgres://rosie:rosie_dev_password@127.0.0.1:15433/rosie_test?sslmode=disable"
```

让 Go API 使用 PostgreSQL：

```powershell
$env:ROSIE_DATABASE_URL="postgres://rosie:rosie_dev_password@127.0.0.1:15433/rosie_test?sslmode=disable"
$env:ROSIE_AI_AGENT_URL="http://127.0.0.1:8010"
go run .\cmd\rosie-api
```

## 验证业务链路

```bash
curl -X POST http://127.0.0.1:8030/simulate/call-result \
  -H "Content-Type: application/json" \
  -d '{
    "call_sid": "go-demo-1",
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
curl http://127.0.0.1:8030/calls/go-demo-1
curl -X PATCH http://127.0.0.1:8030/calls/go-demo-1/inbox-item \
  -H "Content-Type: application/json" \
  -d '{"status": "handled"}'
curl -X POST http://127.0.0.1:8030/calls/go-demo-1/callback-requests \
  -H "Content-Type: application/json" \
  -d '{
    "target_number": "+8613811112222",
    "requested_by": "miniprogram",
    "reason": "merchant_manual_call_detail"
  }'
curl -X PATCH http://127.0.0.1:8030/calls/go-demo-1/callback-requests/1 \
  -H "Content-Type: application/json" \
  -d '{
    "status": "dialed",
    "audit_note": "wx.makePhoneCall invoked by merchant"
  }'
curl http://127.0.0.1:8030/merchant-profile
curl http://127.0.0.1:8030/industry-templates
curl http://127.0.0.1:8030/digests/preview
curl -X POST "http://127.0.0.1:8030/internal/digest-tick?now=20:00"
curl -X POST http://127.0.0.1:8030/internal/notifications/dispatch
curl http://127.0.0.1:8030/notification-logs
```

通话结果入库时，Go API 会优先尝试调用 `ROSIE_AI_AGENT_URL/extract` 生成结构化摘要；如果 ai-agent 未配置、超时、返回非 JSON 或字段不合格，会保留规则摘要兜底，业务入库不受影响。

手动回拨只做审计记录，不做后台主动外呼。`POST /calls/{call_sid}/callback-requests` 会校验原始通话存在，并写入商家、原始 `call_sid` / `call_id`、目标号码、请求来源和审计备注；如果请求未传 `target_number`，会优先使用摘要里的 `customer_phone`，再回退到原始来电号码。`PATCH /calls/{call_sid}/callback-requests/{callback_id}` 可把审计状态更新为 `requested`、`dialed`、`failed` 或 `canceled`。`GET /calls/{call_sid}` 会返回 `callback_requests` 供小程序展示历史。

通话处理动作通过 `PATCH /calls/{call_sid}/inbox-item` 更新收件箱状态，当前支持 `new`、`needs_review`、`handled`、`archived` 和 `filtered`。

运营侧价值看板通过 `GET /value-metrics` 聚合现有业务数据，支持 `period=month`、`period=7d`、`period=30d`，也支持同时传入 RFC3339 格式的 `since` / `until` 自定义窗口。指标包括总来电、有效线索、预约意向、骚扰过滤、跟进、已处理、回拨和预估节省时间。

运营侧试点管理后台通过 `GET /admin` 提供基础版工作台；页面读取 `GET /admin/pilots`，按商家聚合订阅状态、本月价值指标、最近订单、通知失败、realtime-voice 失败上报 retry 和最近来电，并支持 `q` / `status` 筛选。单商家 JSON 详情可用 `GET /admin/pilots/{merchant_id}` 查看，会额外返回最近订单和失败明细。运营动作入口包括 `POST /admin/pilots/{merchant_id}/activate-trial` 开通试用、`POST /admin/pilots/{merchant_id}/assign-access-number` 自动或指定分配接入号、`POST /admin/pilots/{merchant_id}/release-access-number` 释放回收接入号、`POST /admin/pilots/{merchant_id}/dispatch-notifications` 重派指定商家的失败 / 排队通知、`POST /admin/pilots/{merchant_id}/flush-retries` 重放指定商家的失败上报。

虚拟号码池通过 `access_numbers` 管理 Rosie 接入号生命周期。运营可先导入号码，再在后台给商家分配；商家开通试用时如果还没有 `access_number`，Go API 会自动从可用池分配一个号码，并把它写回 `merchants.access_number`，小程序呼叫转移指引会展示该真实分配号。自动分配只会使用已配置 `trunk_id`、`jambonz_application_id` 和 `jambonz_call_hook_url` 的号码；如果配置了 `ROSIE_JAMBONZ_EXPECTED_CALL_HOOK_URL`，还会要求 call hook 与 Rosie 预期地址一致。`/admin` 页面会直接展示 route-check 汇总和明细，并可触发 jambonz 自动同步。

```bash
curl -X POST http://127.0.0.1:8030/admin/access-numbers \
  -H "Content-Type: application/json" \
  -d '{"numbers":["+8617000000800","+8617000000801"],"provider":"pilot-carrier","trunk_id":"sip-trunk-1","jambonz_application_id":"jambonz-app-1"}'

curl http://127.0.0.1:8030/admin/access-numbers

curl http://127.0.0.1:8030/admin/access-numbers/route-check

curl -X POST http://127.0.0.1:8030/admin/jambonz/config-export \
  -H "Content-Type: application/json" \
  -d '{
    "expected_call_hook_url": "https://voice.example.com/webhooks/jambonz/call",
    "expected_status_hook_url": "https://voice.example.com/webhooks/jambonz/status",
    "applications": [
      {
        "application_id": "jambonz-app-1",
        "name": "Rosie inbound",
        "call_hook_url": "https://voice.example.com/webhooks/jambonz/call",
        "status_hook_url": "https://voice.example.com/webhooks/jambonz/status"
      }
    ],
    "phone_numbers": [
      {
        "number": "+8617000000800",
        "trunk_id": "sip-trunk-1",
        "application_id": "jambonz-app-1"
      }
    ]
  }'

curl -X POST http://127.0.0.1:8030/admin/jambonz/sync \
  -H "Content-Type: application/json" \
  -d '{}'

curl -X POST http://127.0.0.1:8030/admin/pilots/demo-merchant/assign-access-number \
  -H "Content-Type: application/json" \
  -d '{}'
```

试点开通通过 `GET /service-status` 和 `POST /service-status/activate-trial` 驱动。当前套餐为 `pilot_basic`，展示 30 元/月基础版、14 天试用、开通步骤、条件呼叫转移拨号码、本月价值指标和 +5 元/月老板音色增值服务占位。

续费订单通过 `POST /payment-orders` 创建，`GET /payment-orders` 查看。微信支付商户配置缺失时会返回 `pending_provider_config`，不会伪造扣款；配置完整后会调用微信支付 JSAPI 下单，返回小程序 `wx.requestPayment` 所需的 `request_params`，并把 `prepay_id` 写回订单。支付回调走 `POST /internal/wechat-pay/notify`，会校验 `Wechatpay-*` 签名头、用 APIv3 Key 解密资源，`trade_state=SUCCESS` 时把订单置为 `paid` 并续期套餐。`POST /internal/payment-orders/{order_no}/mark-paid` 仅用于本地/内部验证支付成功后的续费效果。

`/internal/notifications/dispatch` 默认消费 `queued` 通知日志，成功后标记为 `sent`。失败时会记录 `last_error` / `error_category`，按 backoff 写入 `next_retry_at`；批量重试 `failed` 记录时只消费已到期且未超过 `max_attempts` 的任务，达到上限或遇到永久错误会标记为 `exhausted`。

```bash
curl -X POST "http://127.0.0.1:8030/internal/notifications/dispatch?status=failed"
curl -X POST "http://127.0.0.1:8030/internal/notifications/dispatch?idempotency_key=realtime_call:demo-merchant:call-1&status=failed"
```

`realtime-voice` 上报正式业务结果失败时，会把原始 payload 写入 Go/PostgreSQL 队列，避免 worker 重启后丢失。查看和手动 flush：

```bash
curl http://127.0.0.1:8030/internal/realtime-call-result-retries
curl -X POST "http://127.0.0.1:8030/internal/realtime-call-result-retries/flush?max_attempts=5"
```

小程序登录并绑定 openid 到商家：

```bash
curl -X POST http://127.0.0.1:8030/auth/wechat-login \
  -H "Content-Type: application/json" \
  -d '{
    "code": "wx.login 返回的 code",
    "merchant_id": "demo-merchant"
  }'
```

绑定完成后，通知发送器会优先使用商家绑定的 openid；只有没有绑定时才回退到 `ROSIE_WECHAT_DEFAULT_OPENID`。

## 下一步

1. 在部署环境执行 `go run ./cmd/rosie-migrate -database-url "$ROSIE_DATABASE_URL"`，然后启动 API。
2. 让 jambonz call hook 逐步切到 Go API，Python 保留 AI / 语音服务。
3. 用微信开发者工具联调 `apps/miniprogram`，接入真实小程序 AppID、订阅消息授权和页面错误态。
4. 用微信开发者工具联调小程序手动回拨、订阅消息授权和页面错误态。

可选 PostgreSQL 集成测试：

```powershell
$env:ROSIE_TEST_DATABASE_URL="postgres://user:pass@127.0.0.1:5432/rosie_test?sslmode=disable"
go test ./internal/store -run TestPostgresStoreSmoke
```
