# 配置提交一致性、引用完整性与删除体验

## 目标

近期自动 Alias、多上游 API Key、多 BaseURL、模型发现与同步能力落地后，当前首要风险已从单纯的“悬空字符串”升级为配置提交一致性问题：管理操作可能先写盘，再因热重载严格校验失败而返回错误，导致磁盘配置与运行中代理使用不同快照；并发写入也可能因 Load 与 Save 分离而丢失更新。本任务重新冻结引用语义、生命周期和管理入口契约，确保修改可预览、可冲突检测、可诊断且不会扩大规则作用域。

## 当前事实

- `Service.RemoveProvider` 仍只额外清理未锁定自动 Alias target，随后先 `Save` 再 `ReloadConfig`；manual alias 或锁定 Alias 内 target 可令 reload 失败，而磁盘已经提交。
- CLI 的 Provider/Alias/Rewrite 删除仍直接修改 Config 并 Save，绕过 Service、自动 Alias 清理和运行时应用。
- `Config.Save` 只保证单次原子写；文件锁不覆盖 Load→Mutate→Save，当前没有 revision、ETag 或 stale writer 冲突检测。
- `Alias.Target.Provider` 是强路由依赖；`Target.Model` 是外部上游模型符号，不是 `Provider.Models` 持久化强外键，但可信 discovered 目录仍用于交互式 bind 准入校验。
- `RequestRewriteRule.Alias` 与 `Providers[]` 是匹配选择器；空 `Providers` 明确表示所有 Provider，清理最后一个 ID 会扩大规则作用域。Alias selector 在 direct Provider fallback 中仍可能匹配。
- ProviderPriority 是排序提示，Load/Set 会过滤未知 ID，但删除后的 Save 可能保留原始悬空值并由下次 Load 隐藏。
- 运行时已过滤缺失、禁用、协议不匹配的 Target；无可用 Target 主要返回 404，而不是必然 panic/500，但错误、trace 与 Doctor 的原因不够精确。
- 完整配置 Import 已调用 Validate；当前 import/export 快照遗漏 `ProviderPriority` 与全局 `AutoAliasEnabled`。
- 现有 `DoctorIssue`、删除确认和中英文 i18n 可复用，但没有删除 impact、revision、typed reference reason 或修复 action 契约。
- 当前只有 Provider 上游 `APIKey/APIKeys` 和单个代理访问 key；未来下游 API key scope 尚未实现。

## 范围

- 配置提交事务：revision/digest、同锁读改写、候选 runtime 校验、明确的持久化/应用结果和 409 冲突。
- 当前实体的 typed diagnostics：Provider、Alias/Target、Request Rewrite Rule、Provider Priority、完整导入导出及 OpenCode 外部弱引用。
- Provider/Alias/Rewrite 生命周期 preview/plan/execute；manual alias、unlocked auto alias、锁定 Alias 内 target 与 Upgrade 后 manual 的所有权；模型目录陈旧性分级。
- Service、HTTP/Wails、CLI、React Web/Desktop 和 TUI 的统一契约、i18n、Doctor/trace/日志诊断与测试。

## 非目标

- 不实现尚不存在的下游 API key scope、额度、过期或授权过滤。
- 不把 Provider.Models 变成 Target.Model 的本地强外键。
- 不提供通用 `CleanDanglingRefs`、`Fix All` 或启动时静默自愈。
- 不把 disabled、熔断、网络失败或模型目录暂时为空当作实体删除。
- 不在本设计修订任务中修改 Go、TypeScript、TUI、生成代码或配置格式。

## 产品要求

1. 所有配置 mutation（包括 CRUD、Priority、auto-alias settings、Provider 状态/导入、Proxy settings、Desktop prefs 与完整导入）通过统一事务入口执行；stale revision 返回冲突，失败结果明确 `persisted` 与 `runtimeApplied`。
2. 删除 Provider 前展示自动 target、manual alias target、锁定 Alias 内 target、Priority、Rewrite selector、外部 OpenCode 选择和运行时派生状态影响。
3. 自动 Alias 可自动维护；manual alias、锁定 Alias 内数据或来源不明的数据默认不自动删除。未升级 auto alias 不允许被 edit/bind 静默替换。
4. Rewrite selector 清理不得扩大匹配范围；唯一 Provider selector 不得被转换为 empty=all。
5. Alias selector 不按严格外键处理；删除 Alias 时报告 direct fallback 语义和外部选择影响，由用户选择保留、禁用或删除规则。
6. 完整导入导出保持所有当前顶层字段；导入、启动、reload、sync 和 Doctor 使用同一诊断 code/reason。
7. React Web/Desktop、HTTP/Wails、CLI 与 TUI 共用 preview/execute/revision 契约；所有新增 UI 文案覆盖 `zh-CN`/`en`。
8. 运行时保持现有过滤和 failover，同时修复本地错误的 trace status，并提供 alias_missing、no_available_target、protocol_mismatch 等准确原因。

## 交付标准

- 详细设计只有 `docs/design-plans/config-reference-integrity-ux.md` 一个权威来源；旧版重复方案不纳入实现。
- 当前 Trellis design task 在方案冻结和实现任务 DAG 产出后完成；后续 implementation task 按契约冻结、ConfigStore/diagnostics、生命周期 planner、入口集成、运行时诊断、绑定/适配、UI/i18n、验证、独立评审的顺序执行。
- 自动化测试覆盖并发丢写、磁盘/runtime 原子性、selector 不扩大、auto alias ownership、导入导出 round-trip、运行时原因，以及 HTTP/Wails/CLI/TUI/React 多端契约。
- 本次设计重审只修改本文、task.json 与权威详细设计，不修改产品代码。
