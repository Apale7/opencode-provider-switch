# 详细设计方案：常用配置操作热更与运行时一致性

> 文件类型：详细设计方案（Detailed Design Plan）
> 状态：待执行
> 创建时间：2026-07-21
> 适用范围：Desktop、Web Admin、TUI、传统 CLI、外部配置文件与本地代理运行时

## 约束 / Constraints（不可修改）

1. **Agent 只能修改 `todolist` 区块与各 checkbox 状态（`- [ ]` ↔ `- [x]`）。**
   禁止编辑、改写、重排、新增或删除其他任何内容（steps 正文、tasks 描述、goal 文本、标题、本约束区块）。
2. **同一 step 内的 tasks 必须由多个子代理并行完成。** 一个 step 的全部 tasks需在一批并行子代理调用中下发，禁止在主代理中逐个串行执行；各 task 只能修改其声明的文件所有权范围。
3. **steps 严格顺序执行。** 上一个 step 的 checkbox 勾选完成前，不得开始下一个 step。
4. **不得破坏当前在途请求语义。** 热更后新请求读取新快照，已经进入代理的请求必须继续使用请求开始时捕获的旧快照。
5. **不得把“已写盘但未应用”返回为普通成功。** 持久化版本、运行时版本和待重启字段必须可观测；失败必须使用结构化状态明确表达。
6. **所有新增 UI 文案必须同时提供 `en` 与 `zh-CN` 翻译。** 禁止在组件中新增硬编码的用户可见字符串。

---

## 1. todolist（可实时修改）

> Agent 当前正在实施的任务清单，可随进度实时增删改、勾选。

- [ ] 固化仓库级写入口清单、热更边界与 revision 契约
- [ ] 加固 ConfigStore 提交协议和 prepared artifact 所有权
- [ ] 建立配置运行时协调器与结构化回执
- [ ] 迁移 App、CLI 和 bootstrap 的全部生产配置写入口
- [ ] 实现代理监听重绑、外部文件收敛与 Admin 待重启状态
- [ ] 补齐前端状态、i18n、回归测试和独立评审

---

## 2. steps（顺序执行，存在依赖）

> 步骤之间存在依赖关系，只能顺序执行。每个 step 内部的 tasks 互不依赖，由多个子代理在同一批调用中并行完成。

### - [ ] Step 1：冻结现状矩阵、热更等级与一致性契约

