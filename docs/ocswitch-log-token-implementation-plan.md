# ocswitch 日志页 token 获取与展示实施方案

字段参考：见 [ocswitch 日志字段说明](./ocswitch-log-field-reference.md)

## 1. 目标

这份方案聚焦一件事：

- 让 `ocswitch` 日志页能够稳定展示当前请求的 `input / output / reasoning / cache` token 数量
- 并且尽量保证这些数值来自上游协议 usage，而不是本地估算

补充目标：

- 明确区分 `unknown`、`0`、`partial`
- 为后续 `estimated cost` 展示留出数据结构

## 2. 当前问题结论

你已经明确反馈“日志页展示的 token 数量全是空的”。

结合当前实现，根因不是单点，而是三层问题叠加。

### 2.1 根因 1：当前解析器对 SSE 采用“遇到第一条 usage 就返回”

现有代码：

- `internal/proxy/server.go:585-613`
- `internal/proxy/server.go:616-641`

当前 `extractTokenUsageFromBody()` 在 SSE 场景里会：

1. 逐行找 `data:`
2. 每条 `data:` JSON 都递归调用 `findTokenUsage()`
3. 只要第一次找到 `input_tokens` 或 `output_tokens`，立刻返回

问题在于真实流式协议里，usage 往往不是一次性完整给出的：

- OpenAI Responses 常常在最后一个 `response.completed` 才给最终 usage
- 中间事件即便带 usage，也经常是 `0/0`
- Anthropic Messages 的 `message_start` 和 `message_delta` usage 也是分段更新

所以现在的实现很容易拿到：

- 第一条不完整 usage
- 或第一条 `0/0` usage

然后就提前结束。

### 2.2 根因 2：当前解析器依赖“整段缓冲后再解析”，超过 256KB 就直接放弃

现有代码：

- `internal/proxy/server.go:49`
- `internal/proxy/server.go:562-583`

当前 `outputTokenCollector` 的策略是：

- 把响应体完整缓存到内存 buffer
- 最多只保留 `256KB`
- 超过后 `truncated = true`
- 最终 `Usage()` 直接返回 `false`

这对长流非常脆弱：

- coding agent 输出长文本时，很容易超过 `256KB`
- usage 往往在 SSE 尾部
- 一旦被截断，最终 trace token 全丢

这会让日志页长期显示空。

### 2.3 根因 3：前端把 `<= 0` 统一渲染成 `-`

现有代码：

- `frontend/src/App.tsx:446-451`

```ts
function formatTokenCount(value?: number): string {
  if (value == null || value <= 0) {
    return '-'
  }
  return value.toLocaleString()
}
```

这意味着两种完全不同的情况被混在一起：

- 没有拿到 usage
- 拿到了，但当前值是 `0`

再叠加后端“提前拿到 `0/0` usage”的问题，页面就会表现成“全空”。

### 2.4 根因 4：当前 trace 结构只有 `InputTokens/OutputTokens`

现有结构：

- `internal/proxy/traces.go:53-76`
- `internal/app/types.go:105-133`
- `internal/proxy/sqlite_traces.go:61-90`

当前只存：

- `InputTokens`
- `OutputTokens`

没有：

- `ReasoningTokens`
- `CacheReadTokens`
- `CacheWriteTokens`
- `TotalTokens`
- `UsageSource`
- `UsagePrecision`

所以即使部分协议能给更细粒度 usage，当前模型也承载不了。

### 2.5 现有测试覆盖过于理想化

当前 `server_test.go` 虽然构造了带 usage 的上游响应，但没有真正覆盖这些真实失败模式：

- SSE 前置 `0/0` usage，最终尾部才有非零 usage
- Anthropic `message_start` + `message_delta` 的 usage 合并
- 响应超过 `256KB` 导致 usage 提取失败
- trace 最终存储值断言

例如：

- `internal/proxy/server_test.go:63-123`

这里只验证了请求转发和协议头，没有断言 trace token 字段。

## 3. 设计原则

这次改造建议遵守 4 个原则。

### 3.1 精确优先，不做伪精确

只要上游协议能给 usage，就以协议 usage 为准。

不要用：

- 文本长度 `/4`
- token 速率反推
- request body 长度估算

去冒充精确 token。

### 3.2 协议感知，不再用“递归找任意 `input_tokens/output_tokens`”

当前 `findTokenUsage()` 太宽松：

- 它不区分事件类型
- 不区分最终 usage 和中间 usage
- 不区分 OpenAI Responses 与 Anthropic Messages

这类弱规则不适合做日志页的准确信息来源。

### 3.3 增量解析，不依赖整段缓冲

SSE token 提取必须边转发边解析：

- 只保留 usage 解析状态
- 不保留整段 body

否则长流一定不稳。

### 3.4 unknown 要和 0 分开

