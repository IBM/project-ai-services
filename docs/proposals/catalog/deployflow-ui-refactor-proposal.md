# DeployFlow Refactor — Engineering Proposal

**Components:** `deployFlow/digitalAssistant` & `deployFlow/services`

---

## Problem

We have two multi-step deployment tearsheets — `digitalAssistant` for deploying a full digital assistant (multi-service) and `services` for deploying a single standalone service. They were built independently and ended up sharing the same patterns, UI, and logic, but with zero shared code between them.

The duplication spans every layer — components, styles, state management, hooks, API clients, and types — amounting to roughly 2,500 lines of code that exist twice. Every bug fix or UI change has to be applied in two places, and the data models have already started to drift.

---

## Proposed Solution

All deployment-related components are reorganised under a single `src/components/deployFlow/` parent folder. Inside it, a `shared/` directory holds everything common to both flows, and the two specific flows live alongside it as sibling folders:

```
src/components/
  deployFlow/
    shared/
      types.ts
      deployFlow.shared.module.scss
      utils/
        utils.ts
        paramFilter.ts
      hooks/
        useDeployFlowReducer.ts
      components/
        DynamicSchemaFields.tsx
        ResourceRequirementsPanel.tsx
        DeployTearsheetShell.tsx
        ServiceCredentialDisplay.tsx
        ServiceFieldLabel.tsx
        ServiceConfigCard.tsx
      steps/
        StepOne.tsx
        StepTwo.tsx
    digitalAssistant/
      DigitalAssistantDeployFlow.tsx
      DigitalAssistantDeployFlow.module.scss
      types.ts
      index.ts
      components/
        ResourceRequirements.tsx
      hooks/
        useDeployOptions.ts
      utils/
        formDataInitializer.ts
        digitalAssistantDeploymentTransform.ts
        resourceSharing.ts
    services/
      ServicesDeployFlow.tsx
      ServicesDeployFlow.module.scss
      types.ts
      index.ts
      steps/
        StepZero.tsx
      components/
        ResourceRequirements.tsx
      hooks/
        useServiceDeployOptions.ts
        useProviderSchema.ts
      utils/
        formDataInitializer.ts
        serviceDeploymentTransform.ts
```

This structure makes the relationship explicit — everything deployment-related lives under one roof, the shared pieces are clearly labelled, and the two flows sit as siblings rather than unrelated top-level components.

The `shared/` directory contains everything both flows import from:

- **`types.ts`** — the core TypeScript interfaces that describe the shape of the form data and state. Both flows work with the same fundamental data structures: a deployment has a name, a version, a set of components each with a provider and parameters, and a set of services each containing those components. These types are currently defined twice; they move here once.
- **`deployFlow.shared.module.scss`** — all layout and visual styles that are identical in both stylesheets. Things like the step header, the form grid, the resource tile grid, the service config card layout, and the responsive breakpoints. Both flow stylesheets import from this and only define their own unique classes on top.
- **`DynamicSchemaFields.tsx`** — a form field renderer that takes a provider's JSON schema and renders the appropriate Carbon inputs (text, password, number, dropdown, textarea, checkbox). Both flows use this to render provider-specific credential and configuration fields. It is a purely presentational component — it knows nothing about deployment, services, or APIs. It just renders fields from a schema and calls `onChange` when a value changes.
- **`ResourceRequirementsPanel.tsx`** — a purely presentational component that displays the CPU, memory, accelerator, and storage tiles with their loading, error, and empty states. It receives already-computed numbers as props and renders them. Both flows calculate their own resource totals (using different logic) and then hand the results to this panel to display.
- **`useDeployFlowReducer.ts`** — a React hook that owns the shared state management: the reducer, the action types, and the four callbacks (`handleFormDataChange`, `handleEditingChange`, `handleResourceStatusChange`, `handleBack`) that are identical in both root components. Each flow calls this hook and gets the shared state and callbacks back, then adds only its own flow-specific actions on top — `digitalAssistant` adds `SET_IS_LOADING` and `SET_ERROR` (driven by `useDeployOptions`), and `services` adds `SET_SELECTED_SERVICE` (for tracking which standalone service the user picked in StepZero).
- **`DeployTearsheetShell.tsx`** — a wrapper component that renders the `Tearsheet`, the vertical `ProgressIndicator` in the influencer panel(the optional side panel), and the deployment error toast notification. Each flow passes in its own step labels and the content for the current step as children. The shell handles all the surrounding chrome.

