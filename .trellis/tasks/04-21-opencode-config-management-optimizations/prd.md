# OpenCode Config Management Optimizations

## 概要

本任务面向 `ocswitch` 在读取、同步、诊断与管理 OpenCode 配置时的下一阶段优化。

输入依据有两部分：

- `docs/opencode-sdk-go-research.md` 的调研结论
- `../opencode-sdk-go` 仓库暴露的 `Config.Get`、`App.Providers`、`RequestOption`、typed error、`RawJSON` / `ExtraFields` 等能力

核心判断：

- 当前项目写侧已经较稳，不应优先重写
- 最有价值优化点在读侧、对账、诊断、观测，不在 proxy 数据面
- 第一阶段应先做“低风险高信息量”的只读增强，再考虑更深接入

一句话目标：

**把 `ocswitch` 从“能把配置同步进 OpenCode”升级为“能解释 OpenCode 配置现在是什么、为什么这样、哪里漂移了”的工具。**

## 背景

当前项目已经具备：

- 本地 `ocswitch` 配置读写
- OpenCode 配置文件同步
- `provider.ocswitch` / `provider.ocswitch-anthropic` 合同维护
- alias / provider / target 路由建模
- doctor 基础校验
- trace 基础记录
- GUI / CLI 两套操作入口

当前实现中，几个关键文件的角色已经较清晰：

- `internal/config/config.go`：本地 `ocswitch` 配置真源
- `internal/opencode/opencode.go`：OpenCode 目标文件读写与 sync 合同维护
- `internal/app/service.go`：doctor、sync、GUI orchestration
- `internal/opencode/provider_models.go`：provider 模型发现网络调用
- `internal/proxy/traces.go`：请求 trace

其中写侧已有明显优势：

- `internal/opencode/opencode.go` 支持 JSON / JSONC 读取
- 写回时只 patch `provider.ocswitch*`，不粗暴重写整个文档
- 会保留未知字段
- 会保留同名 model object 的已有 metadata

但读侧与管理侧仍有明显短板：

- 主要停留在“目标文件视角”，缺少“OpenCode 运行时视角”
- `doctor` 以合同校验为主，对 runtime drift 可解释性不足
- `provider_models` 仍是一次性手写 HTTP 逻辑，缺少统一 option / middleware / retry 抽象
- trace 与模型元数据保真度不足，难以支撑更强 GUI 诊断

## 核心问题

当前用户在以下场景中容易遇到“同步成功但不知是否真正生效”的问题：

1. `ocswitch opencode sync` 已写入目标文件，但 OpenCode 运行时未加载或未暴露对应 provider。
2. 目标文件里的 `provider.ocswitch*` 与 OpenCode 运行时 `App.Providers` 不一致。
3. `model` / `small_model` 指向已不可路由 alias，但只有弱提示。
4. 模型发现失败时，只能看到字符串错误，难定位是超时、认证、上游状态码还是响应结构问题。
5. GUI 能看到配置和 trace，但看不到“文件态”和“运行时态”之间的差异。

本任务要解决的不是“如何替换现有 sync 逻辑”，而是“如何让配置读取和管理更可解释、更可验证”。

## 产品目标

本期优化完成后，项目应逐步具备以下能力：

1. 同时理解 OpenCode 配置文件状态与 OpenCode 运行时状态。
2. 在 `doctor` 与 GUI 中输出结构化对账结果，而不只是字符串错误。
3. 将 provider model discovery、health probe、未来诊断探测统一到同一套 transport option 风格。
4. 在内部 view / trace 中保留更多 provider / model / raw metadata，提升调试价值。
5. 明确区分“文件目标路径”和“OpenCode 运行目录 / 作用域”，为 project-scoped config 留出空间。

## 非目标

本任务明确不做：

1. 用 `opencode-sdk-go` 直接替换 `internal/proxy/server.go`。
2. 用 SDK 重写 `internal/opencode/opencode.go` 的写侧 patch 逻辑。
3. 把 SDK 类型直接渗透到仓库所有层。
4. 第一阶段就接入完整事件流、GUI 实时事件面板。
5. 把项目拉成通用 AI gateway 或平台型控制面。

## 设计原则

### 1. 写侧继续稳定，读侧先增强

当前写侧的关键价值是：

- 对 OpenCode 配置文件侵入小
- round-trip 保真较好
- 与本地 sync 合同边界清晰

因此后续优化应坚持：

- 文件写回仍由 `internal/opencode/opencode.go` 主导
- SDK 优先用于读取、对账、诊断、观测

### 2. 双轨真相，不混成一层

后续设计中要明确区分两个真相来源：

1. 文件真相：目标 OpenCode 配置文件当前长什么样
2. 运行时真相：OpenCode 当前实际加载 / 暴露了什么

二者都重要，但不能互相替代。

### 3. 强类型在边界收敛，不向全仓扩散

SDK 提供强类型 `Config`、`Provider`、`Model` 很有价值，但项目内部仍应保留自己的 adapter 层。

建议做法：

- SDK 类型只停留在 `internal/opencode` 附近
- `internal/app` 消费项目自定义 snapshot / diff 结构
- frontend 继续消费 `internal/app/types.go` 暴露的 view model

