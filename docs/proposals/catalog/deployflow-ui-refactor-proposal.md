# DeployFlow Refactor — Engineering Proposal

**Components:** `deployFlow/DigitalAssistant` & `deployFlow/Services`

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
    DigitalAssistant/
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
    Services/
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
- **`useDeployFlowReducer.ts`** — a React hook that owns the shared state management: the reducer, the action types, and the four callbacks (`handleFormDataChange`, `handleEditingChange`, `handleResourceStatusChange`, `handleBack`) that are identical in both root components. It also includes `SHOW_DEPLOY_TOAST` and `HIDE_DEPLOY_TOAST` since the shell renders the toast and those actions need to live alongside the state the shell consumes. Each flow calls this hook and gets the shared state and callbacks back, then adds only its own flow-specific actions on top using a wrapping reducer — the flow reducer handles its own action types and falls through to the shared reducer for everything else. `services` adds `SET_SELECTED_SERVICE` to track which standalone service the user picked in StepZero.
- **`DeployTearsheetShell.tsx`** — a wrapper component that renders the `Tearsheet`, the vertical `ProgressIndicator` in the influencer panel(the optional side panel), and the deployment error toast notification. Each flow passes in its own step labels and the content for the current step as children. The shell handles all the surrounding chrome.

**What stays unique to each flow:**

`deployFlow/DigitalAssistant/` keeps:

- Its own API calls (`deployApplication`, `fetchServices`) and store (`useDeployStore`)
- Its own form initialisation logic
- `ResourceRequirements.tsx` — fetches available system resources and calculates totals accounting for global components plus every enabled service, then passes the results to the shared `ResourceRequirementsPanel` to display

`deployFlow/Services/` keeps:

- Its own API calls (`deployApplication` via `applications.api`) and store (`useServiceDeployStore`)
- `StepZero` — the service selection screen where the user picks which standalone service to deploy
- `ResourceRequirements.tsx` — fetches available system resources and calculates totals for a single service only, then passes the results to the shared `ResourceRequirementsPanel` to display
- `formDataInitializer.ts` — initialises the form data structure when a service is selected

The work is split into 12 PRs, each under 500 lines of diff so they stay easy to review. PR 0 consolidates duplicated API infrastructure. PRs 1–3 and 5–7 are pure refactors with no behaviour changes. PR 4 is mostly structural but includes two behaviour changes called out below. PRs 8a, 8b, 8c, and 9 involve behaviour alignment and structural unification.

---

## Pull Requests

### PR 0 — Consolidate duplicated API infrastructure into `src/types/api.types.ts` and `src/api/applications.api.ts`

The two deploy flows were built independently and each created their own parallel API layer hitting the same backend. This PR collapses both duplications in one pass.

**Types:** `src/types/digitalAssistants.ts` and `src/services/deployment.api.ts` independently define the same TypeScript interfaces for the same API contracts — `Provider`, `Application`, `ApplicationService`, `ServiceComponent`, `PaginationMetadata`, `ApplicationListResponse`, `FetchApplicationsParams`, `DeleteApplicationResponse`, `DeployApplicationResponse` are all defined twice. This PR creates `src/types/api.types.ts` as the single home for all API contract types, moves every type definition there, and deletes `src/types/digitalAssistants.ts` entirely. `deployment.api.ts` is deleted in full — all its type definitions and function implementations move to their new homes. All import sites are updated to `@/types/api.types`.

Type names are unified during the move:
- `Component` / `DeployOptionsComponent` — same shape, unified as `DeployOptionsComponent`
- `Service` / `DeployOptionsService` — same shape, unified as `DeployOptionsService`
- `ResourcesResponse` — confirmed identical in both files (same endpoint), one copy kept
- `ResourcesApiResponse` (in `digitalAssistants.ts`, uses `used_cpu`/`used_bytes`) — different endpoint, renamed to `UsedResourcesResponse` for clarity

