# OLPX MVP Design

## Summary

`opencode-provider-switch` (`olpx`) is a local CLI + proxy for OpenCode.

Its job is narrow:

- Accept OpenCode traffic on `http://127.0.0.1:9982`
- Route requests by protocol, not by provider brand
- Retry/fail over across unreliable upstream relay providers
- Let multiple upstream providers share one logical model alias
- Manage state in SQLite, not in an `olpx` config file
- Rewrite OpenCode global config to point at the local proxy

MVP intentionally does **not** try to become a general AI gateway, a dashboard product, or a provider-agnostic orchestration platform.

## Confirmed Decisions

- Language: Go
- Persistence: SQLite
- Default listen address: `127.0.0.1:9982`
- Delivery shape: CLI only
- MVP protocols:
  - `openai-chat-completions`
  - `openai-responses`
- Anthropic native protocol: out of MVP
- Logical model aliasing: required
- Billing/cost accounting: out of MVP
- Provider model discovery via `/models`: in MVP, best-effort

## Product Goals

1. Keep OpenCode usable when cheap relay providers fail intermittently.
2. Reduce OpenCode config complexity by centralizing provider management in `olpx`.
3. Make failover behavior predictable, visible, and debuggable.
4. Preserve OpenCode-native workflow instead of asking users to switch tools.

## Non-Goals

1. No GUI, web dashboard, or hosted control plane.
2. No Anthropic native API support in MVP.
3. No automatic price tracking or billing reconciliation.
4. No smart routing based on prompt content, latency scoring, or cost optimization.
5. No distributed/high-availability deployment story.
6. No attempt to normalize every provider quirk in MVP.

## Why Go

Go is the right fit for this product shape:

- `net/http` is strong enough for proxying and streaming without extra framework weight.
- Cross-platform static builds are straightforward.
- SQLite works well as a local state store.
- CLI ergonomics are mature with `cobra`.
- Concurrency model is a good fit for retries, request cancellation, and stream handling.

## Core Design Principle

`olpx` should manage **protocol pools** and **logical aliases**, not raw provider selection inside OpenCode.

OpenCode should see a small number of local proxy providers. `olpx` should own:

- upstream providers
- API keys and headers
- failover order
- alias to provider-model mapping
- model discovery cache

This keeps OpenCode config stable even when upstream providers are added, removed, or reprioritized.

## High-Level Architecture

```text
OpenCode
  -> local OpenAI-compatible provider config
  -> olpx proxy (127.0.0.1:9982)
  -> protocol router
  -> alias resolver
  -> failover engine
  -> upstream relay provider A / B / C
```

### Major Components

1. `cli`
   - all user-facing commands
   - install, restore, provider management, alias management, doctor, serve

2. `sqlite store`
   - source of truth for providers, aliases, protocol pools, model cache, install state

3. `proxy server`
   - handles incoming HTTP requests from OpenCode
   - supports streaming and non-streaming pass-through

4. `router`
   - resolves protocol + alias -> ordered upstream targets

5. `failover engine`
   - sequentially tries candidate targets based on priority and failure policy

6. `opencode integration`
   - imports current OpenCode config
   - writes generated proxy-backed OpenCode config
   - restores backup when requested

7. `model discovery`
   - fetches raw `/models` data from providers
   - stores normalized cache for alias creation and diagnostics

## Supported Protocols in MVP

### 1. OpenAI Chat Completions

- Incoming route: `POST /v1/chat/completions`
- Discovery route: `GET /v1/models`
- OpenCode provider package: `@ai-sdk/openai-compatible`

### 2. OpenAI Responses

- Incoming route: `POST /v1/responses`
- Discovery route: `GET /v1/models`
- OpenCode provider package: `@ai-sdk/openai`

## Critical Scope Constraint

Even though both protocols are OpenAI-family APIs, they are **not** interchangeable at the OpenCode config level.

For MVP, `olpx` should expose two local providers to OpenCode:

- one chat-completions provider
- one responses provider

This mirrors how OpenCode expects provider wiring today and avoids hidden protocol translation logic inside `olpx`.

## OpenCode Integration Strategy

### Generated OpenCode Config

`olpx install` should rewrite the user's global OpenCode config so that OpenCode points to local proxy providers instead of raw upstream relays.

Generated provider shape should be conceptually like this:

