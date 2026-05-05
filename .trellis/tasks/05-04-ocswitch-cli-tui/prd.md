# BubbleTea TUI for CLI configuration

## Summary

`ocswitch` currently exposes configuration through Cobra subcommands and desktop/server web UI. This task adds a terminal UI powered by BubbleTea so running `ocswitch` with no subcommand opens an interactive configuration surface by default.

The TUI should make first-time setup and day-to-day config edits easier without removing existing CLI commands. Explicit commands such as `ocswitch provider list`, `ocswitch doctor`, `ocswitch opencode sync`, `ocswitch serve`, and `ocswitch server` must continue to behave exactly as they do today.

## Problem

New users must learn several command sequences before they can produce a usable OpenCode setup:

- add or import providers
- create aliases
- bind provider/model targets in failover order
- run doctor
- sync OpenCode config
- optionally start the local proxy

The command flow is correct but not discoverable enough. Desktop UI covers GUI users, but CLI-first users need an in-terminal guided flow that works well over SSH or local terminals and keeps all config changes explicit.

## Goals

- Launch TUI when `ocswitch` is executed with no subcommand and stdin/stdout are interactive terminals.
- Use BubbleTea as the TUI runtime.
- Keep existing Cobra command tree and flags stable for all explicit subcommands.
- Reuse existing config and app service logic instead of duplicating persistence behavior.
- Support core configuration workflows from terminal:
  - overview/status
  - provider list/add/edit/enable/disable/remove/import from OpenCode
  - alias list/add/edit/remove
  - bind/unbind/reorder/enable/disable alias targets
  - doctor run and issue display
  - OpenCode sync preview/apply
  - proxy start/stop/status when feasible in the current process
- Preserve safe write semantics: show confirmations for destructive or external-file-changing actions.
- Add i18n-ready TUI copy with English and zh-CN strings.
- Keep tests focused on root command routing and TUI state/update logic, not brittle terminal snapshots.

## Non-Goals

1. Do not replace existing Cobra subcommands.
2. Do not add a separate `ocswitch tui` command or command-line flag for TUI launch.
3. Do not add a new persistent database or new config file format.
4. Do not redesign desktop UI or server web admin UI.
5. Do not implement a full text editor for raw JSON config.
6. Do not make TUI proxy process management daemonized in this task.
7. Do not contact upstream providers except when user explicitly chooses model discovery, provider ping, import, doctor, or sync flows that already imply I/O.

## User Experience

### Entry Behavior

- `ocswitch` with no subcommand opens the TUI only when stdin/stdout are interactive terminals.
- `ocswitch --config <path>` with no subcommand opens the TUI against that config path only when stdin/stdout are interactive terminals.
- `ocswitch` with no subcommand in non-interactive contexts prints concise help/setup guidance and exits without launching TUI.
- `ocswitch --help`, `ocswitch -h`, and `ocswitch --version` keep current help/version behavior.
- Any explicit subcommand keeps current behavior and output.

### Initial Screen

Show a dashboard with:

- config path
- provider count
- alias count
- available/routable alias summary
- proxy status and bind address
- primary next actions, especially when config is empty

Suggested top-level navigation:

- Overview
- Providers
- Aliases
- Doctor
- OpenCode Sync
- Proxy
- Help

### Provider Flow

Capabilities:

- list providers with enabled/disabled state, protocol, base URL, API key mask, model count
- add provider with fields matching existing provider add/service input:
  - id
  - display name
  - protocol
  - one or more base URLs
  - base URL strategy
  - API key
  - extra headers
  - disabled flag
  - skip model discovery flag
- edit provider while preserving omitted secrets unless user explicitly changes them
- enable/disable provider
- remove provider after confirmation
- import providers from OpenCode with overwrite option
- show model discovery warnings without hiding successful save results

### Alias Flow

Capabilities:

