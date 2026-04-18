# Journal - Apale (Part 1)

> AI development session journal
> Started: 2026-04-16

---



## Session 1: Finalize OPS MVP review PRD

**Date**: 2026-04-17
**Task**: Finalize OPS MVP review PRD
**Branch**: `master`

### Summary

(Add summary)

### Main Changes

| Area | Description |
|------|-------------|
| PRD Redesign | Reframed OPS MVP around alias-driven multi-provider failover for OpenAI Responses only. |
| OpenCode Integration | Locked config-sync approach for exposing aliases through `provider.ops.models`, with connected-provider requirement. |
| Failover Semantics | Finalized pre-first-byte-only failover, streaming pass-through, and no mid-stream provider switching. |
| MVP Boundaries | Locked import scope to config-defined `@ai-sdk/openai`, excluded `auth.json` as provider definition source, and kept `ops doctor` static by default. |
| Task Closure | Marked review task completed, cleared current task, and archived both finished docs tasks. |

**Updated Files**:
- `.trellis/tasks/archive/2026-04/04-17-04-17-ops-mvp-design-review/prd.md`
- `.trellis/tasks/archive/2026-04/04-17-04-17-ops-mvp-design-review/task.json`
- `.trellis/tasks/archive/2026-04/04-17-ops-mvp-design/task.json`

**Notes**:
- Follow-up implementation should start in a new task from the finalized PRD.
- Existing unrelated worktree change in `AGENTS.md` was left untouched.


### Git Commits

| Hash | Message |
|------|---------|
| `eeacdc4` | (see git log) |
| `00747fa` | (see git log) |
| `ca0b3c4` | (see git log) |

### Testing

- [OK] (Add test results)

### Status

[OK] **Completed**

### Next Steps

- None - task complete


## Session 2: Build OPS MVP failover proxy

**Date**: 2026-04-17
**Task**: Build OPS MVP failover proxy
**Branch**: `master`

### Summary

Implemented the Go-based OPS MVP: local config, provider and alias CLI, OpenCode sync/import, and a streaming OpenAI Responses proxy with deterministic pre-first-byte failover. Added Chinese and English README documentation for quick onboarding.

### Main Changes

(Add details)

### Git Commits

| Hash | Message |
|------|---------|
| `2b04d91` | (see git log) |

### Testing

- [OK] (Add test results)

### Status

[OK] **Completed**

### Next Steps

- None - task complete


## Session 3: Support disabling providers in ocswitch

**Date**: 2026-04-17
**Task**: Support disabling providers in ocswitch
**Branch**: `master`

### Summary

Added provider-level disable/enable support, made failover skip disabled providers without mutating alias target state, and aligned doctor/opencode sync/models exposure with routable aliases.

### Main Changes

(Add details)

### Git Commits

| Hash | Message |
|------|---------|
| `HEAD` | (see git log) |

### Testing

- [OK] (Add test results)

### Status

[OK] **Completed**

### Next Steps

- None - task complete


## Session 4: Review MVP completion status

**Date**: 2026-04-17
**Task**: Review MVP completion status
**Branch**: `master`

### Summary

Reviewed current implementation against the archived MVP PRD and classified implemented, missing, and beyond-MVP features.

### Main Changes

| Category | Result |
|---|---|
| Overall status | MVP core flow is effectively complete |
| Implemented MVP | Local ocswitch config, provider/alias CLI, OpenCode sync, static doctor, `/v1/responses` proxy, streaming pass-through, pre-first-byte failover, debug headers |
| Partial / missing MVP edges | `doctor` validates generated preview more than on-disk synced state; OpenCode provider import only reads `options.apiKey`, not broader header-style auth |
| Beyond MVP | `provider enable/disable`, minimal `/v1/models`, broader alias normalization, careful OpenCode config patching |

**Reviewed Sources**:
- `.trellis/tasks/archive/2026-04/04-17-04-17-ops-mvp-design-review/prd.md`
- `internal/config/config.go`
- `internal/cli/provider.go`
- `internal/cli/alias.go`
- `internal/cli/opencode.go`
- `internal/cli/doctor.go`
- `internal/cli/serve.go`
- `internal/opencode/opencode.go`
- `internal/proxy/server.go`
- `internal/proxy/server_test.go`
- `internal/opencode/opencode_test.go`
- `internal/cli/cli_test.go`
- `internal/config/config_test.go`

