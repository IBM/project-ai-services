# Rotating Watsonx API Keys

How to replace an expired or compromised Watsonx API key for a RAG application
running on the Podman (PowerVS) runtime without losing application data.

## Prerequisites

- `ai-services` CLI installed and on `PATH`.
- The AI Services catalog is running (`ai-services catalog configure` completed).
- The RAG application is deployed and the LLM pod is in a running state.
- You have the new Watsonx API key ready.

## Steps

**1. Export the new API key**

```sh
export WATSONX_APIKEY_NEW="<new-api-key>"
```

The script reads the key from the environment so it is never written to shell history.

**2. Run the rotation script**

From the repository root:

```sh
./hack/rotate-watsonx-key/rotate-watsonx-key.sh <app-name>
```

Replace `<app-name>` with the name of the application
(visible in `ai-services application ps --runtime podman`).

**3. Verify**

```sh
ai-services application ps <app-name> --runtime podman
```

Then test an end-to-end query through the Catalog UI to confirm Watsonx is
responding with the new key.

## Troubleshooting

**LLM pod not found** — confirm the application is deployed and the LLM pod is running:

```sh
ai-services application ps <app-name> --runtime podman
```

**Pod is not a Watsonx pod** — the application may be using a different LLM
provider (vLLM). This script only works with Watsonx. Check the pod labels to confirm:

```sh
podman pod inspect llm-<slug> --format '{{.Labels}}'
```

**Mounted key mismatch after rotation** — the container may still be starting.
Wait a few more seconds then check:

```sh
podman exec llm-<slug>-litellm cat /etc/secret/watsonx-secret/apiKey | cut -c1-8
podman logs llm-<slug>-litellm
```

**Script failed mid-rotation** — if failure occurred after the pod was removed,
the rendered manifest is preserved at the path printed in the error output.
Replay it to restore the LLM pod:

```sh
podman kube play /tmp/llm-rotate-XXXXXX.yaml
```

If the manifest was not preserved, re-run the script with the new key.
