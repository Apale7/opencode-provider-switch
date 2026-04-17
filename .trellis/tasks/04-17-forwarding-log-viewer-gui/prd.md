# OCSWITCH Forwarding Log Viewer Design

## Summary

`ocswitch` currently exposes enough per-request routing information for header-level debugging, but it does not keep a durable request log and it does not provide any local UI.

This task defines a minimal, implementation-ready design for a local web log viewer that shows each forwarded request with:

- the selected provider
- the selected upstream model
- input, output, and total token counts when available
- whether failover was triggered
- the attempt chain that led to the final result

The design is intentionally scoped to the current codebase:

- Go backend only
- no existing frontend build pipeline
- only OpenAI Responses proxying today
- failover is only valid before first downstream byte

## Current Project State

### What already exists

`ocswitch` already has the core proxy path in `internal/proxy/server.go`:

- `POST /v1/responses`
- deterministic ordered failover
- retry on transport errors, `429`, and `5xx`
- no mid-stream failover after downstream response starts
- debug response headers:
  - `X-OCSWITCH-Alias`
  - `X-OCSWITCH-Provider`
  - `X-OCSWITCH-Remote-Model`
  - `X-OCSWITCH-Attempt`
  - `X-OCSWITCH-Failover-Count`

### What is missing

- no durable per-request log
- no token accounting capture
- no HTTP endpoint for log history
- no local HTML page
- no request attempt history persisted anywhere

## Product Goal

Give the user a simple local page that makes forwarding behavior visible without reading raw terminal logs.

For every completed forwarding operation, the page should make it obvious:

1. which alias was requested
2. which provider and upstream model finally served it
3. whether failover happened
4. how many attempts were made
5. how many input, output, and total tokens were reported

## Non-Goals

1. No hosted dashboard.
2. No Electron or native desktop GUI.
3. No cost accounting or billing estimation.
4. No prompt/response body browsing.
5. No full-text search over request payloads.
6. No live stream reconstruction in the browser.
7. No multi-user auth model beyond the current local-only default posture.

## Recommended MVP Shape

Choose a local web UI, not a separate GUI app.

Reason:

- it fits the current Go-only codebase
- it avoids adding a frontend toolchain
- it can run off the same `ocswitch serve` process and listener
- it is the smallest path to a usable visual log surface

## High-Level Design

When `ocswitch serve` is running, the same HTTP server should expose three surfaces:

1. proxy API
   - `POST /v1/responses`
   - `GET /v1/models`

2. log viewer UI
   - `GET /logs`

3. log viewer data API
   - `GET /api/logs`
   - `GET /api/logs/{id}`

The UI should be a single embedded HTML page with minimal CSS and vanilla JS.

No Node, bundler, or SPA framework should be introduced for this feature.

## Data Flow

```text
incoming /v1/responses request
  -> create in-memory request log draft
  -> record each upstream attempt result
  -> on final completion, attach usage if available
  -> append one finalized JSON record to local log file
  -> push finalized record into in-memory recent ring buffer
  -> /api/logs serves recent records to /logs page
```

The key design choice is: persist one finalized record per request, not one row per streaming chunk and not one row per partial attempt.

This keeps storage simple and makes the UI directly match the user's mental model: one forwarding operation equals one log entry.

## Storage Strategy

### Recommended MVP storage

Use an append-only JSONL file stored next to the existing `ocswitch` config:

- config path today: `~/.config/ocswitch/config.json` by default
- proposed log path: `~/.config/ocswitch/request-log.jsonl`

Reason:

- no database dependency required
- minimal code and operational complexity
- easy to inspect manually
- easy to rotate or delete
- aligned with current non-SQLite implementation

### In-memory cache

Maintain a small in-memory ring buffer of the most recent finalized records, for example the latest `500` entries.

On process start:

- load the latest records from `request-log.jsonl` into memory

During runtime:

- append finalized record to disk
- append finalized record to ring buffer

This gives fast UI reads while still preserving history across restarts.

## Log Record Schema

Each finalized request log entry should contain at least the following fields:

```json
{
  "id": "req_20260417_000123",
  "started_at": "2026-04-17T12:34:56.123Z",
  "completed_at": "2026-04-17T12:34:58.004Z",
  "duration_ms": 1881,
  "protocol": "openai-responses",
  "alias": "gpt-5.4",
  "raw_model": "ocswitch/gpt-5.4",
  "stream": true,
  "final_status": 200,
  "final_provider": "p2",
  "final_remote_model": "GPT-5.4",
  "attempt_count": 2,
  "failover_count": 1,
  "failover_triggered": true,
  "usage": {
    "input_tokens": 812,
    "output_tokens": 143,
    "total_tokens": 955,
    "available": true,
    "source": "response_usage"
  },
  "attempts": [
    {
      "attempt": 1,
      "provider": "p1",
      "remote_model": "up-1",
      "outcome": "retryable_status",
      "upstream_status": 429,
      "error_summary": "upstream 429: rate limit",
      "duration_ms": 220
    },
    {
      "attempt": 2,
      "provider": "p2",
      "remote_model": "GPT-5.4",
      "outcome": "success",
      "upstream_status": 200,
      "duration_ms": 1661
    }
  ]
}
```

### Required field semantics

- `alias`: normalized local alias actually routed by `ocswitch`
- `raw_model`: original request payload `model` value before normalization
- `final_provider`: provider that produced the downstream response visible to the client
- `final_remote_model`: upstream model name actually sent to that provider
- `failover_triggered`: `true` when the request had to move beyond the initial target
- `attempts`: ordered attempt chain, including retryable failures before success

