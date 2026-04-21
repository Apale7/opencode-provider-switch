# OpenCode token / context / cost 梳理

## 1. 范围与一句话结论

基于以下材料交叉核对：

- `docs/opencode-sdk-go-research.md`
- `../opencode-sdk-go`
- `../opencode`
- 当前仓库 `ocswitch` 的 trace / 日志页实现

一句话结论：

- `opencode-sdk-go` 本身不计算 token 和 cost，它只是把 OpenCode HTTP API 返回的 `cost`、`tokens` 字段强类型化。
- OpenCode 中 `input/output/reasoning/cache` token 的核心来源是上游 provider / AI SDK 返回的 usage，再由 `Session.getUsage()` 做归一化拆分；这部分大多是“基于上游 usage 的精确换算”，不是纯估算。
- OpenCode 中“当前 context 使用率”展示的是“最近一个带 token 的 assistant message”的 usage 快照，不是重新扫描整段会话后精确重算出的全会话上下文。
- OpenCode 中“当前会话计费”是本地按模型价格表计算出的估算值，不是 provider 账单侧权威值。
- `ocswitch` 当前日志页只记录 `inputTokens/outputTokens`，且来自对响应体/SSE 的抓取解析，信息量和精度都明显弱于 OpenCode 内部语义，适合补 usage 来源、精度标签、reasoning/cache 细分和估算成本说明。

## 2. `opencode-sdk-go` 在这里的角色

`opencode-sdk-go/session.go` 里的 `AssistantMessage`、`AssistantMessageTokens`、`Cost` 只是 OpenCode API 响应字段的 Go 映射：

- `../opencode-sdk-go/session.go:363-377`
- `../opencode-sdk-go/session.go:472-519`

也就是说：

- SDK 不负责计算 `tokens`
- SDK 不负责计算 `cost`
- SDK 只是把 OpenCode 已经算好的结果读出来

因此真正要看的是 `../opencode/packages/opencode/src` 里的生产链路。

## 3. OpenCode 中 token 数量怎么来

### 3.1 主链路

主链路在这里：

- `../opencode/packages/opencode/src/session/processor.ts:357-376`
- `../opencode/packages/opencode/src/session/session.ts:262-325`

在 `finish-step` 事件里，OpenCode 会：

1. 取上游返回的 `value.usage`
2. 调 `Session.getUsage({ model, usage, metadata })`
3. 得到归一化后的 `usage.tokens` 与 `usage.cost`
4. 把 `usage.cost` 累加到 `assistantMessage.cost`
5. 把 `usage.tokens` 覆盖写到 `assistantMessage.tokens`
6. 再写一个 `step-finish` part，里面也带本 step 的 `tokens/cost`

注意这里有一个重要语义差异：

- `assistantMessage.cost` 是累计值：`ctx.assistantMessage.cost += usage.cost`
- `assistantMessage.tokens` 是覆盖值：`ctx.assistantMessage.tokens = usage.tokens`

这意味着：

- 一条 assistant message 如果经历多 step，`cost` 是多 step 累加
- 但 `tokens` 更像“最近一次 step 的 usage 快照”

这点和“整条消息累计 token”不是一回事。

### 3.2 `input / output / reasoning / cache` 的具体计算

核心逻辑在 `Session.getUsage()`：

- `../opencode/packages/opencode/src/session/session.ts:262-325`

字段来源如下。

#### `input`

来源：

- `usage.inputTokens`

然后会扣掉 cache token：

- `cacheReadTokens = usage.inputTokenDetails?.cacheReadTokens ?? usage.cachedInputTokens ?? 0`
- `cacheWriteTokens = usage.inputTokenDetails?.cacheWriteTokens ?? metadata fallback ?? 0`
- `adjustedInputTokens = inputTokens - cacheReadTokens - cacheWriteTokens`

所以最终展示/存储的 `tokens.input` 不是原始 `inputTokens`，而是“非缓存 input”。

