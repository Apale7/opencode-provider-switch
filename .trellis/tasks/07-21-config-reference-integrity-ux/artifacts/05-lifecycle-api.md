# 配置生命周期 API V1

> 本文冻结领域层 revision、preview/plan/execute 与提交结果；不定义 HTTP/Wails/CLI/TUI 映射。

## 状态机

```text
revisioned snapshot -> preview -> immutable plan
  -> resolve explicit choices -> preview again
  -> execute exact plan token -> persist -> runtime apply/pending
```

Preview 与 Execute 必须使用同一 planner、规范化器、typed diagnostics 和 error-returning candidate runtime builder。未解决 blocker/required choice 不签发 token。Execute 锁内重读 latest 并 CAS；stale 不写盘、不 reload、不清 runtime state。

## Revision

`ConfigRevision` 是规范配置路径作用域内、基于原始持久化字节计算的 keyed digest。ConfigStore 首次使用时原子生成并以 0600 保存 path-scoped revision key sidecar；协作进程在同一文件锁下读取同一 key。revision 使用 HMAC-SHA-256（domain + canonical path identity + exact bytes），使客户端无法离线枚举低熵凭据。任意字节变化均改变 revision；missing 与 empty 使用不同 domain；revision 不写入 JSON、不泄露路径或凭据。key sidecar异常是持久化前 I/O 阻断，不得退回无密钥摘要。

Planner 基于将实际写入的精确候选字节计算 `candidateRevision`。字节级 no-op 保留原始字节且 candidate=base，不格式化重写。

## 领域 DTO

```go
type ConfigRevision string
type LifecyclePlanToken string

type LifecycleOperation struct {
    Kind string `json:"kind"`
    Payload json.RawMessage `json:"payload"`
}

type LifecyclePreviewInput struct {
    Revision ConfigRevision `json:"revision"`
    Operation LifecycleOperation `json:"operation"`
    Selections []LifecycleSelection `json:"selections"`
    PreparationToken string `json:"preparationToken,omitempty"`
}

type LifecyclePlan struct {
    ContractVersion string `json:"contractVersion"`
    PlannerVersion string `json:"plannerVersion"`
    BaseRevision ConfigRevision `json:"baseRevision"`
    CandidateRevision ConfigRevision `json:"candidateRevision,omitempty"`
    OperationKind string `json:"operationKind"`
    Executable bool `json:"executable"`
    NoOp bool `json:"noOp"`
    PlanToken LifecyclePlanToken `json:"planToken,omitempty"`
    ExpiresAt *time.Time `json:"expiresAt,omitempty"`
    RequestedChanges []LifecycleChange `json:"requestedChanges"`
    AutomaticChanges []LifecycleChange `json:"automaticChanges"`
    SelectedChanges []LifecycleChange `json:"selectedChanges"`
    Blockers []LifecycleIssue `json:"blockers"`
    Choices []LifecycleChoice `json:"choices"`
    PreservedIssues []LifecycleIssue `json:"preservedIssues"`
    RuntimeImpact LifecycleRuntimeImpact `json:"runtimeImpact"`
}

type LifecycleExecuteInput struct {
    Revision ConfigRevision `json:"revision"`
    PlanToken LifecyclePlanToken `json:"planToken"`
    Operation LifecycleOperation `json:"operation"`
    Selections []LifecycleSelection `json:"selections"`
    PreparationToken string `json:"preparationToken,omitempty"`
}

type LifecycleExecuteResult struct {
    ContractVersion string `json:"contractVersion"`
    BaseRevision ConfigRevision `json:"baseRevision"`
    CommittedRevision ConfigRevision `json:"committedRevision"`
    RuntimeRevision *ConfigRevision `json:"runtimeRevision"`
    Persisted bool `json:"persisted"`
    WritePerformed bool `json:"writePerformed"`
    Changed bool `json:"changed"`
    NoOp bool `json:"noOp"`
    CandidateAlreadyPresent bool `json:"candidateAlreadyPresent"`
    RuntimeApplied bool `json:"runtimeApplied"`
    PendingRestart bool `json:"pendingRestart"`
    RuntimeState string `json:"runtimeState"`
    ApplyStates []LifecycleApplyState `json:"applyStates"`
    Issues []LifecycleIssue `json:"issues"`
}

func (s *Service) PreviewLifecycle(context.Context, LifecyclePreviewInput) (LifecyclePlan, error)
func (s *Service) ExecuteLifecycle(context.Context, LifecycleExecuteInput) (LifecycleExecuteResult, error)
```

