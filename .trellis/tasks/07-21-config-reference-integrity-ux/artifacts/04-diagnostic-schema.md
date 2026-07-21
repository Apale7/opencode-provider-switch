# Typed Diagnostic Schema V1

> 本文冻结诊断 issue 的机器契约。生命周期执行结果与 transport 映射不在本文定义。

## Wire schema

```json
{
  "schemaVersion": 1,
  "code": "alias_target_provider_missing",
  "severity": "error",
  "path": "/config/aliases/0/targets/1/provider",
  "source": {"kind": "alias_target", "key": "chat#1", "path": "/config/aliases/0/targets/1"},
  "target": {"kind": "provider", "key": "provider-b", "path": null},
  "reason": "missing",
  "allowedActions": ["rebind_target", "remove_target", "delete_alias"],
  "params": {"alias": "chat", "targetIndex": 1, "providerId": "provider-b", "model": "gpt-5"}
}
```

全部字段必需。无目标用 `target:null`；无 action 用 `[]`；无 params 用 `{}`。不得序列化为 null 或省略。`path` 使用 RFC 6901 转义的逻辑路径，根仅允许 `/config`、`/opencode/file`、`/opencode/runtime`、`/runtime`；不得包含文件系统绝对路径。

`source/target.kind` V1：`config`、`provider`、`alias`、`alias_target`、`rewrite_rule`、`priority_entry`、`model_catalog`、`model_symbol`、`external_config_field`、`runtime`、`request`。重复或空 identity 使用 `@index:<n>` 合成 key，不得选择第一个对象冒充唯一实体。

## Severity 与 reason

Severity 固定为 `error|warning|info`，同一 code 不因入口变化。它用于展示，不直接决定 lifecycle 是否阻断。Scanner 只生成本 schema；planner 按 `code × operation/context` 映射为 `blocker|required_choice|preserved`，客户端不得从 severity 或 actions 推导 disposition。

核心 reason：

| reason | 语义 |
|---|---|
| `missing` | 目标实体或当前选择器目标不存在；是否强依赖由 code 决定 |
| `disabled` | 实体存在但显式禁用 |
| `protocol_mismatch` | 两端存在但协议不兼容 |
| `catalog_stale` | 模型目录未观察、失败、空、不完整、指纹变化或来源不可信 |
| `runtime_unavailable` | 静态结构有效，但熔断、超时、网络或 endpoint 暂不可用 |
| `no_available_target` | Alias 存在但当前无可选 target |
| `ambiguous` | 重复 identity 或 ownership 冲突 |
| `invalid` | 结构/语法无效且无更精确分类 |
| `legacy` | 兼容但需迁移的旧表示 |
| `drift` | 文件、契约或 runtime 快照差异 |

静态 scanner 不读取 circuit/latency cache，不得产生 `runtime_unavailable`。`catalog_stale` 不证明 Target.Model 不存在，也不排除 target。主原因优先级：missing > protocol_mismatch > disabled > runtime_unavailable > catalog_stale。

## Canonical codes

| code | severity | reason | 说明 |
|---|---|---|---|
| `provider_identity_ambiguous` | error | ambiguous | Provider 重复 |
| `alias_identity_ambiguous` | error | ambiguous | Alias 重复 |
| `alias_target_identity_ambiguous` | error | ambiguous | target identity 重复 |
| `rewrite_rule_identity_ambiguous` | error | ambiguous | Rule 重复 |
| `alias_target_provider_missing` | error | missing | 强路由依赖缺失 |
| `alias_disabled` | info | disabled | Alias 禁用 |
| `alias_target_disabled` | info | disabled | Target 禁用 |
| `alias_target_provider_disabled` | warning | disabled | Provider 禁用 |
| `alias_target_protocol_mismatch` | error | protocol_mismatch | 协议冲突 |
| `provider_model_catalog_stale` | warning | catalog_stale | Provider 目录不可信 |
| `alias_target_model_unconfirmed` | info | catalog_stale | 外部模型符号未确认 |
| `alias_no_available_target` | warning | no_available_target | 配置视图无可用 target |
| `rewrite_alias_selector_unresolved` | info | missing | 弱 Alias selector；direct fallback 仍可能命中 |
| `rewrite_provider_selector_missing` | warning | missing | 弱 Provider selector |
| `provider_priority_entry_missing` | info | missing | 排序提示残留 |
| `opencode_default_model_unroutable` | warning | missing | 外部弱引用 |
| `opencode_small_model_unroutable` | warning | missing | 外部弱引用 |
| `opencode_provider_contract_missing` | warning | missing | OpenCode managed contract 缺失 |
| `opencode_provider_contract_invalid` | error | invalid | managed contract 无效 |
| `opencode_provider_contract_drift` | error | drift | managed contract drift |
| `opencode_catalog_drift` | warning | drift | managed catalog drift |
| `alias_missing` | error | missing | 请求 alias 不存在 |
| `no_available_target` | error | no_available_target | 请求无可用 target |
| `alias_target_runtime_unavailable` | warning | runtime_unavailable | target 暂不可用 |
| `runtime_unreachable` | warning | runtime_unavailable | runtime 不可达 |
| `runtime_auth_failed` | error | runtime_unavailable | runtime 鉴权失败 |
| `runtime_bad_status` | error | runtime_unavailable | runtime 状态异常 |
| `runtime_parse_error` | error | invalid | runtime 响应不可解析 |
| `runtime_provider_missing` | warning | missing | runtime managed Provider 缺失 |
| `runtime_provider_protocol_mismatch` | error | protocol_mismatch | runtime 协议冲突 |
| `config_invalid` | error | invalid | 仅作尚未结构化的兼容兜底 |
| `file_parse_error` | error | invalid | 文件解析失败，不带原文 |
| `rewrite_rule_legacy` | warning | legacy | legacy Rewrite |

