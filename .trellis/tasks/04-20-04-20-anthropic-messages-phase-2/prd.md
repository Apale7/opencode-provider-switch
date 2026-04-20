# Anthropic Messages Support Phase 2

## 概要

第二期建立在第一期 protocol-aware foundation 之上，为项目新增 Anthropic Messages 协议的真实支持。

目标不是把 `ocswitch` 做成任意协议之间的万能转换网关，而是在既有“provider + alias + deterministic failover + OpenCode sync + desktop UI”产品形态下，增加第二种正式协议：Anthropic Messages。

用户明确要求：

1. provider、alias 需要选择协议。
2. 添加 Anthropic 协议 provider 时，需要按 Anthropic 协议获取模型列表。
3. sync opencode 功能需要为 Anthropic 协议增加一个新的 provider，具体 sync 逻辑与当前 `openai-responses` 的 sync 逻辑一致。
4. 日志页面需要体现请求的接口协议。

此外，本期还需要补充一组工程上必需但用户未逐条写出的实用细节，包括：

- Anthropic 请求认证头与版本头
- Anthropic `/v1/models` 的最小兼容处理
- `/v1/messages` 的本地代理入口
- provider import 对 `@ai-sdk/anthropic` 的支持
- 与现有 failover 语义兼容的 streaming 处理边界

## 产品目标

本期交付后，项目需要同时支持两条本地协议路径：

1. `openai-responses`
2. `anthropic-messages`

并满足：

1. 用户可以创建 Anthropic provider。
2. 用户可以创建 Anthropic alias，并且只能绑定 Anthropic provider 的模型。
3. 本地代理可接受 `POST /v1/messages` 并按 alias 顺序做同协议 failover。
4. `ocswitch opencode sync` 能把 Anthropic alias 以单独 provider 的方式暴露给 OpenCode。
5. 日志与网络视图可明确区分请求协议。

## 非目标

1. 不做 OpenAI Responses 与 Anthropic Messages 的双向协议翻译。
2. 不做跨协议 alias 路由。
3. 不做 Anthropic tools 全量兼容映射。
4. 不做 thinking / redacted thinking / compaction 等高级特性的 UI 级建模。
5. 不做 image / document / PDF 等多模态额外增强。
6. 不扩展 provider 模型缓存为复杂 capability schema，继续以 `[]string` 模型 id 为最小可用设计。

## 协议事实与工程约束

### Anthropic 最小请求事实

最小消息生成请求：

- 路径：`POST /v1/messages`
- 认证头：`x-api-key: <key>`
- 版本头：`anthropic-version: 2023-06-01`

最小请求体核心字段：

- `model`
- `max_tokens`
- `messages`

流式时增加：

- `stream: true`

### Anthropic 模型列表事实

模型列表路径：

- `GET /v1/models`

返回结构虽然比 OpenAI `/v1/models` 更丰富，但同样有：

- `data[].id`

这与当前项目只缓存 `[]string` 模型 id 的最小设计兼容。

### Anthropic SSE 对 failover 的影响

当前项目语义是：

- 首字节前失败可切换
- 首字节后不切换

Anthropic streaming 下，最先到达的首块字节可能不是文本，而是：

- `message_start`
- `ping`
- `content_block_start`
- `thinking_delta`
- `input_json_delta`

因此一旦任意 SSE 事件首字节开始向下游写出，就必须锁定当前上游，不再 failover。

本期必须明确保持这个语义，不做例外处理。

## 范围决策

### 本期必须支持

1. Anthropic provider 配置、编辑、展示。
2. Anthropic alias 配置、编辑、展示。
3. Anthropic provider import。
4. Anthropic model discovery。
5. 本地 `POST /v1/messages` 转发与同协议 failover。
6. OpenCode sync 输出单独的 Anthropic provider。
7. trace / log / network 体现 protocol。

### 本期最多支持到“原样透传”的内容

只要客户端已经发送的是 Anthropic Messages 合法请求，本地代理负责：

- alias 解析
- model 重写
- 上游请求头补齐
- streaming/non-streaming 透传
- 首字节前 failover

不负责：

- 帮 OpenAI 风格请求改写成 Anthropic 风格
- 对 tools / thinking / content blocks 做深层结构归一化

