# Proxy SSE Minimal Fix

## Summary

The local Responses proxy can report `upstream_status=200` while still handing OpenCode a broken stream in two narrow cases inside `internal/proxy/server.go`:

- the upstream returns `200` with `text/event-stream` but closes before sending any bytes
- the upstream sends one SSE chunk and then stays silent longer than `streamIdleTimeout`, causing the proxy to terminate a still-valid SSE stream

This task applies the smallest possible fix in the proxy layer only, without changing CLI, desktop, app, config, or public interfaces.

## Requirements

- Keep the existing `firstByteTimeout` behavior.
- Treat `readFirstChunk()` returning `io.EOF` with zero bytes as a retryable pre-first-byte failure.
- Do not write downstream `200` for that empty first-read case.
- Preserve existing failover behavior for transport errors and `429`/`5xx` responses.
- After the first chunk succeeds, bypass `streamIdleTimeout` only for `text/event-stream` responses.
- Keep non-SSE responses on the existing idle-timeout path.
- Add regression coverage for empty-200-SSE failover and long-idle SSE continuation.

## Non-Goals

1. No proxy refactor.
2. No new config flags.
3. No changes outside `internal/proxy/server.go` and `internal/proxy/server_test.go`.
