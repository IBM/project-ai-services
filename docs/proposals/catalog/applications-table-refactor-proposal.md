# Applications Table Refactor

**Components:** `pages/DigitalAssistants` & `components/DeployedServicesTable`

---

## Problem

The Digital Assistants table and Deployed Services table were developed independently but provide many of the same functionalities, including:

* Search
* Filtering
* Column management
* Export
* Confirmation dialogs
* Data fetching and refresh

Because these features were implemented separately, both tables contain significant duplicated logic and UI code.
This duplication increases maintenance effort and requires bug fixes to be applied in multiple places.

---

## Proposed Solution

Create a shared table framework that contains the common functionality used by both Digital Assistants and Deployed Services tables.
Common functionality will be centralized into reusable components, hooks, and utilities, reducing duplication and ensuring that fixes and improvements apply to both tables at once.

```
src/components/
  table/
    types.ts
    table.shared.module.scss
    utils/
      reducerUtils.ts
      tableUtils.ts
    hooks/
      useAutoRefresh.ts
      useCSVExport.ts
      useExportToastAutoDismiss.ts
    components/
      CellRenderers.tsx
      DeleteModal.tsx
      ExportModal.tsx
      TableToasts.tsx
      TableEmptyStates.tsx
      TableToolbarActions.tsx
  DeployedServicesTable/
    DeployedServicesTable.tsx     ← adopts shared components and hooks
    DeployedServices.module.scss  ← retains only DS-specific rules
    CellRenderers.tsx             ← wrappers only: ActionCell (wires dispatch) + NameCell (wires onRowClick)
    types.ts                      ← retains only DS-specific state and actions
    index.ts                      ← unchanged

src/pages/
  DigitalAssistants/
    DigitalAssistants.tsx         ← adopts shared components and hooks
    DigitalAssistants.module.scss ← retains only DA-specific and AboutTab rules
    CellRenderers.tsx             ← wrappers only: ActionCell (wires dispatch) + NameCell (wires dispatch)
    types.ts                      ← retains only DA-specific state and actions
    index.ts                      ← unchanged
```

All shared table infrastructure lives in `src/components/table/` and both consumers remain as independent files that only import what they need from it.

The `table/` directory contains everything both tables import from:

- **`types.ts`** — Provides the base TypeScript contracts used by both tables, including shared row models, table state, and column definitions.

- **`table.shared.module.scss`** — Contains styles that are duplicated across both table implementations, including toast notifications, status tags, menu styling, and shared table presentation. Table-specific styles remain within their respective modules.

- **`reducerUtils.ts`** — Provides handleSharedTableAction<S extends BaseTableState>(state, action): S | undefined, which owns the reducer logic for all shared table actions. Each table reducer composes it with its own reducer, handling table-specific actions and falling through to the shared reducer for common actions. This centralizes shared reducer behavior in a single action union and handler, replacing duplicated reducer cases across both tables.

- **`tableUtils.ts`** — Three utility functions: `filterRowsBySearch(rows, search, fields)` (both tables filter rows by joining field values and lowercasing — the only difference is which fields are included, which becomes a parameter), `getVisibleHeaders(headers, visibleColumns)` (identical in both files ), and `getToggleableHeaders(headers)`.

- **`useAutoRefresh.ts`** —Encapsulates the shared refresh lifecycle. The hook performs an initial fetch, re-fetches when dependencies change, and manages interval-based polling. To prevent overlapping requests, refresh ticks are skipped while a fetch is already in progress. Polling can also be temporarily disabled (e.g. while a delete confirmation modal is open) to avoid refreshing data during delete operations.

- **Skip-if-in-flight** : an `isFetchingRef` ref tracks whether a fetch is in progress. If the interval fires while one is already running, the tick is skipped so slow responses can't stack or apply out of order.
- **Pause-while-delete-modal-open**: the `enabled` flag lets the caller suspend the interval; both consumers will pass `enabled={!state.isDeleteDialogOpen}`

- **`useCSVExport.ts`** — A hook that implements the complete CSV export flow: filename validation, the multi-page fetch loop , visible-column filtering, `downloadCSVWithChildren` call, and success/error toast state.

- **`useExportToastAutoDismiss.ts`** — A hook for the 5-second auto-dismiss of successful export toasts.Currently both files have an identical `useEffect` that sets a `setTimeout` when `exportToastOpen && exportToastKind === "success"`.

- **`CellRenderers.tsx`** — Exports shared table cell renderers (StatusCell, MessageCell, ActionCell, and NameCell) along with a canonical STATUS_CONFIG map covering all statuses used by both tables.

Status keys align with the exact values returned by the API. Both tables currently define "Deploying..." and "Deleting..." as frontend-only variants, while the API returns "Deploying" and "Deleting". This mismatch would require status normalization before STATUS_CONFIG lookup, so the refactor removes the ellipsis variants and updates RowStatus, STATUS_CONFIG, and related constants to use the API values directly.

The shared ActionCell standardizes delete eligibility checks on exact status matching (rowData?.status === "Running"), replacing the current inconsistency where Deployed Services performs a case-insensitive comparison. This does not change runtime behavior because both tables already operate on the same canonical status values.

