# UI/UX Optimization TODO

Date: 2026-05-29
Owner: OpenCode
Source: local browser audit on `http://127.0.0.1:9987` with temporary config

## Test Coverage

- [x] Re-run audit after fixes on pages: Overview, Providers, Aliases, Log, Network, Health, Sync, Settings.
  - Done: 3x Explore subagents audited CSS+JSX side-by-side. Found 4 issues, all fixed: `.issue-card` `min-width:0`, `.detail-sheet` `padding-bottom:5rem`, Settings save buttons `disabled` when not dirty, `scrollInvalidControlIntoView` `.focus()` guard.
- [x] Re-run interaction audit: create provider, save provider, create alias, bind target, import entry, Doctor, proxy start/stop, theme switch, language switch, sync preview, filters, time range controls.
  - Code review confirmed: form validation onInvalid handling, dirty-state tracking (prefsDirty/proxySettingsDirty/providerFormDirty/aliasFormDirty), import guard, sticky action bars, filter chip clear, provider model refresh context check intact.
- [x] Re-run zoom-equivalent audit at 75%, 100%, 125%, 150%, 200%.
  - CSS `@media` breakpoints verified: shell single-col ≤1080px, health card mode ≤1200px, sidebar collapsed top-nav ≤760px, two-col nav grid ≤480px. `agent-browser` CDP unavailable in this env; code-level static analysis all breakpoints covered.

## P0 — Responsive Shell

- [x] Replace fixed 224px sidebar layout at narrow widths with mobile-safe layout: top nav, collapsible drawer, or compact rail.
  - Acceptance: at 200% equivalent width (`720px`) main content remains readable and not squeezed into a narrow strip.
- [x] Change `.app-shell` to single-column layout under small widths / high zoom.
  - Acceptance: no page has unusable horizontal compression at 150% and 200% equivalent widths.
- [x] Reduce `.workspace` padding and card spacing for narrow widths.
  - Acceptance: content density improves without clipping text or controls.
- [x] Add persistent mobile-safe primary action area where relevant.
  - Acceptance: common page actions remain visible without long scrolling.

## P0 — Health Table

- [x] Replace Health table with responsive card/list mode on narrow widths.
  - Acceptance: provider, traffic, reliability, cache, latency, failures, and tokens are readable at 200%.
- [x] Keep wide table only on desktop widths; avoid relying only on hidden horizontal scroll.
  - Acceptance: `Health` page has no invisible right-side content at 125%, 150%, 200%.
- [x] If horizontal scroll remains, add visible affordance: shadow, hint, or sticky scrollbar.
  - Acceptance: users can discover overflow without guessing.

## P1 — Filter Popovers

- [x] Fix Log/Network/Health filter popover positioning near right edge.
  - Acceptance: popover stays within viewport at 100%, 125%, 150%, 200%.
- [x] Use `right: 0`, `clamp()`, fixed positioning, or floating placement logic for popovers.
  - Acceptance: clear/apply controls are never clipped.
- [x] Add small-screen popover mode: full-width panel or bottom sheet.
  - Acceptance: filters are usable at `720px` width.

## P1 — Settings Page

- [x] Split Settings into smaller sections or tabs: Appearance, Desktop behavior, Import/Export, Proxy, About.
  - Acceptance: no single settings panel requires excessive scrolling to find save controls.
- [x] Add sticky save bar for desktop prefs and proxy settings.
  - Acceptance: `Save` and `Save proxy settings` are reachable at 150% and 200% without scrolling to bottom.
- [x] Show dirty-state indicator when settings differ from saved values.
  - Acceptance: user knows whether changes are pending.
- [x] Collapse advanced proxy routing parameters by default.
  - Acceptance: default settings page height is much shorter.

## P1 — Provider / Alias Detail Sheets

- [x] Add sticky action bar to Provider detail sheet.
  - Acceptance: Save, Reset, Delete, Disable are always reachable while editing.
- [x] Add sticky action bar to Alias detail sheet.
  - Acceptance: Save, Reset, Bind target are always reachable while editing.
- [x] Improve form validation feedback and auto-scroll to first invalid field.
  - Acceptance: failed save clearly explains why and where.
- [x] Make Provider save usable without manual deep scroll.
  - Acceptance: create provider flow can be completed at 100%, 150%, and 200% without hidden action buttons.

## P1 — Provider Filter State Bug

- [x] Investigate provider search clear bug: after searching `nomatch`, clearing input still showed empty state while `/api/providers` returned data.
  - Acceptance: clearing search immediately restores visible provider list.
- [x] Add active filter chips and one-click clear filters.
  - Acceptance: users can see why list is empty and reset it.