**API functions:** `src/api/digitalAssistants.ts` and `src/services/deployment.api.ts` are also parallel API clients hitting the same backend. Both export `fetchResources`, `fetchProviderParams`/`fetchProviderSchema` (same endpoint, different names), `deployApplication`, `calculateUptime`, and `transformApplicationToRow` — all duplicated. They were built separately because each flow used its own API file. This PR merges all function exports into a single `src/api/applications.api.ts`, deletes both `src/api/digitalAssistants.ts` and `src/services/deployment.api.ts`, and updates all import sites to `@/api/applications.api`.

One difference comes up during the merge: the two `deployApplication` implementations send different payloads — the DA flow posts without a `deployment_type` field, the services flow posts with `deployment_type: "service"`. Rather than forcing them into one shared type (which would break the DA call), both contracts are modelled explicitly in `src/types/api.types.ts` as `ArchitectureDeploymentPayload` and `ServiceDeploymentPayload`. `DeploymentComponent` and `DeploymentService` are also moved into `src/types/api.types.ts` so both transforms share them. The `deployment_type: "service"` value moves into the services transform itself so the caller doesn't need to add it manually. This PR is independent of all other PRs and can be merged first or in parallel with PR 1.

### PR 1 — Establish the new folder structure, move flow-specific hooks, and delete dead code

This PR lays the foundation for everything that follows. It renames and reorganises the existing component folders into the new structure — `DeployFlow/` becomes `deployFlow/DigitalAssistant/`, `ServicesDeployFlow/` becomes `deployFlow/Services/`, and the `deployFlow/shared/` directory is created ready to receive shared code in subsequent PRs. It also moves `src/utils/formDataInitializer.ts` into `deployFlow/DigitalAssistant/utils/` since it is exclusively used by the digital assistant flow and does not belong in the general utils folder. All internal import paths are updated to match. No logic changes.

It also extracts the first shared utilities into `deployFlow/shared/` — the deploy-error extraction logic and the `getDisplayName` helper. Both are currently copy-pasted across the two flows.

**Hook moves:** Five hooks in `src/hooks/` are exclusively used inside one of the two deploy flows and have no business being in the global hooks folder. They move into their respective flow folders:

- `useDeployOptions.ts` → `deployFlow/DigitalAssistant/hooks/` (only used by `DigitalAssistantDeployFlow` and the `DigitalAssistants` page that opens it)
- `useProviderParams.ts` → `deployFlow/DigitalAssistant/hooks/` (only used within `digitalAssistant` components)
- `useServiceParams.ts` → `deployFlow/DigitalAssistant/hooks/` (only used by `ServiceConfigCard`)
- `useServiceDeployOptions.ts` → `deployFlow/Services/hooks/` (only used by `ServicesDeployFlow`)
- `useProviderSchema.ts` → `deployFlow/Services/hooks/` (only used by `ServicesDeployFlow/StepTwo`)

`useServices`, `useSessionTimeout`, and `useUseCases` stay in `src/hooks/` — they are either used outside the deploy flows or unrelated entirely.

**Dead code deletions:**

- `src/components/DynamicField/` — `DynamicField.tsx` renders a single Carbon input from a `ParsedField` with the same `switch(type)` logic as `DynamicSchemaFields`, but is not imported anywhere in the codebase. The shared `DynamicSchemaFields` created in PR 4 supersedes it.
- `src/hooks/useResources.ts` — fetches system resources and caches them in `useDeployStore`, but neither deploy flow imports it. Both flows call `fetchResources` directly from `@/api/applications.api` instead. Dead code with no consumers.

### PR 2 — Extract shared TypeScript types

Move the shared interfaces — `ComponentConfig`, `DeployFormData`, `ServiceConfig`, `ResourceItem`, base `StepProps`, and the shared action types — into `deployFlow/shared/types.ts`. Each flow's own types file shrinks to just its unique additions. Not all of these are identical across both flows — where differences exist, the shared type covers the common fields and each flow's local file extends it with whatever is unique to that flow.

### PR 3 — Extract shared SCSS module

