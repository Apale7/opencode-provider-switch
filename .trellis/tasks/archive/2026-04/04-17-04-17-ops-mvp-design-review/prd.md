# OCSWITCH MVP Redesign

## Summary

`opencode-provider-switch` (`ocswitch`) is a local proxy for OpenCode focused on one narrow job:

- expose one stable local provider to OpenCode
- let users select logical model aliases instead of concrete upstream models
- route one alias to multiple upstream providers/models in fixed priority order
- automatically fail over when upstream providers are temporarily unavailable

This redesign intentionally removes most of the previous PRD scope. MVP is no longer about full OpenCode config takeover, protocol pools, provider discovery, or broad migration automation.

MVP is about one thing: **multi-provider failover behind a stable OpenCode model alias**.

## Product Goal

When a user chooses `ocswitch/<alias>` inside OpenCode, `ocswitch` should transparently try the configured upstream targets in priority order until one succeeds, without the user needing to care which provider actually served the request.

## Core User Need

User already has, or can define, multiple OpenCode-compatible upstream providers.

User wants:

1. one stable model name inside OpenCode
2. multiple upstream fallbacks behind that name
3. deterministic failover behavior
4. local proxy integration without switching away from OpenCode workflow

## Confirmed MVP Decisions

- Protocol support: `openai-responses` only
- Responses streaming: required in first shippable MVP
- Local proxy shape: one OpenAI Responses-compatible local provider
- OpenCode integration target: custom provider using `@ai-sdk/openai`
- Alias support: required
- Failover priority order: required and explicit
- `auth.json` is not a provider-definition source for MVP import
- Upstream model discovery from provider `/models`: out of MVP
- Full OpenCode config install/restore takeover: out of MVP
- Billing/cost accounting: out of MVP
- Multi-protocol routing: out of MVP
- Anthropic native support: out of MVP
- Dashboard/web UI: out of MVP

## Non-Goals

1. No attempt to become a general AI gateway.
2. No automatic routing by latency, prompt type, or cost.
3. No broad migration of every OpenCode provider shape.
4. No provider capability normalization across vendors.
5. No background health scoring system.
6. No project-wide interception guarantees across every OpenCode config layer.
7. No mid-stream failover or stream splicing across upstream providers.

## Architecture in One Sentence

OpenCode sends `POST /v1/responses` to local provider `ocswitch`; `ocswitch` resolves requested alias to an ordered target list and proxies request to first healthy upstream candidate.

## High-Level Architecture

```text
OpenCode
  -> custom provider `ocswitch` (@ai-sdk/openai)
  -> http://127.0.0.1:9982/v1/responses
  -> alias resolver
  -> failover engine
  -> upstream provider/model #1
  -> upstream provider/model #2
  -> upstream provider/model #3
```

## Integration Strategy With OpenCode

### Local Provider Shape

MVP should expose exactly one local provider to OpenCode:

- provider id: `ocswitch`
- npm package: `@ai-sdk/openai`
- base URL: `http://127.0.0.1:9982/v1`
- local API key: static placeholder such as `ocswitch-local`

Conceptual OpenCode config shape:

```jsonc
{
  "$schema": "https://opencode.ai/config.json",
  "provider": {
    "ocswitch": {
      "npm": "@ai-sdk/openai",
      "name": "OPS",
      "options": {
        "baseURL": "http://127.0.0.1:9982/v1",
        "apiKey": "ocswitch-local"
      },
      "models": {
        "gpt-5.4": {
          "name": "gpt-5.4"
        },
        "gpt-5.4-mini": {
          "name": "gpt-5.4-mini"
        }
      }
    }
  },
  "model": "ocswitch/gpt-5.4"
}
```

### Why This Is Enough For `/models`

OpenCode source confirms this path is valid.

`opencode models` calls `Provider.list()` and prints entries from each resolved `provider.models` map.

OpenCode Web/TUI model pickers also read runtime provider state and only surface models from connected providers.