- 依赖：无
- 完成定义：所有生产配置写入口均有唯一归属；明确哪些操作当前已经局部热更、哪些确实需要人工重启及其根因；后续实现不得重新解释并发与生效语义。
- Tasks（并行，子代理执行）：
  - [ ] Task 1.1：按以下仓库级矩阵建立测试清单，执行时逐项映射到测试名。

    | 操作 / 写入口 | 当前行为与人工重启根因 | 目标等级与行为 |
    |---|---|---|
    | `internal/app/manage.go` 的 Provider、Alias、Target、Rewrite、Priority、Auto Alias 写入口 | 已从直接 `cfg.Save()` 迁到 `commitConfig`，Apply 时调用 `reloadRunningProxyConfig`，运行中已局部热更；仍缺 prepared runtime、NoOp 短路、状态代际和行为测试，并非都需要人工重启 | L1；升级现有管线，下一请求使用新 revision，在途请求保持旧 revision |
    | `SaveProxySettings` | 已走 `commitConfig` 并替换 proxy runtime，多数 timeout/failover/routing 已实际热更；固定 warning 仍声称需重启，listener 级 `http.Server.ReadTimeout` 不会更新 | timeout/failover/routing/api key 全部收敛为 L1；把 request-read deadline 下沉到 handler，删除错误重启提示 |
    | `ImportConfig` | 已走 `commitConfig`，L1 字段会随 Apply 替换 runtime，但固定 warning 错称仍使用旧内存配置；proxy listener 和 Admin listener/auth 不会变化 | 路由配置 L1、代理 host/port L2、Admin 字段 L3；返回分字段回执 |
    | `SaveDesktopPrefs` | 已走 `commitConfig`，但通用 Apply 会不必要地 reload proxy；代理无需重启 | L0；保留 CAS 持久化，只执行桌面副作用，不构建或替换代理 runtime |
    | `internal/app/config_store.go`、`lifecycle_api.go`、`RemoveProviderWithPlan` | 已有 ConfigStore/CAS 基础；`commitConfig` 空 expected revision 会先取最新值，lifecycle NoOp 仍主动 reload，所有路径共享“Build=Config、Apply=reload”的粗粒度 hooks | 统一升级为协调器；NoOp 零 Apply，planned delete/upgrade 保留 preview、plan token 与原 revision 校验 |
    | `internal/cli/provider.go` 4 个、`alias.go` 4 个、`rewrite.go` 3 个生产写入口 | 共 11 个入口已通过 `appService()` 创建短生命周期 Service 并走 ConfigStore，但会初始化不需要的 runtime 资源，且无法判断另一进程状态 | 改用轻量共享 facade；返回 persisted revision/`persisted_only`，长驻进程由 watcher 收敛 |
    | `ocswitch serve` | 创建长生命周期 Service 并启动代理，当前无 watcher/Close | 作为 `cli_serve` 长驻模式启用 watcher，并在退出时关闭 Service |
    | `internal/server.ensureAdminConfig` 启动写入 | 生成 Admin key、应用 CLI host/port 后直接 `cfg.Save()`；发生在 Service/watch 启动前 | Bootstrap 例外；仍通过 ConfigStore 建立 revision，但不触发 live apply，完成后才创建 active baseline |
    | 外部编辑或原子替换 `config.json` | 无 watcher，长驻进程永久停留在旧内存配置 | L1/L2/L3 分类收敛；非法配置保留最后可用 runtime |
    | OpenCode Sync | 写外部 OpenCode 配置，不修改 ocswitch runtime 配置 | L0 External Apply；维持独立流程 |

    功能矩阵必须覆盖：Provider 新增/编辑/刷新模型/导入/启停/删除/优先级；Alias 新增/编辑/启停/删除；Target 绑定/解绑/启停/排序；Rewrite 新增/启停/删除/排序；Auto Alias 开关与升级；Proxy Settings；完整导入；Desktop Prefs；传统 CLI；外部文件。
  - [ ] Task 1.2：冻结字段等级和运行时边界。
    - `L0 Side Effect`：Desktop Prefs、OpenCode Sync；不替换代理 runtime。
    - `L1 Live Swap`：`server.api_key`、全部代理 timeout、failover status codes、routing、Providers、Aliases、Targets、Rewrite Rules、Priority、Auto Alias；候选 runtime 在提交前构建，提交后原子发布。
    - `L2 Managed Rebind`：`server.host`、`server.port`；提交前预绑定，提交后启动新 listener 并排空旧 listener。
    - `L3 Process Restart`：`admin.host`、`admin.port`、`admin.api_key`、`admin.public_base_url`；仅 Server Mode 且与启动时 active Admin baseline 不同时报告 pending restart，Desktop/TUI 不得误报。
    - 新请求在 handler 入口只捕获一次 `serverRuntime`；鉴权、Rewrite、Alias、候选、重试、流式转发均使用该快照。发布只关闭旧 transport idle connections，不中断 active connections。
  - [ ] Task 1.3：冻结 revision、写意图和外部写并发语义。
    - 整体替换、远端观察后提交或基于表单/列表旧值的操作必须携带 `expectedRevision`：完整导入、Provider/Alias/Rewrite upsert 或编辑、Provider 模型 refresh、`ImportProviders(overwrite=true)`（所有 HTTP/Wails/CLI transport）、Provider Priority、Alias/Target/Rewrite reorder、Proxy Settings；冲突返回 `config_revision_conflict`。
    - 绝对且幂等、无级联选择的意图命令可在最新 snapshot 上重放：enable/disable、简单 Rewrite delete、bind/unbind；每次仍携带内部 expected revision，冲突后最多重新取最新 snapshot 并重放 3 次，目标已满足则返回 NoOp，耗尽后返回冲突。
    - Provider/Alias delete、Alias upgrade 等带引用诊断、级联选择或 plan token 的 lifecycle 操作不得自动重放；冲突后必须基于新 revision 重新 preview，并由用户重新确认新计划。
    - 经 ConfigStore/共享锁协作的写入提供严格 CAS 和不丢更新；非协作外部编辑器不遵守 advisory lock，无法承诺任意竞争窗口严格零丢失，只保证提交前二次 revision 复核、事件最终收敛和最后观察 revision 生效。文档、测试和 UI 禁止扩大承诺。
    - persisted revision 是磁盘字节的 HMAC revision；applied revision 是当前代理 runtime 捕获的完整配置 revision。二者不同时状态必须明确：当前 Service 的代理未运行为 `not_running`，纯 L0 变化为 `not_applicable`，短 CLI 为 `persisted_only`，L3 差异为 `pending_restart`，应用失败为 `degraded`。
  - [ ] Task 1.4：冻结失败和 prepared artifact 所有权语义。
    - mutation 返回 `Changed=false` 时在 Validate/Build 前短路：不写盘、不构建、不 Apply、不推进 applied revision。
    - Build 成功后 artifact 所有权属于 ConfigStore；校验后取消、写前二次冲突、确定未提交、未知提交且无法确认候选 revision 等所有 Apply 前退出路径都调用幂等 `Discard()`。
    - 持久化是不可逆提交点。提交前使用调用方 context；确认提交后即使客户端取消，也必须使用 `context.WithoutCancel` 派生的内部 10 秒有界 context 完成 Apply/状态收敛，禁止 Discard 后跳过 Apply。
    - 一旦调用 `Apply()`，artifact 所有权转移给 Apply；Apply 在成功或失败时都必须关闭未接管资源。L1 pointer swap 不返回业务错误；L2 若提交后启动失败，保留旧 listener/runtime 并进入 `degraded`。
    - 写盘结果未知时重读：等于候选 revision 则按已提交继续 Apply；等于旧 revision 则 Discard 并返回未提交；等于第三方 revision 则 Discard、禁止覆盖，由 watcher 收敛并返回 `config_commit_unknown`。