Both stylesheets are around 480 lines and share roughly 300 lines of identical class definitions — layout, resource tiles, service config cards, typography, and responsive breakpoints. Move all of that to a single shared stylesheet so a spacing or token change only needs to happen once.

### PR 4 — Unify DynamicSchemaFields into one component

The biggest single duplication — both flows have their own copy of a component that renders Carbon form fields from a provider schema. The entire field-rendering switch is duplicated. This PR merges them into one. It also addresses two inconsistencies: the first is a behaviour change, the second is structural only.

The first is a gap in ServicesDeployFlow: provider schemas can contain UI-only fields — checkboxes defined with `x-ui-only: true` in the JSON schema that exist purely to show or hide another field (the system prompt textarea is the current example). They are never sent in the deployment payload; they only control the UI. DeployFlow supports this but ServicesDeployFlow never implemented it — none of the current service provider schemas happen to use it, so it has not been a visible problem yet. However, as new providers or services are added, this could become one. The unified component brings this support to both flows so it is handled correctly from the start. This is a behaviour change for the services flow: any future schema that includes `x-ui-only` fields will render them where it previously would not have.

The second is a structural inconsistency in how validation errors are computed: both flows show errors only after the user clicks Apply (same timing, same user experience), but DeployFlow computes them inline inside the field renderer via `validateField`, while ServicesDeployFlow receives pre-computed `fieldErrors` from the parent as props. The unified component standardises on the props approach — validation runs in the parent on Apply and the results are passed down — keeping the field renderer purely presentational. No behaviour change for users.

### PR 5 — Extract shared ResourceRequirementsPanel

The Tile-based resource display — CPU, Memory, Accelerators, Storage — with its loading, error, and empty states is identical in both flows. Promote the presentational panel to the shared layer. The resource calculation logic stays separate in each flow because it is genuinely different (global components vs single service).

### PR 6 — Extract shared useDeployFlowReducer hook

Both root components define a `deployFlowReducer` with the same 9 action cases — `SET_CURRENT_STEP`, `SET_IS_DEPLOYING`, `SET_IS_EDITING`, `SET_HAS_INSUFFICIENT_RESOURCES`, `SET_DEPLOY_ERROR`, `SET_FORM_DATA`, `UPDATE_FORM_DATA`, `SET_SHOW_STEP_ONE_NAME_ERROR`, and `RESET_STATE` — and the same four callbacks: `handleFormDataChange`, `handleEditingChange`, and `handleResourceStatusChange` (all `useCallback`), and `handleBack` (plain function in both). This is copied verbatim across both files.

This PR moves the shared reducer cases and callbacks into `useDeployFlowReducer`, a hook in `deployFlow/shared/hooks/`. `SHOW_DEPLOY_TOAST` and `HIDE_DEPLOY_TOAST` also move into the shared reducer because those actions need to live alongside the state the shell consumes — the shell renders the toast and reads that state directly. These two cases currently only exist in the DA flow reducer, so they are added to the services flow reducer here as part of the extraction. Each flow then wraps the shared reducer with its own — the flow reducer handles its own action types and falls through to the shared reducer for everything else. At this point `digitalAssistant` still adds `SET_IS_LOADING` and `SET_ERROR` (driven by `useDeployOptions`), and `services` adds `SET_SELECTED_SERVICE`. Both `SET_IS_LOADING` and `SET_ERROR` are removed from the DA flow reducer in PR 8a — `useDeployOptions` is updated to return them as plain values instead, matching how `useServiceDeployOptions` already works. After PR 8a lands, `digitalAssistant` adds no extra actions of its own.

### PR 7 — Extract shared DeployTearsheetShell wrapper

The outermost JSX — `Tearsheet` with the vertical progress indicator influencer and the deployment error notification — is structurally the same in both root components. Wrap it in a single shell component that accepts the step definitions and page content as props, and replace the duplicated Tearsheet boilerplate in both flows.

### PR 8a — Standardise eager schema fetching across both flows

