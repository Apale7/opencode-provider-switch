# opencode-provider-switch (`ocswitch`)

A tiny local proxy for [OpenCode](https://opencode.ai) that gives you **one
stable model alias** routed to **multiple upstream providers** with
**deterministic failover**.

- Expose one custom provider `ocswitch` to OpenCode.
- Configure logical aliases (`ocswitch/gpt-5.4`, etc.).
- Each alias has an ordered list of upstream `provider/model` targets.
- Providers can be disabled without mutating alias target state.
- When the primary upstream returns `5xx`/`429`/connect error *before* any
  stream bytes are flushed, `ocswitch` transparently retries the next target.
- Once a stream has started, the upstream is locked for the rest of that
  request — no mid-stream splicing.

Protocol: OpenAI Responses (`POST /v1/responses`) only. Streaming supported.

## Install

```bash
go build -o ocswitch ./cmd/ocswitch
```

## Desktop GUI

The repository also ships a Wails-based desktop control panel for managing
providers, aliases, sync flows, and desktop preferences on Windows.

Current desktop capabilities:

- Sidebar tabs: `Overview` / `Providers` / `Aliases` / `Log` / `Network` / `Sync` / `Settings`
- UI language preference: `en-US` / `zh-CN` / `system`
- Theme preference: `light` / `dark` / `system`
- Shared frontend between the desktop shell and the browser fallback shell

### Build the desktop executable

Install frontend dependencies and verify the frontend build first:

```bash
cd frontend
npm install
npm run build
```

Then build the desktop app from the repository root:

```bash
wails build -tags desktop_wails
```

On Windows, the default output path is:

```text
build/bin/ocswitch-desktop.exe
```

Note:

- Windows 11 generally includes the WebView2 Runtime, and most mainstream Windows 10 devices already have it installed.
- `ocswitch-desktop.exe` is distributed as a single-file artifact and does not require extra sidecar files in the same directory.
- If the desktop app fails to start on Windows, one common cause is a missing WebView2 Runtime. Install the Microsoft Edge WebView2 Runtime and try again:
  https://developer.microsoft.com/microsoft-edge/webview2/

### Development mode

To run the desktop GUI in local development mode:

```bash
wails dev -tags desktop_wails
```

### Usage

After launching the desktop app, you can:

- inspect proxy state, config path, and doctor summary
- manage providers with search, filtering, editing, and OpenCode import
- manage aliases and target bindings
- inspect business logs and network traces, including failover chains, status codes, TTFB, and request/response metadata
- inspect token and usage diagnostics, including input/output, reasoning, cache, usage source/precision, and estimated cost
- preview and apply `ocswitch opencode sync`
- save desktop preferences including launch-at-login, tray behavior,
  notifications, theme, and language

Log field reference: `docs/ocswitch-log-field-reference.md`

If you already built the executable, you can run it directly:

```bash
./build/bin/ocswitch-desktop.exe
```

## Quick start

```bash
# 1. add upstream providers (models are discovered from /v1/models by default; warnings do not block saving)
ocswitch provider add --id su8   --base-url https://cn2.su8.codes/v1 --api-key sk-...
ocswitch provider add --id codex --base-url https://api-vip.codex-for.me/v1 --api-key sk-...

# 2. create alias and bind targets in priority order (preferred Provider/Model form)
ocswitch alias add --name gpt-5.4
ocswitch alias bind --alias gpt-5.4 --model su8/gpt-5.4
ocswitch alias bind --alias gpt-5.4 --model codex/GPT-5.4

# 3. push alias exposure into OpenCode global config
ocswitch opencode sync

# optional: temporarily disable one provider without editing alias targets
ocswitch provider disable su8

# 4. run the proxy
ocswitch serve
```
Inside OpenCode you can now pick `ocswitch/gpt-5.4`.

### Import providers from an existing OpenCode config

```bash
ocswitch provider import-opencode             # reads global OpenCode config
ocswitch provider import-opencode --from ./examples/opencode.jsonc
```

The default import/sync target is the global user config only. It does not
follow `OPENCODE_CONFIG_DIR`; use `--from` or `--target` when you want a
different file.

Only `@ai-sdk/openai` custom providers with a `baseURL` are imported. An empty
`apiKey` is allowed and kept as-is so you can complete credentials later.
Imported provider model lists are preserved when the source config already
declares `models`, and `provider add` will otherwise refresh them from
`/v1/models` by default. Imported lists are kept for migration context, while
hard typo validation only uses catalogs actively discovered from `/v1/models`.
If connection details change but discovery is skipped, fails, or returns an
empty list, any old catalog is retained only as untrusted metadata and no longer
used for strict validation.

### Validate before serving

```bash
ocswitch doctor
```

Runs structural checks only — never issues real upstream requests.

Enabled aliases must have at least one **routable** target. A target is
considered routable only when:

- the alias itself is enabled
- the target itself is enabled
- the referenced provider exists
- the referenced provider is not disabled

`ocswitch opencode sync` and `/v1/models` use the same routable-alias view, so
OpenCode does not see aliases that the proxy would immediately reject.

### Provider state

```bash
ocswitch provider disable <id>
ocswitch provider enable <id>
```

Disabling a provider only removes it from routing/failover consideration. It
does **not** rewrite alias target `enabled` flags in config, which avoids odd
interactions when the same provider is shared across multiple aliases.

## CLI reference

For exact command behavior, defaults, write scope, and side effects, prefer the
matching `--help` page. This README is the quick-start narrative, while CLI help
is the authoritative local execution contract.

- `ocswitch serve` — run the proxy
- `ocswitch doctor` — validate config
- `ocswitch provider {add,list,enable,disable,remove,import-opencode}`
- `ocswitch alias {add,list,bind,unbind,remove}`
- `ocswitch opencode sync [--target FILE] [--set-model ALIAS] [--set-small-model ALIAS] [--dry-run]`

Preferred bind form: `ocswitch alias bind --alias <alias> --model <provider>/<model>` when `--provider` is omitted.
The legacy `--provider <id> --model <model>` form is still accepted as a fallback, including models whose names already contain `/`.
The same combined form also works for `alias unbind`.

To clear a previously saved upstream API key, pass `--api-key ""` explicitly on `provider add`.
To clear previously saved extra provider headers, pass `--clear-headers` on `provider add`.

Global flag: `--config PATH` (default `$OCSWITCH_CONFIG`, else `$XDG_CONFIG_HOME/ocswitch/config.json`, else `~/.config/ocswitch/config.json`).

## Debug headers

Responses include these debug headers once a concrete upstream attempt is being returned to the client:

- `X-OCSWITCH-Alias`
- `X-OCSWITCH-Provider`
- `X-OCSWITCH-Remote-Model`
- `X-OCSWITCH-Attempt`
- `X-OCSWITCH-Failover-Count`

## Scope

Out of MVP: Anthropic native, multi-protocol routing, dashboard, billing,
latency-based routing, and full OpenCode config takeover.
See `.trellis/tasks/archive/2026-04/04-17-04-17-ops-mvp-design-review/prd.md`
for the authoritative design notes.