### - [ ] Step 2：加固底层提交、配置编解码与路由状态代际

- 依赖：Step 1
- 完成定义：底层 API 独立可测；NoOp、二次 CAS、未知提交和资源释放具有确定语义；旧在途请求无法污染新 Provider 状态。
- Tasks（并行，文件所有权互斥）：
  - [ ] Task 2.1：修改 `internal/configstore/store.go` 及其测试，实现 Step 1.3/1.4 的协议。
    - `Candidate.Changed=false` 在 hooks 前返回 NoOp receipt。
    - Build 返回带幂等 `Discard` 的 prepared artifact；Store 显式跟踪 Build、Persist、Apply 三阶段所有权。
    - Build 后、`AtomicWriteFile` 前在同一协作锁内重读 bytes/revision；与初始 revision 不同即 Discard 并返回 conflict。
    - 配置 Apply 的内部超时（默认 10 秒）；确认提交后忽略调用方取消并在有界 context 内完成 Apply。故障注入覆盖 Validate/Build/Discard、提交前与提交后取消、二次 conflict、persist 未提交/已提交/未知、Apply 失败；断言预绑定 listener/transport 无泄漏。
    - Snapshot 解码失败必须返回携带 path、exists、raw HMAC revision 的 typed decode error/raw inspection；watcher 可报告 degraded persisted revision，但不得暴露 raw bytes。
    - 验证：`go test ./internal/configstore -run "NoOp|SecondCAS|Prepared|Discard|CommitUnknown" -count=1`。
  - [ ] Task 2.2：修改 `internal/config/config.go`、`internal/config/config_test.go`，审计并补强现有无 I/O `LoadFromBytes`、深拷贝 `CloneDeep` 与稳定 `MarshalPersistent`。
    - `Load/Save` 和 `internal/app/config_store.go` 的 codec 继续复用现有 API，不引入同义重复接口；保持兼容字段、默认值、排序、规范化、显式 `auto_alias_enabled=false` 与结尾换行。
    - Round trip 覆盖八个顶层配置块；Clone 修改不得污染 Providers、Headers、Models、Aliases/Targets、Rewrite JSON、Priority 或内部 path。
    - 验证：`go test ./internal/config -run "Decode|Encode|Clone|RoundTrip" -count=1`。
  - [ ] Task 2.3：修改 `internal/routing/**` 并独立新增 `provider_generation_test.go`，为 `StateKey`、circuit breaker 和 memory store 增加 `ProviderGeneration` 与有界回收。
    - generation 由连接 fingerprint 与当前进程内单调 incarnation 组成。fingerprint 使用 `RuntimeResources` 的进程随机 HMAC key，覆盖 protocol、规范化 base URLs、URL strategy、API key/key set 和规范化后的全部 configured Headers；日志与状态不得暴露凭据。未变化 Provider 复用现有 generation，连接身份变化或同 ID 删除后重建必须分配新 incarnation。
    - 旧 runtime 只能回写旧 generation，新 runtime 永不读取；memory store 记录最后访问时间，并按 TTL 与每 Provider 最大代际数做有界惰性清理，即使旧请求再次写入也不污染当前代际。
    - 验证：`go test ./internal/routing -run "ProviderGeneration|StateIsolation|StateRetention|GenerationGC" -count=1`。

