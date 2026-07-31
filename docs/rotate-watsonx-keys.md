# Rotating Watsonx API Keys

How to rotate an IBM Watsonx API key for an AI Services application running on the Podman runtime (PowerVS).

## Prerequisites

- The Catalog UI is deployed and accessible. See [Configuring with PowerVS and IBM watsonx](https://www.ibm.com/docs/en/aiservices/2026.06.0?topic=podman-configuring-powervs-watsonx).
- You have the new Watsonx API key, project ID, and Watsonx URL ready.

## Before you begin

Rotating a key requires deleting and recreating the application. Each new application gets a new internal ID, so all Podman volumes (ingested documents, vector store) are created under a new name and the old data becomes unreachable.

**Back up your data before proceeding.** The backup and restore steps apply to the following deployments:

| Deployment | Backup needed | Targets |
|---|---|---|
| `rag` (Digital Assistant) | ✅ Yes | `opensearch`, `digitize` |
| `digitize` standalone | ✅ Yes | `opensearch`, `digitize` |
| `summarize` standalone | ⚠️ No backup target available — data will be lost | — |
| `chat` standalone | No persistent data | — |

If you have no ingested data to preserve, skip to [Step 3](#3-delete-the-application).

> **Note:** Backup and restore for the `summarize` standalone service is not currently supported. Job history stored in its Postgres database cannot be recovered after deletion.

## Steps

### 1. Back up your data

Use the CLI to back up both OpenSearch and digitize data. Backup is not available in the Catalog UI.

```bash
ai-services application backup <app-name> --target opensearch --runtime podman
ai-services application backup <app-name> --target digitize --runtime podman
```

Note the generated `.tar.gz` filenames — you will need them to restore.

### 2. Set the new credentials

Export your new Watsonx API key, project ID, and URL in the terminal before proceeding. This is required for the CLI path. If using the Catalog UI to redeploy, have these values ready to enter in the deploy form.

### 3. Delete the application

**CLI:** Delete the application using the CLI.

**Catalog UI:** Open the application, click the overflow menu, and select **Delete**.

### 4. Recreate the application with the new key

**CLI:** Create the application using the new exported credentials.

**Catalog UI:** Deploy the application again from the Catalog and enter the new Watsonx API key in the deploy form.

### 5. Restore your data

Use the CLI to restore both targets into the newly created application. Restore is not available in the Catalog UI.

```bash
ai-services application restore <app-name> --target opensearch --filename <opensearch-backup>.tar.gz --runtime podman --yes
ai-services application restore <app-name> --target digitize --filename <digitize-backup>.tar.gz --runtime podman --yes
```

### 6. Verify

Access the Q&A interface and send a test query to confirm the application is working correctly with the new key.
