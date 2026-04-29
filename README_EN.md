# opencode-provider-switch (`ocswitch`)

`ocswitch` is a provider switcher for OpenCode. OpenCode sees one stable model name, for example `ocswitch/gpt-5.4`; `ocswitch` routes that alias to one or more upstream `provider/model` targets and fails over to the next target when the first attempt fails before first byte.

Supported protocols: OpenAI Responses, Anthropic Messages, and OpenAI-compatible Chat Completions. Streaming, request logs, network traces, and configurable routing strategies are supported. The default routing strategy is `circuit-breaker`.

## Three Usage Modes

`ocswitch` has three main usage modes. Command names are intentionally different: `ocswitch serve` starts only the local proxy; `ocswitch server` starts the server web admin and the proxy.

| Mode | Entry point | Best for |
| --- | --- | --- |
| CLI only | `ocswitch provider` / `ocswitch alias` / `ocswitch opencode sync` / `ocswitch serve` | No UI, scriptable setup |
| Server web admin | `ocswitch server` | Long-running host managed from a browser |
| Desktop app | `ocswitch-desktop.exe` | Windows GUI, tray, notifications, launch-at-login |

## Install

Build the CLI from source:

```bash
go build -o ocswitch ./cmd/ocswitch
```

Run temporarily:

```bash
go run ./cmd/ocswitch --help
```

Release assets also include a Linux amd64 server archive: `ocswitch-server-linux-amd64.zip`. The `ocswitch-server` binary is the same CLI entrypoint; run `./ocswitch-server server` to start the server web admin.

## Mode 1: CLI Only

CLI-only mode is for users who prefer commands, scripts, or headless environments. It opens no web UI and provides no desktop tray. You manage providers, aliases, and OpenCode config with commands, then run `ocswitch serve` to start the local proxy.

Using an agent for this mode is recommended. The CLI flow has several steps and it is easy to miss `doctor`, `opencode sync`, or default-model switching. Give the agent your provider list, target aliases, and OpenCode config target; ask it to inspect `ocswitch --help`, generate commands, run dry-run first, then execute. Do not paste real API keys into public chats; local agents can use environment variables, private files, or interactive input for secrets.

Example agent prompt:

```text
Help me configure ocswitch in CLI-only mode.
Providers: id/baseURL/protocol/model list below; read API keys from env vars.
Alias: gpt-5.4 should try provider-a/model-a, then provider-b/model-b.
Run dry-run first, sync to this OpenCode config file, run doctor, then tell me which model name to select.
```

### 1. Add or import providers

Add providers manually. `--base-url` usually needs the `/v1` suffix. By default, `ocswitch` tries to discover models from upstream `/v1/models`; add `--skip-models` if the upstream does not expose that endpoint.

```bash
ocswitch provider add --id provider-a --base-url https://provider-a.example/v1 --api-key sk-xxx
ocswitch provider add --id provider-b --base-url https://provider-b.example/v1 --api-key sk-yyy
```

For extra upstream headers, repeat `--header`:

```bash
ocswitch provider add \
  --id relay \
  --base-url https://relay.example/v1 \
  --api-key sk-zzz \
  --header "X-Custom-Token=abc" \
  --header "X-Workspace=my-team"
```

Import existing `@ai-sdk/openai` custom providers from OpenCode config:

```bash
ocswitch provider import-opencode
ocswitch provider import-opencode --from ./examples/opencode.jsonc
```

List providers:

```bash
ocswitch provider list
```

### 2. Create aliases and bind targets

This example means: when OpenCode uses `ocswitch/gpt-5.4`, first try `provider-a/gpt-5.4`; if it fails before first byte, try `provider-b/GPT-5.4`.

```bash
ocswitch alias add --name gpt-5.4 --display-name "GPT 5.4"
ocswitch alias bind --alias gpt-5.4 --model provider-a/gpt-5.4
ocswitch alias bind --alias gpt-5.4 --model provider-b/GPT-5.4
```

List aliases:

```bash
ocswitch alias list
```

Target order is failover order. Enabled aliases must have at least one routable target.

### 3. Validate statically

```bash
ocswitch doctor
```

`doctor` performs structural checks only. It does not call real upstreams or consume quota. It checks the config file, provider references, alias routability, local proxy listener, and OpenCode sync target.

### 4. Sync to OpenCode

Preview first:

```bash
ocswitch opencode sync --dry-run
```

Write OpenCode config:

```bash
ocswitch opencode sync
```

Also set default model:

```bash
ocswitch opencode sync --set-model ocswitch/gpt-5.4
```

Set default large and small models:

```bash
ocswitch opencode sync \
  --set-model ocswitch/gpt-5.4 \
  --set-small-model ocswitch/gpt-5.4-mini
```

Write to a specific OpenCode config file:

```bash
ocswitch opencode sync --target /path/to/opencode.jsonc
```

Note: if the target file was JSONC, sync writes back normalized JSON, so comments and trailing commas are not preserved. The default sync target is the global user config only; it does not follow `OPENCODE_CONFIG_DIR`.

