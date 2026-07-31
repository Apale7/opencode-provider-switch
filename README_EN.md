# opencode-provider-switch (`ocswitch`)

`ocswitch` is a local AI provider switcher originally designed for OpenCode auto-configuration. OpenCode sees one stable model name, for example `ocswitch/gpt-5.4`; `ocswitch` routes that alias to one or more upstream `provider/model` targets and fails over to the next target when the first attempt fails before first byte.

Under the hood it is a local compatible-protocol proxy, so it is not limited to OpenCode. Any client that can manually configure an OpenAI / Anthropic compatible base URL, API key, and model name can talk to the `ocswitch` proxy directly. The difference is that non-OpenCode clients are not configured automatically; you must enter the proxy URL, proxy API key, and alias model name yourself.

Supported protocols: OpenAI Responses, Anthropic Messages, and OpenAI-compatible Chat Completions. Streaming, request logs, network traces, upstream API-key rotation, and configurable routing strategies are supported. `ocswitch` also supports Provider Groups: one Provider can contain multiple business groups, and each group owns its protocol, model catalog, upstream API keys, and enabled state. Legacy single-layer provider configs are migrated automatically into the `default` group. The default routing strategy is `circuit-breaker`.

## Compatibility: OpenCode and Other Clients

| Client type | Supported | Configuration | Notes |
| --- | --- | --- | --- |
| OpenCode | Yes | `ocswitch opencode sync`, or generated config from the desktop/server `Sync` page | Can write or generate OpenCode config automatically. Model names are usually `ocswitch/<alias>`. |
| Other OpenAI Responses clients | Yes | Manually set base URL to `http://127.0.0.1:9982/v1`, API key to `server.api_key`, and call `/v1/responses` | Request `model` should be an alias, for example `gpt-5.4` or `ocswitch/gpt-5.4`. |
| Other OpenAI Chat Completions clients | Yes | Manually use the same base URL and API key, then call `/v1/chat/completions` | Configure the provider/alias protocol as `openai-compatible`. |
| Other Anthropic Messages clients | Yes | Manually use the same proxy address and API key, then call `/v1/messages` | Configure the provider/alias protocol as `anthropic-messages`. |

Automatic sync is OpenCode-only. For non-OpenCode clients, treat `ocswitch` as a normal local proxy service.

## Three Usage Modes

`ocswitch` has three main usage modes. Command names are intentionally different: `ocswitch serve` starts only the local proxy; `ocswitch server` starts the server web admin and the proxy.

| Mode | Entry point | Best for |
| --- | --- | --- |
| TUI / CLI only | Bare `ocswitch` interactive TUI, or `ocswitch provider` / `ocswitch alias` / `ocswitch opencode sync` / `ocswitch serve` | Terminal-first interactive management, or fully command/script-driven setup |
| Server web admin | `ocswitch server` | Long-running host managed from a browser |
| Desktop app | `ocswitch-desktop.exe` | Windows GUI, tray, notifications, launch-at-login |

Running bare `ocswitch` in an interactive terminal opens the TUI. In scripts, pipes, or other non-interactive environments, the bare command prints short help instead of entering a full-screen UI, so automation does not hang.

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

## Mode 1: TUI / CLI Only

TUI / CLI-only mode is for users who prefer terminal interaction, scripts, or headless environments. In an interactive terminal, run bare `ocswitch` to open the TUI; you can also use explicit CLI subcommands. It opens no web UI and provides no desktop tray. You manage providers, groups, aliases, and optional OpenCode config with the TUI or commands, then run `ocswitch serve` to start the local proxy. Non-OpenCode clients can skip `opencode sync` and connect to the proxy manually.

Using an agent for this mode is recommended. The CLI flow has several steps and it is easy to miss `doctor`, `opencode sync`, or default-model switching. Give the agent your provider/group list, target aliases, and OpenCode config target; ask it to inspect `ocswitch --help`, generate commands, run dry-run first, then execute. Do not paste real API keys into public chats; local agents can use environment variables, private files, or interactive input for secrets.

Example agent prompt:

```text
Help me configure ocswitch in TUI / CLI-only mode.
Providers/Groups: provider id/baseURL plus group id/protocol/model list below; read API keys from env vars.
Alias: gpt-5.4 should try provider-a/default/model-a, then provider-b/premium/model-b.
Run dry-run first, sync to this OpenCode config file, run doctor, then tell me which model name to select.
```

### 1. Add or import providers

