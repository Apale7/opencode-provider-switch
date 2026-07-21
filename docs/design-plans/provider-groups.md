# 详细设计方案：Provider 多分组与分组级协议/API Key

> 文件类型：详细设计方案（Detailed Design Plan）
> 状态：待实施
> 创建时间：2026-07-22

## 约束 / Constraints（不可修改）

1. **Agent 只能修改 `todolist` 区块与各 checkbox 状态（`- [ ]` ↔ `- [x]`）。**
   禁止编辑、改写、重排、新增或删除其他任何内容（steps 正文、tasks 描述、goal 文本、标题、本约束区块）。
2. **同一 step 内的 tasks 必须由多个子代理并行完成。** 一个 step 的全部 tasks 需在一批并行子代理调用中下发，禁止在主代理中逐个串行执行。
3. **steps 严格顺序执行。** 上一个 step 的 checkbox 勾选完成前，不得开始下一个 step。

---

## 1. todolist（可实时修改）

> Agent 当前正在实施的任务清单，可随进度实时增删改、勾选。

- [ ] Step 1：冻结配置契约与迁移夹具
- [ ] Step 2：实现配置模型与跨层基础契约
- [ ] Step 3：实现分组级路由与运行时隔离
- [ ] Step 4：改造模型发现、自动 Alias 与诊断
- [ ] Step 5：改造管理 API、Trace 与健康统计
- [ ] Step 6：接入桌面、Web、TUI 与 CLI 管理边界
- [ ] Step 7：生成并冻结前端跨层类型
- [ ] Step 8：实现 GUI/Web 管理体验与 i18n
- [ ] Step 9：删除临时兼容投影并收敛最终契约
- [ ] Step 10：完成分域回归修复
- [ ] Step 11：执行全量与手工验收门禁
- [ ] Step 12：完成独立评审

---

## 2. steps（顺序执行，存在依赖）

> 步骤之间存在依赖关系，只能顺序执行。每个 step 内部的 tasks 之间无依赖，由多个子代理并行完成。

### - [ ] Step 1：冻结配置契约与迁移夹具

- 依赖：无
- 完成定义：schema v1/v2、路由/Rewrite 和管理 DTO 的输入输出样本固定；本 Step 不创建引用尚未实现 Go 类型的测试源码。
- Tasks（并行，子代理执行）：
  - [ ] Task 1.1：仅在 `internal/config/testdata/provider_groups/` 增加 schema v1/v2 JSON golden inputs/outputs，覆盖单/多 Key、多 BaseURL、Alias Target、Rewrite selector、混合格式、空 Groups 与未知版本；不新增 Go 测试源码。
  - [ ] Task 1.2：仅在 `internal/proxy/testdata/provider_groups/` 增加 Alias 双 Group、同模型歧义、认证 Header、Key/熔断隔离、未列兄弟 Group 和 model-not-found 场景数据；不新增 Go 测试源码。
  - [ ] Task 1.3：仅在 `internal/app/testdata/provider_groups/` 增加 `ProviderGroupInput/View`、`apiKeysChanged`、掩码拒绝、明文不回显和两类 API Key 路径隔离的 JSON 样本；不新增 Go 测试源码。

### - [ ] Step 2：实现配置模型与跨层基础契约

- 依赖：Step 1
- 完成定义：配置新模型以加法方式落地并通过 deprecated `json:"-"` default 投影及单 default 写桥维持旧消费者行为；多 Group 旧写入明确拒绝；完整 App DTO Group 字段独立落地；合并后 `go test ./internal/config ./internal/configstore ./internal/app` 通过。
- Tasks（并行，子代理执行）：
  - [ ] Task 2.1：独占 `internal/config/` 的 Provider Group 生产代码与测试，并按 PRD 实现 schema v2、v1→v2 迁移、Target/Rewrite selector、严格校验、canonical 编码及 ConfigStore 门禁；临时保留 `json:"-"` default 投影和仅支持无 Group/单 default 的写桥，多 Group 旧写入必须拒绝。
  - [ ] Task 2.2：仅修改 `internal/app/provider_group_types.go`、`internal/app/types.go` 和独立 JSON shape 测试，按 PRD 一次性增加 `ProviderGroupInput/View`、`ProviderView.Groups`、Alias Target/Ref Group、Rewrite Group selector；保留现有 DTO 字段供迁移期编译，不调用 Task 2.1 helper。

