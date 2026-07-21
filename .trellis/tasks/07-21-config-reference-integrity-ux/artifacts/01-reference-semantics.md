# 配置引用语义基线

> 基线：2026-07-21 当前工作区源码。本文冻结引用分类、当前行为、生命周期约束和测试不变量，不定义传输 DTO。

## 语义分类

| 源 | 目标 | 分类 | 权威语义 |
|---|---|---|---|
| `Alias.Target.Provider` | `Provider.ID` | 强路由依赖 | Provider 不存在即悬空；disabled、熔断或网络失败只是暂不可用 |
| `RequestRewriteRule.Alias` | 运行时 alias 名 | 弱选择器 | 不要求存在同名持久化 Alias；direct Provider fallback 仍可能命中 |
| `RequestRewriteRule.Providers[]` | 当前候选 Provider ID | 弱集合选择器 | 非空为 OR 集合；空或缺省表示所有 Provider |
| `ProviderPriority[]` | `Provider.ID` | 排序提示 | 影响 direct fallback 与 unlocked auto target 排序，不决定实体有效性 |
| `Alias.Target.Model` | 上游模型名 | 外部符号 | 不是 `Provider.Models` 强外键；目录只用于发现、fallback 与条件式 bind |
| circuit/latency cache | Provider/URL | 运行时派生状态 | 不持久化、不阻断配置；成功提交相关变更后按实体失效 |

引用状态使用以下词汇：`missing` 表示强依赖目标不存在；`disabled`、`protocol_mismatch`、`runtime_unavailable` 表示目标存在但当前不可用；`catalog_stale` 表示外部模型目录未确认。后四者不得自动升级为 `missing`。

## Alias Target Provider

`Alias.Target.Provider` 在 manual、auto、locked、enabled/disabled 状态下都保持强依赖属性。`Config.Validate` 检查 Provider 存在性与协议兼容性；`AvailableTargets` 的运行时过滤只是防御，不会令悬空配置合法。

Provider 删除默认策略：

- unlocked auto alias 中明确由系统所有的 target 可自动删除；因本次删除而变空的纯系统 alias 可一并删除。
- manual alias、locked alias 或历史混合数据中的受保护 target 默认阻断删除，要求显式 rebind、remove target 或 delete alias。
- Provider disabled 不清引用；disabled target 仍保有强引用。
- 阻断必须发生在持久化与运行时状态变更之前。

## Rewrite 选择器

匹配公式：

```text
rule.Alias == normalizedAliasName
AND (len(rule.Providers) == 0 OR providerID IN rule.Providers)
```

- Alias 与 Provider 条件是 AND，Providers 内部是 OR。
- `Providers=[]`、缺省和归一化后的 nil 均表示所有 Provider，不表示不匹配任何 Provider。
- matcher 不按 `Target.Model` 过滤。
- 不存在的 selector 当前只是休眠，不构成强引用损坏。

当 manual/auto Alias 未命中时，代理可依据 `Provider.Models` 构造与请求模型同名的虚拟 Alias。Rewrite 仍以该 alias 名和实际 Provider 匹配。因此删除同名持久化 Alias 后，规则仍可能通过 direct fallback 生效，不得自动级联删除。

Provider selector 生命周期规则：

| 原值 | 删除 Provider 后允许的自动动作 |
|---|---|
| `[]` | 保持 wildcard 不变 |
| 不包含目标 | 不变 |
| 多值且移除后仍非空 | 可移除目标，只收窄范围 |
| singleton 目标 | 不得清成 `[]`；保留休眠、显式禁用、删除或替换 |

任何自动动作必须满足作用域不扩大。

## Provider Priority

Load/Set 会过滤空、未知和重复 ID，并补齐现存 Provider；Set 还会重排 unlocked auto alias targets。Provider 删除成功时应在同一事务中移除 Priority 项并持久化。该清理是安全规范化，不得改变 manual、locked 或 mixed alias 的 target 顺序。

## Target Model

`Target.Model` 是发往上游的外部符号。`Config.Validate` 与 `AvailableTargets` 不要求它属于 `Provider.Models`。只有交互式 bind 在可信 `ModelsSource=discovered` 且目录非空时执行条件式准入。

- 目录刷新失败、为空或减集时不得把现有 manual/locked target 判为悬空。
- 目录成员变化影响 direct fallback 候选、可信目录下的交互式 bind 准入和系统 auto alias reconcile，但不影响既有 manual/locked target 的结构有效性。
- 可信非空减集可用于 auto ownership reconcile，但不能建立全局模型外键。

## 运行时派生状态

熔断状态按 strategy/protocol/Provider ID 存于内存；BaseURL 延迟缓存按 Provider/URL 存于内存。它们不进入配置 revision、引用诊断或删除阻断。Provider 删除或连接身份实质变化成功提交后，应仅失效相关状态；被阻断或失败的操作不得提前清理。

## 禁止的错误假设

- 不得把所有字符串引用当成外键。
- 不得因 `AvailableTargets` 能过滤就允许持久化缺失 Provider。
- 不得把 singleton Rewrite selector 自动清成 wildcard。
- 不得断言 Alias 删除后同名 Rewrite 必然失效。
- 不得把 `Target.Model` 目录缺失设为全局 hard error。
- 不得把 disabled、熔断、网络失败或 stale catalog 分类为实体缺失。
- 不得把 Priority 或 runtime cache 纳入强引用图。
- 不得把 Provider 上游 `APIKey/APIKeys` 误作未来下游 scoped API key 实体。

## 源码证据

| 路径 | 关键符号 |
|---|---|
| `internal/config/config.go` | `Target`, `Alias`, `Provider`, `RequestRewriteRule`, `Validate`, `AvailableTargets`, `FindProvidersByModel`, `SetProviderPriority`, `RemoveProviderAutoTargets` |
| `internal/config/rewrite_ops.go` | `ApplyRequestRewriteRules` |
| `internal/proxy/server.go` | `handleProtocolRequest`, `ReloadConfig`, circuit reset 与 latency cache |
| `internal/app/manage.go` | `RemoveProvider`, `RemoveAlias`, `BindAliasTarget`, discovered model 校验 |
| `internal/routing/routing.go` | `StateKey`, `StateStore` |
| `internal/routing/memory_store.go` | `MemoryStateStore` |

## 可测试不变量

- 缺失、禁用、协议不匹配、catalog stale 和 runtime unavailable 产生不同分类。
- manual/locked target 存在时，默认 Provider 删除在写盘前阻断，磁盘与 runtime 不变。
- `Providers=[]` 匹配 alias 下任意 Provider；singleton 删除计划永不变成 `[]`。
- direct fallback 构造的虚拟 alias 可命中 Rewrite Alias selector。
- Provider 删除成功后 Priority 不再含该 ID，manual/locked 顺序不变。
- 显式 Target.Model 不在目录中仍可通过结构校验并用于上游请求。
- circuit/cache 不出现在序列化配置或引用诊断中；提交失败不清状态。