Add providers manually. `--base-url` is the upstream API root; the path varies by provider (often ends with `/v1`, but not always). By default, `ocswitch` tries to discover models from upstream `/models`; add `--skip-models` if the upstream does not expose that endpoint.

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

### 2. Confirm or customize aliases

When provider model discovery succeeds, `ocswitch` creates a same-name automatic alias for each model by default. If multiple providers expose the same model, they are merged into ordered targets using the priority configured on the Providers page. Normal setups therefore only need providers; manual alias binding is optional.

Create a manual alias when you need a different public model name, fixed targets, or precise failover ordering. This example means: when OpenCode uses `ocswitch/gpt-5.4`, first try `provider-a/gpt-5.4`; if it fails before first byte, try `provider-b/GPT-5.4`.

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

Model capability metadata note: `ocswitch opencode sync` only exposes routable aliases as `provider.ocswitch.models.*`. It does not automatically write or infer model capabilities such as `attachment`, `modalities`, `tool_call`, `reasoning`, or `limit`. Relay `/models` responses are often not reliable enough to decide image, PDF, tool-call, reasoning, or other capabilities. OpenCode built-in providers such as `provider.openai` may fill capabilities for known models, but `provider.ocswitch` is a custom provider and does not automatically inherit those built-in capabilities. If OpenCode must explicitly recognize image/attachment support, add the metadata manually under `provider.ocswitch.models.<alias>` in your OpenCode config; future syncs preserve existing same-name model objects.

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

You can test without OpenCode. This is also useful for checking connectivity from other clients:

```bash
curl -sN -X POST http://127.0.0.1:9982/v1/responses \
  -H "Authorization: Bearer ocswitch-local" \
  -H "Content-Type: application/json" \
  -d '{"model":"gpt-5.4","stream":true,"input":"hello"}'
```

The request body `model` can be the bare alias, such as `gpt-5.4`; `ocswitch/gpt-5.4` is also accepted.

For other clients, manually configure:

- Base URL: `http://127.0.0.1:9982/v1`
- API key: `server.api_key`; the default local value is `ocswitch-local`
- Model: an alias such as `gpt-5.4`; if the client expects a provider prefix, `ocswitch/gpt-5.4` also works

If the client only supports Chat Completions, configure the provider/alias protocol as `openai-compatible`. If the client uses Anthropic Messages, configure it as `anthropic-messages`.

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

- Sidebar tabs: `Overview` / `Providers` / `Aliases` / `Log` / `Network` / `Health` / `Sync` / `Settings`
- Provider Groups management: maintain multiple groups under one Provider, with group-scoped protocols, model catalogs, and multiple upstream API keys
- Health page: aggregate provider / group / model health metrics such as success rate, failure categories, cache hit rate, token share, and output speed
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
wails build -tags desktop_wails -ldflags "-X main.version=v0.0.0"
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

To clear a saved upstream API key, pass `--api-key ""`. These `provider add --api-key` flags are compatibility entry points that target the `default` group; for multi-group setups, prefer the `provider group` commands below. To clear extra headers, pass `--clear-headers`.

In the current config shape, Provider stores shared connection settings such as `base_url`, optional `base_urls`, `base_url_strategy`, extra `headers`, provider enabled state, and auto-alias preference. Protocol, model catalog, upstream API keys, and group enabled state live under Provider Groups. Legacy provider-level `protocol` / `api_key` / `api_keys` / `models` / `models_source` fields are migrated into the `default` group when the config is read, preserving old configs.

The desktop app, server web admin, and CLI can store multiple upstream API keys for each Provider Group. In the config file, the first key lives in that group's `api_key`, and additional keys live in `api_keys`. The proxy rotates the starting key across requests and, before first byte, may continue with another key from the same group after a retryable failure. This is for upstream quota spreading or temporary single-key failures; it does not change the local downstream `server.api_key` used by clients.

```json
{
  "providers": [
    {
      "id": "provider-a",
      "base_url": "https://provider-a.example/v1",
      "base_urls": ["https://provider-a.example/v1", "https://provider-a-backup.example/v1"],
      "base_url_strategy": "ordered",
      "groups": [
        {
          "id": "default",
          "name": "Default",
          "protocol": "openai-responses",
          "api_key": "sk-default-first",
          "api_keys": ["sk-default-second"],
          "models": ["gpt-5.4"]
        },
        {
          "id": "premium",
          "name": "Premium pool",
          "protocol": "anthropic-messages",
          "api_key": "sk-premium",
          "models": ["claude-sonnet-4"]
        }
      ]
    }
  ]
}
```

