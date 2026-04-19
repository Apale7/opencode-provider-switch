# Desktop GUI i18n / UI-UX / Theme Refresh

## 概要

当前桌面 GUI 已具备可用的基础功能，但前端还停留在“单文件控制台”形态：

- `frontend/src/App.tsx` 单文件承载全部模块和交互，页面信息密度高但层次弱
- 当前所有主内容都堆在一个长页面里，模块边界不清晰
- 文案基本为英文硬编码，尚未建立 i18n 机制
- `frontend/src/styles.css` 仅支持一套暗色视觉，缺少主题切换能力
- 桌面偏好当前只包含 `launchAtLogin` / `minimizeToTray` / `notifications`

本任务的目标不是直接做完全部 GUI 重构，而是输出一版可以直接进入实现阶段的方案设计，并把范围、分层、数据契约、实施顺序和验收标准先固定下来。

## 产品目标

这轮 GUI 优化聚焦三个结果：

1. 建立正式的 i18n 基础设施，至少支持 `zh-CN` 与 `en-US`
2. 将当前单页大杂烩改为左侧纵向 tab 的模块化信息架构
3. 支持 `system / light / dark` 三档主题切换

同时满足以下使用体验要求：

- 桌面主窗口默认信息组织清晰，不需要依赖通篇向下滚动来完成主要操作
- 尽量实现“页面不滚动、局部区域滚动”
- 保持 Wails 桌面壳与浏览器 fallback 共用同一套前端
- 不重写现有 Go 侧业务逻辑，只补齐桌面偏好与前端展示能力

## 当前事实与约束

### 现有前端事实

- 前端入口很轻：`frontend/src/main.tsx` 仅挂载 `App`
- 当前无路由库、无全局状态库、无 i18n 依赖
- 当前 API 通过 `frontend/src/api.ts` 在 Wails bridge 与 HTTP fallback 间切换
- 当前页面结构全部写在 `frontend/src/App.tsx`，内容包含：概览、桌面偏好、OpenCode 同步、Doctor、Providers、Aliases
- 当前视觉完全由 `frontend/src/styles.css` 管理，且根节点声明为暗色 `color-scheme: dark`

### 现有跨层契约事实

- `internal/config/config.go` 中 `config.Desktop` 只有三个布尔字段
- `internal/app/types.go` 中 `DesktopPrefsView` / `DesktopPrefsInput` 同样只有三个布尔字段
- `internal/app/service.go` 的 `SaveDesktopPrefs()` 目前直接把这三个字段写回配置文件
- `internal/app/service_test.go` 与 `internal/desktop/http_test.go` 已覆盖桌面偏好保存路径，适合作为后续扩展 `theme` / `language` 的回归测试入口

### 现有窗口约束

- `internal/desktop/wails.go` 当前窗口尺寸为 `1280 x 880`
- 最小尺寸为 `980 x 720`

这个尺寸对“左侧纵向 tab + 主内容双栏”是偏紧的，尤其是 Providers / Aliases 两个高密度模块，因此布局方案需要同时考虑：

- 默认窗口尺寸下尽量不出现整页纵向滚动
- 窄窗口时允许局部折叠或内部滚动，但仍保持结构稳定

## 非目标

1. 不引入新的后端业务层或重写 `internal/app`、`internal/config`、`internal/opencode`、`internal/proxy` 的业务归属
2. 不为了 tab 导航引入完整路由系统，除非 hash 方案无法满足需要
3. 不在这一轮引入大型 UI 框架或设计系统迁移
4. 不在这一轮扩展日志分析、历史记录、远程控制等新产品面
5. 不在这一轮做复杂动画、图标系统或品牌视觉重塑

## 核心设计决策

### 1. i18n 方案

推荐使用：`i18next + react-i18next`

选择原因：

- 对 React 生态成熟，后续扩展语言成本低
- 支持命名空间、fallback、插值、运行时切换
- 比手写 context 字典更适合后续“人工精细翻译”与增量维护
- 与当前前端体量相匹配，不需要引入更重的国际化平台

推荐落地结构：

```text
frontend/src/i18n/
  index.ts
  locales/
    en.json
    zh-CN.json
```

推荐规则：

- `en.json` 作为 source of truth
- `zh-CN.json` 先提供可用翻译，后续再人工精修
- 所有可见文案统一改为 key，不再直接在 JSX 中写死英文
- 所有状态文案、按钮文案、空态文案、表单标签、placeholder、错误提示都纳入翻译
- 不翻译配置路径、provider id、model id、URL、技术错误原文等数据性内容

