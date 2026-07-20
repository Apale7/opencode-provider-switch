# todolist

- [ ] 完成现有配置引用关系与失效行为审计
- [ ] 定义 Provider、Alias、Request Rewrite、Provider Priority 的生命周期契约
- [ ] 设计删除影响预览、清理结果与恢复路径
- [ ] 设计加载、保存、导入和运行时的引用完整性校验分层
- [ ] 设计 Wails/Web、TUI、HTTP 管理入口的统一反馈和 i18n 契约
- [ ] 拆分后续实现任务并补齐测试验收矩阵

## 约束 / Constraints

- 本文是设计与任务拆分文档，本阶段不得修改产品代码、配置、脚本或生成代码。
- 执行本方案时，只允许修改本节 `todolist` 内容及各处 checkbox 状态，不得改写、重排或增删其他正文。
- 每个步骤中的任务必须由多个子代理并行完成；不得在主代理中串行执行同一步骤任务。
- 步骤严格按顺序执行，前一步未完成不得开始下一步。
- 新增用户界面文案必须同时覆盖 `zh-CN` 和 `en`，并使用现有 i18n 机制。
- 本方案只针对当前代码中已存在的引用类型；Provider 上游多 API key 与未来的下游 API key scope 不在本次实现范围内，但必须在扩展点中记录依赖。

# steps

1. - [ ] 建立引用关系基线
   - [ ] 并行审计 `Config`、Provider、Alias/Target、RequestRewriteRule、ProviderPriority 的定义、持久化字段和所有 CRUD 入口，形成引用矩阵。
   - [ ] 并行审计代理解析、可用 Target 过滤、failover、Request Rewrite 应用和 circuit breaker 的运行时行为，记录每类失效的 HTTP、日志和 trace 表现。
   - [ ] 并行审计 Wails/Web、TUI、HTTP 管理入口、配置导入和现有 i18n/测试覆盖，确认入口差异与可复用的 Service 边界。
   - 完成定义：每条跨实体引用均标出来源、目标、失效触发、现有行为、期望处理和测试位置；明确区分现有代码与未来任务设想。

2. - [ ] 设计统一的引用扫描与校验契约
   - [ ] 并行设计配置级引用扫描器，覆盖 Alias.Target.Provider、RequestRewriteRule.Alias、RequestRewriteRule.Providers 和 ProviderPriority；输出稳定的引用类型、来源对象、目标值和修复建议。
   - [ ] 并行设计校验分层：编辑/删除前影响预览、保存前硬校验、启动/导入诊断、运行时惰性降级，并定义哪些问题是 error、warning 或可自动修复。
   - [ ] 并行设计向后兼容策略，处理历史配置、部分字段缺失、未知模型名、外部手工编辑和导入配置，不把模型目录刷新误当成 Provider 外键校验。
   - 完成定义：形成可供 Config、Service、Proxy 和各 UI 入口共同使用的结果模型；不允许各入口自行扫描或自行解释字符串错误。

3. - [ ] 设计删除与编辑的生命周期语义
   - [ ] 并行设计 Provider 删除语义：清理所有指向该 Provider 的 Alias Target；自动别名在无 Target 时删除，手动/锁定 Alias 保留元数据并进入明确的无可用 Target 状态；同时清理 ProviderPriority。
   - [ ] 并行设计 Alias 删除语义：保留仍有用户意图的 Request Rewrite 规则但自动禁用并标注失效来源，禁止其静默生效；删除 Provider 时对规则中的 Providers 列表执行同样的可解释清理。
   - [ ] 并行设计模型刷新、Provider 禁用、协议变化和批量删除的边界：删除与禁用不得混为一谈，模型不存在应先 warning，协议不匹配应进入不可路由诊断。
   - 完成定义：每种操作都有“删除前影响集合、执行后变更集合、保留集合、可恢复动作、幂等性”五项说明，并与自动 Alias 的既有锁定语义兼容。

4. - [ ] 设计管理入口与用户反馈
   - [ ] 并行设计 `PreviewDeleteProvider`、`PreviewDeleteAlias` 或等价的统一 Service 结果，至少返回受影响 Alias、Target、Rewrite Rule、Priority 条目和自动删除 Alias。
   - [ ] 并行设计前端/Web 和 Wails 确认流程：先预览影响，再二次确认，展示清理摘要、保留的手动配置和下一步修复动作；所有新增文案提供中英文 key。
   - [ ] 并行设计 TUI/HTTP/CLI 的同等反馈和错误映射；后端返回稳定错误码与参数，展示层负责翻译，不要求在 Go 错误字符串中硬编码语言。
   - 完成定义：所有管理入口使用同一结果契约，删除成功后用户能看到“清理了什么、保留了什么、仍需处理什么”。

5. - [ ] 设计运行时诊断与恢复闭环
   - [ ] 并行设计路由遇到悬空 Target 时的跳过、failover 和全不可用响应，确保不出现 panic 或无上下文的 500，并增加结构化引用诊断字段。
   - [ ] 并行设计启动、热重载和导入时的处理：合法历史配置可启动并报告 warning；真正无法路由的配置不应静默成功；自动修复必须可审计且幂等。
   - [ ] 并行设计 Doctor、Overview、AliasView 和日志的展示，使 `TargetCount` 与 `AvailableTargetCount` 的差异能追溯到具体 Provider/Rule，而不是只显示数字。
   - 完成定义：用户从错误、日志或诊断页均能定位来源配置、失效目标和建议动作，且恢复后再次扫描不再报告同一问题。

6. - [ ] 编排实现任务、测试与发布顺序
   - [ ] 并行拆分 Config 引用扫描/级联清理、Service 影响预览/统一结果、Proxy 诊断/降级、Frontend/TUI i18n UX、导入/启动兼容和测试任务。
   - [ ] 并行建立单元、Service 集成、Proxy 路由、配置导入和前端交互验收矩阵，覆盖单个删除、批量删除、唯一 Target、自动/手动/锁定 Alias、规则空 Providers 和旧配置。
   - [ ] 并行评审与 `07-21-auto-alias-simplify`、`05-16-provider-multi-ak`、Provider 多 BaseURL 路由任务的边界，防止重复实现或把未来 API key scope 误纳入本次改动。
   - 完成定义：产生可独立领取的实现任务、明确依赖顺序、回滚策略、验收命令和发布说明项。

# goal

- [ ] Provider 删除后，不再留下未解释的 Alias Target 或 ProviderPriority 引用；自动 Alias 的空壳按既有自动生成语义清理，手动/锁定 Alias 有明确失效状态和恢复入口。
- [ ] Alias 或 Provider 删除后，Request Rewrite 不再静默失效；规则要么被安全清理，要么被保留为明确 disabled/invalid 状态，并能显示来源和修复建议。
- [ ] 保存、导入、启动和热重载对引用问题使用统一的扫描结果；合法旧配置保持兼容，修复操作幂等，不因一次运行时请求才暴露问题。
- [ ] Wails/Web、HTTP 管理和 TUI 的删除确认均支持影响预览；所有新增 UI 文案通过中英文 i18n key 提供。
- [ ] 运行时遇到失效引用时不会 panic 或返回无上下文的 500，日志、trace、诊断页和用户错误均能关联到具体来源对象。
- [ ] 代码审查完成：审查范围覆盖 Config、Service、Proxy、导入路径、Wails/Web、TUI、i18n 与测试，且无未解释的行为差异。
- [ ] 基础代码测试通过：执行 Go 格式化、编译和相关 `go test`；前端执行既有 lint/typecheck/build 或等价检查，新增引用清理和删除预览用例全部通过。