Operation kinds 至少覆盖 Provider upsert/refresh/import/state/remove/priority/auto setting，Alias upsert/remove/bind/state/unbind/reorder/upgrade，Rewrite upsert/state/remove/reorder，global auto setting/reconcile，Config import patch。Kind 与 payload 必须一一匹配。

`LifecycleChange` 必须含稳定 ID、kind(add/remove/update/reorder)、source(requested/automatic/selection)、entity、reasonCode 与脱敏 field diffs。LifecycleIssue 必须含 `disposition: blocker|required_choice|preserved`，由 planner policy 决定，不能从 diagnostic severity 推导。Issue/Choice 使用稳定 code+params，不含本地化文案。Choice option 只表达具体动作，不提供 force/ignore/continue anyway。

## Plan 规则

- `requestedChanges`：调用者直接要求的变更。
- `automaticChanges`：仅 L0 安全动作，如清 system target、因本次清理变空的 pure auto alias、Priority ID、多值 Rewrite selector 的安全收窄、确定性规范化。
- `selectedChanges`：用户通过 choice 指定的 rebind/remove/delete/disable/replace。
- `blockers`：disposition 为 blocker 或尚未解决的 required_choice，阻止 execute。
- `preservedIssues`：候选刻意保留的非阻断问题，如 singleton selector 休眠、direct fallback 可能命中、catalog stale、runtime unavailable、unknown field preserved。

有 blocker 或 required choice 未解决时 `executable=false` 且无 token。任何输入或 selection 改变都必须重新 Preview。Execute 不得添加计划外动作。

Plan token 必须防篡改并绑定 contract/planner version、path scope、base/candidate revision、operation、selections、prepared facts、public plan digest 与 expiry；不包含配置原文或秘密。Token 不是鉴权凭据。planner 版本变化或首次提交尝试时 token 已过期要求重新 Preview。

## Execute 与幂等

Execute 锁内流程先验证 token 并读取 current revision，然后分支：current=base 时从 base 快照使用同 planner 重算，比较 plan/candidate digest，Validate、candidate runtime build、atomic persist并 apply；current=candidate 时不得重放 mutation 或基于 candidate 再运行删除等 planner，只验证 token MAC、path scope、candidate revision、plan digest 与有效期，然后构建/应用当前磁盘 candidate 以收敛 runtime；其他 revision 立即 conflict。

- 当 base=candidate=current（字节级 no-op）时，优先走 candidate-already-present 分支，不运行 planner、不写盘；`NoOp=true`、`CandidateAlreadyPresent=true`、`Changed=false`。
- current=base 且 candidate!=base：可执行 planner 与提交。
- current=candidate：候选字节已存在，不声称该 token 曾执行；不写盘，可在 token 仍有效且 plan digest 一致时显式重试 runtime 收敛，并设置 `candidateAlreadyPresent=true`。
- 其他：`revision_conflict`，零变化。
- operation/selections/preparation/planner 不匹配：`plan_mismatch`。
- 同一路径 mutation sequencer 必须持续到 apply disposition 已记录，旧 revision 不得晚于新 revision覆盖 runtime。

## 生命周期策略

### Provider remove

自动动作：删除 unlocked auto alias 内 `Target.AutoGenerated=true` 的目标；仅删除因本次清理变空的 pure auto alias；清 Priority；多值 Rewrite selector 移除目标且剩余非空。Wildcard 保持 wildcard；singleton 原样保留休眠，绝不清成 empty=all。

manual、locked、mixed protected target 默认 required choice：rebind、remove target 或 delete alias。不得提供“保留悬空 target 并继续”。这些 lifecycle choices 独立于通用 diagnostic actions。singleton Rewrite 同样产生 required choice：必须显式选择 keep dormant、disable、delete 或 replace non-empty providers；未选择不签发 token。

### Alias 与 Rewrite

普通 Alias mutation 先 any-alias lookup。auto/locked 必须先 Upgrade；拒绝发生在任何候选变更前。Upgrade 原子清 Alias AutoGenerated/Locked 与全部 target AutoGenerated，其他字段和顺序不变。Alias remove 不自动删 Rewrite；报告 direct fallback 与 OpenCode 外部弱引用。

Rewrite 保持 `Alias AND (Providers wildcard OR membership)`；未知 selector 非阻断休眠；自动动作永不扩大 scope。

### Auto alias/discovery

开关只改变未来 reconcile，toggle 零 Alias diff。skip/error/empty/不完整观察保留目录且零 Alias diff。当前首个非空 discovery 不足以证明完整，不能 prune。只有可信完整非空观察可维护 pure system target；manual/locked/unknown 保留；mixed 顺序不变；重复 reconcile 空 diff。

### Provider disabled

