# SSE idle timeout and precommit buffer

## Summary

Improve ocswitch behavior when an upstream provider starts a streaming SSE response and then stalls. Today, once a provider returns `200` with `text/event-stream`, ocswitch can keep the local client waiting indefinitely because SSE reads bypass `stream_idle_timeout_ms`.

This task adds:

1. Default SSE idle protection: SSE streams must obey `server.stream_idle_timeout_ms`; on idle, ocswitch emits a protocol-compatible SSE error event, flushes, closes the stream, and records failure.
2. Optional continuity mode: `server.stream_precommit_buffer_ms` delays downstream response commit for a short window so metadata-only or fake-started streams can fail over before the client sees bytes.

Plan Review status: APPROVED.

## Problem

For coding workloads, task completion is more important than immediate first-token latency. Current behavior has a bad failure mode:

- ocswitch selects an alias target and connects to provider.
- provider returns SSE headers and possibly metadata/first bytes.
- provider then stops sending meaningful content and does not close.
- ocswitch has already committed the downstream response, so normal failover cannot safely continue.
- current SSE loop reads directly from `resp.Body.Read` and does not enforce `stream_idle_timeout_ms`, so client can hang until user/client/network aborts.

Evidence from current code:

- `internal/proxy/server.go` uses `stream_idle_timeout_ms` only for non-SSE post-first-byte reads.
- `text/event-stream` currently bypasses timeout-aware reads.
- Existing test `TestHandleResponsesSSEBypassesIdleTimeout` documents the current bypass behavior.

## Goals

- Make SSE streams obey `stream_idle_timeout_ms` after downstream response commit.
- Emit protocol-compatible SSE error events on post-commit idle timeout.
- Preserve safe routing semantics: no transparent provider switch after downstream commit.
- Add optional `stream_precommit_buffer_ms` that allows failover before downstream commit when the upstream only sends metadata/pings or fake-starts and then stalls.
- Treat protocol completion markers as success and stop waiting for upstream EOF, so a complete stream cannot hang because TCP remains open.
- Persist and expose the new setting in desktop and server web settings UI with English and zh-CN text.
- Keep non-SSE behavior and existing status-code failover behavior unchanged.

## Non-Goals

1. Do not replay or merge partial responses from multiple providers after bytes were delivered to the client.
2. Do not implement transparent mid-stream failover after downstream response commit.
3. Do not add provider-specific hidden heuristics beyond protocol-level SSE frame classification.
4. Do not change default first-token latency; precommit buffering is disabled by default.
5. Do not emit fake success or final usage events.
6. Do not expose upstream API keys in logs, traces, or error events.

## User Experience

### Default behavior

With default settings:

- `stream_precommit_buffer_ms = 0`
- `stream_idle_timeout_ms = 60000`

SSE response still starts immediately after first upstream bytes, but if the stream stalls later, the client receives a clear stream error instead of waiting forever.

### Optional coding-continuity mode

Users can set `stream_precommit_buffer_ms` to a short window, such as `3000-5000` ms. ocswitch then delays sending the first SSE bytes downstream until it sees commit-worthy content, terminal success, buffer cap, or timeout.

If upstream only sends metadata/ping/fake-start and then stalls during this window, ocswitch has not committed downstream yet, so it can fail over to another base URL, API key, or alias target.

Recommended values:

- Default: `stream_precommit_buffer_ms: 0`, `stream_idle_timeout_ms: 60000`
- Coding continuity: `stream_precommit_buffer_ms: 3000-5000`
- Slow/high-latency providers: `stream_precommit_buffer_ms: 8000-10000`, `stream_idle_timeout_ms: 90000-120000`

## State Model

1. **pre-response**: no upstream response body bytes yet. Existing response-header and first-byte timeouts apply. Failures are retryable.
2. **precommit**: upstream returned 2xx SSE and ocswitch buffered bytes, but downstream has not received `WriteHeader` or body. Failures are retryable.
3. **committed**: downstream received headers/body. Failures are non-retryable; ocswitch may only emit protocol error event and close.
4. **terminal-success observed**: protocol completion marker was seen. ocswitch writes it, flushes, records success, closes/cancels upstream, and does not wait for EOF.

## Timeout Semantics

