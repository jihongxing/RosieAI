# Rosie AI 号码与 SIP Trunk 接入答疑

更新时间：2026-04-30

## 1. jambonz 为什么能接运营商号码的电话？

jambonz 本身不是号码运营商，不能凭空接到一个手机号或座机号的电话。它能接电话，是因为某个运营商或线路服务商把这通电话通过 SIP 送到了 jambonz。

真实链路是：

```text
客户拨打电话
  -> 公共电话网 PSTN / 运营商网络
  -> 可入站号码 / SIP Trunk / IMS 中继
  -> jambonz
  -> Rosie call hook
  -> realtime-voice / Go API
```

所以关键不是“jambonz 有没有号码”，而是：

```text
被拨打的号码最终是否被线路方以 SIP INVITE 送到 jambonz。
```

## 2. 商家原号码和 Rosie 接入号是什么关系？

Rosie 项目里应该明确区分两个号码：

```text
商家原号码 original_number
老板已有手机号、座机号或门店电话，客户原本拨打它。

Rosie 接入号 access_number
平台分配给商家的幕后接入号，商家把无人接听 / 忙线 / 不可及时的来电转移到它。
```

典型链路是：

```text
客户拨打商家原号码
  -> 商家运营商判断无人接听 / 忙线 / 不可及
  -> 呼叫转移到 Rosie 接入号
  -> Rosie 接入号所属线路把电话送到 jambonz
  -> jambonz 调用 Rosie call hook
```

因此，商家设置呼叫转移时转到的号码必须是一个真实可拨打、且已经路由到 jambonz 的 Rosie 接入号。

## 3. “jambonz 提供虚拟号”是什么意思？

严格说，jambonz 通常不是底层号码资源方。所谓 jambonz 提供虚拟号，通常意味着：

```text
jambonz cloud / 部署方已经接好了某个 DID / SIP trunk 供应商，
然后在 jambonz 控制台里把这些号码暴露出来给应用使用。
```

号码仍然来自底层运营商、DID 服务商或线路集成商，只是 jambonz 帮你管理号码与 application/call hook 的绑定。

## 4. 生产环境用 jambonz.cloud 还是自部署？

建议路线：

```text
试点和早期生产：优先使用 jambonz.cloud
业务稳定后：保留 cloud/self-host 双兼容
规模、合规、成本和运维能力成熟后：再评估自部署
```

原因：

- 当前最大风险是能否稳定打通真实电话链路、验证商家付费意愿和运营流程，而不是 jambonz 成本。
- 自部署 jambonz 不只是部署一个 Web 服务，还涉及 SBC、RTP、Feature Server、Redis、数据库、监控、证书、网络和 SIP 故障排查。
- 中国区如果后续有数据驻留、网络质量、专线/SIP Trunk 深度控制要求，再考虑自部署更合理。
- 现有 Rosie 代码已把号码池、route-check 和 jambonz sync 抽象出来，后续从 cloud 切到 self-host 不应影响商家业务层。

短期结论：

```text
先用 jambonz.cloud 跑 3-10 家试点。
不要过早把团队拖进通信平台自运维。
```

## 5. 中国区运营商是否支持 SIP Trunk？

支持，但通常不是面向开发者自助开通的海外 DID 模式，而是政企语音中继 / 云中继 / IMS 中继 / 语音专线 / 企业总机 / 呼叫中心线路这类产品形态。

可能的产品名称包括：

- SIP 中继
- 云中继
- IMS 中继
- 语音专线
- 数字中继 E1/PRI + 网关
- 400 / 95 / 呼叫中心接入
- 企业总机 / 云 PBX 线路

中国区难点通常不在 SIP 技术是否存在，而在商务、资质、用途审核和合规管控。

## 6. 向运营商或线路集成商必须确认的问题

### 6.1 入站能力

