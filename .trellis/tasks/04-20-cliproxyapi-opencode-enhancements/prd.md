# CLIProxyAPI 参考增强清单

## 结论

当前项目最合适的路线不是放弃 `ocswitch` 现有核心、转做 CLIProxyAPI 的 GUI 壳，而是：

- 保留当前 `OpenCode + alias + deterministic failover + opencode sync` 的核心定位
- 借鉴 CLIProxyAPI 在观测、管理、桌面体验、外部生态接入上的成熟思路
- 明确避免把项目拉成一个多协议、多账号、OAuth 驱动的通用代理平台

CLIProxyAPI 的价值主要在于提供了一个更广义的“AI CLI 代理平台”参考样本；当前项目的价值则在于把 OpenCode 使用路径做短、做稳、做清晰。后续增强应围绕这个差异化定位展开。

## 当前项目已具备的基础

从仓库当前实现看，项目已经具备以下能力：

- provider / alias 管理
- OpenCode provider 导入
- `provider.ocswitch.models` 同步与预览
- 本地代理启停
- 请求 trace 列表
- doctor 结构校验
- Wails 桌面 GUI 与浏览器 fallback shell
- 桌面偏好、主题、语言、通知等基础桌面能力

这意味着项目并不缺“基础控制台”，而是更适合进入“增强管理面和增强观测面”的阶段。

## 可立即参考增强清单

这些能力与当前项目定位一致，实现成本相对可控，而且能直接提升 OpenCode 使用体验。

### 1. 请求链路观测增强

目标：让用户明确知道一次请求为什么失败、为什么切换、最终落到了哪里。

建议增强：

- 在现有 request trace 基础上补齐更清晰的 attempt 时间线
- 展示每次尝试的失败分类：连接失败、超时、`429`、`5xx`、请求被拒绝、首字节超时
- 区分“首字节前 failover”与“已开始流式输出不可切换”两类状态
- 在 GUI 中直接呈现命中 provider、remote model、attempt 数、failover 次数

为什么值得优先做：

- 这正好对应 CLIProxyAPI 一类项目最强的“可解释代理”价值
- 当前项目已经有 `internal/proxy/traces.go` 和 trace UI 基础，属于低风险增量增强
- 能直接降低“代理到底有没有切换成功”的排障成本

### 2. Provider 健康视图

目标：让 provider 列表不只是静态配置，而是带有最近健康信号的操作面板。

建议增强：

- 显示每个 provider 最近请求结果摘要
- 增加最近错误原因统计
- 显示最近一次成功、最近一次失败时间
- 在 provider 详情中突出“当前可路由 / 不可路由原因”

为什么值得优先做：

- 当前项目的 failover 强依赖 provider 状态，但用户对健康信息还不够直观
- 这能把“配置管理”提升成“运维可视化”

### 3. Sync / Config Diff 体验增强

目标：让 `opencode sync` 更像一个安全、可审阅的配置变更流程。

建议增强：

- 预览页增加更清晰的结构化 diff，而不只是结果文本
- 明确标记将新增、更新、删除的 alias/model 映射
- 当目标配置文件存在手工自定义内容时，增加风险提示
- 在 GUI 中保留最近一次 sync 结果与目标路径摘要

为什么值得优先做：

- 这是当前项目相对 CLIProxyAPI 的差异化核心之一
- 做好后能明显强化“OpenCode 生态支持”的产品认知

### 4. 桌面运维体验增强

目标：把桌面端从“配置入口”提升为“日常操作台”。

建议增强：

- 首页增加运行状态、最近错误、最近切换摘要
- 托盘菜单提供常用动作：启动代理、停止代理、打开日志/网络视图
- 代理异常或频繁 failover 时提供桌面通知
- 增加面向排障的复制按钮，如复制当前代理地址、配置路径、最近 trace 摘要

为什么值得优先做：

- 当前项目已经有 Wails 桌面基础和偏好设置，补强这层收益很直接
- 这类能力来自 CLIProxyAPI 生态周边项目的成熟经验，但不改变核心协议层

## 中期增强清单

这些能力值得规划，但不建议马上做进主线，避免打散当前项目的聚焦定位。

### 1. 轻量 Management API

