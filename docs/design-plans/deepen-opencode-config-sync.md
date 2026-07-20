# 详细设计方案：深化 OpenCode 配置同步

> **方案状态**：待执行
> **创建日期**：2026-06-25
> **关联任务**：Phase 1 — 深化 OpenCode 配置生成与同步

---

## 约束 / Constraints

> **执行本方案时，Agent 必须遵守以下硬性约束：**

1. **Agent 只能修改 `todolist` 区域和 checkbox 状态（`- [ ]` ↔ `- [x]`）。** 不得编辑、改写、重排、新增或删除任何其他内容（步骤正文、任务描述、goal 文本、标题、本约束块）。
2. **同一 step 内的 task 必须由多个 subagent 并行执行。** 一次性派发该 step 的全部 task 到并行 subagent，不得在主 agent 中逐个串行执行。
3. **Step 严格按顺序执行。** 上一个 step 的 checkbox 未勾选前，不得开始下一个 step。

---

## todolist

> Agent 的实时工作清单。仅此区域可在执行中自由修改。

- [x] Step 1: 模型能力探测引擎
- [x] Step 2: 定价数据获取模块
- [x] Step 3: 同步差异计算引擎
- [x] Step 4: 扩展 EnsureOcswitchProvider
- [x] Step 5: 后端 API 端点扩展
- [x] Step 6: 前端可视化预览页面
- [x] Step 7: i18n 文案补齐与联调
- [x] Step 8: Goal — 代码审查与测试验证

---

## steps

### Step 1: 模型能力探测引擎

- [x] **Step 1 完成**

**完成定义**：`internal/opencode/model_capability.go` 可独立编译，`known_models.json` 内置文件就位，三级 fallback 探测逻辑通过单元测试。

#### Task 1.1 — 创建 known_models.json 内置模型数据库

- **产出文件**：`internal/opencode/known_models.json`
- **内容要求**：
  - 覆盖 Top 20 主流模型（GPT-4o / GPT-4.1 / GPT-5 / Claude Sonnet 4 / Claude Opus 4 / Gemini 2.5 Pro / DeepSeek V3 / DeepSeek R1 等）
  - 每个模型包含字段：`contextLimit`、`outputLimit`、`inputModalities`、`outputModalities`、`reasoning`、`toolCall`、`attachment`、`temperature`、`experimental`、`cost.input`、`cost.output`、`cost.cacheRead`、`cost.cacheWrite`
  - JSON 格式，可直接 `embed.FS` 嵌入二进制
- **验证**：`go build ./internal/opencode/` 通过，JSON 语法合法

#### Task 1.2 — 实现 model_capability.go 探测引擎

- **产出文件**：`internal/opencode/model_capability.go`
- **核心类型**：
  ```go
  type ModelCapabilityProbe struct {
      ModelID           string
      ProviderID        string
      Protocol          string
      ContextLimit      int64
      OutputLimit       int64
      InputModalities   []string
      OutputModalities  []string
      SupportsReasoning bool
      SupportsTools     bool
      SupportsImages    bool
      ProbeSource       string   // "upstream" | "known_db" | "fallback"
      ProbeError        string
  }
  ```
- **核心函数**：`ProbeModelCapability(ctx, provider, modelID) ModelCapabilityProbe`
- **三级 fallback 逻辑**：
  - L1：调用上游 `/models` 端点（复用现有 `FetchProviderModels` 的 HTTP 基础设施）
  - L2：查询 `known_models.json` 内置数据库
  - L3：按协议返回安全默认值（`contextLimit=128000`, `outputLimit=4096`, `inputModalities=["text"]`, `outputModalities=["text"]`）
- **验证**：`go test ./internal/opencode/ -run ModelCapability -v` 全部通过

#### Task 1.3 — 编写 model_capability_test.go

- **产出文件**：`internal/opencode/model_capability_test.go`
- **测试用例**：
  - L1 上游探测成功 → 返回 upstream 来源
  - L1 失败 → fallback 到 L2 known_db
  - L1+L2 都失败 → fallback 到 L3 默认值
  - 未知协议 → 返回默认值 + ProbeError
  - 空 modelID → 返回错误
- **验证**：`go test ./internal/opencode/ -run ModelCapability -v -count=1` 全部通过

---

### Step 2: 定价数据获取模块

- [x] **Step 2 完成**

