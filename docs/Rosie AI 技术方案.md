# Rosie AI 技术方案

版本：v3.0 开源项目集成定型版  
目标：降低实现难度、稳定落地、可扩展、尽可能少依赖付费 API  
结论：采用 jambonz + Pipecat + 本地开源 AI 模型作为主线  

## 1. 最终定型结论

Rosie AI 不从零自研通信平台，也不直接裸写 FreeSWITCH 业务逻辑。最终主线改为：

```text
微信小程序
  -> Rosie 业务后端
  -> jambonz 自托管 CPaaS
  -> Pipecat 实时语音 Agent
  -> 本地 STT / 本地 LLM / 本地 TTS
  -> PostgreSQL + Redis/Valkey + 对象存储
```

核心判断：

| 过去方案 | 新定型 |
|----------|--------|
| 裸 FreeSWITCH 写 dialplan 和 ESL | 改用 jambonz 管呼叫控制、SIP、媒体流 |
| 自己手写 STT -> LLM -> TTS 实时流水线 | 改用 Pipecat 做实时语音 Agent 管线 |
| 第一阶段就考虑 Kamailio | 后期高并发再加 |
| 4G Dongle / 安卓机接听 | 只保留实验室 Demo，不进生产 |
| 云厂商语音 API | 只做短期兜底，不作为主线 |

一句话：用成熟开源项目吃掉通信和实时语音最硬的部分，我们主要开发商家配置、防骚扰、预约闭环、通知、计费和运营后台。

### 1.1 通信能力边界

jambonz 具备入站接听和出站拨号能力，但 Rosie 产品主线只使用入站接听能力，不把出站拨号作为增长或营销能力。

技术边界：

- 默认链路只处理客户主动呼入，并通过条件呼叫转移进入 Rosie 的电话。
- 禁止实现陌生号码批量外呼、营销外呼、自动获客外呼、电销坐席工作台。
- 可以保留“有上下文回拨”的技术接口，但必须由业务层强约束：原始来电、预约、订单或商家手动确认是必要前置条件。
- 回拨能力要独立开关、独立审计、独立限频，不能和普通通话任务混用。
- 系统设计优先降低运营黑洞和政策风险，避免把 Rosie 演化成外呼平台。

## 2. 为什么改成 jambonz + Pipecat

### 2.1 降低通信实现难度

裸 FreeSWITCH 的问题：

- dialplan、ESL、RTP、SIP 排障门槛高。
- 呼叫控制、媒体流、转接、挂断都要自己封装。
- 新人接手困难。
- 业务逻辑容易和通信配置搅在一起。

jambonz 的价值：

- 它是开源 CPaaS，底层仍然使用 FreeSWITCH / drachtio。
- 对上提供 Webhook / 应用式呼叫控制。
- 对接 SIP Trunk、接听、挂断、转接、媒体流更工程化。
- 我们可以把 FreeSWITCH 当底层能力，而不是直接在业务里操作它。

### 2.2 降低实时语音 AI 难度

自己写实时语音 Agent 的问题：

- VAD、打断、STT 流、LLM 流、TTS 流、音频回放都要调度。
- 稍有问题就会出现抢话、延迟、重复回答、挂死。
- 电话实时体验很依赖状态流控制。

Pipecat 的价值：

- 专门做实时语音 AI pipeline。
- 适合承接 STT -> LLM -> TTS 的连续流式处理。
- 更容易实现打断、状态流、事件回调和多 provider 切换。
- 后期全球化时，也可复用到 WebRTC / LiveKit 等场景。

## 3. 总体架构

```text
[客户来电]
    |
[商家原号条件呼叫转移：无应答 / 忙线 / 不可及]
    |
[Rosie 幕后接入号所在 SIP Trunk / DID Provider]
    |
[jambonz]
    |-- SIP 接入
    |-- 呼叫控制
    |-- 幕后接入号路由
    |-- 转接 / 挂断
    |-- 媒体流 WebSocket
    |
[Rosie Call Webhook]
    |-- 根据被叫幕后接入号查商家
    |-- 生成本次通话上下文
    |-- 调用 Pipecat Agent
    |
[Pipecat Voice Agent]
    |-- VAD
    |-- STT: FunASR / SenseVoice
    |-- 防骚扰规则
    |-- Dialogue Manager
    |-- LLM: Qwen3
    |-- TTS: Fun-CosyVoice
    |
[Rosie Business API]
    |-- 商家配置
    |-- 预约规则
    |-- 通话记录
    |-- 声音克隆
    |-- 通知推送
    |-- 计费续费
    |
[Data Layer]
    |-- PostgreSQL
    |-- Redis / Valkey
    |-- MinIO / COS
    |
[Wechat Mini Program]
    |-- 开通 / 支付
    |-- 商家配置
    |-- 声音设置
    |-- 通话记录
    |-- 续费
```

