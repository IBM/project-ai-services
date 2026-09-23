# BYOS — Overview

This page explains the core concepts behind the **Bring Your Own Service (BYOS)** feature and lists all built-in catalog items whose IDs are reserved. Read this before the authoring or integration guides.

**In this series:**
- **01 — Overview** ← you are here
- [02 — Bundle Structure and Templates](02-bundle-structure.md)
- [03 — Integrating Bundles into AI-Services](03-integrating-bundles.md)

---

## What is a Service?

A **service** is a deployable AI workload — the top-level thing a user launches from the catalog. It represents a complete functional unit such as a chatbot, a document summarizer, or a similarity-search API. A service can declare **dependencies** on one or more components (e.g. an LLM, an embedding model, a vector store) that the platform resolves and wires up automatically at deploy time.

## What is a Component?

A **component** is a reusable infrastructure provider that a service depends on. Components are not deployed standalone — they are always launched as part of a service deployment. There are four component types:

| Type | Role |
|---|---|
| `llm` | Large language model inference endpoint (e.g. vLLM on CPU, vLLM on Spyre, watsonx.ai) |
| `embedding` | Text embedding model endpoint used for vector indexing and retrieval |
| `reranker` | Reranking model endpoint used to re-score retrieved passages |
| `vector_db` | Vector database for storing and searching embeddings (e.g. OpenSearch) |

When a service declares `dependencies: [{id: llm}]`, the platform asks the user to choose an LLM component provider at deploy time and injects its connection details into the service's template automatically.

## How BYOS fits in

The platform's built-in catalog is compiled into the binary at build time. BYOS introduces a **bundle** mechanism: you package your service or component definition as a `.tar.gz` archive and upload it to the running catalog backend over HTTPS. The platform validates, registers, and hot-reloads the new asset immediately — no pod restart or platform rebuild required.

```
You (author)                  Catalog Backend             Catalog Provider
     │                              │                            │
     │  POST /api/v1/catalog/bundles│                            │
     │  (my-bundle.tar.gz)          │                            │
     │─────────────────────────────>│                            │
     │                              │── validate + extract ──>   │
     │                              │── insert DB row ────────>  │
     │                              │── CatalogProvider.Reload() │
     │                              │<── hot-reload complete ─── │
     │<── 201 Created ──────────────│                            │
```

**Key facts:**
- Both Podman and OpenShift deployments are supported.
- Bundles are stored on a dedicated named volume (`catalog-bundles`) so they survive pod restarts.
- At most one bundle per `(catalog_type, catalog_id)` pair is active at any time — use `bundle update` to upgrade a version.
- Built-in platform service IDs are protected; uploading a bundle with a reserved ID is rejected with `422`.

---

## Built-in Services

The following services ship with the platform. Their IDs are reserved — you cannot upload a bundle using any of these values.

| Service ID   | Description                        | Assets |
|--------------|------------------------------------|--------|
| `chat`       | Question and answer chatbot        | [`assets/services/chat/`](assets/services/chat/) |
| `digitize`   | Document digitization and indexing | [`assets/services/digitize/`](assets/services/digitize/) |
| `similarity` | Semantic similarity search         | [`assets/services/similarity/`](assets/services/similarity/) |
| `summarize`  | Document summarization             | [`assets/services/summarize/`](assets/services/summarize/) |
| `extract`    | Information extraction             | [`assets/services/extract/`](assets/services/extract/) |
| `translate`  | Language translation               | [`assets/services/translate/`](assets/services/translate/) |

Custom services can declare dependencies on any of the four supported component types: `llm`, `embedding`, `reranker`, `vector_db`.

## Built-in Components

The platform ships the following component providers. These IDs are reserved and cannot be overwritten by a bundle.

#### LLM providers (`component_type: llm`)

| Provider ID  | Runtime  | Default Port | Description | Assets |
|--------------|----------|--------------|-------------|--------|
| `vllm-cpu`   | Podman / OpenShift | `8000` | vLLM inference on CPU / ppc64le | [`assets/components/llm/vllm-cpu/`](assets/components/llm/vllm-cpu/) |
| `vllm-spyre` | Podman / OpenShift | `8000` | vLLM inference on IBM Spyre accelerator cards | [`assets/components/llm/vllm-spyre/`](assets/components/llm/vllm-spyre/) |
| `watsonx`    | Podman / OpenShift | `8000` | LiteLLM proxy forwarding to IBM watsonx.ai | [`assets/components/llm/watsonx/`](assets/components/llm/watsonx/) |

#### Embedding providers (`component_type: embedding`)

| Provider ID | Runtime  | Default Port | Description | Assets |
|-------------|----------|--------------|-------------|--------|
| `vllm-cpu`  | Podman / OpenShift | `8001` | vLLM embedding on CPU | [`assets/components/embedding/vllm-cpu/`](assets/components/embedding/vllm-cpu/) |

#### Reranker providers (`component_type: reranker`)

| Provider ID  | Runtime  | Default Port | Description | Assets |
|--------------|----------|--------------|-------------|--------|
| `vllm-cpu`   | Podman / OpenShift | `8002` | vLLM reranker on CPU | [`assets/components/reranker/vllm-cpu/`](assets/components/reranker/vllm-cpu/) |
| `vllm-spyre` | Podman / OpenShift | `8002` | vLLM reranker on Spyre cards | [`assets/components/reranker/vllm-spyre/`](assets/components/reranker/vllm-spyre/) |

#### Vector-store providers (`component_type: vector_db`)

| Provider ID  | Runtime  | Default Port | Description | Assets |
|--------------|----------|--------------|-------------|--------|
| `opensearch` | Podman / OpenShift | `9200` | OpenSearch vector database | [`assets/components/vector_db/opensearch/`](assets/components/vector_db/opensearch/) |

---

## Reserved IDs

A bundle whose `catalog_id` matches a built-in catalog item is rejected with `422 Unprocessable Entity`. Choose a unique `id` that does not appear in the following lists.

**Reserved service IDs:** `chat`, `digitize`, `similarity`, `summarize`, `extract`, `translate`, `rag`

**Reserved component IDs** (composite `<component_type>--<id>` format):

| `component_type` | Reserved IDs |
|---|---|
| `llm` | `llm--vllm-cpu`, `llm--vllm-spyre`, `llm--watsonx` |
| `embedding` | `embedding--vllm-cpu` |
| `reranker` | `reranker--vllm-cpu`, `reranker--vllm-spyre` |
| `vector_db` | `vector_db--opensearch` |
