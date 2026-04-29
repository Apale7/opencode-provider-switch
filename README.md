# opencode-provider-switch (`ocswitch`)

English README: `README_EN.md`

`ocswitch` 是给 OpenCode 使用的 provider switcher：你在 OpenCode 里只选择一个稳定模型名，例如 `ocswitch/gpt-5.4`，`ocswitch` 再把这个 alias 路由到一个或多个上游 `provider/model`，并在首字节前失败时按顺序切换到下一个上游。

当前支持 OpenAI Responses、Anthropic Messages、OpenAI-compatible Chat Completions 协议，支持流式响应、请求日志、网络 trace 和可配置路由策略。默认路由策略是 `circuit-breaker`。

## 三种使用方式

`ocswitch` 有三种主要使用方式。命令名需要区分清楚：`ocswitch serve` 只启动本地代理；`ocswitch server` 启动服务器版 Web 管理后台并同时启动代理。

| 模式 | 入口 | 适合场景 |
| --- | --- | --- |
| 仅使用 CLI | `ocswitch provider` / `ocswitch alias` / `ocswitch opencode sync` / `ocswitch serve` | 不需要 UI，希望全程用命令管理 |
| 服务器版 Web | `ocswitch server` | 放在长期运行的服务器上，通过浏览器管理 |
| 桌面应用 | `ocswitch-desktop.exe` | Windows 本机图形化管理、托盘、通知、开机启动 |

## 安装

从源码构建 CLI：

```bash
go build -o ocswitch ./cmd/ocswitch
```

临时运行：

```bash
go run ./cmd/ocswitch --help
```

发布版除桌面 GUI 外，也会提供 Linux amd64 服务器版压缩包：`ocswitch-server-linux-amd64.zip`。压缩包内的 `ocswitch-server` 是同一个 CLI 入口，运行 `./ocswitch-server server` 即可启动服务器版 Web 管理后台。

## 模式一：仅使用 CLI

CLI 模式适合喜欢命令行、脚本化配置或在无桌面环境中运行的人。它不会打开 Web UI，也不会提供桌面托盘；你用命令维护 provider、alias 和 OpenCode 配置，然后用 `ocswitch serve` 启动本地代理。

推荐让 agent 辅助设置。CLI 模式步骤多，容易漏掉 `doctor`、`opencode sync` 或默认模型切换；可以把 provider 清单、目标 alias 和希望同步到哪个 OpenCode 配置文件告诉 agent，让 agent 先查看 `ocswitch --help`，再生成并执行命令。注意不要把真实 API key 发到公共聊天；本机 agent 可用环境变量、私有配置文件或交互输入处理密钥。

可给 agent 的任务描述示例：

```text
帮我配置 ocswitch CLI-only 模式。
Provider：id/baseURL/protocol/model 列表如下，API key 用环境变量读取。
Alias：gpt-5.4 先走 provider-a/model-a，再走 provider-b/model-b。
请先 dry-run，同步到指定 OpenCode 配置，再运行 doctor，最后告诉我用哪个模型名。
```

### 1. 添加或导入 provider

手动添加 provider。`--base-url` 通常需要带 `/v1`，默认会尝试请求上游 `/v1/models` 发现模型列表；如果上游不开放该接口，可加 `--skip-models`。

```bash
ocswitch provider add --id provider-a --base-url https://provider-a.example/v1 --api-key sk-xxx
ocswitch provider add --id provider-b --base-url https://provider-b.example/v1 --api-key sk-yyy
```

如果上游需要额外请求头，可以重复传 `--header`：

```bash
ocswitch provider add \
  --id relay \
  --base-url https://relay.example/v1 \
  --api-key sk-zzz \
  --header "X-Custom-Token=abc" \
  --header "X-Workspace=my-team"
```

如果原来已经在 OpenCode 里配置过 `@ai-sdk/openai` 自定义 provider，可以导入：

```bash
ocswitch provider import-opencode
ocswitch provider import-opencode --from ./examples/opencode.jsonc
```

查看 provider：

```bash
ocswitch provider list
```

### 2. 创建 alias 并绑定上游 target

下面例子表示：OpenCode 使用 `ocswitch/gpt-5.4` 时，优先走 `provider-a/gpt-5.4`，首字节前失败后再走 `provider-b/GPT-5.4`。