## 4. 核心开源项目选型

| 层级 | 项目 | 用途 | 定型结论 |
|------|------|------|----------|
| 开源 CPaaS / 电话控制 | jambonz | SIP Trunk 接入、呼叫控制、媒体流 | 主线采用 |
| SIP 应用服务器 | drachtio | jambonz 底层 SIP 控制 | 由 jambonz 管理 |
| 媒体网关 | FreeSWITCH | RTP、媒体处理、转接 | 作为 jambonz 底层，不直接裸写 |
| 实时语音 Agent | Pipecat | STT/LLM/TTS 流水线、打断、事件流 | 主线采用 |
| SIP 负载均衡 | Kamailio | 高并发 SIP 入口 | 3000+ 商家后引入 |
| SIP 监控 | HOMER / HEP | SIP 信令监控、排障 | 早期就建议部署 |
| SIP 抓包 | sngrep | 单机 SIP 排障 | 运维必备 |
| SIP 压测 | SIPp | 并发呼叫压测 | 上线前必测 |
| STT | FunASR + SenseVoice | 中文电话语音识别 | 本地部署 |
| LLM | Qwen3 | 对话、预约、摘要、结构化 | 本地部署为主 |
| TTS / 声音克隆 | Fun-CosyVoice 3.0 | 语音合成、授权声音克隆 | 本地部署 |
| 推理服务 | vLLM / llama.cpp | LLM 推理 | GPU 用 vLLM，CPU/量化用 llama.cpp |
| 数据库 | PostgreSQL | 主业务数据 | 17.x |
| 缓存 | Valkey / Redis | 会话、黑名单、热点配置 | Valkey 8.x / Redis 7.2.x |
| 对象存储 | MinIO / 腾讯云 COS | 录音、音频缓存 | MVP 可用 COS，降本用 MinIO |
| 监控 | Prometheus + Grafana | 指标、告警 | 必备 |

## 5. 推荐版本

生产环境要固定小版本，禁止无计划滚动升级。

| 模块 | 推荐版本 / 系列 | 说明 |
|------|----------------|------|
| 通信节点 OS | Debian 12 | jambonz / FreeSWITCH / Kamailio 节点 |
| AI 节点 OS | Ubuntu 24.04 LTS | GPU、CUDA、AI 框架生态更顺 |
| jambonz | 当前稳定版，Docker 部署 | 以官方 docker compose / helm 部署方式为准 |
| FreeSWITCH | 1.10.x | 由 jambonz 管理，避免直接写复杂 dialplan |
| Kamailio | 6.1.x，保守可先 6.0.x | 后期多节点引入 |
| Pipecat | 当前稳定版 | 作为实时语音 Agent 框架 |
| Python | 3.12 | 后端、Pipecat、自研服务 |
| Go | 1.22+ | 后续高并发音频网关或工具服务 |
| PostgreSQL | 17.x | 主业务数据库 |
| Valkey / Redis | Valkey 8.x / Redis 7.2.x | 优先 Valkey，自建或托管均可 |
| STT | FunASR + SenseVoice Small | 电话场景先用 Small 控延迟 |
| VAD | Silero VAD | 人声检测、打断 |
| LLM | Qwen3-8B / Qwen3-30B-A3B-Instruct-2507 | 8B 实时，30B-A3B 摘要和结构化 |
| LLM 推理 | vLLM / llama.cpp | GPU 用 vLLM，量化用 llama.cpp |
| TTS | Fun-CosyVoice 3.0，保守备选 CosyVoice 2.0 | 标准音色和声音克隆 |
| 对象存储 | MinIO RELEASE 稳定版 / 腾讯云 COS | 按阶段选择 |

## 6. 中国区厂家建议

中国区运营优先考虑线路合规、微信生态、数据境内存储。

