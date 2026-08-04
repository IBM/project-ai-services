# Rotating Watsonx API Keys

How to rotate an IBM Watsonx API key for an AI Services application running on the Podman runtime (PowerVS).

## Prerequisites

- The Catalog UI is deployed and accessible. See [Configuring with PowerVS and IBM watsonx](https://www.ibm.com/docs/en/aiservices/2026.06.0?topic=podman-configuring-powervs-watsonx).
- You have the new Watsonx API key ready.

## Approach 1 — Script (no data loss)

Rotates the key by replacing only the LiteLLM pod. All ingested data is preserved.

```bash
export WATSONX_APIKEY_NEW="<your-new-api-key>"
./hack/rotate-watsonx-key/rotate-watsonx-key.sh <app-name>
```

The script must be run from within the repository. After it completes, access the Q&A interface and send a test query to confirm the application is working correctly with the new key.

> **Note:** This approach is not available in the Catalog UI.

## Approach 2 — Delete and recreate (data loss without backup)

Deletes and recreates the entire application. Use this if the script approach is not available.

### Before you begin

Each new application gets a new internal ID so all data volumes become unreachable after deletion. Back up your data first if you have ingested documents to preserve.

| Deployment | Backup needed | Targets |
|---|---|---|
| `rag` (Digital Assistant) | ✅ Yes | `opensearch`, `digitize` |
| `digitize` standalone | ✅ Yes | `opensearch`, `digitize` |
| `summarize` standalone | ⚠️ No backup target available — data will be lost | — |
| `chat` standalone | No persistent data | — |

### Steps

**1. Back up your data** (CLI only — skip if no data to preserve)

```bash
ai-services application backup <app-name> --target opensearch --runtime podman
ai-services application backup <app-name> --target digitize --runtime podman
```

**2. Set the new credentials**

Export your new Watsonx API key, project ID, and URL before proceeding. If using the Catalog UI, have these values ready to enter in the deploy form.

**3. Delete the application**

**CLI:** Delete the application using the CLI.

**Catalog UI:** Open the application, click the overflow menu, and select **Delete**.

**4. Recreate the application with the new key**

**CLI:** Create the application using the new exported credentials.

**Catalog UI:** Deploy the application again from the Catalog and enter the new Watsonx API key in the deploy form.

**5. Restore your data** (CLI only — skip if no backup was taken)

```bash
ai-services application restore <app-name> --target opensearch --filename <opensearch-backup>.tar.gz --runtime podman --yes
ai-services application restore <app-name> --target digitize --filename <digitize-backup>.tar.gz --runtime podman --yes
```

**6. Verify**

Access the Q&A interface and send a test query to confirm the application is working correctly with the new key.