### - [ ] Step 3：实现稳定的运行时接口、结构化类型和 listener 原语

- 依赖：Step 2
- 完成定义：后续协调器只依赖已实现的稳定接口；类型、代理 prepared API 和进程上下文互不反向依赖。
- Tasks（并行，文件所有权互斥）：
  - [ ] Task 3.1：在 `internal/app/types.go` 固化 `ConfigMutationInput/Receipt`、`RuntimeConfigStatus`、`ProcessRuntimeInfo` 与 error codes；在 `internal/webadmin` 只增加对应 envelope/HTTP 映射测试。
    - Receipt 至少包含 `persistedRevision`、`appliedRevision`、`applyMode`、`state`、`changedSections`、`pendingRestartFields`、`appliedAt`、`errorCode`；Overview 暴露脱敏 runtime status。
    - `ConfigApplyState` 明确包含 `synchronized`、`not_running`、`not_applicable`、`persisted_only`、`pending_restart`、`degraded`；短 CLI 只报告本进程 `persisted_only`，不得猜测其他进程。`ProcessRuntimeInfo` 包含 mode（desktop/server/tui/cli_serve/cli_command）、是否启用 watcher、active Admin baseline；只有 server mode 计算 Admin L3 diff。
    - `ConfigExportView`、Overview 和所有可编辑表单的加载结果携带 base revision；输入 DTO 对 Step 1.3 列出的操作提供 required `expectedRevision`。
    - 冻结 Service mutation 方法、Web `appService` 接口和 Wails input DTO 的 expected revision 字段，使 Step 4/5 的实现与 transport 可独立编码，不在同一步临时发明签名。
    - 验证：`go test ./internal/webadmin -run "ConfigRevision|ConfigError|RuntimeStatus" -count=1`。
  - [ ] Task 3.2：修改 `internal/proxy/server.go` 并独立新增 `server_runtime_test.go`，实现 `PrepareConfig`、`PreparedConfig.Apply/Discard`、runtime revision 与 Provider generation；不得修改 listener 测试文件。
    - 引入共享 `RuntimeResources`（routing store、generation allocator、Provider latency cache、trace、计数器）；L1 Server 与 L2 新旧 instance 必须引用同一对象。
    - `PrepareConfig` 接收 active generation map，clone/Validate 并构建 transport/client/policy；匹配 provider ID+fingerprint 时复用 generation，否则从共享 allocator 分配 incarnation；`Apply` atomic swap，`Discard` 幂等关闭候选资源。
    - 同步把 `request_read_timeout_ms` 从 listener 级 `http.Server.ReadTimeout` 下沉到 handler：固定 `ReadTimeout=0`、保留 `ReadHeaderTimeout=10s`，捕获 runtime 后用 `http.NewResponseController(w).SetReadDeadline` 应用并在读取结束后清除。
    - request trace/debug 记录捕获的 revision，不记录秘密。
    - 验证：`go test ./internal/proxy -run "PrepareConfig|ApplyPrepared|DiscardPrepared|RuntimeRevision|ProviderGeneration" -count=1`。
  - [ ] Task 3.3：在 `internal/proxy/listener.go` 与独立 `listener_test.go` 中实现 `PrepareListener(address)`、`ServeListener`、ready 信号和异步 drain 原语；不得修改 `server.go/server_runtime_test.go`。
    - 预绑定失败不得影响当前 listener；Windows wildcard/具体地址重叠无法并行绑定时返回 `proxy_rebind_preflight_failed`，禁止先停旧 listener 再尝试。
    - 验证：`go test ./internal/proxy -run "PrepareListener|ServeListener|DrainListener|RebindPreflight" -count=1`。
  - [ ] Task 3.4：新增 `internal/configmutation/**` 及独立测试，提供 App bootstrap 和传统 CLI 共用的 ConfigStore codec/facade。
    - 基础 facade 提供 Snapshot、CAS mutation、replace、bootstrap，不依赖 Service/runtime Apply；短 CLI 使用后关闭并返回 `persisted_only`。
    - `internal/configmutation/lifecycle.go` 提供 App/CLI 共用的 `PreviewLifecycle`、`BuildLifecycleCandidate`、`ExecutePersistedLifecycle`：token 签名覆盖 base revision、operation、choices、issued-at、expiry；Build 验证 token/expiry 并重建 candidate，App 将 candidate 交给运行时协调器，CLI 走 persisted-only execute，禁止复制 planner/token 协议。
    - 验证：`go test ./internal/configmutation -run "Snapshot|Mutate|Replace|Bootstrap|Conflict|Lifecycle|PlanExpired" -count=1`。

