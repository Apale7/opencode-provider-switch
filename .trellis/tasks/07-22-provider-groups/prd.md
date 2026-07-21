# Provider groups with per-group protocols and API keys

## Summary

Provider 经常由多个上游业务分组组成。各分组共用相同的服务地址，但协议、可用模型和 API Key 集合不同。当前 ocswitch 只能把这些分组拆成多个 Provider，造成重复配置、列表噪声和错误的健康状态聚合。

本任务引入一等 Provider Group：Provider 保存共享连接属性，Group 保存协议、多个上游 API Key 与模型目录，Alias Target 精确引用 Provider Group。旧配置在加载时自动转换为唯一的 `default` Group，且启动过程不得主动改写配置文件。

## Terminology

- **Provider**：一个上游服务主体，拥有共享的 Base URL、连接策略和公共请求头。
- **Provider Group**：Provider 下的业务分组，拥有独立协议、上游 API Key 集合、模型目录和启停状态。
- **Upstream API Key**：ocswitch 调用 Provider 时使用的密钥。本任务不涉及客户端访问 ocswitch 的代理鉴权密钥。
- **Resolved Target**：已解析为 `(providerID, groupID, model)` 的可路由目标。

## Problem

现有 Provider 同时承担服务主体和路由身份两种职责：

- `protocol`、`api_key/api_keys`、`models/models_source` 位于 Provider 顶层。
- Alias Target 只能引用 `provider + model`。
- 路由、模型发现、自动 Alias、熔断和健康统计均以 Provider 为最小粒度。

当同一服务主体包含多个分组时，只能复制 Provider。复制项拥有相同 Base URL，却需要分别维护名称、连接地址、探测状态和优先级，无法准确表达业务关系。

## Goals

1. 单个 Provider 可以配置一个或多个 Group。
2. `protocol`、`api_key/api_keys`、`models/models_source` 下放到 Group。
3. 每个 Group 支持多个上游 API Key，并保持现有轮换与失败重试语义。
4. Alias Target 使用稳定 Group ID 精确引用目标。
5. 路由、模型发现、能力探测、自动 Alias、熔断、Trace 和健康统计按 Group 隔离。
6. 旧配置无感加载为一个 `default` Group，迁移前后路由结果、模型可见性和 Key 顺序一致。
7. 桌面 GUI、HTTP Web UI、TUI/CLI 管理入口能够查看和编辑 Group。
8. 所有新增 UI 文案同时提供英文和简体中文翻译。

## Non-Goals

1. 不实现不同 Group 使用不同 Base URL；Base URL 仍由 Provider 共享。
2. 不改变 Alias 的外部模型名或协议入口语义。
3. 不把 `models` 升级为安全授权 ACL；它继续表示发现或配置的模型目录。
4. 不改变客户端访问 ocswitch 的鉴权密钥与权限策略。
5. 不在程序启动时自动保存或改写用户配置。
6. 不在缺失或无效 Group 时自动选择其他 Group。
7. 不要求旧版本 ocswitch 能读取新格式；新格式首次写盘前应沿用现有备份机制。
8. 不引入 direct provider-native model 路由；外部请求模型仍必须先解析为已启用 Alias。

## Frozen Data Contract

### Provider

```json
{
  "id": "vendor-a",
  "name": "Vendor A",
  "base_url": "https://api.example.com/v1",
  "base_urls": [],
  "base_url_strategy": "ordered",
  "headers": {},
  "disabled": false,
  "auto_alias_enabled": true,
  "groups": [
    {
      "id": "default",
      "name": "Default",
      "protocol": "openai",
      "api_key": "sk-primary",
      "models": ["model-a"],
      "disabled": false
    }
  ]
}
```

Provider 保留共享字段：

- `id`、`name`
- `base_url`、`base_urls`、`base_url_strategy`
- `headers`
- `disabled`
- `auto_alias_enabled`
- `groups`

### Provider Group

```json
{
  "id": "premium",
  "name": "Premium",
  "protocol": "openai",
  "api_key": "sk-primary",
  "api_keys": ["sk-backup"],
  "models": ["model-a", "model-b"],
  "models_source": "discovered",
  "disabled": false
}
```

