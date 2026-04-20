# Protocol-Aware Foundation Phase 1

## 概要

当前 `ocswitch` 的数据模型、CLI、GUI、OpenCode sync、proxy trace 与文档都默认只有一种协议：OpenAI Responses。

这个假设目前散落在多层实现中：

- `internal/config/config.go` 里 `Provider` / `Alias` / `Target` 没有协议字段
- `internal/app/manage.go` 与 `internal/opencode/provider_models.go` 默认用 OpenAI 兼容 `/v1/models`
- `internal/opencode/opencode.go` 的 sync contract 固定写 `provider.ocswitch` 且 `npm` 为 `@ai-sdk/openai`
- `internal/proxy/server.go` 只暴露 `/v1/responses`，trace 中也没有协议字段
- `frontend/src/types.ts`、`frontend/src/api.ts`、`frontend/src/App.tsx` 的 provider/alias 表单与日志视图都不表达协议

第一期的目标不是引入新的协议实现，而是把项目从“单协议硬编码”重构为“协议感知但当前只启用一种协议”的状态，为第二期接入 Anthropic Messages 打基础。

用户明确要求：

1. 只考虑新增协议字段，不考虑 Anthropic 协议支持的具体逻辑。
2. 补齐新增协议字段后的所有逻辑和页面交互，协议需要可选，即使当前只有一个选项，也要用单选组件表达。
3. 修改所有把 OpenAI Responses 当成唯一协议写死的地方，做合理抽象。
4. 日志页面需要体现当前接口协议。
5. 所有为后续增加新的协议种类的准备和重构工作都要在本期完成。

本期验收标准：完成重构后，CLI 与 UI 仍然可用，现有 OpenAI Responses 功能不退化。

## 产品目标

本期只解决“协议存在感”和“协议抽象基础”两个问题。

需要达成的结果：

1. 配置层能够明确表达 provider / alias / request trace 所属协议。
2. CLI 与桌面 GUI 都能查看、创建、编辑带协议的 provider / alias。
3. 所有现有默认值都显式落到 `openai-responses`，而不是隐式假设。
4. 与协议相关的硬编码逻辑被收敛到少数中心位置，第二期新增协议时不需要再做大面积清理。
5. 现有 `POST /v1/responses`、`/v1/models`、provider 导入、alias 绑定、sync opencode、日志查看、doctor 仍按当前行为正常工作。

## 非目标

1. 本期不新增 `POST /v1/messages` 或任何 Anthropic 代理路径。
2. 本期不实现 Anthropic provider import、Anthropic model discovery、Anthropic sync 输出。
3. 本期不改变现有 failover 语义：仍然是首字节前可切换、首字节后不切换。
4. 本期不扩展模型元数据存储结构，继续只缓存 `[]string` 模型 id。
5. 本期不引入跨协议路由，不允许一个 alias 同时引用不同协议 provider。

## 当前事实与约束

### 配置层当前事实

- `internal/config/config.go`
  - `Target` 只有 `provider/model/enabled`
  - `Alias` 只有 `alias/display_name/enabled/targets`
  - `Provider` 只有 `id/name/base_url/api_key/headers/models/models_source/disabled`
- `ValidateProviderBaseURL()` 当前要求 `base_url` 必须以 `/v1` 结尾，这是 OpenAI 风格硬编码。
- `Save()` / `Load()` 当前没有版本迁移层，新增字段需要天然兼容旧配置读写。

### 应用层当前事实

- `internal/app/manage.go`
  - `UpsertProvider()` 会调用 `opencode.FetchProviderModels()` 自动发现模型
  - `ImportProviders()` 只认 `ImportCustomProviders()` 返回的 OpenAI custom providers
  - `BindAliasTarget()` 只验证 provider 存在与 model 是否命中已发现模型，没有协议一致性校验
- `internal/app/types.go`
  - `ProviderView`、`AliasView`、`ProviderUpsertInput`、`AliasUpsertInput`、`RequestTraceView` 当前都没有协议字段

### OpenCode 集成当前事实

- `internal/opencode/opencode.go`
  - `ProviderKey` 固定为 `ocswitch`
  - `EnsureOcswitchProvider()` 固定写 `npm: @ai-sdk/openai`
  - `ValidateOcswitchProvider()` 固定校验 `@ai-sdk/openai`
- `internal/opencode/provider_models.go`
  - `FetchProviderModels()` 固定按 OpenAI 风格 `GET {baseURL}/models` 解析 `data[].id`

### 代理与日志当前事实

- `internal/proxy/server.go`
  - 只挂 `/v1/responses` 和 `/v1/models`
  - trace 没有 protocol 字段
- `internal/proxy/traces.go`
  - `RequestTrace` 记录 alias、raw model、final provider、final model、attempt 链，但不区分协议
