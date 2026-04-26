# ocswitch 日志字段说明

本文说明 `ocswitch` 当前日志页中各字段的含义。当前页面分成两种视角：

- `Log`：业务路由视角，重点看 alias 如何落到最终 provider/model，以及 token / usage
- `Network`：技术排障视角，重点看每次上游调用的 URL、状态码、请求头、响应头、响应体摘要

## 1. 通用显示规则

- 显示 `-`：表示当前字段没有值，或当前协议/场景下无法得到该值
- 显示 `0`：表示该值被明确报告为 `0`，不是 unknown
- `badge`：通常表示状态或分类，例如成功/失败、协议、usage 精度

## 2. Log 列表页字段

### 2.1 主标题区

- `Alias`
  - 本地 alias 名称
  - 如果当前请求没有命中 alias，则退回显示客户端请求里的原始模型名
- `#ID`
  - 该请求在本地 trace store 中的唯一请求 ID
- 副标题左侧：`Final route`
  - 最终实际命中的 `provider/model`
  - 如果请求失败且没有明确最终路由，可能退回显示错误信息
- 副标题右侧：`Duration`
  - 整次请求总耗时

### 2.2 状态与摘要

- `Success / Failed`
  - 整条请求最终是否成功
- `Protocol`
  - 本次请求使用的协议
  - 当前支持：`openai-responses`、`anthropic-messages`
- `Started at`
  - 代理接收到请求的开始时间
- `First byte`
  - 首字节时间（TTFB）
  - 从代理发起上游请求，到收到首个上游响应字节的耗时
- `Attempt chain`
  - 一共发生了多少次上游尝试
  - 如果发生过切换，会显示类似 `2 · Failover`
- `Output tokens`
  - 当前主展示用的输出 token 数
- `Output rate`
  - 输出速率
  - 公式：`outputTokens / durationMs * 1000`
  - 单位：`token/s`

## 3. Log 详情页字段

### 3.1 顶部摘要卡

- `Alias`
  - 本地 alias 名称
  - 如果没有 alias，则显示原始模型名
- `Final route`
  - 最终命中的上游 `provider/model`
- `Protocol`
  - 请求协议
- `Status`
  - 最终返回给客户端的 HTTP 状态码
- `Total time`
  - 整次请求总耗时

### 3.2 基础字段

- `Started at`
  - 请求开始时间
- `First byte`
  - 首字节耗时
- `Stream`
  - 本次请求是否为流式请求
- `Failover`
  - 本次请求是否发生 provider 切换
- `Input tokens`
  - 主展示用输入 token 数
- `Output tokens`
  - 主展示用输出 token 数
- `Output rate`
  - 输出速率，含义同列表页

### 3.3 Usage 字段

- `Reasoning tokens`
  - 推理 token 数
  - 对支持该字段的协议有意义
  - 对拿不到 reasoning token 的协议，不伪造为 `0`
- `Cache read tokens`
  - 从缓存命中读取的 token 数
- `Cache write tokens`
  - 写入缓存的 token 数
- `Cache write 1h tokens`
  - 写入 1 小时档缓存的 token 数
- `Raw input tokens`
  - 上游 usage 原始输入 token 数
- `Raw output tokens`
  - 上游 usage 原始输出 token 数
- `Raw total tokens`
  - 上游 usage 原始总 token 数
- `Usage source`
  - usage 数据来源
  - 当前一般等于协议来源，例如 `OpenAI Responses`、`Anthropic Messages`
- `Usage precision`
  - 当前 usage 数据的完整度/可信度
- `Usage notes`
  - usage 采集备注
  - 常用于解释为什么是 `partial` / `unavailable`，或者为什么未拿到最终 usage

### 3.4 错误字段

- 红色错误文本
  - 整条请求最终错误信息
  - 通常是上游失败、流中断、下游写失败等摘要

## 4. Usage 字段的精确语义

### 4.1 `raw*` 与主展示字段的区别

- `rawInputTokens` / `rawOutputTokens` / `rawTotalTokens`
  - 尽量保留上游 usage 原始值
- `inputTokens` / `outputTokens`
  - 用于页面主展示的归一化值

### 4.2 OpenAI Responses 的归一化规则

- `rawInputTokens = usage.input_tokens`
- `rawOutputTokens = usage.output_tokens`
- `rawTotalTokens = usage.total_tokens`；若缺失则为 `input + output`
- `cacheReadTokens = usage.input_tokens_details.cached_tokens`
- `reasoningTokens = usage.output_tokens_details.reasoning_tokens`
- `inputTokens = rawInputTokens - cacheReadTokens`
- `outputTokens = rawOutputTokens - reasoningTokens`