推荐 key 命名方式：

```text
app.title
nav.overview
overview.proxyRunning
providers.form.baseUrl
aliases.empty
sync.previewChanges
settings.theme.system
messages.refreshing
errors.bridgeUnavailable
```

### 2. 语言偏好持久化

推荐把语言偏好纳入桌面偏好，而不是只放前端本地状态。

建议新增字段：

- `language`: `system | en-US | zh-CN`

对应变更位置：

- `internal/config/config.go` 的 `config.Desktop`
- `internal/app/types.go` 的 `DesktopPrefsView` / `DesktopPrefsInput`
- `internal/app/service.go` 的读写映射
- `frontend/src/types.ts`

语言决议顺序：

1. 用户显式选择的 `language`
2. 若为 `system`，使用 `navigator.language`
3. 若系统语言不在支持列表，则回退到 `en-US`

这样做的原因：

- 桌面应用重启后保留用户偏好
- 浏览器 fallback 与 Wails 行为一致
- 后续新增日语等语言时不需要重构偏好模型

### 3. 主题方案

推荐继续使用原生 CSS，不引入 Tailwind 或 CSS-in-JS 重构。

建议新增字段：

- `theme`: `system | light | dark`

实现原则：

- 前端只维护一个“用户选择主题”字段
- 运行时计算一个 `resolvedTheme`，结果为 `light` 或 `dark`
- 把结果写到 `document.documentElement.dataset.theme`
- 用 CSS 变量承载所有颜色 token

推荐样式组织：

```css
:root {
  --bg: ...;
  --panel: ...;
  --text: ...;
  --muted: ...;
  --border: ...;
  --accent: ...;
}

html[data-theme='dark'] { ... }
html[data-theme='light'] { ... }
```

注意事项：

- 现在的 `:root { color-scheme: dark; }` 需要改为按主题动态切换
- 表单控件、滚动条、badge、panel、背景渐变都需要转为 token 化
- 不要在各组件里散落 `if (theme === 'dark')` 之类分支，主题只通过 CSS token 生效

### 4. 导航与信息架构

推荐使用左侧纵向 tab，且 tab 状态优先使用 hash 管理，而不是新增路由依赖。

推荐 tab：

1. `overview` 概览
2. `providers` 提供商
3. `aliases` 别名路由
4. `sync` 同步与体检
5. `settings` 设置

推荐原因：

- 这 5 个模块正好对应当前功能边界
- hash 方案足够轻量，Wails 与 HTTP fallback 都可直接工作
- 可支持 `#providers` 这类深链接，便于问题定位与后续文档引用

不建议保留当前结构：

- 概览、设置、同步、Doctor、Providers、Aliases 在单页堆叠
- 大量 `pre` 原始 JSON 长块直接暴露在主流程中

### 5. 页面滚动策略

目标不是“任何情况下零滚动”，而是：

- 主页面整体尽量不产生纵向长滚动
- 长列表和明细列表在局部 pane 内滚动
- 表单和操作区域尽量固定在视口中

推荐布局原则：

- 应用根容器使用接近全高布局
- 左侧 sidebar 固定宽度
- 右侧内容区使用 tab 面板切换
- Providers / Aliases 使用左右 split-pane
- 列表区独立 `overflow: auto`
- 原始 JSON / 诊断详情放入折叠区或次级详情区，不占主操作流

## 推荐信息架构

### Overview Tab

目标：进入应用后 5 秒内看懂当前状态，并完成最常见的启停操作。

推荐内容：

- 顶部状态卡：代理状态、provider 数、alias 数、可路由 alias 数
- 主操作区：`Refresh`、`Start proxy`、`Stop proxy`、`Run doctor`
- 环境信息：版本、shell、配置路径
- 简版健康摘要：最近一次 doctor 状态或提示入口

不推荐继续保留：

- 默认展示完整 `overview` JSON 大块文本

建议：

- 原始 JSON 仅在“调试详情”折叠区显示

### Providers Tab

目标：高频管理 provider，避免当前“左边长表单 + 右边长列表 + 整页继续往下”的阅读负担。

推荐结构：

- 左侧：provider 列表、搜索、状态筛选、`新建 provider`
- 右侧：当前选中 provider 的编辑表单
- 右上或右下：导入 OpenCode provider 的辅助卡片

交互原则：