## 数据模型与契约

### Protocol 值

第二期正式启用两种协议：

- `openai-responses`
- `anthropic-messages`

### Provider

`internal/config.Provider.Protocol`

新增合法值：

- `anthropic-messages`

语义：

- provider 的 base URL、认证方式、模型发现方式，都由协议决定

### Alias

`internal/config.Alias.Protocol`

新增合法值：

- `anthropic-messages`

约束：

- Anthropic alias 只能绑定 Anthropic provider
- OpenAI Responses alias 只能绑定 OpenAI Responses provider

### Trace

`RequestTrace.Protocol`

新增第二个值：

- `anthropic-messages`

用于：

- log list badge
- log detail summary
- network detail summary
- 后续协议筛选能力

## Provider 设计

### Anthropic provider 创建

UI / CLI 创建 Anthropic provider 时：

- 协议选择 `anthropic-messages`
- `baseURL` 要求指向 Anthropic 风格 `/v1` 根
- `apiKey` 允许通过专门字段保存
- `headers` 默认追加或保留：
  - `anthropic-version: 2023-06-01`

建议规则：

- `apiKey` 仍保存在统一字段 `api_key`
- 真正发请求时按协议决定写入 `Authorization` 还是 `x-api-key`
- `anthropic-version` 放在协议默认请求头层，而不是要求用户每次手填

### 默认头策略

建议新增“协议默认头合成”层，而不是把默认头写死在 UI 表单里。

例如：

- OpenAI Responses：无额外默认头
- Anthropic Messages：默认补 `anthropic-version: 2023-06-01`

用户自定义 headers 仍允许覆盖或追加。

### 模型发现

新增协议感知模型发现：

- `FetchProviderModels(protocol, baseURL, apiKey, headers)`

Anthropic 分支要求：

- 请求 `GET {baseURL}/models`
- 认证头使用 `x-api-key`
- 自动带 `anthropic-version`
- 解析 `data[].id` 作为本地缓存模型列表

本期不存：

- `display_name`
- `capabilities`
- `max_tokens`

因为当前项目的 alias 绑定与校验只需要模型 id。

## Provider Import 设计

### OpenCode import 扩展

`internal/opencode.ImportCustomProviders()` 当前只识别 `@ai-sdk/openai`。

本期需要扩展为：

- 能识别 `@ai-sdk/anthropic`

新增 `ImportableProvider` 字段：

- `Protocol`

导入规则：

- `@ai-sdk/openai` -> `openai-responses`
- `@ai-sdk/anthropic` -> `anthropic-messages`

Anthropic import 需要从 OpenCode provider entry 中提取：

- `baseURL`
- `apiKey` 或兼容的认证字段
- `models`
- 可选 headers

### 不确定项与保守策略

如果 OpenCode 对 Anthropic provider 的字段形态在仓库内无法完全证明，本期应采取保守策略：

- 只支持明确可识别的 `@ai-sdk/anthropic` provider entry
- 无法稳定解析的字段只给 warning，不做猜测性导入

## Alias 设计

### 创建与编辑

Anthropic alias 的创建流程与现有 alias 基本一致，但协议必须显式选择。

要求：

- create / edit 都展示协议 radio
- 创建后协议不可通过 target 自动漂移
- alias 列表与详情展示 protocol badge

### 绑定约束

`BindAliasTarget()` 必须新增严格校验：

- `alias.protocol == provider.protocol`

错误提示需要直接告诉用户：

- 当前 alias 是 Anthropic Messages
- 只能绑定 Anthropic Messages provider

### 自动创建 alias

当 `alias bind` 遇到 alias 不存在时：

- 自动创建 alias
- alias.protocol 从 provider.protocol 继承

这是防止 CLI 自动创建路径继续默默落成错误默认协议的关键点。

## Proxy 设计

### 本地路由

`internal/proxy/server.go` 需要新增：

- `/v1/messages`

保持现有：

- `/v1/responses`
- `/v1/models`

### 入口处理

建议新增与 `handleResponses()` 平行的：

- `handleAnthropicMessages()` 或 `handleMessages()`

该入口职责：

