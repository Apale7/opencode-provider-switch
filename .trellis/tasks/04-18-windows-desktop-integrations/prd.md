# Windows Desktop Integrations

## Summary

`ocswitch desktop` already has shared Go application services, a React control panel, a browser fallback shell, and a Wails window wrapper. What is still missing is the set of desktop-native integrations that make the Windows build behave like a real resident utility instead of only a packaged webview.

This task completes that gap with a minimal extension to the current architecture:

- fix the Wails frontend bridge so the GUI actually talks to Go bindings
- add native tray integration for the Wails desktop build
- add native notification delivery for desktop events
- add launch-at-login support for Windows
- preserve the existing browser fallback shell and shared service layer

## Product Goal

Provide a Windows-friendly desktop shell for `ocswitch` that can stay resident, expose quick tray controls, surface important events through native notifications, and reopen automatically at login without rewriting existing business logic.

## Current State

### Already done

- `internal/app` owns shared workflows for overview, proxy control, doctor, desktop preferences, and OpenCode sync.
- `internal/desktop/http.go` already provides a browser fallback shell with the same workflows.
- `frontend/src/App.tsx` already renders the main control panel for overview, desktop prefs, sync, doctor, providers, and aliases.
- `internal/desktop/wails.go` already boots a Wails window and hides on close.

### Missing

- frontend Wails bridge still points at `window.go.main.App` even though generated bindings live at `window.go.desktop.App`
- tray support is only a placeholder and does not expose resident controls
- notifications are only a placeholder and do not use Wails runtime APIs
- launch-at-login only supports Linux XDG autostart today

## Requirements

### Desktop integration

- Wails desktop build must expose a real system tray on Windows.
- Tray menu must at least support opening the window, hiding the window, starting the proxy, stopping the proxy, and quitting the app.
- Close-to-background behavior must continue to respect `minimizeToTray` preference.
- Native notifications must be available when `notifications` preference is enabled.
- Native launch-at-login must be available on Windows through a practical low-complexity mechanism.

### Reuse and layering

- Keep `internal/app` as the owner of shared workflows and state transitions.
- Keep `internal/desktop` focused on shell integration only.
- Do not duplicate proxy, config, or sync business rules inside tray or frontend code.
- Browser fallback shell must remain operational.

## Design Decisions

### Tray implementation

Wails v2 does not provide a stable public tray API, so tray support should use `github.com/getlantern/systray` inside the `desktop_wails` build.

Why this path:

- lowest implementation cost in current Go codebase
- already present in module graph
- works alongside Wails window lifecycle
- avoids replacing the desktop shell framework

### Notification implementation

Use Wails runtime notification APIs for the desktop build. This keeps notifications native and avoids introducing another platform adapter.

Expected event coverage:

- proxy started
- proxy stopped
- OpenCode sync applied
- notifications preference enabled

### Windows launch-at-login

Implement Windows startup integration by writing a `.cmd` launcher into the user Startup folder.

Why this path:

- much simpler than generating `.lnk`
- good enough for a local utility
- easy to inspect and delete

## Non-Goals

1. No migration from Wails to Fyne unless current approach proves unworkable.
2. No tray support requirement for browser fallback mode.
3. No redesign of the React control panel information architecture.
4. No expansion into advanced desktop telemetry or background sync daemons.

## Acceptance Criteria

1. Wails frontend can successfully call Go bindings through generated `desktop` namespace.
2. Windows desktop build can be hidden and restored through tray controls.
3. Tray can start and stop proxy without reopening main window.
4. Enabling notifications allows native desktop alerts for key lifecycle events.
5. Enabling launch at login creates Windows startup entry; disabling removes it.
6. Existing browser fallback flow and current tests remain green.