- “创建”与“编辑”共用同一表单
- 点击列表项进入编辑态
- 删除、启用、禁用作为列表项级操作
- `Headers` 与 `Models` 属于细节信息，不要默认铺得过长

列表过长时：

- 仅让左侧列表内部滚动
- 右侧表单固定显示

### Aliases Tab

目标：把“别名编辑”和“target 绑定管理”从当前混排状态拆开。

推荐结构：

- 左侧：alias 列表、状态摘要、`新建 alias`
- 右侧上半区：alias 基本信息编辑
- 右侧下半区：targets 表格或卡片列表

交互原则：

- `Bind target` 不再作为全局独立表单漂浮存在，而是明确附着到当前 alias
- 当未选中 alias 时，右侧给出“请选择或新建 alias”空态
- target 的启用/停用/解绑应靠近 target 行，不要再依赖跨区域操作

### Sync Tab

目标：把 OpenCode 同步与 Doctor 合并成“维护与诊断”模块，但在视觉上清楚分区。

推荐结构：

- 左列：OpenCode sync 表单与 preview / apply 结果
- 右列：Doctor 摘要、问题列表、重新检查按钮

展示方式：

- preview / apply 结果先展示结构化摘要
- 原始结果放在折叠详情中
- doctor 结果优先展示 `OK / 有问题`、问题数量和核心问题列表

### Settings Tab

目标：把“桌面偏好”和“外观语言偏好”统一收口。

推荐内容：

- Appearance
- Language
- Desktop behavior
- About / debug info

建议顺序：

1. 主题选择：`跟随系统 / 浅色 / 深色`
2. 语言选择：`跟随系统 / 中文 / English`
3. 桌面行为：开机启动、最小化到托盘、通知
4. 应用信息：版本、shell、配置路径

这会把原本散落在首页的桌面偏好移到更符合用户心智的位置。

## 推荐前端结构调整

当前 `App.tsx` 已达到必须拆分的规模。推荐最小可控拆分如下：

```text
frontend/src/
  App.tsx
  api.ts
  main.tsx
  types.ts
  styles.css
  i18n/
    index.ts
    locales/
      en.json
      zh-CN.json
  app/
    tabs.ts
    useDesktopPreferences.ts
    useResolvedTheme.ts
  features/
    overview/OverviewTab.tsx
    providers/ProvidersTab.tsx
    aliases/AliasesTab.tsx
    sync/SyncTab.tsx
    settings/SettingsTab.tsx
  shared/
    AppSidebar.tsx
    AppHeader.tsx
    Panel.tsx
```

拆分原则：

- API 访问仍保留在 `api.ts`
- 跨模块共享状态仍由 `App.tsx` 统一拥有
- tab 组件只接收明确 props，不私自重复请求数据
- theme / language 的解析逻辑抽到 hook 或 util，避免每个 tab 重复判断

## 跨层数据契约变更

### 配置层

`internal/config/config.go`

建议将：

```go
type Desktop struct {
    LaunchAtLogin  bool
    MinimizeToTray bool
    Notifications  bool
}
```

扩展为：

```go
type Desktop struct {
    LaunchAtLogin  bool   `json:"launch_at_login,omitempty"`
    MinimizeToTray bool   `json:"minimize_to_tray,omitempty"`
    Notifications  bool   `json:"notifications,omitempty"`
    Theme          string `json:"theme,omitempty"`
    Language       string `json:"language,omitempty"`
}
```

允许值建议：

- `Theme`: `system | light | dark`
- `Language`: `system | en-US | zh-CN`

### 应用层 DTO

`internal/app/types.go`

- `DesktopPrefsView`
- `DesktopPrefsInput`
- 如需保证兼容性，可继续让 `DesktopPrefsSaveResult` 结构不变，只扩展 `prefs` 内容

### 应用层服务

`internal/app/service.go`

需要新增：

- 对 `theme` / `language` 的合法值归一化
- 对未知值回退到 `system`
- `desktopPrefsView()` 的新字段映射

建议不要把合法性校验放到前端单独维护，前后端都应该认同同一套枚举语义。

### 前端类型

`frontend/src/types.ts`

需要扩展：

- `DesktopPrefsView`
- `DesktopPrefsSaveResult`

建议新增：

- `type ThemePreference = 'system' | 'light' | 'dark'`
- `type LanguagePreference = 'system' | 'en-US' | 'zh-CN'`

## 实施顺序

推荐拆成 4 个小阶段，按风险从低到高推进。

### Phase 1: 补齐桌面偏好契约