- list aliases with enabled/disabled state, protocol, target count, available target count
- add/edit alias metadata
- remove alias after confirmation
- bind target from existing provider/model catalog when available
- accept manual model input when catalog is missing/untrusted, following current validation semantics
- unbind target after confirmation
- enable/disable target
- reorder targets while preserving enabled state

### Doctor Flow

Capabilities:

- run existing doctor checks
- show OK/failing state clearly
- group issues by severity/code when practical
- show actionable hints from `DoctorIssue.ActionHint`
- allow returning to relevant provider/alias/sync screen manually; automatic deep links are optional

### OpenCode Sync Flow

Capabilities:

- preview sync using existing app service logic
- show target path, protocols, aliases, would-change state, doctor issues, and summary
- allow optional default model and small model selection from available aliases
- apply sync only after confirmation because it writes OpenCode config
- support dry-run preview before apply

### Proxy Flow

Capabilities:

- show current proxy status and bind address
- start proxy from TUI using existing service behavior when process lifetime is acceptable
- stop proxy started in current TUI session
- show startup errors and doctor/config validation failures
- do not promise background daemon behavior in this task

## i18n Requirements

TUI is UI and must be i18n-ready.

- Add a small Go-side TUI message catalog rather than hardcoding user-visible strings throughout components.
- Include English and zh-CN strings for all TUI labels, help text, confirmations, errors, and success messages introduced in this task.
- Default to English on first launch.
- Provide an in-TUI language setting that persists to local `ocswitch` config.
- Reuse `desktop.language` for persistence if it remains the smallest compatible config path; otherwise add the smallest general config field needed for CLI UI language.
- Do not reuse frontend JSON locale files directly unless doing so proves simpler and type-safe.

## Technical Approach

### Package Shape

Recommended new package:

- `internal/tui`

Recommended responsibilities:

- BubbleTea model/update/view code
- screen routing and form state
- TUI i18n catalog
- thin adapter over `app.Service`
- terminal-specific input validation and confirmation prompts

Keep CLI command wiring in `internal/cli/root.go` minimal:

- detect no subcommand after Cobra parsed persistent flags
- run TUI with resolved `configPath`
- keep command/subcommand code paths unchanged

### Dependency

Add BubbleTea dependency:

- `github.com/charmbracelet/bubbletea`

Optional Charmbracelet dependencies such as Bubbles/Lipgloss may be used only if they materially reduce code and keep implementation maintainable.

### Service Reuse

Prefer `app.Service` methods for operations already exposed to desktop/server UI:

- `GetOverview`
- `ListProviders`
- `UpsertProvider`
- `ImportProviders`
- `SetProviderDisabled`
- `RemoveProvider`
- `ListAliases`
- `UpsertAlias`
- `BindAliasTarget`
- `UnbindAliasTarget`
- `SetAliasTargetDisabled`
- `ReorderAliasTargets`
- `RemoveAlias`
- `RunDoctor`
- `PreviewOpenCodeSync`
- `ApplyOpenCodeSync`
- `GetProxyStatus`
- `StartProxy`
- `StopProxy`

If a service method is missing for a necessary CLI operation, add the smallest app-level method instead of writing config file mutations inside TUI screens.

### Root Command Routing

Implementation should avoid breaking Cobra behavior.

Likely route:

- keep root `Use`, help, version, and subcommands
- add a root `RunE` or equivalent no-subcommand handler that launches TUI
- ensure help/version still short-circuit before TUI
- ensure unknown subcommands still error as today
- ensure `--config` is honored before TUI starts
- detect non-interactive stdin/stdout and avoid launching TUI there

### Terminal Behavior

- Support narrow terminal widths with readable fallback layout.
- Avoid requiring mouse support.
- Use keyboard-first controls.
- Show consistent shortcuts, for example `q` quit, `esc` back, `?` help, `enter` select, `ctrl+c` quit.
- Avoid printing API keys back to terminal except masked values.

## Suggested Implementation Phases

### Phase 1: Foundation

