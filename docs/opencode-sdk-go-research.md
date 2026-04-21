# opencode-sdk-go SDK 源码调研结论

## 1. 调研背景

当前项目 `opencode-provider-switch` 已经具备：

- provider / alias / target 抽象
- OpenAI Responses 与 Anthropic Messages 协议分流
- 本地代理转发、SSE 透传、首字节前 failover
- OpenCode 配置同步与模型发现能力

在此基础上，`../opencode-sdk-go` 可能对项目有帮助。为避免误判，本次调研重点阅读了 SDK 的：

- 仓库结构与公开 API
- client 初始化方式
- request option / transport / retry / timeout 设计
- SSE / 流式事件模型
- 类型系统、union、元数据保真策略
- README / 测试 / 示例代码中的真实使用方式

## 2. 一句话结论

`opencode-sdk-go` 更像 OpenCode 控制面 SDK，不是通用 OpenAI / Anthropic 上游调用 SDK。

它最适合用于：

- 读取 OpenCode 当前配置与 provider 元数据
- 消费 OpenCode 的强类型事件流
- 做 sync 后校验、doctor、桌面诊断、状态观测

它不适合直接替换当前项目的：

- 上游 provider 转发链
- protocol-aware 本地代理
- alias failover 运行时逻辑

## 3. 核心结论摘要

### 3.1 SDK 的真实定位

从源码看，SDK 默认面向的是 OpenCode 自己的 HTTP API，而不是第三方模型厂商 API：

- 默认 `BaseURL` 为 `http://localhost:54321/`
- `Client` 暴露的是 `App / Config / Session / Event / File / Find / Project / Path / Agent / Tui`
- 流式入口是 `Event.ListStreaming(...)`
- 配置模型中有 provider / model / apiKey / baseURL 等字段，但 transport 层没有看到通用 `WithAPIKey(...)` 一类封装

这说明它主要服务于：

- 本地运行中的 OpenCode 实例
- OpenCode 自身暴露的控制面 / 状态面 API
- 会话、事件、配置、provider 清单等资源

### 3.2 对当前项目最有价值的部分

最值得借鉴或接入的点有四类：

1. `RequestOption + RequestConfig` 网络层设计
2. 强类型 SSE / 事件流设计
3. 元数据保真设计：`ProviderID / ModelID / Metadata / ExtraFields / RawJSON`
4. 错误分层与调试能力：typed error + raw response

### 3.3 最重要的边界判断

当前项目里：

- `internal/proxy/server.go` 是数据面
- `internal/config/protocol.go` 是协议差异层
- `internal/opencode/opencode.go` 是 OpenCode 配置同步层
- `internal/app/service.go` 是编排层

`opencode-sdk-go` 最适合增强的是 OpenCode 集成层和观测层，不是替代 `proxy` 数据面。

## 4. SDK 仓库结构与模块分层

### 4.1 关键文件总览

| 文件 | 作用 |
| --- | --- |
| `client.go` | 顶层 `Client`、默认选项、通用 `Execute/Get/Post/...` |
| `option/requestoption.go` | `WithBaseURL`、`WithHTTPClient`、`WithHeader`、`WithMiddleware`、`WithRequestTimeout`、`WithMaxRetries` 等 |
| `option/middleware.go` | 调试日志 middleware 示例 |
| `internal/requestconfig/requestconfig.go` | 请求构造、默认头、middleware 链、重试、超时、响应解析、错误处理 |
| `event.go` | `/event` 资源与流式事件 union |
| `packages/ssestream/ssestream.go` | SSE decoder 与泛型 `Stream[T]` |
| `session.go` | `Session`、`Message`、`Part`、错误类型、工具状态等大量核心模型 |
| `app.go` | `Provider`、`Model`、`AppService.Providers()` |
| `config.go` | OpenCode 配置模型与 provider 配置 |
| `shared/shared.go` | 共享错误类型：`ProviderAuthError`、`MessageAbortedError`、`UnknownError` |
| `shared/union.go` | 标量 union 辅助类型 |
| `internal/apijson/*` | JSON 解码、union 选择、原始字段元信息 |

### 4.2 架构层次

