# Rosie AI jambonz 外部联调指南

版本：v1.0  
目标：让 jambonz 能调用 Rosie 的 call webhook，并连接 Rosie 的 realtime voice WebSocket。

## 1. 当前服务器信息

服务器公网 IP 以服务器实际输出为准：

```bash
curl -4 ifconfig.me
```

示例：

```text
119.28.50.29
```

当前 Rosie 服务端口：

| 服务 | 端口 | 用途 |
|------|------|------|
| call-webhook | 8000 | jambonz call hook / status hook |
| ai-agent | 8010 | DeepSeek / OpenAI-compatible LLM |
| realtime-voice | 8020 | jambonz realtime audio WebSocket |

## 2. 服务器侧必须可访问

在开发机执行：

```powershell
Test-NetConnection 119.28.50.29 -Port 8000
Test-NetConnection 119.28.50.29 -Port 8020
```

或者：

```bash
curl http://119.28.50.29:8000/health
curl http://119.28.50.29:8020/health
```

两个都通之后再配置 jambonz。

## 3. jambonz Application 配置

进入 jambonz portal：

```text
Applications -> Add Application
```

创建：

```text
Name:
Rosie MVP
```

Call Hook：

```text
Method:
POST

URL:
http://119.28.50.29:8000/webhooks/jambonz/call
```

Call Status Hook：

```text
Method:
POST

URL:
http://119.28.50.29:8000/webhooks/jambonz/status
```

Authentication：

```text
None
```

Content-Type：

```text
application/json
```

## 4. Phone Number 绑定

在 jambonz 的号码管理页面：

```text
Phone Numbers / Numbers / DID
```

选择测试号码，绑定：

```text
Application:
Rosie MVP
```

之后拨打这个测试号码，jambonz 会请求：

```text
POST http://119.28.50.29:8000/webhooks/jambonz/call
```

## 5. Rosie 返回的预期 verbs

Rosie 当前会返回：

```json
[
  {
    "verb": "say",
    "text": "您好，我是测试商家的AI前台Rosie..."
  },
  {
    "verb": "listen",
    "url": "ws://119.28.50.29:8020/ws/jambonz/audio",
    "sampleRate": 16000,
    "mixType": "mono",
    "bidirectionalAudio": {
      "enabled": true,
      "streaming": true,
      "sampleRate": 16000
    }
  },
  {
    "verb": "hangup"
  }
]
```

## 6. 服务器观察命令

窗口 A：看 call-webhook：

```bash
cd /opt/RosieAI/services/call-webhook
docker compose logs -f
```

成功时会看到：

```text
POST /webhooks/jambonz/call
```

窗口 B：看 realtime-voice：

```bash
cd /opt/RosieAI/services/realtime-voice
docker compose logs -f
```

成功时会看到：

```text
jambonz audio websocket connected
```

查看实时音频会话：

```bash
curl http://127.0.0.1:8020/sessions
```

如果成功，`audio_bytes` 会大于 0。

## 7. 常见问题

### 7.1 call webhook 没有日志

说明 jambonz 没有打到 Rosie。

检查：

- Application 的 Call Hook URL 是否正确。
- 测试号码是否绑定到 Rosie MVP Application。
- 服务器 `8000` 是否公网可访问。

### 7.2 有 call webhook，但没有 realtime session

说明 call hook 到了，但 jambonz 没有连接 `listen` WebSocket。

检查：

- 返回 verbs 里的 `listen.url` 是否是公网可访问地址。
- `8020` 是否公网可访问。
- realtime-voice 是否运行。

### 7.3 服务器访问公网 IP 不通

很多云服务器不支持访问自己的公网 IP。应该从开发机测试公网 IP。

服务器本机用：

```bash
curl http://127.0.0.1:8000/health
curl http://127.0.0.1:8020/health
```

开发机用：

```powershell
Test-NetConnection 服务器公网IP -Port 8000
Test-NetConnection 服务器公网IP -Port 8020
```

## 8. 下一步

当 `/sessions` 中出现 `audio_bytes > 0` 后，进入下一阶段：

```text
realtime-voice -> STT -> ai-agent -> TTS -> WebSocket audio out
```

也就是实时语音多轮对话。

