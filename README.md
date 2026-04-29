# RosieAI

RosieAI 是一个面向中国中小商家和高频电话业务人士的 AI 电话前台项目。

当前仓库包含：

- `docs/`：需求文档、技术方案、项目实施计划。
- `services/call-webhook/`：Phase 1 最小通信 webhook，接收 jambonz 呼入回调。
- `services/ai-agent/`：Phase 2 本地 LLM MVP，默认对接 Ollama + Qwen3。

## 当前推进阶段

Phase 1 已完成最小 webhook：

- jambonz 呼入 webhook。
- SQLite 通话记录。
- 多商家幕后接入号映射。
- Docker 部署。

Phase 2 当前目标：

- 先跑通本地 LLM。
- 使用 Ollama 承载 Qwen3。
- 提供 `/chat` 和 `/extract` 接口。
- 后续再接入 Pipecat、STT、TTS。

## 服务端口

| 服务 | 端口 | 说明 |
|------|------|------|
| call-webhook | 8000 | jambonz 呼入 webhook |
| ai-agent | 8010 | 本地 LLM Agent API |