**完成定义**：`internal/opencode/model_pricing.go` 可独立编译，能从 `known_models.json` 读取定价，单元测试通过。

#### Task 2.1 — 实现 model_pricing.go

- **产出文件**：`internal/opencode/model_pricing.go`
- **核心类型**：
  ```go
  type ModelPricing struct {
      ModelID         string
      InputPer1K      float64
      OutputPer1K     float64
      CacheReadPer1K  float64
      CacheWritePer1K float64
      Currency        string
      Source          string  // "known_db" | "manual"
  }
  ```
- **核心函数**：`LookupModelPricing(modelID string) (ModelPricing, bool)`
- **数据来源**：从 Step 1 的 `known_models.json` 中提取 `cost.*` 字段
- **验证**：`go build ./internal/opencode/` 通过

#### Task 2.2 — 编写 model_pricing_test.go

- **产出文件**：`internal/opencode/model_pricing_test.go`
- **测试用例**：
  - 已知模型 → 返回正确定价
  - 未知模型 → 返回 `false`
  - 定价字段类型校验（确保是合法浮点数）
- **验证**：`go test ./internal/opencode/ -run Pricing -v -count=1` 全部通过

---

### Step 3: 同步差异计算引擎

- [x] **Step 3 完成**

**完成定义**：`internal/opencode/sync_diff.go` 可独立编译，能逐字段对比用户配置与建议配置，单元测试覆盖全部 5 种状态。

#### Task 3.1 — 实现 sync_diff.go

- **产出文件**：`internal/opencode/sync_diff.go`
- **核心类型**：
  ```go
  type SyncDiffEntry struct {
      Path           string
      UserValue      any
      ProposedValue  any
      Status         string  // "new" | "changed" | "unchanged" | "conflict" | "failed"
      ConflictNote   string
      AutoDetected   bool
  }

  type AliasSyncDiff struct {
      AliasName  string
      Protocol   string
      ProviderKey string
      Entries    []SyncDiffEntry
      Summary    DiffSummary
  }

  type DiffSummary struct {
      Total      int
      New        int
      Changed    int
      Unchanged  int
      Conflict   int
      Failed     int
  }
  ```
- **核心函数**：`ComputeSyncDiff(aliasName, protocol string, userModelConfig map[string]any, proposedConfig map[string]any, probeErrors map[string]string) AliasSyncDiff`
- **冲突处理规则**（严格按此实现）：

  | 场景 | Status | 行为 |
  |------|--------|------|
  | 用户字段不存在，ocswitch 能探测到 | `new` | 建议值 = 探测值 |
  | 用户字段存在，值与探测值相同 | `unchanged` | 保留用户值 |
  | 用户字段存在，值与探测值不同 | `conflict` | **以用户值为准**，记录冲突提示 |
  | 用户字段不存在，ocswitch 探测失败 | `failed` | 填充安全默认值，标记探测失败原因 |
  | 用户字段存在，ocswitch 探测失败 | `unchanged` | 保留用户值不动 |

- **覆盖的字段路径**：
  - `name`
  - `limit.context` / `limit.output`
  - `cost.input` / `cost.output` / `cost.cacheRead` / `cost.cacheWrite`
  - `inputModalities` / `outputModalities`
  - `reasoning` / `toolCall` / `attachment` / `temperature` / `experimental`
  - `variants`
  - `status` / `releaseDate`
- **验证**：`go build ./internal/opencode/` 通过

#### Task 3.2 — 编写 sync_diff_test.go

- **产出文件**：`internal/opencode/sync_diff_test.go`
- **测试用例**：
  - 全部字段新增 → 所有 entry status = `new`
  - 全部字段匹配 → 所有 entry status = `unchanged`
  - 用户值冲突 → status = `conflict`，UserValue 保留
  - 探测失败 → status = `failed`，ProposedValue 为安全默认值
  - 混合场景（部分新增、部分冲突、部分失败）
  - 空用户配置 → 全部 `new` 或 `failed`
- **验证**：`go test ./internal/opencode/ -run SyncDiff -v -count=1` 全部通过

---

### Step 4: 扩展 EnsureOcswitchProvider

- [x] **Step 4 完成**

**完成定义**：`EnsureOcswitchProvider` 支持传入模型能力探测结果，对 models 子树执行"用户值优先"合并，现有测试不回归。

#### Task 4.1 — 扩展 EnsureOcswitchProvider 签名与逻辑

