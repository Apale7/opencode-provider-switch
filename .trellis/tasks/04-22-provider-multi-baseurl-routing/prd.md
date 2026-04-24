# Provider multi-baseURL routing and alias target ordering

## Summary

Provider already has multi-baseURL management and ping UX in progress. This task extends the same ordering interaction to alias bound targets so users can control deterministic routing priority directly from the desktop UI.

Alias target order is already meaningful in the backend: targets are stored as an ordered array, returned to the frontend in order, and used by routing/failover in that order. The missing capability is a safe management API and UI to reorder existing bound targets.

## Requirements

- Allow users to reorder targets bound to an alias from the alias detail view.
- Reuse the provider multi-baseURL interaction model: visible order index, up/down controls, and drag-and-drop reorder.
- Persist target order by reordering the alias `targets` array; do not introduce a separate order field.
- Preserve target enabled/disabled state while reordering.
- Validate reorder requests so they must reference the exact same target set as the alias currently has: no missing, duplicate, or unknown target refs.
- Return the updated `AliasView` after reorder so UI can refresh without losing context.
- Keep alias target ordering i18n-ready with English and zh-CN strings.
- Keep desktop Wails bridge and browser fallback HTTP API behavior aligned.

## Non-Goals

1. Do not change routing strategy semantics beyond using the existing ordered target array.
2. Do not add weighted, latency-based, or cost-based target routing.
3. Do not change alias/provider/model naming or matching rules.
4. Do not change OpenCode sync output format except through existing alias target order persistence.
5. Do not redesign the alias detail page beyond the ordering controls needed here.

## Acceptance Criteria

1. Alias detail target list shows target order and supports up/down reordering.
2. Alias detail target list supports drag-and-drop reordering with a visible dragging state.
3. Reordering persists to config and survives reload.
4. Disabled targets keep their disabled state after reorder.
5. Invalid reorder payloads are rejected when they omit, duplicate, or include unknown targets.
6. Browser fallback API and Wails bridge both expose the same reorder capability.
7. Frontend English and zh-CN locales include all new alias ordering labels/status text.
8. Existing provider multi-baseURL UX remains unchanged.
