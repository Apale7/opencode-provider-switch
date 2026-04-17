# Deep Code Review: ocswitch

Date: 2026-04-17
Reviewer: Apale
Scope: `internal/config`, `internal/proxy`, `internal/opencode`, `internal/cli`, `README.md`
Verification: `go test ./...`, `go test -race ./...`

## Summary

本次 review 未发现立即可触发的数据破坏级致命 bug，但确认存在 2 个高风险项、4 个中风险项，以及 4 个低风险/改进项。高风险集中在配置文件并发写入和代理入站 body 无读取超时；中风险集中在流式连接悬挂、错误语义丢失、OpenCode 默认模型静默写坏，以及默认本地 API Key 在非 loopback 场景下的暴露风险。

## High Risk

### 1. Config/OpenCode save path is not truly safe under concurrent writers

- Files:
  - `internal/config/config.go:147-175`
  - `internal/opencode/opencode.go:84-97`
- Problem:
  - 两处保存逻辑都使用固定临时文件名 `path + ".tmp"`，且没有文件锁。
  - `opencode.Save` 还属于 read-modify-write，基于旧文件内容生成新内容，存在并发 stale read 覆盖问题。
- Trigger:
  - 两个 CLI 进程、两个自动化脚本或未来后台流程同时写同一配置文件。
- Impact:
  - 临时文件互相覆盖、最后写入者静默丢掉前一个更新、配置内容回退或错乱。
- Recommended fix:
  - 抽公共原子写 helper。
  - 使用 `os.CreateTemp(dir, base+".*.tmp")` 创建唯一 tmp。
  - 写入后 `Sync` 文件、`Close`、`Rename`，然后 `Sync` 父目录。
  - 外层增加 advisory file lock。
  - `opencode.Save` 必须在锁内完成 read-modify-write，避免旧快照覆盖新文件。

### 2. Proxy request body has no read timeout after headers

- Files:
  - `internal/proxy/server.go:79-83`
  - `internal/proxy/server.go:148-151`
- Problem:
  - 仅配置 `ReadHeaderTimeout`，body 通过 `io.ReadAll(http.MaxBytesReader(...))` 全量读取，没有 body read timeout。
- Trigger:
  - 客户端快速发完 headers，然后慢速发送 body 或中途长期停住。
- Impact:
  - slowloris 式连接占用，长期占住 goroutine/socket，代理容易被低成本拖死。
- Recommended fix:
  - 给 `http.Server` 增加 `ReadTimeout`，至少覆盖 header+body 接收阶段。
  - 若后续 body 可能变大，再把该值做成配置项。
  - 将底层超时统一映射成稳定的客户端错误，而不是直接回传底层 I/O 文本。

## Medium Risk

### 3. Streaming response has no idle timeout once bytes start flowing

- Files:
  - `internal/proxy/server.go:61-64`
  - `internal/proxy/server.go:307-323`
- Problem:
  - `http.Client.Timeout = 0`，流式转发阶段没有任何后续 idle timeout。
- Trigger:
  - 上游先返回部分 chunk，随后长时间不再发送数据也不主动断开。
- Impact:
  - 请求长期挂起，占住上下游连接与 handler，累计后形成资源耗尽。
- Recommended fix:
  - 非流式请求使用整体超时。
  - 流式请求增加“空闲超时”而非“总超时”，超时后取消上游 context 并中断转发。

### 4. Retryable upstream failures are collapsed into a generic 502

- Files:
  - `internal/proxy/server.go:216-217`
  - `internal/proxy/server.go:257-260`
- Problem:
  - 上游 `429`/`5xx` 被当作可重试失败吞掉；所有 target 都失败时统一返回 `502 all upstream targets failed`。
- Trigger:
  - 单个 target 返回 `429/5xx`，或所有 target 都返回 retryable failure。
- Impact:
  - 客户端无法拿到真实 `429`、`Retry-After`、上游错误体；限流与退避语义丢失，排障信息也被抹平。