Group 不变量：

- `id` 在所属 Provider 内非空且唯一，并作为稳定引用键。
- `name` 是显示名称，不参与路由；允许重复，但 UI 应提示区分 ID。
- `protocol` 必须是已支持且规范化后的协议。
- `api_key/api_keys` 继续兼容现有主 Key 加剩余 Key 的持久化形式，对外统一通过 `EffectiveAPIKeys()` 使用。
- 同一 Group 内的多个 Key 必须具有相同分组权限和模型集合；权限不同的 Key 必须拆到不同 Group。
- `disabled` 只禁用当前 Group；Provider `disabled` 禁用其全部 Group。

运行时 Go 契约固定为以下公开形状，Step 2 的生产代码和测试可据此并行实现：

```go
const CurrentSchemaVersion = 2

type ProviderGroup struct {
	ID           string   `json:"id"`
	Name         string   `json:"name,omitempty"`
	Protocol     string   `json:"protocol"`
	APIKey       string   `json:"api_key,omitempty"`
	APIKeys      []string `json:"api_keys,omitempty"`
	Models       []string `json:"models,omitempty"`
	ModelsSource string   `json:"models_source,omitempty"`
	Disabled     bool     `json:"disabled,omitempty"`
}

func (g ProviderGroup) IsEnabled() bool
func (g ProviderGroup) EffectiveAPIKeys() []string
func (p *Provider) FindGroup(id string) *ProviderGroup
func (c *Config) FindProviderGroup(providerID, groupID string) (*Provider, *ProviderGroup)
```

`Config` 增加 `SchemaVersion int`，`Provider` 增加 `Groups []ProviderGroup` 并移除运行时顶层分组字段，`Target` 与 `TargetRef` 增加 `Group string`。Legacy 字段只存在于私有 wire DTO，不得继续暴露给运行时调用方。

为保证分阶段实施期间主分支可编译并保持旧管理测试可用，允许在未迁移完全部消费者前临时保留一组标记为 deprecated、`json:"-"` 的 Provider 顶层兼容投影。投影只能从唯一的 `default` Group 派生；过渡写桥只允许在 Provider 尚无 Groups 或仅有 `default` Group 时把旧写入规范化到该 Group，遇到多 Group 必须返回明确的 ambiguous legacy write 错误。所有管理写路径切换到 Group 后必须删除投影、写桥和错误分支。临时机制不得进入持久化 JSON，也不得出现在最终交付 diff 中。

### Alias Target

```json
{
  "provider": "vendor-a",
  "group": "premium",
  "model": "model-a",
  "enabled": true
}
```

- Target 的稳定身份是 `(provider, group, model)`。
- Alias 协议与 Group 协议匹配，不再与 Provider 比较。
- Target、Provider、Group 必须全部启用才可路由。
- Group 缺失、禁用或协议不匹配时，该 Target 不可用且不得回退到 `default`、第一个 Group 或同协议 Group。

## Backward Compatibility

### Legacy Provider Migration

当前新格式的顶层版本字段固定为整数 `"schema_version": 2`。加载规则在进入运行时模型前完成：

- 缺少 `schema_version` 或值为 `1`：严格按 legacy wire model 解码；任何 Provider 出现 `groups` 字段（包括空数组）都作为混合格式拒绝加载。
- `schema_version` 为 `2`：严格按新 wire model 解码；每个 Provider 必须显式包含至少一个 Group，每个 Target 必须显式包含非空 Group；Provider 顶层出现任一 legacy 分组字段都拒绝加载。
- `schema_version` 为其他值、非整数或 `null`：拒绝加载并返回 unsupported schema version 错误。
- 字段缺失与显式空数组必须由 wire DTO 保留差异，不得只依赖运行时 slice 零值猜测格式。

加载 legacy Provider 时，在内存中创建唯一 Group：

```json
{
  "id": "default",
  "name": "Default",
  "protocol": "<legacy provider.protocol>",
  "api_key": "<legacy provider.api_key>",
  "api_keys": ["<legacy provider.api_keys>"],
  "models": ["<legacy provider.models>"],
  "models_source": "<legacy provider.models_source>",
  "disabled": false
}
```

