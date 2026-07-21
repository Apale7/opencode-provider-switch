# 详细设计方案：配置引用完整性与用户体验瑕疵梳理

> **方案状态**：待执行
> **创建日期**：2026-07-21
> **关联任务**：07-21-config-ref-integrity — 审计配置引用图，消除悬空引用，统一 UX 反馈

---

## 约束 / Constraints

> **执行本方案时，Agent 必须遵守以下硬性约束：**

1. **Agent 只能修改 `todolist` 区域和 checkbox 状态（`- [ ]` ↔ `- [x]`）。** 不得编辑、改写、重排、新增或删除任何其他内容（步骤正文、任务描述、goal 文本、标题、本约束块）。
2. **同一 step 内的 task 必须由多个 subagent 并行执行。** 一次性派发该 step 的全部 task 到并行 subagent，不得在主 agent 中逐个串行执行。
3. **Step 严格按顺序执行。** 上一个 step 的 checkbox 未勾选前，不得开始下一个 step。

---

## todolist

> Agent 的实时工作清单。仅此区域可在执行中自由修改。

- [ ] Step 1: 引用关系审计 — 建立引用图与校验注册表
- [ ] Step 2: Config 层引用完整性 — 级联清理 + Save 校验
- [ ] Step 3: App Service 层 — CRUD 生命周期集成
- [ ] Step 4: Proxy 运行时降级 — 悬空引用优雅处理
- [ ] Step 5: 前端 UI — 悬空引用警告与修复入口
- [ ] Step 6: i18n 文案补齐
- [ ] Step 7: Goal — 代码审查与测试验证

---

## steps

### Step 1: 引用关系审计 — 建立引用图与校验注册表

- [ ] **Step 1 完成**

**完成定义**：产出完整的引用关系矩阵文档（嵌入本设计文档附录或独立 reference-graph.md），标注所有实体类型、引用字段、引用强度、清理策略。

#### Task 1.1 — 实体类型枚举与引用字段提取

- **产出文件**：本设计文档附录 A「引用关系矩阵」
- **内容要求**：
  - 枚举所有配置实体：`Provider`、`Alias`、`Target`、`APIKey`、`RewriteRule`、`ProviderPriority`、`CircuitBreakerState`、`ServerConfig`
  - 对每个实体的每个字段，标注是否为引用型字段（引用其他实体 ID/Name/Model）
  - 标注引用方向（A → B），记录被引用方是否有反向索引
  - 产出引用矩阵表

#### Task 1.2 — 引用强度与容忍度标注

- **产出文件**：本设计文档附录 A（同一文档）
- **内容要求**：
  - 对每个引用关系标注强度：
    - **Strong**：缺失引用会导致功能不可用（如 Alias.Target → Provider）
    - **Weak**：缺失引用可降级处理（如 ProviderPriority 中已删除的 ID）
  - 对每个引用关系标注清理策略：
    - `cascade-delete`：删除被引用实体时自动清理
    - `warn-preserve`：删除被引用实体时保留但警告
    - `block-delete`：存在引用时阻止删除

#### Task 1.3 — 边界校验矩阵

- **产出文件**：本设计文档附录 B「校验边界矩阵」
- **内容要求**：
  - 列出所有可能产生引用完整性问题的操作边界：
    - `Config.Save()`（Wails bridge）
    - HTTP REST Admin API
    - CLI TUI
    - Config 文件导入
    - OpenCode sync 输出
    - 系统启动 config 加载
  - 标注当前各边界的校验行为（有/无/部分）
  - 标注目标统一行为

---

### Step 2: Config 层引用完整性 — 级联清理 + Save 校验

- [ ] **Step 2 完成**

**完成定义**：`internal/config/config.go` 新增引用完整性方法：级联清理方法和 Save 时跨实体引用校验；所有新增代码通过编译和单元测试。

#### Task 2.1 — 引用校验注册表