原因：

- `input_tokens` 包含 cached input
- `output_tokens` 包含 reasoning output

### 4.3 Anthropic Messages 的归一化规则

- `rawInputTokens = usage.input_tokens`
- `rawOutputTokens = usage.output_tokens`
- `cacheReadTokens = usage.cache_read_input_tokens`
- `cacheWriteTokens = usage.cache_creation.ephemeral_5m_input_tokens ?? usage.cache_creation_input_tokens`
- `cacheWrite1HTokens = usage.cache_creation.ephemeral_1h_input_tokens`
- `inputTokens = rawInputTokens`
- `outputTokens = rawOutputTokens`

当前不做：

- 不把 Anthropic output 再拆出 reasoning token
- 拿不到 reasoning 时，不伪造为 `0`

## 5. `Usage precision` 各取值含义

- `exact`
  - 成功拿到完整可用的 usage
- `partial`
  - 流提前中断，但中途已经拿到部分 usage
  - 此时 token 数据可用于排障，但不应视为完整账单口径
- `unavailable`
  - 未拿到可用 usage
  - 常见原因：
    - 上游没有返回 usage
    - 流结束前没有出现最终 usage 事件
    - 响应体过大导致 usage 捕获被截断
    - 响应体为空或解析失败

## 6. Attempt chain 字段

`Log` 详情页底部会按尝试链展示每次上游尝试：

- `Attempt N`
  - 第几次上游尝试
- `result`
  - 该次尝试结果，例如：
    - `success`
    - `retryable_failure`
    - `stream_error`
    - `downstream_write_error`
    - `skipped`
- `Provider`
  - 该次尝试命中的 provider ID
- `Model`
  - 该次尝试实际发给上游的模型名
- `Status`
  - 该次尝试对应的上游 HTTP 状态码
- `Total time`
  - 该次尝试自身耗时
- 红色错误文本
  - 该次尝试对应的错误摘要

## 7. Network 列表页字段

`Network` 列表不是业务语义视角，而是技术排障入口。

- `#ID`
  - 请求 ID，与 `Log` 视图中的同一条 trace 对应
- 右侧 code
  - 优先显示最终 provider
  - 如果没有最终 provider，则退回显示 alias 或原始模型名
- 副标题左侧
  - 优先显示最终 URL
  - 如果没有最终 URL，则退回显示最终路由文本
- 副标题右侧
  - 首字节耗时
- 状态 badge
  - 最终 HTTP 状态码
- `Protocol`
  - 请求协议
- `Started at`
  - 请求开始时间
- `Total time`
  - 整次请求总耗时
- `Attempt chain`
  - 发生了多少次上游尝试

## 8. Network 详情页字段

### 8.1 顶部摘要

- `URL`
  - 最终请求到的上游 URL
- `Protocol`
  - 请求协议
- `TTFB`
  - 首字节耗时
- `Total time`
  - 总耗时
- `Status code`
  - 最终 HTTP 状态码

### 8.2 全局请求快照

- `Client request headers`
  - 客户端发送到代理的请求头快照
  - 会做脱敏处理，不用于恢复完整敏感信息
- `Client request params`
  - 客户端发送到代理的请求参数快照
  - 用于排查模型名、stream 开关、协议字段、推理参数等

### 8.3 每个 Attempt 的技术字段

- `URL`
  - 该次尝试访问的具体上游地址
- `Status code`
  - 该次尝试收到的上游状态码
- `TTFB`
  - 该次尝试的首字节耗时
- `Total time`
  - 该次尝试总耗时
- `Request headers`
  - 代理发给该上游的请求头
- `Request params`
  - 代理发给该上游的请求参数
- `Response headers`
  - 上游响应头快照
- `Response body`
  - 上游响应体摘要/文本
  - 主要用于排查 4xx/5xx、协议不兼容、上游报错等问题

## 9. 最容易混淆的字段

- `Alias`
  - 本地入口名
- `Raw model`
  - 客户端请求里原始传入的模型字符串
- `Final model`
  - 代理最终真正发给上游的模型名
- `Status`
  - 整条请求的最终状态
- `Attempt status`
  - 某一次上游尝试自己的状态

## 10. 当前限制

- 当前不统计费用，也不调用 `models.dev` 价格目录
- 不同协议的 usage 粒度不同，字段完整度也不同
- `Anthropic Messages` 当前不拆 reasoning token
- `partial` / `unavailable` 是显式状态，表示采集边界，不应和 `0` 混淆