**Verification**:
- Ran `go test ./...` successfully: 34 tests passed across 5 packages.


### Git Commits

(No commits - planning session)

### Testing

- [OK] (Add test results)

### Status

[OK] **Completed**

### Next Steps

- None - task complete


## Session 5: Fix sync command panic

**Date**: 2026-04-17
**Task**: Fix sync command panic
**Branch**: `master`

### Summary

Fixed a panic in opencode sync when preserved model metadata contains slices, and added package plus CLI regression coverage.

### Main Changes

- Root cause: `internal/opencode/opencode.go` compared `interface{}` values directly inside `mapsEqualShallow`, which panicked when preserved `provider.ocswitch.models.<alias>` metadata included slices.
- Fix: replaced the unsafe hand-rolled comparison with `reflect.DeepEqual` so existing model metadata with nested maps/slices can be compared safely during `sync`.
- Added regression tests in `internal/opencode/opencode_test.go` and `internal/cli/cli_test.go` covering preserved slice metadata and a real `opencode sync --target` no-op path.
- Verification: ran `rtk go test ./internal/opencode`, `rtk go test ./internal/cli ./internal/opencode`, and `rtk go test ./...` successfully.


### Git Commits

| Hash | Message |
|------|---------|
| `887eb14` | (see git log) |

### Testing

- [OK] (Add test results)

### Status

[OK] **Completed**

### Next Steps

- None - task complete


## Session 6: Rename CLI to ocswitch

**Date**: 2026-04-17
**Task**: Rename CLI to ocswitch
**Branch**: `master`

### Summary

Renamed the CLI and synced docs, examples, and Trellis history from olpx/opswitch to ocswitch while keeping the repository name opencode-provider-switch.

### Main Changes

(Add details)

### Git Commits

| Hash | Message |
|------|---------|
| `cf3dcec` | (see git log) |

### Testing

- [OK] (Add test results)

### Status

[OK] **Completed**

### Next Steps

- None - task complete


## Session 7: Deep code review for ocswitch

**Date**: 2026-04-17
**Task**: Deep code review for ocswitch
**Branch**: `master`

### Summary

Recorded the deep code review findings in Trellis, including 2 high-risk, 4 medium-risk, and 4 low-risk/improvement items with concrete remediation guidance.

### Main Changes

- Added `.trellis/tasks/04-17-deep-code-review-ocswitch/review.md` to capture the full deep review output and concrete remediation guidance.
- Recorded 2 high-risk items:
  - concurrent config/OpenCode save paths are unsafe under concurrent writers
  - proxy request body has no read timeout after headers
- Recorded 4 medium-risk items:
  - streaming responses have no idle timeout
  - retryable upstream failures are collapsed into a generic `502`
  - `opencode sync --set-model` / `--set-small-model` can silently write invalid defaults
  - fixed default API key remains unsafe when binding to non-loopback addresses
- Recorded 4 low-risk or improvement items:
  - alias lifecycle lacks enable/disable recovery path
  - provider import docs and implementation disagree on empty `apiKey`
  - `opencode sync` side effects on JSONC comments and `$schema` need clearer docs
  - header forwarding should better handle dynamic hop-by-hop headers and narrower forwarding rules
- Verification already completed during review:
  - `go test ./...`
  - `go test -race ./...`


### Git Commits

(No commits - planning session)

### Testing

- [OK] (Add test results)

### Status

[OK] **Completed**

### Next Steps

- None - task complete


## Session 8: Provider discovery and model ref hardening

**Date**: 2026-04-18
**Task**: Provider discovery and model ref hardening
**Branch**: `master`

### Summary

Completed provider model discovery hardening, provider/model parsing improvements, README/help sync, follow-up review fixes, and final test-name cleanup.

### Main Changes

(Add details)

### Git Commits

| Hash | Message |
|------|---------|
| `09d0c1f` | (see git log) |

### Testing

- [OK] (Add test results)

### Status

[OK] **Completed**

### Next Steps

- None - task complete


## Session 9: GUI desktop direction and architecture decision

**Date**: 2026-04-18
**Task**: forwarding-log-viewer-gui
**Branch**: `master`

### Summary

Recorded the GUI direction change from a local web-only surface to a desktop-shell follow-up, driven by explicit requirements for launch at login, native menu/tray integration, and native notifications.

### Main Changes