规则：

1. 字段值、模型顺序和 Key 顺序原样进入现有规范化流程。
2. 原 Provider `disabled` 仍作为全局禁用状态，不复制到 Group。
3. 旧 Alias Target 缺少 `group` 时仅补为 `default`。
4. 旧配置加载不得触发 `Save`、不得改变文件内容、修改时间或 ConfigStore revision。
5. 用户后续执行正常配置保存时，写出 canonical Group 格式并移除 Provider 顶层旧分组字段。
6. 再次读取新格式必须幂等，不得重复创建 `default` Group。
7. 混合格式在加载边界立即拒绝并给出明确诊断，禁止让其进入运行时后再等到保存阶段失败。
8. 新格式中的 Target 缺少 Group 属于结构错误；只有 schema v1/无版本 legacy Target 才允许补 `default`。
9. 当前写入器始终输出 `schema_version: 2`，不得输出 Provider 顶层 legacy 分组字段。

## Routing Semantics

1. 请求模型必须先命中已启用 Alias，再将 Alias 解析为有序 Target；未命中 Alias 直接返回既有 model-not-found 错误，不按 Provider/Group 模型目录生成 direct 候选。
2. 路由候选必须携带 Provider ID、Group ID、协议、模型、Base URLs 和该 Group 的 Key 集合。
3. Base URL 顺序沿用 Provider 策略；每个 Base URL 内只轮换当前 Group 的 Key。
4. API Key 轮换起点继续由 request/trace ID 决定，但不同 Group 的 Key 池完全隔离。
5. 协议路径、认证头和默认头均来自 Group 协议。
6. 熔断和失败状态键至少包含 Provider ID 与 Group ID，防止同 Provider 的不同 Group 互相污染。
7. Trace Attempt 记录 Group ID；Provider 健康页提供 Group 明细和 Provider 汇总。
8. 共享 `headers` 继续位于 Provider。若多个 Group 存在，配置校验必须阻止共享 Header 覆盖由协议管理的认证头；单一 legacy default Group 保持旧配置兼容诊断。
9. 同模型存在于多个 Group 时，仅 Alias Targets 的显式顺序决定尝试顺序；一个 Target 的 Base URL/Key 重试耗尽后才进入 Alias 中下一个 Target，不允许在 Provider 内搜索未列出的兄弟 Group。

## Request Rewrite Contract

新格式将 Rewrite 的 Provider 选择器升级为精确 Group 选择器：

```go
type ProviderGroupSelector struct {
	Provider string `json:"provider"`
	Group    string `json:"group"`
}

type RequestRewriteRule struct {
	// existing fields omitted
	ProviderGroups []ProviderGroupSelector `json:"provider_groups,omitempty"`
}

func ApplyRequestRewriteRules(payload map[string]any, aliasName, providerID, groupID, model string, rules []RequestRewriteRule)
```

匹配与迁移规则：

- schema v2 不再接受 `providers` 字段；非空 `provider_groups` 仅匹配 Provider ID 与 Group ID 都相等的 resolved target。
- schema v2 的 `provider_groups` 为空表示显式 wildcard，匹配该 Alias 的所有当前和未来目标；这是唯一允许新增 Group 扩大 Rewrite 作用域的形式。
- schema v1/无版本规则的非空 `providers` 中，每个 Provider ID 精确迁移为 `{provider: <id>, group: "default"}`，不得扩展到该 Provider 后续新增的其他 Group。
- schema v1/无版本规则的空 `providers` 保持 wildcard 语义。
- selector 引用缺失 Provider/Group 时规则不匹配该目标，并由诊断报告；不得回退 `default` 或同 Provider 其他 Group。
- 重复 selector 在规范化时按 `(provider, group)` 去重并保持首次出现顺序。

## Model Discovery And Alias Semantics

