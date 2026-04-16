# opencode-provider-switch (`ops`)

`ops` 是一个给 OpenCode 使用的本地代理。

它解决的问题很简单：

- 你在 OpenCode 里只使用一个稳定的模型名，例如 `ops/gpt-5.4`
- `ops` 在本地把这个别名映射到多个上游 `provider/model`
- 按你配置的顺序依次尝试上游
- 如果主上游在响应开始前失败，自动切到下一个上游

当前实现只支持 OpenAI Responses 协议，也就是 `POST /v1/responses`，并且支持流式响应。

## 适合什么场景

- 你有多个 OpenAI 兼容上游
- 你不想在 OpenCode 里频繁切换 provider
- 你希望用一个固定别名承接多个备用上游
- 你希望失败切换行为是确定的、可预期的

## 当前能力

- 本地维护 `ops` 配置文件：上游 provider、alias、监听地址
- 支持手动添加 provider
- 支持从 OpenCode 配置导入 `@ai-sdk/openai` 自定义 provider
- 支持创建 alias，并按顺序绑定多个上游 target
- 支持把 alias 同步到 OpenCode 的 `provider.ops.models`
- 支持本地代理 `POST /v1/responses`
- 支持流式透传
- 支持首字节前失败切换
- 返回调试响应头，便于确认这次请求实际落到了哪个上游

## 不支持的内容

- Anthropic 原生协议
- 多协议路由
- 自动按延迟、价格、提示词类型选路
- 中途流式拼接切换
- SQLite、管理后台、计费统计
- 完整接管 OpenCode 所有配置层
- 从 `auth.json` 自动导入 provider 定义

## 安装

```bash
go build -o ops ./cmd/ops
```

如果你只想临时运行，也可以直接：

```bash
go run ./cmd/ops --help
```

## 5 分钟快速上手

### 1. 添加上游 provider

`ops` 要求上游是 OpenAI 兼容接口，并且 `--base-url` 需要带上 `/v1`。

```bash
ops provider add --id su8 --base-url https://cn2.su8.codes/v1 --api-key sk-xxx
ops provider add --id codex --base-url https://api-vip.codex-for.me/v1 --api-key sk-yyy
```

如果某个上游还需要额外请求头，可以重复传 `--header`：

```bash
ops provider add \
  --id relay \
  --base-url https://example.com/v1 \
  --api-key sk-zzz \
  --header "X-Custom-Token=abc" \
  --header "X-Workspace=my-team"
```

查看当前 provider：

```bash
ops provider list
```

### 2. 创建 alias，并绑定多个上游 target

下面这个例子表示：当你使用 `ops/gpt-5.4` 时，优先走 `su8/gpt-5.4`，失败后再走 `codex/GPT-5.4`。

```bash
ops alias add --name gpt-5.4 --display-name "GPT 5.4"
ops alias bind --alias gpt-5.4 --provider su8 --model gpt-5.4
ops alias bind --alias gpt-5.4 --provider codex --model GPT-5.4
```

查看当前 alias：

```bash
ops alias list
```

注意：

- target 的顺序就是失败切换顺序
- enabled alias 必须至少有一个 enabled target
- `ops alias bind` 在 alias 不存在时会自动创建一个 enabled alias

### 3. 先做一次静态检查

```bash
ops doctor
```

`ops doctor` 只做静态校验，不会真的请求上游，不会消耗额度。

它会检查：

- 本地 `ops` 配置能不能正常加载
- alias 是否引用了不存在的 provider
- enabled alias 是否至少有一个 enabled target
- 本地代理监听地址是否合理
- 默认会同步到哪个 OpenCode 配置文件

### 4. 把 alias 同步到 OpenCode

```bash
ops opencode sync
```

这个命令会做一件事：把当前 alias 列表同步进 OpenCode 的 `provider.ops.models`。

默认行为：