- **产出文件**：`internal/config/config.go`
- **内容要求**：
  - 新增 `type RefIntegrityIssue struct { Entity, Field, RefKind, RefValue, Severity string }`
  - 新增 `func (c *Config) ValidateReferences() []RefIntegrityIssue`：
    - 遍历所有 alias targets，校验 Provider 存在性
    - 遍历所有 API Key scopes，校验 Provider/Alias 存在性
    - 遍历 ProviderPriority，校验 Provider 存在性
    - 遍历 Rewrite Rules，校验引用实体存在性
    - 遍历所有 alias targets，校验 Protocol 一致性（warning 级别）
  - 返回完整的 issues 列表
- **验证**：`go build ./internal/config/` 通过

#### Task 2.2 — Save 时自动校验

- **产出文件**：`internal/config/config.go`
- **修改位置**：`func (c *Config) Save()` 或新增 `SaveWithValidation()`
- **新增逻辑**：
  - Save 前调用 `ValidateReferences()`
  - 存在 `severity=error` 的 issue → 拒绝保存，返回错误列表
  - 存在 `severity=warning` 的 issue → 允许保存，但记录日志
  - `severity=error` 的标准：Strong 引用指向不存在的实体
  - `severity=warning` 的标准：Weak 引用或可降级的 Strong 引用
- **验证**：`go build ./internal/config/` 通过

#### Task 2.3 — 级联清理方法补充

- **产出文件**：`internal/config/config.go`
- **新增方法**（补充 auto-alias-simplify 已有的 `RemoveProviderAutoTargets`）：
  - `RemoveProviderFromPriority(id string)` — 从 ProviderPriority 移除
  - `RemoveProviderFromAPIKeyScopes(id string) (affectedKeys []string)` — 从所有 API Key 的 provider scope 移除
  - `RemoveAliasFromAPIKeyScopes(name string) (affectedKeys []string)` — 从所有 API Key 的 alias scope 移除
  - `CleanDanglingRefs() (cleaned []RefIntegrityIssue)` — 批量清理所有已知悬空引用，返回清理结果
- **验证**：`go build ./internal/config/` 通过

#### Task 2.4 — Config 启动时引用扫描

- **产出文件**：`internal/config/config.go`
- **新增方法**：
  - `func (c *Config) StartupIntegrityCheck() []RefIntegrityIssue` — 在 `Load()` 后调用，扫描并报告所有悬空引用
- **行为**：仅报告，不自动修复（避免加载时意外修改文件）
- **验证**：`go build ./internal/config/` 通过

#### Task 2.5 — Config 单元测试

- **产出文件**：`internal/config/config_test.go`（追加）
- **测试用例**：
  - `TestValidateReferences_AllClean`：完整配置无 issue
  - `TestValidateReferences_DanglingProviderTarget`：alias target 指向不存在的 provider
  - `TestValidateReferences_DanglingAPIKeyProviderScope`：API Key scope 含不存在的 provider
  - `TestValidateReferences_DanglingAPIKeyAliasScope`：API Key scope 含不存在的 alias
  - `TestValidateReferences_DanglingProviderPriority`：priority 列表含不存在的 provider
  - `TestValidateReferences_ProtocolMismatch`：alias protocol 与 target provider protocol 不一致
  - `TestSaveWithValidation_RejectsDanglingStrongRef`：Save 拒绝 error 级 issue
  - `TestSaveWithValidation_AllowsWarning`：Save 允许 warning 级 issue
  - `TestRemoveProviderFromPriority`：删除 provider 后 priority 列表清理
  - `TestRemoveProviderFromAPIKeyScopes`：级联清理 API Key scope
  - `TestRemoveAliasFromAPIKeyScopes`：级联清理 alias scope
  - `TestStartupIntegrityCheck`：启动扫描报告悬空引用
  - `TestCleanDanglingRefs`：批量清理