SDK 基本可以理解为四层：

1. 公开资源层
2. option / middleware 层
3. requestconfig 传输执行层
4. apijson / union / field 序列化层

实际调用链大致是：

`Client.NewClient(...)`
-> `NewXxxService(...)`
-> `Service.Method(...)`
-> `requestconfig.ExecuteNewRequest(...)`
-> `http.Client.Do(...)`

这里最重要的设计特点是：

- service 很薄
- transport 很集中
- 配置项统一走 option
- 流式与非流式共享同一套请求主干

## 5. 对外 API 与初始化模式

### 5.1 `Client` 暴露的主要服务

`client.go` 中 `Client` 暴露了这些服务：

- `Event`
- `Path`
- `App`
- `Agent`
- `Find`
- `File`
- `Config`
- `Command`
- `Project`
- `Session`
- `Tui`

这表明 SDK 并不是“聊天专用 SDK”，而是 OpenCode 全量 API 的强类型客户端。

### 5.2 初始化方式

最短路径：

```go
client := opencode.NewClient()
```

也可以显式指定服务地址：

```go
client := opencode.NewClient(
    option.WithBaseURL(baseURL),
)
```

关键事实：

- 默认会注入 `WithEnvironmentProduction()`
- 默认 base URL 是 `http://localhost:54321/`
- 若存在环境变量 `OPENCODE_BASE_URL`，会覆盖默认地址

这再次说明它是 OpenCode API client，不是通用第三方 provider client。

### 5.3 通用请求入口

除了强类型 service 方法，`Client` 还暴露了：

- `Execute`
- `Get`
- `Post`
- `Put`
- `Patch`
- `Delete`

这类设计的价值是：

- 主路径走强类型 service
- 临时或未文档化 endpoint 仍有兜底调用入口

这个模式对 `opencode-provider-switch` 也有借鉴意义：强类型主路径 + 通用兜底路径，兼容性更强。

## 6. 请求层设计：值得重点借鉴

### 6.1 `RequestOption` 模式

`option/requestoption.go` 是 SDK 最值得借鉴的文件之一。

它把几乎所有请求层能力都收敛成 `RequestOption`：

- `WithBaseURL`
- `WithHTTPClient`
- `WithMiddleware`
- `WithHeader`
- `WithQuery`
- `WithJSONSet`
- `WithRequestBody`
- `WithResponseInto`
- `WithResponseBodyInto`
- `WithRequestTimeout`
- `WithMaxRetries`

优点：

- client 级默认值与 request 级覆盖机制清晰
- 横切能力统一，不会散落在业务层
- 适合渐进式加能力，不需要大量改 service 代码

### 6.2 middleware 设计

SDK 中间件签名非常直接：

```go
type MiddlewareNext = func(*http.Request) (*http.Response, error)
type Middleware = func(*http.Request, MiddlewareNext) (*http.Response, error)
```

价值：

- debug log
- trace id
- metrics
- auth 注入
- 请求审计
- response dump

都可以作为 middleware 叠加，不需要侵入业务方法。

对当前项目来说，这比“每个 provider 单独手写 HTTP 包装”更可维护。

### 6.3 transport 注入

SDK 支持：

- 注入标准 `*http.Client`
- 注入更轻量的 `Doer`
- 保留标准库生态兼容性

这对测试、mock、代理链路替换都很友好。

### 6.4 service 薄、transport 厚

SDK 里的 service 方法通常只做四件事：

1. 合并 options
2. 校验 path 参数
3. 拼接相对路径
4. 调统一执行器

这是非常清晰的边界划分。当前项目后续若继续演化网络层，也建议保持这个方向。

## 7. 超时、重试、取消：实现质量较高

### 7.1 retry 策略

`internal/requestconfig/requestconfig.go` 中的 retry 逻辑比较完整。

会考虑：

- 连接错误
- `408`
- `409`
- `429`
- `>=500`
- `x-should-retry` header 显式覆盖

默认重试次数为 `2`。

### 7.2 retry delay 策略

SDK 会优先解析：

- `Retry-After-Ms`
- `Retry-After`

否则回退到：