- 优先复用全局 OpenCode 配置文件：`opencode.jsonc` > `opencode.json` > `config.json`
- 如果都不存在，就创建 `~/.config/opencode/opencode.jsonc`
- 只更新 `provider.ops`
- 不会修改顶层 `model`
- 不会修改顶层 `small_model`

如果你希望顺手把默认模型也切到 `ops`，需要显式指定：

```bash
ops opencode sync --set-model ops/gpt-5.4
```

如果你还有小模型 alias，也可以这样：

```bash
ops opencode sync \
  --set-model ops/gpt-5.4 \
  --set-small-model ops/gpt-5.4-mini
```

先预览不写入：

```bash
ops opencode sync --dry-run
```

写到指定 OpenCode 配置文件：

```bash
ops opencode sync --target /path/to/opencode.jsonc
```

### 5. 启动本地代理

```bash
ops serve
```

默认监听地址：

- `127.0.0.1:9982`
- 本地 API Key：`ops-local`

启动后，本地代理地址是：

```text
http://127.0.0.1:9982/v1
```

### 6. 在 OpenCode 里使用

完成 `ops opencode sync` 后，你应该能在 OpenCode 里看到 `ops/<alias>`。

例如：

- `ops/gpt-5.4`

如果你执行了：

```bash
ops opencode sync --set-model ops/gpt-5.4
```

那么 OpenCode 默认模型也会直接切到这个 alias。

## 直接验证本地代理

如果你想先不走 OpenCode，直接验证 `ops` 是否能正常代理，可以自己发一个请求：

```bash
curl -sN -X POST http://127.0.0.1:9982/v1/responses \
  -H "Authorization: Bearer ops-local" \
  -H "Content-Type: application/json" \
  -d '{"model":"gpt-5.4","stream":true,"input":"hello"}'
```

注意这里请求体里的 `model` 是 alias 本身，例如 `gpt-5.4`，不是 `ops/gpt-5.4`。

因为 `ops/gpt-5.4` 是 OpenCode 侧的模型选择写法；真正发到本地 provider 的请求里，模型名会是 alias 自身。

## 从现有 OpenCode 配置导入 provider

如果你原来已经在 OpenCode 里配置过一些自定义 provider，可以直接导入：

```bash
ops provider import-opencode
```

或者指定导入源：

```bash
ops provider import-opencode --from ./examples/opencode.jsonc
```

支持范围只有这一类：

- `npm: @ai-sdk/openai`
- 有 `options.baseURL`
- 有 `options.apiKey`

注意：

- 这不是完整迁移工具
- 当前只导入 provider 的基本连接信息
- 如果你的旧配置依赖额外自定义 header，需要导入后自己用 `ops provider add --header ...` 补齐
- `ops` 自己不会被反向导入

覆盖已存在的 provider：

```bash
ops provider import-opencode --overwrite
```

## 常用命令

### provider

添加或更新 provider：

```bash
ops provider add --id <id> --base-url <url-with-/v1> --api-key <key>
```

查看 provider：

```bash
ops provider list
```

删除 provider：

```bash
ops provider remove <id>
```

注意：删除 provider 不会自动帮你清理 alias 里的引用。引用还在的话，`ops doctor` 会报错。

### alias

创建或更新 alias：

```bash
ops alias add --name <alias>
```

给 alias 追加一个 target：

```bash
ops alias bind --alias <alias> --provider <provider-id> --model <upstream-model>
```

解绑 target：

```bash
ops alias unbind --alias <alias> --provider <provider-id> --model <upstream-model>
```

查看 alias：

```bash
ops alias list
```

删除 alias：

```bash
ops alias remove <alias>
```

### 其他

静态检查：

```bash
ops doctor
```

启动代理：

```bash
ops serve
```

同步到 OpenCode：

```bash
ops opencode sync
```

全局帮助：

```bash
ops --help
ops provider --help
ops alias --help
ops opencode sync --help
```

