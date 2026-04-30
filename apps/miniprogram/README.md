# RosieAI 小程序雏形

原生微信小程序雏形，面向商家老板查看漏接电话结果。

## 页面

- `pages/inbox`：收件箱列表、商家 ID 切换、微信登录绑定。
- `pages/call-detail`：通话详情、转写、摘要、关键信息和审计字段。
- `pages/digests`：汇总预览、生成汇总、历史汇总。
- `pages/merchant`：商家配置、行业话术模板、服务项目、FAQ、预约规则。
- `pages/preferences`：通知偏好设置。

## 本地配置

API 地址在 `utils/config.js`：

```js
apiBaseURL: "http://127.0.0.1:8030"
```

在微信开发者工具中打开 `apps/miniprogram`。本地调试时需要在开发者工具里关闭合法域名校验，或把 Go API 暴露到已配置的 HTTPS 域名。

## 后端依赖

默认对接 Go 正式业务后端：

```powershell
cd services\api-go
$env:ROSIE_DATABASE_URL="postgres://rosie:rosie_dev_password@127.0.0.1:15433/rosie_test?sslmode=disable"
go run .\cmd\rosie-api
```