- `frontend/src/App.tsx`
  - Log / Network 详情页展示 provider、model、URL、attempt 等信息，但不显示协议

### UI 当前事实

- Provider 表单与 Alias 表单都是单页详情区，没有协议选择项
- 现有控件体系已经有 `select` 与筛选组，但没有单选组组件
- 用户要求即使当前只有一种协议，也要按单选组件实现，以便第二期直接扩展

## 核心设计原则

### 1. 协议先成为领域字段，再成为实现分支

本期第一优先级是把 `protocol` 变成稳定的数据契约字段，而不是先做协议行为分发。

要求：

- provider 有 `protocol`
- alias 有 `protocol`
- trace 有 `protocol`
- 与 sync 相关的 alias 集合视图也要能按协议区分

### 2. 旧数据无感迁移到 `openai-responses`

旧配置文件不做破坏式迁移，也不要求用户手工改配置。

要求：

- 旧配置读入时，如果 provider / alias 没有 `protocol`，在内存视图中视为 `openai-responses`
- 保存时，统一把 `protocol` 写回文件，完成一次自然迁移
- 旧 CLI / UI 操作路径继续可用，不要求用户补填协议

### 3. 一期只做“协议感知”，不做“协议分发”

本期不新增协议执行逻辑，只需要把“当前请求使用的本地接口协议”表达清楚。

因此本期的 proxy 行为仍然只有：

- 本地支持 `openai-responses`
- `handleResponses()` 产出的 trace 协议固定为 `openai-responses`

但相关代码不应再把这个常量散落在各处字符串里，而是统一抽象。

### 4. 所有跨层契约统一使用协议枚举常量

后端、前端、sync、trace、表单、校验文案必须共享同一组协议命名。

推荐本期唯一正式值：

- `openai-responses`

不建议使用：

- `openai`
- `responses`
- `openai_response`

因为这些名字后续会与 package 名、endpoint 名、导入来源名混淆。

## 数据模型设计

### Protocol 枚举

新增中心协议常量定义，建议落点：

- `internal/config/protocol.go` 或 `internal/config/config.go` 内部协议辅助段
- `frontend/src/types.ts` 中新增 `ProtocolKind`

建议值：

```text
openai-responses
```

为第二期预留，但本期不启用的值可以先不暴露到 UI 枚举里。

### Provider

`internal/config/config.go`

新增：

- `Protocol string json:"protocol,omitempty"`

语义：

- 表示该上游 provider 所遵循的请求协议
- 本期默认固定为 `openai-responses`

配套变更：

- `ProviderView`
- `ProviderUpsertInput`
- `ProviderSaveResult`
- `frontend/src/types.ts` 对应类型
- `frontend/wailsjs/go/models.ts` 自动生成绑定

### Alias

`internal/config/config.go`

新增：

- `Protocol string json:"protocol,omitempty"`

语义：

- 表示该 alias 可被哪个本地协议入口消费
- 本期 alias 只能是 `openai-responses`

配套变更：

- `AliasView`
- `AliasUpsertInput`
- `frontend/src/types.ts` 对应类型

### Target

本期不建议在 `Target` 上新增 `protocol` 字段。

原因：

- target 的协议天然应继承 provider
- provider 与 alias 已经有协议字段，target 重复存储只会引入不一致风险
- 真正需要校验的是“alias.protocol == provider.protocol”

因此 target 仍保持：

- `provider`
- `model`
- `enabled`

### RequestTrace

`internal/proxy/traces.go`

新增：

- `Protocol string json:"protocol,omitempty"`

语义：

- 表示此次请求命中的本地代理协议
- 对当前 `handleResponses()` 固定写 `openai-responses`

如果一期只加请求级协议，不加 attempt 级协议即可。

原因：

- attempt 一定跟请求协议一致
- 二期如果需要多入口，也仍然是每个入口内 attempt 同协议

## 抽象与重构设计

### 1. 收敛协议常量与默认值

需要把以下散落假设收敛成中心辅助函数：

- 默认 provider 协议
- 默认 alias 协议
- provider 协议显示名
- protocol -> 本地入口路径映射
- protocol -> upstream model discovery 策略
- protocol -> OpenCode sync provider spec

本期至少建立这些抽象接口，即使实现只有一个分支。

### 2. Base URL 校验改为协议感知

当前 `ValidateProviderBaseURL()` 直接要求 `/v1` 后缀。

本期建议改造为：

- `ValidateProviderBaseURL(protocol, baseURL)`

本期唯一规则仍然是：

- `openai-responses` provider 的 `baseURL` 必须指向 `/v1`

这样第二期接 Anthropic 时，只需要新增协议规则，而不必再回头拆签名。

