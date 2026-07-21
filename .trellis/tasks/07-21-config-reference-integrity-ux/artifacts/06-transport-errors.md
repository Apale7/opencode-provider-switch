# 传输错误与多端映射 V1

> 本文只映射 `04-diagnostic-schema.md` 与 `05-lifecycle-api.md` 的领域结果，不重新定义其 DTO。

## Canonical envelope

```json
{"ok":true,"data":{},"outcome":{"code":"ok","params":{},"retryable":false}}
```

```json
{"ok":false,"error":"revision_conflict","outcome":{"code":"revision_conflict","params":{},"retryable":false}}
```

`ok` 与 `outcome` 必需。`outcome.code` 为稳定 lower_snake_case，params 无值为 `{}`，retryable 必需。`error` 仅失败时存在且严格等于 code，用作旧 HTTP 兼容；禁止 message/cause。`data` 可承载既定 DTO，尤其 runtime_apply_failed 必须保留 execute result；错误时存在 data 不表示成功。

后端只返回 code+安全 params，客户端本地化。不得解析 `err.Error()` 分类。

## 业务分类

| code | 语义 | HTTP | CLI exit |
|---|---|---:|---:|
| `ok` | 成功或成功 preview（可含 blockers） | 200 | 0 |
| `validation_failed` | 通用输入 validation 失败 | 422 | 2 |
| `plan_not_executable` | execute 仍有 blocker/required choice，或无可执行 token | 422 | 3 |
| `revision_conflict` | 锁内 current 与 expected 不同 | 409 | 4 |
| `not_found` | 当前 revision 的主实体不存在 | 404 | 5 |
| `config_io_failed` | persist 前读取、sidecar 或锁定失败 | 500 | 6 |
| `runtime_candidate_build_failed` | persist 前 candidate builder 拒绝 | 422 | 7 |
| `persist_failed` | 原子持久化失败 | 500 | 6 |
| `runtime_apply_failed` | commit 后 live apply 失败，必须带 result | 500 | 8 |
| `restart_pending` | 成功提交且 result.pendingRestart=true | 200 | 0 |
| `invalid_request` | malformed/shape error | 400 | 2 |
| `revision_required` | mutation 缺 revision | 428 | 2 |
| `config_store_busy` | 确认未提交的临时锁忙 | 503 | 6 |
| `unauthenticated` | 未认证 | 401 | 1 |
| `forbidden` | 无权限 | 403 | 1 |
| `method_not_allowed` | 方法错误 | 405 | 2 |
| `internal_error` | 未分类编程/不变量错误 | 500 | 1 |

05 的领域错误保留原始稳定 code：`plan_mismatch`/`plan_expired`/`plan_not_executable`/`preparation_stale`/`candidate_invalid`/`runtime_candidate_build_failed`/`persist_failed`/`runtime_apply_failed` 分别映射 HTTP `422/410/422/409/422/422/500/500`，CLI `2/2/3/4/2/7/6/8`。`revision_conflict` 保持 409/4。`candidate_invalid` 使用 validation 展示，`persist_failed` 使用 I/O 展示，但 wire code 不重命名。`validation_failed` 仅用于 lifecycle 外普通输入校验；`config_io_failed` 仅用于尚未进入 atomic persist 的读/锁/sidecar I/O。废弃 `lifecycle_blocked` 与 `runtime_build_failed` 别名，新 producer 不得返回。所有入口传递相同原始 code。

409 专用于 revision mismatch（含 preparation stale），不用于 ownership block、validation 或 not found。preview 有 blocker 是成功 200；执行未解决 blocker返回 `plan_not_executable`。restart pending 是成功，不得伪装 apply failure。stale 优先于 not found。

`retryable=true` 仅表示服务端确认未提交且相同 payload 可自动重放；V1 仅 `config_store_busy` 可为 true。revision conflict 必须重新读和 preview；网络结果未知、I/O、runtime apply 均不得自动重放。runtime_apply_failed 的 retryable 仍为 false，但 execute result 可暴露稳定恢复 action，允许用户显式请求“收敛 runtime 到已提交 revision”；这不是自动重放 mutation。

## HTTP