| 层级 | 首选 | 备选 | 说明 |
|------|------|------|------|
| 云服务器 | 腾讯云 CVM / GPU 云服务器 | 华为云、阿里云 | 腾讯云与微信生态协同最好 |
| 对象存储 | 腾讯云 COS | MinIO、华为云 OBS、阿里云 OSS | 录音和音频缓存 |
| 数据库 | 自建 PostgreSQL | 腾讯云 / 阿里云 / 华为云托管 PostgreSQL | MVP 自建，商业化可托管 |
| 缓存 | 自建 Valkey / Redis | 云厂商托管 Redis | 自建省钱，托管省心 |
| 小程序支付 | 微信小程序 + 微信支付 | 无 | 中国区主入口 |
| 通知 | 微信订阅消息 + 企业微信机器人 | 钉钉、短信兜底 | 避免短信成为主要成本 |
| 通信线路 | 三大运营商政企 IMS / SIP 语音专线 | 有资质线路集成商 | 必须签清楚 SLA，并验证条件呼叫转移到 Rosie 幕后接入号 |
| 短信 | 腾讯云短信 | 阿里云短信、华为云短信 | 仅用于必要兜底 |

通信线路采购建议：

- 第一优先级：直接找中国电信 / 中国联通 / 中国移动政企客户经理。
- 询问关键词：IMS、SIP Trunk、企业语音专线、DID 号码、条件呼叫转移接入。
- 第二优先级：找能提供资质、合同、SLA、号码回收机制的线路集成商。
- 不建议把容联云、阿里云语音、腾讯云呼叫中心这类按量云通信 API 作为主线。
- 合同必须写清：号码类型、SIP 接入、是否支持无应答 / 忙线 / 不可及条件呼叫转移、并发费、日呼入阈值、号码回收、故障赔偿。

## 7. 全球化厂家建议

全球运营不要复用中国区线路。每个区域独立接入当地 SIP / DID。

| 区域 | 推荐供应商 | 说明 |
|------|------------|------|
| 北美 / 欧洲 / 亚太多国 | Twilio Elastic SIP Trunking | 覆盖广，适合快速开国家 |
| 北美 / 欧洲 | Telnyx Elastic SIP Trunking | SIP 配置灵活，适合自建网关 |
| FreeSWITCH 生态兜底 | SignalWire | FreeSWITCH 背景，适合技术兜底 |
| 本地低成本号码 | 当地 DID / SIP Provider | 后期按国家单独谈 |

全球化原则：

- 中国区和海外区分开部署。
- 数据库、录音、声音样本不要默认跨境同步。
- 通信供应商通过 Carrier Adapter 抽象。
- 中国区用微信小程序。
- 海外版用 Web 控制台 + WhatsApp / Email / SMS。
- 海外 SIP 先用 Twilio / Telnyx 验证，规模上来再谈本地供应商。

## 8. 最终推荐组合

### 8.1 MVP 到 100 家商家

| 模块 | 选择 |
|------|------|
| 云厂商 | 腾讯云 |
| 通信 | jambonz + 测试 SIP Trunk |
| 底层媒体 | jambonz 管理的 FreeSWITCH |
| 实时语音 | Pipecat |
| 数据库 | PostgreSQL 17 自建 |
| 缓存 | Valkey 8.x 自建 |
| 对象存储 | 腾讯云 COS 或 MinIO |
| STT | FunASR + SenseVoice Small |
| LLM | Qwen3-8B 本地，DeepSeek API 兜底 |
| TTS | Fun-CosyVoice 3.0，保守备选 CosyVoice 2.0 |
| 监控 | Prometheus + Grafana + sngrep |
| 小程序 | 微信小程序 + 微信支付 + 订阅消息 |

### 8.2 100 到 3000 家商家

| 模块 | 选择 |
|------|------|
| 云厂商 | 腾讯云主，华为云或阿里云灾备 |
| 通信 | jambonz 集群 + 三大运营商政企 SIP / IMS |
| 网关 | jambonz 管理 FreeSWITCH 主备 |
| SIP 监控 | HOMER |
| 数据库 | 托管 PostgreSQL 17 或自建主从 |
| AI | Pipecat Agent Pool + STT/LLM/TTS 服务池 |
| 监控 | Prometheus + Grafana + 云厂商告警 |

### 8.3 3000 家以上