`rewrite_alias_selector_unresolved.params.directFallbackPossible` 必须为 true。`Providers=[]` 不产生 selector issue。singleton Provider selector 不允许 `remove_selector`；多值只在删除后仍非空时允许。

## Allowed actions

稳定枚举：`upgrade_alias`、`enable_alias`、`enable_provider`、`enable_target`、`rebind_target`、`align_protocol`、`refresh_catalog`、`retry_runtime`、`reload_runtime`、`restart_runtime`、`select_routable_alias`、`resync_opencode`、`migrate_rewrite_rule`、`replace_selector`、`remove_selector`、`disable_rule`、`remove_target`、`clear_external_value`、`remove_priority_entry`、`delete_rule`、`delete_alias`、`keep`。

按上述顺序去重。action 只表示一般诊断视图可展示，不代表授权或已执行。auto/locked Alias 未 Upgrade 时不得暴露直接 target 编辑动作；ambiguous issue 无 action；不定义 `fix_all`。重复 issue 合并时 action 取交集，不取并集。Lifecycle choice 是特定 operation 下的独立显式授权，不能由 allowedActions 推导；Provider 删除计划可在一次原子 selection 中为 locked target 提供 rebind/remove，而普通 target 编辑仍要求先 Upgrade。

## Params 与安全

`params` 是扁平对象，只允许 string、integer、boolean 及同质数组；禁止 null、float、嵌套对象、混合数组和本地化句子。空字符串应省略；数组去重并稳定排序。

永不允许：Provider/Server/Admin API key、Authorization/Cookie/Header value、原始 body/config/runtime JSON、原始 err.Error、URL userinfo/query/fragment、完整 BaseURL、revision/digest、绝对路径。允许 Provider ID、Alias、Model、Rule 名、协议 token、逻辑 path、计数与稳定状态 token，但日志仍按潜在敏感元数据处理。

## 排序、去重与版本

排序键：severity(error/warning/info)、code、path、source.kind/key、target.kind/key、reason、canonical params JSON。使用稳定字节序，不依赖 locale 或 message。

去重键含 schemaVersion/code/severity/path/source/target/reason/canonical params，不含 actions。V1 必须含 `schemaVersion:1`；缺失表示 legacy DoctorIssue。消费者忽略未知字段/code/reason/action；未知 action 不展示。改变字段类型、null 语义、既有 code 含义/severity/reason、path 规则或 action 安全语义必须提升 schemaVersion。

## Doctor 与 i18n

复用现有 `DoctorReport.issues`，`DoctorIssue` 迁移为本 schema 或等价别名，不新增平行纯文本 warning。legacy message 仅供无 schemaVersion payload 展示，不能参与排序、去重或 action 决策。

i18n keys：

```text
diagnostics.issue.<code>.title
diagnostics.issue.<code>.description
diagnostics.severity.<severity>
diagnostics.reason.<reason>
diagnostics.action.<action>
diagnostics.entity.<kind>
```

后端只返回 code/reason/params。`en`（兼容现有 en-US）与 `zh-CN` key 和 placeholder 集合必须一致；未知 code 安全 fallback，未知 action 不展示。

## Go 形状

```go
type DiagnosticIssue struct {
    SchemaVersion  int              `json:"schemaVersion"`
    Code           string           `json:"code"`
    Severity       string           `json:"severity"`
    Path           string           `json:"path"`
    Source         DiagnosticSource `json:"source"`
    Target         *DiagnosticTarget `json:"target"`
    Reason         string           `json:"reason"`
    AllowedActions []string         `json:"allowedActions"`
    Params         map[string]any   `json:"params"`
}
```

producer 必须把 nil actions/map 规范化为 `[]`/`{}`，集中校验 code/severity/reason、params 类型、action 顺序和敏感信息。

## 测试不变量

- V1 九个字段及 null/empty round-trip 固定。
- missing/disabled/protocol/catalog/runtime 分类互斥准确；静态 scanner 不读取 runtime cache。
- Target.Model 不在目录不产生强外键错误。
- wildcard Rewrite 无 issue，singleton 无 remove action，unresolved Alias 标 direct fallback。
- Priority issue 在破坏性归一化前取得原始 path。
- ownership 过滤 action，ambiguous 无 action。
- 输入顺序变化不改变结果；重复 actions 取交集。
- en/zh-CN keys 与 placeholders 一致；V1 payload 无 message/actionHint。
- 秘密 fixture 的任何片段不出现在序列化 issue。