- 是否支持把真实 PSTN 呼入以 SIP INVITE 送到我们的 jambonz / SBC？
- 是注册型 SIP 账号，还是 IP 白名单 / 专线对接？
- 是否支持只做入站接听，不做外呼？
- 是否支持并发扩容？最小并发数和扩容周期是多少？

### 6.2 号码资源

- 能提供哪类号码作为 Rosie 接入号：固话、95、400、虚商号段、专用 DID？
- 号码是否可被普通手机号/座机直接拨打？
- 号码是否支持被商家原号码设置为呼叫转移目标？
- 号码月租、开通费、最低消费、呼入计费分别是多少？
- 号码回收规则是什么？欠费、停用、投诉后如何处理？

### 6.3 呼叫转移

- 商家原号码是否支持无应答 / 忙线 / 不可及条件呼叫转移？
- 呼叫转移到 Rosie 接入号时，是否保留原主叫号码？
- 是否能在 SIP INVITE 中保留被叫号码、转移信息或 Diversion / History-Info / P-Asserted-Identity？
- 不同运营商之间转移是否稳定，是否有额外计费？

### 6.4 SIP/Jambonz 对接

- SIP INVITE 送到哪个公网 IP / 域名 / 端口？
- 是否要求固定源 IP 白名单？
- 支持 UDP/TCP/TLS 哪些 SIP 传输方式？
- RTP 端口范围、编解码支持和 DTMF 方式是什么？
- 是否支持 G.711 A-law / mu-law？是否支持 Opus？
- 是否能把不同接入号路由到不同 SIP trunk 或不同 jambonz application？
- 发生失败时是否能提供 SIP ladder / CDR / 错误码？

### 6.5 合规和用途

- AI 接待、通话摘要、漏接电话整理是否属于允许用途？
- 是否需要呼叫中心许可证、增值电信许可、95/400 资质或备案？
- 是否允许平台把一个号码池分配给多个商家使用？
- 是否要求每个商家的原号码、营业执照、授权书单独备案？
- 是否要求开场语音告知“AI 接待 / 生成文字摘要 / 可能录音”？
- 是否禁止营销外呼、自动拨号、骚扰投诉高风险行为？

### 6.6 运维和 SLA

- SLA、故障响应时间和赔偿规则是什么？
- 是否提供实时 CDR、通话录音、质检和失败明细？
- 是否支持测试环境或试点号码？
- 能否先给 1-3 个测试接入号跑 20 通真实呼入？
- 是否支持跨省部署和多地容灾？

## 7. Rosie 当前软件侧已经补齐的能力

当前代码已经补齐平台侧的号码管理基础：

- `access_numbers` 号码池。
- 运营侧导入、查看、分配、释放接入号。
- 商家开通试用时自动分配可用接入号。
- `GET /admin/access-numbers/route-check` 检查号码池与路由配置一致性。
- `POST /admin/jambonz/config-export` 导入 jambonz 配置快照。
- `POST /admin/jambonz/sync` 通过 jambonz REST API 自动同步 applications / phone numbers。
- 自动分配只使用已经绑定 SIP trunk、jambonz application、Rosie call hook 的号码。

也就是说，软件侧已经能判断：

```text
号码是否存在
号码是否可用
号码是否绑定商家
号码是否绑定 SIP trunk
号码是否绑定 jambonz application
application 是否指向 Rosie call hook
```

但真实上线仍需要商务/线路侧完成：

```text
真实号码资源
SIP trunk / IMS 入站
号码到 jambonz 的路由
合规授权和用途确认
20 通以上真实手机呼入验证
```

## 8. 推荐落地路径

第一步：继续用 jambonz.cloud 跑真实试点。

第二步：通过运营商政企客户经理或有资质线路集成商拿 1-3 个真实可入站接入号。

第三步：把接入号接到 jambonz，导入或同步到 Rosie 号码池。

第四步：在 `/admin` 后台确认 route-check 全部 ready。

第五步：让商家把原号码条件呼叫转移到 Rosie 接入号。

第六步：完成 20 通真实呼入测试，再进入 3-10 家商家试点。
