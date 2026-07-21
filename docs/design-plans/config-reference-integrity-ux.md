> **约束 / Constraints**
>
> - 本文是后续 implementation task 的唯一权威方案；当前 design task 只完成方案冻结与任务拆分，不直接修改产品代码。
> - 执行本方案时，只允许修改 `todolist` 章节内容及各处 checkbox 状态，不得改写、重排或增删其他正文。
> - 每个步骤中的任务必须由多个子代理并行完成；同一步任务必须没有前置依赖并拥有不冲突的文件或产物边界。
> - 步骤严格按顺序执行，前一步未完成不得开始下一步。
> - 新增用户界面文案必须同时覆盖 `zh-CN` 和 `en`，并使用现有 i18n 机制；后端只返回稳定 code 与参数。
> - 本方案只治理当前已存在的 Provider、Alias/Target、Request Rewrite Rule、Provider Priority 及其配置/运行时边界；未来下游 API key scope 不在范围内。
> - 不在 `Config.Save()` 中加入无条件全局引用硬阻断，不提供通用 `CleanDanglingRefs` 或 `Fix All`；修复策略必须按引用语义和所有权逐项定义。
> - `Target.Model` 是外部上游模型符号，不是 `Provider.Models` 持久化强外键；但交互式 bind 在可信 `ModelsSource=discovered` 时继续执行条件式准入校验。
> - Provider disabled、熔断、网络失败和模型目录暂未观察到不得归类为实体缺失。
> - 未跟踪的 `config-reference-integrity.md` 及同名旧任务不得作为并行事实来源。

# todolist

- [ ] 【进行中】建立 ConfigStore、typed diagnostics 与导入导出一致性基础
- [ ] 设计覆盖全部写入口的 revision 配置事务，消除丢写和磁盘/运行时分裂
- [ ] 设计 typed reference diagnostics，并复用现有 DoctorIssue 展示链路
- [ ] 设计 Provider、Alias、Rewrite Rule 与自动 Alias 的安全生命周期策略
- [ ] 统一 Service、HTTP/Wails、CLI 与 TUI 的预览、执行和冲突契约
- [ ] 补齐运行时错误、trace、Doctor、导入导出和 OpenCode sync 的诊断闭环
- [ ] 实现 React/TUI i18n 交互并通过跨层测试与独立代码审查

# steps

1. - [x] 冻结当前事实与机器契约
   - [x] 独立产出 `.trellis/tasks/07-21-config-reference-integrity-ux/artifacts/01-reference-semantics.md`：定义 `Alias.Target.Provider` 强路由依赖、Rewrite Alias/Providers 弱选择器、ProviderPriority 排序提示、外部模型符号和运行时派生状态。
   - [x] 独立产出 `.trellis/tasks/07-21-config-reference-integrity-ux/artifacts/02-boundary-matrix.md`：覆盖 Load、Save、全部 CRUD/settings、StartProxy、ReloadConfig、ImportConfig、OpenCode Sync、Doctor 和外部手改后的阻断/告警规则。
   - [x] 独立产出 `.trellis/tasks/07-21-config-reference-integrity-ux/artifacts/03-ownership-compatibility.md`：冻结 manual alias、unlocked auto alias、锁定 Alias 内 target、upgraded manual 和历史混合数据的所有权及兼容规则。
   - [x] 完成定义：明确 `RequestRewriteRule.Providers=[]` 表示所有 Provider，Alias selector 可在 direct Provider fallback 中继续匹配；禁止将不存在的 selector 一律按强外键处理。

2. - [x] 冻结诊断、生命周期与传输契约
   - [x] 独立产出 `.trellis/tasks/07-21-config-reference-integrity-ux/artifacts/04-diagnostic-schema.md`：冻结 typed issue、reason、severity、allowedActions 和 i18n params schema。
   - [x] 独立产出 `.trellis/tasks/07-21-config-reference-integrity-ux/artifacts/05-lifecycle-api.md`：冻结 revision、preview/plan/execute、阻断项、自动变更、显式选择与 `persisted/runtimeApplied` DTO。
   - [x] 独立产出 `.trellis/tasks/07-21-config-reference-integrity-ux/artifacts/06-transport-errors.md`：冻结业务错误、HTTP 状态、Wails error envelope、CLI exit code 与 TUI message mapping。
   - [x] 完成定义：三个契约均只依赖步骤 1 的已冻结事实，字段、空值语义、冲突行为和敏感信息边界可直接供后续模块并行实现。

