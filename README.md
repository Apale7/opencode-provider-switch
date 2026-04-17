# opencode-provider-switch (`ocswitch`)

English README: `README_EN.md`

`ocswitch` 是 OpenCode Provider Switch CLI，给 OpenCode 使用的本地代理。

它解决的问题很简单：

- 你在 OpenCode 里只使用一个稳定的模型名，例如 `ocswitch/gpt-5.4`
- `ocswitch` 在本地把这个别名映射到多个上游 `provider/model`
- 按你配置的顺序依次尝试上游
- 如果主上游在响应开始前失败，自动切到下一个上游

当前实现只支持 OpenAI Responses 协议，也就是 `POST /v1/responses`，并且支持流式响应。

## 适合什么场景

- 你有多个 OpenAI 兼容上游
- 你不想在 OpenCode 里频繁切换 provider 或模型入口
- 你希望用一个固定别名承接多个备用上游
- 你希望失败切换行为是确定的、可预期的

## 当前能力

- 本地维护 `ocswitch` 配置文件：上游 provider、alias、监听地址
- 支持手动添加 provider
- 支持从 OpenCode 配置导入 `@ai-sdk/openai` 自定义 provider
- 支持创建 alias，并按顺序绑定多个上游 target
- 支持把 alias 同步到 OpenCode 的 `provider.ocswitch.models`
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
go build -o ocswitch ./cmd/ocswitch
```

如果你只想临时运行，也可以直接：

```bash
go run ./cmd/ocswitch --help
```

## 5 分钟快速上手

### 1. 添加上游 provider

`ocswitch` 要求上游是 OpenAI 兼容接口，并且 `--base-url` 需要带上 `/v1`。

```bash
ocswitch provider add --id su8 --base-url https://cn2.su8.codes/v1 --api-key sk-xxx
ocswitch provider add --id codex --base-url https://api-vip.codex-for.me/v1 --api-key sk-yyy
```

如果某个上游还需要额外请求头，可以重复传 `--header`：

```bash
ocswitch provider add \
  --id relay \
  --base-url https://example.com/v1 \
  --api-key sk-zzz \
  --header "X-Custom-Token=abc" \
  --header "X-Workspace=my-team"
```

查看当前 provider：

```bash
ocswitch provider list
```

### 2. 创建 alias，并绑定多个上游 target

下面这个例子表示：当你使用 `ocswitch/gpt-5.4` 时，优先走 `su8/gpt-5.4`，失败后再走 `codex/GPT-5.4`。

```bash
ocswitch alias add --name gpt-5.4 --display-name "GPT 5.4"
ocswitch alias bind --alias gpt-5.4 --provider su8 --model gpt-5.4
ocswitch alias bind --alias gpt-5.4 --provider codex --model GPT-5.4
```

查看当前 alias：

```bash
ocswitch alias list
```

注意：

- target 的顺序就是失败切换顺序
- enabled alias 必须至少有一个可路由 target
- `ocswitch alias bind` 在 alias 不存在时会自动创建一个 enabled alias

### 3. 先做一次静态检查

```bash
ocswitch doctor
```

`ocswitch doctor` 只做静态校验，不会真的请求上游，不会消耗额度。

它会检查：

- 本地 `ocswitch` 配置能不能正常加载
- alias 是否引用了不存在的 provider
- enabled alias 是否至少有一个可路由 target
- 本地代理监听地址是否合理
- 默认会同步到哪个 OpenCode 配置文件

### 4. 把 alias 同步到 OpenCode

```bash
ocswitch opencode sync
```

这个命令会做一件事：把当前可路由的 alias 列表同步进 OpenCode 的 `provider.ocswitch.models`。

注意：如果目标文件原本是 JSONC，`sync` 写回时会规范化成普通 JSON，因此注释和尾逗号不会保留。
默认行为：

- 优先复用全局 OpenCode 配置文件：`opencode.jsonc` > `opencode.json` > `config.json`
- 如果都不存在，就创建 `~/.config/opencode/opencode.jsonc`
- 默认目标明确只看全局用户配置目录，不跟随 `OPENCODE_CONFIG_DIR`
- 只更新 `provider.ocswitch`
- 不会修改顶层 `model`
- 不会修改顶层 `small_model`

如果你希望顺手把默认模型也切到 `ocswitch`，需要显式指定：

```bash
ocswitch opencode sync --set-model ocswitch/gpt-5.4
```

如果你还有小模型 alias，也可以这样：

```bash
ocswitch opencode sync \
  --set-model ocswitch/gpt-5.4 \
  --set-small-model ocswitch/gpt-5.4-mini
