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
- 先完成小程序收件箱、定时汇总和关键事项提醒闭环。
- STT / TTS / Pipecat 在真实线路资源就绪后继续联调。

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

## 业务层 MVP 验证

在真实 PSTN 线路就绪前，可以用模拟转写文本验证“漏接电话整理器”链路：

```bash
curl -X POST http://127.0.0.1:8000/simulate/call-result \
  -H "Content-Type: application/json" \
  -d '{
    "call_sid": "demo-summary-1",
    "from_number": "+8613811112222",
    "to_number": "8613736849910",
    "transcript": "你好，我想预约明天下午三点剪头发，我姓王。"
  }'

curl http://127.0.0.1:8000/inbox
curl http://127.0.0.1:8000/digests/preview
```

同一个 `call_sid` 重复提交会更新原收件箱条目，不会重复生成待处理事项。`/digests/preview` 会返回 `digest_text`，可作为后续每日汇总通知的正文雏形。

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