- **产出文件**：`internal/opencode/opencode.go`（修改现有文件）
- **变更内容**：
  - 新增可选参数 `modelCapabilities map[string]ModelCapabilityProbe`
  - 在 models 合并逻辑中：
    - 已有 model entry → 调用 `mergeWithUserPriority(existing, capability)` 只补缺失字段
    - 新 model entry → 调用 `buildModelConfig(alias, capability)` 生成完整配置
  - `mergeWithUserPriority` 规则：用户已有字段保留不动；用户没有的字段填充探测值；探测失败的字段填充安全默认值
  - `buildModelConfig` 规则：将 `ModelCapabilityProbe` + `ModelPricing` 转换为 opencode.json 的 model 配置格式
- **向后兼容**：`modelCapabilities` 为 nil 时行为与当前完全一致
- **验证**：`go test ./internal/opencode/ -run EnsureOcswitch -v -count=1` 全部通过

#### Task 4.2 — 扩展 opencode_test.go 覆盖新逻辑

- **产出文件**：`internal/opencode/opencode_test.go`（修改现有文件）
- **新增测试用例**：
  - 传入 modelCapabilities → 新 model entry 包含完整能力字段
  - 已有 model entry + 传入 capabilities → 只补缺失字段，不覆盖已有
  - 用户已设 `limit.context=100000`，探测值为 `200000` → 保留 `100000`
  - 探测失败的字段 → 填充安全默认值
  - modelCapabilities 为 nil → 行为不变（回归测试）
- **验证**：`go test ./internal/opencode/ -v -count=1` 全部通过

---

### Step 5: 后端 API 端点扩展

- [x] **Step 5 完成**

**完成定义**：`prepareSyncWithBaseURL` 集成能力探测和 diff 计算，`SyncPreview` 返回 `aliasPreviews` 数据，新增 `/api/opencode-sync/preview-diff` 端点。

#### Task 5.1 — 扩展 SyncInput / SyncPreview / SyncResult 类型

- **产出文件**：`internal/app/types.go`（修改现有文件）
- **新增类型**：
  ```go
  type SyncDiffEntryView struct {
      Path          string      `json:"path"`
      UserValue     any         `json:"userValue,omitempty"`
      ProposedValue any         `json:"proposedValue,omitempty"`
      Status        string      `json:"status"`
      ConflictNote  string      `json:"conflictNote,omitempty"`
      AutoDetected  bool        `json:"autoDetected"`
  }

  type AliasSyncPreviewView struct {
      AliasName   string               `json:"aliasName"`
      Protocol    string               `json:"protocol"`
      ProviderKey string               `json:"providerKey"`
      Entries     []SyncDiffEntryView  `json:"entries"`
      Summary     DiffSummaryView      `json:"summary"`
  }

  type DiffSummaryView struct {
      Total      int `json:"total"`
      New        int `json:"new"`
      Changed    int `json:"changed"`
      Unchanged  int `json:"unchanged"`
      Conflict   int `json:"conflict"`
      Failed     int `json:"failed"`
  }
  ```
- 在 `SyncPreview` 和 `SyncResult` 中新增字段：
  ```go
  AliasPreviews []AliasSyncPreviewView `json:"aliasPreviews,omitempty"`
  OverallSummary *DiffSummaryView      `json:"overallSummary,omitempty"`
  ```
- **验证**：`go build ./...` 通过

#### Task 5.2 — 扩展 prepareSyncWithBaseURL 集成探测与 diff

- **产出文件**：`internal/app/service.go`（修改现有文件）
- **变更内容**：
  - 在 `prepareSyncWithBaseURL` 中，对每个 alias 绑定的 provider+model 调用 `ProbeModelCapability`
  - 对每个 alias 调用 `ComputeSyncDiff` 生成差异数据
  - 将 `AliasPreviews` 和 `OverallSummary` 填入返回的 `preparedSync`
  - 能力探测失败不阻塞同步流程，仅标记对应字段为 `failed`
- **验证**：`go test ./internal/app/ -run Sync -v -count=1` 全部通过

#### Task 5.3 — 新增 HTTP 端点和 Wails binding

- **产出文件**：`internal/webadmin/handler.go`（修改）、`internal/desktop/bindings.go`（修改）
- **新增 HTTP 端点**：
  - `POST /api/opencode-sync/preview-diff` → 返回含 `aliasPreviews` 的 `SyncPreview`