| 模块 | 选择 |
|------|------|
| SIP 入口 | Kamailio 双节点 |
| 呼叫控制 | jambonz 集群 |
| 媒体网关 | FreeSWITCH 多节点 |
| 通信供应商 | 至少两家运营商或线路商 |
| AI | Pipecat Agent Pool + GPU 推理池 |
| 全球扩展 | Twilio / Telnyx + 海外独立部署 |

## 9. 通话主流程

```text
1. 客户照常拨打商家原手机号或座机
2. 商家无应答、忙线或不可及时，运营商将来电转到 Rosie 幕后接入号
3. jambonz 接收 SIP Trunk 呼入
4. jambonz 调用 Rosie Call Webhook
5. Rosie 根据被叫幕后接入号查询 merchant_id
6. Rosie 返回 jambonz call control 指令
7. jambonz 建立媒体流到 Pipecat Agent
8. Pipecat 执行 VAD / STT / 防骚扰 / LLM / TTS
9. 需要预约时调用 Rosie Business API
10. 需要转人工时通过 jambonz 执行转接
11. 通话结束后 Rosie 生成摘要并推送微信通知
```

### 9.1 回拨约束流程

回拨不是默认主流程，只作为入站来电后的受控补充能力。

允许流程：

```text
1. 客户先主动来电，Rosie 生成通话记录
2. 商家在通话详情中确认需要回拨
3. Rosie 校验该回拨绑定原始 call_id / customer_id / merchant_id
4. 系统检查频率限制、黑名单、商家权限和审计策略
5. 通过 jambonz 发起单次回拨
6. 回拨结束后写入原始来电线程，生成摘要和操作日志
```

禁止流程：

- 导入号码列表后批量拨打。
- 对陌生号码自动营销。
- 根据客户通讯录自动发起促销外呼。
- 把 Rosie 做成电话销售、催收或外呼 CRM。

## 10. 防骚扰设计

防骚扰仍然放在 LLM 之前，避免营销电话消耗推理资源。

三层过滤：

1. 号码黑名单：命中后直接拒接或快速挂断。
2. 前 5 秒关键词：贷款、POS 机、代开发票、财务公司等直接挂断。
3. 行为分析：沉默、录音话术、重复句式进入观察模式。

Pipecat 中的处理顺序：

```text
Audio In
  -> VAD
  -> STT partial
  -> Spam Rule Processor
  -> Dialogue Processor
  -> LLM
  -> TTS
  -> Audio Out
```

## 11. 业务后端

Rosie 业务后端负责产品逻辑，不负责底层 SIP 细节。

| 模块 | 职责 |
|------|------|
| User Service | 用户、套餐、续费、状态 |
| Merchant Service | 商家资料、营业时间、服务项目、FAQ |
| Number Service | 幕后接入号分配、原号与接入号映射、号码回收 |
| Call Webhook | jambonz 呼叫控制入口 |
| Call Service | 通话生命周期、录音、转写、摘要 |
| Appointment Service | 时间槽、预约、冲突检测 |
| Notification Service | 微信、企业微信、钉钉、短信兜底 |
| Voice Profile Service | 声音样本授权、克隆音色、启停 |
| Admin Service | 运营后台、异常处理 |

## 12. 数据库核心表

核心表：

- users
- merchants
- merchant_numbers
- merchant_profiles
- appointment_rules
- appointments
- calls
- call_transcripts
- call_summaries
- spam_numbers
- voice_profiles
- notification_settings
- notification_logs
- payments
- carrier_accounts
- jambonz_app_bindings

### 12.1 号码映射表

```sql
CREATE TABLE merchant_numbers (
    id BIGSERIAL PRIMARY KEY,
    merchant_id BIGINT NOT NULL,
    access_number VARCHAR(32) UNIQUE NOT NULL,
    user_original_number VARCHAR(32),
    provider VARCHAR(64),
    jambonz_account_id VARCHAR(128),
    status VARCHAR(32) NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    recycled_at TIMESTAMP
);
```

### 12.2 通话记录表