### - [ ] Step 4：建立统一协调器与共享 mutation 边界

- 依赖：Step 3
- 完成定义：Service 具有唯一 mutation/reconcile 管线和固定锁序；HTTP 使用 Step 3 冻结的接口传递 expected revision；CLI 具有不依赖 Service 的共享 ConfigStore facade。
- Tasks（并行，文件所有权互斥，只使用 Step 3 接口）：
  - [ ] Task 4.1：新增 `internal/app/config_runtime.go`、独立 `config_runtime_test.go` 并修改 `internal/app/config_store.go`、`service.go` 完成协调器注入。
    - `Service` 持有 ConfigStore、`configApplyMu`、atomic runtime status 和 close-once；`loadConfig` 走 Snapshot。
    - 协调器组合 Step 3.4 facade；`mutateConfig` 串行执行 classify、Prepare、persist、Apply；`reconcileSnapshot` 供 watcher 使用。
    - 建立独立 `proxy_rebind.go`、`config_watcher.go` no-op 扩展槽和 closer 注册机制；Step 6 只修改各自扩展文件，无需再改协调器/Service。
    - 固定锁序为 `configApplyMu -> ConfigStore path lock -> Service.mu`；持有 `Service.mu` 时不得执行文件 I/O、网络探测或等待 goroutine。
    - 验证：`go test ./internal/app -run "ConfigRuntime|MutationReceipt|RevisionConflict|ProxyLifecycleRace|ServiceClose" -count=1`。
  - [ ] Task 4.2：仅修改 `internal/webadmin/**`，按 Step 3 已冻结的 `appService` 接口和 DTO 接入 expected revision 与结构化回执。
    - 409/400/500 保持现有 envelope 兼容并附结构化 error code；旧客户端缺少 expected revision 时，替换/编辑操作返回明确 `config_revision_required`，不得默认为最新 revision。
    - 验证：`go test ./internal/webadmin -run "ExpectedRevision|RevisionRequired|RevisionConflict|MutationReceipt" -count=1`。

### - [ ] Step 5：迁移全部生产配置写入口

- 依赖：Step 4
- 完成定义：除 `Config.Save` 自身和测试 fixture 外，生产代码不再直接调用 `cfg.Save()`；现有 `commitConfig/commitConfigReplace` 粗粒度 hooks 与 lifecycle NoOp reload 均无生产调用，所有协作写入经过统一 revision/CAS 协调器或共享 facade。
- Tasks（并行，文件所有权互斥）：
  - [ ] Task 5.1：迁移 `internal/app/manage.go` 的全部现有 `commitConfig/commitConfigReplace` 路径并独立新增 `manage_hot_reload_test.go`；不得修改其他 Step 5 测试文件。
    - Provider 远端模型发现/探测在进入 path lock 前完成，并携带观察前 revision；mutation closure 只修改候选 Config；NoOp 不写盘、不 rebuild。所有 upsert/reorder/priority 使用调用方 expected revision。
    - 删除旧 `RemoveProviderWithPlan(id, selections)` 便利接口；Provider/Alias remove 与 Alias upgrade 的 transport 调用统一使用 `PreviewLifecycle` 返回的 revision/plan token/choices 再调用 `ExecuteLifecycle`，无 token 的兼容调用返回 `plan_required`。
    - `ImportProviders(overwrite=true)` 在 Service、HTTP、Wails、CLI 全部 transport 都必须接收导入预览时的 expected revision，不只约束 CLI。
    - 验证：`go test ./internal/app -run "Provider.*HotReload|Alias.*HotReload|RewriteRule.*HotReload|NoOp" -count=1`。
  - [ ] Task 5.2：迁移 `internal/app/lifecycle_api.go` 并独立新增 `lifecycle_hot_reload_test.go`；不得修改 manage/settings 测试文件。
    - App API 委托 Step 3.4 共用 lifecycle 层构建 candidate，再交给运行时协调器；NoOp 不 reload；冲突后 token 失效并重新 preview。测试覆盖签名 expiry/issued-at、过期 token 不因重新 Preview 获得新期限。
    - 验证：`go test ./internal/app -run "Lifecycle.*Revision|Lifecycle.*NoOp|Lifecycle.*PlanConflict|Lifecycle.*PlanExpired" -count=1`。
  - [ ] Task 5.3：在 `internal/app/service.go` 迁移 `SaveProxySettings` 与 `SaveDesktopPrefs`，独立新增 `settings_hot_reload_test.go`。
    - Proxy Settings 返回 receipt，并使用 Step 3.2 已实现的动态 request-read deadline；本 task 不修改 `internal/proxy/**`。
    - Desktop Prefs 经 ConfigStore 持久化，但 classify 为 L0，只执行现有托盘/通知/自启动副作用。
    - 验证：`go test ./internal/app ./internal/proxy -run "SaveProxySettings|SaveDesktopPrefs|RequestReadTimeout|TimeoutHotReload" -count=1`。
  - [ ] Task 5.4：迁移 `internal/app/config_transfer.go` 并独立新增 `config_transfer_hot_reload_test.go`。
    - 导入解析、八字段合并、diagnostics、Validate 均在提交前；完整替换必须使用用户加载文本时的 expected revision；冲突保留文本。
    - 删除“previous in-memory config”固定 warning；兼容 warnings 只能由 receipt 的 L2/L3/degraded 状态派生。
    - 验证：`go test ./internal/app -run "ImportConfig.*HotReload|ImportConfig.*Revision|ImportConfig.*PendingRestart" -count=1`。
  - [ ] Task 5.5：迁移 `internal/cli/provider.go`、`alias.go`、`rewrite.go` 共 11 个已使用短生命周期 Service 的入口，并独立新增 `config_mutation_test.go`。
    - 改用轻量 facade，避免初始化 runtime；输出 persisted revision、`persisted_only` 和结构化冲突。Provider/Alias remove、Alias upgrade 保留 lifecycle Preview/Execute；`ImportProviders(overwrite=true)` 与远端观察提交携带观察前 expected revision。
    - 验证：`go test ./internal/cli -run "Provider|Alias|Rewrite|Revision|Conflict" -count=1`。
  - [ ] Task 5.6：仅修改 `internal/desktop/bindings.go` 及对应测试，按 Step 3 冻结的 DTO 透传 expected revision、receipt 和结构化 error code。
    - 不修改 `internal/desktop/app.go` 或 Service 实现；验证：`go test ./internal/desktop -run "ExpectedRevision|RevisionRequired|MutationReceipt" -count=1`。

