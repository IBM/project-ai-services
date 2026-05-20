# Service Dependencies Diagram

---

## Overview

This document illustrates the **proposed** service dependencies for the architecture-service system. It shows how the RAG application uses a component-based architecture with services depending on abstract component types rather than specific implementations.

**Key Architecture:** Services depend on **component types** (e.g., `vector_store`, `llm`), and the system selects appropriate **component providers** (e.g., `opensearch`, `vllm-cpu`) at deployment time based on runtime constraints.

---

## Current State vs Proposed State

### Current RAG Application (Monolithic)

**Current Pods:**
1. **vllm-server** - Single pod with 3 containers
   - instruct
   - embedding
   - reranker
2. **opensearch** - Vector database
3. **chat-bot** - Chat UI and backend
4. **digitize** - Document digitize
5. **summarize-api** - Document summarize

**Problems:**
- Cannot reuse individual models across architectures

### Service Architecture (Component-Based)

**User-Facing Services:**
1. **chat** - Question and answer service
2. **digitize** - Document digitize service
3. **summarize** - Document summarize service (Optional)

**Component Types** (Abstract Dependencies):
1. **vector_store** - Vector database interface
2. **llm** - Large language model interface
3. **embedding** - Embedding model interface
4. **reranker** - Reranker model interface

**Component Providers** (Concrete Implementations):
1. **opensearch** - Implements `vector_store`
2. **vllm-cpu** - Implements `llm`, `embedding`, `reranker` (CPU)
3. **vllm-spyre** - Implements `llm`, `reranker` (Spyre accelerator)

---

## Dependency Graph

```
┌─────────────────────────────────────────────────────────────┐
│                    RAG Architecture                          │
│  (User deploys: chat + digitize + optional summarize)        │
└─────────────────────────────────────────────────────────────┘
                          │
        ┌─────────────────┼─────────────────┐
        │                 │                 │
        ▼                 ▼                 ▼
  ┌──────────┐      ┌──────────────┐  ┌──────────────┐
  │   Chat   │      │   Digitize   │  │  Summarize   │
  │(Required)│      │  (Required)  │  │  (Optional)  │
  └──────────┘      └──────────────┘  └──────────────┘
        │                  │                   │
        │ Depends on       │ Depends on        │ Depends on
        │ Component Types  │ Component Types   │ Component Type
        │                  │                   │
   ┌────┴─────┬────────────┴───────────┐       │
   │          │           │            │       │
   ▼          ▼           ▼            ▼       ▼
┌──────────┐ ┌────────┐ ┌──────────┐ ┌──────────┐
│embedding │ │reranker│ │vector_   │ │   llm    │
│  (type)  │ │ (type) │ │store     │ │  (type)  │
│          │ │        │ │ (type)   │ │          │
└────┬─────┘ └───┬────┘ └────┬─────┘ └────┬─────┘
     │           │           │            │
     │ Resolved  │ Resolved  │ Resolved   │ Resolved
     │ to        │ to        │ to         │ to
     ▼           ▼           ▼            ▼
┌──────────┐ ┌────────┐ ┌──────────┐ ┌──────────┐
│vllm-cpu  │ │vllm-cpu│ │opensearch│ │ vllm-cpu │
│(provider)│ │(provider│ │(provider)│ │(provider)│
└──────────┘ └────────┘ └──────────┘ └──────────┘
```

---

## Service Dependencies Detail

### 1. Chat Service

**Component Type Dependencies:**
- `vector_store` (required) - For vector storage
- `llm` (required) - For language model inference
- `embedding` (required) - For query embeddings
- `reranker` (required) - For result reranking

**Resolved Providers (CPU runtime):**
- `opensearch` (vector_store provider)
- `vllm-cpu` (llm provider)
- `vllm-cpu` (embedding provider)
- `vllm-cpu` (reranker provider)

**Purpose:** Question and answer using RAG

---

### 2. Digitize Service

**Component Type Dependencies:**
- `vector_store` (required) - For document storage
- `llm` (required) - For language model inference
- `embedding` (required) - For document embeddings

**Resolved Providers (CPU runtime):**
- `opensearch` (vector_store provider)
- `vllm-cpu` (llm provider)
- `vllm-cpu` (embedding provider)

**Purpose:** Transform documents into searchable text

---

### 3. Summarize Service (Optional)

**Component Type Dependencies:**
- `llm` (required) - For language model inference

**Resolved Providers (CPU runtime):**
- `vllm-cpu` (llm provider)