- **验证**：`go test ./internal/config/ -run "ValidateReferences|SaveWithValidation|RemoveProvider|RemoveAlias|StartupIntegrity|CleanDangling" -v` 全部通过

---

### Step 3: App Service 层 — CRUD 生命周期集成

- [ ] **Step 3 完成**

**完成定义**：`internal/app/manage.go` 中所有 CRUD 操作集成引用完整性校验和级联清理；Service 层 API 返回结构化警告信息。

#### Task 3.1 — RemoveProvider 完整级联

- **产出文件**：`internal/app/manage.go`
- **修改位置**：`RemoveProvider` 函数
- **新增逻辑**（在 `cfg.RemoveProvider(id)` 后，`cfg.Save()` 前）：
  ```go
  // 1. 清理自动 alias targets（已有 RemoveProviderAutoTargets）
  emptied := cfg.RemoveProviderAutoTargets(id)

  // 2. 清理 ProviderPriority
  cfg.RemoveProviderFromPriority(id)

  // 3. 清理 API Key scopes
  affectedAPIKeys := cfg.RemoveProviderFromAPIKeyScopes(id)

  // 4. 收集手动 alias targets（仅警告，不删除）
  manualTargets := cfg.FindManualTargetsForProvider(id)

  // 5. 输出结构化警告
  warnings := buildRemoveWarnings(emptied, affectedAPIKeys, manualTargets)
  ```
- **验证**：`go build ./internal/app/` 通过

#### Task 3.2 — RemoveAlias 级联

- **产出文件**：`internal/app/manage.go`
- **修改位置**：`RemoveAlias` 函数
- **新增逻辑**：
  - 清理 API Key scope 中对该 alias 的引用
  - 若为自动 alias，记录删除原因
- **验证**：`go build ./internal/app/` 通过

#### Task 3.3 — UpsertProvider / UpsertAlias / CreateAPIKey 保存校验

- **产出文件**：`internal/app/manage.go`
- **修改位置**：各 Upsert/Create 函数
- **新增逻辑**：
  - 在 `cfg.Save()` 前调用 `cfg.ValidateReferences()`
  - 存在 error 级 issue → 返回错误给调用方（API 返回 400）
  - 存在 warning 级 issue → 保存但附带 warnings
- **验证**：`go build ./internal/app/` 通过

#### Task 3.4 — 类型扩展

- **产出文件**：`internal/app/types.go`
- **新增类型**：
  ```go
  type RefIntegrityWarning struct {
      EntityType string `json:"entityType"` // "provider", "alias", "apikey"
      EntityID   string `json:"entityId"`
      Issue      string `json:"issue"`       // human-readable description
      Severity   string `json:"severity"`    // "error", "warning"
      FixAction  string `json:"fixAction"`   // "auto_cleaned", "manual_review"
  }

  type IntegrityCheckResult struct {
      Passed   bool                  `json:"passed"`
      Warnings []RefIntegrityWarning `json:"warnings"`
  }
  ```
- **验证**：`go build ./internal/app/` 通过

#### Task 3.5 — 新增诊断 API

- **产出文件**：`internal/app/manage.go`、`internal/app/types.go`
- **新增 API**：
  - `CheckIntegrity(ctx) (IntegrityCheckResult, error)` — 运行完整引用完整性扫描并返回结果
  - `CleanDanglingRefs(ctx) (IntegrityCheckResult, error)` — 自动清理所有可安全清理的悬空引用
- **验证**：`go build ./internal/app/` 通过

#### Task 3.6 — Service 层单元测试