- Recommended fix:
  - `tryOnce` 返回结构化失败信息（status/headers/body）。
  - 失败链耗尽且尚未向下游写字节时，优先透传最后一个上游错误，至少保留 `429`、`Retry-After`、`Content-Type` 和裁剪后的 body。

### 5. `opencode sync --set-model` can silently write an invalid default model

- Files:
  - `internal/cli/opencode.go:82-93`
- Problem:
  - `--set-model` 和 `--set-small-model` 不校验输入是否是当前可路由 alias。
- Trigger:
  - 用户输入不存在的 alias、已禁用 alias 或不符合 `ocswitch/<alias>` 约定的值。
- Impact:
  - `sync` 表面成功，但会把 OpenCode 顶层默认模型写成无效值，形成静默坏配置。
- Recommended fix:
  - 仅接受 `ocswitch/<alias>` 形式。
  - `<alias>` 必须存在于 `cfg.AvailableAliasNames()`。
  - 报错时给出候选 alias 列表。

### 6. Fixed default local API key remains unsafe when binding to non-loopback addresses

- Files:
  - `internal/config/config.go:18-20`
  - `internal/config/config.go:83-89`
  - `internal/config/config.go:392-395`
  - `internal/proxy/server.go:120-133`
- Problem:
  - 默认本地 API Key 为固定公开值 `ocswitch-local`，同时校验逻辑未阻止用户在非 loopback 监听时继续使用默认 key。
- Trigger:
  - 用户将 `server.host` 设置为 `0.0.0.0`、局域网 IP、容器映射地址等。
- Impact:
  - 网络上任何知道默认 key 的访问者都可直接调用代理并消耗上游额度。
- Recommended fix:
  - 在 `Validate()` 中增加约束：非 loopback 监听时拒绝默认 key。
  - 更稳妥方案是首次生成随机 key，只在纯本机模式允许默认值。

## Low Risk / Improvement Options

### 7. Alias lifecycle is incomplete in CLI

- File: `internal/cli/alias.go:35-84`
- Problem:
  - 缺少 `alias enable/disable`，alias 一旦禁用后没有 CLI 恢复路径。
- Options:
  - 增加 `alias enable` / `alias disable` 子命令。
  - 将 `alias add` 改成三态更新：未指定、启用、禁用。

### 8. Import behavior and docs are inconsistent for empty API key

- Files:
  - `internal/opencode/opencode.go:581-594`
  - `internal/cli/provider.go:273-275`
- Problem:
  - 实现允许导入只有 `baseURL`、没有 `apiKey` 的 provider，但帮助文本/README 表述为需要两者同时存在。
- Options:
  - 收紧实现，要求 `apiKey != ""`。
  - 或放宽文档，明确允许导入空 key provider。

### 9. `opencode sync` still has broader side effects than users may expect

- Files:
  - `internal/opencode/opencode.go:58-60`
  - `internal/opencode/opencode.go:80-83`
  - `internal/opencode/opencode.go:424-428`
- Problem:
  - `opencode sync` 会丢失 JSONC 注释，还可能补写 `$schema`。
- Options:
  - 在 CLI help/README 中明确副作用。
  - 长期方案是引入更细粒度的 JSONC patch 机制，尽量避免无关键重写。

### 10. Header forwarding is still broader than ideal

- Files:
  - `internal/proxy/server.go:394-440`
- Problem:
  - 目前只做固定 hop-by-hop 过滤，转发策略仍偏黑名单。
- Options:
  - 解析 `Connection` 动态声明的 hop-by-hop headers 并移除。
  - 逐步收敛成最小白名单转发。

## Testing Notes

- `go test ./...` passed
- `go test -race ./...` passed

## Coverage Gaps Observed

- 未覆盖并发写配置文件的场景
- 未覆盖慢速 body 读取超时场景
- 未覆盖全部上游 `429/5xx` 时的错误透传语义
- 未覆盖 `--set-model` / `--set-small-model` 非法值校验