```

先预览不写入：

```bash
ocswitch opencode sync --dry-run
```

写到指定 OpenCode 配置文件：

```bash
ocswitch opencode sync --target /path/to/opencode.jsonc
```

### 5. 启动本地代理

```bash
ocswitch serve
```

默认监听地址：

- `127.0.0.1:9982`
- 本地 API Key：`ocswitch-local`

启动后，本地代理地址是：

```text
http://127.0.0.1:9982/v1
```

### 6. 在 OpenCode 里使用

完成 `ocswitch opencode sync` 后，你应该能在 OpenCode 里看到 `ocswitch/<alias>`。

例如：

- `ocswitch/gpt-5.4`

如果你执行了：

```bash
ocswitch opencode sync --set-model ocswitch/gpt-5.4
```

那么 OpenCode 默认模型也会直接切到这个 alias。

## 直接验证本地代理

如果你想先不走 OpenCode，直接验证 `ocswitch` 是否能正常代理，可以自己发一个请求：

```bash
curl -sN -X POST http://127.0.0.1:9982/v1/responses \
  -H "Authorization: Bearer ocswitch-local" \
  -H "Content-Type: application/json" \
  -d '{"model":"gpt-5.4","stream":true,"input":"hello"}'
```

注意这里请求体里的 `model` 是 alias 本身，例如 `gpt-5.4`，不是 `ocswitch/gpt-5.4`。

因为 `ocswitch/gpt-5.4` 是 OpenCode 侧的模型选择写法；真正发到本地 provider 的请求里，模型名会是 alias 自身。

## 从现有 OpenCode 配置导入 provider

如果你原来已经在 OpenCode 里配置过一些自定义 provider，可以直接导入：

```bash
ocswitch provider import-opencode
```

或者指定导入源：

```bash
ocswitch provider import-opencode --from ./examples/opencode.jsonc
```

支持范围只有这一类：

- `npm: @ai-sdk/openai`
- 有 `options.baseURL`
- `options.apiKey` 可以为空；导入后由 `ocswitch doctor` / `serve` 前校验帮助你发现风险
注意：

- 这不是完整迁移工具
- 默认导入源也只看全局用户配置目录，不跟随 `OPENCODE_CONFIG_DIR`
- 如果你要导入别的 OpenCode 配置文件，请显式传 `--from`
- 当前只导入 provider 的基本连接信息
- 如果你的旧配置依赖额外自定义 header，需要导入后自己用 `ocswitch provider add --header ...` 补齐
- `ocswitch` 自己不会被反向导入

覆盖已存在的 provider：

```bash
ocswitch provider import-opencode --overwrite
```

## 常用命令

### provider

添加或更新 provider：

```bash
ocswitch provider add --id <id> --base-url <url-with-/v1> --api-key <key>
```

查看 provider：

```bash
ocswitch provider list
```

禁用 provider：

```bash
ocswitch provider disable <id>
```

重新启用 provider：

```bash
ocswitch provider enable <id>
```

删除 provider：

```bash
ocswitch provider remove <id>
```

注意：删除 provider 不会自动帮你清理 alias 里的引用。引用还在的话，`ocswitch doctor` 会报错。

### alias

创建或更新 alias：

```bash
ocswitch alias add --name <alias>
```

给 alias 追加一个 target：

```bash
ocswitch alias bind --alias <alias> --provider <provider-id> --model <upstream-model>
```

解绑 target：

```bash
ocswitch alias unbind --alias <alias> --provider <provider-id> --model <upstream-model>
```

查看 alias：

```bash
ocswitch alias list
```

删除 alias：

```bash
ocswitch alias remove <alias>
```

### 其他

静态检查：

```bash
ocswitch doctor
```

启动代理：

```bash
ocswitch serve
```

同步到 OpenCode：

```bash
ocswitch opencode sync
```

全局帮助：

```bash
ocswitch --help
ocswitch provider --help
ocswitch alias --help
ocswitch opencode sync --help
```

## 配置文件说明

本地 `ocswitch` 配置文件默认路径：

- 如果设置了 `OCSWITCH_CONFIG`，优先使用它
- 否则使用 `$XDG_CONFIG_HOME/ocswitch/config.json`
- 再否则使用 `~/.config/ocswitch/config.json`

也可以对每个命令显式指定：

```bash
ocswitch --config /path/to/config.json doctor
```

命令级行为、默认值、写入范围与副作用，以对应命令的 `--help` 为准；README 主要保留快速上手与背景说明。

一个最小配置示例：

```json
{
  "server": {
    "host": "127.0.0.1",
    "port": 9982,
    "api_key": "ocswitch-local"
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
      "api_key": "sk-yyy",
      "disabled": true
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

`ocswitch` 的切换规则很保守，也很容易理解。

会切换到下一个 target 的情况：

- 连接失败
- DNS / 网络错误
- 上游在返回首字节前超时或断开
- 上游返回 `429`
- 上游返回 `5xx`

不会切换的情况：

- alias 不存在
- alias 被禁用
- alias 没有可用 target
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

- `X-OCSWITCH-Alias`
- `X-OCSWITCH-Provider`
- `X-OCSWITCH-Remote-Model`
- `X-OCSWITCH-Attempt`
- `X-OCSWITCH-Failover-Count`

你可以用它们确认：

- 本次请求命中了哪个 alias
- 实际走了哪个 provider
- 实际转发成了哪个上游 model
- 这是第几次尝试
- 前面已经失败切换过几次

## 常见问题

### 为什么 `opencode models` 里看不到 `ocswitch/<alias>`？

先检查这几件事：

1. 你是否执行过 `ocswitch opencode sync`
2. 你的 alias 是否是 enabled 状态
3. alias 是否至少绑定了一个可路由 target
4. alias 绑定的 provider 是否都被禁用了
5. OpenCode 当前实际使用的配置文件，是否就是 `ocswitch opencode sync` 写入的那个文件
6. 执行一次 `ocswitch doctor`，看输出里的 `opencode config target`

### 为什么 `ocswitch doctor` 报 alias 没有可用 target？

因为当前实现要求：只要 alias 是 enabled，就必须至少有一个可路由的 target。

可路由的意思是同时满足：

- target 自己是 enabled
- target 引用的 provider 存在且没有被禁用

你可以：

- 给它绑定 target
- 重新启用被禁用的 provider
- 或者把这个 alias 改成 disabled 后再保存

### 为什么要禁用 provider，而不是把 alias target 也改成 disabled？

因为 provider 可能被多个 alias 复用。

`ocswitch provider disable` 只会让路由层在 failover 时自动跳过这个 provider，不会改写 alias 里的 target 状态，这样重新启用 provider 时不会和 alias 上原有的启用关系打架。

### 删除 provider 后为什么还有报错？

因为 alias 里的 target 还是旧引用。需要继续执行：

```bash
ocswitch alias unbind --alias <alias> --provider <provider-id> --model <model>
```

### 本地代理鉴权是什么？

默认是静态 key：

```text
ocswitch-local
```

OpenCode 在 `provider.ocswitch.options.apiKey` 里会使用这个值。直接手工请求本地代理时，也要带上这个 key。

## 安全说明

- 默认只监听 `127.0.0.1`
- 上游凭据保存在本地 `ocswitch` 配置文件中
- 本项目当前没有做多用户或远程网络安全保证

所以请把本地配置文件当成敏感文件处理。

## 英文版 README

原英文版文档已保存在：

- `README_EN.md`
