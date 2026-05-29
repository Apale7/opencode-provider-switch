# UI/UX Optimization TODO

Date: 2026-05-29
Owner: OpenCode
Source: local browser audit on `http://127.0.0.1:9987` with temporary config

## Test Coverage

- [ ] Re-run audit after fixes on pages: Overview, Providers, Aliases, Log, Network, Health, Sync, Settings.
- [ ] Re-run interaction audit: create provider, save provider, create alias, bind target, import entry, Doctor, proxy start/stop, theme switch, language switch, sync preview, filters, time range controls.
- [ ] Re-run zoom-equivalent audit at 75%, 100%, 125%, 150%, 200%.

## P0 — Responsive Shell

- [ ] Replace fixed 224px sidebar layout at narrow widths with mobile-safe layout: top nav, collapsible drawer, or compact rail.
  - Acceptance: at 200% equivalent width (`720px`) main content remains readable and not squeezed into a narrow strip.
- [ ] Change `.app-shell` to single-column layout under small widths / high zoom.
  - Acceptance: no page has unusable horizontal compression at 150% and 200% equivalent widths.
- [ ] Reduce `.workspace` padding and card spacing for narrow widths.
  - Acceptance: content density improves without clipping text or controls.
- [ ] Add persistent mobile-safe primary action area where relevant.
  - Acceptance: common page actions remain visible without long scrolling.

## P0 — Health Table

- [ ] Replace Health table with responsive card/list mode on narrow widths.
  - Acceptance: provider, traffic, reliability, cache, latency, failures, and tokens are readable at 200%.
- [ ] Keep wide table only on desktop widths; avoid relying only on hidden horizontal scroll.
  - Acceptance: `Health` page has no invisible right-side content at 125%, 150%, 200%.
- [ ] If horizontal scroll remains, add visible affordance: shadow, hint, or sticky scrollbar.
  - Acceptance: users can discover overflow without guessing.

## P1 — Filter Popovers

- [ ] Fix Log/Network/Health filter popover positioning near right edge.
  - Acceptance: popover stays within viewport at 100%, 125%, 150%, 200%.
- [ ] Use `right: 0`, `clamp()`, fixed positioning, or floating placement logic for popovers.
  - Acceptance: clear/apply controls are never clipped.
- [ ] Add small-screen popover mode: full-width panel or bottom sheet.
  - Acceptance: filters are usable at `720px` width.

## P1 — Settings Page

- [ ] Split Settings into smaller sections or tabs: Appearance, Desktop behavior, Import/Export, Proxy, About.
  - Acceptance: no single settings panel requires excessive scrolling to find save controls.
- [ ] Add sticky save bar for desktop prefs and proxy settings.
  - Acceptance: `Save` and `Save proxy settings` are reachable at 150% and 200% without scrolling to bottom.
- [ ] Show dirty-state indicator when settings differ from saved values.
  - Acceptance: user knows whether changes are pending.
- [ ] Collapse advanced proxy routing parameters by default.
  - Acceptance: default settings page height is much shorter.

## P1 — Provider / Alias Detail Sheets

- [ ] Add sticky action bar to Provider detail sheet.
  - Acceptance: Save, Reset, Delete, Disable are always reachable while editing.
- [ ] Add sticky action bar to Alias detail sheet.
  - Acceptance: Save, Reset, Bind target are always reachable while editing.
- [ ] Improve form validation feedback and auto-scroll to first invalid field.
  - Acceptance: failed save clearly explains why and where.
- [ ] Make Provider save usable without manual deep scroll.
  - Acceptance: create provider flow can be completed at 100%, 150%, and 200% without hidden action buttons.

## P1 — Provider Filter State Bug

- [ ] Investigate provider search clear bug: after searching `nomatch`, clearing input still showed empty state while `/api/providers` returned data.
  - Acceptance: clearing search immediately restores visible provider list.
- [ ] Add active filter chips and one-click clear filters.
  - Acceptance: users can see why list is empty and reset it.
- [ ] Add regression test or manual test case for provider search/filter reset.
  - Acceptance: bug cannot silently return.

## P1 — Sync / Doctor Output

- [ ] Make Sync output cards responsive and wrap long paths/code safely.
  - Acceptance: sync preview results do not overflow at 125%, 150%, 200%.
- [ ] Change Sync layout from two columns to one column on narrow widths.
  - Acceptance: output does not extend beyond viewport at `720px` width.
- [ ] Group or collapse long Doctor / sync issue lists.
  - Acceptance: preview result remains scannable after many warnings/errors.

## P2 — Navigation and Interaction Feedback

- [ ] Clarify behavior when clicking `Import providers` while detail sheet is open.
  - Acceptance: either import opens immediately, or UI clearly says the edit panel was closed first.
- [ ] Add loading states for Doctor, proxy start/stop, sync preview/apply, ping, refresh models.
  - Acceptance: slow actions never look like no-op.
- [ ] Reduce duplicate empty-state CTAs when toolbar already has same action.
  - Acceptance: empty states feel less noisy.

## P2 — Date / Time Filters

- [ ] Redesign custom time range controls for narrow widths.
  - Acceptance: datetime controls are readable and do not dominate Log/Network/Health pages at 150% and 200%.
- [ ] Ensure i18n text and native date control labels fit Chinese and English.
  - Acceptance: no clipped labels in zh-CN or en-US.
- [ ] Keep `Now` button aligned and tappable on small widths.
  - Acceptance: no overlap with datetime input content.

## P2 — Visual Polish

- [ ] Audit dark-mode contrast for active sidebar item and muted text.
  - Acceptance: active nav, badges, and secondary text meet readable contrast.
- [ ] Reduce excessive decorative shadows/glows on dense data pages.
  - Acceptance: tables/cards feel less heavy and easier to scan.
- [ ] Standardize card spacing between Overview, Providers, Aliases, Health, Settings.
  - Acceptance: layout rhythm feels consistent across pages.

## Verification Checklist

- [ ] 75% equivalent: all pages readable, no broken layout.
- [ ] 100% equivalent: all pages readable, no unexpected overflow.
- [ ] 125% equivalent: all pages readable, popovers within viewport.
- [ ] 150% equivalent: main flows usable, save/actions reachable.
- [ ] 200% equivalent: pages remain usable with mobile-safe layout.
- [ ] zh-CN: navigation, controls, filters, forms fit without clipping.
- [ ] en-US: navigation, controls, filters, forms fit without clipping.
- [ ] Keyboard navigation reaches primary controls in detail sheets and modals.
- [ ] Screen-reader labels remain meaningful after responsive rewrites.
