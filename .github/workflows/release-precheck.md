# Release Workflow Precheck

1. Confirm release-please is enabled on `master`.
2. Confirm conventional commits are used for release notes grouping.
3. Confirm `release-please-config.json` targets the repo root and `CHANGELOG.md`.
4. Confirm a Chinese release note file exists at `.github/release-notes/vX.Y.Z.zh-CN.md` before merging the release PR.
5. Confirm Wails output name matches `wails.json`:
   - `build/bin/ocswitch-desktop.exe` on Windows
   - `build/bin/ocswitch-desktop.app` on macOS
   - `build/bin/ocswitch-desktop` on Linux
6. Confirm Linux runner has the GTK/WebKit packages needed by Wails.
7. Confirm `npm install` is sufficient for frontend build in CI.
8. Confirm release assets are built from the released tag and uploaded in the same workflow run.
9. Confirm each asset has a matching `.sha256` file.
10. Confirm the generated release contains the Windows `.exe`, the Windows archive, and the Linux archive.
11. Confirm tag format is `v*`.