#### `output`

来源：

- `usage.outputTokens`

然后减去 reasoning：

- `reasoningTokens = usage.outputTokenDetails?.reasoningTokens ?? usage.reasoningTokens ?? 0`
- `tokens.output = outputTokens - reasoningTokens`

所以 `output` 表示“非 reasoning 的输出 token”。

#### `reasoning`

来源：

- `usage.outputTokenDetails?.reasoningTokens`
- 或 `usage.reasoningTokens`

这是单独拆出来的 reasoning token。

#### `cache.read`

来源：

- `usage.inputTokenDetails?.cacheReadTokens`
- 或 `usage.cachedInputTokens`

#### `cache.write`

优先从标准 usage 字段取：

- `usage.inputTokenDetails?.cacheWriteTokens`

取不到时，会回退到 provider metadata：

- `metadata.anthropic.cacheCreationInputTokens`
- `metadata.vertex.cacheCreationInputTokens`
- `metadata.bedrock.usage.cacheWriteInputTokens`
- `metadata.venice.usage.cacheCreationInputTokens`

对应位置：

- `../opencode/packages/opencode/src/session/session.ts:271-287`

### 3.3 `total` 是怎么来的

`Session.getUsage()` 内部保留了：

- `total = usage.totalTokens`

但 App 侧“当前 context”并不直接读这个字段，而是自己把以下值重新相加：

- `input + output + reasoning + cache.read + cache.write`

位置：

- `../opencode/packages/app/src/components/session/session-context-metrics.ts:37-39`

由于前面的拆分方式满足：

- `input + cache.read + cache.write = 原始 inputTokens`
- `output + reasoning = 原始 outputTokens`

所以这个和通常意义上的 `totalTokens` 基本一致。

### 3.4 “当前 context 使用率”怎么来的

App 侧逻辑在：

- `../opencode/packages/app/src/components/session/session-context-metrics.ts:41-81`

计算方式：

1. 从当前会话消息列表里倒序找“最近一个 tokenTotal > 0 的 assistant message”
2. 取它的 `tokens`
3. 查该消息对应 model 的 `limit.context`
4. 用 `Math.round((total / limit) * 100)` 算 usage 百分比

这说明 OpenCode UI 里的“当前 context”并不是：

- 对整段消息重新 tokenization 后得出的绝对精确值

而是：

- 基于最近一次 assistant usage 快照的展示值

换句话说，它更接近“最近一次模型调用看到的上下文规模”，不是“前端当前重新精算的整个会话上下文”。

### 3.5 Context Breakdown 是估算值，不是精确 tokenizer 结果

App 的 context breakdown 在这里：

- `../opencode/packages/app/src/components/session/session-context-breakdown.ts:12-132`
- `../opencode/packages/app/src/components/session/session-context-tab.tsx:175-188`

它的做法是：

- 先按字符数估算：`Math.ceil(chars / 4)`
- 把 system/user/assistant/tool 的字符量粗分为估算 token
- 若总和和真实 `input` 对不上，再按比例缩放到 `input`
- 剩余差额记到 `other`

这部分明确是估算，不是精确 tokenizer 结果。

### 3.6 精度判断

可以把 token 相关信息拆成三层精度看。

#### A. `assistant.tokens.input/output/reasoning/cache`

结论：大多是“基于上游 usage 的精确换算”。

原因：

- 源头是 provider / AI SDK usage
- OpenCode只做拆分和归一化
- 没有用字符数或 tokenizer 重新猜

但仍有两个限制：

- 某些 provider 的 `cache.write` 依赖 metadata fallback，不是统一字段
- usage 缺失时会被 `safe()` 处理成 `0`，`0` 不一定代表真实为零，也可能代表“没拿到”

#### B. “当前 context usage % / total tokens”

结论：数值本身来自真实 usage 快照，算术是精确的；但语义上是“最近一次 step 快照”，不是“全会话实时精算”。