### 5. Start the local proxy

```bash
ocswitch serve
```

Default proxy base URL:

```text
http://127.0.0.1:9982/v1
```

Default local API key:

```text
ocswitch-local
```

After `ocswitch opencode sync`, OpenCode should show `ocswitch/<alias>`, for example `ocswitch/gpt-5.4`.

### 6. Test the proxy directly

You can test without OpenCode:

```bash
curl -sN -X POST http://127.0.0.1:9982/v1/responses \
  -H "Authorization: Bearer ocswitch-local" \
  -H "Content-Type: application/json" \
  -d '{"model":"gpt-5.4","stream":true,"input":"hello"}'
```

The request body `model` can be the bare alias, such as `gpt-5.4`; `ocswitch/gpt-5.4` is also accepted.

## Mode 2: Server Web Admin

Server mode is for running `ocswitch` on a long-lived machine and managing providers, aliases, proxy state, logs, and network traces from a browser. It reuses the desktop GUI frontend, but omits desktop-only features such as tray, notifications, and launch-at-login.

Start server mode:

```bash
ocswitch server
```

Default admin URL:

```text
http://127.0.0.1:9983
```

Override listener:

```bash
ocswitch server --host 127.0.0.1 --port 9983
```

Server mode also starts the proxy. Default proxy URL remains:

```text
http://127.0.0.1:9982/v1
```

On first start, if `admin.api_key` is missing, `ocswitch server` generates a strong random admin token, stores it as plaintext in local `ocswitch` config, and prints it once:

```text
[ocswitch-server] admin API key generated and saved in config admin.api_key
[ocswitch-server] Authorization: Bearer <token>
```

Paste that token into the browser login page. The frontend stores it only in `sessionStorage` for the current browser tab.

Server-mode notes:

- Admin API `/api/*` uses `Authorization: Bearer <admin.api_key>`.
- Proxy API `/v1/*` uses `server.api_key`; default local value is `ocswitch-local`.
- Admin token and proxy API key are separate credentials.
- Server mode cannot edit the user's local OpenCode config file directly.
- The `Sync` page generates OpenCode config JSON for copy/paste into the user's local OpenCode config.
- Server mode continues using SQLite for request logs and network traces.
- When listening on `0.0.0.0` or another non-loopback host, protect the admin UI with a firewall, trusted network, or HTTPS reverse proxy.

Example Caddy same-domain reverse proxy:

```caddyfile
ocswitch.example.com {
  reverse_proxy /v1/* 127.0.0.1:9982
  reverse_proxy 127.0.0.1:9983
}
```

## Mode 3: Desktop App

The desktop app is for managing providers, aliases, sync, logs, and desktop preferences from a Windows GUI.

Current desktop capabilities:

- Sidebar tabs: `Overview` / `Providers` / `Aliases` / `Log` / `Network` / `Sync` / `Settings`
- UI language preference: `en-US` / `zh-CN` / `system`
- Theme preference: `light` / `dark` / `system`
- `Settings` can edit proxy timeouts, routing strategy, and strategy-specific parameters
- Tray behavior, notifications, and launch-at-login
- Shared frontend with server web admin

Build frontend first:

```bash
cd frontend
npm install
npm run build
```

Then build the desktop app from the repository root:

```bash
wails build -tags desktop_wails
```

Default Windows output path:

```text
build/bin/ocswitch-desktop.exe
```

Development mode:

```bash
wails dev -tags desktop_wails
```

Run an already built executable:

```bash
./build/bin/ocswitch-desktop.exe
```

Note: Windows 11 generally includes WebView2 Runtime, and most mainstream Windows 10 devices already have it installed. If the desktop app fails to start, install Microsoft Edge WebView2 Runtime:

```text
https://developer.microsoft.com/microsoft-edge/webview2/
```

## Shared Concepts

### Provider

Providers are real upstreams. Add or update providers:

```bash
ocswitch provider add --id <id> --base-url <url-with-/v1> --api-key <key>
ocswitch provider add --id <id> --base-url <url-with-/v1> --api-key ""
ocswitch provider add --id <id> --base-url <url-with-/v1> --clear-headers
ocswitch provider add --id <id> --base-url <url-with-/v1> --skip-models
```

To clear a saved upstream API key, pass `--api-key ""`. To clear extra headers, pass `--clear-headers`.

Common provider commands:

```bash
ocswitch provider list
ocswitch provider disable <id>
ocswitch provider enable <id>
ocswitch provider remove <id>
```

Removing a provider does not remove alias references. If references remain, `ocswitch doctor` reports an error.

### Alias

Aliases are stable model names exposed to OpenCode. Common alias commands:

```bash
ocswitch alias add --name <alias>
ocswitch alias bind --alias <alias> --model <provider-id>/<upstream-model>
ocswitch alias bind --alias <alias> --provider <provider-id> --model <upstream-model>
ocswitch alias unbind --alias <alias> --model <provider-id>/<upstream-model>
ocswitch alias unbind --alias <alias> --provider <provider-id> --model <upstream-model>
ocswitch alias list
ocswitch alias remove <alias>
```