**What stays unique to each flow:**

`deployFlow/digitalAssistant/` keeps:

- Its own API calls (`deployApplication`, `fetchServices`) and store (`useDeployStore`)
- Its own form initialisation logic
- `ResourceRequirements.tsx` — fetches available system resources and calculates totals accounting for global components plus every enabled service, then passes the results to the shared `ResourceRequirementsPanel` to display

`deployFlow/services/` keeps:

- Its own API calls (`deployApplication` via `applications.api`) and store (`useServiceDeployStore`)
- `StepZero` — the service selection screen where the user picks which standalone service to deploy
- `ResourceRequirements.tsx` — fetches available system resources and calculates totals for a single service only, then passes the results to the shared `ResourceRequirementsPanel` to display
- `formDataInitializer.ts` — initialises the form data structure when a service is selected

The work is split into 10 PRs, each under 500 lines of diff so they stay easy to review. PR 0 consolidates duplicated API infrastructure. PRs 1–7 are pure refactors with no behaviour changes. PRs 8 and 9 involve behaviour alignment and structural unification.

---

## Pull Requests

### PR 0 — Consolidate duplicated API infrastructure into `src/types/api.ts` and `src/api/applications.api.ts`

The two deploy flows were built independently and each created their own parallel API layer hitting the same backend. This PR collapses both duplications in one pass.

**Types:** `src/types/digitalAssistants.ts` and `src/services/deployment.api.ts` independently define the same TypeScript interfaces for the same API contracts — `Provider`, `Application`, `ApplicationService`, `ServiceComponent`, `PaginationMetadata`, `ApplicationListResponse`, `FetchApplicationsParams`, `DeleteApplicationResponse`, `DeployApplicationResponse` are all defined twice. This PR creates `src/types/api.ts` as the single home for all API contract types, moves every type definition there, and deletes `src/types/digitalAssistants.ts` entirely. `deployment.api.ts` is stripped of all its type definitions and becomes a pure function-exports file. All import sites are updated to `@/types/api`.

Type names are unified during the move:
- `Component` / `DeployOptionsComponent` — same shape, unified as `Component`
- `Service` / `DeployOptionsService` — same shape, unified as `DeployOptionsService`
- `ResourcesResponse` — confirmed identical in both files (same endpoint), one copy kept
- `ResourcesApiResponse` (in `digitalAssistants.ts`, uses `used_cpu`/`used_bytes`) — different endpoint, renamed to `UsedResourcesResponse` for clarity

**API functions:** `src/api/digitalAssistants.ts` and `src/services/deployment.api.ts` are also parallel API clients hitting the same backend. Both export `fetchResources`, `fetchProviderParams`/`fetchProviderSchema` (same endpoint, different names), `deployApplication`, `calculateUptime`, and `transformApplicationToRow` — all duplicated. They were built separately because each flow used its own API file. This PR merges all function exports into a single `src/api/applications.api.ts`, deletes `src/api/digitalAssistants.ts`, and updates `deployment.api.ts` to re-export from `applications.api.ts` for any consumers that can't be updated atomically. All import sites are updated to `@/api/applications.api`.

This PR is a pure consolidation — no logic changes anywhere. It is independent of all other PRs and can be merged first or in parallel with PR 1.

### PR 1 — Establish the new folder structure, move flow-specific hooks, and delete dead code

