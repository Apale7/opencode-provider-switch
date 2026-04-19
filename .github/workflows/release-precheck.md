# Release Workflow Precheck

1. Confirm release-please is enabled on `master`.
2. Confirm conventional commits are used for release notes grouping.
3. Confirm `release-please-config.json` targets the repo root and `CHANGELOG.md`.
4. Confirm Wails output name matches `wails.json`:
   - `build/bin/ocswitch-desktop.exe` on Windows
   - `build/bin/ocswitch-desktop.app` on macOS
   - `build/bin/ocswitch-desktop` on Linux
5. Confirm Linux runner has the GTK/WebKit packages needed by Wails.
6. Confirm `npm install` is sufficient for frontend build in CI.
7. Confirm release assets are uploaded only after the GitHub Release is published.
8. Confirm each asset has a matching `.sha256` file.
9. Confirm tag format is `v*`.
10. Confirm the generated release contains the three platform archives.
