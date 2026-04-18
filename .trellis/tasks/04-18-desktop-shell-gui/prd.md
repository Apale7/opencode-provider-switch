# OCSWITCH Desktop Shell Design

## Summary

`ocswitch` is currently a Go CLI and local proxy. It already has the core routing, config persistence, and OpenCode sync logic needed for daily use, but it does not yet provide a desktop-native control surface.

This task defines a desktop-shell architecture for `ocswitch` with these explicit product requirements:

- launch at login
- native tray/menu integration
- native notifications
- a GUI control panel for configuration and runtime status

The key constraint is equally explicit:

- do not rewrite existing Go core logic into desktop-only code

Instead, this task introduces a shared application service layer so the CLI and desktop GUI can reuse the same workflows.

## Background And Why This Is A Separate Task

The completed task `04-17-forwarding-log-viewer-gui` correctly designed a same-process local web log viewer and explicitly excluded Electron or native desktop GUI.

That task remains valid for its original goal.

This task exists because product direction has changed. The GUI now needs real desktop capabilities:

- launch at login
- tray/menu presence
- notifications
- background-style operation

Those requirements justify a desktop shell and should not be retrofitted into the completed local-web-viewer task.

## Product Goal

Provide a desktop-native control surface for `ocswitch` that can:

1. manage providers, aliases, and sync operations through a GUI
2. control and observe local proxy runtime state
3. run comfortably as a background desktop utility
4. expose tray/menu and notification workflows expected from a resident local tool
5. preserve the existing Go codebase as the primary implementation of business rules

## Primary Requirements

### Desktop capabilities

- Launch at login must be supported.
- Native tray/menu integration must be supported.
- Native notifications must be supported.
- The application should be able to behave like a local resident utility, not just a browser page.

### Reuse requirements

- `internal/config` must continue to own config structures, loading, saving, and validation.
- `internal/proxy` must continue to own proxy runtime and failover behavior.
- `internal/opencode` must continue to own OpenCode config sync behavior.
- A new `internal/app` layer must own shared workflows used by both CLI and GUI.
- The desktop shell must not directly reimplement provider/alias/sync rules.

### GUI scope

The first GUI iteration should focus on a control-panel workflow rather than a full analytics product.

Minimum surface:

- overview/status page
- provider management page
- alias management page
- doctor and OpenCode sync page

## Non-Goals

1. No rewrite of config/proxy/opencode logic into desktop-specific modules.
2. No replacement of the existing CLI.
3. No hosted service or remote control plane.
4. No multi-user or authenticated remote administration model.
5. No forced migration away from existing config file formats.
6. No requirement that the initial desktop shell also ships the full request log viewer.

## Technology Recommendation

### Recommended stack

- Desktop shell: `Wails`
- Frontend: `React + TypeScript`
- Styling: `Tailwind CSS`
- Forms and validation: `react-hook-form + zod`

### Why Wails

`ocswitch` already centers on Go. The desktop shell should wrap that Go core rather than forcing a new primary runtime.

Wails is recommended because:

- it matches the existing Go-heavy codebase
- it avoids introducing Rust as another core runtime concern
- it supports the desktop shell product shape better than a browser-only UI
- it allows Go services to remain the real source of business behavior

### Why React over Vue

The GUI is primarily a configuration-heavy control panel.

React is recommended because:

- the form-heavy workflow aligns well with `react-hook-form + zod`
- long-term ecosystem support for control-panel style applications is strong
- it is a pragmatic default when the frontend is not the product center but must remain maintainable as scope grows

Vue is not rejected as incapable; it is simply not the preferred choice for this project's chosen form stack and expected long-term expansion.

## Current Project State

### Existing reusable layers

#### `internal/config`

Already provides:

- `Config`, `Provider`, `Alias`, `Target`, `Server` models
- config load/save
- default path resolution
- provider and alias mutation helpers
- structural validation

#### `internal/proxy`

Already provides:

- local HTTP server
- `/v1/responses`
- `/v1/models`
- deterministic alias failover behavior
- downstream streaming pass-through

#### `internal/opencode`

Already provides:

- OpenCode config load/save
- provider sync patching
- global target path resolution

#### `internal/cli`

Currently provides command entry points, but it also contains business orchestration that should move into a shared application layer.

Examples:

- provider add/update orchestration
- alias bind/unbind orchestration
- doctor orchestration
- OpenCode sync orchestration
- serve command bootstrap

## Proposed Architecture

### Layer model

Recommended layers:

1. `internal/config`
2. `internal/proxy`
3. `internal/opencode`
4. `internal/app`
5. `internal/cli`
6. `internal/desktop`
7. `web/`

### Responsibility boundaries

#### `internal/config`

Owns:

- persisted config structures
- file path resolution
- load/save with locking and atomic write behavior
- config-level validation helpers

Must not own:

- desktop shell behavior
- UI-specific DTOs
- tray/menu integration

#### `internal/proxy`

Owns:

- proxy server construction
- request routing
- upstream retry/failover semantics
- HTTP runtime behavior

Must not own:

- desktop lifecycle decisions
- UI-specific runtime state projection

#### `internal/opencode`

Owns:

- OpenCode config parsing and writing
- `provider.ocswitch` sync logic
- target resolution for the OpenCode config file

Must not own:

- GUI orchestration
- desktop notifications

#### `internal/app`

Owns:

- reusable business workflows
- shared DTOs for CLI and GUI
- orchestration across `config`, `proxy`, and `opencode`
- runtime supervision of the embedded proxy process in desktop mode

Must not own:

- Wails-specific bindings
- frontend rendering
- tray/menu implementation

#### `internal/cli`

Owns:

- cobra command tree
- flag parsing
- terminal printing

Must delegate business workflows to `internal/app`.

#### `internal/desktop`

Owns:

- Wails application bootstrap
- window lifecycle
- tray/menu wiring
- native notifications
- launch-at-login integration
- binding frontend calls to `internal/app`

Must not own:

- provider/alias mutation rules
- OpenCode sync rules
- direct config mutation logic beyond reading desktop preference values via services

#### `web/`

Owns:

- React pages and components
- visual state and form UX
- calling Wails-bound methods

Must not own:

- config file persistence rules
- routing/failover behavior

## Recommended Directory Shape

```text
cmd/
  ocswitch/
    main.go
  ocswitch-desktop/
    main.go

internal/
  config/
  proxy/
  opencode/
  app/
    service.go
    types.go
    config_service.go
    provider_service.go
    alias_service.go
    sync_service.go
    runtime_service.go
  cli/
  desktop/
    app.go
    tray.go
    menu.go
    notify.go
    autostart.go
    bindings.go

web/
  package.json
  src/
    pages/
    components/
    lib/
```

The exact filenames can vary, but the boundary itself should remain stable.

## `internal/app` Design

### Purpose

`internal/app` is the shared application service layer.

It exists to solve a current structural problem in the codebase: many workflows already exist, but they are embedded inside CLI command handlers. The desktop GUI should not duplicate those workflows, and the CLI should not remain the only caller.

`internal/app` should therefore become the only place where cross-package use cases are orchestrated.

### Design principles

1. Organize by user workflows, not by storage structs.
2. Return stable DTOs instead of exposing raw persistence objects directly to GUI code.
3. Keep validation and persistence in the existing packages where appropriate.
4. Keep the initial design small and explicit rather than overly abstract.

### Top-level service shape

Recommended root type:

```go
type Service struct {
    configPath string

    mu          sync.Mutex
    proxyCancel context.CancelFunc
    proxyDone   chan struct{}
    proxyStatus ProxyStatusView
}

func NewService(configPath string) *Service
```

The root service may either expose all methods directly or hold narrower sub-services internally. The important point is that callers should consume one coherent application boundary.

### Shared DTOs

Recommended DTOs are intentionally UI-safe and transport-safe.

```go
type Overview struct {
    ConfigPath        string            `json:"configPath"`
    ProviderCount     int               `json:"providerCount"`
    AliasCount        int               `json:"aliasCount"`
    AvailableAliases  []string          `json:"availableAliases"`
    Proxy             ProxyStatusView   `json:"proxy"`
    Desktop           DesktopPrefsView  `json:"desktop"`
}

type ProxyStatusView struct {
    Running     bool      `json:"running"`
    BindAddress string    `json:"bindAddress"`
    StartedAt   time.Time `json:"startedAt,omitempty"`
    LastError   string    `json:"lastError,omitempty"`
}

type ProviderView struct {
    ID           string            `json:"id"`
    Name         string            `json:"name,omitempty"`
    BaseURL      string            `json:"baseUrl"`
    APIKeySet    bool              `json:"apiKeySet"`
    Headers      map[string]string `json:"headers,omitempty"`
    Models       []string          `json:"models,omitempty"`
    ModelsSource string            `json:"modelsSource,omitempty"`
    Disabled     bool              `json:"disabled"`
}

type AliasTargetView struct {
    Provider string `json:"provider"`
    Model    string `json:"model"`
    Enabled  bool   `json:"enabled"`
}

type AliasView struct {
    Alias         string            `json:"alias"`
    DisplayName   string            `json:"displayName,omitempty"`
    Enabled       bool              `json:"enabled"`
    Targets       []AliasTargetView `json:"targets"`
    Available     bool              `json:"available"`
}

type DoctorIssue struct {
    Message string `json:"message"`
}

type DoctorReport struct {
    OK                  bool          `json:"ok"`
    Issues              []DoctorIssue `json:"issues"`
    ConfigPath          string        `json:"configPath"`
    ProviderCount       int           `json:"providerCount"`
    AliasCount          int           `json:"aliasCount"`
    ProxyBindAddress    string        `json:"proxyBindAddress"`
    OpenCodeTargetPath  string        `json:"openCodeTargetPath"`
    OpenCodeTargetFound bool          `json:"openCodeTargetFound"`
}

type SyncPreview struct {
    TargetPath      string   `json:"targetPath"`
    AliasNames      []string `json:"aliasNames"`
    SetModel        string   `json:"setModel,omitempty"`
    SetSmallModel   string   `json:"setSmallModel,omitempty"`
    WouldChange     bool     `json:"wouldChange"`
}

type SyncResult struct {
    TargetPath    string   `json:"targetPath"`
    AliasNames    []string `json:"aliasNames"`
    Changed       bool     `json:"changed"`
    DryRun        bool     `json:"dryRun"`
    SetModel      string   `json:"setModel,omitempty"`
    SetSmallModel string   `json:"setSmallModel,omitempty"`
}

type DesktopPrefsView struct {
    LaunchAtLogin bool `json:"launchAtLogin"`
    MinimizeToTray bool `json:"minimizeToTray"`
    Notifications bool `json:"notifications"`
}
```