- `first_byte_timeout_ms`: request start to first upstream body bytes. Retryable before downstream commit.
- `stream_idle_timeout_ms`: maximum gap between upstream body reads after first bytes. Applies in precommit and committed phases.
- `stream_precommit_buffer_ms`: absolute maximum precommit wait from first upstream body bytes to commit-worthy content. Default `0` disables precommit buffering.
- During precommit, next read deadline is `min(stream_idle_timeout_ms, remaining stream_precommit_buffer_ms)`.
  - If stream idle fires first: retryable `precommit_stream_idle_timeout`.
  - If precommit window expires before commit-worthy content or terminal success: retryable `precommit_no_content_timeout`.
  - If metadata/comments/pings keep arriving but no content appears, absolute precommit window still expires and failover occurs.
  - If safe byte cap is reached before content classification completes, commit immediately to avoid memory growth.

## Protocol Terminal Markers

Terminal success markers must be detected only from complete SSE frames, including split frames across chunks. After forwarding a terminal success marker, do not wait for EOF and do not emit synthetic idle errors.

- OpenAI-compatible Chat Completions: `data: [DONE]`
- Anthropic Messages: `event: message_stop`
- OpenAI Responses: `event: response.completed` with complete data frame
- `response.incomplete` is not success. Forward it and classify as failure only if implementation has explicit semantics and tests.
- Provider-sent `event: error` or error payload is not terminal success. Forward normal committed bytes; trace behavior should remain governed by EOF/error unless explicit error classification is implemented and tested.

## Protocol Error Events

Use `json.Marshal`; do not string-interpolate JSON. Do not change HTTP status after downstream commit. Do not append fake final usage events.

OpenAI Responses:

```text
event: error
data: {"type":"error","error":{"type":"server_error","code":"upstream_stream_idle_timeout","message":"..."}}

```

OpenAI-compatible:

```text
data: {"error":{"message":"...","type":"server_error","code":"upstream_stream_idle_timeout"}}

```

Anthropic Messages:

```text
event: error
data: {"type":"error","error":{"type":"api_error","message":"..."}}

```

Default: do not append `[DONE]` after a synthetic error unless supported-client tests prove it is necessary.

## Precommit Buffer Behavior

Applies only when all are true:

- `stream_precommit_buffer_ms > 0`
- upstream status is 2xx
- upstream `Content-Type` is `text/event-stream`

Implementation should use a protocol-aware SSE state helper with two buffers:

- raw output buffer: exact bytes to forward downstream unchanged after commit
- parser buffer: complete-frame classification and terminal detection

### Commit-worthy content

Commit immediately when one of these appears:

- OpenAI Responses: `response.output_text.delta`, `response.output_item.added`, `response.output_item.done`, `response.function_call_arguments.delta`, or any non-empty data frame not classified as metadata-only.
- OpenAI-compatible: non-empty `choices[].delta.content`, `choices[].delta.tool_calls`, `choices[].delta.function_call`, or any non-empty data frame not classified as metadata-only.
- Anthropic Messages: `content_block_start`, `content_block_delta`, `input_json_delta`, `message_delta` carrying output/tool payload, or any non-empty data frame not classified as metadata-only.

Metadata-only frames do not force commit:

- `message_start`
- `response.created`
- ping/comment frames
- empty frames
- provider keepalives

Conservative fallback: unknown non-empty data commits rather than causing false failover.

### Precommit outcomes

- Commit-worthy content before deadline: write original upstream status/headers, write buffered raw bytes in exact order, flush, enter committed timeout-aware loop.
- Terminal success before commit: write original status/headers plus buffered raw bytes, flush, mark success, close/cancel upstream, return success.
- Precommit window expires with only metadata/pings/comments or silence: close/cancel upstream, do not write downstream, return retryable `precommit_no_content_timeout`.
- Stream idle timeout fires before precommit window: close/cancel upstream, do not write downstream, return retryable `precommit_stream_idle_timeout`.
- EOF before commit with terminal success marker: success.
- EOF before commit without terminal success and without commit-worthy content: retryable `precommit_incomplete_stream`.
- Safe byte cap reached, recommended 256 KiB: commit immediately with buffered bytes, then enter committed loop.

## Cross-Layer Contract

### Config

Add server field:

```json
"stream_precommit_buffer_ms": 0
```

Rules:

- Default `0`.
- Old configs load unchanged.
- Value must be non-negative.
- `0` disables precommit buffering.
- `stream_idle_timeout_ms` existing default remains `60000` and now applies to SSE too.

### App Service / Web Admin / Wails

- Add `StreamPrecommitBufferMs` to `ProxySettingsView` and `ProxySettingsInput`.
- Persist through `SaveProxySettings`.
- Return normalized value from `GetProxySettings`.
- Update Wails generated models or ambient bridge declarations if needed.