### - [ ] Step 3：实现分组级路由与运行时隔离

- 依赖：Step 2
- 完成定义：代理只消费精确 `ResolvedTarget(provider, group, model)`；Base URL 由 Provider 提供，协议和 Key 由 Group 提供；两组之间不存在 Key、重试、熔断或 rewrite 状态污染。
- Tasks（并行，子代理执行）：
  - [ ] Task 3.1：仅修改 `internal/proxy/server.go` 及其测试，在 Alias 候选解析、`tryProviderBaseURLs`、`providerAPIKeyOptions` 和 `tryOnce` 中显式传递 `ProviderGroup`；未命中 Alias 返回 model-not-found，同 Provider 兄弟 Group 只有被 Target 明确列出时才可尝试。
  - [ ] Task 3.2：修改 `internal/routing/` 的候选与熔断状态键，使 Provider ID、Group ID、Model 形成稳定身份；为同 Provider 双 Group 的 401/429/5xx、重试和半开恢复增加隔离测试。
  - [ ] Task 3.3：仅修改 `internal/config/rewrite_ops.go` 及其测试，实现 PRD 冻结的 `provider_groups` 精确 selector、legacy 非空 providers→default、空 selector wildcard 和缺失 Group 不匹配语义；不修改 proxy 或 routing 文件。

### - [ ] Step 4：改造模型发现、自动 Alias 与诊断

- 依赖：Step 3
- 完成定义：所有探测和目录更新都绑定一个 Group；自动 Alias Target 使用完整三元组；删除/禁用 Group 后诊断准确且不会访问兄弟 Group 的协议或 Key。
- Tasks（并行，子代理执行）：
  - [ ] Task 4.1：修改 `internal/opencode/provider_models.go` 及调用方，使模型刷新、Base URL Ping 和能力探测输入包含 Group ID、Group 协议与 Group Keys，结果只写回目标 Group。
  - [ ] Task 4.2：独占 `internal/lifecycle/` 与 `Config.AutoGenerateAliases` 相关符号，实现自动 Target 的 `(provider, group, model)` 生成/去重/升级/清理，以及 Group 删除/ID 变更引用规划。
  - [ ] Task 4.3：仅修改 `internal/diagnostics/`，增加 Group entity/path/catalog 状态和缺失/禁用/协议不匹配诊断，验证诊断参数不包含明文 Key；不修改 lifecycle 或 config 文件。

### - [ ] Step 5：改造管理 API、Trace 与健康统计

- 依赖：Step 4
- 完成定义：管理服务完全改用 Group typed contract；Trace 持久化与健康聚合由同一所有者一起升级，避免健康任务等待同 Step 的 Trace 字段。
- Tasks（并行，子代理执行）：
  - [ ] Task 5.1：修改 `internal/app/manage.go` 与 `internal/app/service.go`，消费 Step 2 已存在的 `provider_group_types.go` 实现 Group CRUD/refresh/ping；列表只返回掩码 Key，且本 Task 不修改 `internal/app/types.go`。
  - [ ] Task 5.2：独占 `internal/proxy/traces.go`、`internal/proxy/sqlite_traces.go`、`internal/proxy/server.go` 的 Trace 写入/调试 Header 区块、`internal/app/types.go` 的 Trace 区块及 `internal/app/provider_health.go`，同时增加 Group trace 字段、实际 attempt/final Group 写入、Group 调试 Header、历史 default 适配和 Provider + Group + Model 健康聚合及测试。

### - [ ] Step 6：接入桌面、Web、TUI 与 CLI 管理边界

- 依赖：Step 5
- 完成定义：所有非前端管理适配器使用 Step 5 的同一 typed contract，且各 Task 修改互不重叠的入口目录。
- Tasks（并行，子代理执行）：
  - [ ] Task 6.1：仅修改 `internal/webadmin/`，增加 `/api/admin/providers/{providerID}/groups` CRUD/refresh/ping 路由及 Alias Group 字段，复用管理认证边界且不注册 `/api-keys` 路径。
  - [ ] Task 6.2：仅修改 `internal/desktop/` 与 Wails App 暴露方法，接入同一 Provider Group service contract；本 Task 不生成前端 bindings。
  - [ ] Task 6.3：仅修改 `internal/tui/` 与 `internal/cli/`，增加显式 Group 选择和管理；default 保持旧命令体验，非 default 使用独立 `--group` 参数；为新增 TUI 文案增加与现有 locale 机制一致的中英文 key/切换，并禁止硬编码单语言文本。