```json
{
  "$schema": "https://opencode.ai/config.json",
  "provider": {
    "olpx-chat": {
      "npm": "@ai-sdk/openai-compatible",
      "name": "OLPX Chat",
      "options": {
        "baseURL": "http://127.0.0.1:9982/v1",
        "apiKey": "olpx-local"
      },
      "models": {
        "gpt-5.4": {
          "name": "gpt-5.4"
        }
      }
    },
    "olpx-responses": {
      "npm": "@ai-sdk/openai",
      "name": "OLPX Responses",
      "options": {
        "baseURL": "http://127.0.0.1:9982/v1",
        "apiKey": "olpx-local"
      },
      "models": {
        "gpt-5.4": {
          "name": "gpt-5.4"
        }
      }
    }
  },
  "model": "olpx-responses/gpt-5.4",
  "small_model": "olpx-chat/gpt-5.4-mini"
}
```

### Important Rule

`olpx` should preserve as much of the user's existing OpenCode config as possible.

`olpx install` should:

1. Back up current global config file.
2. Import provider/model information relevant to migration.
3. Preserve unrelated keys like `plugin`, `agent`, `formatter`, `permission`, `instructions`, and other user settings.
4. Replace only the parts that need to point to local proxy providers:
   - `provider`
   - `model`
   - `small_model`

### Project Config Limitation

OpenCode merges config from multiple sources, and project config can override global config.

That means `olpx install` cannot guarantee full interception if a repository-local `opencode.json` or environment override replaces provider/model settings.

MVP response:

- document this clearly
- make `olpx doctor` detect likely overrides
- support global install first

Do **not** promise perfect takeover across all OpenCode precedence layers in MVP.

## Migration Strategy

### Input Sources

`olpx install` should inspect:

1. `~/.config/opencode/opencode.json`
2. `~/.config/opencode/opencode.jsonc`
3. `~/.local/share/opencode/auth.json` when present

Reason:

- some users store `apiKey` inline in config
- OpenCode official flow often stores credentials in `auth.json`

### Migration Behavior

For each imported provider/model entry from OpenCode:

1. Determine whether it is chat-completions-oriented or responses-oriented.
2. Create an upstream provider record in SQLite.
3. Create logical aliases from existing model names.
4. Preserve model metadata when available from config:
   - display name
   - limit
   - attachment
   - reasoning
   - tool_call
   - options
   - variants
5. Generate local `olpx-*` providers for OpenCode.

### Protocol Classification

Import classification rules for MVP:

- `@ai-sdk/openai-compatible` -> `openai-chat-completions`
- `@ai-sdk/openai` -> `openai-responses`
- everything else -> unsupported for automatic migration in MVP

If unsupported providers exist, `olpx install` should warn and skip them instead of guessing.

## SQLite-First State Model

`olpx` should not keep its own user-editable config file.

Recommended database path:

- Linux/WSL: `~/.local/share/olpx/olpx.db`
- Windows native: use `os.UserConfigDir()` or `os.UserCacheDir()`-appropriate app path, finalized in implementation

The generated OpenCode config file is an output artifact, not `olpx` source of truth.

### Proposed Tables

#### `providers`

Stores upstream provider connection info.

Suggested columns:

- `id`
- `name`
- `protocol`
- `base_url`
- `api_key`
- `headers_json`
- `priority`
- `enabled`
- `created_at`
- `updated_at`

#### `provider_models`

Stores raw models discovered from upstream or imported from OpenCode config.

Suggested columns:

- `id`
- `provider_id`
- `remote_model_id`
- `display_name`
- `source`
- `raw_json`
- `last_seen_at`

#### `aliases`

Stores user-facing logical model names per protocol.

Suggested columns:

- `id`
- `protocol`
- `alias`
- `display_name`
- `capabilities_json`
- `limits_json`
- `options_json`
- `variants_json`
- `enabled`
- `created_at`
- `updated_at`

#### `alias_targets`

Maps one logical alias to one or more provider-specific targets.

Suggested columns:

- `id`
- `alias_id`
- `provider_id`
- `remote_model_id`
- `priority`
- `enabled`
- `created_at`
- `updated_at`

#### `install_state`

Tracks OpenCode integration state.

Suggested columns:

- `id`
- `backup_path`
- `managed_config_path`
- `installed_at`
- `last_generated_at`
- `original_config_hash`
- `generated_config_hash`

#### `request_log`

Optional but useful even in MVP for debugging. Keep retention small.

Suggested columns:

- `id`
- `protocol`
- `alias`
- `provider_id`
- `remote_model_id`
- `attempt`
- `result`
- `status_code`
- `duration_ms`
- `error_text`
- `created_at`