#### C. Context Breakdown 图

结论：估算值。

原因：字符数 `/4` 再缩放。

## 4. OpenCode 中当前会话计费怎么计算

### 4.1 单次 step cost 的公式

公式在：

- `../opencode/packages/opencode/src/session/session.ts:307-323`

本质是：

```text
cost =
  input * input_price / 1_000_000
  + output * output_price / 1_000_000
  + cache.read * cache_read_price / 1_000_000
  + cache.write * cache_write_price / 1_000_000
  + reasoning * output_price / 1_000_000
```

补充两点：

- 使用 `Decimal` 做金额计算，再转 `number`
- reasoning 目前按 output 单价计费，这是一个明确的临时策略

源码里的 TODO 写得很直白：

- `../opencode/packages/opencode/src/session/session.ts:318-320`
- 注释含义是“models.dev 价格模型还不够好，reasoning 暂时按 output 价格收费”

因此 reasoning 成本不是 provider 账单口径的精确复现，而是本地估算规则。

### 4.2 定价表从哪里来

模型价格主要来自 `models.dev`：

- `../opencode/packages/opencode/src/provider/models.ts:30-43`
- `../opencode/packages/opencode/src/provider/models.ts:122-180`
- `../opencode/packages/opencode/src/provider/provider.ts:949-969`
- `../opencode/packages/opencode/src/provider/provider.ts:971-1034`

OpenCode 会把 `models.dev` 里的：

- `cost.input`
- `cost.output`
- `cost.cache_read`
- `cost.cache_write`
- `cost.context_over_200k`

映射成内部 `model.cost`。

补充说明：

- 默认来源是 `models.dev`，具体实现见 `../opencode/packages/opencode/src/provider/models.ts`
- 默认 URL 是 `https://models.dev`，拉取的是 `${url()}/api.json`
- 该来源可被 `Flag.OPENCODE_MODELS_URL` 覆盖，因此更准确的说法是“默认来源是 `models.dev`，但实现允许替换”
- 当前找到的 OpenCode 代码里，没有看到按 `serviceTier` / `fast` / `priority` 切换另一套 `model.cost` 的逻辑
- `serviceTier` 更像模型 metadata / request option，不会自动改写这里的静态单价表

对应证据：

- `../opencode/packages/opencode/src/provider/models.ts:110-149`
- `../opencode/packages/opencode/src/provider/provider.ts:949-969`
- `../opencode/packages/opencode/src/session/session.ts:307-323`

这意味着：

- 当用户没有在 `opencode.jsonc` 手动写 `cost` 时，OpenCode 仍可依赖默认模型目录价格完成本地估算
- 但这个估算依旧是“本地价格表估算”，不是 provider 返回的 billed amount
- 如果 provider 存在 tier 化、区域化、模式化加价，而默认模型目录没有完整表达，估算就会偏离实际账单

### 4.2.1 OpenAI 官方 pricing tiers 与 GPT-5.4

OpenAI 官方开发者价格页：

- `https://developers.openai.com/api/docs/pricing?latest-pricing=standard`

截至本次调研，`gpt-5.4` 明确存在 4 种 pricing tier：

- `Standard`
- `Batch`
- `Flex`
- `Priority`

其中 `gpt-5.4` 官方单价如下（单位：`USD / 1M tokens`）。

`Standard`

- short context：`input 2.50` / `cached input 0.25` / `output 15.00`
- long context：`input 5.00` / `cached input 0.50` / `output 22.50`

`Batch`

- short context：`input 1.25` / `cached input 0.13` / `output 7.50`
- long context：`input 2.50` / `cached input 0.25` / `output 11.25`

`Flex`

- short context：`input 1.25` / `cached input 0.13` / `output 7.50`
- long context：`input 2.50` / `cached input 0.25` / `output 11.25`

`Priority`

- `input 5.00` / `cached input 0.50` / `output 30.00`