### - [ ] Step 6：实现 L2 重绑、外部文件收敛与 Admin L3

- 依赖：Step 5
- 完成定义：进程内写入和非协作外部文件最终通过同一协调器发布；代理地址可安全重绑；Admin 差异只在正确模式下报告。
- Tasks（并行，文件所有权互斥）：
  - [ ] Task 6.1：只替换 `internal/app/proxy_rebind.go` no-op 扩展并独立新增 `proxy_rebind_test.go`，接入 prepared listener 与 `proxyInstance`；不得修改协调器、watcher、server、desktop 或 TUI 文件。
    - 地址变化时持久化前准备“候选 runtime + 独立 proxy instance + 预绑定 listener”；候选接收 active generation map，新旧 instance 共享完整 `RuntimeResources`。
    - 提交后启动候选并确认 ready；失败则关闭候选、旧 instance 不动并置 degraded。ready 后停止旧 listener 接受新连接，再切换 active instance/status。同步等待 drain 最多 5 秒；超时不得 `Close` active connection，旧 instance 转入 retired 列表并在 active count 归零后异步释放，SSE/流式请求可继续完成。
    - L1 保持 `StartedAt`，L2 更新 `StartedAt`；trace、计数器和共享状态不因 listener 切换丢失。
    - 验证：`go test ./internal/app -run "ManagedRebind|RebindPreflight|DrainInFlight|RebindDegraded" -count=1`。
  - [ ] Task 6.2：只替换 `internal/app/config_watcher.go` no-op 扩展并独立新增 `config_watcher_test.go`，按需更新 `go.mod/go.sum` 引入 `fsnotify`；不得修改协调器或 rebind 文件。
    - 监听父目录并按 basename 过滤 Create/Write/Rename/Remove，150ms 去抖；自身保存按 revision 去重。
    - watcher 先取得 `configApplyMu`，读取 Snapshot、Decode/Validate/Prepare，并在发布前再次 Snapshot；已观察或已入队的 revisions 严禁倒序发布。最后一次检查后仍可能发生未协作写入，不承诺该窗口绝对同步，由后续事件最终收敛最后观察 revision。
    - typed decode error 的 raw revision 用于报告非法/半写文件的 persisted revision；保持最后可用 runtime 并置 degraded，不暴露文件内容。
    - 验证：`go test ./internal/app -run "ConfigWatcher|AtomicReplace|WatcherInvalid|WatcherBurst|WatcherPublishRace" -count=1`。
  - [ ] Task 6.3：修改 `internal/server/server.go` 及测试，迁移 `ensureAdminConfig` bootstrap 并实现 active Admin baseline。
    - Bootstrap 在 Service/watcher 前通过 ConfigStore 提交 key/CLI overrides 并建立 revision；随后把实际 listener/auth/publicBaseURL 作为 immutable baseline 注入 Service。
    - 运行中 Admin 字段变化只更新 persisted revision 和 `pendingRestartFields`，不替换当前 listener/auth/session token；日志只记录字段路径。
    - Server 退出调用 `Service.Close()`；验证 Desktop/TUI 模式不会产生 Admin pending restart。
    - 验证：`go test ./internal/server -run "AdminBootstrapRevision|AdminPendingRestart|AdminAuthStable|AdminSecretRedacted" -count=1`。
  - [ ] Task 6.4：仅修改 `internal/desktop/app.go`、Desktop shutdown 接线与 `internal/tui/**`，传入各自 `ProcessRuntimeInfo` 并保证退出调用 `Service.Close()`。
    - Desktop/TUI 不设置 Admin baseline；Desktop 使用应用 shutdown/defer，TUI `Run` 使用 defer。验证：`go test ./internal/desktop ./internal/tui -run "ProcessRuntimeInfo|ServiceClose|AdminPendingRestart" -count=1`。
  - [ ] Task 6.5：仅修改 `internal/cli/serve.go` 并独立新增 `serve_lifecycle_test.go`，把 `ocswitch serve` 标记为 `cli_serve` 长驻模式、启用 watcher，并在信号/错误退出时调用 `Service.Close()`。
    - 不修改 provider/alias/rewrite 命令文件；验证：`go test ./internal/cli -run "ServeWatcher|ServeServiceClose|ServeExternalReload" -count=1`。