## Model Alias Design

Alias support is a core MVP feature, not a nice-to-have.

Example:

- logical alias: `gpt-5.4`
- responses target priority 1: provider `su8`, remote model `gpt-5.4`
- responses target priority 2: provider `codex-for-me`, remote model `GPT-5.4`
- chat target priority 1: provider `relay-x`, remote model `gpt-5.4-chat`

This allows the user to keep using one stable model name inside OpenCode while `olpx` handles provider-specific naming.

### Important Constraint

Aliases should be scoped by protocol.

`gpt-5.4` for chat completions and `gpt-5.4` for responses may exist simultaneously, but they should resolve through separate alias records or a protocol-aware unique key.

This avoids accidental cross-protocol reuse.

## `/models` Discovery Design

### Purpose

Reduce manual provider setup work.

### Behavior

`olpx` should be able to call upstream `GET /v1/models` and cache results per provider.

Useful commands:

- `olpx provider models sync <provider>`
- `olpx provider models list <provider>`
- `olpx alias suggest <provider>`

### Important Limitation

Provider `/models` data is often incomplete or inconsistent.

It usually does **not** fully describe:

- context length
- output limits
- attachment support
- reasoning support
- tool calling support
- custom request options

So for MVP, `/models` must be treated as:

- good for raw model ID discovery
- not authoritative for OpenCode capability metadata

This means alias metadata may still need manual editing or migration from prior OpenCode config.

## Proxy Request Flow

### Non-Streaming Request

1. Receive request on `/v1/chat/completions` or `/v1/responses`.
2. Parse request body enough to extract `model`.
3. Resolve `(protocol, alias)` to ordered alias targets.
4. Replace logical alias with provider-specific remote model ID.
5. Forward request to highest-priority enabled provider.
6. If response is retryable failure, try next target.
7. Return first successful response.

### Streaming Request

Same as above, with one critical rule:

- failover is only allowed **before first upstream response byte is sent to the client**

Once a stream begins successfully, `olpx` must stay on that provider for that request.

## Failover Policy

MVP failover should stay simple and explicit.

### Retryable Conditions

- DNS/connect failure
- TCP reset / EOF before response
- request timeout
- chunk timeout before first byte
- HTTP `429`
- HTTP `500-599`

### Non-Retryable Conditions

- HTTP `400`
- HTTP `401`
- HTTP `403`
- HTTP `404`
- request validation errors from upstream after request is accepted

### Special Case: Model Not Found

Some providers return `404` or `400` for unknown model IDs.

MVP should **not** build broad heuristic parsing for this.

Reason:

- error bodies are inconsistent
- aggressive parsing will create surprising behavior

Safer MVP rule:

- do not auto-fail over on ambiguous `400/404`
- surface error clearly

This is conservative, but predictable.

## Proxy Response Headers

For debugging, add response headers when possible:

- `X-OLPX-Protocol`
- `X-OLPX-Alias`
- `X-OLPX-Provider`
- `X-OLPX-Remote-Model`
- `X-OLPX-Attempt`
- `X-OLPX-Failover-Count`

These headers are low-cost and help explain behavior fast.

## Local Proxy Security Model

MVP security posture should be intentionally modest but clear.

### Default Behavior

- bind only to `127.0.0.1`
- generated OpenCode config uses local static API key like `olpx-local`
- proxy accepts only loopback traffic by default

### Why This Is Acceptable For MVP

- tool is local-only by default
- threat model is mostly accidental exposure, not multi-tenant isolation

### Caveat

SQLite-stored API keys are sensitive.

MVP options:

1. Store plaintext in SQLite with strict file permissions.
2. Defer OS keychain integration to later.

Recommended MVP choice:

- plaintext in SQLite
- create DB with owner-only permissions where possible
- document this explicitly

Reason:

- simplest implementation
- consistent cross-platform behavior
- avoids blocking MVP on secret-store complexity

## CLI Surface

Proposed command set:

### Lifecycle

- `olpx init`
- `olpx serve`
- `olpx doctor`
- `olpx install`
- `olpx restore`

### Provider Management

- `olpx provider add`
- `olpx provider list`
- `olpx provider edit`
- `olpx provider remove`
- `olpx provider enable`
- `olpx provider disable`

### Model Discovery

- `olpx provider models sync <provider>`
- `olpx provider models list <provider>`

### Alias Management