### - [ ] Step 7：生成并冻结前端跨层类型

- 依赖：Step 6
- 完成定义：Wails 与 HTTP 两条前端数据链路拥有一致的 Provider Group 字段和掩码语义，生成物稳定后才允许开始页面实现。
- Tasks（并行，子代理执行）：
  - [ ] Task 7.1：运行项目既有 Wails binding 生成命令，只更新 `frontend/wailsjs/go/*` 与对应声明，并验证 `ProviderGroupInput/View`、Target Group 和 `apiKeysChanged` 均存在。
  - [ ] Task 7.2：仅修改 `frontend/src/types.ts` 与 `frontend/src/api.ts`，为 HTTP fallback 定义与 Wails 等价的 Provider Group 类型和方法，不修改页面组件或生成目录。
  - [ ] Task 7.3：新增 `frontend/src/providerGroupUiContract.ts`，冻结 Step 8 唯一允许使用的 `providers.groups.*` i18n key 清单与 `provider-group-*` CSS class 清单；不修改页面、locale 或样式文件。

### - [ ] Step 8：实现 GUI/Web 管理体验与 i18n

- 依赖：Step 7
- 完成定义：桌面与 HTTP Web 共用的界面可管理 Group 和精确 Alias Target，Key 不回显，新增中英文文案完整；构建与浏览器验收留到后续独立 Step。
- Tasks（并行，子代理执行）：
  - [ ] Task 8.1：仅修改 `frontend/src/App.tsx` 及其已有组件文件，严格消费 `providerGroupUiContract.ts` 中的 key/class，完成 Provider 共享设置、Group 列表/详情、Group CRUD/refresh/ping，以及 Provider → Group → Model 的 Alias Target 选择。
  - [ ] Task 8.2：仅修改 `frontend/src/i18n/locales/en.json` 与 `zh-CN.json`，严格实现 `providerGroupUiContract.ts` 的 key 清单及 Upstream keys/上游密钥文案，并校验两份 locale key 集一致。
  - [ ] Task 8.3：仅修改 `frontend/src/styles.css`，严格实现 `providerGroupUiContract.ts` 的 class 清单及 Group 分栏、Key 编辑、Alias 三级选择和窄屏布局，不修改 React 或 locale 文件。

### - [ ] Step 9：删除临时兼容投影并收敛最终契约

- 依赖：Step 8
- 完成定义：所有生产消费者已经使用 Group；config 与 app DTO 中 deprecated Provider 顶层协议/Key/模型、legacy Rewrite Providers 及同步投影全部删除；各 Task 按 PRD 最终契约独立修改不重叠文件。
- Tasks（并行，子代理执行）：
  - [ ] Task 9.1：仅修改 `internal/config/`，删除 deprecated Provider 顶层投影、同步代码及 runtime Rewrite `Providers` 字段，确认 legacy 字段只存在私有 v1 wire DTO 并运行 config/configstore 测试。
  - [ ] Task 9.2：仅修改 `internal/app/types.go`、`provider_group_types.go` 和 app service/manage 调用点，删除 deprecated ProviderView 与 Rewrite View/Input 顶层字段，确保最终 DTO 只暴露 Group 契约。
  - [ ] Task 9.3：仅修改 `internal/proxy/`、`internal/opencode/` 与 `internal/routing/` 的残余调用点，按 PRD 最终 config API 消除任何 deprecated Provider 字段访问。
  - [ ] Task 9.4：仅修改 `internal/webadmin/`、`internal/desktop/`、`internal/tui/`、`internal/cli/` 与 `frontend/` 的残余调用点，消除 deprecated App DTO 字段访问。

### - [ ] Step 10：完成分域回归修复