日志页需要明确区分：

- `unknown`: 没拿到 / 协议不提供 / 流中断
- `0`: 明确拿到了，就是 0
- `partial`: 拿到了一部分字段

## 4. 两种协议的精确 usage 获取规则

下面是推荐的“精确获取”口径。

## 4.1 OpenAI Responses

参考：

- `../opencode/packages/opencode/src/provider/sdk/copilot/responses/openai-responses-language-model.ts:1261-1273`
- `../opencode/packages/opencode/src/provider/sdk/copilot/responses/openai-responses-language-model.ts:1357-1386`
- `../opencode/packages/console/app/src/routes/zen/util/provider/openai.ts:27-62`

### 4.1.1 非流式 JSON

优先读取：

- 顶层 `usage`
- 或 `response.usage`

需要支持字段：

- `input_tokens`
- `output_tokens`
- `total_tokens`
- `input_tokens_details.cached_tokens`
- `output_tokens_details.reasoning_tokens`

### 4.1.2 流式 SSE

只把以下事件视为“最终精确 usage 事件”：

- `event: response.completed`
- `event: response.incomplete`

从对应 `data:` JSON 中读取：

- `response.usage.input_tokens`
- `response.usage.output_tokens`
- `response.usage.input_tokens_details.cached_tokens`
- `response.usage.output_tokens_details.reasoning_tokens`

不建议把前面任何普通 delta 事件里的 usage 直接当最终值。

### 4.1.3 归一化规则

为了和 OpenCode 语义对齐，建议同时保存原始值和展示值。

原始值：

- `rawInputTokens = input_tokens`
- `rawOutputTokens = output_tokens`
- `rawTotalTokens = total_tokens`，若缺失则 `input_tokens + output_tokens`
- `cacheReadTokens = input_tokens_details.cached_tokens`
- `reasoningTokens = output_tokens_details.reasoning_tokens`
- `cacheWriteTokens = unknown`

展示值：

- `inputTokens = rawInputTokens - cacheReadTokens`
- `outputTokens = rawOutputTokens - reasoningTokens`

这里的前提是 OpenAI Responses 协议里：

- `input_tokens` 包含 cached input
- `output_tokens` 包含 reasoning output

这和 OpenCode 当前读法一致。

## 4.2 Anthropic Messages

参考：

- `../opencode/packages/opencode/test/session/llm.test.ts:809-843`
- `../opencode/packages/console/app/src/routes/zen/util/provider/anthropic.ts:145-186`

### 4.2.1 非流式 JSON

优先读取顶层 `usage`。

支持字段：

- `input_tokens`
- `output_tokens`
- `cache_read_input_tokens`
- `cache_creation_input_tokens`
- `cache_creation.ephemeral_5m_input_tokens`
- `cache_creation.ephemeral_1h_input_tokens`

### 4.2.2 流式 SSE

Anthropic 的 usage 需要“合并”，不是“第一次命中就结束”。

要支持两类事件：

- `message_start` 中的 `message.usage`
- `message_delta` 中的 `usage`

推荐规则：

1. 收到 `message_start.message.usage` 时，记录初始 usage
2. 收到 `message_delta.usage` 时，用非空字段覆盖已有 usage
3. 直到流结束，取最终合并结果

也就是说 Anthropic 流式 usage 要做“latest non-nil merge”。

### 4.2.3 归一化规则

建议保存：

- `rawInputTokens = input_tokens`
- `rawOutputTokens = output_tokens`
- `cacheReadTokens = cache_read_input_tokens`
- `cacheWriteTokens = cache_creation.ephemeral_5m_input_tokens ?? cache_creation_input_tokens`
- `cacheWrite1hTokens = cache_creation.ephemeral_1h_input_tokens`
- `reasoningTokens = unknown`

展示值建议：

- `inputTokens = rawInputTokens`
- `outputTokens = rawOutputTokens`

原因：Anthropic Messages 协议当前没有稳定、独立的 reasoning token 字段；不要硬拆。

## 4.3 为什么不能继续用当前通用递归方案

当前 `findTokenUsage()` 的问题不是“没写全字段”，而是建模方式本身不对。

它的问题包括：

1. 不区分事件类型
2. 不区分最终 usage 与中间 usage
3. 不支持 usage merge
4. 只支持 `input_tokens/output_tokens`
5. 无法表达 unknown / partial

所以推荐直接替换为“协议感知 usage parser”，而不是在 `findTokenUsage()` 上继续打补丁。

## 5. 推荐的数据结构改造

## 5.1 RequestTrace 新增 `Usage`

推荐把当前：

- `InputTokens int64`
- `OutputTokens int64`

升级为结构化 usage。

建议形态：