### 4. 先做只读诊断，再做更深优化

第一阶段最应该交付的是：

- 更准确读
- 更清晰报
- 更容易定位 drift

而不是更激进地“改写”现有能力。

## 跨层数据流

本任务涉及的目标数据流如下：

`ocswitch config` -> `prepareSync / doctor` -> `OpenCode target file` -> `OpenCode runtime API` -> `reconciliation report` -> `CLI / GUI`

边界拆分：

1. `config.Config` 边界
   - 输入：本地 provider / alias / target / server 设置
   - 输出：可路由 alias 集、预期 sync 合同

2. `opencode.Raw` 文件边界
   - 输入：目标文件 JSON / JSONC
   - 输出：文件级 provider / model / option / 默认模型信息

3. SDK runtime 边界
   - 输入：OpenCode base URL、directory
   - 输出：`Config.Get`、`App.Providers` 的运行时信息

4. service 对账边界
   - 输入：本地预期状态 + 文件状态 + runtime 状态
   - 输出：结构化 drift / warning / error

5. presentation 边界
   - 输入：结构化诊断结果
   - 输出：CLI doctor 文本、GUI 诊断摘要、sync 结果补充信息

验证职责建议：

- 文件解析错误：`internal/opencode`
- runtime API / transport 错误：SDK adapter
- drift 归类与严重度：`internal/app/service.go`
- 展示文案与 badge：`internal/app/types.go` + frontend

## 优化方向

### 方向一：新增 OpenCode 读侧 adapter

目标：把 OpenCode “文件态”和“运行时态”都读出来。

建议新增一层只读 adapter，职责包括：

- 读取目标 OpenCode 配置文件
- 通过 SDK 读取 `Config.Get`
- 通过 SDK 读取 `App.Providers`
- 将两者归一成项目内部 snapshot 结构

建议产物：

- `FileConfigSnapshot`
- `RuntimeConfigSnapshot`
- `OpenCodeReadResult`

最小要求：

- runtime 不可达时，文件读仍可成功
- SDK 读失败时要给出分层错误
- 不影响现有 sync 写侧路径

### 方向二：把 doctor 升级为“对账型 doctor”

目标：让 `doctor` 不只回答“合同是否满足”，还回答“现在到底哪里不一致”。

应新增的诊断类别：

- `runtime_unreachable`
- `file_parse_error`
- `sync_contract_mismatch`
- `runtime_provider_missing`
- `runtime_provider_protocol_mismatch`
- `default_model_invalid`
- `small_model_invalid`
- `catalog_drift`

每类诊断至少应包含：

- 严重度
- 分类 code
- 人类可读 message
- 相关 provider / alias / path / protocol
- 是否可自动修复或建议下一步动作

### 方向三：明确“目标文件路径”和“运行目录”两个概念

当前 sync 更偏向“给某个目标文件写内容”。

后续读侧增强后，需要显式区分：

- `targetPath`：写入或读取的 OpenCode 配置文件路径
- `directory`：SDK 调用时请求 OpenCode 当前工作目录上下文

这样才能支持：

- 用户同步到某个路径
- 再查询某个 directory 下 OpenCode 运行时实际加载结果
- 后续兼容 project-scoped OpenCode config

### 方向四：将 provider model discovery 改为可组合 transport

`internal/opencode/provider_models.go` 当前问题：

- timeout 固定
- retry 缺失
- middleware 缺失
- raw response / headers 不保留
- 错误分类弱

建议参考 SDK 的 option 风格，收敛一套项目内 transport 选项：

- `BaseURL`
- `Headers`
- `HTTPClient`
- `Middleware`
- `RequestTimeout`
- `MaxRetries`
- `ResponseInto` / raw response capture

第一阶段不要求做成完整 SDK clone，但要做到：

- `FetchProviderModels()` 不再是孤立特例
- 后续 provider probe / doctor runtime 检查可复用同一 transport

### 方向五：提高 metadata 与 trace 保真度

当前项目虽然已在 OpenCode sync 写侧保留同名 model metadata，但业务展示层仍看不到足够多的信息。

建议增强保真字段：

- `ProviderID`
- `ModelID`
- 原始错误 body
- response headers
- retryable 标记
- 原始 JSON 片段
- 未识别字段 key 列表

应用位置：

- `internal/app/types.go`
- `internal/proxy/traces.go`
- doctor 输出
- GUI provider / trace / sync 详情

### 方向六：默认模型管理从“校验存在”升级到“诊断与建议”

当前 `prepareSync()` 已对 `--set-model`、`--set-small-model` 做 alias 可路由校验。

后续建议补充：

- 当前 OpenCode 文件中的 `model` / `small_model` 是否仍指向 routable alias
- runtime `model` / `small_model` 是否已与文件一致
- 若未设置，给出推荐 alias 候选
- 若协议错位或 alias 缺失，给出精确诊断

## 分阶段落地

### Phase 1：只读对账 PoC

目标：最小风险验证 SDK 接入边界。

交付建议：