This PR lays the foundation for everything that follows. It renames and reorganises the existing component folders into the new structure — `DeployFlow/` becomes `deployFlow/digitalAssistant/`, `ServicesDeployFlow/` becomes `deployFlow/services/`, and the `deployFlow/shared/` directory is created ready to receive shared code in subsequent PRs. It also moves `src/utils/formDataInitializer.ts` into `deployFlow/digitalAssistant/utils/` since it is exclusively used by the digital assistant flow and does not belong in the general utils folder. All internal import paths are updated to match. No logic changes.

It also extracts the first shared utilities into `deployFlow/shared/` — the deploy-error extraction logic and the `getDisplayName` helper, both of which are currently copy-pasted across the two flows.

**Hook moves:** Five hooks in `src/hooks/` are exclusively used inside one of the two deploy flows and have no business being in the global hooks folder. They move into their respective flow folders:

- `useDeployOptions.ts` → `deployFlow/digitalAssistant/hooks/` (only used by `DigitalAssistantDeployFlow` and the `DigitalAssistants` page that opens it)
- `useProviderParams.ts` → `deployFlow/digitalAssistant/hooks/` (only used within `digitalAssistant` components)
- `useServiceParams.ts` → `deployFlow/digitalAssistant/hooks/` (only used by `ServiceConfigCard`)
- `useServiceDeployOptions.ts` → `deployFlow/services/hooks/` (only used by `ServicesDeployFlow`)
- `useProviderSchema.ts` → `deployFlow/services/hooks/` (only used by `ServicesDeployFlow/StepTwo`)

`useServices`, `useSessionTimeout`, and `useUseCases` stay in `src/hooks/` — they are either used outside the deploy flows or unrelated entirely.

**Dead code deletions:**

- `src/components/DynamicField/` — `DynamicField.tsx` renders a single Carbon input from a `ParsedField` with the same `switch(type)` logic as `DynamicSchemaFields`, but is not imported anywhere in the codebase. The shared `DynamicSchemaFields` created in PR 4 supersedes it.
- `src/hooks/useResources.ts` — fetches system resources and caches them in `useDeployStore`, but neither deploy flow imports it. Both flows call `fetchResources` directly from `@/api/applications.api` instead. Dead code with no consumers.

### PR 2 — Extract shared TypeScript types

Move the identical interfaces — `ComponentConfig`, `DeployFormData`, `ServiceConfig`, `ResourceItem`, base `StepProps`, and the shared action types — into `deployFlow/shared/types.ts`. Each flow's own types file shrinks to just its unique additions.

### PR 3 — Extract shared SCSS module

Both stylesheets are around 480 lines and share roughly 300 lines of identical class definitions — layout, resource tiles, service config cards, typography, and responsive breakpoints. Move all of that to a single shared stylesheet so a spacing or token change only needs to happen once.

### PR 4 — Unify DynamicSchemaFields into one component

The biggest single duplication — both flows have their own copy of a component that renders Carbon form fields from a provider schema. The entire field-rendering switch is duplicated. This PR merges them into one and also fixes two inconsistencies identified during the analysis.

The first is a gap in ServicesDeployFlow: provider schemas can contain UI-only fields — checkboxes defined with `x-ui-only: true` in the JSON schema that exist purely to show or hide another field (the system prompt textarea is the current example). They are never sent in the deployment payload; they only control the UI. DeployFlow supports this but ServicesDeployFlow never implemented it — none of the current service provider schemas happen to use it, so it has not been a visible problem yet. However, as new providers or services are added, this could become one. The unified component brings this support to both flows so it is handled correctly from the start.

The second is a validation inconsistency: both flows use the same `validateField` function under the hood, but DeployFlow runs it inline inside the field renderer on every render, while ServicesDeployFlow runs it in the parent on Apply and passes the errors down as props. The outcome looks the same to the user but the logic is split across two layers in one flow and kept in one place in the other. The unified component standardises on the cleaner approach — validation runs in the parent step on submit and the component receives the resulting errors as props, keeping the field renderer purely presentational.

