# OpenCode Go 模型与 Alias 维护流程

本文记录 OpenCode Go 官方模型列表更新后，维护线上 `opencode go` Provider、Group 模型目录和手动 Alias 的流程。

> 本流程不包含 API Key 新增、更换、排序或删除。Key 由管理员通过网页管理。

## 1. 固定线上契约

| 项目 | 值 |
|---|---|
| Provider ID | `opencode go` |
| Base URL | `https://opencode.ai/zen/go/v1` |
| OpenAI Group ID | `openai-compatible` |
| OpenAI 协议 | `openai-compatible` |
| Anthropic Group ID | `anthropic` |
| Anthropic 协议 | `anthropic-messages` |
| Alias 格式 | `opencode-go/<model-id>` |
| Alias Target Model | 上游原始 `<model-id>` |

## 2. 获取官方信息

只使用普通 HTTP GET 或 Web Search，不使用浏览器自动化。

1. 获取官方 API 端点表：
   - `https://opencode.ai/docs/go/#endpoints`
   - 中文页面：`https://opencode.ai/docs/zh-cn/go/#api-%E7%AB%AF%E7%82%B9`
2. 获取实时模型目录：
   - `GET https://opencode.ai/zen/go/v1/models`
3. 提取 `/v1/models` 返回的全部 `data[].id`。
4. 从官方端点表提取每个模型对应的协议：
   - `/v1/chat/completions` → `openai-compatible`
   - `/v1/messages` → `anthropic-messages`

`/v1/models` 目前不提供协议字段，因此不能仅凭模型列表自动决定 Group。

## 3. 差异分类规则

将实时模型列表、官方端点表和线上现有模型目录进行三方比较：

1. **实时列表与官方端点表均存在**：按官方端点表协议配置。
2. **实时列表新增、但官方端点表未说明协议**：不得猜测或直接上线；列出模型 ID，请管理员确认协议或确认是否为已下线/残留模型。
3. **官方端点表存在、但实时列表缺失**：先报告，不自动删除。
4. **线上存在、官方和实时列表均缺失**：标记为疑似下线，请管理员确认后再删除 Group 模型项和 Alias。
5. 不根据模型名称前缀擅自推断协议；名称家族只能作为人工确认参考。

## 4. 生成变更计划

变更前输出不含密钥的计划：

- 新增、保留、删除的模型 ID；
- 每个模型所属 Group；
- 将新增或删除的 Alias；
- Alias 冲突；
- Provider/Group 是否已经存在。

Alias 使用以下结构：

```json
{
  "alias": "opencode-go/<model-id>",
  "display_name": "OpenCode Go / <model-id>",
  "protocol": "<group-protocol>",
  "enabled": true,
  "targets": [
    {
      "provider": "opencode go",
      "group": "<group-id>",
      "model": "<model-id>",
      "enabled": true
    }
  ]
}
```

维护模型和 Alias 时必须保持现有 Key 字段原样，不输出、重排或重写 Key 池。

## 5. 安全更新线上配置

线上配置路径：`/root/.config/ocswitch/config.json`。

1. 读取配置时只输出脱敏结构和数量，不输出任何 Key。
2. 检查 `schema_version == 2`。
3. 检查 Provider、Group 和 Alias 唯一性。
4. 在同目录生成权限为 `0600` 的候选配置文件。
5. 使用候选配置执行静态检查：

   ```bash
   /usr/local/bin/ocswitch-server \
     --config /root/.config/ocswitch/config.json.ocgo.new \
     doctor
   ```

6. `doctor` 不得包含 `[error/*]`；已有环境 warning 可以单独记录。
7. 创建带时间戳的配置备份：

   ```text
   /root/.config/ocswitch/config.json.bak.ocgo.<timestamp>
   ```

8. 使用同文件系统原子替换配置并重启：

   ```bash
   systemctl restart ocswitch-server
   systemctl is-active ocswitch-server
   ```

9. 若服务启动失败，立即恢复备份并再次启动服务。

## 6. 验证

更新后至少验证：

1. Provider 仍只有两个目标 Group，协议正确。
2. 两个 Group 的模型数量与确认后的映射一致。
3. `opencode-go/` Alias 数量与目标模型总数一致。
4. 每个 Alias 只指向 `opencode go` 下正确的 Group 和原始模型 ID。
5. 选取一个 OpenAI-compatible 模型，通过本地 `/v1/chat/completions` 发起最小请求，期望 HTTP 200。
6. 选取一个 Anthropic 模型，通过本地 `/v1/messages` 发起最小请求，期望 HTTP 200。
7. `systemctl is-active ocswitch-server` 返回 `active`。
8. `https://ocswitch.apale7.cn/` 返回 HTTP 200。
9. 清理全部候选文件、临时脚本和响应文件。

验证过程不得打印客户端鉴权、上游 Key 或完整敏感响应头。

## 7. 2026-07-28 基线

当前线上基线为 17 个模型：

### OpenAI-compatible（11）

- `grok-4.5`
- `glm-5.2`
- `glm-5.1`
- `kimi-k3`
- `kimi-k2.7-code`
- `kimi-k2.6`
- `deepseek-v4-pro`
- `deepseek-v4-flash`
- `mimo-v2.5`
- `mimo-v2.5-pro`
- `hy3`

### Anthropic Messages（6）

- `minimax-m3`
- `minimax-m2.7`
- `minimax-m2.5`
- `qwen3.7-max`
- `qwen3.7-plus`
- `qwen3.6-plus`

以下模型当时仍由 `/v1/models` 返回，但管理员已确认下线，因此未配置：

- `kimi-k2.5`
- `glm-5`
- `qwen3.5-plus`
- `mimo-v2-pro`
- `mimo-v2-omni`
- `hy3-preview`