Common provider commands:

```bash
ocswitch provider list
ocswitch provider disable <id>
ocswitch provider enable <id>
ocswitch provider remove <id>
```

Removing a provider cleans its automatic targets from unlocked automatic aliases and deletes aliases that become empty. Manual aliases and aliases upgraded to manual are not rewritten automatically; if they still reference the removed provider, `ocswitch doctor` reports an error.

### Provider Groups

A Provider Group is a business group under one Provider. Typical uses include splitting one upstream domain or relay service by plan, protocol, model catalog, or API-key pool. Provider-level `base_url` / `base_urls` / `headers` are shared by all groups under that Provider; group-level `protocol` / `api_key` / `api_keys` / `models` affect only that group.

Common group commands:

```bash
ocswitch provider group list --provider provider-a
ocswitch provider group create --provider provider-a --id premium --protocol openai-responses --api-key sk-premium
ocswitch provider group update --provider provider-a --group premium --name "Premium pool" --api-keys sk-a --api-keys sk-b
ocswitch provider group refresh-models --provider provider-a --group premium
ocswitch provider group ping --provider provider-a --group premium
ocswitch provider group delete --provider provider-a --group premium --dry-run
```

Group identity is explicit: create, update, delete, model refresh, and ping all require concrete `--provider` and `--group` values. `default` is the compatibility group used for migrated legacy configs and compatibility commands; non-default groups are never silently replaced by same-protocol siblings.

Alias targets are now precise `provider/group/model` tuples. The default group still supports the older shorthand:

```bash
ocswitch alias bind --alias gpt-5.4 --model provider-a/gpt-5.4
ocswitch alias bind --alias gpt-5.4 --provider provider-a --group premium --model gpt-5.4
```

Request rewrite rules can also target exact provider/group pairs:

```bash
ocswitch rewrite add --name premium-tier --alias gpt-5.4 \
  --provider-group provider-a/premium \
  --op 'set:$.service_tier="priority"'
```

When deleting a group or changing a group id, the CLI, TUI, desktop app, and server web admin preview alias / rewrite reference impact first. You can choose to remove targets, delete aliases, rebind targets, keep/disable/delete rewrite rules, or replace rewrite provider-group selectors.

### Automatic aliases and zero-config routing

After a provider save or model refresh successfully discovers models, `ocswitch` creates same-name aliases for those models. When multiple enabled providers declare the same model, the automatic alias contains multiple targets ordered by the drag-and-drop priority on the Providers page.

Requests are resolved in this order:

1. Same-name manual alias
2. Same-name automatic alias
3. Direct match against discovered models of enabled providers

This means the proxy can route a model even when no alias exists, provided that an enabled provider lists that model. Protocols must match; for example, an `openai-compatible` provider cannot handle an `openai-responses` request.

The desktop app and server web admin expose these controls:

- Drag handle on the Providers page: changes provider priority for both automatic alias targets and direct fallback.
- “Auto-generate aliases” in the provider form: when disabled, future saves or refreshes for that provider do not create or append automatic aliases. Existing targets are not removed by toggling it off.
- Global “Auto-generate aliases” in Settings: stops automatic generation and skips automatic aliases during routing. Direct provider fallback is still attempted when no manual alias matches.
- “Upgrade to Manual” on the Aliases page: converts the automatic alias and its current targets into a manual alias. Later model refreshes, priority changes, and automatic provider cleanup no longer rewrite it.

Automatic aliases are created only when the group's `models_source` is `discovered` and the model list is non-empty. `--skip-models`, an upstream without a model catalog, or failed discovery does not create aliases; configure them manually in those cases.

Configuration example:

```json
{
  "auto_alias_enabled": true,
  "provider_priority": ["provider-a", "provider-b"],
  "providers": [
    {
      "id": "provider-a",
      "base_url": "https://provider-a.example/v1",
      "auto_alias_enabled": true,
      "groups": [
        {
          "id": "default",
          "protocol": "openai-responses",
          "api_key": "sk-example",
          "models": ["gpt-5.4"],
          "models_source": "discovered"
        }
      ]
    }
  ]
}
```

For legacy configs without these fields, global and per-provider automatic aliases default to enabled. System-managed fields such as `auto_generated`, `locked`, and automatic target markers should normally be managed through the UI rather than edited manually.

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
- The SSE pre-commit buffer sees only metadata / ping / fake-start frames before any downstream bytes are written
- Upstream `5xx`
- Upstream status codes listed in `server.failover_status_codes`; defaults are `401` / `402` / `403` / `429`