### PR 5 — Extract shared ResourceRequirementsPanel

The Tile-based resource display — CPU, Memory, Accelerators, Storage — with its loading, error, and empty states is identical in both flows. Promote the presentational panel to the shared layer. The resource calculation logic stays separate in each flow because it is genuinely different (global components vs single service).

### PR 6 — Extract shared useDeployFlowReducer hook

Both root components define a `deployFlowReducer` with the same 9 action cases — `SET_CURRENT_STEP`, `SET_IS_DEPLOYING`, `SET_IS_EDITING`, `SET_HAS_INSUFFICIENT_RESOURCES`, `SET_DEPLOY_ERROR`, `SET_FORM_DATA`, `UPDATE_FORM_DATA`, `SET_SHOW_STEP_ONE_NAME_ERROR`, and `RESET_STATE` — and the same three `useCallback` dispatch wrappers: `handleFormDataChange`, `handleEditingChange`, and `handleResourceStatusChange`. This is copied verbatim across both files.

This PR moves the shared reducer cases and callbacks into `useDeployFlowReducer`, a hook in `deployFlow/shared/hooks/`. Each flow calls the hook to get the shared state and callbacks, then handles only its own unique actions on top — `digitalAssistant` adds `SET_IS_LOADING`, `SET_ERROR`, `SHOW_DEPLOY_TOAST`, and `HIDE_DEPLOY_TOAST` (driven by `useDeployOptions` and the deploy toast lifecycle), and `services` adds `SET_SELECTED_SERVICE` (for tracking which standalone service the user picked in StepZero).

### PR 7 — Extract shared DeployTearsheetShell wrapper

The outermost JSX — `Tearsheet` with the vertical progress indicator influencer and the deployment error notification — is structurally the same in both root components. Wrap it in a single shell component that accepts the step definitions and page content as props, and replace the duplicated Tearsheet boilerplate in both flows.

### PR 8 — Unify StepOne into a shared component

Both StepOne components show the same fields to the user — name, version, and a list of component dropdowns (embedding, vector store etc.). The data is the same, it just comes from different keys in the API response (`global_components` vs `components`), and the selection is saved to different parts of the form (`formData.globalComponents` vs `formData.services[selectedServiceId].components`). Both of these differences can be handled cleanly with props — the caller passes in the component list and an `onComponentChange` callback. The shared component itself will not import from either store or call any API — it only receives data as props and calls `onChange` to report selections back. The parent (`digitalAssistant` or `services`) remains responsible for reading from the right store and writing to the right part of the form.

This PR also aligns the selection behaviour between the two flows.

Currently in DeployFlow, the dropdown visually shows a model name — it fetches the provider's schema and uses the first model title as the display text. However the value stored on selection is still the provider ID, not the model. This works today because there is only one provider per component and one model per provider, so they map 1-to-1. But when multiple providers exist each with multiple models, this breaks down — it only ever reads the first model from the first provider's schema and uses that as the label regardless of what else is available. The user is effectively making a provider selection disguised as a model selection.

In ServicesDeployFlow the dropdown options are model names. The user picks a model and the code automatically resolves which provider serves that model and sets it in the background.

The unified component standardises on the ServicesDeployFlow approach. The provider is an infrastructure detail — it is the runtime that serves the model. The user doesn't care which runtime is running, they care which model they are getting. Showing provider names in the dropdown is asking the user to make a decision that the system should make for them. Model names are what the user understands and what they need to make a meaningful choice. The provider resolves itself from the model selection, which is the right direction.