**Purpose:** Consolidate text into brief summaries

---

## Component Providers Detail

### Vector Store Providers

#### OpenSearch
- **Component Type:** `vector_store`
- **Purpose:** Distributed search and analytics engine for vector storage
- **Runtimes:** Podman, OpenShift

---

### LLM Providers

#### vLLM CPU
- **Component Type:** `llm`
- **Purpose:** Deploy instruct models on vLLM inference engine (CPU-only)
- **Model:** granite-3.3-8b-instruct
- **Runtimes:** Podman

#### vLLM Spyre
- **Component Type:** `llm`
- **Purpose:** Deploy instruct models on vLLM with Spyre acceleration
- **Model:** granite-3.3-8b-instruct
- **Runtimes:** OpenShift (with Spyre hardware)

---

### Embedding Providers

#### vLLM CPU Embedding
- **Component Type:** `embedding`
- **Purpose:** Generate text embeddings for documents and queries
- **Model:** granite-embedding-278m-multilingual
- **Runtimes:** Podman

---

### Reranker Providers

#### vLLM CPU Reranker
- **Component Type:** `reranker`
- **Purpose:** Rerank search results for better relevance
- **Model:** bge-reranker-v2-m3
- **Runtimes:** Podman

#### vLLM Spyre Reranker
- **Component Type:** `reranker`
- **Purpose:** Rerank with Spyre acceleration
- **Model:** bge-reranker-v2-m3
- **Runtimes:** OpenShift (with Spyre hardware)

---

## Deployment Scenarios

### Scenario 1: Full RAG Architecture (CPU Runtime)

**User Command:**
```bash
ai-services application create my-rag --template rag
```

**Component Providers Deployed (Spyre available):**
1. **opensearch** (vector_store provider)
2. **vllm-spyre** (llm provider)
3. **vllm-cpu** (embedding provider)
4. **vllm-spyre** (reranker provider)

**Services Deployed:**
5. **chat** (required service)
6. **digitize** (required service)
7. **summarize** (optional service, enabled by default)

---

### Scenario 2: RAG without Summarization

This can be achieved by using `--ignore-service` flag. This will deploy all services except the specified one.

**User Command:**
```bash
ai-services application create my-rag --template rag --ignore-service=summarize
```

**Component Providers Deployed:**
1. **opensearch** (vector_store provider)
2. **vllm-cpu** (llm provider)
3. **vllm-cpu** (embedding provider)
4. **vllm-cpu** (reranker provider)

**Services Deployed:**
5. **chat** (required service)
6. **digitize** (required service)

---

### Scenario 3: Standalone Chat Service

**User Command:**
```bash
ai-services application create my-chat --template chat
```

**Component Providers Deployed:**
1. **opensearch** (vector_store provider)
2. **vllm-cpu** (llm provider)
3. **vllm-cpu** (embedding provider)
4. **vllm-cpu** (reranker provider)

**Services Deployed:**
5. **chat** (service)

**Note:** Only deploys component providers that chat needs (no digitize or summarize)

---

### Scenario 4: Standalone Digitize Service

**User Command:**
```bash
ai-services application create my-digitize --template digitize
```

**Component Providers Deployed:**
1. **opensearch** (vector_store provider)
2. **vllm-cpu** (llm provider)
3. **vllm-cpu** (embedding provider)

**Services Deployed:**
4. **digitize** (service)

