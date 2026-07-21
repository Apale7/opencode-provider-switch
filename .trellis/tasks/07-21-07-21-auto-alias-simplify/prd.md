# 自动别名与配置简化

## Summary

引入自动别名生成机制和直接 provider 回退路由，让 alias 配置从必填项变成可选项。用户只需配置 provider 的 API+Key 即可使用，系统自动发现模型并生成 alias。高级用户可切换到手动模式精细控制路由。

## Problem

当前 ocswitch 对供应商最大粒度的分类是协议，多个 provider 只要协议一样就可以通过 alias 把请求模型路由到多个 provider。但每引入一个新的 provider，用户都需要手动配置 alias 和 target 绑定。

竞品 aio 只需要配置 provider 的 API 和 Key 即可使用，配置量极小。用户更乐于使用 aio 因为它配置简单。

ocswitch 的劣势：配置过于复杂，每引入一个新 provider 都需要配置 alias。
ocswitch 的优势：alias 路由更灵活，暴露 model 给用户更多选择，且模型感知路由可避免无效熔断。

## Goals

1. **自动别名生成**：Provider 保存/刷新模型时自动创建/更新 alias，用户无需手动配置 target 绑定
2. **Provider 优先级**：支持拖拽排序 provider 优先级，自动 alias 的 target 按优先级排列
3. **直接回退路由**：无 alias 匹配时直接从 provider models 搜索路由，实现零配置可用
4. **手动模式保留**：现有 alias 管理功能保留为"高级模式"，手动 alias 不受自动逻辑影响
5. **向后兼容**：旧配置文件中的 alias 自动识别为手动模式，行为不变
6. **i18n 覆盖**：所有新增 UI 文案提供中英文双语

## Non-Goals

1. 不改变现有的熔断器（circuit-breaker）路由策略
2. 不改变协议匹配逻辑
3. 不引入新的持久化存储（仍使用 JSON config 文件）
4. 不改变 OpenCode sync 的输出格式
5. 不改变 request rewrite rules 的行为

## Architecture

### 三层路由 fallback

```
请求 model="gpt-4o"
  │
  ├─ 1. FindAlias(model) ──── 手动 alias（现有逻辑，优先级最高）
  │   找到 → 使用手动 targets 路由
  │
  ├─ 2. FindAutoAlias(model) ─ 自动 alias（新增）
  │   找到 → 使用自动 targets 路由（按 provider 优先级排序）
  │
  └─ 3. FindProvidersByModel(model) ─ 直接 provider 回退（新增）
      找到 → 构造虚拟 targets，按 provider 优先级路由
      未找到 → 404
```

### 自动 alias 生成规则

触发时机：`UpsertProvider` / `RefreshProviderModels` 后

生成逻辑：
- 遍历 provider.Models 中每个 model `m`：
  - alias `m` 不存在 → 创建自动 alias（protocol=provider.Protocol，targets=[{Provider, m}]）
  - alias `m` 已存在且为自动且未锁定且协议匹配 → 追加 target（若该 provider+model 组合不存在）
  - alias `m` 已存在且为手动或已锁定 → 不修改

### Provider 优先级

- Config 新增 `provider_priority` 字段：按优先级排序的 provider ID 列表
- 自动 alias 的 targets 按 `provider_priority` 排序
- 直接回退路由按 `provider_priority` 选择 provider
- UI 支持拖拽排序调整优先级

## Acceptance Criteria

1. 新建 provider（含模型自动发现）→ 自动创建对应 alias
2. 已有自动 alias 的 provider 更新模型 → 自动追加/更新 targets
3. 手动 alias 不受自动生成逻辑影响
4. 锁定自动 alias → 后续 provider 更新不修改该 alias
5. Provider 优先级拖拽排序生效，自动 alias target 顺序随之更新
6. 删除 provider → 自动清理其自动生成的 targets（空 alias 一并删除）
7. 无 alias 场景：直接 provider 回退路由正常工作
8. 全局自动 alias 开关关闭时，不触发任何自动生成
9. 所有新增 UI 文案有中英文 i18n 覆盖
10. 旧配置文件向后兼容，行为不变

## Verification

- `go build ./...` 通过
- `go test ./internal/config/...` 通过
- `go test ./internal/proxy/...` 通过
- `go test ./internal/app/...` 通过
- `npm run build` 通过
- 手动冒烟：新建 provider → 自动 alias 生成 → 请求路由成功