```go
type TraceUsage struct {
    RawInputTokens   *int64 `json:"rawInputTokens,omitempty"`
    RawOutputTokens  *int64 `json:"rawOutputTokens,omitempty"`
    RawTotalTokens   *int64 `json:"rawTotalTokens,omitempty"`

    InputTokens      *int64 `json:"inputTokens,omitempty"`
    OutputTokens     *int64 `json:"outputTokens,omitempty"`
    ReasoningTokens  *int64 `json:"reasoningTokens,omitempty"`
    CacheReadTokens  *int64 `json:"cacheReadTokens,omitempty"`
    CacheWriteTokens *int64 `json:"cacheWriteTokens,omitempty"`
    CacheWrite1hTokens *int64 `json:"cacheWrite1hTokens,omitempty"`

    Source      string   `json:"source,omitempty"`      // openai-responses-final, anthropic-message-merge, none
    Precision   string   `json:"precision,omitempty"`   // exact, partial, unavailable
    Notes       []string `json:"notes,omitempty"`
}
```

然后 `RequestTrace` 里改成：

```go
Usage TraceUsage `json:"usage,omitempty"`
```

如果想兼容旧前端，可以在过渡期继续保留：

- `InputTokens`
- `OutputTokens`

但应改为从 `Usage.InputTokens / Usage.OutputTokens` 投影出来，而不是继续当主字段。

## 5.2 为什么建议用指针字段

因为必须表达三种状态：

- `nil`: unknown
- `0`: 明确为 0
- `>0`: 明确非零

如果继续用 `int64` 默认值，会永远分不清 `0` 和 `unknown`。

## 5.3 SQLite 存储改造

当前表只有：

- `input_tokens`
- `output_tokens`

建议新增列：

- `raw_input_tokens`
- `raw_output_tokens`
- `raw_total_tokens`
- `reasoning_tokens`
- `cache_read_tokens`
- `cache_write_tokens`
- `cache_write_1h_tokens`
- `usage_source`
- `usage_precision`
- `usage_notes_json`

如果想减少迁移复杂度，也可以：

- 新增 `usage_json TEXT NOT NULL DEFAULT ''`

由后端直接序列化 `TraceUsage`。

对当前项目来说，`usage_json` 更灵活，后续字段扩展成本更低。

## 6. 后端实现方案

## 6.1 核心方向：把 `outputTokenCollector` 替换成 `usageCollector`

当前：

- `outputTokenCollector` 只保存整个 body buffer

建议改成：

- `usageCollector`
- 内部按协议维护状态
- 每收到一个 chunk 就增量解析
- 最终返回 `TraceUsage`

### 6.1.1 新接口建议

```go
type usageCollector interface {
    Add(chunk []byte)
    Finish() TraceUsage
}
```

实际工厂：

```go
func newUsageCollector(protocol string, contentType string) usageCollector
```

## 6.2 OpenAI Responses 解析器

建议实现 `openAIResponsesUsageCollector`。

职责：

- 识别 JSON / SSE
- SSE 只在 `response.completed` / `response.incomplete` 上更新最终 usage
- 记录 latest final usage
- 流结束后归一化为 `TraceUsage`

### 6.2.1 SSE 实现建议

不要再把所有 chunk 拼成完整字符串后再拆。

推荐：

1. 保留一个小型行缓冲
2. 按 `\n\n` 切出完整 SSE event frame
3. 解析其中的 `event:` 和 `data:`
4. 只处理：
   - `response.completed`
   - `response.incomplete`
5. 每次命中就覆盖 `finalUsage`
6. `Finish()` 返回 `finalUsage`

这样即使流很长，也不会因为文本本身过大而丢 usage。

## 6.3 Anthropic Messages 解析器

建议实现 `anthropicMessagesUsageCollector`。

职责：

- 识别 JSON / SSE
- SSE 支持 `message_start` + `message_delta` usage 合并
- 维护 latest merged usage
- 流结束时输出 `TraceUsage`

### 6.3.1 合并逻辑建议

对每个 usage update：

- 非空字段覆盖旧值
- `cache_creation` 子结构做深合并
- `output_tokens` 以后到值覆盖先到值

不要在第一次看到 usage 时就定稿。

## 6.4 非流式 JSON 处理

非流式 JSON 可以继续在响应结束后解析，但建议：

- 不再复用通用递归逻辑
- 按协议走专用 parser

推荐规则：

- `protocol == openai-responses` 时解析 OpenAI Responses JSON usage
- `protocol == anthropic-messages` 时解析 Anthropic JSON usage

## 6.5 对“长流截断”的处理建议

SSE 场景：

- 彻底去掉“整段 body 最多 256KB 才能解析 usage”的前提
- 只保留行级 / event 级缓冲

JSON 场景：

- 可以保留较宽松 body cap
- 但若超限，应返回：
  - `precision = unavailable`
  - `notes = ["response body exceeded JSON usage capture limit"]`