### Fields that must stay nullable

`usage.input_tokens`, `usage.output_tokens`, and `usage.total_tokens` must be nullable in storage and UI.

Reason:

- some failures end before usage exists
- some providers may omit usage
- some stream terminations may prevent complete usage extraction

## Token Collection Design

### Requirement boundary

The user asked for input/output token counts, so MVP must collect usage from upstream responses when that information is present.

### Non-streaming path

For non-streaming OpenAI Responses responses:

- capture the full JSON body already headed to the client
- parse `usage` from the final response object

### Streaming path

For streaming responses:

- do not buffer the whole response before writing downstream
- keep current streaming pass-through behavior
- tee streamed bytes into a lightweight SSE parser
- extract usage from the terminal lifecycle event that contains final response usage when present

In practice this will usually mean parsing the final response completion event and reading its usage payload.

### Missing usage behavior

If usage cannot be determined:

- keep the request log entry
- set usage fields to `null`
- set `usage.available = false`
- record a short reason such as `missing_from_upstream` or `stream_incomplete`

The request itself must not fail just because logging could not extract usage.

## Failover Visibility Design

The UI must make failover impossible to miss.

### Request list presentation

Each row should show a failover badge with one of two states:

- `Primary` when `failover_triggered = false`
- `Failover` when `failover_triggered = true`

### Request detail presentation

Expanding a row should show the attempt chain in order, for example:

```text
Attempt 1  p1 / up-1     429 retryable
Attempt 2  p2 / GPT-5.4 success
```

This detail view is necessary because `failover_count` alone does not explain why failover happened.

## UI Design

### Route

`GET /logs`

### Layout

Use one compact page with two sections:

1. top summary strip
2. request table with expandable detail rows

### Summary strip

Show a small set of metrics derived from loaded records:

- recent request count
- recent failover count
- recent failover rate
- last request time

This should stay informational only. No charts are required for MVP.

### Request table columns

The main table should show:

- Time
- Alias
- Final Provider
- Final Model
- Status
- Tokens In
- Tokens Out
- Tokens Total
- Failover
- Duration

### Filters

Keep filters intentionally small:

- text search for alias/provider/model
- failover only toggle
- status filter
- page size selector

### Detail row

Expanding a row should show:

- request id
- raw model
- stream true/false
- attempt timeline
- short error summary for failed attempts

Do not show prompt text, response text, or authorization headers.

## HTTP API Design

### `GET /api/logs`

Returns recent finalized records in reverse chronological order.

Suggested query params:

- `limit`
- `cursor`
- `failover_only`
- `status`
- `q`

### `GET /api/logs/{id}`

Returns one full finalized record including all attempts.

### Pagination

Cursor pagination is preferred over offset pagination.

Reason:

- append-only log shape
- stable ordering by completion time
- easier incremental page refresh

## Security and Privacy

### Must not log

- upstream API keys
- incoming Authorization headers
- full request body
- full model input text
- full model output text

### Why

The viewer is for routing observability, not transcript storage.

This keeps the feature useful without turning it into a sensitive local prompt archive.

### Local exposure model

For MVP, the log viewer can follow the same trust model as the current local proxy:

- same local process
- same loopback listener by default
- intended for single-user localhost usage

If `ocswitch` is later allowed to bind non-loopback addresses, the log UI and log API should be explicitly gated before reuse in that mode.

## Implementation Plan

### Phase 1: request log model and storage

- add a request log record type
- add append-only JSONL writer
- add startup loader for recent records
- add in-memory ring buffer

### Phase 2: proxy instrumentation

- create one draft log object per incoming request
- capture normalized alias and raw model
- record every attempt outcome
- record final provider, remote model, status, and duration
- extract usage from success responses when available

### Phase 3: local web surface

- add `GET /logs`
- add `GET /api/logs`
- add `GET /api/logs/{id}`
- embed a single static HTML/CSS/JS page

### Phase 4: docs and tests

- document the viewer route in `README.md`
- add tests for failover logging
- add tests for non-failover logging
- add tests for usage extraction fallback behavior

## Suggested Package Additions

Keep the changes minimal.

Suggested new package:

```text
internal/requestlog/
  model.go
  store.go
  api.go
```

Suggested existing files to extend:

- `internal/proxy/server.go`
- `internal/proxy/server_test.go`
- `internal/cli/serve.go`
- `README.md`

## Acceptance Criteria

This design is considered implemented successfully when:

1. running `ocswitch serve` exposes a local page at `/logs`
2. the page shows one row per completed forwarding request
3. each row clearly shows final provider and final upstream model
4. each row clearly shows whether failover happened
5. token counts are displayed when upstream usage is available
6. expanding a row shows the attempt chain that led to the final result
7. no request/response text or secrets are persisted to the log

## Out of Scope for This Task

1. historical charts and trends
2. exporting CSV
3. provider health scoring
4. live websocket or SSE push to the browser
5. per-provider cost calculation
6. editing provider or alias config from the UI

## Final Recommendation

Implement this as a same-process local web viewer backed by append-only JSONL request summaries.

This is the smallest design that satisfies the user's stated need:

- concise GUI/web page
- explicit provider/model visibility
- token visibility
- unambiguous failover visibility

It also fits the actual repository state today without introducing a frontend stack, database migration, or broader dashboard scope.