3. - [ ] 建立提交一致性与诊断基础
   - [ ] 在独立 ConfigStore 模块实现 revision/digest、同一文件锁内的 reload-latest、clone/mutate、policy validate、原子持久化流程，并以冲突错误阻止 stale writer 覆盖新配置。
   - [ ] 在独立 reference diagnostics 模块实现稳定 `code/severity/path/source/target/reason/allowedActions/params`，区分 `missing`、`disabled`、`protocol_mismatch`、`catalog_stale` 和 `runtime_unavailable`。
   - [ ] 在配置导入导出模块补齐 `ProviderPriority`、`AutoAliasEnabled` 的 round-trip，并加入未知字段/旧配置策略，不与 ConfigStore 或 scanner 文件重叠。
   - [ ] 完成定义：候选配置在持久化前完成结构校验和 runtime 可构建性检查；调用失败不得出现“磁盘已提交、运行时仍是旧快照但结果看似回滚”的模糊状态。

4. - [ ] 实现独立生命周期规划器
   - [ ] 在 Provider 生命周期模块实现 preview/plan：自动清理 unlocked auto targets 和 Priority；manual alias 或锁定 Alias 内 target 默认阻断并提供显式动作；Rewrite Provider selector 仅在不扩大作用域时清理，唯一 selector 绝不变成 empty=all。
   - [ ] 在 Alias/Rewrite 生命周期模块实现 preview/plan：Alias 删除必须报告 Rewrite selector 与 OpenCode 顶层选择影响，但不得把 selector 不存在绝对判为失效；需要变更规则时只能显式禁用、删除或保留。
   - [ ] 在自动 Alias 生命周期模块修复修改入口所有权：未升级的 auto alias 不得被 bind/edit 静默替换；discovery error/empty 保留现状，可信非空模型减集只自动处理系统所有项。
   - [ ] 完成定义：每个 plan 均返回 revision、阻断项、自动变更、显式选择、保留问题与恢复建议；preview 与 execute 复用同一 planner，重复执行幂等。

5. - [ ] 统一全部配置写入口
   - [ ] Service mutation 集成任务将 Provider、Alias、Rewrite、Priority、auto-alias settings、Provider disabled/import、Proxy settings、Desktop prefs 和完整导入全部接入 ConfigStore，并遵循已冻结 contract；本任务只负责写入路径，不定义 Import/Sync 诊断内容。
   - [ ] HTTP/Wails 与 server bootstrap 集成任务按已冻结 contract 增加 preview/execute DTO、稳定错误和冲突映射，并将 `ensureAdminConfig` 等启动写入接入 ConfigStore；HTTP stale revision 返回 409，Wails 暴露同一 code/params。
   - [ ] CLI 集成任务将 Provider、Alias、Rewrite 的 add/update/bind/toggle/remove 等全部写命令从直接 `Config.* + Save` 迁移到 Service；提供 `--dry-run`、结构化输出和显式非交互确认。
   - [ ] 完成定义：任何仍可写同一配置文件的入口都不能绕过 ConfigStore；不同入口对同一 revision/plan 得到相同动作，旧 plan 必须冲突并要求重新预览。

6. - [ ] 完善运行时、Doctor、导入与同步诊断
   - [ ] Proxy 任务保持现有可用 Target 过滤与 failover，修复本地 404 trace status，并区分 alias_missing、no_available_target、protocol_mismatch 等稳定原因；不得把暂时不可用记录为 dangling。
   - [ ] Doctor/Overview 任务扩展现有 `DoctorIssue` 和 AliasTargetView，映射 typed diagnostics、具体 target 可用性原因与可执行 action，不新增平行的纯文本 warning 体系。
   - [ ] Import/Sync 任务只定义候选配置的引用诊断与同步保护：OpenCode Sync 对临时空可路由集合不得破坏性 prune，并检查顶层 `model/small_model` 的外部弱引用；事务接入由步骤 5 完成。
   - [ ] 完成定义：日志、trace、Doctor 和管理 API 使用同一 code/reason 词汇；`/healthz` 仍只表示进程存活，配置健康由管理诊断接口表达。

7. - [ ] 生成绑定并准备独立客户端适配层
   - [ ] Wails 绑定任务按冻结后的 Go DTO 更新并执行既有生成流程，只负责生成物和类型一致性，不修改 React 交互。
   - [ ] React transport 任务在 `frontend/src/api.ts` 与共享 types 中适配 preview/execute/revision/error contract，不修改页面组件。
   - [ ] TUI presenter 任务建立纯展示模型和消息类型，适配同一 preview/result contract，不修改具体 screen 交互。
   - [ ] 完成定义：Wails 与 browser HTTP transport 对同一后端结果产生等价前端类型；React/TUI 实现可基于冻结适配层并行开发。