OpenCode provider loading also merges custom config-defined providers and models into runtime provider state. That means `ocswitch` does **not** need a special external model catalog protocol for MVP.

Simplest MVP path:

- keep alias list in `ocswitch`
- sync alias list into OpenCode `provider.ocswitch.models`
- make sure `provider.ocswitch` is valid enough to appear as a connected runtime provider
- let OpenCode surface `ocswitch/<alias>` in `/models` and `/model`

### Important Scope Rule

MVP should **not** rewrite the entire OpenCode config.

Instead, `ocswitch` should only support a narrow integration step:

- ensure `provider.ocswitch` exists or is updated
- optionally sync alias entries into `provider.ocswitch.models`
- optionally let user set `model` or `small_model` manually

This avoids the previous PRD's high-risk install/restore workflow.

### Config Precedence Caveat

OpenCode source also confirms config is merged from multiple layers, and lower-precedence files can be overridden by project-local config.

Implication for MVP:

- `ocswitch opencode sync` must know which config file it is targeting
- MVP should not silently write one low-precedence file and assume aliases will appear at runtime
- default target should be global user config under `~/.config/opencode/`
- if global config already exists, reuse existing main file in this order: `opencode.jsonc`, `opencode.json`, `config.json`
- if no global config file exists, create `~/.config/opencode/opencode.jsonc`
- project `opencode.json`, `.opencode/`, `OPENCODE_CONFIG_DIR`, managed config, and account config are out of default sync scope unless user explicitly targets them

## Provider Source Model

`ocswitch` needs upstream provider definitions, but this is no longer the product center.

MVP should support two input paths:

1. manual provider entry through `ocswitch` CLI
2. one-shot import from one explicit OpenCode config file, defaulting to global user config

### Supported Provider Shape In MVP

MVP should only promise import for OpenCode custom providers that already declare:

- `npm: @ai-sdk/openai`
- `options.baseURL`
- `options.apiKey` or equivalent proxyable auth/header settings

This keeps import logic narrow and predictable.

Explicit MVP boundary:

- import support is limited to config-defined `@ai-sdk/openai` custom providers
- `@ai-sdk/openai-compatible` provider import is out of MVP even if source config contains it

OpenCode source also shows that `auth.json` provides credentials for an existing provider ID, but does not by itself define a brand-new custom provider shape. A custom provider still needs config describing its `npm`, endpoint, and models.

Implication for MVP:

- `ocswitch` should import provider definitions from config, not from merged runtime auth state
- `auth.json` support, if ever added later, should be treated as credential enrichment for already-known provider IDs
- MVP import does not need to evaluate account config, managed config, or remote well-known config layers

### Explicit Non-Goal For MVP

MVP should not promise full support for every OpenCode credential source such as complex `/connect`-managed auth flows or provider-specific auth conventions.

Those can be added later if needed.

## Alias Design

Alias is the primary user-facing abstraction.

User should use alias directly inside OpenCode. User should not need to know concrete upstream model IDs after alias is configured.

### Alias Rules

1. Every alias maps to one or more concrete targets.
2. Every alias must contain at least one enabled target.
3. Alias target order is explicit and defines failover priority.
4. Alias name must be unique within `ocswitch`.
5. OpenCode should reference alias as `ocswitch/<alias>`.

### Example

```text
alias: gpt-5.4
  1. provider: su8        model: gpt-5.4
  2. provider: codexfm    model: GPT-5.4
  3. provider: relay-x    model: gpt-5.4-2026-04-01
```

### Minimal Alias Metadata

MVP alias record should contain:

- `alias`
- `display_name` optional
- ordered target list
- `enabled`

Rich capability metadata such as `limit`, `attachment`, `reasoning`, `tool_call`, and `variants` should be treated as optional passthrough data, not required for MVP routing correctness.

## Proxy Behavior

### Supported Incoming Route

- `POST /v1/responses`

### Request Flow