1. 校验方法与本地 API key
2. 读取 JSON body
3. 提取 `model`
4. 将 raw model 解析为本地 alias
5. 查找 `protocol=anthropic-messages` 的 alias
6. 校验 alias 可用 targets
7. 重写 payload 中的 `model` 为 target model
8. 构造上游 `POST {baseURL}/messages`
9. 写入 Anthropic 认证头和版本头
10. 复用当前首字节前 failover 机制

### 错误处理

本期建议：

- 对 Anthropic 路径返回 Anthropic 风格错误 envelope
- 不要继续复用 OpenAI error envelope

原因：

- 用户直接走 Anthropic 客户端时，会期待 Anthropic 语义错误结构

但实现上不需要全量复制所有 error shape，只要做到：

- 缺 model
- alias 不存在
- unauthorized
- method not allowed
- upstream transport/5xx/429 最终失败

都能返回稳定的 Anthropic 风格 JSON 即可。

### Streaming 处理

本期要求：

- 对 `text/event-stream` 原样透传
- 任意首块字节写出后不再 failover
- 流中 `event: error` 视为首字节后失败，不做切换

### /v1/models 本地输出

当前 `/v1/models` 返回 alias 列表。

第二期需要决定如何处理本地模型枚举：

- 最保守方案：继续返回所有当前可路由 alias 名，不区分协议
- 更合理方案：按请求入口或认证上下文区分

考虑当前项目结构，建议本期采用保守方案：

- `/v1/models` 仍返回本地所有可路由 alias
- 但每个 alias 的协议信息不在该接口暴露

原因：

- 当前 OpenCode 与现有客户端主要把它当连接探针，而不是严格协议发现接口

## OpenCode Sync 设计

### 当前问题

当前 sync 只生成：

- `provider.ocswitch`
- `npm: @ai-sdk/openai`

### 第二期目标

新增一份 Anthropic protocol 对应的 OpenCode provider，逻辑与当前 sync 一致：

- 生成 provider entry
- 写入对应 alias 模型列表
- 可选设置 top-level `model` / `small_model`

### 推荐输出形态

建议保留现有 OpenAI provider：

- `provider.ocswitch`

新增 Anthropic provider：

- `provider.ocswitch-anthropic`

推荐原因：

- 不破坏现有 sync contract
- key 名可读且稳定
- 避免一个 provider entry 混装多协议模型

### Builder 抽象

`internal/opencode/opencode.go` 需要从“单 provider spec”变成“按协议生成多个 provider spec”。

建议抽象：

- `BuildSyncedProvider(protocol, baseURL, apiKey, aliasNames)`
- `EnsureSyncedProviders(...)`
- `ValidateSyncedProviders(...)`

### Anthropic provider spec

需要确认或保守约束的内容：

- `npm`：应为 `@ai-sdk/anthropic`
- `options.baseURL`：本地 `ocswitch` 代理根地址 `/v1`
- `options.apiKey`：本地代理 api key
- 如果 OpenCode 需要额外 header 支持来驱动 Anthropic provider，则需在 sync builder 中输出固定 header 或等价选项

如果仓库内仍无法完全证明某些 Anthropic provider options 字段，PRD 应允许实现时以最保守、最小可跑通的 OpenCode 配置为准。

### Sync alias 集合

要求：

- `provider.ocswitch.models` 只同步 `openai-responses` alias
- `provider.ocswitch-anthropic.models` 只同步 `anthropic-messages` alias

如果用户设置：

- `--set-model`
- `--set-small-model`

则必须校验该 alias 所属 provider key 与 model 引用一致。

## UI 设计

### Provider 视图

需要新增：

- protocol radio 两个选项
  - OpenAI Responses
  - Anthropic Messages
- 当选择 Anthropic 时：
  - base URL 文案提示改为 Anthropic `/v1` 根
  - 模型发现说明改为 Anthropic `/v1/models`
  - 默认头说明展示 `anthropic-version`

### Alias 视图

需要新增：

- protocol radio 两个选项
- target 绑定弹窗仅显示相同协议 provider
- 若当前 alias 为 Anthropic 协议，列表与详情中明确展示

### 日志与网络视图

要求：

- 卡片级 protocol badge
- detail summary 显示 protocol
- Anthropic 请求时，network detail 中应能看见 `/v1/messages` URL