可见：

- `Priority` 对 `gpt-5.4` 不是“仅 output 1.5x”，而是相对 `Standard short context` 的 `input / cached input / output` 全部 `2x`
- `gpt-5.4` 官方价格还带有 `short context / long context` 两档，不是单一 input/output 单价
- `Regional processing (data residency)` 对 `gpt-5.4`、`gpt-5.4-mini`、`gpt-5.4-nano`、`gpt-5.4-pro` 还有额外 `10% uplift`

这对代理层估算的直接含义是：

- 不能再把某些模型简单视为“一个固定 input/output 价”
- 至少要考虑 `tier` 和 `context band` 这两个维度
- 若未来实现 GPT-5.4 的特殊计费逻辑，建议抽象成可扩展 pricing modifier，而不是写死一个“fast = x2”分支

如果模型带 `experimentalOver200K`，且：

- `tokens.input + tokens.cache.read > 200_000`

则改用 over-200k 档位价格：

- `../opencode/packages/opencode/src/session/session.ts:307-310`

### 4.3 assistant message 的 cost 是怎么累计的

在每个 `finish-step`：

- `ctx.assistantMessage.cost += usage.cost`

位置：

- `../opencode/packages/opencode/src/session/processor.ts:357-376`

所以一条 assistant message 如果中间发生多 step / tool loop：

- `message.cost` 是这些 step cost 的累计和

### 4.4 “当前会话总成本”怎么计算

App 侧逻辑：

- `../opencode/packages/app/src/components/session/session-context-metrics.ts:50-53`

直接把当前会话中所有 assistant message 的 `cost` 累加：

```text
totalCost = sum(assistant_message.cost)
```

CLI stats 也是同一路思路：

- `../opencode/packages/opencode/src/cli/cmd/stats.ts:194-207`
- `../opencode/packages/opencode/src/cli/cmd/stats.ts:255-280`

ACP agent 侧也一样：

- `../opencode/packages/opencode/src/acp/agent.ts:114-123`

### 4.5 计费精度判断

结论：当前会话 cost 是估算值，不是 provider 账单权威值。

原因有四个。

1. 单价来自本地价格表，不是每次请求后 provider 返回的真实 billed amount。
2. reasoning token 暂按 output 单价计算，是 OpenCode 本地约定，不是通用行业真值。
3. 价格表来自 `models.dev` / 配置映射，可能滞后、缺失或和供应商实际结算不完全一致。
4. 如果上游 usage 字段本身缺失或被不同 SDK/provider 归一方式影响，cost 也会跟着偏差。

所以更准确的说法应该是：

- token：多数是“provider usage 驱动的归一化计数”
- cost：多数是“基于 provider usage + 本地价格表的估算值”

## 5. 和 `ocswitch` 当前日志页现状对比

### 5.1 `ocswitch` 当前记录了什么

当前 trace 结构只有：

- `InputTokens`
- `OutputTokens`

位置：

- `internal/proxy/traces.go:53-76`
- `internal/app/types.go:105-133`

这些 token 是代理在转发响应时，从响应体 / SSE 中尝试抓 `usage`：

- `internal/proxy/server.go:540-544`
- `internal/proxy/server.go:578-613`
- `internal/proxy/server.go:616-639`

当前规则本质上是：

- 只要 JSON 或 SSE 某处出现 `input_tokens` / `output_tokens`
- 就递归提取
- 没有就记不到

它还受几个限制：

- 只抓到 `input_tokens` / `output_tokens`
- 没有 `reasoning` / `cache.read` / `cache.write`
- 没有 cost
- 响应体抓取有上限，截断后 `Usage()` 直接返回 false
- 对“0”和“未获取到”没有精度区分

### 5.2 `ocswitch` 日志页当前展示了什么

前端现在展示：

- 输入 token
- 输出 token
- 输出速率
- failover 次数
- route / status / latency

位置：