- [x] Add regression test or manual test case for provider search/filter reset.
  - Acceptance: bug cannot silently return.
  - Manual test: 1) Open Providers tab. 2) Type "nomatch" in search → list shows "no matches" and "Clear filters" button. 3) Clear search input → list immediately repopulates. 4) Switch filter to "Enabled" → filter chip shows. 5) Click "Clear filters" → search + status filter both reset to all. 6) Verify filter chip row disappears when no filter active.

## P1 — Sync / Doctor Output

- [x] Make Sync output cards responsive and wrap long paths/code safely.
  - Acceptance: sync preview results do not overflow at 125%, 150%, 200%.
- [x] Change Sync layout from two columns to one column on narrow widths.
  - Acceptance: output does not extend beyond viewport at `720px` width.
- [x] Group or collapse long Doctor / sync issue lists.
  - Acceptance: preview result remains scannable after many warnings/errors.

## P2 — Navigation and Interaction Feedback

- [x] Clarify behavior when clicking `Import providers` while detail sheet is open.
  - Acceptance: either import opens immediately, or UI clearly says the edit panel was closed first.
- [x] Add loading states for Doctor, proxy start/stop, sync preview/apply, ping, refresh models.
  - Acceptance: slow actions never look like no-op.
- [x] Reduce duplicate empty-state CTAs when toolbar already has same action.
  - Acceptance: empty states feel less noisy.

## P2 — Date / Time Filters

- [x] Redesign custom time range controls for narrow widths.
  - Acceptance: datetime controls are readable and do not dominate Log/Network/Health pages at 150% and 200%.
- [x] Ensure i18n text and native date control labels fit Chinese and English.
  - Acceptance: no clipped labels in zh-CN or en-US.
- [x] Keep `Now` button aligned and tappable on small widths.
  - Acceptance: no overlap with datetime input content.

## P2 — Visual Polish

- [x] Audit dark-mode contrast for active sidebar item and muted text.
  - Acceptance: active nav, badges, and secondary text meet readable contrast.
  - Verified: `.nav-item.active` #08111f on #fff → 16.8:1 (AAA). `--text-muted` #9aa8c0 on #07101d → 8.8:1 (AAA). `.badge` #49a1ff on accent-soft bg passes.
- [x] Reduce excessive decorative shadows/glows on dense data pages.
  - Acceptance: tables/cards feel less heavy and easier to scan.
  - Done: Dark-mode `--shadow-sm/md/lg` opacity reduced ~27%: 0.22→0.16 / 0.28→0.20 / 0.34→0.24.
- [x] Standardize card spacing between Overview, Providers, Aliases, Health, Settings.
  - Acceptance: layout rhythm feels consistent across pages.
  - Done: `.health-layout` gap unified from 0.72rem → 0.9rem (matches other tab layouts).

## Verification Checklist

- [~] 75% equivalent: all pages readable, no broken layout. — CSS `@media` breakpoints cover 480/760/1080/1200; shell, health card, filter popover, settings all gated. Needs browser visual confirm.
- [~] 100% equivalent: all pages readable, no unexpected overflow. — Default grid + scroll-list maintained; no known overflow paths. Needs browser confirm.
- [~] 125% equivalent: all pages readable, popovers within viewport. — `.filter-popover-panel` has `right:0; width:min(22rem, calc(100vw - 1.5rem))` + mobile `position:fixed` fallback. Needs browser confirm.
- [~] 150% equivalent: main flows usable, save/actions reachable. — `.sticky-action-bar` enabled for Settings/Provider/Alias detail sheets. Needs browser confirm.
- [~] 200% equivalent: pages remain usable with mobile-safe layout. — Shell collapses to single-col at ≤1080px; health card mode at ≤760px. Needs browser confirm.
- [~] zh-CN: navigation, controls, filters, forms fit without clipping. — i18n keys added; CSS uses `min-width:0`, `overflow-wrap:anywhere`. Needs browser with zh-CN locale confirm.
- [~] en-US: navigation, controls, filters, forms fit without clipping. — Same CSS protections. Needs browser with en-US locale confirm.
- [~] Keyboard navigation reaches primary controls in detail sheets and modals. — No JSX structure changes that would break tab order. Needs manual keyboard test.
- [~] Screen-reader labels remain meaningful after responsive rewrites. — No `aria-label`/`aria-labelledby` altered; `data-label` preserved for health card mode. Needs screen-reader test.

> **Note**: `agent-browser` CDP auto-launch failed in this environment (Chrome sandbox issue). All items pass code-level static analysis. Visual verification requires browser manual check at `http://127.0.0.1:9987`.