- Confirmed the existing Trellis task `04-17-forwarding-log-viewer-gui` remains historically correct as a completed local web log-viewer design task and should not be retrofitted in place.
- Recorded follow-up product guidance that desktop capabilities now justify a separate native-shell track rather than a browser-only UI.
- Captured the recommended follow-up stack as `Wails + React + TypeScript + Tailwind CSS + react-hook-form + zod`.
- Captured the preferred layering so existing Go logic is not rewritten into desktop-specific code:
  - `internal/config` continues config IO and validation
  - `internal/proxy` continues proxy runtime and failover
  - `internal/opencode` continues OpenCode sync
  - new `internal/app` application service layer is shared by CLI and GUI
  - Wails desktop shell owns window, tray/menu, notifications, autostart, and frontend hosting only
- Recorded the frontend recommendation that React is a better fit than Vue for this project because the desktop control panel is form-heavy and aligns better with the chosen `react-hook-form + zod` stack.

### Git Commits

(No commits - Trellis documentation update only)

### Testing

- [OK] No code changes; updated Trellis task metadata and workspace journal only

### Status

[OK] **Completed**

### Next Steps

- Create a separate Trellis task for the desktop shell if implementation should proceed


## Session 10: Desktop shell skeleton implementation

**Date**: 2026-04-18
**Task**: Desktop shell skeleton implementation
**Branch**: `master`

### Summary

Implemented the first desktop-shell skeleton by introducing a shared internal/app layer, desktop preference persistence, CLI reuse of shared workflows, and a minimal desktop bootstrap with passing Go tests/builds.

### Main Changes

- Added `config.Desktop` to persist desktop-shell preferences (`launch_at_login`, `minimize_to_tray`, `notifications`) without coupling core runtime logic to a desktop framework.
- Added `internal/app/types.go` and `internal/app/service.go` as the shared application-service layer described in the PRD.
- Implemented shared DTOs and workflows for:
  - overview
  - provider listing
  - doctor report generation
  - OpenCode sync preview/apply plumbing
  - proxy lifecycle management
  - desktop preference read/write
- Refactored CLI reuse points so `doctor`, `opencode sync`, `provider list`, and `serve` now delegate through `internal/app` instead of keeping those orchestration paths CLI-only.
- Preserved CLI output behavior closely while centralizing the orchestration logic that future desktop bindings can call.
- Added `internal/desktop/` skeleton adapters (`app`, `bindings`, `tray`, `notify`, `autostart`) as placeholders for a future Wails/native shell integration, keeping desktop-only concerns outside `internal/app`.
- Added `cmd/ocswitch-desktop/main.go` as a minimal desktop bootstrap binary that exercises the shared bindings and confirms the skeleton is wired.
- Added `internal/app/service_test.go` to cover desktop preference persistence and proxy start/stop status flow.
- Verified the implementation with:
  - `gofmt -w ...`
  - `rtk go test ./...`
  - `rtk go build ./...`
- Scope intentionally stayed at the PRD's recommended “skeleton wiring only” level: no Wails dependency, no React frontend, and no tray/autostart implementation yet.


### Git Commits

(No commits - planning session)

### Testing

- [OK] (Add test results)

### Status

[OK] **Completed**

### Next Steps

- None - task complete


## Session 11: Desktop control panel implementation

**Date**: 2026-04-18
**Task**: Desktop control panel implementation
**Branch**: `master`

### Summary

Extended the desktop-shell skeleton into a minimal usable local control panel by adding an embedded web UI, desktop HTTP bindings, alias/provider overview surfaces, proxy controls, desktop preference editing, and OpenCode sync/doctor actions while keeping the shared Go application layer intact.

### Main Changes

- Upgraded `cmd/ocswitch-desktop/main.go` from a skeleton print-only bootstrap into a runnable local desktop control panel entrypoint with `--config`, `--listen`, and `--no-open` flags.
- Added `internal/desktop/http.go` as a lightweight desktop shell runtime that:
  - serves embedded frontend assets
  - exposes JSON endpoints for overview, providers, aliases, proxy status/start/stop, desktop prefs, doctor, and OpenCode sync preview/apply
  - opens the browser by default and shuts down cleanly on SIGINT/SIGTERM
