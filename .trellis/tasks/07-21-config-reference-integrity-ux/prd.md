# 配置引用完整性与删除体验

## 目标

用户删除 Provider 后，相关 Alias Target、Request Rewrite 和 Provider Priority 不应继续以悬空字符串存在，更不应等到下一次请求才以难以定位的错误暴露。用户需要在删除前看到影响范围，删除后看到清理摘要，并能从诊断信息定位仍需人工处理的配置。

## 已确认的现状

- `Config.RemoveProvider` 只删除 Provider 条目。
- Service 删除 Provider 时调用 `RemoveProviderAutoTargets`，只处理未锁定自动 Alias；手动/锁定 Alias Target 会残留。
- `Config.Save` 负责归一化和原子写入，但不主动调用 `Validate`。
- `Validate` 已检查 Alias Target 指向的 Provider，但未覆盖 Request Rewrite 的 Alias/Providers 引用完整性。
- 删除 Alias 不检查其 Request Rewrite 引用，规则会静默不匹配。
- ProviderPriority 的未知 ID 会在排序时被过滤，但删除操作没有明确清理与反馈。
- 前端和 TUI 有删除确认，但没有影响预览；新增 UI 文案必须接入现有中英文 i18n。

## 范围

本任务只设计当前代码中已存在的 Provider、Alias/Target、Request Rewrite Rule、Provider Priority 及其导入、保存、启动、热重载、运行时路由行为。Provider 的上游 `APIKey/APIKeys` 是连接凭据，不把未来的下游 API key scope、配额、过期策略等规划内容提前纳入。

## 非目标

- 不在本任务中实施 Go、TypeScript、TUI 或配置格式代码。
- 不重新设计 Provider、Alias 或 Request Rewrite 的业务模型。
- 不将所有历史 warning 强行升级为启动阻断错误；错误级别必须在设计文档中逐类定义。

## 产品要求

1. 删除 Provider 时，展示受影响的 Alias Target、自动 Alias、Rewrite Rule 和 Priority 条目。
2. 自动生成 Alias 延续现有所有权规则；手动/锁定 Alias 不得被无提示地整体删除，但其失效 Target 必须被清理或显式标记。
3. 删除 Alias 或 Provider 后，Rewrite Rule 不得静默失效；必须清理、禁用或标记，并能解释原因。
4. 保存、导入、启动和运行时使用统一的引用诊断模型。
5. Wails/Web、HTTP 管理和 TUI 的结果与文案语义一致，并覆盖 `zh-CN`/`en`。
6. 后续实现必须提供针对唯一 Target、批量删除、自动/手动/锁定 Alias、旧配置和重复执行的测试。

## 交付标准

- 详细设计文档中的引用矩阵、生命周期策略、入口反馈、运行时诊断、任务拆分和验收矩阵完整。
- 后续实现可以按 Config、Service、Proxy、UI/i18n、导入兼容、测试等边界独立领取。
- 本设计任务本身只提交文档与 Trellis 元数据，不包含产品代码变更。