- 指数退避
- 上限控制
- 带 jitter

这是比较成熟的实现，不是“固定 sleep”。

### 7.3 双层 timeout 语义

SDK 明确区分：

- `context timeout`：整个请求生命周期
- `WithRequestTimeout`：单次 attempt 的 timeout

这点很重要。

对于当前项目的 failover / alias 切换场景，双层 timeout 非常有价值：

- 外层控制整体用户体验
- 内层控制单次上游卡死
- 避免一个上游拖垮整轮切换

### 7.4 streaming timeout 处理

SDK 对 streaming request 做了专门处理：

- 返回 `*http.Response` 后，不会立即吃掉 body
- 若存在 per-request timeout，会用包装过的 body 管理 cancel 生命周期
- `stream.Err()` 能正确反映 context deadline / timeout

这说明它不是“普通请求逻辑硬套到 SSE 上”，而是认真处理了流式请求的生命周期。

## 8. SSE / 流式事件模型：非常值得研究

### 8.1 流式入口

流式 API 入口：

```go
client.Event.ListStreaming(ctx, opencode.EventListParams{})
```

它会自动设置：

```http
Accept: text/event-stream
```

然后返回：

```go
*ssestream.Stream[EventListResponse]
```

消费方式：

```go
for stream.Next() {
    evt := stream.Current()
}
if err := stream.Err(); err != nil {
    // handle
}
```

### 8.2 `ssestream` 的职责

`packages/ssestream/ssestream.go` 提供了：

- SSE 帧解析
- `event:` / `data:` 处理
- 多行 `data:` 拼接
- 泛型 `Stream[T]`
- `Next / Current / Err / Close` 统一接口

这层做得很薄，也很干净。

### 8.3 事件不是“纯文本 token 流”

这点非常关键。

SDK 的事件模型是强类型事件流，而不是简单的“文本 chunk 流”。

重要事件包括：

- `message.updated`
- `message.removed`
- `message.part.updated`
- `message.part.removed`
- `session.error`
- `session.idle`
- `server.connected`
- 以及 todo / file watcher / permission / lsp 等其他事件

对当前项目的启发是：

如果未来要把更多 OpenCode 事件接到 GUI、trace viewer、debug panel 中，内部建模不能只考虑“delta 文本”。

### 8.4 `message.part.updated` 是关键桥点

这个事件最有价值：

- `Part`
- `Delta`

也就是它同时给出：

- 当前 part 的完整态
- 本次文本增量

这意味着理想消费方式应该是“双通道”：

- 实时展示用 `Delta`
- 最终一致性用 `Part` / `Part.Text`

对于协议适配层、事件回放、debug trace 都很重要。

### 8.5 结束信号分层明确

SDK 的流式结束语义并不混乱，至少有三层：

1. transport EOF
2. transport error
3. business event
   - `session.error`
   - `session.idle`

这是一个很成熟的设计点。

对当前项目启发：

- 不能把“HTTP 断流”“业务结束”“业务报错”混成一类 finish
- 未来若做更深的协议桥接，内部状态机必须保留这几个层次

## 9. 类型系统与元数据保真：本次调研最大亮点之一

### 9.1 请求参数不是普通 Go struct

SDK 请求字段使用 `param.Field[T]` 建模。

辅助函数包括：

- `F(value)`
- `Null()`
- `Raw()`
- `String()`
- `Int()`
- `Bool()`

这让 SDK 可以明确表达：

- 字段未传
- 字段显式为 `null`
- 字段是正常值
- 字段要以原始形态透传

这对协议转换非常重要，因为很多协议差异不在“字段名”，而在“字段是否应该省略”。

### 9.2 响应对象保留 `.JSON` 元数据

几乎所有响应 struct 都带一个 `JSON` 字段，内部会记录：

- 每个字段的 `apijson.Field`
- `raw` 原始 JSON
- `ExtraFields` 未声明字段

这是一种很强的保真设计。

价值：

- 区分“字段缺失”和“字段为零值”
- 保留未知字段
- 兼容服务端 schema 演进
- 降低协议转换时的信息损失

### 9.3 union 使用广泛

SDK 中广泛使用 union，典型场景包括：