- **新增 Wails binding**：
  - `PreviewOpenCodeSyncDiff(ctx, SyncInput) (SyncPreview, error)` — 代理到 `Service.PreviewOpenCodeSync`
- **验证**：`go build ./...` 通过

---

### Step 6: 前端可视化预览页面

- [x] **Step 6 完成**

**完成定义**：Sync 页面新增"配置差异预览"区域，按 alias 展示逐字段 diff，5 种状态用不同颜色/图标区分，冲突和失败有 tooltip 提示。

#### Task 6.1 — 扩展前端类型定义

- **产出文件**：`frontend/src/types.ts`（修改现有文件）
- **新增类型**（与后端 `SyncDiffEntryView` / `AliasSyncPreviewView` / `DiffSummaryView` 对齐）：
  ```typescript
  export type SyncDiffEntry = {
    path: string
    userValue?: unknown
    proposedValue?: unknown
    status: 'new' | 'changed' | 'unchanged' | 'conflict' | 'failed'
    conflictNote?: string
    autoDetected: boolean
  }

  export type AliasSyncPreview = {
    aliasName: string
    protocol: ProviderProtocol
    providerKey: string
    entries: SyncDiffEntry[]
    summary: DiffSummary
  }

  export type DiffSummary = {
    total: number
    new: number
    changed: number
    unchanged: number
    conflict: number
    failed: number
  }
  ```
- 在 `SyncPreview` 和 `SyncResult` 中新增：
  ```typescript
  aliasPreviews?: AliasSyncPreview[]
  overallSummary?: DiffSummary
  ```
- **验证**：`cd frontend && npx tsc --noEmit` 通过

#### Task 6.2 — 实现 diff 预览 UI 组件

- **产出文件**：`frontend/src/App.tsx`（修改现有文件，在 sync tab 区域新增）
- **UI 布局**：
  ```
  ┌─ 协议筛选 tabs ───────────────────────────────────┐
  │ [全部] [openai-responses] [anthropic] [compat]      │
  ├─ 汇总统计 ──────────────────────────────────────────┤
  │ 12 个 alias | 5 个冲突 | 18 个新增 | 3 个探测失败     │
  ├─ Alias 卡片列表（可折叠）───────────────────────────┤
  │ ┌─ gpt-4o ────────────────────────────────────┐    │
  │ │ 协议: openai-responses  3⚠冲突  2✚新增  1✖失败 │    │
  │ │ ┌──────────────────────────────────────────┐ │    │
  │ │ │ 字段          当前值      建议值     状态  │ │    │
  │ │ │ limit.context  128000     200000     ⚠冲突 │ │    │
  │ │ │   💡 用户值优先。探测值: 200000            │ │    │
  │ │ │ limit.output   16384      -          ✓不变 │ │    │
  │ │ │ inputModalities -         ["text",   ✚新增 │ │    │
  │ │ │                          "image"]          │ │    │
  │ │ │ temperature    -          -          ✖失败  │ │    │
  │ │ │   💡 无法探测，已填充默认值 true            │ │    │
  │ │ └──────────────────────────────────────────┘ │    │
  │ │ [展开完整 JSON 预览]                          │    │
  │ └──────────────────────────────────────────────┘    │
  └─────────────────────────────────────────────────────┘
  ```
- **状态颜色方案**：
  - `unchanged` → 灰色文字，✓ 图标
  - `new` → 绿色文字，✚ 图标
  - `conflict` → 橙色文字，⚠ 图标 + tooltip
  - `failed` → 红色文字，✖ 图标 + tooltip
- **交互行为**：
  - 默认折叠所有 alias 卡片
  - 有 conflict 或 failed 的 alias 默认展开
  - 协议筛选 tabs 过滤 alias 列表
  - 点击"展开完整 JSON"显示该 alias 的完整 model 配置 JSON
- **验证**：`cd frontend && npx tsc --noEmit && npx vite build` 通过

#### Task 6.3 — 更新前端 API 调用

- **产出文件**：`frontend/src/api.ts`（修改现有文件）
- **变更内容**：
  - `previewSync` 函数返回的 `SyncPreview` 类型已包含 `aliasPreviews`
  - 无需新增 API 函数，现有 `previewSync` / `applySync` 即可
- **验证**：`cd frontend && npx tsc --noEmit` 通过

---

