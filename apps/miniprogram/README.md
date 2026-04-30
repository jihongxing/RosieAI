# RosieAI 小程序雏形

原生微信小程序雏形，面向商家老板查看漏接电话结果。

## 页面

- `pages/dashboard`：试点价值看板，展示来电、有效线索、预约、骚扰过滤、回拨和节省时间。
- `pages/onboarding`：试点开通、套餐权益、试用状态、续费订单、条件呼叫转移引导和增值服务展示。
- `pages/inbox`：收件箱列表、商家 ID 切换、微信登录绑定。
- `pages/call-detail`：通话详情、转写、摘要、关键信息、处理动作和回拨审计状态。
- `pages/digests`：汇总预览、生成汇总、历史汇总。
- `pages/merchant`：商家配置、行业话术模板、服务项目、FAQ、预约规则。
- `pages/preferences`：通知偏好设置、微信订阅消息授权。

## 本地配置

API 地址在 `utils/config.js`：

```js
apiBaseURL: "http://127.0.0.1:8030",
subscribeTemplateIds: {
  digest: "微信公众平台订阅消息模板 ID",
  realtime: "微信公众平台订阅消息模板 ID"
}
```

在微信开发者工具中打开 `apps/miniprogram`。本地调试时需要在开发者工具里关闭合法域名校验，或把 Go API 暴露到已配置的 HTTPS 域名。

订阅消息授权入口在“通知”页。授权前需要先在微信公众平台配置订阅消息模板，并把模板 ID 写入 `utils/config.js`；没有模板 ID 时页面会显示“未配置模板”。

页面级错误态已覆盖收件箱、通话详情、汇总、商家配置和通知偏好；后端不可用、网络失败或接口返回错误时，页面会显示错误信息和重试按钮。

试点开通页会调用 `POST /payment-orders` 创建续费订单；当后端已配置微信支付 JSAPI 时，会直接使用返回的 `request_params` 发起 `wx.requestPayment`，支付结果以微信支付回调为准刷新服务状态。

## 后端依赖

默认对接 Go 正式业务后端：

```powershell
cd services\api-go
$env:ROSIE_DATABASE_URL="postgres://rosie:rosie_dev_password@127.0.0.1:15433/rosie_test?sslmode=disable"
go run .\cmd\rosie-api
```
