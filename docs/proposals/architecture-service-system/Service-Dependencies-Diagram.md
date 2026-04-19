# Service Dependencies Diagram

---

## Overview

This document illustrates the **proposed** service dependencies for the new architecture-service system. It shows how we plan to decompose the current monolithic RAG application into independent, reusable services.

**Key Change:** Breaking down the monolithic vllm-server pod into separate instruct, embedding, and reranker services for independent scaling and deployment.

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

### Proposed Service Architecture (Modular)

**Proposed Services:**

**User-Facing Services:**
1. **chat** - Question and answer service
2. **digitize** - Document digitize service
3. **summarize** - Document summarize service (Optional)

**Infrastructure Services:**
1. **instruct** - Language model service (separate pod)
2. **embedding** - Embedding model service (separate pod)
3. **reranker** - Reranking model service (separate pod)
4. **opensearch** - Vector database service

---

## Proposed Dependency Graph

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
        │                  │                   │
   ┌────┴─────┬────────────┴───────────┐       │
   │          │           │            │       │
   ▼          ▼           ▼            ▼       ▼
┌──────────┐ ┌────────┐ ┌──────────┐ ┌──────────┐
│Embedding │ │Reranker│ │OpenSearch│ │ Instruct │
│(Service) │ │(Service│ │(Service) │ │ (Service)│
└──────────┘ └────────┘ └──────────┘ └──────────┘
```

---

## Proposed Service Dependencies Detail

### 1. Chat Service

**Dependencies:**
- `instruct` (required) - For language model inference
- `embedding` (required) - For query embeddings
- `reranker` (required) - For result reranking
- `opensearch` (required) - For vector storage

**Purpose:** Question and answer using RAG

---

### 2. Digitize Service

**Dependencies:**
- `instruct` (required) - For language model inference
- `embedding` (required) - For document embeddings
- `opensearch` (required) - For document storage

**Purpose:** Transform documents into searchable text

---

### 3. Summarize Service (Optional)

**Dependencies:**
- `instruct` (required) - For language model inference

**Purpose:** Consolidate text into brief summaries

---

### 4. Instruct Service (Infrastructure)

**Type:** AI model inference service

**Model:** granite-3.3-8b-instruct

**Purpose:** Language model for instruction following

---

### 5. Embedding Service (Infrastructure)

**Type:** AI model inference service

**Model:** granite-embedding-278m-multilingual

**Purpose:** Generate text embeddings for documents and queries

---

### 6. Reranker Service (Infrastructure)

**Type:** AI model inference service

**Model:** bge-reranker-v2-m3

**Purpose:** Rerank search results for better relevance

---

### 7. OpenSearch Service (Infrastructure)

**Type:** Vector database

**Purpose:** Store and retrieve document vectors

---

## Proposed Deployment Scenarios

### Scenario 1: Full RAG Architecture

**User Command:**
```bash
ai-services application create my-rag --template rag
```

**Services Deployed:**
1. **instruct** (dependency)
2. **embedding** (dependency)
3. **reranker** (dependency)
4. **opensearch** (dependency)
5. **chat** (required service)
6. **digitize** (required service)
6. **summarize** (optional service, enabled by default)

---

### Scenario 2: RAG without Summarization

This can be achieved by using `--ignore-service` flag. This will deploy all services except the specified one.

**User Command:**
```bash
ai-services application create my-rag --template rag --ignore-service=summarize
```

**Services Deployed:**
1. **instruct** (dependency, shared)
2. **embedding** (dependency)
3. **reranker** (dependency)
4. **opensearch** (dependency)
5. **chat** (required service)
6. **digitize** (required service)

---

### Scenario 3: Standalone Chat Service

**User Command:**
```bash
ai-services application create my-chat --template chat
```

**Services Deployed:**
1. **instruct** (dependency)
2. **embedding** (dependency)
3. **reranker** (dependency)
4. **opensearch** (dependency)
5. **chat** (service)

**Note:** Only deploys what chat needs (no digitize or summarize)

---

### Scenario 4: Standalone Digitize Service

**User Command:**
```bash
ai-services application create my-digitize --template digitize
```

**Services Deployed:**
1. **instruct** (dependency)
2. **embedding** (dependency)
3. **opensearch** (dependency)
4. **digitize** (service)

**Note:** No reranker deployed (digitize doesn't need it)

---

### Scenario 5: Standalone Summarize Service

**User Command:**
```bash
ai-services application create my-summarize --template summarize
```

**Services Deployed:**
1. **instruct** (dependency)
2. **summarize** (service)

**Note:** Minimal deployment - only instruct model needed

---

## Proposed Dependency Resolution Logic

### 1. Identify Required Services
```
RAG Architecture requires:
- chat (user-facing)
- digitize (user-facing)
```

### 2. Resolve Dependencies Recursively
```
chat requires:
  - instruct
  - embedding
  - reranker
  - opensearch

digitize requires:
  - instruct (already in list)
  - embedding (already in list)
  - opensearch (already in list)
```

### 3. Deduplicate Dependencies
```
Final deployment list:
1. instruct (dependency, shared)
2. embedding (dependency, shared)
3. reranker (dependency)
4. opensearch (dependency, shared)
5. chat (user-facing)
6. digitize (user-facing)
```

### 4. Deploy in Dependency Order
```
Phase 1: Deploy infrastructure services
  - instruct
  - embedding
  - reranker
  - opensearch

Phase 2: Deploy user-facing services
  - chat
  - digitize
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

## Proposed Metadata Schemas

### Architecture Metadata

```yaml
# assets/architectures/rag/metadata.yaml
id: rag
name: "Digital Assistant"
description: "RAG architecture with Q&A, digitize, and summarize"
version: "1.0.0"
type: architecture

services:
  - id: chat
    version: ">=1.0.0"
    
  - id: digitize
    version: ">=1.0.0"
  
  - id: summarize
    version: ">=1.0.0"
    optional: true  # Optional service
```

### Service Metadata Examples

#### Chat Service
```yaml
# assets/services/chat/metadata.yaml
id: chat
name: "Question and Answer"
description: "Answer questions in natural language by sourcing general & domain-specific knowledge"
version: "1.0.0"
type: service

dependencies:
  - id: opensearch
    version: ">=1.0.0"
  - id: embedding
    version: ">=1.0.0"
  - id: instruct
    version: ">=1.0.0"
  - id: reranker
    version: ">=1.0.0"
```

#### Digitize Service
```yaml
# assets/services/digitize/metadata.yaml
id: digitize
name: "Document Digitization"
description: "Transform documents into searchable text"
version: "1.0.0"
type: service

dependencies:
  - id: opensearch
    version: ">=1.0.0"
  - id: embedding
    version: ">=1.0.0"
  - id: instruct
    version: ">=1.0.0"
```

#### Summarize Service
```yaml
# assets/services/summarize/metadata.yaml
id: summarize
name: "Document Summarization"
description: "Consolidate text into brief summaries"
version: "1.0.0"
type: service

dependencies:
  - id: instruct
    version: ">=1.0.0"
```

#### Instruct Service (Infrastructure)
```yaml
# assets/services/instruct/metadata.yaml
id: instruct
name: "Instruct Model"
description: "Language model for instruction following"
version: "1.0.0"
type: service

dependency_only: true
```

#### Embedding Service (Infrastructure)
```yaml
# assets/services/embedding/metadata.yaml
id: embedding
name: "Embedding Model"
description: "Text embedding generation"
version: "1.0.0"
type: service

dependency_only: true
```

#### Reranker Service (Infrastructure)
```yaml
# assets/services/reranker/metadata.yaml
id: reranker
name: "Reranker Model"
description: "Result reranking for better relevance"
version: "1.0.0"
type: service

dependency_only: true
```

#### OpenSearch Service (Infrastructure)
```yaml
# assets/services/opensearch/metadata.yaml
id: opensearch
name: "OpenSearch"
description: "Vector database for document storage"
version: "1.0.0"
type: service

dependency_only: true
```

---

## Summary

**Proposed RAG Architecture:**
- **Required Services:** chat, digitize
- **Optional Services:** summarize
- **Infrastructure Services:** instruct, embedding, reranker, opensearch (auto-deployed)

**Proposed Dependency Relationships:**
- chat → opensearch, embedding, instruct, reranker
- digitize → opensearch, embedding, instruct
- summarize → instruct

**Proposed Deployment Strategy:**
1. Resolve all dependencies recursively
2. Deduplicate shared infrastructure services
3. Deploy infrastructure services first (opensearch, embedding, instruct, reranker)
4. Deploy user-facing services (chat, digitize, summarize)