- `frontend/src/App.tsx:453-458`
- `frontend/src/App.tsx:2025-2066`
- `frontend/src/App.tsx:3037-3090`

这对代理链路诊断已经够用，但如果想承接“OpenCode token/cost 认知”，语义还不够完整。

## 6. 对 `ocswitch` 日志页面的优化建议

以下建议按“收益高且贴合现有结构”排序。

### 6.1 把“token”分成 `reported`、`derived`、`missing` 三种状态

建议在 trace 上新增类似字段：

- `usageSource`: `provider_reported | response_parsed | derived | missing`
- `usagePrecision`: `exactish | estimated | unknown`

原因：

- 当前页面看到 `-` 或 `0`，用户无法知道是“真实没有”还是“代理没抓到”
- OpenCode 自身也存在“usage 缺失回零”的语义风险
- 这类状态比单纯显示数字更重要

落到 UI 上可以是：

- token 数字旁加小 badge
- 详情页加一句说明：`usage extracted from response body` / `usage unavailable`

### 6.2 新增 `reasoning/cache/total` 明细，不再只显示 input/output

建议把 trace usage 结构扩成：

- `input`
- `output`
- `reasoning`
- `cacheRead`
- `cacheWrite`
- `total`

原因：

- OpenCode 内部已经把这几个维度视为一等公民
- 只看 input/output，用户会误读 reasoning-heavy 模型的真实开销
- cache 命中对“为什么 cost 低 / 为什么 context 高”非常关键

如果短期后端做不到全量获取，也建议前端先为这些字段预留位置，并明确显示：

- `—` = unknown
- `0` = known zero

### 6.3 明确区分“最近一次请求 usage”与“会话总成本”

建议不要直接把 OpenCode 的“current context / total cost”概念原样搬到 `ocswitch` 日志页。

更稳妥的命名应该是：

- `本次请求 Usage`
- `本次请求估算成本`
- `该 alias / provider 最近 N 次请求累计估算成本`

原因：

- OpenCode 的 current context 来自最近一次 assistant step usage 快照
- `ocswitch` 看到的是代理层请求，不是 OpenCode 内部 session 语义
- 两者粒度不同，直接混用会误导用户

### 6.4 为成本增加“估算值”标签，并显示公式来源

如果后续在 `ocswitch` 中增加 cost 展示，建议默认文案就是：

- `Estimated Cost`
- `Based on configured/model pricing, not provider billing`

并支持展开查看：

- input 单价
- output 单价
- cache read/write 单价
- reasoning 是否按 output 计价
- 价格来源：`models.dev` / 本地 provider 配置 / alias 覆盖

原因：

- OpenCode 的 cost 本质就是估算值
- 代理层如果继续沿这个思路，更应该避免给出“精确账单”的错误暗示

### 6.5 在 failover 视图中增加“每次 attempt 的 usage/cost”

当前日志页的 chain 只展示：

- provider/model
- status
- duration

建议补：

- attempt input/output/reasoning/cache
- attempt estimated cost
- attempt usage availability

原因：

- failover 最大的问题通常不是“总共花了多少”，而是“哪次尝试烧掉了多少上下文/成本”
- 同一个 alias 下多次切换时，用户更需要看单 attempt 消耗

### 6.6 在详情页增加“精度说明”区块

建议在日志详情加一个固定说明块，明确告知：

- token 是否来自 provider usage
- 是否只抓到 input/output
- 是否缺失 reasoning/cache
- cost 是否为估算
- 是否因为 body 截断而无法解析 usage

这类说明对排查“为什么前端看到的 token 和 OpenCode/上游控制台不一致”非常重要。

### 6.7 如果要对齐 OpenCode 认知，优先补后端数据结构而不是只改前端

建议优先级：

1. trace 数据结构升级
2. usage 解析升级
3. 再改日志页展示

因为当前 `ocswitch` 后端只持有：

- `InputTokens`
- `OutputTokens`

