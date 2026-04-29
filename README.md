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
- `services/call-webhook/`：Phase 1 最小通信 webhook，接收 jambonz 呼入回调。
- `services/ai-agent/`：Phase 2 本地 LLM MVP，默认对接 Ollama + Qwen3。

## 当前推进阶段

Phase 1 通信技术链路已完成：

- jambonz 呼入 webhook。
- SQLite 通话记录。
- 多商家幕后接入号映射。
- Docker 部署。
- 公网 `call-webhook -> ai-agent -> realtime listen verbs` 已验证。
- 真实 PSTN 入站依赖 SIP Trunk / DID / 运营商线路资源，当前归类为商务前置项，不再阻塞业务层 MVP 开发。

下一步研发目标：

- 建立通话会话、转写、摘要和结构化信息模型。
- 接入 AI 摘要与来电意图识别。
- 先完成商家可感知的通话记录和企业微信群机器人通知闭环。
- STT / TTS / Pipecat 在真实线路资源就绪后继续联调。

## 通知策略

Rosie 默认使用企业微信群机器人 Webhook 通知商家：

- 来电摘要、预约、骚扰拦截、异常告警优先发到企业微信群。
- 微信订阅消息、钉钉、短信只作为兜底渠道。
- 群机器人适合高频单向通知；续费、咨询、指令交互、跳转小程序后续通过企业微信应用 / 客服能力承接。

## 服务端口

| 服务 | 端口 | 说明 |
|------|------|------|
| call-webhook | 8000 | jambonz 呼入 webhook |
| ai-agent | 8010 | 本地 LLM Agent API |
| realtime-voice | 8020 | jambonz 实时音频 WebSocket |

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