### Input types

Recommended command inputs:

```go
type SaveProviderInput struct {
    ID           string            `json:"id"`
    Name         string            `json:"name,omitempty"`
    BaseURL      string            `json:"baseUrl"`
    APIKey       string            `json:"apiKey,omitempty"`
    Headers      map[string]string `json:"headers,omitempty"`
    ClearHeaders bool              `json:"clearHeaders"`
    Disabled     bool              `json:"disabled"`
    SkipModels   bool              `json:"skipModels"`
}

type SaveAliasInput struct {
    Alias       string `json:"alias"`
    DisplayName string `json:"displayName,omitempty"`
    Disabled    bool   `json:"disabled"`
}

type BindAliasTargetInput struct {
    Alias    string `json:"alias"`
    Provider string `json:"provider,omitempty"`
    Model    string `json:"model"`
    Disabled bool   `json:"disabled"`
}

type UnbindAliasTargetInput struct {
    Alias    string `json:"alias"`
    Provider string `json:"provider,omitempty"`
    Model    string `json:"model"`
}

type SyncInput struct {
    Target        string `json:"target,omitempty"`
    SetModel      string `json:"setModel,omitempty"`
    SetSmallModel string `json:"setSmallModel,omitempty"`
    DryRun        bool   `json:"dryRun"`
}

type DesktopPrefsInput struct {
    LaunchAtLogin bool `json:"launchAtLogin"`
    MinimizeToTray bool `json:"minimizeToTray"`
    Notifications bool `json:"notifications"`
}
```

### Required service methods

At minimum, `internal/app` should expose these use cases:

```go
func (s *Service) GetOverview(ctx context.Context) (Overview, error)

func (s *Service) ListProviders(ctx context.Context) ([]ProviderView, error)
func (s *Service) SaveProvider(ctx context.Context, in SaveProviderInput) (ProviderView, error)
func (s *Service) EnableProvider(ctx context.Context, id string) error
func (s *Service) DisableProvider(ctx context.Context, id string) error
func (s *Service) RemoveProvider(ctx context.Context, id string) error
func (s *Service) ImportProvidersFromOpenCode(ctx context.Context, from string, overwrite bool) ([]ProviderView, error)

func (s *Service) ListAliases(ctx context.Context) ([]AliasView, error)
func (s *Service) SaveAlias(ctx context.Context, in SaveAliasInput) (AliasView, error)
func (s *Service) RemoveAlias(ctx context.Context, alias string) error
func (s *Service) BindAliasTarget(ctx context.Context, in BindAliasTargetInput) error
func (s *Service) UnbindAliasTarget(ctx context.Context, in UnbindAliasTargetInput) error

func (s *Service) RunDoctor(ctx context.Context) (DoctorReport, error)
func (s *Service) PreviewOpenCodeSync(ctx context.Context, in SyncInput) (SyncPreview, error)
func (s *Service) ApplyOpenCodeSync(ctx context.Context, in SyncInput) (SyncResult, error)

func (s *Service) StartProxy(ctx context.Context) error
func (s *Service) StopProxy(ctx context.Context) error
func (s *Service) RestartProxy(ctx context.Context) error
func (s *Service) GetProxyStatus(ctx context.Context) (ProxyStatusView, error)

func (s *Service) GetDesktopPrefs(ctx context.Context) (DesktopPrefsView, error)
func (s *Service) SaveDesktopPrefs(ctx context.Context, in DesktopPrefsInput) (DesktopPrefsView, error)
```