`digitalAssistant` currently fetches provider schemas lazily — when the user selects a provider, `useProviderParams` fires an API call on demand, causing a loading spinner mid-flow. This PR migrates it to the same eager-fetch pattern that `services` already uses: `useDeployOptions` is extended to fetch and cache all component provider schemas upfront on mount. The per-provider lazy hooks (`useProviderParams`, `useServiceParams`) are deleted and replaced by a store selector. `useDeployOptions` is also updated to return `isLoading` and `error` as plain values rather than dispatching them into the reducer, matching how `useServiceDeployOptions` works. No UI changes — the only visible difference is fewer mid-flow loading spinners.

This PR also upgrades the fetch pattern in both flows from `Promise.all` with individual catches to `Promise.allSettled` through the existing request-manager deduplication utility. `services` already does eager fetching but uses `Promise.all` — this brings both hooks to the same standard at the same time so neither flow ends up with a less robust pattern than the other.

**Scale assumption:** eager fetch fires one GET `.../params` per `(component × provider)` pair across global components and every service. For the current RAG architecture this is a small fixed set of requests. This assumption should be revisited if a significantly larger architecture (e.g. 10+ services) is introduced. All requests are fired in parallel via `Promise.allSettled` through the existing request-manager deduplication utility, not sequentially.

**Failure mode:** lazy fetch fails at the point of selection with a local error scoped to that one provider. Eager fetch can partially fail at mount — some schemas load, others don't. Failed schemas are never written to the store, so closing and reopening the tearsheet automatically triggers a re-fetch for them. If any schemas are in an error state when the flow opens, an inline `InlineNotification` with `kind="warning"` is rendered at the top of StepOne — the step where the affected component dropdowns appear — listing the component types that failed to load (e.g. "Some component configurations failed to load — embedding, vector store. Close and reopen to try again."). This notification is scoped to StepOne and owned there; it does not go through the shell's toast or the shared reducer, so there is no risk of two error surfaces stacking and no new wiring is needed in `DeployTearsheetShell`. Successfully loaded schemas are unaffected and the flow remains usable.

### PR 8b — Extract shared StepOne component

Both StepOne components show the same fields — name, version, and a list of component dropdowns (embedding, vector store etc.). The data comes from different API keys (`global_components` vs `components`) and is saved to different parts of the form, but both differences are handled with props — the caller passes in the component list and an `onComponentChange` callback. The shared component does not touch any store or API — it only receives data as props and calls `onChange` to report selections back. Pure structural change, no behaviour differences.

### PR 8c — Switch to model-first selection in digitalAssistant

Currently the component dropdowns in DeployFlow show a model name as the label but store the provider ID on selection. This works today because there is one provider per component and one model per provider, so they map 1-to-1. With multiple providers each having multiple models this breaks — the label is always the first model from the first provider regardless of what the user picked.

ServicesDeployFlow already does this correctly — the user picks a model and the provider is resolved automatically in the background. This PR aligns `digitalAssistant` to the same approach. The provider is an infrastructure detail the user doesn't need to think about — they care which model they're getting, not which runtime serves it. This is a user-visible UX change and is kept as its own PR so it can be tested and reverted independently.

### PR 9 — Unify StepTwo and ServiceConfigCard into shared components

Both flows render a service configuration card with the same structure — a title, description, an edit/apply/cancel pattern, LLM model selection, inference backend dropdown, reranker, and credential fields. In `digitalAssistant`, this card is a standalone `ServiceConfigCard` component and `StepTwo` loops over multiple services to render one per service. In `services`, the same card is rendered inline inside `StepTwo` for a single service.

This PR moves `ServiceConfigCard` to `shared/`, refactors `services/StepTwo` to use it instead of its inline card, and makes `StepTwo` itself a shared component that accepts a `services` array as a prop — rendering one card when there is a single service and looping when there are multiple. The resource calculation and API wiring remain in each flow's root component and are passed down as props.