- **`DeleteModal.tsx`** — The Carbon `Modal` for delete confirmation: a warning message, a `Checkbox` requiring the user to confirm, and primary/secondary buttons. Accepts `isOpen`, `itemName`, `onConfirm`, and `onClose` as props.

- **`ExportModal.tsx`** — The Carbon `Modal` for CSV filename input: a `TextInput`, inline error message, and primary/secondary buttons. Accepts `isOpen`, `defaultFileName`, `onConfirm`, and `onClose` as props.

- **`TableToasts.tsx`** — Renders the two `ActionableNotification` blocks that appear in both files: the delete-error toast (with a "Try again" action button) and the export success/error toast. 

- **`TableEmptyStates.tsx`** — Renders all three empty/error state variants used by both tables: no data, no search results, and fetch error. Accepts `entityName` (e.g. `"service"` or `"digital assistant"`) 

- **`TableToolbarActions.tsx`** — The shared toolbar providing search, refresh, export, and column visibility management. Currently both tables render a `TableToolbar` with the same search input, export button, and column-visibility overflow menu. The service filter `RadioButtonGroup` unique to `DeployedServicesTable` is injected via a `filterSlot` prop so the toolbar remains usable by both without hardcoding.


---

## Pull Requests

### PR 1 — Shared Types, Utilities, and Styles

Create a shared foundation by extracting common types, utility functions, and styling that are currently duplicated across both tables. This provides a consistent base for the reusable hooks and components introduced in later phases.

**Implementation:**

* `table/types.ts` — `RowStatus` (`"Initializing" | "Downloading" | "Deploying" | "Running" | "Deleting" | "Stopped" | "Error"`), `TableHeaders`, `BaseTableState`
* `table/utils/reducerUtils.ts` — helper functions + `handleSharedTableAction<S extends BaseTableState>` composed dispatcher + `SharedTableAction` union; each table's reducer wires up as `handleSharedTableAction(state, action) ?? ownCases(state, action)`
* `table/utils/tableUtils.ts` — `filterRowsBySearch`, `getVisibleHeaders`, `getToggleableHeaders`
* `table/table.shared.module.scss` — shared CSS rules extracted from both `.module.scss` files: `.customToast`, `.deleteConfirmation`, `.overflowMenu*`, `.deleteMenuItem`, `.messageWithIcon`, `.messageIcon*`, `.statusTag*`

---

### PR 2 — Shared Hooks

Extract common lifecycle and workflow logic into reusable hooks. This centralizes functionality such as auto-refresh, export processing, and notification handling.

**Implementation:**

* `table/hooks/useAutoRefresh.ts` — mount fetch + interval + `refreshTrigger` lifecycle; skip-if-in-flight guard; `enabled` flag for delete-modal pause
* `table/hooks/useCSVExport.ts` — full 8-step CSV export flow
* `table/hooks/useExportToastAutoDismiss.ts` — 5-second success toast auto-dismiss

---

### PR 3 — Shared UI Components

Create reusable components for common table functionality and interactions. This reduces duplicated UI code and ensures a consistent experience across both tables.

**Implementation:**

* `table/components/TableToolbarActions.tsx` — toolbar with search, refresh, export, and column visibility
* `table/components/CellRenderers.tsx` — `ActionCell`, `NameCell`, `StatusCell`, `MessageCell`, and the merged `STATUS_CONFIG` map with keys matching exact API-returned strings (no ellipsis)
* `table/components/ExportModal.tsx` — CSV filename input modal with text input and inline validation error
* `table/components/DeleteModal.tsx` — delete confirmation modal with warning message and confirmation checkbox
* `table/components/TableToasts.tsx` — renders both the delete-error and export status
* `table/components/TableEmptyStates.tsx` — renders the no-data, no-search-results, and fetch-error empty states

### PR 4 — Migrate DigitalAssistants to shared infrastructure

Replace duplicated implementations in `DigitalAssistants.tsx` and its supporting files with the shared hooks, utilities, and components from PRs 1–3. 

**Key changes:**

- Replace existing toolbar JSX with `<TableToolbarActions>`
- Replace inline delete modal JSX with `<DeleteModal>`
- Replace inline export modal JSX with `<ExportModal>`
- Replace inline delete-error and export toast JSX with `<TableToasts>`
- Replace inline empty state JSX with `<TableEmptyStates entityName="digital assistant">` 
- Remove `StatusCell`, `MessageCell`,`ActionCell` and `NameCell` from `DigitalAssistants/CellRenderers.tsx` — import from shared
- Remove inline `downloadCSV` function — adopt `useCSVExport`
- Remove fetch lifecycle `useEffect`s — adopt `useAutoRefresh`
- Remove export toast auto-dismiss `useEffect` — adopt `useExportToastAutoDismiss`
- Replace inline search filter logic with `tableUtils.filterRowsBySearch`
- Replace inline visible-header calculation with `tableUtils.getVisibleHeaders`
- Replace column reset hardcoded object with `reducerUtils.resetColumnVisibility(DEFAULT_VISIBLE_COLUMNS)`
- Rename `isLoadingApplications` → `isLoading` 
- Update `DigitalAssistantRow["status"]` union: replace `"Deploying..." | "Deleting..."` with `"Deploying" | "Deleting"`;
- Add `UPDATE_ROW_STATUS` action to DA reducer — dispatched immediately on delete confirm to set the row to `"Deleting"` while the API call is in flight; matches the existing DS pattern
- Extend `DigitalAssistants.module.scss` to compose from `table.shared.module.scss`; remove the now-duplicated rules