- `Message`
- `Part`
- `ToolPartState`
- `AssistantMessageError`
- `EventListResponse`

并且通常会提供：

- `AsUnion()`
- `UnmarshalJSON(...)` 中先解 union，再回填通用壳对象

这是比 `map[string]any` 更工程化的做法。

### 9.4 `ProviderID` 与 `ModelID` 分离建模

这是一个很值得当前项目学习的设计。

SDK 中多个地方都同时保留：

- `ProviderID`
- `ModelID`

而不是只用一个拼接字符串。

这对当前项目尤其重要，因为 `opencode-provider-switch` 的核心就是：

- alias
- provider
- model
- protocol
- failover

若只保留 `provider/model` 字符串，很多上下文很容易在 trace、UI、错误归因时丢失。

### 9.5 模型元数据很丰富

`app.go` / `config.go` 中的模型相关结构包含：

- `Options`
- `Modalities`
- `Reasoning`
- `ToolCall`
- `Temperature`
- `Experimental`
- `Status`
- `Cost`
- `Limit`
- `Provider`

这说明 SDK 对模型的理解不是“只有一个 model id”。

对于当前项目后续若要做：

- alias 能力标注
- 模型能力差异可视化
- OpenCode sync 对账
- provider model catalog 保真

这些字段都很有参考价值。

## 10. 错误模型：值得直接借鉴思路

SDK 把错误分成了几类：

- `ProviderAuthError`
- `MessageAbortedError`
- `UnknownError`
- `APIError`

其中 `APIError` 还保留：

- `StatusCode`
- `ResponseBody`
- `ResponseHeaders`
- `IsRetryable`

优点：

- 不只知道“失败了”
- 还知道“哪类失败”“是否可重试”“上游原始响应是什么”

对当前项目最直接的借鉴点在：

- `internal/proxy/server.go`
- `internal/proxy/traces.go`
- GUI / trace viewer / doctor 输出

目前项目如果进一步细化错误分类，这套思路会很有帮助。

## 11. 示例代码与真实使用方式

### 11.1 普通读取

```go
client := opencode.NewClient()
sessions, err := client.Session.List(context.TODO(), opencode.SessionListParams{})
```

### 11.2 指定 BaseURL

```go
client := opencode.NewClient(
    option.WithBaseURL(baseURL),
)
```

### 11.3 流式读取

```go
stream := client.Event.ListStreaming(ctx, opencode.EventListParams{})
for stream.Next() {
    evt := stream.Current()
    _ = evt
}
if err := stream.Err(); err != nil {
    // handle
}
```

### 11.4 说明

SDK 仓库里没有成熟的 `examples/` 目录示例程序。真实使用方式主要来自：

- `README.md`
- `usage_test.go`
- `session_test.go`
- `client_test.go`

这意味着如果当前项目后续要引入 SDK，最好自己补一层贴近业务的封装，而不是直接把测试代码搬进主仓库。

## 12. 与当前仓库的映射分析

## 12.1 当前仓库最相关文件

与 `opencode-sdk-go` 最相关的现有接入点，按优先级排序：

1. `internal/opencode/opencode.go`
2. `internal/opencode/provider_models.go`
3. `internal/app/service.go`
4. `internal/app/types.go`
5. `internal/proxy/traces.go`
6. `internal/proxy/server.go`

### 12.2 为什么不是 `proxy` 优先

因为当前 `proxy` 层负责的是：

- 上游协议路由
- alias -> target 路由
- failover
- 流式透传
- 首字节前失败重试
- Anthropics / OpenAI 头与路径差异

而 SDK 负责的是：

- OpenCode 自己的控制面 API
- 强类型事件与资源对象
- 配置、provider、session、event 访问

这两层关注点不一样。强行替换会造成边界混乱。

### 12.3 最适合接 SDK 的位置

#### `internal/opencode/opencode.go`

最像 SDK adapter 层。

适合承接：

- 读取 OpenCode 当前配置
- 读取 OpenCode provider 列表
- sync 后做对账与验收
- 输出更结构化的诊断信息

#### `internal/app/service.go`

最像 orchestration 层。

