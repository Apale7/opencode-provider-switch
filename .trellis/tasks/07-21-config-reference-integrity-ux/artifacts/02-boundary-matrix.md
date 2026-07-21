# 配置边界矩阵

> 基线：2026-07-21 当前工作区源码。本文冻结各写入与运行边界的现状、目标阻断规则和 revision/CAS 约束，不冻结 Step 2 DTO 字段。

## 核心事实

1. `Config.Save` 只保证单次文件替换原子；文件锁不覆盖此前的 Load、mutation、Validate 和 runtime apply。
2. 两个独立 Config 快照可依次保存，后一个 stale writer 会整份覆盖前一个结果。
3. 多数 Service mutation 为 `Load -> mutate -> Save -> ReloadConfig`。Reload 失败不回滚磁盘，形成新磁盘/旧 runtime 分裂。
4. Service 没有运行 proxy 时 reload helper 直接成功，同一无效候选可能停机时写入、下次 Start 才失败。
5. CLI 多个写命令直接 `Config.* + Save`，绕过 Service 生命周期、auto ownership 与 runtime apply。
6. Import/Export 遗漏 `ProviderPriority` 与全局 `AutoAliasEnabled`；未知字段会在后续 Save 丢失。
7. 当前无主配置 revision、CAS、ETag 或磁盘/runtime revision 对照。

## 边界矩阵

| 边界 | 当前行为 | 目标行为 |
|---|---|---|
| `Config.Load` | 无文件锁；missing/empty 返回默认；内存静默归一化 Priority/routing 等；忽略未知字段 | mutation 仅在 ConfigStore 锁内读取 latest；快照带 revision 与诊断；未知字段 preserve 或显式拒绝 |
| `Config.Save` | Config mutex + path lock；已知字段快照原子替换；不调用完整 Validate | 仅保留低层序列化职责；所有写入口必须由 ConfigStore 调用，不加入全局引用 hard block |
| Provider Upsert/Refresh/Import | 锁外 Load/mutate/discovery；Save 后 reload | 网络准备长锁外且绑定 revision；锁内 CAS、确定性 merge、policy 与 candidate runtime build |
| Provider Disable | 可让 alias 无可用 target；Save 后 reload 可能失败 | 统一事务；disabled 作为可用性诊断，不按删除处理 |
| Provider Remove | 只清 unlocked auto alias；不清 manual/locked、Priority、Rewrite | revision-bound lifecycle plan；安全清系统项与 Priority；manual/locked 默认阻断 |
| Alias Upsert/Upgrade/Target CRUD | 所有权门禁不一致；unbind/disable 可写出无可用 target | any-alias lookup 后统一 ownership policy；候选提交前完成操作策略检查 |
| Alias Remove | 无影响计划；Save 后 reload | 报告 Rewrite selector 与 OpenCode 外部弱引用；不自动按强外键级联 |
| Rewrite CRUD | 部分路径 Save 前 Validate，部分没有；Save 后 reload | 统一事务；selector 保持弱引用语义，清理不扩大 scope |
| Priority/Auto settings | fresh Load 后整份 Save | 统一事务；Priority 仍是排序提示 |
| Proxy settings | Save 后只给 restart warning，不 reload | 区分 hot apply 与 restart-only，明确 persisted/applied/pending |
| Desktop prefs | Save 后应用 native setting，后者失败不回滚 | 明确磁盘提交与 native apply 阶段及恢复方式 |
| Export | 漏两个顶层字段；包含秘密值 | 当前 typed 字段完整 round-trip，明确敏感备份属性 |
| Import | Save 前完整 Validate；无 CAS；不 hot reload | 构造完整候选、CAS、字段 parity；按字段明确 hot apply 或 pending restart |
| `StartProxy` | fresh Load 后严格 Validate；失败不启动 | 使用统一诊断与 candidate build；管理面和 runtime 状态可区分 |
| `ReloadConfig` | Validate 后调用 `newServerRuntime` 并 atomic swap；routing 构建会 fallback，默认策略仍失败时可能 panic，当前不是完整 error-returning builder | 新增可返回错误的 candidate runtime builder，在 persist 前证明可构建；意外 post-commit 失败必须明确分裂状态 |
| Doctor | 只读；Validate 错误折叠；无法识别 disk/runtime split | 共享 typed 原因，报告磁盘与 runtime revision 差异，不提供 Fix All |
| OpenCode Sync | 主配置与 target 均缺少 preview/apply 双 revision；空可路由集合可能 destructive prune | 同时绑定主配置和 target revision；临时空集合不 prune；只修改授权 subtree |
| `ensureAdminConfig` | Load 后补 key/override 并 Save；后续 bind/start 失败不回滚 | 接入 ConfigStore；分别表达 config commit、admin bind 与 proxy start |
| HTTP | 普通业务错误统一 400 string | stale revision 映射 409；稳定错误语义与其他入口一致 |
| Wails/TUI | 转发 Service，但没有 typed revision/result | 使用同一 revision/plan/result 语义 |
| CLI | 大量直接 Config write；无法应用其他进程 runtime | 全部写命令走 Service/ConfigStore；无 IPC ack 时只报告未应用/需重启 |
| 外部编辑器 | 无 watcher；可造成磁盘/runtime drift；下次 Save 可覆盖 | revision 冲突与 drift 可观测；不宣称能绝对隔离不协作编辑器 |