### - [ ] Step 7：同步前端契约与双语资源

- 依赖：Step 6
- 完成定义：前端能够基于结构化 revision/状态编译，所有将要展示的状态均有 en/zh-CN 同构翻译；本 step 不修改视图行为。
- Tasks（并行，文件所有权互斥）：
  - [ ] Task 7.1：更新 `frontend/src/types.ts`、`frontend/src/api.ts` 并使用项目命令重新生成 Wails bindings。
    - 同步 receipt、runtime status、Overview、expected revision inputs 和 error codes；409 映射为类型化 conflict error，不匹配英文字符串。
    - 生成前检查并保留工作区已有的合法用户变更，禁止手工拼改生成模型。
    - 验证：`npm run build`（`frontend/`）。
  - [ ] Task 7.2：更新 `frontend/src/i18n/locales/en.json`、`zh-CN.json` 与 key 一致性校验。
    - 至少包含 live applied、proxy rebound、proxy not running、runtime not applicable、pending restart、degraded、disk/applied revision、conflict、refresh/retry、external reload、revision required。
    - 删除或改写所有声称 Proxy Settings/timeout 必须重启的文案；新增 key 在两语言中集合完全一致。
    - 验证：运行项目 i18n key 校验和 `npm run build`。

### - [ ] Step 8：完成 UI、回归测试与运行时文档

- 依赖：Step 7
- 完成定义：用户能区分实时生效、内部重绑、等待服务重启和降级；关键并发/在途场景自动化覆盖；文档不夸大外部写保证。
- Tasks（并行，文件所有权互斥）：
  - [ ] Task 8.1：更新 `frontend/src/App.tsx` 与相关样式，实现紧凑、非嵌套的 runtime 状态行。
    - 显示本地化的实时生效/代理已重绑/代理未运行/不适用于运行时/等待服务重启/热更失败，以及 disk/applied revision 短前缀；不显示秘密。
    - Proxy Settings 成功不再提示重启；Import 按 apply mode/pending fields 展示；冲突保留编辑内容并提供刷新命令。
    - Provider/Alias/Target/Rewrite 沿用现有控件，不新增逐项重启按钮；360px、768px、1440px 无溢出、遮挡或布局跳动。
    - 验证：`npm run build` 和项目 UI screenshot audit。
  - [ ] Task 8.2：扩充 `internal/proxy`、`internal/app` 集成与故障注入测试。
    - 表驱动覆盖 Provider/Alias/Target/Rewrite/Auth/Timeout/Routing 的旧请求旧 revision、新请求新 revision；覆盖 SSE/流式请求与下一请求切换。
    - 覆盖 Validate/Build/Discard/AtomicWrite/Apply、CAS、watcher parse/publish race、L2 prebind；断言 artifact 无泄漏、状态代际隔离、磁盘/runtime status 正确。
    - 并发执行 Provider 编辑、Alias 排序、协作 CLI 写、外部原子替换和 Start/Stop；外部写只断言最终收敛，协作写断言无丢更新。
    - 验证：定向测试 `-count=10` 与 `go test -race ./internal/app ./internal/proxy`。
  - [ ] Task 8.3：更新 README 或 `docs/` 的运行时配置说明与日志字段参考。
    - 记录完整入口矩阵、L0-L3、expected revision、协作 CAS/外部最终一致边界、最后可用配置、Admin L3 和手动冒烟步骤。
    - 日志字段包含 `config_source`、`persisted_revision`、`applied_revision`、`apply_mode`、`pending_restart_fields`、`reload_error_code`，秘密必须脱敏。

