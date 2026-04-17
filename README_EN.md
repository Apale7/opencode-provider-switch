# opencode-provider-switch (`olpx`)

A tiny local proxy for [OpenCode](https://opencode.ai) that gives you **one
stable model alias** routed to **multiple upstream providers** with
**deterministic failover**.

- Expose one custom provider `olpx` to OpenCode.
- Configure logical aliases (`olpx/gpt-5.4`, etc.).
- Each alias has an ordered list of upstream `provider/model` targets.
- Providers can be disabled without mutating alias target state.
- When the primary upstream returns `5xx`/`429`/connect error *before* any
  stream bytes are flushed, `olpx` transparently retries the next target.
- Once a stream has started, the upstream is locked for the rest of that
  request — no mid-stream splicing.

Protocol: OpenAI Responses (`POST /v1/responses`) only. Streaming supported.

## Install

```bash
go build -o olpx ./cmd/olpx
```

## Quick start

```bash
# 1. add upstream providers
olpx provider add --id su8   --base-url https://cn2.su8.codes/v1 --api-key sk-...
olpx provider add --id codex --base-url https://api-vip.codex-for.me/v1 --api-key sk-...

# 2. create alias and bind targets in priority order
olpx alias add --name gpt-5.4
olpx alias bind --alias gpt-5.4 --provider su8   --model gpt-5.4
olpx alias bind --alias gpt-5.4 --provider codex --model GPT-5.4

# 3. push alias exposure into OpenCode global config
olpx opencode sync

# optional: temporarily disable one provider without editing alias targets
olpx provider disable su8

# 4. run the proxy
olpx serve
```

Inside OpenCode you can now pick `olpx/gpt-5.4`.

### Import providers from an existing OpenCode config

```bash
olpx provider import-opencode             # reads global OpenCode config
olpx provider import-opencode --from ./examples/opencode.jsonc
```

The default import/sync target is the global user config only. It does not
follow `OPENCODE_CONFIG_DIR`; use `--from` or `--target` when you want a
different file.

Only `@ai-sdk/openai` custom providers with a `baseURL` and `apiKey` are
imported. Everything else is out of MVP scope.

### Doctor (static)

```bash
olpx doctor
```

Runs structural checks only — never issues real upstream requests.

Enabled aliases must have at least one **routable** target. A target is
considered routable only when:

- the alias itself is enabled
- the target itself is enabled
- the referenced provider exists
- the referenced provider is not disabled

`olpx opencode sync` and `/v1/models` use the same routable-alias view, so
OpenCode does not see aliases that the proxy would immediately reject.

### Provider state

```bash
olpx provider disable <id>
olpx provider enable <id>
```

Disabling a provider only removes it from routing/failover consideration. It
does **not** rewrite alias target `enabled` flags in config, which avoids odd
interactions when the same provider is shared across multiple aliases.

## CLI reference

- `olpx serve` — run the proxy
- `olpx doctor` — validate config
- `olpx provider {add,list,enable,disable,remove,import-opencode}`
- `olpx alias {add,list,bind,unbind,remove}`
- `olpx opencode sync [--target FILE] [--set-model ALIAS] [--set-small-model ALIAS] [--dry-run]`

Global flag: `--config PATH` (default `$XDG_CONFIG_HOME/olpx/config.json`).

## Debug headers

Every proxied response includes:

- `X-OLPX-Alias`
- `X-OLPX-Provider`
- `X-OLPX-Remote-Model`
- `X-OLPX-Attempt`
- `X-OLPX-Failover-Count`

## Scope

Out of MVP: Anthropic native, multi-protocol routing, dashboard, billing,
latency-based routing, full `/v1/models` provider discovery, full OpenCode
config takeover.
See `.trellis/tasks/archive/2026-04/04-17-04-17-ops-mvp-design-review/prd.md`
for the authoritative design notes.