- Added `web/` embedded assets (`web/assets.go`, `web/dist/index.html`, `web/dist/app.css`, `web/dist/app.js`) to provide a minimal control-panel UI without introducing Wails/WebKit runtime dependencies into the current repo.
- Expanded `internal/app`/`internal/desktop` bindings with alias DTOs and alias listing so the GUI can show both routable provider state and alias routing state.
- Added `internal/desktop/http_test.go` to verify the desktop control panel serves the app shell, returns overview JSON, and persists desktop preferences through the HTTP API.
- Verified with:
  - `gofmt -w cmd/ocswitch-desktop/main.go internal/app/service.go internal/app/types.go internal/desktop/bindings.go internal/desktop/http.go internal/desktop/http_test.go web/assets.go`
  - `rtk go test ./...`
  - `rtk go build ./...`
  - `go run ./cmd/ocswitch-desktop --no-open` followed by live HTTP checks against `/` and `/api/overview`
- Constraint note: the PRD still recommends Wails as the long-term native shell, but the current environment lacks `wails` CLI and desktop system dependencies, so this implementation deliberately ships a dependency-light local control panel first while keeping `internal/app` and `internal/desktop` ready for a future Wails swap-in.


### Git Commits

(No commits - planning session)

### Testing

- [OK] (Add test results)

### Status

[OK] **Completed**

### Next Steps

- None - task complete


## Session 12: Wails desktop shell integration

**Date**: 2026-04-18
**Task**: Wails desktop shell integration
**Branch**: `master`

### Summary

Migrated the desktop control panel onto a formal Wails + React/TypeScript structure while preserving a browser fallback shell, added real Linux XDG launch-at-login support, and verified both default and desktop_wails Go paths. Native desktop compilation now fails only on missing Linux system packages rather than repository structure issues.

### Main Changes

- Added Wails v2.12.0 to `go.mod` and introduced `wails.json` so the repository has a formal desktop-shell project structure.
- Split desktop entrypoints by build tag:
  - `cmd/ocswitch-desktop/main_fallback.go` keeps the browser fallback shell for default builds.
  - `cmd/ocswitch-desktop/main_wails.go` and root `main_wails.go` provide the real `desktop_wails` Wails entry path expected by the Wails CLI.
- Added `internal/desktop/wails.go` to centralize Wails startup configuration, bind the desktop app object, and serve `frontend/dist` assets from the same source used by the browser fallback shell.
- Expanded `internal/desktop/app.go` into a real desktop composition root with startup, shutdown, close-to-background, preference sync, metadata, and Wails-callable facade methods.
- Replaced the ad-hoc `web/` assets with a formal `frontend/` Vite + React + TypeScript app:
  - `frontend/src/App.tsx` now owns the GUI.
  - `frontend/src/api.ts` bridges both Wails-bound calls and fallback HTTP `/api/*` calls.
  - `frontend/src/types.ts` mirrors the Go DTOs.
  - `frontend/src/env.d.ts` declares the Wails bridge surface.
  - `frontend/src/styles.css` carries forward the control-panel styling.
- Updated the fallback HTTP shell in `internal/desktop/http.go` to serve `frontendassets.DistFS()` and to align doctor/meta responses with the Wails bridge semantics.
- Implemented real Linux XDG launch-at-login handling in `internal/desktop/autostart.go` and added `internal/desktop/autostart_test.go` to cover write/remove behavior.
- Wired `MinimizeToTray` into close-to-background behavior through `internal/desktop/tray.go` using public Wails window lifecycle APIs only; intentionally did not depend on private Wails tray internals because the pinned Wails version does not expose a stable public tray API.
- Added tag-specific runtime helpers:
  - `internal/desktop/runtime_wails.go` hides the Wails window.
  - `internal/desktop/runtime_fallback.go` is a no-op for browser fallback mode.
- Removed the obsolete `web/` embedded assets directory after the frontend migration because all code now serves assets from `frontend/`.
- Verified with:
  - `rtk npm install` (frontend dependencies)
  - `rtk npm run build`
  - `rtk go test ./...`
  - `rtk go test -tags desktop_wails ./...`
  - `wails build -tags desktop_wails -debug`
- Verification outcome:
  - repository structure, bindings generation, frontend build, and tagged Go compilation all succeed
  - `wails build` now reaches native Linux compilation and stops only at missing system packages (`pkg-config`, and likely GTK/WebKit dev packages)
  - native tray menus and native notifications remain intentionally incomplete because the current pinned Wails release does not provide a stable public tray API and no notification backend has been integrated yet


### Git Commits

(No commits - planning session)

### Testing

- [OK] (Add test results)

### Status

[OK] **Completed**

### Next Steps

- None - task complete
