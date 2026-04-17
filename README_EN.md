# opencode-provider-switch (`ops`)

A tiny local proxy for [OpenCode](https://opencode.ai) that gives you **one
stable model alias** routed to **multiple upstream providers** with
**deterministic failover**.

- Expose one custom provider `ops` to OpenCode.
- Configure logical aliases (`ops/gpt-5.4`, etc.).
- Each alias has an ordered list of upstream `provider/model` targets.
- When the primary upstream returns `5xx`/`429`/connect error *before* any
  stream bytes are flushed, `ops` transparently retries the next target.
- Once a stream has started, the upstream is locked for the rest of that
  request — no mid-stream splicing.

Protocol: OpenAI Responses (`POST /v1/responses`) only. Streaming supported.

## Install

```bash
go build -o ops ./cmd/ops
```

## Quick start

```bash
# 1. add upstream providers
ops provider add --id su8   --base-url https://cn2.su8.codes/v1 --api-key sk-...
ops provider add --id codex --base-url https://api-vip.codex-for.me/v1 --api-key sk-...

# 2. create alias and bind targets in priority order
ops alias add --name gpt-5.4
ops alias bind --alias gpt-5.4 --provider su8   --model gpt-5.4
ops alias bind --alias gpt-5.4 --provider codex --model GPT-5.4

# 3. push alias exposure into OpenCode global config
ops opencode sync

# 4. run the proxy
ops serve
```

Inside OpenCode you can now pick `ops/gpt-5.4`.

### Import providers from an existing OpenCode config

```bash
ops provider import-opencode             # reads global OpenCode config
ops provider import-opencode --from ./examples/opencode.jsonc
```

The default import/sync target is the global user config only. It does not
follow `OPENCODE_CONFIG_DIR`; use `--from` or `--target` when you want a
different file.

Only `@ai-sdk/openai` custom providers with a `baseURL` and `apiKey` are
imported. Everything else is out of MVP scope.

### Doctor (static)

```bash
ops doctor
```

Runs structural checks only — never issues real upstream requests.

## CLI reference

- `ops serve` — run the proxy
- `ops doctor` — validate config
- `ops provider {add,list,remove,import-opencode}`
- `ops alias {add,list,bind,unbind,remove}`
- `ops opencode sync [--target FILE] [--set-model ALIAS] [--set-small-model ALIAS] [--dry-run]`

Global flag: `--config PATH` (default `$XDG_CONFIG_HOME/ops/config.json`).

## Debug headers

Every proxied response includes:

- `X-OPS-Alias`
- `X-OPS-Provider`
- `X-OPS-Remote-Model`
- `X-OPS-Attempt`
- `X-OPS-Failover-Count`

## Scope

Out of MVP: Anthropic native, multi-protocol routing, dashboard, billing,
latency-based routing, `/v1/models` discovery, full OpenCode config takeover.
See `.trellis/tasks/archive/2026-04/04-17-04-17-ops-mvp-design-review/prd.md`
for the authoritative design notes.