如果数据模型不扩展，前端再怎么做也只能显示不完整信息。

### 6.8 推荐一个最小可落地方案

如果希望先做一版最小改造，建议顺序如下：

1. `RequestTrace` 新增 `ReasoningTokens`、`CacheReadTokens`、`CacheWriteTokens`、`TotalTokens`
2. 新增 `UsageCaptured bool` 或 `UsageSource string`
3. 日志详情页增加 `Usage` 小节，展示 `input/output/reasoning/cache/total`
4. 日志详情页增加 `Estimated Cost` 小节，但默认带 `估算` 标识
5. attempt 卡片补 token/cost 概览

这样既能对齐 OpenCode 的语义，又不会把代理层页面做成伪“精确账单系统”。

## 7. 最终回答

### 7.1 问题 1：OpenCode 中当前 context 使用、input、output、cache token 怎么获取/计算，估算还是精确？

- `input/output/reasoning/cache` 的基础数据来自 provider / AI SDK usage。
- OpenCode 通过 `Session.getUsage()` 做标准化拆分：
  - `input = inputTokens - cacheRead - cacheWrite`
  - `output = outputTokens - reasoning`
  - `reasoning = reasoningTokens`
  - `cache.read/cache.write` 来自 usage 或 provider metadata fallback
- 这部分大多不是估算，而是“基于上游 usage 的归一化计算”。
- “当前 context 使用率”来自最近一个带 token 的 assistant message，用其 `total / model.limit.context` 计算。
- 这不是前端重算全会话上下文的精确值，而是“最近一次 step usage 快照”的展示。
- context breakdown 图是字符数 `/4` 再缩放出来的估算值。

### 7.2 问题 2：OpenCode 中当前会话计费怎么计算，估算还是精确？

- 每个 step 的 cost 由 OpenCode 本地按价格表计算：input/output/cache/read/write/reasoning 分别乘单价后求和。
- 单价主要来自 `models.dev`，不是 provider 返回的账单金额。
- 当前找到的默认实现没有按 `serviceTier` / `Priority` / `fast` 自动切换另一套价格表。
- reasoning 目前临时按 output 单价计费。
- assistant message 的 cost 是多 step 累加。
- 当前会话 cost 是所有 assistant message 的 cost 求和。
- 因此它是估算值，不是 provider 账单权威值。

### 7.4 问题 4：OpenCode / OpenAI 的模型单价来源、tier 与当前 `ocswitch` 的差异是什么？

- OpenCode 默认单价来源主要是 `models.dev`，允许通过配置覆盖来源，但不是直接读取 provider 账单。
- OpenAI 官方对 `gpt-5.4` 已明确区分 `Standard / Batch / Flex / Priority`，且还存在 `short context / long context` 两档。
- 对 `gpt-5.4` 来说，官方 `Priority` 是相对 `Standard short context` 的全项 `2x`：`input 5.00`、`cached input 0.50`、`output 30.00`。
- 因此，如果某个供应商账单表现为“input / cache 约 2x，但 output 不是 2x”，不能直接视为 OpenAI 官方 `Priority` 原样透传，更可能是供应商二次定价、context 档位差异或多维规则叠加。
- 当前 `ocswitch` 的 estimated cost 只读取 OpenCode 配置中的静态 `cost` 字段，本身还没有 `tier` / `context band` / `regional uplift` 感知，因此和真实账单不一致是预期内现象。

### 7.3 问题 3：对 `ocswitch` 日志页的优化建议

- 增加 usage 来源/精度标记，避免把“未抓到”误读成“为 0”。
- 补 `reasoning/cache/total`，不要只展示 `input/output`。
- 若增加 cost，必须明确标注为“估算值”。
- failover 详情里展示每次 attempt 的 usage/cost。
- 不要直接把 OpenCode 的“current context”概念原样搬到代理层页面，建议改成“本次请求 usage / 估算成本”。
