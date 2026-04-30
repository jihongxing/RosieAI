# Rosie API Go

正式业务后端起点。

## 架构决策

- Go 负责正式业务后端：商家、号码、通话、收件箱、汇总、通知日志、小程序 API、支付、审计、后台。
- Python 暂时保留 AI / 语音相关能力：`ai-agent`、后续 STT / TTS / Pipecat、实验性推理链路。
- Python `call-webhook` 是 MVP 行为参考，后续不再继续堆正式业务功能。

## 当前接口

当前 Go 版先对齐 Python MVP 已验证的业务接口：

- `GET /health`
- `POST /auth/wechat-login`
- `GET /merchants`
- `POST /merchants`
- `GET /merchant-profile`
- `PUT /merchant-profile`
- `GET /industry-templates`
- `GET /calls`
- `GET /calls/{call_sid}`
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

## 环境变量

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `ROSIE_API_ADDR` | `127.0.0.1:8030` | Go API 监听地址 |
| `ROSIE_DATABASE_URL` | 空 | PostgreSQL 连接串；为空时使用内存 store |
| `ROSIE_DEFAULT_ACCESS_NUMBER` | `8613736849910` | 默认幕后接入号 |
| `ROSIE_DEFAULT_MERCHANT_ID` | `demo-merchant` | 默认商家 ID |
| `ROSIE_DEFAULT_MERCHANT_NAME` | `测试商家` | 默认商家名称 |
| `ROSIE_DEFAULT_TRANSFER_PHONE` | 空 | 默认转人工号码 |
| `ROSIE_WECHAT_APP_ID` | 空 | 微信小程序 AppID |
| `ROSIE_WECHAT_APP_SECRET` | 空 | 微信小程序 AppSecret |
| `ROSIE_WECHAT_SUBSCRIBE_TEMPLATE_ID` | 空 | 订阅消息模板 ID |
| `ROSIE_WECHAT_DEFAULT_OPENID` | 空 | 本地测试默认接收 openid，正式版应来自用户绑定关系 |
| `ROSIE_WECHAT_SUBSCRIBE_PAGE` | `pages/inbox/index` | 点击订阅消息进入的小程序页面 |
| `ROSIE_WECHAT_MINIPROGRAM_STATE` | `formal` | `formal` / `trial` / `developer` |
| `ROSIE_WECHAT_DIGEST_TITLE_KEY` | `thing1` | 汇总标题模板字段 |
| `ROSIE_WECHAT_DIGEST_SUMMARY_KEY` | `thing2` | 汇总正文模板字段 |
| `ROSIE_WECHAT_DIGEST_TIME_KEY` | `time3` | 发送时间模板字段 |

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
Get-ChildItem migrations\*.sql | Sort-Object Name | ForEach-Object {
  Get-Content -Raw $_.FullName |
    podman exec -i rosie-postgres psql -U rosie -d rosie_test -v ON_ERROR_STOP=1
}
```

让 Go API 使用 PostgreSQL：

```powershell
$env:ROSIE_DATABASE_URL="postgres://rosie:rosie_dev_password@127.0.0.1:15433/rosie_test?sslmode=disable"
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
curl http://127.0.0.1:8030/calls/go-demo-1
curl http://127.0.0.1:8030/merchant-profile
curl http://127.0.0.1:8030/industry-templates
curl http://127.0.0.1:8030/digests/preview
curl -X POST "http://127.0.0.1:8030/internal/digest-tick?now=20:00"
curl -X POST http://127.0.0.1:8030/internal/notifications/dispatch
curl http://127.0.0.1:8030/notification-logs
```

`/internal/notifications/dispatch` 默认消费 `queued` 通知日志，成功后标记为 `sent`，失败后标记为 `failed` 并记录 `last_error`。重试失败记录：

```bash
curl -X POST "http://127.0.0.1:8030/internal/notifications/dispatch?status=failed"
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

1. 在部署环境按文件名顺序执行 `migrations/`，然后配置 `ROSIE_DATABASE_URL`。
2. 让 jambonz call hook 逐步切到 Go API，Python 保留 AI / 语音服务。
3. 用微信开发者工具联调 `apps/miniprogram`，接入真实小程序 AppID、订阅消息授权和页面错误态。
4. 继续补基于原始来电上下文的手动回拨审计。

可选 PostgreSQL 集成测试：

```powershell
$env:ROSIE_TEST_DATABASE_URL="postgres://user:pass@127.0.0.1:5432/rosie_test?sslmode=disable"
go test ./internal/store -run TestPostgresStoreSmoke
```