No failover:

- Alias missing, disabled, or without routable targets
- Upstream `4xx` not configured as retryable, such as default `400` / `404`
- Error after response bytes have already started

Streaming has two related settings:

- `server.stream_idle_timeout_ms` defaults to `60000`. SSE streams also obey this idle timeout. If the upstream stalls after bytes have already been written downstream, `ocswitch` writes a protocol-compatible SSE error event, flushes, and closes the stream. It does not switch providers and does not append a fake `[DONE]`.
- `server.stream_precommit_buffer_ms` defaults to `0`. With `0`, 2xx SSE responses are committed immediately. When set to a positive value, `ocswitch` briefly buffers raw SSE frames before committing so metadata / ping / fake-start-only streams can fail over to the next target before the client sees bytes. Positive values increase first-byte / commit latency; use `3000`-`5000` ms for coding continuity, or `8000`-`10000` ms for slow or high-latency providers.

The default `circuit-breaker` strategy temporarily skips a provider after consecutive retryable failures, then probes it in half-open mode after cooldown. Failure thresholds, cooldowns, backoff, half-open concurrency, and related parameters can be changed in desktop `Settings` or config `server.routing`. Extra HTTP status codes that trigger failover can be changed in `Settings` or `server.failover_status_codes`; clearing the list keeps only `5xx` failover.

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
ocswitch rewrite {add,list,enable,disable,remove}
ocswitch opencode sync [--target FILE] [--set-model ALIAS] [--set-small-model ALIAS] [--dry-run]
ocswitch --config PATH <command>
```

### Request Config Rewrites

`rewrite` rules live in local `ocswitch` config under `request_rewrite_rules`. They use `ops` to add or rewrite JSONPath targets on the outbound request sent to the upstream provider. Rules run in config order and must match the incoming alias; they may also be limited to one or more providers from that alias's targets. Use the bare alias name, for example `gpt-5.5-fast`, not `ocswitch/gpt-5.5-fast`. Empty `providers` means every provider under that alias matches.

Recommended flow: capture a real request first, then configure rules from the request JSON shown in `Network`. Start the proxy, send a request, open the desktop or server Web `Network` page, select that request, and inspect `Client request params` in the `Network inspector`. JSONPath supports an RFC 9535 singular subset: root `$`, dot names, bracket quoted names, and non-negative array indexes. Wildcard, filter, slice, union, recursive descent, and root-only mutation targets are not supported.

By default `override=false`: `set` only fills missing object members, caller-supplied values win, and missing intermediate objects can be created. With `--override`, `set` may replace existing values and `delete`, `append`, and `insert` are allowed. `append`/`insert`/`delete` do not create missing paths; missing paths are no-ops.

Legacy top-level `set` / `delete` config no longer runs. `doctor`, Web, and `rewrite list` warn about migration; CLI `--set` / `--delete` remain only as legacy prompts, so use `--op` for active rules.

```bash
ocswitch rewrite add --name gpt-fast --alias gpt-5.5-fast --op 'set:$.service_tier="priority"' --op 'set:$.store=false' --op 'set:$.reasoning.effort="medium"'
ocswitch rewrite add --name no-store --alias gpt-5.5 --provider provider-a --provider provider-b --override --op 'delete:$.store' --op 'append:$.include="reasoning.encrypted_content"'
ocswitch rewrite add --name tool-first --alias gpt-5.5 --override --op 'insert:$.tools:0={"type":"web_search"}'
ocswitch rewrite disable gpt-fast
ocswitch rewrite list
```

Equivalent config snippet:

```json
{
  "request_rewrite_rules": [
    {
      "name": "gpt-fast",
      "alias": "gpt-5.5-fast",
      "enabled": true,
      "ops": [
        { "op": "set", "path": "$.service_tier", "value": "priority" },
        { "op": "set", "path": "$.store", "value": false },
        { "op": "set", "path": "$.reasoning.effort", "value": "medium" }
      ]
    },
    {
      "name": "no-store",
      "alias": "gpt-5.5",
      "providers": ["provider-a", "provider-b"],
      "enabled": true,
      "override": true,
      "ops": [
        { "op": "delete", "path": "$.store" },
        { "op": "append", "path": "$.include", "value": "reasoning.encrypted_content" },
        { "op": "insert", "path": "$.tools", "index": 0, "value": { "type": "web_search" } }
      ]
    }
  ]
}
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