1. Receive request from OpenCode.
2. Read and buffer full JSON request body once.
3. Parse `model` from that JSON body.
4. Treat `model` as `ocswitch` alias.
5. Resolve alias to ordered enabled targets.
6. Replace only alias model field with concrete upstream model ID.
7. Forward request to highest-priority target.
8. If failure is retryable and no response has started to client yet, replay same JSON body to next target.
9. Return first successful upstream response.

### Request Shape Constraint

OpenCode source confirms real `@ai-sdk/openai` traffic uses OpenAI Responses API JSON payloads, typically with `stream: true`.

Implication for MVP:

- proxy only needs to support JSON `POST /v1/responses` requests emitted by OpenCode
- image/file inputs that OpenCode already serializes into JSON data URLs remain in scope
- multipart upload compatibility is out of MVP

### Streaming Rule

Responses streaming is part of normal OpenCode usage, not an optional edge path.

OpenCode source shows `@ai-sdk/openai` requests go to `/v1/responses` with `stream: true` by default.

MVP proxy must therefore support streaming pass-through from first release.

Failover rule for streaming:

- failover allowed only before any response headers or body bytes are written to OpenCode
- receiving an upstream response is not commitment by itself if proxy has not flushed anything downstream yet
- once any headers or body bytes are written to client, current provider is locked for that request
- if locked stream later fails, proxy returns failure from current upstream and does not switch mid-stream

### Why Mid-Stream Failover Is Out

OpenCode source parses OpenAI Responses streams as stateful SSE event sequences, including text deltas, reasoning items, tool calls, and response lifecycle events.

MVP should not attempt to splice or continue one started stream with another provider.

## Failover Policy

MVP failover should be deterministic, sequential, and easy to reason about.

### Retryable Failures

- DNS/connect failure
- connection reset / EOF before first downstream byte
- request timeout before first downstream byte
- stream chunk timeout before first downstream byte
- HTTP `429` before first downstream byte
- HTTP `500-599` before first downstream byte

### Non-Retryable Failures

- alias not found
- alias has no enabled targets
- HTTP `400`
- HTTP `401`
- HTTP `403`
- HTTP `404`
- upstream validation error after request accepted

### Why Conservative Rules

Vendors return inconsistent error bodies. MVP should not try to infer hidden semantics such as "model not found, maybe retry elsewhere" from provider-specific error text.

Conservative failover is preferable to surprising failover.

## Proxy Debugging Headers

For debugging, `ocswitch` should add response headers where possible:

- `X-OCSWITCH-Alias`
- `X-OCSWITCH-Provider`
- `X-OCSWITCH-Remote-Model`
- `X-OCSWITCH-Attempt`
- `X-OCSWITCH-Failover-Count`

These headers are cheap and make failover behavior understandable.

## Config and State Strategy

Previous PRD centered around SQLite-first state. That is no longer necessary for MVP.

MVP should prefer a simpler user-editable config shape unless implementation proves this too limiting.

### Recommended MVP Direction

Use one local `ocswitch` JSON or JSONC config file for:

- upstream providers
- aliases
- local proxy settings

Reason:

- easiest to inspect and debug
- lowest implementation cost
- aligns with MVP goal of deterministic failover, not platform management

SQLite is explicitly out of MVP. It can be reconsidered later if request logs, mutation history, or richer syncing become important.

## Minimal CLI Surface

Recommended MVP commands:

### Core

- `ocswitch serve`
- `ocswitch doctor`

### `ocswitch doctor` MVP Boundary

First-release `ocswitch doctor` should stay side-effect free.

It should validate:

- local `ocswitch` config can be loaded
- every alias resolves to at least one enabled target
- local proxy bind address and config are internally consistent
- generated or synced `provider.ocswitch` config shape is structurally valid

It should not, by default:

- send real test requests to upstream providers
- consume quota from user providers
- mutate local or OpenCode config as part of diagnosis

If live upstream probing is needed later, it should be added as explicit opt-in behavior such as `ocswitch doctor --live`.

### Provider Management