**Note:** No reranker provider deployed (digitize doesn't need it)

---

### Scenario 5: Standalone Summarize Service

**User Command:**
```bash
ai-services application create my-summarize --template summarize
```

**Component Providers Deployed:**
1. **vllm-cpu** (llm provider)

**Services Deployed:**
2. **summarize** (service)

**Note:** Minimal deployment - only llm provider needed

---

## Dependency Resolution Logic

### 1. Identify Required Services
```
RAG Architecture requires:
- chat (user-facing)
- digitize (user-facing)
- summarize (optional, user-facing)
```

### 2. Resolve Component Type Dependencies
```
chat requires component types:
  - vector_store
  - llm
  - embedding
  - reranker

digitize requires component types:
  - vector_store (already in list)
  - llm (already in list)
  - embedding (already in list)

summarize requires component types:
  - llm (already in list)
```

### 3. Select Component Providers
```
Based on runtime (Podman/CPU):
- vector_store → opensearch
- llm → vllm-cpu
- embedding → vllm-cpu
- reranker → vllm-cpu
```

### 4. Deduplicate Providers
```
Final deployment list:
1. opensearch (vector_store provider)
2. vllm-cpu (llm provider)
3. vllm-cpu (embedding provider)
4. vllm-cpu (reranker provider)
5. chat (user-facing)
6. digitize (user-facing)
7. summarize (user-facing, optional)
```

### 5. Deploy in Dependency Order
```
Phase 1: Deploy component providers
  - opensearch
  - vllm-cpu (llm)
  - vllm-cpu (embedding)
  - vllm-cpu (reranker)

Phase 2: Deploy user-facing services
  - chat
  - digitize
  - summarize
```

---

## Proposed Service Discovery Pattern

### Convention-Based Naming

Services discover each other using predictable naming:

```
{{ .AppName }}--<service-id>:<port>
```

**Examples:**
```
Application: production-rag

Infrastructure endpoints:
- production-rag--instruct:8000
- production-rag--embedding:8000
- production-rag--reranker:8000
- production-rag--opensearch:9200

User service endpoints:
- production-rag--chat:3000 (UI)
- production-rag--chat:5000 (API)
```

---

## Metadata Schemas

### Architecture Metadata

```yaml
# assets/architectures/rag/metadata.yaml
id: rag
name: "Digital Assistant"
description: "Enable digital assistants using RAG"
version: "1.0.0"
type: architecture

certified_by: "IBM"
runtimes:
  - podman

services:
  - id: chat
    version: ">=1.0.0"
    
  - id: digitize
    version: ">=1.0.0"
  
  - id: summarize
    version: ">=1.0.0"
    optional: true
```

### Service Metadata Examples

#### Chat Service
```yaml
# assets/services/chat/metadata.yaml
id: chat
name: "Question and Answer"
description: "Answer questions in natural language"
type: service

certified_by: "IBM"

architectures:
  - rag

dependencies:
  - id: vector_store    # Component type
  - id: embedding       # Component type
  - id: llm            # Component type
  - id: reranker       # Component type
```

#### Digitize Service
```yaml
# assets/services/digitize/metadata.yaml
id: digitize
name: "Digitize Documents"
description: "Transforms documents into texts"
type: service

certified_by: "IBM"

architectures:
  - rag

dependencies:
  - id: vector_store    # Component type
  - id: embedding       # Component type
  - id: llm            # Component type
```

#### Summarize Service
```yaml
# assets/services/summarize/metadata.yaml
id: summarize
name: "Summarize Documents"
description: "Consolidate text into brief summaries"
type: service

certified_by: "IBM"

architectures:
  - rag

dependencies:
  - id: llm            # Component type
```

### Component Provider Metadata Examples

#### OpenSearch (Vector Store Provider)
```yaml
# assets/components/vector_db/opensearch/metadata.yaml
type: component
id: opensearch
name: "OpenSearch"
description: "Distributed search and analytics engine for vector storage"
component_type: vector_store
```

#### vLLM CPU (LLM Provider)
```yaml
# assets/components/llm/vllm-cpu/metadata.yaml
type: component
id: vllm-cpu
name: "vLLM CPU Instruct"
description: "Deploy instruct models on vLLM inference engine (CPU-only)"
component_type: llm
```

#### vLLM CPU Embedding (Embedding Provider)
```yaml
# assets/components/embedding/vllm-cpu/metadata.yaml
type: component
id: vllm-cpu
name: "vLLM CPU Embedding"
description: "Generate text embeddings"
component_type: embedding
```

#### vLLM CPU Reranker (Reranker Provider)
```yaml
# assets/components/reranker/vllm-cpu/metadata.yaml
type: component
id: vllm-cpu
name: "vLLM CPU Reranker"
description: "Rerank search results"
component_type: reranker
```

---

## Summary

**RAG Architecture:**
- **Required Services:** chat, digitize
- **Optional Services:** summarize
- **Component Types:** vector_store, llm, embedding, reranker
- **Component Providers (CPU):** opensearch, vllm-cpu (auto-deployed)

**Dependency Relationships:**
- chat → vector_store, llm, embedding, reranker (component types)
- digitize → vector_store, llm, embedding (component types)
- summarize → llm (component type)

**Deployment Strategy:**
1. Resolve component type dependencies recursively
2. Select appropriate component providers based on runtime
3. Deduplicate shared component providers
4. Deploy component providers first (opensearch, vllm-cpu instances)
5. Deploy user-facing services (chat, digitize, summarize)

**Key Benefit:** Services depend on abstract component types, allowing runtime selection of providers (CPU vs Spyre) without changing service definitions.