目标：让桌面 GUI、浏览器 fallback、外部小工具共用一组稳定管理接口。

建议范围：

- provider / alias / sync / proxy status / traces 的只读或有限写入 API
- 默认仅监听 localhost
- 保持当前本地工具属性，不引入远程多用户管理

价值：

- 便于未来做更轻的外部集成
- 有利于把 `frontend/src/api.ts` 当前的桥接逻辑再收敛一层

### 2. 外部工具接入能力

目标：让项目不仅支持 OpenCode，还能更容易被其他本地工具消费。

建议方向：

- 提供更稳定的本地管理接口而不是直接开放更多推理协议
- 补充“如何把当前代理配置给其他工具”的文档或导出功能
- 在不改变核心定位的前提下，探索最小化生态接入层

价值：

- 能扩大项目生态半径
- 但不必进入 CLIProxyAPI 那种全量多客户端兼容路线

### 3. 路由策略可视化

目标：让 alias 到 target 的实际执行顺序与生效条件更清楚。

建议增强：

- 在 alias 详情中显示当前可路由 target 列表与被排除 target 原因
- 展示 provider disabled、target disabled、provider missing 等状态来源
- 对 doctor 与 sync 共用同一套“routable alias”可视解释

价值：

- 能减少用户对 alias 绑定状态的误解
- 属于现有配置模型的自然增强

### 4. 更强的导入与迁移工具

目标：降低从其他本地代理或 OpenCode 现有配置迁移到 `ocswitch` 的成本。

建议增强：

- 强化现有 OpenCode provider import 的冲突提示
- 支持导入更多已存在的 OpenAI 兼容 provider 信息
- 为迁移场景保留更多来源元数据，便于用户复核

价值：

- 有助于吸纳 CLIProxyAPI 用户群中只需要 OpenCode 子集能力的人
- 但仍保持“迁移到 OpenCode 专用代理”这个明确方向

## 不建议做的清单

这些方向虽然能从 CLIProxyAPI 找到参考，但会显著稀释当前项目定位，或直接把复杂度拉升到另一个产品层级。

### 1. 直接转向多协议通用网关

不建议做：

- 原生支持 Claude / Gemini / Anthropic messages / 多协议翻译层
- 把项目从 `OpenAI Responses for OpenCode` 扩展成统一 AI gateway

原因：

- 会把当前简洁的代理逻辑拖入协议转换和兼容细节泥潭
- 会弱化项目最有辨识度的 OpenCode 专用价值

### 2. 引入 OAuth 多账户体系

不建议做：

- OpenAI Codex OAuth
- Claude Code OAuth
- 多账号轮询与账号池管理

原因：

- 这是 CLIProxyAPI 的核心复杂度来源之一
- 一旦引入，就不再是“本地 alias failover 工具”，而是“账号代理平台”
- 对当前项目用户价值并不成比例

### 3. 做配额、计费、商业化控制台

不建议做：

- 配额监控平台
- 账单统计
- 定价看板
- 多租户后台

原因：

- 这些更适合构建在 Management API 和账号体系之上
- 与当前项目的核心问题并不直接相关

### 4. 为了兼容更多工具而重写核心模型

不建议做：

- 以“兼容所有 AI 编码工具”为目标重做 provider 抽象
- 提前引入策略路由、成本路由、延迟优选等平台能力

原因：

- 当前项目的核心优势是确定性和可预测性
- 过早平台化会破坏这条产品线最清晰的边界

## 推荐推进顺序

### Phase 1

- 请求链路观测增强
- Provider 健康视图
- Sync / Config diff 体验增强
- 桌面运维体验增强

### Phase 2

- 轻量 Management API
- 路由策略可视化
- 更强的导入与迁移工具

### Phase 3

- 再评估是否需要更广的外部工具接入
- 仅在明确存在用户需求时，考虑非常克制的生态扩展

## 最终建议

后续如果参考 CLIProxyAPI，原则应当是：

- 借它的“管理与观测经验”
- 不借它的“平台化复杂度”

一句话总结：

**把 `ocswitch` 做成 OpenCode 场景里最好用、最可解释、最稳定的本地代理，而不是做成另一个 CLIProxyAPI。**