目标：先把 `theme` / `language` 变成可保存、可读取、可测试的数据。

涉及：

- `internal/config/config.go`
- `internal/app/types.go`
- `internal/app/service.go`
- `internal/app/service_test.go`
- `internal/desktop/http_test.go`
- `frontend/src/types.ts`

验收：

- 保存桌面偏好时可写入 `theme` / `language`
- 旧配置不报错，缺省值按 `system` 处理
- 现有桌面偏好测试继续通过，并增加新字段断言

### Phase 2: 搭 i18n 与主题基础设施

目标：先把底座搭起来，再替换可见文案。

涉及：

- 新增 i18n 初始化
- 新增中英文资源文件
- 把现有根样式改为 token 化
- 新增主题解析与 DOM 同步逻辑
- Settings 中先提供主题 / 语言切换控件

验收：

- 应用启动后可按配置或系统语言切换文案
- 应用启动后可按配置或系统主题切换样式
- Wails 与 HTTP fallback 行为一致

### Phase 3: 重构为左侧纵向 tab 布局

目标：把当前单页拆成清晰模块，不改变业务能力。

涉及：

- 提炼 `Sidebar` 与 tab metadata
- 将当前大页面拆为 5 个 tab 内容区
- 将 Provider / Alias 改为 split-pane 布局
- 将 JSON 大块输出收进折叠区或调试区

验收：

- 默认视图下无需整页向下滚动才能完成主要操作
- 大部分高频操作都能在当前 tab 内完成
- hash 切换 tab 正常，可直接进入某个模块

### Phase 4: 文案收口与中文首轮翻译

目标：把所有硬编码文案收口到资源文件，并提供首轮中文。

涉及：

- 清理残余硬编码字符串
- 生成文案清单供人工精修
- 完成 `zh-CN` 首轮翻译

验收：

- 主界面无裸露英文硬编码
- 翻译 key 命名稳定、按模块组织
- 人工精修时只需要维护 locale 文件，不需要再搜 JSX

## 测试与验证建议

### Go 侧

- `go test ./internal/app`
- `go test ./internal/desktop`

重点校验：

- 旧配置读取兼容
- 新偏好字段保存正确
- HTTP 接口序列化包含新字段

### 前端侧

当前仓库未引入测试框架，本轮至少应保留：

- `npm run build`

手动验证清单：

- 语言切换后 tab、表单、状态文案、空态是否同步变化
- 主题切换后按钮、输入框、badge、panel、背景是否都正确
- 窗口宽度在默认值与最小值附近时，布局是否稳定
- hash 切换 tab 与刷新后定位是否符合预期
- Wails / 浏览器 fallback 两种壳下切换行为是否一致

## 风险与缓解

### 风险 1: `App.tsx` 一次性重构过大

缓解：

- 先抽 tab 壳与 shared 组件
- 每拆一个 tab 就保留原有业务处理函数，不同时改业务与 UI

### 风险 2: i18n key 粒度不稳定，后续翻译返工

缓解：

- 先按模块命名空间统一 key
- 避免把整句状态文案拼接在多个地方
- 动态文案尽量通过插值模板统一

### 风险 3: 主题切换改动大，容易出现漏色

缓解：

- 先把当前样式中的颜色集中成 token
- 搜索所有十六进制颜色和 `rgba()`，逐步收口

### 风险 4: 窄窗口下 tab 布局拥挤

缓解：

- 默认保持左侧 tab
- 在低于阈值时允许 sidebar 收缩
- 若验证后仍过于拥挤，再上调 `MinWidth`，建议目标区间为 `1100-1120`

## 验收标准

1. `DesktopPrefs` 契约中包含 `theme` 与 `language`，并可在配置中持久化
2. 前端引入正式 i18n 机制，至少可在 `en-US` 与 `zh-CN` 间切换
3. 主界面切换为左侧纵向 tab，至少包含概览、提供商、别名路由、同步与体检、设置五个模块
4. 主题支持 `system / light / dark`
5. 主流程页面不再依赖整页长滚动；长列表改为局部 pane 内滚动
6. 主要可见文案已从 JSX 抽离到翻译资源文件
7. 构建与现有相关测试路径不回归

## 建议的下一步

1. 先实现 Phase 1，补齐 `theme` / `language` 跨层契约
2. 再实现 Phase 2，搭建 i18n 与 CSS token 底座
3. 最后进入 tab 布局重构，避免同时改数据层与视觉层

这是当前仓库里风险最低、可持续迭代的推进顺序。