- **产出文件**：`internal/app/service_test.go`（追加）
- **测试用例**：
  - `TestRemoveProvider_CascadesToAutoTargets`：级联清理 auto alias
  - `TestRemoveProvider_PreservesManualTargetsWithWarning`：保留手动 target + 警告
  - `TestRemoveProvider_CleansProviderPriority`：清理优先级列表
  - `TestRemoveProvider_CleansAPIKeyScopes`：清理 API Key scope
  - `TestRemoveAlias_CleansAPIKeyScopes`：alias 删除级联
  - `TestUpsertProvider_RejectsInvalidRefs`：保存时拒绝无效引用
  - `TestCreateAPIKey_RejectsUnknownProvider`：拒绝引用不存在的 provider
  - `TestCreateAPIKey_RejectsUnknownAlias`：拒绝引用不存在的 alias
  - `TestCheckIntegrity_ReturnsAllIssues`：诊断 API 返回完整结果
- **验证**：`go test ./internal/app/ -run "Cascade|RejectsUnknown|CheckIntegrity" -v` 全部通过

---

### Step 4: Proxy 运行时降级 — 悬空引用优雅处理

- [ ] **Step 4 完成**

**完成定义**：`internal/proxy/server.go` 在路由过程中遇到悬空 target 时跳过并记录 warning 日志，而非报 500；所有路径通过单元测试。

#### Task 4.1 — AvailableTargets 悬空过滤

- **产出文件**：`internal/proxy/server.go`（或 `internal/routing/`）
- **修改位置**：`AvailableTargets` 或路由选择逻辑
- **新增逻辑**：
  - 在筛选可用 target 时，过滤掉 provider ID 不存在的 target
  - 记录结构化警告日志：`dangling_ref=true ref_type=provider ref_target=<id>`
  - 若过滤后无可用 target，返回明确错误（含"alias 所有 target 的 provider 均已失效"提示）
- **验证**：`go build ./internal/proxy/` 通过

#### Task 4.2 — Provider 查找容错

- **产出文件**：`internal/proxy/server.go`
- **修改位置**：所有 `FindProvider(id)` 调用点
- **新增逻辑**：
  - 统一包装为 `FindProviderSafe(id)` 或检查返回值
  - Provider 不存在时记录日志并返回可用错误（非 panic/500）
- **验证**：`go build ./internal/proxy/` 通过

#### Task 4.3 — Proxy 单元测试

- **产出文件**：`internal/proxy/server_test.go`（追加）
- **测试用例**：
  - `TestRouteRequest_SkipsDanglingTarget`：路由跳过 provider 已删除的 target
  - `TestRouteRequest_AllTargetsDangling`：所有 target 悬空时返回明确错误
  - `TestRouteRequest_DanglingRefLogged`：悬空引用产生结构化日志
  - `TestModelsEndpoint_FiltersDanglingAliases`：`/models` 过滤不可用 alias
- **验证**：`go test ./internal/proxy/ -run "DanglingTarget|DanglingRef|FiltersDangling" -v` 全部通过

---

### Step 5: 前端 UI — 悬空引用警告与修复入口

- [ ] **Step 5 完成**

**完成定义**：前端在 Provider/Alias/API Key 详情页展示悬空引用警告；提供"一键修复"入口；所有新增 UI 通过类型检查。

#### Task 5.1 — 悬空引用警告组件

- **产出文件**：`frontend/src/App.tsx`（新增组件或区域）
- **功能要求**：
  - 创建通用 `<IntegrityWarningBanner>` 组件
  - 接收 `warnings: RefIntegrityWarning[]`，按 severity 渲染不同样式（error=红色, warning=黄色）
  - 每条 warning 显示实体类型、ID、问题描述、推荐操作
- **验证**：`npm run build` 通过

#### Task 5.2 — Provider 详情页集成

- **产出文件**：`frontend/src/App.tsx`
- **功能要求**：
  - Provider 详情页顶部展示相关悬空引用（若 alias targets 指向不存在的 provider）
  - Provider 编辑页保存失败时展示 `ValidateReferences` 返回的 error 列表
- **验证**：`npm run build` 通过

#### Task 5.3 — Alias 详情页集成

