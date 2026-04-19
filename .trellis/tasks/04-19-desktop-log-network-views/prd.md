# Desktop Log, Network, And List-First Forms

## Summary

Desktop shell already contains provider and alias management pages with inline split-view editing, plus dedicated log and network tabs. Current provider and alias pages always reserve detail-pane space and automatically select first list item. This makes browsing feel heavy and wastes space when user only wants to inspect list.

This task keeps current data flow and form logic, but changes page behavior to a list-first layout:

- provider page shows only list by default
- alias route page shows only list by default
- clicking a list item opens edit panel
- clicking new opens create form panel
- import and bind target flows stay modal-based

## Product Goal

Make provider and alias route management feel lighter and more focused by separating browsing from editing, while preserving existing CRUD behavior and minimizing architectural churn.

## Current State

### Already done

- `frontend/src/App.tsx` contains provider and alias list, search, filter, create, edit, delete, import, and bind-target flows.
- provider and alias forms already reuse local state and existing API calls.
- desktop shell already uses modal overlays for provider import, alias target binding, and confirm dialogs.

### Problems

- provider and alias tabs default to split-view with always-visible detail panel.
- first filtered item is auto-selected, so page never rests in pure list mode.
- empty detail area still occupies layout width before user chooses an action.

## Requirements

### Provider page

- Default state shows only provider list and list controls.
- No provider detail panel should be visible until user clicks a provider row or `New provider`.
- Clicking provider row opens edit panel for that provider.
- Clicking `New provider` opens create panel with empty form.

### Alias route page

- Default state shows only alias list and list controls.
- No alias detail panel should be visible until user clicks alias row or `New alias`.
- Clicking alias row opens edit panel for that alias.
- Clicking `New alias` opens create panel with empty form.

### Interaction and reuse

- Reuse existing provider and alias form logic as much as possible.
- Reuse current modal/backdrop interaction patterns already used in desktop shell.
- Keep import-provider modal and bind-target modal unchanged unless minor adjustments are required for consistency.
- Keep existing backend/API contracts unchanged.

## Design Decisions

### Detail presentation

Use conditional overlay detail panel instead of always-mounted split-view pane.

Why:

- smallest frontend-only change that matches requested behavior
- consistent with existing modal/backdrop interaction already present in app
- preserves current form structure, submit handlers, and confirmation flows

### Selection behavior

Do not auto-select first filtered provider or alias.

Why:

- page should remain in pure list mode until explicit user action
- prevents accidental context switching when search/filter changes

## Non-Goals

1. No backend changes for providers, aliases, logs, or network.
2. No redesign of import-provider or bind-target workflows.
3. No new global state layer or routing changes.
4. No visual redesign outside provider and alias page layout adjustments needed for list-first behavior.

## Acceptance Criteria

1. Provider tab initially renders list without visible edit pane.
2. Alias tab initially renders list without visible edit pane.
3. Clicking provider row opens provider edit panel.
4. Clicking alias row opens alias edit panel.
5. Clicking provider create button opens provider create panel.
6. Clicking alias create button opens alias create panel.
7. Search/filter changes do not auto-open a detail panel.
8. Existing save, delete, import, bind, and toggle actions still work.
