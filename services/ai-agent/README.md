# Rosie AI Agent MVP

本服务用于 Phase 2 的第一步：跑通本地 LLM。

当前目标：

- 支持 DeepSeek / OpenAI-compatible API。
- 支持本机 Ollama 承载 Qwen3 模型。
- 提供 `/chat` 接口，生成 Rosie 电话前台回复。
- 提供 `/extract` 接口，做通话摘要和关键信息提取的最小验证。
- 暂不处理电话音频、STT、TTS、Pipecat。那些放到下一步。

## 方案 A：DeepSeek API，推荐小服务器使用

你的服务器如果只有 2C2G，推荐先用 DeepSeek API。

```bash
cd services/ai-agent
cp .env.example .env
vi .env
```

配置：

```env
ROSIE_LLM_PROVIDER=openai_compatible
ROSIE_OPENAI_BASE_URL=https://api.deepseek.com
ROSIE_OPENAI_API_KEY=你的_deepseek_api_key
ROSIE_LLM_MODEL=deepseek-chat
```

启动：

```bash
docker compose up -d --build
```

测试：

```bash
curl http://127.0.0.1:8010/health

curl -X POST http://127.0.0.1:8010/chat \
  -H "Content-Type: application/json" \
  -d '{
    "merchant_name": "张三理发店",
    "customer_text": "你好，我想预约明天下午剪头发",
    "history": []
  }'
```

## 方案 B：Ollama 本地模型

如果服务器内存足够，再使用 Ollama。

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

```bash
cd services/ai-agent
cp .env.example .env
docker compose up -d --build
```

如果使用 Ollama，Linux Docker 里通过 `host.docker.internal` 访问宿主机 Ollama。

## API 测试

健康检查：

```bash
curl http://127.0.0.1:8010/health
```

模型检查。DeepSeek 模式下这里只检查配置，真正联调用 `/chat`：

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