```sql
CREATE TABLE calls (
    id BIGSERIAL PRIMARY KEY,
    call_id VARCHAR(128) UNIQUE NOT NULL,
    jambonz_call_sid VARCHAR(128),
    merchant_id BIGINT,
    caller_number VARCHAR(32),
    callee_number VARCHAR(32),
    started_at TIMESTAMP NOT NULL,
    ended_at TIMESTAMP,
    duration_seconds INT,
    status VARCHAR(32) NOT NULL,
    call_type VARCHAR(32),
    is_spam BOOLEAN NOT NULL DEFAULT FALSE,
    priority VARCHAR(32),
    audio_url TEXT,
    transcript TEXT,
    summary TEXT,
    extracted_info JSONB,
    created_at TIMESTAMP NOT NULL DEFAULT NOW()
);
```

### 12.3 声音配置表

```sql
CREATE TABLE voice_profiles (
    id BIGSERIAL PRIMARY KEY,
    merchant_id BIGINT NOT NULL,
    service_status VARCHAR(32) NOT NULL,
    consent_status VARCHAR(32) NOT NULL,
    sample_audio_url TEXT,
    cloned_voice_id VARCHAR(128),
    allowed_usage JSONB,
    enabled BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMP
);
```

## 13. 部署方案

### 13.1 MVP 部署

```text
[Server A: Communication]
  - jambonz
  - FreeSWITCH / drachtio
  - sngrep

[Server B: App + AI]
  - Rosie Business API
  - Rosie Call Webhook
  - Pipecat Agent
  - FunASR / SenseVoice
  - Qwen3
  - Fun-CosyVoice
  - PostgreSQL
  - Valkey / Redis
  - MinIO
```

MVP 可以两台服务器起步。预算紧张时可单机，但不建议把通信和数据库长期放在同一台机器。

### 13.2 小规模商业化

```text
[SIP Trunk]
    |
[jambonz + FreeSWITCH 主]
    |
[jambonz + FreeSWITCH 备]
    |
[Pipecat Agent x 2]
    |
[Business API x 2]
    |
[PostgreSQL + Redis/Valkey + Object Storage]
```

### 13.3 规模化

```text
[SIP Trunk Provider A/B]
        |
[Kamailio x 2]
        |
[jambonz Cluster]
        |
[FreeSWITCH Media Nodes]
        |
[Pipecat Agent Pool]
        |
[STT Pool] [LLM Pool] [TTS Pool]
        |
[Business API Pool]
        |
[PostgreSQL 主从 / Redis Cluster / Object Storage]
```

## 14. 成本结构

### 14.1 不可避免的付费项

| 项目 | 是否可避免 | 说明 |
|------|------------|------|
| 服务器 | 不可避免 | 自托管也需要机器 |
| 号码月租 | 不可避免 | Rosie 幕后接入号资源；用户原号码不变 |
| SIP Trunk 并发 | 基本不可避免 | 承接真实电话呼入 |
| 短信 | 可大幅减少 | 只做开通和预约确认兜底 |
| LLM API | 可避免 | Qwen3 本地为主 |
| STT API | 可避免 | FunASR / SenseVoice 本地 |
| TTS API | 可避免 | Fun-CosyVoice 本地 |
| 云呼叫中心 API | 可避免 | jambonz 自托管替代 |

### 14.2 开源项目带来的降本

| 难点 | 原本成本 | 使用开源项目后 |
|------|----------|----------------|
| 呼叫控制 | 自研 FreeSWITCH 业务封装 | jambonz 提供 CPaaS 层 |
| 实时语音管线 | 自研音频状态机 | Pipecat 提供 Agent pipeline |
| SIP 排障 | 人肉抓包 | HOMER + sngrep |
| 压测 | 自己写模拟器 | SIPp |
| STT/LLM/TTS | 按量 API | 本地模型 |

## 15. 稳定性设计

### 15.1 通信稳定性

- jambonz 主备或集群。
- FreeSWITCH 媒体节点健康检查。
- SIP Trunk 至少两家供应商备选。
- HOMER 记录 SIP 信令。
- sngrep 用于现场排障。
- 通话事件写入队列，避免业务后端短暂故障影响通信。

### 15.2 AI 稳定性

- Pipecat Agent 超时控制。
- STT、LLM、TTS 服务独立部署。
- LLM 超时使用固定话术。
- TTS 失败播放缓存音频。
- AI 不可用时通过 jambonz 转人工或记录后挂断。

### 15.3 数据稳定性

- PostgreSQL 每日备份。
- Redis/Valkey 不保存唯一业务事实。
- 录音进入对象存储。
- 通知失败进入重试队列。