1. 新增只读 OpenCode adapter。
2. 基于 adapter 扩展 `doctor`。
3. 生成首版文件态 / 运行时态 diff。

推荐 PoC 名称：

- `doctor --opencode-runtime`

PoC 需要回答：

- 哪些 alias 本地可路由但未同步到目标文件
- 哪些 `provider.ocswitch*` 文件合同异常
- OpenCode runtime 是否暴露了预期 provider
- `model` / `small_model` 是否指向有效 alias

验收标准：

- OpenCode runtime 不可达时，doctor 仍能完成文件态诊断
- 有 drift 时输出结构化问题，而不是只有一条泛化错误
- 不修改现有 sync 写侧行为

### Phase 2：transport 统一抽象

目标：让 provider model discovery 与后续 probe / doctor runtime 检查共享一套网络层。

交付建议：

1. 重构 `internal/opencode/provider_models.go`。
2. 抽可组合 request option 风格 transport。
3. 引入基础 retry / timeout / middleware / raw response capture。

验收标准：

- 模型发现支持 request timeout 与 retry 配置
- 状态码、认证失败、解码失败可区分
- 后续 health probe 不需要重复写一套 HTTP 包装

### Phase 3：metadata / trace / GUI 诊断增强

目标：把新增读侧与对账能力体现在实际用户界面上。

交付建议：

1. 扩 `internal/app/types.go` 的诊断 view model。
2. 在 GUI 中展示关键 drift、provider 运行时状态、默认模型状态。
3. 提高 trace 详情的信息量。

验收标准：

- GUI 能区分“文件已写入”和“运行时已生效”
- trace 详情能展示更高保真错误与 provider/model 信息
- 用户能从界面直接判断下一步该做什么

### Phase 4：事件流观测（可选）

目标：若前面几阶段验证顺利，再评估是否接入 SDK 事件流做更实时诊断。

注意：

- 不是当前任务第一优先级
- 必须建立在 snapshot / doctor / transport 已稳的前提上

## 建议的数据结构

以下是建议的内部抽象，不要求与最终代码命名完全一致，但边界应类似：

### FileConfigSnapshot

关注内容：

- 目标路径
- 是否存在
- provider.ocswitch / provider.ocswitch-anthropic 合同摘要
- `model` / `small_model`
- alias 集合与 model entry key 集合

### RuntimeConfigSnapshot

关注内容：

- OpenCode base URL
- directory
- runtime config 中 `model` / `small_model`
- runtime provider 列表
- provider -> model catalog 摘要
- raw / extra metadata 可选保留

### ReconciliationReport

关注内容：

- `issues`
- `warnings`
- `driftSummary`
- `availableAliases`
- `missingProviders`
- `invalidDefaultModels`
- `catalogMismatches`

## 对现有文件的影响评估

### `internal/opencode/opencode.go`

继续承担：

- 目标文件读写
- sync 合同 patch

新增或演化方向：

- 只读 snapshot 提取辅助函数
- provider / model / default model 摘要读取

### `internal/opencode/provider_models.go`

继续承担：

- provider model discovery

新增或演化方向：

- 引入更统一 transport option
- 错误分层与 raw response capture

### `internal/app/service.go`

继续承担：

- doctor / preview sync / apply sync orchestration

新增或演化方向：

- file + runtime + expected state 对账
- 结构化诊断聚合

### `internal/app/types.go`

新增或演化方向：

- 承载更结构化 doctor / sync / runtime 诊断 view
- 保留 provider/model 级辅助字段

### `internal/proxy/traces.go`

新增或演化方向：

- 保留更高保真错误、headers、retryable 信息
- 与 doctor / provider 健康视图共享一部分概念

### frontend

新增或演化方向：

- 展示 sync 后文件态 / 运行时态摘要
- 展示 drift badge / warning / fix suggestion
- 让 provider 与 trace 页面更像诊断面板，而不只是配置面板

## 风险与约束

1. OpenCode runtime API 依赖本地实例可达，不能让 runtime 不可达拖垮基础 sync / doctor 能力。
2. 如果 SDK 的类型模型后续变化较快，不应把它直接扩散成全仓稳定契约。
3. 项目当前已有较稳写侧，任何读侧增强都不应破坏现有 round-trip 保真。
4. transport 抽象要避免一次性泛化过度，先服务 `provider_models` 和 `doctor` 即可。

## 推荐推进顺序

1. 新增只读 OpenCode adapter 与 snapshot 抽象。
2. 扩 `doctor`，输出 file/runtime reconciliation。
3. 抽 `provider_models` 的 transport option 层。
4. 扩 `internal/app/types.go` 与 GUI 展示。
5. 最后再评估事件流与更深观测。

## 最终建议

如果只做一个最值得做的 PoC，应优先做：

**基于 SDK 的 OpenCode 运行时对账 doctor。**

原因：

- 风险最低
- 与 SDK 定位最匹配
- 能直接补上当前项目“写入成功但是否真正生效”这块认知缺口

本任务后续实现阶段应坚持的边界是：

- 写侧不动核心路径
- 先补读侧真相
- 再把诊断能力变成用户看得懂、能采取行动的信息