## 6.6 流中断时的行为

如果流异常结束：

- 如果已经拿到了最终 usage，就照常写入
- 如果只拿到部分 usage，就写 `precision = partial`
- 如果完全没拿到，就写 `precision = unavailable`

不要再简单地写成 `0`。

## 7. 前端日志页改造建议

## 7.1 展示层不要再把 `0` 和 `unknown` 混掉

当前 `formatTokenCount()` 需要改。

建议改成：

- `undefined/null => '-'`
- `0 => '0'`
- `>0 => 正常格式化`

## 7.2 日志卡片先显示简版 usage

列表卡片建议显示：

- `input`
- `output`
- `reasoning/cache` 只在有值时显示小 badge
- `precision` 小标签：`exact` / `partial` / `unknown`

## 7.3 详情页显示完整 usage

详情页新增 `Usage` 区块，展示：

- input
- output
- reasoning
- cache read
- cache write
- total
- source
- precision
- notes

## 7.4 命名建议

为减少误读，建议使用：

- `Input Tokens`
- `Output Tokens`
- `Reasoning Tokens`
- `Cache Read`
- `Cache Write`
- `Total Tokens`
- `Usage Source`
- `Precision`

如果是 Anthropic 且 reasoning 不可得：

- 直接显示 `-`
- tooltip 写明 `Anthropic Messages does not expose reasoning token count separately`

## 8. 推荐实施顺序

## Phase 1：修正 token 全空问题

目标：先让页面稳定出现非空 token。

范围：

1. 新增协议感知 `usageCollector`
2. OpenAI Responses SSE 只认 `response.completed` / `response.incomplete`
3. Anthropic SSE 做 usage merge
4. 前端 `0` 不再显示成 `-`
5. 增加 trace token 断言测试

这一阶段先不改 DB 结构也可以：

- 先继续投影到旧字段 `InputTokens/OutputTokens`

但建议同时把 `UsageSource/Precision` 带上，至少能排障。

## Phase 2：扩展到 reasoning/cache/total

范围：

1. RequestTrace 增加 `Usage` 结构
2. SQLite 增加 `usage_json` 或细分列
3. 前端详情页展示完整 usage 维度
4. 协议差异说明文案

## Phase 3：支持 estimated cost

前提：

- usage 结构稳定
- model 价格来源明确

做法：

- 后端基于 usage + 配置价格生成 `estimatedCost`
- 前端明确标注 `Estimated`

## 9. 测试清单

这是这次改造最关键的部分。

## 9.1 OpenAI Responses

需要新增测试：

1. `response.completed` 才有最终 usage，trace 正确写入
2. 前面出现 `usage: {input_tokens:0, output_tokens:0}`，最终仍取最后一个 completed usage
3. `cached_tokens` 被正确拆成 `cacheRead` 与归一化 `input`
4. `reasoning_tokens` 被正确拆成 `reasoning` 与归一化 `output`
5. 超长 SSE 文本不影响最终 usage 获取

## 9.2 Anthropic Messages

需要新增测试：

1. `message_start.message.usage` + `message_delta.usage` 正确合并
2. `cache_creation_input_tokens` / `cache_creation.ephemeral_5m_input_tokens` 被正确记录
3. 流中断但已有部分 usage 时，precision = partial
4. reasoning 字段保持 unknown，不伪造为 0

## 9.3 UI

需要新增测试：

1. `0` 显示为 `0`
2. `undefined` 显示为 `-`
3. `partial` / `unknown` 徽标正确显示

## 10. 建议的最小落地版本

如果你要优先修复“现在为什么全空”，我建议按这个最小切口做：

1. 替换 `extractTokenUsageFromBody()`，改成协议感知、增量式 usage parser
2. OpenAI Responses SSE 只取最终 `response.completed` usage
3. Anthropic SSE 改成 usage merge
4. 前端 `0` 不再显示 `-`
5. 增加 4 个回归测试：
   - OpenAI 前置 `0/0`
   - OpenAI 长流
   - Anthropic `message_start + message_delta`
   - trace 最终字段断言

这样做完之后，日志页的“token 全空”问题大概率就会直接消失。

## 11. 最终建议

我建议的判断是：

- 当前 token 全空，核心不是 UI，而是后端 usage 解析策略有结构性缺陷。
- 这次不要再修修补补 `findTokenUsage()` 了，直接换成协议感知 usage parser。
- 精确 token 获取应只依赖协议 usage，不要引入本地估算兜底。
- 页面上必须把 `unknown` 和 `0` 区分开，否则排障会一直混乱。

如果下一步进入实现，我建议优先做 `Phase 1`，先把“非空且可信的 input/output”修好，再扩展到 `reasoning/cache/total`。