- 全部 `/api/` JSON 使用 envelope，管理配置响应 `Cache-Control:no-store`。
- 非 2xx 仍返回完整 envelope；React adapter 不得只抛普通 Error 丢 data。
- runtime_apply_failed 为 500 但 data 保留 execute result，明确“磁盘已提交/runtime 旧”；proxy 未运行且 result.pendingRestart=false 时 outcome 为 `ok`，data.runtimeState=`not_running`，不是 restart_pending。
- 可发送 Retry-After 的只有 config_store_busy。
- 不返回 OS error、路径、stack 或秘密。

## Wails

新 revision/lifecycle 方法返回与 HTTP 等价 envelope。已分类业务失败通过 resolved Promise 返回；只有 bridge/serialization/context/未捕获故障使用 rejection。runtime_apply_failed 必须 resolved 并保留 result。

旧 direct-return 方法不原地改 shape；新增 versioned 方法。无 revision 的旧 mutation fail closed 为 revision_required，不得自动读取 latest 代替用户意图。React Wails adapter 可抛 typed exception，但必须保存完整 envelope/data。

## CLI

`--json`：stdout 恰好一个 envelope 加换行；已分类错误 stderr 为空；exit code 按表；无 ANSI/进度/Cobra 重复错误。JSON 编码失败退出 1 且不得输出半截 JSON。

Human：成功摘要 stdout，失败 stderr，首行为 `error[<code>]: <localized summary>`。runtime_apply_failed 首先说明“配置已保存，但 runtime 未应用”；restart_pending 保存成功并在 stderr warning，exit 0。preview 有 blockers 仍 exit 0；execute blocked exit 3。CLI 与 TUI human presenter 必须使用同一 sanitizer 移除控制字符和 ANSI escape，并对实体 ID 限长。

独立 CLI 无认证 IPC acknowledgement 时不得报告其他进程 runtimeApplied，只能 restart_pending/not running。

## TUI

Tea business message 携带 typed envelope；`err error` 只用于框架故障。validation 保留表单；lifecycle blocked 停留 impact view；revision conflict 废弃 plan/revision并刷新；not found 刷新列表；runtime build 不显示已保存；runtime apply failure 显示持久 banner，直到确认恢复；restart pending 用 warning/status 非失败样式。

Presenter 只按 code+params 本地化。用户 ID 展示前移除控制字符/ANSI。未知 code 安全 fallback。

## Params 与 i18n

Outcome params 只放摘要：operation、issueCount/blockerCount、scopes、resourceType/resourceId、stage/pathKind、component、稳定 reason。revision、choices、actions、blockers 与 committed 状态留在业务 DTO，不仅放 params。

i18n keys：`transport.outcome.<code>` 及 `transport.operation/scope/resourceType/stage/component/reason.<value>`。React/TUI/CLI human 使用现有 `en-US` 与 `zh-CN`，两端 placeholder 一致；JSON/HTTP/Wails 与 locale 无关。未知 code 显示通用文案和安全 code，不显示内部 cause。

禁止 envelope/params/fallback 含 API key、Authorization/Cookie/Header value、config/import/export 原文、body、敏感 URL、OS error、stack、绝对路径。可含不透明 revision（仅业务 DTO）、稳定 enum、限长转义后的实体 ID 与计数。

## 旧客户端兼容

- HTTP 保留顶层 data 和失败时 string error，但 error 值改稳定 code；ok/outcome 增量加入。
- 缺 revision 的旧 mutation 返回 428 且零变化，不静默补 current revision。
- 新 preview/execute 使用新 endpoint/绑定，避免旧返回类型误解。
- Wails 旧方法不改 shape；不能安全适配的 mutation fail closed。
- CLI human 默认为兼容模式；失败仍非零，但细分 exit code需在迁移说明标注。
- capability 增量暴露 `transportEnvelopeVersion:1` 与 `lifecycleContractVersion:1`。

## 测试不变量

- 同一领域错误经 HTTP/Wails/CLI/TUI 得到相同 code/params/retryable。
- HTTP 409 只由 revision mismatch；preview blocker 200，execute blocked 422。
- candidate build 422 且 persist 前；post-commit apply 500/CLI8/Wails resolved 且保留 result。
- restart pending HTTP/Wails success、CLI0、TUI warning。
- CLI JSON 单文档、classified stderr 空。
- TUI 不展示业务 err.Error；Wails classified failure 不 rejection。
- error 字符串严格等于 code。
- en-US/zh-CN keys 与 placeholders 对齐；恶意实体 ID fixture 在 CLI/TUI 均不能注入 ANSI 或控制序列。
- 秘密、路径、stack 不进入 envelope。
- unknown code 不导致任一客户端崩溃。