This PR also removes an unnecessary data model inconsistency in `digitalAssistant`. Currently `digitalAssistant` tracks the selected LLM/reranker provider via a separate `inferenceBackend` field on `ServiceConfig`, while `services` tracks it directly via `components.llm.providerId`. Looking at the API payload (from the swagger spec), there is no `inference_backend` field — both flows send the same structure: a list of components each with a `component_type` and a `provider_id`. The `inferenceBackend` field is a UI-only concept that exists purely so the payload transform can put the right `provider_id` on the LLM component. But `services` already does this correctly by just reading `components.llm.providerId` directly — no separate field needed. This PR aligns `digitalAssistant` to do the same, removing `inferenceBackend` from `ServiceConfig` and the `inferenceComponentHelper.ts` utility that exists solely to support it.

This is the largest PR in the series because `ServiceConfigCard` is 911 lines and aligning the data model and LLM/reranker model fetching across the two flows requires careful testing. It is kept as the final PR so all the shared infrastructure from PRs 1–8c is already in place before it lands.

---

## Merge Order

PR 0 is fully independent — no other PR depends on it and it does not depend on any other PR. It can be merged first or in parallel with PR 1.

PRs 1, 2, and 3 are independent and can be reviewed in any order. PRs 4 and 5 each need both 2 and 3 first. PR 6 needs only 2 (it has no dependency on 3). PR 7 needs both 3 and 6. PRs 8a, 8b, and 8c each need 7, and can be reviewed in parallel but must merge in order (8a → 8b → 8c) since 8b needs 7 (which includes the shared reducer and shell) and the store selector introduced in 8a, and 8c depends on the shared StepOne from 8b. PR 9 needs 8c.

**Note on hook consolidation:** `useProviderParams` and `useServiceParams` move into `deployFlow/DigitalAssistant/hooks/` in PR 1 as intermediate steps. In PR 8a, both are deleted when `digitalAssistant` adopts the eager-fetch pattern. Both hooks are temporary — they exist only between PR 1 and PR 8a. After PR 8a lands, `deployFlow/DigitalAssistant/hooks/` contains only `useDeployOptions.ts`.

**Note on inferenceComponentHelper.ts:** `inferenceComponentHelper.ts` moves into `deployFlow/DigitalAssistant/utils/` in PR 1 as an intermediate step and is deleted in PR 9 when `digitalAssistant` aligns its data model to read `components.llm.providerId` directly. It is a temporary file — it exists only between PR 1 and PR 9 and is absent from the folder tree above, which reflects the final state only.

The straightforward path is **1 → 2 → 3**, then **2 → 6** and **2+3 → 4 and 5** (6 can start as soon as 2 lands; 4 and 5 must wait for both 2 and 3), then **3+6 → 7 → 8a → 8b → 8c → 9** to close it out.

---

## Out of Scope

The following are intentionally not merged:

- **`StepZero`** — service tile selection, unique to `services` flow
- **Submit and API call logic** — different endpoints, payload transforms, and stores
- **Resource calculation logic** — the aggregation pattern is the same but the inputs are structurally different. `digitalAssistant` must account for global components shared across all services plus each service's own components, with deduplication logic for shared providers. `services` only iterates over a single service's components. Sharing the calculation would require passing so many conditional inputs that it would be harder to follow than the two separate implementations.

---

## File Changes Summary

A complete map of every file touched across all 12 PRs.

### `src/components/`

| Path | Change |
|---|---|
| `DeployFlow/` | Deleted — replaced by `deployFlow/DigitalAssistant/` |
| `ServicesDeployFlow/` | Deleted — replaced by `deployFlow/Services/` |
| `DynamicField/` | Deleted — dead code, never imported anywhere |
| `deployFlow/` | Created — contains `shared/`, `DigitalAssistant/`, and `Services/` |
| `AppHeader/`, `AuthRoute/`, `DeployedServicesTable/`, `DeploymentDetails/`, `Navbar/`, `ServiceCard/`, `ServiceDetailPanel/`, `SessionManager/`, `SessionTimeoutModal/`, `SolutionCard/`, `SolutionDetailPanel/` | Untouched |