- 模型刷新、Base URL Ping、协议能力探测均要求精确 Group ID。
- 探测只使用目标 Group 的协议和 Key，不得合并或尝试兄弟 Group 的 Key。
- 刷新结果只更新目标 Group 的 `models/models_source`。
- Provider 共享 Base URL 变化使全部 Group 的发现结果失效；Group 协议或 Key 变化只使该 Group 的发现结果失效。
- 自动 Alias 以 `(provider, group, model)` 生成、更新和清理 Target。
- 删除 Group 时必须通过现有配置生命周期机制预览或处理所有 Target 引用，不得静默重绑到其他 Group。
- Provider Priority 继续只控制 Provider 层优先级；同一 Provider 内 Target 顺序由 Alias Targets 明确表达。

## Management API And UX

### Service Contract

- Provider 创建时必须至少包含一个 Group；UI 新建 Provider 默认生成可编辑的 `default` Group。
- Provider 共享配置更新与 Group CRUD 使用不会覆盖兄弟 Group 的输入契约。
- Group 删除、ID 修改和 Provider 删除进入统一引用诊断/生命周期规划。
- Provider List/View 返回 Group 摘要和掩码 Key，禁止返回明文 Key。
- Group 新增或更新使用独立 `ProviderGroupInput`/`ProviderGroupView`，不得复用客户端代理密钥 DTO；HTTP 管理路径位于 Provider Group 资源下，不使用 `/api-keys`。
- Group 更新输入使用 `apiKeysChanged: boolean` 与 `apiKeys: string[]`：`false` 时 `apiKeys` 必须为空且保留已存 Key；`true` 时用规范化后的数组整体替换，空数组表示明确清空。输入包含任何掩码占位符都拒绝。
- Group List/View 和 create/update 响应只返回 `apiKeysMasked`/`apiKeyCount`，禁止回显明文；前端标签固定使用 `Upstream keys` / `上游密钥`，与客户端代理 API Key 页面隔离。

管理契约固定为独立文件中的以下核心字段：

```go
type ProviderGroupInput struct {
	ID             string   `json:"id"`
	Name           string   `json:"name,omitempty"`
	Protocol       string   `json:"protocol"`
	APIKeysChanged bool     `json:"apiKeysChanged"`
	APIKeys        []string `json:"apiKeys,omitempty"`
	Models         []string `json:"models,omitempty"`
	Disabled       bool     `json:"disabled,omitempty"`
}

type ProviderGroupView struct {
	ID            string   `json:"id"`
	Name          string   `json:"name,omitempty"`
	Protocol      string   `json:"protocol"`
	APIKeyCount   int      `json:"apiKeyCount"`
	APIKeysMasked []string `json:"apiKeysMasked,omitempty"`
	Models        []string `json:"models,omitempty"`
	ModelsSource  string   `json:"modelsSource,omitempty"`
	Disabled      bool     `json:"disabled"`
}
```

同一基础契约阶段还必须一次性给以下现有 App DTO 增加 Group 字段，避免后续入口各自发明 wire shape：

- `ProviderView.Groups []ProviderGroupView`；迁移期可暂留 deprecated 顶层协议/Key/模型只读字段，最终删除。
- `AliasTargetView.Group string`。
- `AliasTargetRefInput.Group string`，以及所有 bind/unbind/reorder 输入中的精确 Group。
- `ProviderGroupSelectorInput/View { Provider string; Group string }`。
- `RequestRewriteRuleInput/View.ProviderGroups []ProviderGroupSelectorInput/View`；迁移期 deprecated `Providers` 只用于读取 v1 展示，最终删除。

### GUI And Web UI

- Provider 编辑器分为共享连接设置和 Group 列表/详情。
- Group 支持新增、重命名显示名、配置稳定 ID、启停、协议、多个 Key、模型刷新和删除。
- Alias Target 编辑顺序为 Provider → 协议兼容 Group → Model。
- Provider 列表展示 Group 数量和可用状态，不把每个 Group 展示成伪 Provider。
- 窄屏布局不得产生字段重叠；新增 UI 字符串必须进入 `en.json` 和 `zh-CN.json`。

### TUI And CLI

- TUI Provider 编辑流程增加 Group 选择与编辑。
- CLI 对 `default` Group 可保留旧参数体验；非 default Group 必须使用显式 `--provider`、`--group`、`--model` 参数。
- 不使用 `provider/group/model` 拼接语法，因为模型名自身可能包含 `/`。