### 3. 模型发现改为协议感知

当前 `FetchProviderModels(baseURL, apiKey, headers)` 是 OpenAI 风格固定实现。

本期建议改造为：

- `FetchProviderModels(protocol, baseURL, apiKey, headers)`

本期仍只实现 `openai-responses` 分支，底层行为不变。

收益：

- 第二期接 Anthropic `/v1/models` 时直接新增分支
- `manage.go`、CLI、desktop API 不需要再改调用签名

### 4. OpenCode sync provider spec 改为协议感知

当前 `internal/opencode/opencode.go` 把 `provider.ocswitch` 固定写为 OpenAI provider。

本期不改变 sync 结果，但要把内部实现重构为：

- 协议无关的 alias 收集逻辑
- 协议相关的 provider spec builder

本期仅保留：

- `openai-responses -> provider.ocswitch -> @ai-sdk/openai`

但结构上要允许第二期新增：

- 另一份 provider key / provider spec / alias 集合

### 5. 日志视图补 protocol 展示

本期日志页与网络页必须显示协议。

最小要求：

- log 列表卡片：显示协议 badge
- log 详情：显示 protocol 字段
- network 详情：显示 protocol 字段

本期 trace 仍只有一种协议，因此 UI 的价值主要是“把协议概念明确展示出来”。

## CLI 设计

### Provider CLI

涉及文件：

- `internal/cli/provider.go`
- `internal/cli/root.go`
- 相关 CLI tests

建议新增：

- `ocswitch provider add --protocol openai-responses`

行为要求：

- 参数可选，默认 `openai-responses`
- `provider list` 输出中显示 protocol 列或 bracket 标签
- help 文案不再写“OpenAI-compatible”作为唯一协议事实，而是写“当前支持的协议：openai-responses”

### Alias CLI

涉及文件：

- `internal/cli/alias.go`

建议新增：

- `ocswitch alias add --protocol openai-responses`

行为要求：

- 参数可选，默认 `openai-responses`
- `alias list` 输出中显示 protocol
- `alias bind` 在绑定时校验 alias 与 provider 协议一致
- 若 alias 不存在自动创建，自动创建时协议取 provider 的协议；本期会自然成为 `openai-responses`

### Doctor / Sync CLI

行为要求：

- doctor 输出需让用户知道当前校验的是哪一种协议的 sync 视图
- `ocswitch opencode sync --help` 文案需不再暗示“ocswitch 只有一种协议”，而是解释当前只同步 `openai-responses` alias

## GUI 设计

### Provider 表单

涉及文件：

- `frontend/src/App.tsx`
- `frontend/src/types.ts`
- `frontend/src/i18n/*.json`

新增一组单选组件：

- 标题：`Protocol`
- 选项：`OpenAI Responses`

要求：

- 即使只有一个选项，也用单选组，不用下拉框
- create / edit 模式都显示
- detail 头部摘要与 provider 卡片上都展示协议 badge

### Alias 表单

新增一组单选组件：

- 标题：`Protocol`
- 选项：`OpenAI Responses`

要求：

- create / edit 模式显示
- alias 列表卡片与详情摘要展示协议
- target 绑定弹窗里仅显示与当前 alias 协议一致的 provider

### Provider Import 弹窗

本期仍只导入 OpenAI custom providers。

但 UI 文案必须改成：

- “导入支持的 provider 协议”
- 当前仅导入 `openai-responses` 协议来源

避免继续用“所有 provider import”这种误导性表述。

### 日志与网络页

要求：

- trace 卡片显示 protocol badge
- detail summary 增加 protocol 项
- 不要求按协议筛选，但代码结构要允许后续加 filter

## 跨层数据流

### Provider 创建 / 编辑

```text
UI protocol radio
  -> frontend/src/types.ts ProviderUpsertInput.protocol
  -> frontend/src/api.ts saveProvider()
  -> internal/app/types.go ProviderUpsertInput.Protocol
  -> internal/app/manage.go UpsertProvider()
  -> internal/config.Provider.Protocol
  -> config save/load round-trip
```

### Alias 创建 / 编辑

```text
UI protocol radio
  -> frontend/src/types.ts AliasUpsertInput.protocol
  -> frontend/src/api.ts saveAlias()
  -> internal/app/types.go AliasUpsertInput.Protocol
  -> internal/app/manage.go UpsertAlias()
  -> internal/config.Alias.Protocol
```

### Trace protocol 展示

```text
internal/proxy/server.go handleResponses()
  -> internal/proxy.RequestTrace.Protocol = openai-responses
  -> internal/app/service.go RequestTraces()
  -> internal/app/types.go trace view mapping
  -> frontend/src/types.ts RequestTrace.protocol
  -> frontend/src/App.tsx log/network UI
```

