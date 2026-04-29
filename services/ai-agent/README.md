# Rosie AI Agent MVP

本服务用于 Phase 2 的第一步：跑通本地 LLM。

当前目标：

- 使用本机 Ollama 承载 Qwen3 模型。
- 提供 `/chat` 接口，生成 Rosie 电话前台回复。
- 提供 `/extract` 接口，做通话摘要和关键信息提取的最小验证。
- 暂不处理电话音频、STT、TTS、Pipecat。那些放到下一步。

## 推荐模型

MVP 推荐：

```bash
ollama pull qwen3:8b
```

如果服务器资源较小，可以先用：

```bash
ollama pull qwen3:4b
```

然后在 `.env` 中调整：

```env
ROSIE_LLM_MODEL=qwen3:4b
```

## 本地启动

```bash
cd services/ai-agent
python -m venv .venv
.venv\Scripts\activate
pip install -r requirements.txt
python -m rosie_ai_agent
```

默认地址：

```text
http://127.0.0.1:8010
```

## Docker 启动

如果 Ollama 安装在宿主机：

```bash
cd services/ai-agent
cp .env.example .env
docker compose up -d --build
```

Linux Docker 里通过 `host.docker.internal` 访问宿主机 Ollama。

## API 测试

健康检查：

```bash
curl http://127.0.0.1:8010/health
```

模型检查：

```bash
curl http://127.0.0.1:8010/health/llm
```

对话：

```bash
curl -X POST http://127.0.0.1:8010/chat \
  -H "Content-Type: application/json" \
  -d '{
    "merchant_name": "张三理发店",
    "customer_text": "你好，我想预约明天下午剪头发",
    "history": []
  }'
```

摘要提取：

```bash
curl -X POST http://127.0.0.1:8010/extract \
  -H "Content-Type: application/json" \
  -d '{
    "merchant_name": "张三理发店",
    "transcript": "客户说想预约明天下午两点剪头发，姓王，电话是13811112222。"
  }'
```

## 服务器安装 Ollama

OpenCloudOS / Linux：

```bash
curl -fsSL https://ollama.com/install.sh | sh
systemctl enable --now ollama
ollama pull qwen3:8b
ollama run qwen3:8b "你好"
```

如果机器内存不足，先用 `qwen3:4b`。