8. - [ ] 实现管理端影响预览与修复体验
   - [ ] React 任务为 Web/Desktop 共用 UI 实现删除影响弹窗、revision 冲突刷新、执行摘要、target 原因和修复入口，并同步 `en`/`zh-CN` key 与占位符。
   - [ ] TUI 任务使用已冻结 presenter 分组展示阻断项、自动变更和保留问题；窄终端可滚动，确认与冲突文案覆盖中英文。
   - [ ] CLI 体验任务完善人类可读与 JSON 输出、退出码、TTY/非交互行为和删除摘要，不修改已完成的 Service/ConfigStore 实现。
   - [ ] 完成定义：用户删除前能看到准确影响，删除后能区分已清理、已禁用、被保留和仍需处理的项目；所有操作均可从 code 映射本地化文案。

9. - [ ] 完成独立代码审查
   - [ ] 架构审查任务检查 ConfigStore 事务、runtime apply、生命周期 planner、跨入口契约和步骤 1 冻结文档的一致性。
   - [ ] 安全与兼容审查任务检查 scope 不扩大、敏感凭据脱敏、旧配置/未知字段、OpenCode 非破坏性同步和 revision 冲突行为。
   - [ ] UX 与发布审查任务检查 React/TUI/CLI 一致性、i18n、错误可操作性、迁移说明和未跟踪重复方案隔离。
   - [ ] 完成定义：形成按 backend、transport/client、docs/release 分区的 findings 清单；每项具有严重度、文件所有权和可验证整改条件。

10. - [ ] 按文件所有权整改审查发现
   - [ ] Backend 整改任务只处理 ConfigStore、diagnostics、lifecycle、Proxy、Doctor、Import/Sync 的高/中 findings，并运行所属包目标测试；无 finding 时记录 no-op。
   - [ ] Transport/client 整改任务只处理 HTTP/Wails/CLI/TUI/React/i18n 的高/中 findings，并运行所属模块目标测试；无 finding 时记录 no-op。
   - [ ] Docs/release 整改任务只处理契约 artifact、迁移说明、发布说明和重复方案隔离 findings；无 finding 时记录 no-op。
   - [ ] 完成定义：所有高/中 findings 已修复或有 owner 明确接受记录；整改任务不越过各自文件所有权。

11. - [ ] 执行最终只读验证与发布门禁
   - [ ] Go 验证任务以 `gofmt -l` 断言无待格式化文件，再执行 `go test ./...` 和 `go test -race ./internal/config ./internal/app ./internal/proxy`；不得在本步骤修改工作树。
   - [ ] 边界契约验证任务只运行测试，覆盖 HTTP 409/code/params、Wails 对等结果、CLI 全部写命令/dry-run/JSON/退出码、TUI presenter、server bootstrap 和所有写入口均经过 ConfigStore。
   - [ ] 前端验证任务使用 `npm ci` 执行锁定依赖安装，再做类型检查/build 及交互测试，覆盖 Rewrite 零选择语义、删除预览、409 刷新、执行摘要和 en/zh-CN key/placeholder 对齐；不得修改生成物。
   - [ ] 完成定义：所有自动化门禁通过且 `git diff --check` 无错误；发布说明写明配置 revision、冲突处理、删除行为与恢复方式。

# goal

- [ ] 所有配置写入口均具备 revision 冲突检测；两个 stale writer 不会静默丢写，任何失败结果都能明确说明是否持久化和是否应用到 runtime。
- [ ] Provider 删除不会留下未分类问题：自动项与 Priority 安全清理，manual alias 或锁定 Alias 内引用默认阻断或按用户显式 plan 处理。
- [ ] Rewrite Provider selector 永远不会因自动清理从单一 Provider 变成 empty=all；Alias selector 的 direct fallback 语义有契约测试。
- [ ] 自动 Alias 修改必须经过 Upgrade/所有权转换；模型发现失败或空结果不触发误删，可信模型减集只处理系统所有项。
- [ ] `Target.Model` 维持外部弱符号语义，同时保留 discovered 模型的交互式 bind 准入校验，不把目录陈旧升级为全局强外键错误。
- [ ] 完整配置导入导出保留 ProviderPriority 与 AutoAliasEnabled；导入、启动、reload、sync 和 Doctor 使用一致的 typed diagnostics。
- [ ] 运行时继续安全过滤不可用 Target，同时向 trace、日志和管理诊断提供准确原因，不以泛化 500 或错误的 dangling 分类掩盖问题。
- [ ] Web/Desktop、HTTP、Wails、CLI 与 TUI 共用 preview/execute/revision 契约，新增 UI 文案和错误展示完整支持 `zh-CN`/`en`。
- [ ] 代码审查完成：无通用破坏性自动修复、无 scope 扩大、无敏感凭据泄漏、无重复权威方案或未解释的入口差异。
- [ ] 基础代码测试通过：Go 编译、全量测试、目标 race 测试、跨入口契约测试和前端 typecheck/build/交互测试全部通过。