不清 target/Rewrite/ownership，不触发 auto prune/generate/sort。disabled 是可用性诊断，不是 missing。

## Import patch 兼容

Import 使用 presence-aware parser，重复 JSON key 阻断。全部 typed 顶层字段的显式 `null` 均拒绝；数组只接受数组、对象只接受对象、boolean 只接受 boolean。`server/providers/aliases` 缺失拒绝；`admin/desktop/request_rewrite_rules/provider_priority/auto_alias_enabled` 缺失保留目标现值。显式 `[]` 清 Rewrite/Priority；显式 false 关闭全局 auto。普通 Load 缺 auto 字段才默认 true。顶层对象为 replace，不做任意递归 merge，仅以下 key 有兼容例外。

Import 的 `server.api_key` 缺失时应用既有默认 `ocswitch-local`，不保留目标旧值；显式空保留空。admin 整体缺失时保留现值；admin 对象存在但 api_key 缺失或空时保留目标旧 key，显式非空替换。failover 字段缺失使用既有默认，显式空列表保持空；嵌套显式 null 一律拒绝。

Unknown field 目标策略为 preserve：普通 mutation 保留；Import 缺失保留、显式同名替换；实体删除时随实体删除；identity ambiguous 阻断。Export 覆盖八个 typed 顶层字段并视为敏感备份。

## 提交结果真值表

| 场景 | persisted | writePerformed | runtimeApplied | pendingRestart | runtimeState |
|---|---:|---:|---:|---:|---|
| no-op，runtime 一致 | true | false | true | false | already_applied |
| 写入+hot apply | true | true | true | false | applied |
| candidate 已存在且 runtime 一致 | true | false | true | false | already_applied |
| candidate 已存在且 runtime 落后，收敛成功 | true | false | true | false | applied |
| candidate 已存在且为 restart-only | true | false | false | true | restart_required |
| no-op/candidate 已存在，proxy 未运行 | true | false | false | false | not_running |
| candidate 已存在，runtime 收敛失败 | true | false | false | false | apply_failed |
| no-op 且 runtime 落后，收敛成功 | true | false | true | false | applied |
| no-op 且 runtime 落后，收敛失败 | true | false | false | false | apply_failed |
| 写入，proxy 未运行 | true | true | false | false | not_running |
| restart-only | true | true | false | true | restart_required |
| post-commit apply 失败 | true | true | false | false | apply_failed |
| pre-commit failure | 无 execute result；零变化 |

`runtimeApplied=true` 仅当运行中的 runtime revision 等于 committed。`pendingRestart` 只表示 restart-only 变更；apply failure 使用 `runtimeState=apply_failed` 与 apply issue 表达，不得与 restart-only 混合。post-commit apply failure 不自动回滚磁盘，必须以 typed error 携带完整 result；candidate 已存在后的收敛失败 `writePerformed=false`。runtime cache 只在成功提交后的 apply 阶段定向失效。

布尔优先关系：`NoOp` 只表示 candidate 字节等于 base；`CandidateAlreadyPresent` 表示 Execute 开始时 current 等于 token candidate，二者可以同时为 true。`Changed = !NoOp`，与本次是否写盘无关。`already_applied` 仅用于调用开始时 runtime 已等于 committed；本次成功收敛使用 `applied`。

## 领域错误与安全

稳定 code：`revision_conflict`、`plan_mismatch`、`plan_expired`、`plan_not_executable`、`preparation_stale`、`candidate_invalid`、`runtime_candidate_build_failed`、`persist_failed`、`runtime_apply_failed`。仅最后一项可能已持久化并必须携带 result。

Plan/diff/params 不得包含 API key、Header value、Import 原文、完整候选、token、可比较秘密 hash。敏感字段只显示 present/redacted/count。

## 测试不变量

- preview 零副作用；current=base 且 candidate!=base 时 preview/execute 使用同 planner且计划完全一致；current=candidate（含 no-op）分支 planner 调用次数为零，只验证 token/candidate 后收敛 runtime。
- stale、token mismatch、candidate invalid/build failure/persist failure 均零写盘、零 reload、零 cache 清理。
- current=candidate 表示候选已存在，不写盘且可显式收敛 runtime，不声称 token 曾执行。
- Provider remove 自动动作不删 protected target、不扩大 Rewrite scope。
- Alias remove 保留 Rewrite；direct fallback 语义有测试。
- Upgrade 只改变 ownership 标志。
- discovery error/empty/首个非空均不 destructive prune。
- Import presence、显式空、八字段 parity 和 unknown preserve 有 fixture。
- post-commit apply failure 明确 persisted=true/runtimeApplied=false。
- 所有返回值、日志与 token 均不泄露秘密。