### Proxy

- Replace SSE direct `resp.Body.Read` loop with timeout-aware reads.
- Preserve exact raw SSE bytes on normal forwarding.
- Emit synthetic protocol error frame only after post-commit idle timeout.
- Return `handled=false, retryable=true` for precommit failures so existing baseURL/API-key/target failover remains the single routing path.
- Return `handled=true, retryable=false` for post-commit idle failures.
- On idle/precommit timeout or terminal success, close/cancel upstream body/request to avoid blocked reads, leaked goroutines, and idle connections.

### Frontend

- Add `streamPrecommitBufferMs` to frontend proxy settings type and default state.
- Add Settings panel number input with min `0`.
- Add bilingual help text explaining latency vs failover continuity.
- Keep all new UI strings in `en.json` and `zh-CN.json`.

## Acceptance Criteria

1. SSE stream returning 200 + first bytes then post-commit stall closes near `stream_idle_timeout_ms` with protocol error event; no indefinite hang.
2. Completion marker followed by upstream hang is treated as success, not timeout failure.
3. With `stream_precommit_buffer_ms = 0`, first bytes commit immediately except terminal marker handling and post-commit idle protection.
4. With `stream_precommit_buffer_ms > 0`, metadata-only fake-starts fail over before downstream commit when no content arrives by precommit deadline.
5. Metadata continuously arriving without content still fails over at absolute precommit deadline.
6. Unknown non-empty data commits conservatively rather than causing false failover.
7. Split SSE frames are classified only after complete frame, while raw bytes are forwarded unchanged after commit.
8. Safe cap prevents unbounded buffering and forces commit.
9. After downstream commit, no transparent provider switch occurs.
10. Non-SSE behavior and status-code failover remain unchanged.
11. Settings persist and are visible in desktop/server web UI.
12. All new UI copy has English and zh-CN entries.
13. Targeted and full regressions pass.
14. Local opencode audit is run after backend and frontend milestones; findings are fixed or escalated.

## Implementation Plan

1. Baseline evidence
   - Confirm current SSE bypass behavior and capture targeted test status with `go test ./internal/proxy ./internal/config ./internal/app`.
2. Backend config contract
   - Add and validate `stream_precommit_buffer_ms` across config, app DTOs, settings save/view paths, and tests.
3. SSE frame state helper
   - Add protocol-aware parser/classifier for terminal success, metadata-only, commit-worthy content, split frames, unknown non-empty fallback, and cap decisions.
4. SSE error event helper
   - Add protocol-specific JSON-marshaled SSE error event helper and unit tests for three protocols.
5. Post-commit SSE idle timeout
   - Enforce `stream_idle_timeout_ms` in committed SSE loop; honor terminal success markers; record trace failure/success correctly.
6. Precommit buffer
   - Implement optional buffer, retryable precommit failures, cap-forced commit, and reuse committed streaming loop.
7. Proxy regression tests
   - Replace old bypass test and cover post-commit idle, no failover after commit, terminal marker success despite upstream hang, precommit failover, continuous metadata no-content failover, cap-forced commit, split-frame preservation/classification, trace/failure reason, and all supported protocols.
8. Backend local audit
   - Run project-required local opencode audit and iterate until no obvious issues or blockers are escalated.
9. Frontend settings and i18n
   - Expose precommit buffer in Settings with bilingual copy.
10. Frontend verification and local audit
    - Run `npm run build` in `frontend/` and local audit.
11. Docs/operator notes
    - Document settings, defaults, no post-commit failover rule, protocol error close behavior, and recommended values.
12. Final regression
    - Run `go test ./...`, frontend build, changed-file review, and record residual risks.

## Verification

- Backend targeted: `go test ./internal/proxy ./internal/config ./internal/app`
- Full backend: `go test ./...`
- Frontend: from `frontend/`, `npm run build`
- Local opencode audit after backend module and frontend module

## Notes

- Relevant code areas:
  - `internal/proxy/server.go`
  - `internal/proxy/server_test.go`
  - `internal/proxy/usage_collector.go`
  - `internal/config/config.go`
  - `internal/app/types.go`
  - `internal/app/service.go`
  - `frontend/src/App.tsx`
  - `frontend/src/types.ts`
  - `frontend/src/i18n/locales/en.json`
  - `frontend/src/i18n/locales/zh-CN.json`
- Plan Review returned `APPROVED` with no required changes.