### - [ ] Step 9：执行最终门禁与独立审查

- 依赖：Step 8
- 完成定义：构建、测试、UI 审计和独立 code review 均给出可追溯结论；发现问题必须回到对应 step 修复后重新执行本 step。
- Tasks（并行，仅读取/验证）：
  - [ ] Task 9.1：执行 Go 门禁：`go build ./...`、`go test ./...`、`go test -race ./internal/app ./internal/proxy`，记录失败命令与修复提交。
  - [ ] Task 9.2：执行前端门禁：`npm run build`、i18n key 校验，并在 360px、768px、1440px 完成截图和交互审计。
  - [ ] Task 9.3：由独立 reviewer 对照本方案审查生产 `cfg.Save()` 与旧 `commitConfig/commitConfigReplace` 调用零残留、锁序、artifact 所有权、revision 回执、generation 隔离、watcher 防旧版本回滚、Admin mode/baseline 和生命周期关闭。

---

## 3. goal（验收标准）

> 验收标准，至少包含 Code Review 与基础代码测试。

- [ ] **Code Review**：独立评审无阻断性问题；所有生产配置写入口均经过 ConfigStore 或明确的 OpenCode 外部流程，bootstrap 例外仍建立 revision；同 step 子任务文件所有权无冲突。
- [ ] **基础代码测试**：`go build ./...`、`go test ./...`、`go test -race ./internal/app ./internal/proxy`、前端 `npm run build` 和 i18n key 校验全部通过。
- [ ] **Provider/Alias/规则热更**：Provider、Alias、Target、Rewrite、Priority、Auto Alias 的增删改、启停、绑定和排序无需人工 Stop/Start；下一请求使用新 revision，在途请求保持旧 revision。
- [ ] **设置与导入**：Proxy timeout/failover/routing 保存后 L1 生效；完整导入的 L1 字段同步生效、代理地址完成 L2 重绑、Admin 字段仅在 Server Mode 精确列入 L3。
- [ ] **全入口一致性**：App 全部 manage/lifecycle 路径、Proxy Settings、Desktop Prefs、Import、传统 CLI 11 个入口与 Admin bootstrap 均不再绕过统一 revision 管线；生产代码无遗留 `cfg.Save()` 或旧粗粒度 `commitConfig/commitConfigReplace` 调用。
- [ ] **并发与冲突**：协作写入严格 CAS、无丢更新；整体替换/编辑类携带 expected revision，绝对意图按上限重放；非协作外部写只承诺最后观察 revision 最终收敛，UI 和文档无夸大表述。
- [ ] **故障安全**：NoOp 不 Build/Apply；取消、二次冲突、未提交/未知写盘结果均按所有权释放 prepared artifact；非法外部配置不替换最后可用 runtime。
- [ ] **状态隔离**：Provider generation 变化后旧在途请求回写无法污染新代际，不变连接身份保留状态，旧代际存储有界回收。
- [ ] **可观测性**：任何变更结果可区分 persisted/applied revision、apply mode、pending restart 和 degraded；日志与 UI 不泄露 API key。
- [ ] **生命周期**：Desktop、Server、TUI、`ocswitch serve` 退出均调用 `Service.Close()`；watcher、listener、transport 与 goroutine 无泄漏，在途 SSE 不被 5 秒 drain 超时强杀。
- [ ] **i18n 与 UI**：全部新增用户文案有 en/zh-CN 翻译；360px、768px、1440px 下状态与操作控件无溢出、遮挡或布局跳动。