This PR also aligns the provider schema fetching strategy between the two flows. Currently `digitalAssistant` fetches provider schemas lazily — when the user selects a provider from a dropdown, `useProviderParams` fires an API call on demand for that specific `componentType + providerId` pair. This causes a loading spinner mid-flow every time the user makes a selection. The `services` flow does the opposite — `useServiceDeployOptions` fetches all provider schemas eagerly on mount in two stages (Step 1 component models first for immediate display, LLM models in the background), so by the time the user reaches any step everything is already in the store and `useProviderSchema` is just a synchronous store read. This PR migrates `digitalAssistant` to the same eager-fetch pattern: `useDeployOptions` is extended to fetch and cache all component provider schemas upfront when it loads the architecture deploy options. The per-provider lazy hooks (`useProviderParams`, `useServiceParams`) are replaced by a single `useProviderSchema`-style store selector. This eliminates the mid-flow loading states in `digitalAssistant` and makes the hook layer between the two flows structurally identical — both read from the store synchronously in components, both populate the store eagerly on mount.

### PR 9 — Unify StepTwo and ServiceConfigCard into shared components

Both flows render a service configuration card with the same structure — a title, description, an edit/apply/cancel pattern, LLM model selection, inference backend dropdown, reranker, and credential fields. In `digitalAssistant`, this card is a standalone `ServiceConfigCard` component and `StepTwo` loops over multiple services to render one per service. In `services`, the same card is rendered inline inside `StepTwo` for a single service.

This PR moves `ServiceConfigCard` to `shared/`, refactors `services/StepTwo` to use it instead of its inline card, and makes `StepTwo` itself a shared component that accepts a `services` array as a prop — rendering one card when there is a single service and looping when there are multiple. The resource calculation and API wiring remain in each flow's root component and are passed down as props.

This PR also removes an unnecessary data model inconsistency in `digitalAssistant`. Currently `digitalAssistant` tracks the selected LLM/reranker provider via a separate `inferenceBackend` field on `ServiceConfig`, while `services` tracks it directly via `components.llm.providerId`. Looking at the API payload (from the swagger spec), there is no `inference_backend` field — both flows send the same structure: a list of components each with a `component_type` and a `provider_id`. The `inferenceBackend` field is a UI-only concept that exists purely so the payload transform can put the right `provider_id` on the LLM component. But `services` already does this correctly by just reading `components.llm.providerId` directly — no separate field needed. This PR aligns `digitalAssistant` to do the same, removing `inferenceBackend` from `ServiceConfig` and the `inferenceComponentHelper.ts` utility that exists solely to support it.

This is the largest PR in the series because `ServiceConfigCard` is 911 lines and aligning the data model and LLM/reranker model fetching across the two flows requires careful testing. It is kept as the final PR so all the shared infrastructure from PRs 1–8 is already in place before it lands.

---

## Merge Order

PR 0 is fully independent — no other PR depends on it and it does not depend on any other PR. It can be merged first or in parallel with PR 1.

PRs 1, 2, and 3 are independent and can be reviewed in any order. PRs 4 and 5 each need 2 and 3 first. PR 6 needs 2. PR 7 needs 3 and 6. PR 8 needs 7. PR 9 needs 8.

**Note on hook consolidation:** `useProviderParams` and `useServiceParams` move into `deployFlow/digitalAssistant/hooks/` in PR 1 as intermediate steps. In PR 8, both are replaced entirely when `digitalAssistant` adopts the eager-fetch pattern — `useDeployOptions` absorbs all schema fetching upfront and components read from the store via a selector. Both hooks are temporary — they exist only between PR 1 and PR 8. After PR 8 lands, `deployFlow/digitalAssistant/hooks/` contains only `useDeployOptions.ts`.

The straightforward path is **1 → 2 → 3**, then **4, 5, and 6 in parallel**, then **7 → 8 → 9** to close it out.

---

## Out of Scope

The following are intentionally not merged:

- **`StepZero`** — service tile selection, unique to `services` flow
- **Submit and API call logic** — different endpoints, payload transforms, and stores
- **Resource calculation logic** — the aggregation pattern is the same but the inputs are structurally different. `digitalAssistant` must account for global components shared across all services plus each service's own components, with deduplication logic for shared providers. `services` only iterates over a single service's components. Sharing the calculation would require passing so many conditional inputs that it would be harder to follow than the two separate implementations.

---

## File Changes Summary