### `src/api/`

| Path                          | Change                                                                                                          |
| ------------------------------| -----------------------------------------------------------------------------------------------------------------|
| `applications.api.ts`         | Created — single canonical API client merging all functions from `digitalAssistants.ts` and `deployment.api.ts` |
| `src/api/digitalAssistants.ts` | Deleted — all functions moved to `applications.api.ts`                                                          |
| `axios.ts`                    | Untouched                                                                                                       |

### `src/types/`

| Path                                           | Change                                                                                                    |
| ------------------------------------------------| -----------------------------------------------------------------------------------------------------------|
| `api.types.ts`                                 | Created — single home for all API contract types from both `digitalAssistants.ts` and `deployment.api.ts` |
| `digitalAssistants.ts`                         | Deleted — every type moves to `api.types.ts`                                                              |
| `auth.ts`, `navigation.types.ts`, `useCase.ts` | Untouched                                                                                                 |

### `src/services/`

| Path | Change |
|---|---|
| `deployment.api.ts` | Deleted — all type definitions moved to `src/types/api.types.ts`, all function implementations moved to `src/api/applications.api.ts`, all consumers updated to import from those locations directly |
| `auth.ts`, `serviceDetails.api.ts` | Untouched |

### `src/hooks/`

| Path                                                       | Change                                                                                                           |
| ------------------------------------------------------------| ------------------------------------------------------------------------------------------------------------------|
| `useDeployOptions.ts`                                      | Moved to `deployFlow/DigitalAssistant/hooks/` — extended in PR 8a to fetch all schemas eagerly on mount          |
| `useProviderParams.ts`                                     | Moved to `deployFlow/DigitalAssistant/hooks/` in PR 1 — deleted in PR 8a when eager-fetch replaces lazy fetching |
| `useServiceParams.ts`                                      | Moved to `deployFlow/DigitalAssistant/hooks/` in PR 1 — deleted in PR 8a when eager-fetch replaces lazy fetching |
| `useServiceDeployOptions.ts`                               | Moved to `deployFlow/Services/hooks/`                                                                            |
| `useProviderSchema.ts`                                     | Moved to `deployFlow/Services/hooks/`                                                                            |
| `useResources.ts`                                          | Deleted — dead code, no consumers                                                                                |
| `useServices.ts`, `useSessionTimeout.ts`, `useUseCases.ts` | Untouched                                                                                                        |

### `src/utils/`

| Path                                                                                                              | Change                                                                                                                                                                                                               |
| -------------------------------------------------------------------------------------------------------------------| ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `formDataInitializer.ts`                                                                                          | Moved to `deployFlow/DigitalAssistant/utils/` (DA-specific initialiser; the Services-scoped `ServicesDeployFlow/utils/formDataInitializer.ts` moves to `deployFlow/Services/utils/` separately)                      |
| `deploymentTransform.ts`                                                                                          | Moved to `deployFlow/DigitalAssistant/utils/` in PR 1 — renamed to `digitalAssistantDeploymentTransform.ts`                                                                                                          |
| `inferenceComponentHelper.ts`                                                                                     | Moved to `deployFlow/DigitalAssistant/utils/` — deleted in PR 9                                                                                                                                                      |
| `resourceSharing.ts`                                                                                              | Moved to `deployFlow/DigitalAssistant/utils/`                                                                                                                                                                        |
| `paramFilter.ts`                                                                                                  | Moved to `deployFlow/shared/utils/` in PR 9 — used by both deployment transforms once PR 9 unifies them; `serviceDeploymentTransform`'s inline equivalent (`mergeParamsWithUserValues`) is replaced by `shouldIncludeParam` |
| `serviceDeploymentTransform.ts`                                                                                   | Moved to `deployFlow/Services/utils/`                                                                                                                                                                                |
| `schemaParser.ts`, `requestManager.ts`, `csv.ts`, `string.tsx`, `sessionTimeout.ts`, `serviceDetailsTransform.ts` | Untouched                                                                                                                                                                                                            |