## 配置文件说明

本地 `ops` 配置文件默认路径：

- 如果设置了 `OPS_CONFIG`，优先使用它
- 否则使用 `$XDG_CONFIG_HOME/ops/config.json`
- 再否则使用 `~/.config/ops/config.json`

也可以对每个命令显式指定：

```bash
ops --config /path/to/config.json doctor
```

一个最小配置示例：

```json
{
  "server": {
    "host": "127.0.0.1",
    "port": 9982,
    "api_key": "ops-local"
  },
  "providers": [
    {
      "id": "su8",
      "name": "SU8",
      "base_url": "https://cn2.su8.codes/v1",
      "api_key": "sk-xxx"
    },
    {
      "id": "codex",
      "name": "Codex",
      "base_url": "https://api-vip.codex-for.me/v1",
      "api_key": "sk-yyy"
    }
  ],
  "aliases": [
    {
      "alias": "gpt-5.4",
      "display_name": "GPT 5.4",
      "enabled": true,
      "targets": [
        {
          "provider": "su8",
          "model": "gpt-5.4",
          "enabled": true
        },
        {
          "provider": "codex",
          "model": "GPT-5.4",
          "enabled": true
        }
      ]
    }
  ]
}
```

## 失败切换规则

`ops` 的切换规则很保守，也很容易理解。

会切换到下一个 target 的情况：

- 连接失败
- DNS / 网络错误
- 上游在返回首字节前超时或断开
- 上游返回 `429`
- 上游返回 `5xx`

不会切换的情况：

- alias 不存在
- alias 被禁用
- alias 没有 enabled target
- 上游返回 `400`
- 上游返回 `401`
- 上游返回 `403`
- 上游返回 `404`
- 响应已经开始向客户端写出后才出错

重点是：

- 只有在“还没向下游写出任何字节”之前，才允许切换
- 一旦开始向客户端输出流，当前上游就锁定了
- 不支持中途把一半流接到另一个 provider 上继续输出

## 调试响应头

每次成功代理或透传上游错误时，响应里都会附带这些头：

- `X-OPS-Alias`
- `X-OPS-Provider`
- `X-OPS-Remote-Model`
- `X-OPS-Attempt`
- `X-OPS-Failover-Count`

你可以用它们确认：

- 本次请求命中了哪个 alias
- 实际走了哪个 provider
- 实际转发成了哪个上游 model
- 这是第几次尝试
- 前面已经失败切换过几次

## 常见问题

### 为什么 `opencode models` 里看不到 `ops/<alias>`？

先检查这几件事：

1. 你是否执行过 `ops opencode sync`
2. 你的 alias 是否是 enabled 状态
3. alias 是否至少绑定了一个 enabled target
4. OpenCode 当前实际使用的配置文件，是否就是 `ops opencode sync` 写入的那个文件
5. 执行一次 `ops doctor`，看输出里的 `opencode config target`

### 为什么 `ops doctor` 报 alias 没有 enabled target？

因为当前实现要求：只要 alias 是 enabled，就必须至少有一个 enabled target。

你可以：

- 给它绑定 target
- 或者把这个 alias 改成 disabled 后再保存

### 删除 provider 后为什么还有报错？

因为 alias 里的 target 还是旧引用。需要继续执行：

```bash
ops alias unbind --alias <alias> --provider <provider-id> --model <model>
```

### 本地代理鉴权是什么？

默认是静态 key：

```text
ops-local
```

OpenCode 在 `provider.ops.options.apiKey` 里会使用这个值。直接手工请求本地代理时，也要带上这个 key。

## 安全说明

- 默认只监听 `127.0.0.1`
- 上游凭据保存在本地 `ops` 配置文件中
- 本项目当前没有做多用户或远程网络安全保证

所以请把本地配置文件当成敏感文件处理。

## 英文版 README

原英文版文档已保存在：

- `README_EN.md`
