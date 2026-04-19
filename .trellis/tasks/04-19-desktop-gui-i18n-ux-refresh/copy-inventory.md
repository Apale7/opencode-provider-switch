# GUI 文案收口规划

## 目的

这份文件不是最终翻译稿，而是为后续“人工精细翻译”做结构准备。

本轮建议先把所有可见文案按命名空间收口到 i18n 资源中，再由人工直接维护 `en.json` / `zh-CN.json`。

## 建议命名空间

### app

- 应用标题
- 副标题
- 版本 / shell / 配置路径说明

### nav

- 左侧 tab 名称
- tab 简短描述

### overview

- 概览标题
- 代理状态
- 统计卡标题
- 主操作按钮
- 调试详情标题

### providers

- provider 列表空态
- 创建 / 编辑表单标签
- 导入区文案
- 列表项操作按钮
- 保存 / 删除 / 启停状态消息

### aliases

- alias 列表空态
- alias 表单标签
- target 绑定表单标签
- target 行操作按钮
- 绑定 / 解绑 / 启停状态消息

### sync

- sync 标题
- target path / model / small_model 标签
- preview / apply 按钮
- preview 结果摘要
- apply 结果摘要

### doctor

- doctor 标题
- 检查中 / 成功 / 失败状态
- 问题列表
- 空态与说明文案

### settings

- 主题设置
- 语言设置
- 桌面行为设置
- 关于信息

### actions

- refresh
- save
- reset
- edit
- delete
- enable
- disable
- preview
- apply
- runDoctor

### messages

- refreshing
- saving
- saved
- applying
- previewing
- importing
- loading
- noData

### errors

- requestFailed
- bridgeUnavailable
- invalidHeader

## 需要纳入翻译的文案类型

1. 页面标题与模块标题
2. 按钮文本
3. 表单标签
4. placeholder
5. 空态提示
6. 成功 / 失败 / 进行中状态文案
7. 折叠区标题与说明性文字

## 不建议翻译的内容

1. provider id
2. alias 原始值
3. model id
4. 文件路径
5. URL
6. 原始技术错误详情

## 初始覆盖源文件

- `frontend/src/App.tsx`
- `frontend/src/api.ts`

## 实施建议

1. 先把现有硬编码文案替换为 key
2. `en.json` 先完整收口
3. `zh-CN.json` 先给出可用翻译
4. 人工精修阶段只改 locale 文件，不再回头搜 JSX