- 依赖：Step 9
- 完成定义：各领域定向测试在互不重叠的文件范围内通过；发现的问题由对应领域 Task 修复并重新运行该领域门禁；静态搜索确认生产代码无 deprecated 投影引用。
- Tasks（并行，子代理执行）：
  - [ ] Task 10.1：负责 config/configstore/lifecycle/diagnostics 域，运行对应 package tests，修复本域失败并验证 legacy revision、备份、schema 与 rewrite selector 矩阵。
  - [ ] Task 10.2：负责 proxy/routing/opencode 域，运行对应 package tests，修复本域失败并验证 Alias-only、双 Group Key、协议、重试和熔断隔离。
  - [ ] Task 10.3：负责 app/webadmin/desktop 域，运行对应 package tests，修复本域失败并验证管理响应无明文与两类 API Key 隔离。
  - [ ] Task 10.4：负责 frontend/TUI/CLI 域，先重新生成最终 Wails bindings 并确认无 deprecated 字段，再运行定向测试和 TypeScript 检查；修复本域失败并验证 Group 参数、GUI locale key、TUI 中英文 key/切换与 UI contract。

### - [ ] Step 11：执行全量与手工验收门禁

- 依赖：Step 10
- 完成定义：所有 Task 只执行稳定代码上的验收并记录结果；任何门禁失败则本 Step 保持未完成，在 todolist 增加修复项后重新执行 Step 10 和本 Step。
- Tasks（并行，子代理执行）：
  - [ ] Task 11.1：运行 `go test ./...`，并搜索生产代码确认 deprecated 投影、顶层 Provider 协议/Key/模型及 runtime Rewrite Providers 均已删除；不在本 Task 内修改文件。
  - [ ] Task 11.2：在 `frontend` 运行 `npm run build`，并以桌面/移动视口审计桌面与 HTTP Web 的 Provider Group、Alias 和中英文布局，同时确认 TUI 新增 Group 操作可切换中英文；不在本 Task 内修改文件。
  - [ ] Task 11.3：使用临时配置副本手工验证 legacy 加载不改盘/revision、首次 v2 保存备份、二次 round-trip、双协议路由、Key 重试和禁止 Group fallback；不在本 Task 内修改文件。

### - [ ] Step 12：完成独立评审

- 依赖：Step 11
- 完成定义：三项独立审查均无高/中严重度问题；若有发现，在 todolist 新增回到对应 Step 的修复与重验项，修复后重新执行 Step 10、Step 11 和本 Step。
- Tasks（并行，子代理执行）：
  - [ ] Task 12.1：由 result-reviewer 对照 PRD 审查完整 diff、测试结果、生成文件和无关改动。
  - [ ] Task 12.2：由独立安全审查聚焦明文 Key 泄漏、掩码持久化、认证 Header 覆盖、fail-closed Group 解析和两类 API Key 边界。
  - [ ] Task 12.3：由独立兼容性审查聚焦 schema v1/v2、ConfigStore revision、备份失败、历史 Trace、临时投影清理和 legacy 行为等价性。

---

## 3. goal（验收标准）

> 验收标准，至少包含 code review 与基础代码测试。

- [ ] **Code Review**：独立评审对照 PRD 和冻结契约完成，配置迁移、路由隔离、密钥保密与引用完整性无阻断性问题。
- [ ] **基础代码测试**：`go test ./...` 与 `frontend` 的 `npm run build` 全部通过，Wails bindings 与 TypeScript 类型一致。
- [ ] **向后兼容**：legacy golden config 加载后文件字节、修改时间和 ConfigStore revision 不变；内存中唯一 `default` Group 与旧行为等价；首次保存先备份再写 v2，备份失败不覆盖原文件，二次 round-trip 幂等。
- [ ] **路由正确性**：Alias Target 精确路由到指定 Group；禁用、缺失、协议不匹配 Group 均 fail closed，不回退其他 Group。
- [ ] **状态隔离**：同 Provider 多 Group 的 Key 轮换、重试、熔断、模型发现、Trace 与健康统计互不污染。
- [ ] **管理体验**：桌面 GUI、HTTP Web UI、TUI/CLI 均可管理 Group 和绑定 Alias Target；新增界面中英文完整，桌面与移动视口无重叠。
- [ ] **密钥边界**：Provider Group 管理响应无明文 Key、掩码不可持久化、`apiKeysChanged` 三态行为正确，且上游密钥类型/路由/UI 不复用客户端代理 API Key。