### Step 7: i18n 文案补齐与联调

- [x] **Step 7 完成**

**完成定义**：中英文 i18n 文件包含所有新增 key，sync 页面 diff 区域文案完整。

#### Task 7.1 — 补齐 zh-CN.json

- **产出文件**：`frontend/src/i18n/locales/zh-CN.json`（修改现有文件）
- **新增 key**：
  ```json
  {
    "sync": {
      "diffTitle": "配置差异预览",
      "diffSubtitle": "逐字段对比当前配置与建议配置。用户已配置的字段不会被覆盖。",
      "diffFilterAll": "全部协议",
      "diffSummary": "{{aliases}} 个 alias | {{conflicts}} 个冲突 | {{new}} 个新增 | {{failed}} 个探测失败",
      "diffStatusNew": "新增",
      "diffStatusChanged": "变更",
      "diffStatusUnchanged": "不变",
      "diffStatusConflict": "冲突",
      "diffStatusFailed": "探测失败",
      "diffConflictHint": "用户值优先，不会被覆盖。探测值: {{value}}（来源: {{source}}）",
      "diffFailedHint": "无法自动探测，已填充默认值 {{value}}。请手动确认或修改。",
      "diffExpandJson": "展开完整 JSON 预览",
      "diffCollapseJson": "收起 JSON 预览",
      "diffNoChanges": "所有配置已是最新，无需同步。",
      "diffProbeSourceUpstream": "上游 /models 端点",
      "diffProbeSourceKnownDb": "内置模型数据库",
      "diffProbeSourceFallback": "协议默认值",
      "diffProbeSourceManual": "手动配置",
      "capabilityProbeNote": "模型能力通过上游 /models 端点和内置数据库自动探测。标记为"探测失败"的字段已填入不影响配置文件语法正确性的安全默认值，请手动确认。"
    }
  }
  ```
- **验证**：JSON 语法合法

#### Task 7.2 — 补齐 en.json

- **产出文件**：`frontend/src/i18n/locales/en.json`（修改现有文件）
- **新增 key**（与 zh-CN.json 一一对应，英文翻译）
- **验证**：JSON 语法合法，key 集合与 zh-CN.json 完全一致

---

### Step 8: Goal — 代码审查与测试验证

- [x] **Step 8 完成**

**完成定义**：全部代码通过编译、单元测试、前端构建，人工 code review 无阻塞性问题。

#### Task 8.1 — 后端编译与测试

- [x] `go build ./...` 通过（全量编译无错误）
- [x] `go test ./internal/opencode/... -v -count=1` 全部通过
- [x] `go test ./internal/app/... -v -count=1` 全部通过
- [x] 无现有测试回归

#### Task 8.2 — 前端编译与构建

- [x] `cd frontend && npx tsc --noEmit` 通过（类型检查无错误）
- [x] `cd frontend && npx vite build` 通过（生产构建成功）

#### Task 8.3 — Code Review

- [x] 所有新增 Go 文件有完整的 godoc 注释
- [x] 所有新增 TypeScript 类型有 JSDoc 注释
- [x] 冲突处理逻辑符合"用户值优先"原则
- [x] 三级 fallback 探测逻辑无死循环风险
- [x] `known_models.json` 数据来源标注清晰
- [x] 前端 diff 页面在暗色/亮色主题下均可读
- [x] i18n key 中英文齐全且语义一致

---

## goal

> 验收标准。全部勾选后方可视为方案完成。

- [x] **代码审查通过**：Step 8.3 全部 review 项无阻塞性问题。
- [x] **后端测试通过**：`go build ./...` + `go test ./internal/opencode/... ./internal/app/... -count=1` 全部通过，无回归。
- [x] **前端构建通过**：`npx tsc --noEmit` + `npx vite build` 全部通过。
- [x] **功能验收**：
  - [x] 用户点击"预览"后，sync 页面展示逐 alias 的字段 diff
  - [x] 冲突字段以橙色标记，tooltip 明确说明"用户值优先"
  - [x] 探测失败字段以红色标记，tooltip 说明失败原因和默认值
  - [x] 新增字段以绿色标记，标注探测来源
  - [x] 用户已有的字段值在同步后保持不变
  - [x] 新 alias 自动生成包含能力声明的完整 model 配置
  - [x] 协议筛选 tabs 正确过滤 alias 列表
  - [x] 中英文切换后 diff 页面文案正确