```bash
ocswitch alias add --name gpt-5.4 --display-name "GPT 5.4"
ocswitch alias bind --alias gpt-5.4 --model provider-a/gpt-5.4
ocswitch alias bind --alias gpt-5.4 --model provider-b/GPT-5.4
```

查看 alias：

```bash
ocswitch alias list
```

target 顺序就是失败切换顺序。enabled alias 必须至少有一个可路由 target。

### 3. 静态检查

```bash
ocswitch doctor
```

`doctor` 只做静态校验，不会请求真实上游，不会消耗额度。它会检查配置文件、provider 引用、alias 可路由性、本地代理监听地址和 OpenCode 同步目标。

### 4. 同步到 OpenCode

先预览：

```bash
ocswitch opencode sync --dry-run
```

写入 OpenCode 配置：

```bash
ocswitch opencode sync
```

同时设置默认模型：

```bash
ocswitch opencode sync --set-model ocswitch/gpt-5.4
```

同时设置默认大模型和小模型：

```bash
ocswitch opencode sync \
  --set-model ocswitch/gpt-5.4 \
  --set-small-model ocswitch/gpt-5.4-mini
```

写到指定 OpenCode 配置文件：

```bash
ocswitch opencode sync --target /path/to/opencode.jsonc
```

注意：如果目标文件原本是 JSONC，写回时会规范化成普通 JSON，注释和尾逗号不会保留。默认同步目标只看全局用户配置目录，不跟随 `OPENCODE_CONFIG_DIR`。

### 5. 启动本地代理

```bash
ocswitch serve
```

默认代理地址：

```text
http://127.0.0.1:9982/v1
```

默认本地 API key：

```text
ocswitch-local
```

完成 `ocswitch opencode sync` 后，OpenCode 里应该能看到 `ocswitch/<alias>`，例如 `ocswitch/gpt-5.4`。

### 6. 直接验证代理

不经过 OpenCode，也可以直接请求本地代理：

```bash
curl -sN -X POST http://127.0.0.1:9982/v1/responses \
  -H "Authorization: Bearer ocswitch-local" \
  -H "Content-Type: application/json" \
  -d '{"model":"gpt-5.4","stream":true,"input":"hello"}'
```

请求体里的 `model` 可以写 alias 本身，例如 `gpt-5.4`；也兼容 `ocswitch/gpt-5.4`。

## 模式二：服务器版 Web 管理后台

服务器版适合把 `ocswitch` 放在一台长期运行的机器上，通过浏览器管理 provider、alias、代理状态、日志和网络 trace。它复用桌面 GUI 的 Web 页面，但不包含托盘、通知、开机启动等桌面专属能力。

启动服务器版：

```bash
ocswitch server
```

默认管理后台地址：

```text
http://127.0.0.1:9983
```

显式指定监听地址：

```bash
ocswitch server --host 127.0.0.1 --port 9983
```

服务器版会同时启动代理，默认代理仍是：

```text
http://127.0.0.1:9982/v1
```

首次启动时，如果配置文件没有 `admin.api_key`，`ocswitch server` 会生成强随机管理 Token，明文写入本地 `ocswitch` 配置文件，并在日志里打印一次：

```text
[ocswitch-server] admin API key generated and saved in config admin.api_key
[ocswitch-server] Authorization: Bearer <token>
```

打开浏览器后，在登录页粘贴这个 Token。前端只把 Token 保存在当前浏览器标签页的 `sessionStorage` 里，不会持久写入浏览器本地存储。

服务器版注意事项：

- 管理 API `/api/*` 使用 `Authorization: Bearer <admin.api_key>`。
- 代理 API `/v1/*` 使用 `server.api_key`，默认本地值是 `ocswitch-local`。
- 管理 Token 和代理 API key 是两套凭据，不要混用。
- 服务器版不能直接修改用户电脑上的 OpenCode 配置文件。
- `Sync` 页面会生成 OpenCode 配置 JSON，用户需要复制后粘贴到本机 OpenCode 配置文件。
- server 模式继续使用 SQLite 保存请求日志和网络 trace。
- 监听 `0.0.0.0` 或其他非本机地址时，必须用防火墙、可信内网或 HTTPS 反向代理保护管理后台。

如果用 Caddy 同域反代，可以把 `/v1/*` 转发到代理端口，其余请求转发到管理后台：