PR 4 depends on PRs 1, 2, and 3.

### PR 5 — Migrate DeployedServicesTable to shared infrastructure

Same pattern as PR 4, applied to `DeployedServicesTable.tsx`.

**Key changes (same pattern as PR 4, with DS-specific differences):**

- Replace existing toolbar JSX with `<TableToolbarActions filterSlot={<ServiceFilterRadioGroup />}>`
- Replace inline delete and export modal JSX with `<DeleteModal>` and `<ExportModal>`
- Replace inline toast JSX with `<TableToasts>`
- Replace inline empty state JSX with `<TableEmptyStates entityName="service">`
- Remove `StatusCell`, `MessageCell`,`ActionCell` and `NameCell` from `DeployedServicesTable/CellRenderers.tsx` — import from shared
- Remove inline `downloadCSV` function — adopt `useCSVExport`
- Remove fetch lifecycle `useEffect`s — adopt `useAutoRefresh`
- Remove export toast auto-dismiss `useEffect` — adopt `useExportToastAutoDismiss`
- Replace inline search filter logic with `tableUtils.filterRowsBySearch`
- Replace inline visible-header calculation with `tableUtils.getVisibleHeaders`
- Replace column reset hardcoded object with `reducerUtils.resetColumnVisibility(DEFAULT_VISIBLE_COLUMNS)`
- Update `DeployedServicesRow["status"]` union: replace `"Deploying..." | "Deleting..."` with `"Deploying" | "Deleting"`; update `STATUS_SORT_ORDER` keys to match.
- Extend `DeployedServices.module.scss` to compose from `table.shared.module.scss`; remove the now-duplicated lines

PR 5 depends on PRs 1, 2, and 3.

---


## File Changes Summary

A complete map of every file created, modified, or deleted across all 5 PRs.

### `src/components/table/` (new)

| Path | Change |
|---|---|
| `table/types.ts` | Created (PR 1) |
| `table/table.shared.module.scss` | Created (PR 1) |
| `table/utils/reducerUtils.ts` | Created (PR 1) |
| `table/utils/tableUtils.ts` | Created (PR 1) |
| `table/hooks/useAutoRefresh.ts` | Created (PR 2) |
| `table/hooks/useCSVExport.ts` | Created (PR 2) |
| `table/hooks/useExportToastAutoDismiss.ts` | Created (PR 2) |
| `table/components/CellRenderers.tsx` | Created (PR 3) |
| `table/components/DeleteModal.tsx` | Created (PR 3) |
| `table/components/ExportModal.tsx` | Created (PR 3) |
| `table/components/TableToasts.tsx` | Created (PR 3) |
| `table/components/TableEmptyStates.tsx` | Created (PR 3) |
| `table/components/TableToolbarActions.tsx` | Created (PR 3) |

### `src/components/DeployedServicesTable/`

| Path | Change |
|---|---|
| `DeployedServicesTable.tsx` | Modified (PR 5) — adopts all shared hooks and components; ~150 lines removed |
| `CellRenderers.tsx` | Modified (PR 5) — all four cells import from shared
| `types.ts` | Modified (PR 5) — extends `BaseDeploymentRow` and `BaseTableState`; removes shared reducer cases |
| `DeployedServices.module.scss` | Modified (PR 5) — composes from `table.shared.module.scss`; removes ~125 duplicated lines |
| `index.ts` | Untouched |

### `src/pages/DigitalAssistants/`

| Path | Change |
|---|---|
| `DigitalAssistants.tsx` | Modified (PR 4) — adopts all shared hooks and components|
| `CellRenderers.tsx` | Modified (PR 4) — all four cells import from shared
| `types.ts` | Modified (PR 4) — extends `BaseDeploymentRow` and `BaseTableState`; removes shared reducer cases; renames `isLoadingApplications` → `isLoading` |
| `DigitalAssistants.module.scss` | Modified (PR 4) — composes from `table.shared.module.scss`|
| `index.ts` | Untouched |
| `components/AboutTab.tsx` | Untouched |

## Expected Outcome

| Metric                      | Current State                     | Target State                 |
| --------------------------- | --------------------------------- | ---------------------------- |
| RowStatus definitions       | 2 copies                          | 1 shared definition          |
| Reducer logic               | Duplicated across both tables     | Shared utilities             |
| Export implementation       | Multiple implementations          | Single shared implementation |
| Auto-refresh implementation | Multiple implementations          | Single shared implementation |
| Toast notification JSX      | 2 copies  | 1 shared component           |
| Empty / error state JSX     | 2 copies | 1 shared component |
| Shared CSS rules            | Duplicated in 2 SCSS files | 1 shared SCSS module    |