## 16. 安全与合规

必须做到：

- 通话开场告知 AI 接待和可能录音。
- 隐私协议说明录音、转写、摘要用途。
- 手机号默认脱敏展示。
- 录音 URL 使用短期签名。
- 商家数据强隔离。
- 管理后台最小权限。
- 声音克隆必须单独授权。
- 用户可删除声音样本和克隆音色。

上线前确认：

| 项目 | 必查内容 |
|------|----------|
| 模型许可证 | Qwen3、FunASR、Fun-CosyVoice、Pipecat、jambonz 是否允许商业使用 |
| 号码资质 | 号码是否允许作为 Rosie 幕后接入号承接条件呼叫转移和 AI 接听 |
| SIP Trunk SLA | 并发、可用性、故障赔偿、扩容周期 |
| 数据存储 | 中国用户数据是否留在中国境内 |
| 声音克隆 | 单独授权、删除机制、AI 身份说明 |
| 海外运营 | 当地号码、录音告知、隐私法规、短信规则 |

## 17. 监控指标

通信指标：

- SIP 注册状态。
- 呼入成功率。
- 接听成功率。
- 异常挂断率。
- jambonz call session 数。
- FreeSWITCH 并发数。
- 每个幕后接入号日呼入量。

AI 指标：

- Pipecat Agent 会话数。
- STT 延迟和失败率。
- LLM 延迟和超时率。
- TTS 延迟和失败率。
- 本地模型 GPU / CPU / 内存占用。

业务指标：

- 有效来电数。
- 骚扰拦截数。
- 预约创建数。
- 人工转接数。
- 摘要生成成功率。
- 通知发送成功率。
- 声音克隆启用率。

## 18. 测试计划

### 18.1 通信链路测试

- SIP Trunk 呼入 jambonz。
- jambonz 调用 Rosie Call Webhook。
- jambonz 建立媒体流到 Pipecat。
- Pipecat 音频回传电话侧。
- jambonz 转人工。
- 通话结束事件回调。

### 18.2 AI 效果测试

- 常见问题问答。
- 预约时间收集。
- 预约冲突确认。
- 骚扰电话识别。
- 人工转接。
- 打断处理。
- 通话摘要。

### 18.3 压力测试

- SIPp 模拟并发呼叫。
- jambonz 并发会话。
- FreeSWITCH 媒体并发。
- Pipecat Agent 并发。
- STT / LLM / TTS 多路并发。
- 通知队列堆积与恢复。

### 18.4 试点测试

- 至少 3 个行业。
- 每个行业至少 1 家真实商家。
- 连续试点 7 天以上。
- 记录误判、漏判、延迟、投诉、转接失败。

## 19. 实施计划

### Week 1：jambonz 通信 POC

- 部署 jambonz。
- 对接测试 SIP Trunk。
- 配置一个 Rosie 测试幕后接入号。
- 实现 Rosie Call Webhook。
- 跑通接听、播放、挂断、转人工。

### Week 2：Pipecat AI 链路

- 部署 Pipecat Agent。
- 接入 jambonz 媒体流。
- 部署 FunASR / SenseVoice。
- 接入 Qwen3。
- 接入 Fun-CosyVoice。

### Week 3：业务闭环

- 商家配置。
- 号码映射。
- 防骚扰规则。
- 预约规则。
- 通话记录。
- 微信 / 企业微信通知。

### Week 4：可试点版本

- 小程序基础页面。
- 声音克隆设置入口。
- HOMER / Prometheus / Grafana。
- SIPp 压测。
- 接入真实商家试点。

## 20. 后续演进

短期：

- jambonz 单节点。
- Pipecat 单 Agent 服务。
- 本地模型为主，API 兜底。

中期：

- jambonz 主备。
- Pipecat Agent Pool。
- STT / LLM / TTS 服务池。
- HOMER 完整上线。

长期：

- Kamailio + jambonz 集群。
- 多 FreeSWITCH 媒体节点。
- 多运营商线路。
- 海外 Twilio / Telnyx 独立区域部署。

## 21. 最终架构一句话

Rosie AI 的新定型方案是：用 jambonz 解决电话和 SIP 复杂度，用 Pipecat 解决实时语音 Agent 复杂度，用本地开源模型控制 AI 成本，用微信小程序完成中国区商业闭环。