```caddyfile
ocswitch.example.com {
  reverse_proxy /v1/* 127.0.0.1:9982
  reverse_proxy 127.0.0.1:9983
}
```

## 模式三：桌面应用

桌面应用适合直接在 Windows 上图形化管理 provider、alias、同步、日志和桌面偏好。

当前桌面界面提供：

- 左侧导航页签：`Overview` / `Providers` / `Aliases` / `Log` / `Network` / `Sync` / `Settings`
- 中英文界面切换：`en-US` / `zh-CN` / `system`
- 主题偏好：`light` / `dark` / `system`
- 在 `Settings` 中配置代理超时、路由策略和策略参数
- 托盘行为、通知、开机启动等桌面能力
- 与服务器版 Web 共用同一套前端

构建桌面应用前，先构建前端：

```bash
cd frontend
npm install
npm run build
```

再回到仓库根目录构建桌面应用：

```bash
wails build -tags desktop_wails
```

Windows 默认产物路径：

```text
build/bin/ocswitch-desktop.exe
```

开发模式：

```bash
wails dev -tags desktop_wails
```

直接运行已构建产物：

```bash
./build/bin/ocswitch-desktop.exe
```

提示：Windows 11 一般已内置 WebView2 Runtime，主流 Windows 10 设备通常也已安装。如果桌面应用无法启动，请先安装 Microsoft Edge WebView2 Runtime：

```text
https://developer.microsoft.com/microsoft-edge/webview2/
```

## 共享概念

### Provider

Provider 是真实上游。添加或更新 provider：

```bash
ocswitch provider add --id <id> --base-url <url-with-/v1> --api-key <key>
ocswitch provider add --id <id> --base-url <url-with-/v1> --api-key ""
ocswitch provider add --id <id> --base-url <url-with-/v1> --clear-headers
ocswitch provider add --id <id> --base-url <url-with-/v1> --skip-models
```

如果要清空已保存的上游 API key，显式传 `--api-key ""`。如果要清空额外 header，显式传 `--clear-headers`。

常用 provider 命令：

```bash
ocswitch provider list
ocswitch provider disable <id>
ocswitch provider enable <id>
ocswitch provider remove <id>
```

删除 provider 不会自动清理 alias 里的引用。引用还在时，`ocswitch doctor` 会报错。

### Alias

Alias 是 OpenCode 里看到的稳定模型名。常用 alias 命令：

```bash
ocswitch alias add --name <alias>
ocswitch alias bind --alias <alias> --model <provider-id>/<upstream-model>
ocswitch alias bind --alias <alias> --provider <provider-id> --model <upstream-model>
ocswitch alias unbind --alias <alias> --model <provider-id>/<upstream-model>
ocswitch alias unbind --alias <alias> --provider <provider-id> --model <upstream-model>
ocswitch alias list
ocswitch alias remove <alias>
```

推荐使用 `--model <provider-id>/<upstream-model>`。旧的 `--provider <id> --model <model>` 仍保留作为兼容兜底。

### OpenCode 同步

`ocswitch opencode sync` 只更新 OpenCode 配置里的 `provider.ocswitch`，默认不会修改顶层 `model` 或 `small_model`，除非显式传 `--set-model` 或 `--set-small-model`。

默认行为：

- 优先复用全局 OpenCode 配置文件：`opencode.jsonc` > `opencode.json` > `config.json`
- 如果都不存在，就创建 `~/.config/opencode/opencode.jsonc`
- 默认目标明确只看全局用户配置目录，不跟随 `OPENCODE_CONFIG_DIR`
- 只同步可路由 alias

### 配置文件

本地 `ocswitch` 配置文件默认路径：

- 如果设置了 `OCSWITCH_CONFIG`，优先使用它
- 否则使用 `$XDG_CONFIG_HOME/ocswitch/config.json`
- 再否则使用 `~/.config/ocswitch/config.json`

也可以对每个命令显式指定：

```bash
ocswitch --config /path/to/config.json doctor
```

命令级行为、默认值、写入范围与副作用，以对应命令的 `--help` 为准。

### 失败切换规则

`ocswitch` 的切换规则很保守：只有在还没向下游写出任何字节前，才允许切换到下一个 target。一旦开始输出流，当前上游就锁定；不支持中途把一半流接到另一个 provider 上继续输出。

会切换到下一个 target 的情况：