- `ocswitch provider add`
- `ocswitch provider list`
- `ocswitch provider remove`
- `ocswitch provider import-opencode`

### Alias Management

- `ocswitch alias add`
- `ocswitch alias list`
- `ocswitch alias bind`
- `ocswitch alias unbind`
- `ocswitch alias remove`

### OpenCode Integration

- `ocswitch opencode sync`

## `ocswitch opencode sync` Responsibility

This command should do one narrow job:

- by default update or create custom provider `ocswitch` in global OpenCode user config
- prefer existing global config file in this order: `opencode.jsonc`, `opencode.json`, `config.json`
- if none exists, create `~/.config/opencode/opencode.jsonc`
- sync current alias names into `provider.ocswitch.models`

Optional extra behavior:

- set `model` only when user requests it explicitly, for example via a flag
- set `small_model` only when user requests it explicitly, for example via a flag

This command should not attempt a broad migration of existing providers.

Default MVP behavior should be conservative:

- do not rewrite existing OpenCode `model`
- do not rewrite existing OpenCode `small_model`
- do not write project-local OpenCode config unless user explicitly targets that file

## OpenCode `/models` Research Conclusion

Research result from OpenCode source:

1. OpenCode custom providers are declared in config under `provider.<id>`.
2. Custom model IDs come from keys under `provider.<id>.models`.
3. `opencode models` prints models from runtime `Provider.list()` state.
4. Web/TUI `/model` pickers read runtime provider lists and only show connected providers' models.
5. OpenCode supports alias key and real upstream model ID being different values.

Conclusion:

- alias exposure inside OpenCode is feasible in MVP
- simplest path is config sync, not custom remote model registry work
- syncing alias keys into `provider.ocswitch.models` is sufficient for exposure if `provider.ocswitch` lands in connected runtime provider state

## Security Model

MVP security posture should stay simple:

- listen on `127.0.0.1` by default
- use static local placeholder API key between OpenCode and `ocswitch`
- store upstream credentials in local `ocswitch` config
- document that local credential storage is sensitive

No multi-user or remote-network security guarantees in MVP.

## Success Criteria

MVP is successful if a user can:

1. configure at least two upstream providers manually or through OpenCode sync
2. create an alias with ordered targets across those providers
3. run `ocswitch opencode sync` and see alias names appear in OpenCode `opencode models` output and `/model` picker
4. select `ocswitch/<alias>` in OpenCode without exposing concrete upstream model IDs
5. send normal streaming OpenAI Responses traffic through `ocswitch`
6. get automatic failover when primary provider returns `429` or `5xx`, or fails before first downstream byte
7. observe that once a stream has started, later upstream failure is surfaced as failure rather than hidden mid-stream switching

## Recommended Implementation Order

### Phase 1

- bootstrap Go project
- config file loader/writer
- provider schema
- alias schema

### Phase 2

- `ocswitch opencode sync`
- narrow OpenCode provider import
- alias list exposure in OpenCode config
- connected-provider validation in `ocswitch doctor`

### Phase 3

- local proxy server
- `/v1/responses` forwarding
- JSON body replay for retry
- streaming pass-through
- sequential failover before first downstream byte

### Phase 4

- `ocswitch doctor`
- debugging headers and logs

## Finalized MVP Decisions

The following implementation choices are now locked for first-release MVP:

1. `ocswitch opencode sync` updates `provider.ocswitch` and alias exposure only by default. It must not modify OpenCode `model` or `small_model` unless explicit opt-in flags are provided.
2. Provider import support is limited to config-defined `@ai-sdk/openai` custom providers.
3. `ocswitch doctor` is static by default and must not issue live upstream requests unless future explicit opt-in behavior is added.

## Strong Recommendation

Do not keep the old PRD as implementation basis.

Old PRD optimized for a broader product: install/restore, protocol split, SQLite-first state, discovery, migration.

New MVP should optimize for one thing only:

- stable alias in OpenCode
- multiple upstream targets behind it
- deterministic local failover for OpenAI Responses traffic