## 典型分裂流程

```text
D0/R0
  -> 锁外 Load 与 mutation 得 D1
  -> Save 原子提交 D1
  -> ReloadConfig(D1) 严格校验失败
  -> 磁盘 D1，runtime R0，调用方仅收到普通 error
```

可由删除仍被 manual/locked target 引用的 Provider、禁用最后可用 Provider、unbind 最后 target、协议修改等触发。目标方案必须在持久化前构建可应用候选；罕见 post-commit apply 失败不得伪装成“未保存”。

## 阻断级别

| 级别 | 语义 | 示例 |
|---|---|---|
| L3 持久化前阻断 | 候选不可安全解析、验证、构建或 revision 冲突 | JSON/I/O、非法 identity/protocol/routing、candidate runtime build 失败、CAS mismatch |
| L2 需显式生命周期计划 | 合法但具有破坏性或 scope 扩大风险 | manual/locked Provider 引用、Alias 外部影响、singleton Rewrite selector |
| L1 非阻断诊断 | 不证明实体缺失 | disabled、runtime unavailable、catalog stale、restart required、保留的 unknown field |
| L0 安全自动动作 | 确定性且不扩大语义 | 清 unlocked system target、移除 Priority ID、稳定排序 |

## Revision/CAS 约束

- revision 为 path-scoped、不透明的原始持久化字节强摘要，不写入 JSON，不泄露凭据；missing 使用固定 sentinel。
- ConfigStore 在同一 path lock 内完成：读取 latest、计算/比较 revision、解析、clone/mutate、operation policy、通过新增的 error-returning candidate runtime builder 验证、原子 persist、生成 committed revision；不得直接依赖当前可能 panic/fallback 的 `newServerRuntime` 充当验证器。
- preview/plan 必须绑定 revision；stale execute 不写盘。HTTP 为 409，其他入口提供等价冲突语义。
- discovery/probe 不长期持锁；结果绑定输入与观察 revision，最终提交时复核。
- runtime 记录所应用的 committed revision；Doctor 可比较 disk/runtime revision。
- restart-only 字段必须显式 pending，不得声称 runtimeApplied。
- OpenCode 使用独立 target revision；Sync apply 同时校验主配置与 target revision。
- `.lock` 只对协作写者提供强保证；不承诺消除任意外部编辑器的检查后竞争。

## Import/Export parity

当前完整配置顶层字段为：`server`、`admin`、`desktop`、`providers`、`aliases`、`request_rewrite_rules`、`provider_priority`、`auto_alias_enabled`。后两项当前未被 Export/Import 覆盖，必须补齐。

当前 Import 是兼容合并而非所有字段强制替换：只强制输入包含 `server/providers/aliases`；缺失 `admin`、`desktop`、`request_rewrite_rules` 时保留目标配置现值；输入 `admin.api_key: ""` 时也保留旧 key。ConfigStore 接入不得无意改变这些语义；若后续决定废弃，必须作为显式迁移与契约变更处理。

兼容规则：

- 普通主配置 Load 缺失全局 `auto_alias_enabled` 时继续默认为 true；Import patch 缺失该字段时保留目标配置现值。Import patch 缺失 `provider_priority` 时同样保留目标配置现值，只有显式提供时才替换。
- `server.api_key` 缺失与显式空保持不同语义。
- failover code 的 nil/default 与显式空列表保持不同语义。
- Rewrite `Providers=[]` 继续表示所有 Provider。
- Rewrite 保持用户顺序；Provider/Alias 保持现有规范排序。
- unknown 主配置字段必须 preserve 或显式拒绝，不得在无关 Save 中静默丢失。
- Export 继续视为包含 Provider/Admin API key 的敏感备份。

## 源码证据

| 路径 | 关键范围 |
|---|---|
| `internal/config/config.go` | `Load`, `Save`, `Validate`, priority normalization |
| `internal/fileutil/fileutil.go` | path lock 与 atomic write |
| `internal/app/manage.go` | Provider/Alias/Rewrite CRUD |
| `internal/app/service.go` | Start/Reload helper、settings、Doctor、Sync |
| `internal/app/config_transfer.go` | Import/Export |
| `internal/proxy/server.go` | runtime build/atomic swap |
| `internal/server/server.go` | bootstrap/ensureAdminConfig |
| `internal/cli/provider.go`, `alias.go`, `rewrite.go` | 直接写入口 |
| `internal/webadmin/handler.go` | HTTP 错误映射 |
| `internal/opencode/opencode.go` | target Load/Save 与 managed subtree patch |

## 验收不变量

- 两个 stale writer 中后者冲突，不覆盖前者。
- stale lifecycle plan 不写盘。
- 可预见的 Validate/runtime-build 错误发生在 persist 前。
- 注入 post-commit apply failure 时明确返回磁盘已提交/runtime 未应用。
- 全部 Service、CLI、TUI、HTTP、Wails 与 bootstrap 写入口不能绕过 ConfigStore。
- 八个 typed 顶层字段完整 round-trip，unknown 字段不 silent drop。
- Doctor 可识别 disk/runtime revision 不同。
- OpenCode Sync 检查双 revision，临时空目录不清空 managed models。