A complete map of every file touched across all 10 PRs.

### `src/components/`

| Path | Change |
|---|---|
| `DeployFlow/` | Deleted — replaced by `deployFlow/digitalAssistant/` |
| `ServicesDeployFlow/` | Deleted — replaced by `deployFlow/services/` |
| `DynamicField/` | Deleted — dead code, never imported anywhere |
| `deployFlow/` | Created — contains `shared/`, `digitalAssistant/`, and `services/` |
| `AppHeader/`, `AuthRoute/`, `DeployedServicesTable/`, `DeploymentDetails/`, `Navbar/`, `ServiceCard/`, `ServiceDetailPanel/`, `SessionManager/`, `SessionTimeoutModal/`, `SolutionCard/`, `SolutionDetailPanel/` | Untouched |

### `src/api/`

| Path | Change |
|---|---|
| `applications.api.ts` | Created — single canonical API client merging all functions from `digitalAssistants.ts` and `deployment.api.ts` |
| `digitalAssistants.ts` | Deleted — all functions moved to `applications.api.ts` |
| `axios.ts` | Untouched |

### `src/types/`

| Path | Change |
|---|---|
| `api.ts` | Created — single home for all API contract types from both `digitalAssistants.ts` and `deployment.api.ts` |
| `digitalAssistants.ts` | Deleted — every type moves to `api.ts` |
| `auth.ts`, `navigation.types.ts`, `useCase.ts` | Untouched |

### `src/services/`

| Path | Change |
|---|---|
| `deployment.api.ts` | Deleted — all type definitions moved to `src/types/api.ts`, all function implementations moved to `src/api/applications.api.ts`, all consumers updated to import from those locations directly |
| `auth.ts`, `serviceDetails.api.ts` | Untouched |

### `src/hooks/`

| Path                                                       | Change                                                                                              |
| ------------------------------------------------------------| -----------------------------------------------------------------------------------------------------|
| `useDeployOptions.ts` | Moved to `deployFlow/digitalAssistant/hooks/` — extended in PR 8 to fetch all schemas eagerly on mount |
| `useProviderParams.ts` | Moved to `deployFlow/digitalAssistant/hooks/` in PR 1 — deleted in PR 8 when eager-fetch replaces lazy fetching |
| `useServiceParams.ts` | Moved to `deployFlow/digitalAssistant/hooks/` in PR 1 — deleted in PR 8 when eager-fetch replaces lazy fetching |
| `useServiceDeployOptions.ts` | Moved to `deployFlow/services/hooks/` |
| `useProviderSchema.ts` | Moved to `deployFlow/services/hooks/` |
| `useResources.ts` | Deleted — dead code, no consumers |
| `useServices.ts`, `useSessionTimeout.ts`, `useUseCases.ts` | Untouched |

### `src/utils/`

| Path                            | Change                                                                                              |
| ---------------------------------| -----------------------------------------------------------------------------------------------------|
| `formDataInitializer.ts` | Moved to `deployFlow/digitalAssistant/utils/` |
| `deploymentTransform.ts` | Moved to `deployFlow/digitalAssistant/utils/` — renamed to `digitalAssistantDeploymentTransform.ts` |
| `inferenceComponentHelper.ts` | Moved to `deployFlow/digitalAssistant/utils/` — deleted in PR 9 |
| `resourceSharing.ts` | Moved to `deployFlow/digitalAssistant/utils/` |
| `paramFilter.ts` | Moved to `deployFlow/shared/utils/` — used by both deployment transforms after PR 9 unifies them; `serviceDeploymentTransform`'s inline equivalent (`mergeParamsWithUserValues`) is replaced by `shouldIncludeParam` |
| `serviceDeploymentTransform.ts` | Moved to `deployFlow/services/utils/` |
| `schemaParser.ts`, `requestManager.ts`, `csv.ts`, `string.tsx`, `sessionTimeout.ts`, `serviceDetailsTransform.ts` | Untouched |