- **产出文件**：`frontend/src/App.tsx`
- **功能要求**：
  - Alias target 列表中标记悬空 target（Provider 已删除），显示 ⚠️ 图标
  - 提供「移除无效 target」按钮（手动 alias 需确认，自动 alias 直接移除）
- **验证**：`npm run build` 通过

#### Task 5.4 — 全局诊断入口

- **产出文件**：`frontend/src/App.tsx`
- **功能要求**：
  - Settings 页或侧边栏底部增加「配置健康检查」入口
  - 调用 `CheckIntegrity` API，结果以列表展示
  - 提供「一键修复」（调用 `CleanDanglingRefs` API）
- **验证**：`npm run build` 通过

#### Task 5.5 — 前端类型定义同步

- **产出文件**：`frontend/src/types.ts`、`frontend/wailsjs/go/models.ts`
- **内容要求**：
  - 新增 `RefIntegrityWarning`、`IntegrityCheckResult` 类型
  - `AliasTargetView` 新增 `providerExists: boolean` 字段（前端判断悬空）
- **验证**：TypeScript 编译无类型错误

---

### Step 6: i18n 文案补齐

- [ ] **Step 6 完成**

**完成定义**：所有新增 UI 字符串在 `en.json` 和 `zh-CN.json` 中存在对应翻译。

#### Task 6.1 — 英文 i18n 文案

- **产出文件**：`frontend/src/i18n/locales/en.json`（追加）
- **新增 key**：
  ```json
  "integrity.title": "Configuration Health",
  "integrity.checking": "Scanning configuration...",
  "integrity.passed": "All references are valid",
  "integrity.failed": "{count} issue(s) found",
  "integrity.fixAll": "Fix All (Auto-cleanable)",
  "integrity.warning.providerDangling": "Alias target references deleted provider: {provider}",
  "integrity.warning.apikeyScopeProvider": "API Key scope includes deleted provider: {provider}",
  "integrity.warning.apikeyScopeAlias": "API Key scope includes deleted alias: {alias}",
  "integrity.warning.priorityOrphan": "Provider priority list contains deleted provider: {provider}",
  "integrity.warning.protocolMismatch": "Alias protocol ({aliasProtocol}) differs from target provider protocol ({providerProtocol})",
  "integrity.warning.modelStale": "Model '{model}' no longer exists in provider '{provider}'",
  "integrity.action.remove": "Remove",
  "integrity.action.review": "Review",
  "integrity.action.cleaned": "Cleaned {count} dangling reference(s)",
  "integrity.deleteBlocked": "Cannot delete {entity}: still referenced by {count} other config(s)",
  "integrity.saveBlocked": "Save blocked: configuration contains invalid references",
  "integrity.danglingTarget": "Dangling target"
  ```
- **验证**：JSON 格式合法

#### Task 6.2 — 中文 i18n 文案

- **产出文件**：`frontend/src/i18n/locales/zh-CN.json`（追加）
- **新增 key**：
  ```json
  "integrity.title": "配置健康检查",
  "integrity.checking": "正在扫描配置...",
  "integrity.passed": "所有引用关系正常",
  "integrity.failed": "发现 {count} 个问题",
  "integrity.fixAll": "一键修复（可自动清理项）",
  "integrity.warning.providerDangling": "别名目标引用了已删除的供应商：{provider}",
  "integrity.warning.apikeyScopeProvider": "API 密钥范围包含已删除的供应商：{provider}",
  "integrity.warning.apikeyScopeAlias": "API 密钥范围包含已删除的别名：{alias}",
  "integrity.warning.priorityOrphan": "供应商优先级列表包含已删除的供应商：{provider}",
  "integrity.warning.protocolMismatch": "别名协议（{aliasProtocol}）与目标供应商协议（{providerProtocol}）不一致",
  "integrity.warning.modelStale": "模型 '{model}' 在供应商 '{provider}' 中已不存在",
  "integrity.action.remove": "移除",
  "integrity.action.review": "检查",
  "integrity.action.cleaned": "已清理 {count} 个悬空引用",
  "integrity.deleteBlocked": "无法删除 {entity}：仍被 {count} 个配置引用",
  "integrity.saveBlocked": "保存被阻止：配置包含无效引用",
  "integrity.danglingTarget": "悬空目标"
  ```