适合承接：

- `doctor`
- `preview sync`
- `apply sync`
- 桌面 GUI 发起的状态检查

#### `internal/app/types.go` 与 `internal/proxy/traces.go`

适合借鉴 SDK 的建模风格：

- 保留 `ProviderID`
- 保留 `ModelID`
- 保留更多模型能力信息
- 保留 raw / extra metadata

#### `internal/opencode/provider_models.go`

适合借鉴 SDK 的 transport / option / retry / timeout 风格，但不一定要直接替换为 SDK。

## 13. 对当前项目的实用建议

### 13.1 建议一：先做“读侧接入”，不要先做“写侧替换”

建议优先把 SDK 用在：

- OpenCode 当前配置读取
- provider / model 元数据读取
- event 观测
- sync 后校验

不建议第一步就尝试：

- 用 SDK 改写本地配置写回逻辑
- 用 SDK 替换上游 provider 请求链

原因：

- 读侧风险低
- 价值高
- 可快速验证 SDK 是否稳定、是否适合项目边界

### 13.2 建议二：把 SDK 的 option 设计内化到项目 transport 层

即便不直接引入 SDK 作为运行时依赖，也建议参考其设计，把当前项目网络层逐步统一到：

- `BaseURL`
- `Headers`
- `HTTPClient`
- `Middleware`
- `RequestTimeout`
- `MaxRetries`
- `ResponseInto`

这对 `provider_models` 与未来更多协议扩展都很有帮助。

### 13.3 建议三：内部事件模型不要只围绕文本构建

未来若接更多 OpenCode 事件给 GUI 或调试层，建议内部归一化事件模型至少区分：

- message 级更新
- part 级更新
- 文本 delta
- tool / snapshot / retry / step 状态
- business error
- idle / completed

不要只保留“最后吐出来的文本”。

### 13.4 建议四：提升 trace 保真度

当前项目已有 trace 能力。下一步可考虑对齐 SDK 的思路，补充：

- `ProviderID`
- `ModelID`
- 原始错误 body
- response headers
- retryable 标记
- 更细的错误分类

这能显著提升：

- failover 可解释性
- GUI 调试价值
- 故障排查效率

## 14. 最值得做的 PoC

如果只做一个最有价值、风险最低的 PoC，建议是：新增一个基于 SDK 的“只读诊断能力”。

目标：

1. 读取 OpenCode 当前配置
2. 读取 OpenCode 当前 provider 列表
3. 与本地 `ocswitch` 配置做对账
4. 输出诊断结果

建议诊断内容：

- 哪些 alias 已同步到 OpenCode
- 哪些 alias 本地可路由但未同步
- OpenCode 里 provider 是否存在协议错位
- provider `baseURL` / `timeout` / `apiKey` 是否异常
- 模型 catalog 是否与本地发现结果不一致

这个 PoC 兼具：

- 低风险
- 高实用性
- 与 SDK 定位高度一致

## 15. 明确不建议的方向

以下方向不建议作为第一阶段动作：

1. 用 `opencode-sdk-go` 直接替换 `internal/proxy/server.go`
2. 用 SDK 直接承接所有 OpenAI / Anthropic 上游调用
3. 让 SDK 的类型直接渗透到仓库所有层
4. 把 SDK 误当成通用 provider transport 层

原因都很一致：SDK 的中心是 OpenCode API，不是当前项目代理到各家上游 provider 的数据面。

## 16. 最终结论

`opencode-sdk-go` 对当前项目确实有帮助，但帮助点主要在“控制面”和“观测面”，不是“数据面转发”。

最值得吸收的能力是：

- 干净的 request option / transport 设计
- 强类型事件流与流式生命周期建模
- `ProviderID / ModelID / Metadata / RawJSON / ExtraFields` 这类保真策略
- 更细粒度的错误分类与调试信息保留

最现实、最实用的落地方向是：

- 增强 `internal/opencode/*` 与 `internal/app/service.go`
- 增强 doctor / sync 对账 / GUI 诊断
- 参考 SDK 改善本项目 transport 与 trace 设计

而不是试图直接让 SDK 接管本项目的 proxy 主链路。