- `olpx alias add`
- `olpx alias list`
- `olpx alias bind`
- `olpx alias unbind`
- `olpx alias enable`
- `olpx alias disable`
- `olpx alias inspect <alias>`

### Diagnostics

- `olpx logs tail`
- `olpx route test --protocol <protocol> --model <alias>`

## Recommended Minimal UX

Prefer explicit CLI over magical automation.

Good path:

1. `olpx init`
2. `olpx provider add`
3. `olpx provider models sync`
4. `olpx alias add`
5. `olpx alias bind`
6. `olpx install`
7. `olpx serve`

This is easy to explain and easy to debug.

## Suggested Go Package Layout

```text
cmd/olpx/
internal/cli/
internal/db/
internal/models/
internal/providers/
internal/aliases/
internal/proxy/
internal/router/
internal/failover/
internal/opencode/
internal/doctor/
internal/logging/
```

### Library Choices

- CLI: `cobra`
- SQLite driver: `modernc.org/sqlite`
- Logging: stdlib `log/slog`
- HTTP: stdlib `net/http`
- JSONC parsing for OpenCode config import/export: `tailscale/hujson` or equivalent

Avoid adding a heavy HTTP framework unless a real need appears.

## Generated OpenCode Provider Strategy

MVP should generate only providers that `olpx` can actually back.

That means:

- `olpx-chat`
- `olpx-responses`

Do **not** generate fake Anthropic provider entries in MVP.

Do **not** attempt to preserve original provider IDs inside OpenCode after installation.

Reason:

- `olpx` becomes the stable local provider boundary
- upstream providers should move into SQLite management only

## WSL / Windows Strategy

This requirement is important, but it needs careful wording.

### What MVP Should Promise

1. Native Linux/WSL build works.
2. Native Windows build works.
3. Running OpenCode and `olpx` in the **same environment** is supported.
4. `olpx doctor` helps detect config-path and loopback issues.

### What MVP Should Not Promise Yet

1. Fully automatic cross-boundary migration between WSL OpenCode and Windows `olpx`.
2. Transparent path translation for every user setup.
3. Zero-config interop when OpenCode runs on one side and proxy on the other.

### Practical Recommendation

For MVP, recommend users run OpenCode and `olpx` in the same environment.

Cross-environment support can be added later via explicit install target flags.

## Biggest Risks

### 1. OpenCode Config Precedence

Global config rewrite alone may not capture project-level overrides.

Mitigation:

- `olpx doctor`
- clear docs
- possible future `ops install --project`

### 2. Provider `/models` Quality

Discovery helps with model IDs, but not with full capability metadata.

Mitigation:

- preserve metadata during import
- allow manual alias metadata editing

### 3. Streaming Edge Cases

Once bytes are sent, failover is no longer safe.

Mitigation:

- fail over only before first byte
- good timeout defaults
- precise logging

### 4. Secret Storage

SQLite plaintext credentials are acceptable for MVP, but they are still sensitive.

Mitigation:

- strict file permissions
- local-only binding
- document tradeoff

### 5. Protocol Ambiguity During Migration

OpenCode custom providers can mix package usage in ways that are not cleanly inferable.

Mitigation:

- support only known package mappings in MVP
- warn instead of guessing

## MVP Success Criteria

MVP is successful if a user can:

1. Import existing OpenCode provider setup into `ops`.
2. Create or verify aliases for commonly used models.
3. Install proxy-backed OpenCode global config.
4. Run `olpx serve`.
5. Use OpenCode normally against `127.0.0.1:9982`.
6. Survive common upstream failures by automatic provider failover.

## Recommended Implementation Order

### Phase 1

- bootstrap Go project
- SQLite store
- provider CRUD
- alias CRUD

### Phase 2

- `GET /v1/models` sync
- import from OpenCode config/auth
- generated OpenCode config writer

### Phase 3

- proxy server
- chat completions forwarding
- responses forwarding
- non-streaming failover

### Phase 4

- streaming support
- logging/diagnostics
- `olpx doctor`
- restore flow

## Strong Recommendation

Do not start with background health checks, latency scoring, or dynamic routing.

Start with deterministic priority failover only.

That gives the product a sharp identity:

- local
- understandable
- reliable enough
- much smaller than LiteLLM

## Next Step

Next practical step is implementation bootstrap for:

1. Go module structure
2. SQLite schema migrations
3. Cobra command tree
4. OpenCode config importer/exporter

That is the smallest slice that validates the design without touching complex proxy streaming first.
