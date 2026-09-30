# BYOS — Integrating Bundles into AI-Services

This guide covers uploading, managing, and verifying your BYOS bundle against a live AI-Services deployment. For background, see [01 — Overview](01-overview.md). For authoring and packaging, see [02 — Bundle Structure and Templates](02-bundle-structure.md).

---

## Table of Contents

1. [Managing Bundles with the CLI](#1-managing-bundles-with-the-cli)
   - 1.1 [Prerequisites](#11-prerequisites)
   - 1.2 [Create a Bundle](#12-create-a-bundle)
   - 1.3 [Validate a Bundle Before Creating](#13-validate-a-bundle-before-creating)
   - 1.4 [Update a Bundle](#14-update-a-bundle)
   - 1.5 [List Bundles](#15-list-bundles)
   - 1.6 [Get Bundle Details](#16-get-bundle-details)
   - 1.7 [Delete a Bundle](#17-delete-a-bundle)
2. [Viewing Custom Services in the Catalog UI](#2-viewing-custom-services-in-the-catalog-ui)
3. [Tips and Troubleshooting](#3-tips-and-troubleshooting)

---

## 1. Managing Bundles with the CLI

The `ai-services catalog bundle` subcommand group handles all bundle lifecycle operations. All subcommands require an active catalog session (see §1.1 below).

### 1.1 Prerequisites

**Step 1 — Log in to the catalog backend (once per session):**

```bash
./bin/ai-services catalog login \
  --server https://<catalog-backend-endpoint> \
  --username admin \
  --runtime podman \
  --insecure
```

Credentials are stored locally; subsequent `bundle` commands pick them up automatically — no `--server` or `--username` flags needed.

**Step 2 — Package your bundle:**

```bash
tar -czf my-bundle.tar.gz my-service/
```

### 1.2 Create a Bundle

Creates a new bundle from a `.tar.gz` archive. The command is synchronous — it blocks until the server returns `201 Created` and the bundle is `active`. All metadata (`id`, `type`, `version`) is read from `metadata.yaml` inside the archive; no extra flags are needed.

```bash
./bin/ai-services catalog bundle create --file my-bundle.tar.gz
```

**Example output (service bundle):**

```
Creating bundle from my-bundle.tar.gz...
✓ Bundle created successfully
  ID:           550e8400-e29b-41d4-a716-446655440000
  Catalog type: service
  Catalog ID:   my-service
  Dir name:     my-service-1.0.0
  Version:      1.0.0
  Status:       active
  Size:         280 KB
```

**Example output (component bundle with `component_type: llm`):**

```
Creating bundle from my-provider-bundle.tar.gz...
✓ Bundle created successfully
  ID:             c3d4e5f6-...
  Catalog type:   component
  Component type: llm
  Catalog ID:     llm--my-provider
  Version:        1.0.0
  Status:         active
  Size:           192 KB
```

**Common errors:**

| Condition | Error |
|---|---|
| Bundle with same `catalog_id` already exists | `409 Conflict` — use `bundle update` instead |
| `metadata.yaml` missing, malformed, or ID is reserved | `422 Unprocessable Entity` |
| File not found or not a `.tar.gz` | Exit `1` immediately |

### 1.3 Validate a Bundle Before Creating

Validates the archive structure and `metadata.yaml` without writing anything to disk or the database. Use this in CI/CD pipelines before promoting to a live deployment.

```bash
./bin/ai-services catalog bundle validate --file my-bundle.tar.gz
```

**Example output (valid service bundle):**

```
Validating bundle from my-bundle.tar.gz...
✓ Bundle is valid
  Catalog type: service
  Catalog ID:   my-service
  Dir name:     my-service-1.0.0
  Version:      1.0.0
  Name:         My Custom Service
```

**Example output (valid component bundle):**

```
Validating bundle from my-provider-bundle.tar.gz...
✓ Bundle is valid
  Catalog type:   component
  Component type: llm
  Catalog ID:     llm--my-provider
  Version:        1.0.0
  Name:           My Custom LLM Provider
```

**Example output (invalid bundle):**

```
Validating bundle from broken-bundle.tar.gz...
✗ Bundle validation failed: metadata.yaml is missing required field "component_type"
```

### 1.4 Update a Bundle

Replaces an existing bundle by its internal UUID. The `bundle_id` is the UUID shown in `bundle list` or the `bundle create` output. The replacement `metadata.yaml` inside the archive must carry the same `id`, `type`, and (for components) `component_type` as the existing record — mismatches are rejected with `422`.

> **Important:** If any service or component is currently running that was deployed from this bundle, the update is rejected with `409 Conflict`. Stop or delete those running instances first, then retry.

```bash
./bin/ai-services catalog bundle update <bundle_id> --file my-bundle-v2.tar.gz
```

**Example:**

```bash
./bin/ai-services catalog bundle update 550e8400-e29b-41d4-a716-446655440000 \
  --file my-bundle-v2.tar.gz
```

**Example output:**

```
Updating bundle 550e8400-e29b-41d4-a716-446655440000 from my-bundle-v2.tar.gz...
  Catalog type: service  |  Catalog ID: my-service  |  Version: 2.0.0
  Dir name:     my-service-2.0.0
✓ Bundle updated successfully (status: active)
```

### 1.5 List Bundles

Prints a table of all registered bundles, ordered by creation time (most recent first).

```bash
./bin/ai-services catalog bundle list
```

**Example output:**

```
ID                                     CATALOG TYPE  CATALOG ID        VERSION  STATUS  CREATED AT
550e8400-e29b-41d4-a716-446655440000   service       my-service        1.0.0    active  2026-05-12 09:14:02
a1b2c3d4-e5f6-7890-abcd-ef1234567890   component     llm--my-provider  1.0.0    active  2026-05-13 11:30:00
```

### 1.6 Get Bundle Details

Prints full details for a single bundle by its UUID.

```bash
./bin/ai-services catalog bundle get <bundle_id>
```

**Example output:**

```
ID:           550e8400-e29b-41d4-a716-446655440000
Name:         My Custom Service
Dir name:     my-service-1.0.0
Catalog type: service
Catalog ID:   my-service
Version:      1.0.0
Status:       active
Size:         280 KB
Created by:   admin
Created at:   2026-05-12 09:14:02
```

### 1.7 Delete a Bundle

Permanently removes a bundle: deletes the on-disk directory, triggers a `CatalogProvider` reload so the item is no longer served, and removes the database record.

> **Important:** If any service or component is currently running from this bundle, the delete is rejected with `409 Conflict`. Stop or delete those running instances first.

```bash
./bin/ai-services catalog bundle delete <bundle_id>
```

Use `--yes` to skip the confirmation prompt (useful in scripts):

```bash
./bin/ai-services catalog bundle delete 550e8400-e29b-41d4-a716-446655440000 --yes
```

**Example output:**

```
Delete bundle 550e8400-e29b-41d4-a716-446655440000 (my-service-1.0.0)? [y/N] y
✓ Bundle deleted.
```

> Deleting a bundle does **not** affect any applications that were already deployed from it — existing running pods are independent of the catalog once launched.

---

## 2. Viewing Custom Services in the Catalog UI

Once a bundle is successfully created and its status is `active`, the custom service or component is immediately available in the platform — no restart is needed.

**Step 1 — Open the Catalog UI.**

Navigate to the Catalog UI endpoint (output when you ran `catalog configure`):

```bash
./bin/ai-services catalog info --runtime podman
```

This prints the Catalog UI URL.

**Step 2 — Go to the Services Catalog page.**

In the Catalog UI, click **Services Catalog** in the left navigation. Custom services appear alongside the built-in ones, each showing the `name` from your `metadata.yaml`.

**Step 3 — Deploy an application from the custom service.**

Select your custom service from the catalog, configure any parameters exposed by `values.schema.json`, and deploy. You can also use the CLI:

```bash
# List all available service templates, including custom ones
./bin/ai-services application templates --runtime podman

# View parameters for your custom service
./bin/ai-services application templates parameters \
  --template my-service \
  --runtime podman

# Create an application
./bin/ai-services application create my-deployment \
  --template my-service \
  --runtime podman
```

**Verifying the bundle is loaded via the API:**

```bash
# List all services — custom ones appear alongside built-in services
curl -s https://<catalog-backend-endpoint>/api/v1/services \
  -H "Authorization: Bearer $(cat token.txt)" | jq '.[].id'
# "chat"
# "digitize"
# "similarity"
# "summarize"
# "my-service"   ← your custom service
```

---

## 3. Tips and Troubleshooting

**Bundle rejected with `409 Conflict` on `bundle create`**

A bundle with the same `catalog_id` already exists. Use `bundle list` to find its ID and `bundle update` to replace it:

```bash
./bin/ai-services catalog bundle list
./bin/ai-services catalog bundle update <bundle_id> --file my-bundle-v2.tar.gz
```

**Bundle rejected with `422 Unprocessable Entity`**

Common causes:
- `metadata.yaml` is missing from the archive root.
- A required field (`id`, `type`, `version`, or `component_type` for components) is missing.
- The `catalog_id` matches a reserved built-in ID (see [Reserved IDs](01-overview.md#reserved-ids)).
- The `podman/metadata.yaml` or `values.yaml` is missing.
- A template file has a syntax error.

Run `bundle validate` first to catch errors before uploading:

```bash
./bin/ai-services catalog bundle validate --file my-bundle.tar.gz
```

**Bundle rejected with `409 Conflict` on `bundle update` or `bundle delete`**

A running service or component is using this bundle's `catalog_id`. Stop or delete the running application first:

```bash
./bin/ai-services application ps --runtime podman
./bin/ai-services application delete <app-name> --runtime podman
# Then retry the update or delete
```

**Custom service does not appear in the UI after upload**

Check that the bundle status is `active`:

```bash
./bin/ai-services catalog bundle list
```

If the status is `failed`, get more details:

```bash
./bin/ai-services catalog bundle get <bundle_id>
```

A `failed` status means an error occurred during extraction or reload after the archive was accepted. Fix the issue and re-upload using `bundle update`.

**Getting help**

```bash
./bin/ai-services catalog bundle --help
./bin/ai-services catalog bundle create --help
./bin/ai-services catalog bundle validate --help
```