Preferred bind form is `--model <provider-id>/<upstream-model>`. The legacy `--provider <id> --model <model>` form remains available as fallback.

### OpenCode sync

`ocswitch opencode sync` updates only `provider.ocswitch` in OpenCode config by default. It does not modify top-level `model` or `small_model` unless `--set-model` or `--set-small-model` is passed.

Default behavior:

- Reuse global OpenCode config in this order: `opencode.jsonc` > `opencode.json` > `config.json`
- Create `~/.config/opencode/opencode.jsonc` if none exists
- Use only the global user config directory; do not follow `OPENCODE_CONFIG_DIR`
- Sync only routable aliases

### Config file

Default local `ocswitch` config path:

- `$OCSWITCH_CONFIG`, if set
- Else `$XDG_CONFIG_HOME/ocswitch/config.json`
- Else `~/.config/ocswitch/config.json`

Explicit per-command config path:

```bash
ocswitch --config /path/to/config.json doctor
```

Command behavior, defaults, write scope, and side effects are defined by each command's `--help` output.

### Failover rules

Failover is conservative: `ocswitch` may switch to the next target only before writing any bytes downstream. Once streaming starts, the upstream is locked. Mid-stream splicing across providers is not supported.

Retryable failover cases:

- Connect failure
- DNS / network error
- Upstream timeout or disconnect before first byte
- Upstream `429`
- Upstream `5xx`

No failover:

- Alias missing, disabled, or without routable targets
- Upstream `400` / `401` / `403` / `404`
- Error after response bytes have already started

The default `circuit-breaker` strategy temporarily skips a provider after consecutive retryable failures, then probes it in half-open mode after cooldown. Failure thresholds, cooldowns, backoff, half-open concurrency, and related parameters can be changed in desktop `Settings` or config `server.routing`.

### Debug headers

Once a concrete upstream attempt is selected and returned, responses include:

- `X-OCSWITCH-Alias`
- `X-OCSWITCH-Provider`
- `X-OCSWITCH-Remote-Model`
- `X-OCSWITCH-Attempt`
- `X-OCSWITCH-Failover-Count`

### Logs and traces

Desktop app and server web admin can inspect business logs and network details, including failover chains, status codes, TTFB, request/response metadata, and token / usage diagnostics. Log field reference: `docs/ocswitch-log-field-reference.md`.

## CLI Reference

This README is a quick-start narrative. Exact behavior is defined by command-local `--help`.

```bash
ocswitch serve
ocswitch server [--host HOST] [--port PORT]
ocswitch doctor
ocswitch provider {add,list,enable,disable,remove,import-opencode}
ocswitch alias {add,list,bind,unbind,remove}
ocswitch opencode sync [--target FILE] [--set-model ALIAS] [--set-small-model ALIAS] [--dry-run]
ocswitch --config PATH <command>
```

## FAQ

### Why does `opencode models` not show `ocswitch/<alias>`?

Check whether `ocswitch opencode sync` has run, the alias is enabled, the alias has at least one routable target, referenced providers are not all disabled, and OpenCode is using the config file that sync wrote. Run `ocswitch doctor` to see the OpenCode config target.

### Why does `ocswitch doctor` report no available target?

Enabled aliases must have at least one routable target. A routable target must be enabled, reference an existing provider, and the provider must not be disabled.

### Why does disabling a provider not edit alias targets?

The same provider can be shared by multiple aliases. `ocswitch provider disable` only makes routing skip that provider; it does not mutate alias target state, so re-enabling the provider does not disturb alias relationships.

### Why do errors remain after removing a provider?

Alias targets still reference the old provider. Unbind them:

```bash
ocswitch alias unbind --alias <alias> --model <provider-id>/<model>
ocswitch alias unbind --alias <alias> --provider <provider-id> --model <model>
```

### What if I forget the server admin token?

The server admin token is stored as plaintext in `admin.api_key` in the `ocswitch` config file. To rotate it, stop `ocswitch server`, edit or delete `admin.api_key`, then restart. If the field is empty, a new strong token is generated and printed.

### How does server mode configure local OpenCode?

Server mode runs on the server and cannot directly edit a user's local OpenCode config. Open the `Sync` page in the web admin, generate config, copy the JSON, and paste it into the local OpenCode config file.

## Security Notes

- Listeners default to `127.0.0.1`.
- Upstream credentials are stored in local `ocswitch` config.
- Server-mode admin token is stored as plaintext in `admin.api_key` so it remains recoverable if forgotten.
- Server-mode `/api/*` requires a Bearer token and sends baseline security headers.
- When listening on a non-loopback host, use a firewall, trusted network, or HTTPS reverse proxy.
- Multi-user accounts and RBAC are not implemented.

Treat the local `ocswitch` config as a sensitive file.

## Scope

Out of scope: latency/price/prompt-type routing, mid-stream splicing across providers, billing stats, full OpenCode config takeover, automatic import from `auth.json`, multi-user auth, and RBAC.
