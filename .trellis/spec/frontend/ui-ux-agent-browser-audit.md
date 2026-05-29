# UI/UX Agent Browser Audit Spec

Use this workflow when auditing the local desktop/web UI with `agent-browser` or a CDP-connected Chrome instance.

## Goals

- Verify every user-facing page and major interaction path.
- Test zoom-sensitive layouts from 75% through 200%.
- Use isolated temporary config data so real provider keys and user config are not modified.
- Produce a checklist-style remediation plan with verifiable acceptance criteria.

## Start the Local UI

Prefer the desktop fallback web control panel because it serves the same GUI pages without requiring server-mode admin auth.

```powershell
go run ./cmd/ocswitch-desktop \
  --config "C:\Users\ADMINI~1\AppData\Local\Temp\opencode\ocswitch-uiux-test.json" \
  --listen "127.0.0.1:9987" \
  --no-open
```

Rules:

- Use a temp config path.
- Do not use real upstream API keys.
- Use fake providers and aliases for UI state coverage.
- Do not run destructive actions against real OpenCode config unless the target path is explicitly changed to a temp file.

## Browser Connection

First try direct `agent-browser` launch:

```powershell
agent-browser --session uiux batch "set viewport 1440 900" "open http://127.0.0.1:9987" "snapshot -i --urls"
```

If Chrome opens a blank/home page and `agent-browser` reports `DevToolsActivePort` errors, start Chrome manually with CDP and connect to it:

```powershell
$profile = "C:\Users\ADMINI~1\AppData\Local\Temp\opencode\chrome-uiux-profile"
Start-Process -FilePath "C:\Program Files\Google\Chrome\Application\chrome.exe" -ArgumentList @(
  "--remote-debugging-port=9222",
  "--user-data-dir=$profile",
  "--no-first-run",
  "--no-default-browser-check",
  "--new-window",
  "http://127.0.0.1:9987"
)
agent-browser --session uiux connect 9222
agent-browser --session uiux snapshot -i --urls
```

## Required Page Coverage

Audit these pages:

- Overview
- Providers
- Aliases
- Log
- Network
- Health
- Sync
- Settings

For each page, capture:

- interactive snapshot: `agent-browser --session uiux snapshot -i --urls`
- at least one visual screenshot when layout is suspicious: `agent-browser --session uiux screenshot --full`
- text state after critical actions: `agent-browser --session uiux get text body`
- console/errors if behavior looks stuck: `agent-browser --session uiux console` and `agent-browser --session uiux errors`

## Required Interaction Coverage

Minimum interactions to test:

- Overview: refresh, Doctor, start/stop proxy.
- Providers: create provider, save provider, search, status filter, edit drawer, import entry, delete confirmation entry.
- Provider form: base URL row, API key section, skip model discovery, action visibility.
- Aliases: create alias, bind target, target list display.
- Log: time presets, custom time controls, pagination empty state.
- Network: time presets, Open Health navigation, pagination empty state.
- Health: refresh, provider/status/failover filters, table/card readability.
- Sync: preview with safe target data; do not apply to real config by default.
- Settings: theme switch, language switch, desktop prefs, config import/export entry, proxy settings save entry.

## Safe Synthetic Data

Create only fake data, for example:

- provider id: `uiux-provider`
- provider display name: `UIUX Provider`
- base URL: `https://example.com/v1`
- API key: `sk-uiux-test-key`
- skip `/v1/models` discovery: enabled
- alias: `uiux-alias`
- model: `gpt-uiux`

This gives Providers, Aliases, Health, Sync, and Overview enough state without hitting real upstream APIs.

## Zoom Testing

Chrome Ctrl+plus/minus may not change page zoom under CDP automation. Use equivalent CSS viewport sizes for a fixed 1440x900 physical window:

| Zoom | Viewport |
|------|----------|
| 75%  | 1920x1200 |
| 100% | 1440x900 |
| 125% | 1152x720 |
| 150% | 960x600 |
| 200% | 720x450 |

Command pattern:

```powershell
agent-browser --session uiux set viewport 720 450
agent-browser --session uiux open http://127.0.0.1:9987/#settings
agent-browser --session uiux wait 500
agent-browser --session uiux screenshot --full
```

Check each zoom level for:

- horizontal overflow
- hidden primary buttons
- popovers clipped by viewport edges
- tables that require undiscoverable horizontal scroll
- fixed sidebar squeezing content
- long settings/sync pages with unreachable actions

## DOM Diagnostics

Use one-line `eval` expressions for layout diagnostics. `eval --stdin` may return `null` in some environments, so prefer a direct expression when possible.

Example checks:

```powershell
agent-browser --session uiux eval 'JSON.stringify({iw: window.innerWidth, ih: window.innerHeight, dpr: window.devicePixelRatio, sw: document.documentElement.scrollWidth})'
```

For overflow, inspect elements whose bounding rect exceeds `document.documentElement.clientWidth`.

## Screenshot Path Safety

Prefer default screenshot output:

```powershell
agent-browser --session uiux screenshot --full
```

Avoid passing Windows paths with backslashes to `--screenshot-dir` unless verified. A malformed path can create a repository-root file such as `UsersADMINI~1AppDataLocalTempopencode`.

If such a file appears, it is a stray screenshot artifact and can be removed.

## Cleanup

After audit:

```powershell
agent-browser close --all
```

Then stop local ports if still active:

```powershell
$ports = @(9987, 9222)
foreach ($port in $ports) {
  Get-NetTCPConnection -LocalPort $port -ErrorAction SilentlyContinue |
    ForEach-Object { Stop-Process -Id $_.OwningProcess -Force -ErrorAction SilentlyContinue }
}
```

## Expected Output

Produce a local checklist file with:

- issue title
- priority (`P0`, `P1`, `P2`)
- checkbox state (`- [ ]`)
- acceptance criteria
- verification zoom levels

Recommended output location for ad-hoc audit notes:

```text
.trellis/workspace/OpenCode/ui-ux-optimization-todo.md
```