## Diagnostics And Observability

- 诊断实体和逻辑路径支持 Provider Group。
- 检测重复/空 Group ID、空 Group 列表、未知协议、混合格式和悬空 Group Target。
- Trace、转发日志、调试 Header 和健康数据能够区分 Group；敏感 Key 仍只显示掩码和组内索引。
- 现有历史 Trace 缺少 Group 时按 `default` 展示，不影响读取；新记录必须写入 Group。
- Rewrite、健康过滤和聚合若引用 Provider，必须明确其作用是 Provider 汇总还是精确 Group，禁止新增 Group 后隐式扩大规则作用域。
- Rewrite 使用 `provider_groups` 精确选择器；只有显式空 selector wildcard 可以随新增 Group 扩展。

## Acceptance Criteria

1. 单个 Provider 可保存并重载多个 Group，每个 Group 具有独立协议、模型和多个 Key。
2. 旧配置加载后在内存中得到唯一 `default` Group，启动不改盘，正常保存后输出 canonical 新格式。
3. 旧 Alias Target 自动绑定 `default`，迁移前后候选顺序与路由行为一致。
4. 无效、禁用或协议不匹配的 Group 不可路由且不会回退其他 Group。
5. 同 Provider 两个 Group 的 Key 轮换、失败重试和熔断状态互不影响。
6. 模型发现、Ping、能力探测和自动 Alias 始终使用精确 Group。
7. Group 删除和 ID 变更不会产生未诊断的悬空引用或自动重绑。
8. Trace 和健康视图可以区分 Provider Group，并兼容历史无 Group 数据。
9. 桌面 GUI、HTTP Web UI、TUI/CLI 能管理 Group；所有新增 UI 文案具有中英文翻译。
10. Provider、Alias、同步、代理和配置引用完整性既有流程无回归。
11. Legacy 加载不会改变 ConfigStore revision；首次写入 v2 前生成现有机制定义的备份，备份失败时不覆盖原配置。
12. Provider Group 管理 API 不回显明文 Key，掩码占位符不能进入持久化配置，两类 API Key DTO 和路由互不复用。
13. 未命中 Alias 的模型不产生 direct Group 候选；同 Provider 兄弟 Group 仅在 Alias Targets 明确列出时才可依序重试。
14. Legacy Rewrite 非空 Provider selector 只迁移到 `default`，v2 精确 Group selector 和 wildcard 语义通过 round-trip 与新增 Group 测试。

## Verification

- `go test ./internal/config ./internal/proxy ./internal/app ./internal/opencode ./internal/lifecycle ./internal/diagnostics ./internal/routing`
- `go test ./internal/cli ./internal/tui ./internal/server ./internal/webadmin ./internal/desktop`
- `go test ./...`
- `npm run build`（`frontend`）
- Golden fixture：旧配置加载不改盘、首次保存输出新格式、二次 round-trip 幂等。
- ConfigStore 测试：legacy 只读加载 revision 不变；首次 v2 保存生成备份；备份失败保持原文件和运行时快照。
- Proxy 集成测试：同 Provider 多 Group、不同协议、Key 轮换、失败重试、熔断隔离和禁止 fallback。
- 管理 API 测试：`apiKeysChanged` 保留/替换/清空、掩码输入拒绝、所有 list/create/update 响应无明文 Key。
- UI 手工验证：桌面与 HTTP Web 模式下完成 Provider Group CRUD、模型刷新和 Alias Target 绑定。

## Dependencies And Risks

- 本任务依赖当前 ConfigStore/引用诊断改造保持提交原子性；实现前需基于合并后的契约重新确认目标文件。
- 与 `05-16-provider-multi-ak` 无语义依赖；两项 API Key 功能必须使用不同类型和 UI 名称。
- 新格式保存后旧版本无法可靠读取，属于预期的单向格式升级，必须保留保存前备份。
- 共享认证 Header、熔断状态键和 Rewrite Provider 选择器是最高风险区域，必须在代理集成前冻结并覆盖测试。