- **验证**：JSON 格式合法

---

### Step 7: Goal — 代码审查与测试验证

- [ ] **Step 7 完成**

**完成定义**：所有步骤的单元测试通过；`go build ./...` 和 `npm run build` 全部通过；代码评审无阻断性问题。

- [ ] **Code Review**：对照需求与编码规范完成代码评审，无阻断性问题。重点检查：
  - 引用完整性校验覆盖所有引用类型（不遗漏任何 ID/Name 引用字段）
  - 级联清理不产生二次悬空引用
  - Save 校验在 Wails/HTTP/CLI/Import 四边界一致
  - Proxy 降级不改变正常路由行为
  - i18n key 100% 覆盖新增 UI 文案
  - 向后兼容：旧配置文件加载不阻塞启动
- [ ] **基础代码测试**：
  - `go build ./...` 零错误通过
  - `go test ./internal/config/...` 全部通过
  - `go test ./internal/app/...` 全部通过
  - `go test ./internal/proxy/...` 全部通过
  - `npm run build`（`frontend/`）零错误通过
- [ ] **手动冒烟验证**：
  - 删除 provider → auto alias target 自动清理 + manual target 保留警告
  - 删除 provider → ProviderPriority + API Key scope 级联清理
  - 创建 API Key 引用不存在 provider → Save 拒绝
  - Proxy 遇到悬空 target → 跳过 + 日志 warning（非 500）
  - 旧 config 含悬空引用 → 启动扫描报告 + 不阻塞
  - UI 健康检查 → 列出所有问题 → 一键修复可清理项

---

## 附录 A：引用关系矩阵（待 Step 1 填充）

> **说明**：此矩阵在执行 Step 1 时根据实际代码审计结果填充。以下为预期骨架。

| 源实体 | 引用字段 | 目标实体 | 引用方式 | 强度 | 清理策略 |
|--------|----------|----------|----------|------|----------|
| Alias.Target | `Provider` | Provider | ID 引用 | Strong | cascade-delete（auto）/ warn-preserve（manual） |
| Alias.Target | `Model` | Provider.Models | Name 引用 | Strong | warn-preserve |
| Alias | `Protocol` | Provider.Protocol | 值一致性 | Weak | warn |
| APIKey | `Providers[]` | Provider | ID 引用 | Strong | cascade-delete |
| APIKey | `Aliases[]` | Alias | Name 引用 | Strong | cascade-delete |
| Config | `ProviderPriority[]` | Provider | ID 引用 | Weak | cascade-delete |
| RewriteRule | `Provider` | Provider | ID 引用 | Strong | warn-preserve |
| RewriteRule | `Alias` | Alias | Name 引用 | Strong | warn-preserve |
| CircuitBreaker | `ProviderID` | Provider | ID 引用（内存） | Weak | cascade-delete |

---

## 附录 B：校验边界矩阵（待 Step 1 填充）

| 边界 | 当前校验 | 目标行为 | 优先级 |
|------|----------|----------|--------|
| Wails Bridge Save | 部分（auto alias 清理） | 完整 ValidateReferences + 级联清理 | P0 |
| HTTP REST Admin | 与 Wails 同一路径 | 与 Wails 一致 | P1 |
| CLI TUI | 未知 | 与 Wails 一致 | P2 |
| Config 文件导入 | 无 | 启动扫描 + 报告 | P2 |
| OpenCode Sync | 无 | 输出前校验 | P3 |
| 系统启动加载 | 无 | 扫描 + warning 日志 | P1 |