- Add BubbleTea dependency.
- Add `internal/tui` package skeleton.
- Add root no-subcommand launch path.
- Add overview screen with config path, provider count, alias count, proxy status.
- Add tests that explicit subcommands/help/version do not launch TUI.

### Phase 2: Read-Only Navigation

- Add top-level navigation.
- Add provider list screen.
- Add alias list screen.
- Add doctor read/run screen.
- Add basic help screen.

### Phase 3: Provider Editing

- Add provider add/edit forms.
- Add provider enable/disable/remove confirmations.
- Add import from OpenCode flow.
- Surface warnings from model discovery/import.

### Phase 4: Alias Editing

- Add alias add/edit/remove forms.
- Add bind/unbind target flow.
- Add target enable/disable and reorder flow.

### Phase 5: Sync and Proxy Operations

- Add OpenCode sync preview/apply flow with confirmation.
- Add proxy status/start/stop flow scoped to current TUI process.
- Clarify limits in help text.

### Phase 6: Polish and Verification

- Add i18n catalog coverage for all new TUI strings.
- Add focused unit tests for state transitions, validation, root launch routing, and service adapter calls.
- Run Go tests.
- Manually smoke test TUI startup and core navigation in terminal.

## Acceptance Criteria

1. Running `ocswitch` with no subcommand in an interactive terminal opens BubbleTea TUI.
2. Running `ocswitch --config <path>` with no subcommand in an interactive terminal opens TUI using that config path.
3. `ocswitch --help`, `ocswitch --version`, and every explicit existing subcommand keep current behavior.
4. TUI overview shows config path, provider count, alias count, routable alias summary, and proxy status.
5. TUI can list, add/edit, enable/disable, remove, and import providers using existing validation/persistence semantics.
6. TUI can list, add/edit, remove aliases, bind/unbind targets, toggle targets, and reorder target priority without losing target enabled state.
7. TUI can run doctor and show issue severity, message, and action hints.
8. TUI can preview OpenCode sync and apply only after explicit confirmation.
9. TUI masks saved API keys and does not print full secrets in views or logs.
10. TUI user-visible strings are available in English and zh-CN through a Go-side message catalog.
11. TUI defaults to English and can change language inside the TUI; the selected language persists across restarts.
12. Running bare `ocswitch` in a non-interactive context does not launch TUI and does not hang waiting for key input.
13. Core TUI model/update behavior and root command launch routing have automated tests.
14. `go test ./...` passes.

## Risks and Mitigations

- Risk: root command TUI launch breaks scripts expecting help output from bare `ocswitch`.
  - Mitigation: preserve help/version flags and document behavior change; explicit commands remain stable.
- Risk: TUI duplicates CLI config mutation logic and drifts over time.
  - Mitigation: route writes through `app.Service`; add missing service methods only when needed.
- Risk: terminal snapshot tests become brittle.
  - Mitigation: test state/update logic and adapter calls; keep view tests minimal.
- Risk: proxy start from TUI may be mistaken for daemon mode.
  - Mitigation: label it as current-session process control only.
- Risk: i18n string sprawl.
  - Mitigation: centralize TUI strings in one catalog and fail tests on missing keys if practical.

## Open Questions for User Confirmation

Resolved decisions:

- Bare `ocswitch` enters TUI only in interactive terminals.
- No explicit `ocswitch tui` command.
- TUI defaults to English; users can change language inside TUI and the choice persists.
- MVP should ship core functionality first.

## Proposed MVP Cut

For first development pass, recommended MVP:

- bare `ocswitch` opens TUI in interactive terminals; non-interactive bare `ocswitch` prints concise help/setup guidance
- no explicit `ocswitch tui` command
- overview, provider list/add/edit, alias list/add/bind/reorder, doctor, sync preview/apply
- provider import and proxy start/stop included only if they stay small after implementation research

This gives immediate setup value while keeping first TUI release manageable.