### Method behavior notes

#### `SaveProvider`

Must preserve current provider semantics already implemented in CLI orchestration:

- update-or-insert behavior
- optional model discovery
- preservation of fields not explicitly changed
- stale discovered catalog downgrade to untrusted metadata when needed

The GUI must not reimplement any of this itself.

#### `BindAliasTarget`

Must preserve current CLI semantics:

- allow combined `provider/model` parsing when provider is omitted
- validate provider existence
- validate known model when model catalog is trusted
- auto-create alias when current workflow requires it

#### `RunDoctor`

Should centralize the same checks currently wired in the CLI command:

- config validation
- OpenCode target resolution
- provider.ocswitch preview validation

#### Runtime methods

Must make desktop mode safe:

- no duplicate proxy start
- clean shutdown
- observable runtime state
- clear last-error reporting for the GUI and tray status

### Runtime supervision model

Desktop mode requires the proxy to be managed as an internal long-running component.

Recommended runtime model:

```text
desktop shell starts
  -> app.Service constructed
  -> user or preference requests proxy start
  -> app.Service loads config and validates it
  -> app.Service creates proxy.Server
  -> app.Service starts ListenAndServe under managed context
  -> proxy status is updated for GUI/tray visibility
```

Important implementation requirements:

- repeated start requests must be idempotent or return a clear error
- stop must call graceful shutdown through context cancellation
- runtime state must survive UI refreshes within the same process

## Desktop Shell Design

### Recommended `internal/desktop` responsibilities

#### `app.go`

- bootstrap Wails
- construct `internal/app.Service`
- wire lifecycle hooks

#### `bindings.go`

- expose thin Wails-callable methods
- forward all real work to `internal/app`

#### `tray.go`

- tray icon
- quick actions such as show window, start proxy, stop proxy, sync, quit
- tray status updates based on `app.Service` runtime state

#### `menu.go`

- native app menu entries if needed per platform

#### `notify.go`

- success and error notifications for:
  - proxy start/stop
  - config save failures
  - sync results

#### `autostart.go`

- platform-specific launch-at-login registration
- read desired state from desktop preferences exposed through `internal/app`

## Configuration Extensions

Desktop behavior may require a small extension to the persisted config.

Recommended addition:

```go
type Desktop struct {
    LaunchAtLogin  bool `json:"launch_at_login,omitempty"`
    MinimizeToTray bool `json:"minimize_to_tray,omitempty"`
    Notifications  bool `json:"notifications,omitempty"`
}
```

Then extend `config.Config` with:

```go
Desktop Desktop `json:"desktop,omitempty"`
```

This should remain a lightweight preference store, not a place to encode platform-specific shell details.

## CLI Migration Plan

The CLI should remain as a first-class interface.

Recommended migration pattern:

1. move workflow orchestration from `internal/cli/*.go` into `internal/app`
2. keep cobra commands as thin argument adapters
3. update CLI output formatting without changing business semantics

This task is successful only if CLI and GUI both use the same business paths.

## GUI Surface Recommendation

### Initial pages

1. Overview
2. Providers
3. Aliases
4. Doctor and OpenCode Sync
5. Settings

### Overview page should show

- current config path
- proxy runtime status
- bind address
- provider count
- alias count
- last doctor result summary if available

### Provider page should support

- add provider
- edit provider
- enable/disable provider
- remove provider
- show discovered model metadata summary

### Alias page should support

- add/edit alias
- remove alias
- bind/unbind provider targets
- show failover order clearly

### Doctor and Sync page should support

- run doctor
- preview sync target
- apply sync
- show resolved OpenCode target path

### Settings page should support

- launch at login
- minimize to tray
- notifications preference
- optional auto-start proxy preference in a later iteration

## Acceptance Criteria

This design task is complete when the PRD clearly defines:

1. the desktop product shape and why it is separate from the earlier local-web-viewer task
2. the recommended technology stack
3. the layer boundaries between `config`, `proxy`, `opencode`, `app`, `cli`, `desktop`, and `web`
4. the required `internal/app` service methods and DTO shapes
5. the runtime supervision model for the embedded proxy
6. the desktop-only responsibilities for Wails shell code
7. the first GUI page set and minimum feature scope

## Recommended Next Implementation Task

After this design is accepted, the next implementation task should focus on skeleton wiring only:

1. create `internal/app` with one or two end-to-end migrated workflows
2. create Wails shell bootstrap
3. wire a minimal React frontend
4. expose overview plus provider list through the new shared layer

That first implementation task should avoid trying to deliver every page and every shell integration at once.