### Sync 页面

需要新增协议维度说明：

- 预览结果中明确列出每个同步 provider key 及其 alias 数
- 不再只展示单一 `provider.ocswitch`

## 测试设计

### 配置与服务层

- provider 保存 Anthropic protocol
- alias 保存 Anthropic protocol
- alias 绑定拒绝跨协议 provider
- import provider 支持 `@ai-sdk/anthropic`

### 模型发现

- `internal/opencode/provider_models_test.go`
  - Anthropic `/v1/models` 响应可解析 `data[].id`
  - Anthropic 请求头包含 `x-api-key`
  - Anthropic 默认头包含 `anthropic-version`

### Proxy

- `internal/proxy/server_test.go`
  - `/v1/messages` 缺 alias 返回错误
  - 429 / 5xx 可在首字节前 failover
  - 已开始 SSE 后流中断不 failover
  - trace.protocol = `anthropic-messages`

### OpenCode sync

- `internal/opencode/opencode_test.go`
  - 生成两个 provider entry
  - alias 按协议分流
  - validate 可同时校验两个 provider spec

### 前端

- provider protocol 交互
- alias protocol 交互
- target provider 按协议过滤
- log/network 协议显示
- sync 预览展示多 provider

## 实施顺序

### Step 1 Provider / Alias / Import

- provider protocol 双选项启用
- alias protocol 双选项启用
- import 扩展到 `@ai-sdk/anthropic`

### Step 2 Model discovery

- 协议感知模型发现
- Anthropic `/v1/models` 请求头与解析

### Step 3 Proxy

- 新增 `/v1/messages`
- Anthropic request / response / stream 透传
- trace protocol 补齐

### Step 4 Sync

- 多协议 provider spec builder
- `provider.ocswitch-anthropic`
- preview/apply/doctor 同步更新

### Step 5 UI / Regression

- GUI 双协议交互
- 日志与 network 展示
- 构建与手工回归

## 验收标准

### 功能验收

1. 用户可以创建 Anthropic provider 并成功发现模型列表。
2. 用户可以创建 Anthropic alias，并只能绑定 Anthropic provider。
3. 本地代理可成功处理 `POST /v1/messages`。
4. Anthropic 路径在首字节前失败时可切换，首字节后失败不切换。
5. `ocswitch opencode sync` 会新增一个 Anthropic provider entry，并同步 Anthropic alias。
6. 日志与网络页面可明确显示协议与 `/v1/messages` 请求链路。

### 边界验收

1. 不支持跨协议 alias。
2. 不支持 OpenAI Responses 请求自动翻译成 Anthropic Messages。
3. 不支持 Anthropic tools / thinking / multimodal 的深层兼容处理。

## 风险与规避

### 风险 1：把协议支持误做成协议翻译

规避：

- 本期只做代理透传与 alias/model 重写，不做请求结构转换

### 风险 2：Anthropic SSE 早早锁死 failover 窗口

规避：

- 明确保持当前首字节语义，测试覆盖 `message_start`/首块字节后失败不切换

### 风险 3：OpenCode Anthropic provider spec 不确定

规避：

- 以 `@ai-sdk/anthropic` 为目标实现
- 若具体 options 字段有不确定项，先走最小稳定配置，必要时通过 OpenCode 实测校验补充

### 风险 4：范围膨胀到 tools / thinking / multimodal

规避：

- 在实现和测试中严格限定支持边界
- 任何超出原样透传的高级特性都留到后续任务

## 本期绝对不要做

1. 不做 OpenAI Responses <-> Anthropic Messages 双向协议转换。
2. 不做完整 tools schema 兼容与 tool delta 组装。
3. 不做 thinking 事件的展示与归一化。
4. 不做图片、文档、PDF 等多模态增强。
5. 不做 provider 模型能力缓存扩展。
6. 不改变现有 failover 核心语义。

## 本期完成后的项目状态

完成第二期后，项目应达到：

- `ocswitch` 已原生支持两种正式协议
- provider / alias / sync / trace / GUI 都能正确区分协议
- OpenCode 可以同时看到 OpenAI Responses 与 Anthropic Messages 两类本地 provider 暴露
- 但项目依然保持克制边界，没有演变成跨协议万能翻译网关