- 连接失败
- DNS / 网络错误
- 上游在返回首字节前超时或断开
- 上游返回 `429`
- 上游返回 `5xx`

不会切换的情况：

- alias 不存在、被禁用或没有可用 target
- 上游返回 `400` / `401` / `403` / `404`
- 响应已经开始向客户端写出后才出错

默认 `circuit-breaker` 策略会在 provider 连续出现可重试失败后，冷却一段时间并临时跳过它；冷却结束后再以半开探测方式恢复。失败阈值、冷却时间、回退倍率、半开并发等参数可在桌面 `Settings` 或配置文件 `server.routing` 中调整。

### 调试响应头

当请求已经选定某个上游并开始返回时，响应会附带这些头：

- `X-OCSWITCH-Alias`
- `X-OCSWITCH-Provider`
- `X-OCSWITCH-Remote-Model`
- `X-OCSWITCH-Attempt`
- `X-OCSWITCH-Failover-Count`

### 日志与 trace

桌面应用和服务器版 Web 都可以查看业务日志与网络详情，包括 failover 链路、状态码、TTFB、请求/响应元数据，以及 token / usage 诊断信息。日志字段参考：`docs/ocswitch-log-field-reference.md`。

## CLI 参考

README 是快速上手说明，准确行为以命令自己的 `--help` 为准。

```bash
ocswitch serve
ocswitch server [--host HOST] [--port PORT]
ocswitch doctor
ocswitch provider {add,list,enable,disable,remove,import-opencode}
ocswitch alias {add,list,bind,unbind,remove}
ocswitch opencode sync [--target FILE] [--set-model ALIAS] [--set-small-model ALIAS] [--dry-run]
ocswitch --config PATH <command>
```

## 常见问题

### 为什么 `opencode models` 里看不到 `ocswitch/<alias>`？

先检查：是否执行过 `ocswitch opencode sync`，alias 是否 enabled，alias 是否至少有一个可路由 target，alias 绑定的 provider 是否都被禁用，OpenCode 当前实际使用的配置文件是否就是同步写入的文件。执行 `ocswitch doctor` 可以看到 OpenCode config target。

### 为什么 `ocswitch doctor` 报 alias 没有可用 target？

enabled alias 必须至少有一个可路由 target。可路由 target 需要自身 enabled，引用的 provider 存在，并且 provider 没有被禁用。

### 为什么禁用 provider 不会改 alias target？

因为 provider 可能被多个 alias 复用。`ocswitch provider disable` 只让路由层跳过这个 provider，不改写 alias 里的 target 状态，避免重新启用 provider 时破坏原有 alias 关系。

### 删除 provider 后为什么还有报错？

因为 alias 里的 target 还是旧引用。继续执行：

```bash
ocswitch alias unbind --alias <alias> --model <provider-id>/<model>
ocswitch alias unbind --alias <alias> --provider <provider-id> --model <model>
```

### 服务器版忘记管理 Token 怎么办？

服务器版管理 Token 明文保存在 `ocswitch` 配置文件的 `admin.api_key` 字段。如果要轮换 Token，可以停止 `ocswitch server`，手动修改或删除 `admin.api_key`，再重新启动。字段为空时会重新生成强随机 Token 并打印在日志里。

### 服务器版如何配置本机 OpenCode？

服务器版运行在服务器上，不能直接修改用户电脑上的 OpenCode 配置文件。请在 Web 管理后台打开 `Sync` 页面，生成配置，复制生成的 OpenCode 配置 JSON，然后粘贴到本机 OpenCode 配置文件。

## 安全说明

- 默认只监听 `127.0.0.1`
- 上游凭据保存在本地 `ocswitch` 配置文件中
- 服务器版管理 Token 明文保存在 `admin.api_key`，用于防止忘记 Token 后无法恢复
- 服务器版 `/api/*` 要求 Bearer Token，并带基础安全响应头
- 监听非本机地址时，应使用防火墙、可信内网或 HTTPS 反向代理
- 本项目当前没有做多用户或 RBAC 权限模型

请把本地 `ocswitch` 配置文件当作敏感文件处理。

## 范围

不支持：自动按延迟、价格、提示词类型选路；中途流式拼接切换；计费统计；完整接管 OpenCode 所有配置层；从 `auth.json` 自动导入 provider 定义；多用户账号或 RBAC。