### Sync 视图

```text
internal/app/service.go prepareSync()
  -> filter protocol-compatible aliases for current sync target
  -> internal/opencode/opencode.go build provider spec by protocol
  -> PreviewSync / ApplySync result
```

## 校验规则

### Provider

1. `protocol` 必填，默认 `openai-responses`
2. `openai-responses` provider 的 `baseURL` 必须以 `/v1` 结尾
3. 旧 provider 读入时缺 protocol，按 `openai-responses` 处理

### Alias

1. `protocol` 必填，默认 `openai-responses`
2. alias target 绑定时，provider.protocol 必须与 alias.protocol 一致
3. 自动创建 alias 时协议必须从当前绑定 provider 继承，不能继续用隐式默认值散落在多个入口

### Sync

1. 本期 sync 只处理 `openai-responses` alias
2. 若未来出现其他协议 alias，本期 doctor 与 sync 需要明确说明“该协议暂未同步到 OpenCode”
3. 但本期因为 UI/CLI 仍只有一个协议选项，正常路径下不会出现混合协议数据

## 测试设计

### 配置与应用层

新增或补强测试：

- `internal/config/config_test.go`
  - 旧配置缺 protocol 时读取默认值
  - 保存后写回 protocol
  - 协议感知 baseURL 校验
- `internal/app/service_test.go`
  - save provider round-trip 包含 protocol
  - save alias round-trip 包含 protocol
  - bind alias target 会拒绝协议不一致 provider
  - import provider 默认导入为 `openai-responses`

### OpenCode 集成层

- `internal/opencode/opencode_test.go`
  - 现有 `provider.ocswitch` 输出不变
  - 内部抽象后 validate / ensure 行为仍兼容旧测试

### Proxy / Trace

- `internal/proxy/server_test.go`
  - trace 带 `protocol=openai-responses`
  - 现有 failover / first-byte 行为不变

### 前端

至少覆盖：

- provider / alias 表单提交包含 protocol
- 日志详情显示 protocol
- target 绑定弹窗按协议过滤 provider 的派生逻辑

如果仓库当前没有前端单测基线，可先通过 build + 手工验证兜底。

## 实施顺序

### Step 1 数据模型与默认值

- 增加 protocol 常量
- config / app / frontend types 补 protocol 字段
- 补旧数据默认值辅助函数

### Step 2 服务层与校验层重构

- `ValidateProviderBaseURL(protocol, baseURL)`
- `FetchProviderModels(protocol, baseURL, apiKey, headers)`
- alias-provider 协议一致性校验
- sync 准备逻辑加入协议抽象

### Step 3 CLI 输出与输入

- provider add/list/help
- alias add/list/bind/help
- doctor / sync 文案

### Step 4 GUI 交互与显示

- provider radio group
- alias radio group
- protocol badges
- log/network protocol 展示

### Step 5 回归验证

- go test 相关包
- frontend build
- 手工走通 provider / alias / sync / serve / log 页面

## 验收标准

### 功能验收

1. 旧配置可直接加载并正常使用。
2. provider / alias 创建与编辑都能看到 protocol 单选组件。
3. provider / alias 列表与详情均能显示 protocol。
4. 日志页和网络页都能显示请求协议。
5. 现有 OpenAI Responses provider add / alias bind / serve / sync / log 功能不退化。

### 工程验收

1. 项目中不再散落“只支持 OpenAI Responses”的无抽象硬编码。
2. 协议相关默认值集中在少量中心函数中。
3. 第二期新增协议时，不需要再重改 config/app/frontend 的基础契约。

## 风险与规避

### 风险 1：字段加了但默认值散落

规避：

- 统一通过 `NormalizeProviderProtocol()` / `NormalizeAliasProtocol()` 之类中心函数收敛

### 风险 2：UI 只加展示，没有真正参与提交

规避：

- 前后端输入输出类型都要带 protocol
- 表单 submit payload 明确包含 protocol

### 风险 3：sync 抽象过度，反而影响现有行为

规避：

- 本期不改 sync 外部 contract
- 只做内部 builder 抽象，确保 `provider.ocswitch` 输出保持现状

### 风险 4：trace 只在一处加 protocol，前端没有消费

规避：

- 同时修改 `traces.go`、`app/types.go`、`frontend/types.ts`、`App.tsx`

## 本期完成后的项目状态

完成第一期后，项目应达到下面这个状态：

- 逻辑上已经是“协议感知”的本地代理
- 当前唯一启用协议仍是 `openai-responses`
- CLI 与 GUI 都已显式表达协议
- sync / model discovery / trace / validation 的关键边界已抽象出协议入口
- 第二期只需在既有抽象上新增 Anthropic Messages 分支，而不需要再清理历史硬编码
