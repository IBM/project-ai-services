# Design Proposal: vLLM API Authentication

---

## 1. Executive Summary

**vLLM Authentication** provides simple, API key-based authentication for vLLM inference services (instruct, embedding, and reranker models). Each service can have its own independent API key. Authentication is controlled by the presence of API keys supplied via environment variables - if an API key is provided for a service, authentication is enabled for that service; otherwise, it remains disabled. This approach ensures flexibility, security isolation, and simplicity.

## 2. Problem Statement

### Current State
- vLLM services (instruct, embedding, reranker) are deployed **without authentication**
- Any client with network access can consume vLLM APIs
- No access control or audit trail for API usage
- Security risk in production environments

### Requirements
1. **Simple Configuration**: User-Provided API Key - The system authenticates using an API key supplied directly by the user
2. **Default Behavior**: Authentication disabled by default (no API key = no auth)
3. **Opt-In Mechanism**: Users enable authentication by providing API keys
4. **Service Isolation**: Each service (instruct, embedding, reranker) has its own API key
5. **Minimal Overhead**: No performance degradation or complex configuration

## 3. Solution Architecture

### 3.1 Authentication Flow

```
Client Service
      |
      | HTTP Request + Authorization: Bearer <service_api_key>
      v
vLLM Server (instruct/embedding/reranker)
      |
      +--> API Key Validation
            |
            +--[Valid API Key]-------> Model Inference --> Response
            |
            +--[Invalid/Missing]-----> 401 Unauthorized
```

### 3.2 System Components

| Component | Role | Implementation |
|-----------|------|----------------|
| **vLLM Server** | Validates API key using env var | Native vLLM support (v0.4.1+) |
| **Client Services** | Include `Authorization: Bearer <api_key>` header | Python utilities (misc_utils, emb_utils, llm_utils) |
| **Configuration** | API keys supplied via parameter or env var | values.yaml with separate keys per service |

### 3.3 API Key Architecture

**Service-Specific API Keys:**

Users provide individual API keys for each vLLM service:

**Key Properties:**
- **User-Controlled**: API keys provided by user, not hardcoded
- **Optional**: If no API key supplied for a service, authentication is disabled for that service
- **Service-Isolated**: Each service has its own independent API key
- **Plain Text**: No encryption or encoding
- **Granular Control**: Enable auth for some services while leaving others open

## 4. Feature Specification

### 4.1 Default Behavior (Authentication Disabled)

When a user creates an application **without** specifying API keys:

```bash
$ ai-services application create my-app -t rag

⚠ vLLM authentication is disabled for all services (no API keys provided)

Application 'my-app' created successfully
=====================================
```

**What Happens:**
1. All `vllm.*.apiKey` fields are empty/unset in values
2. vLLM servers start without authentication
3. Client services do not include Authorization headers
4. No API key storage or secrets created

### 4.2 Enabling Authentication (Opt-In)

Users enable authentication by providing API keys for specific services via the `--params` flag:

#### Enable authentication for all services:
```bash
$ ai-services application create my-app -t rag \
  --params vllm.instruct.apiKey=instruct-key-123 \
  --params vllm.embedding.apiKey=embedding-key-456 \
  --params vllm.reranker.apiKey=reranker-key-789

✓ vLLM authentication enabled:
  - Instruct service: enabled
  - Embedding service: enabled
  - Reranker service: enabled

Application 'my-app' created successfully
=====================================
```

#### Enable authentication for specific services only:
```bash
$ ai-services application create my-app -t rag \
  --params vllm.instruct.apiKey=instruct-key-123

✓ vLLM authentication status:
  - Instruct service: enabled
  - Embedding service: disabled (no API key)
  - Reranker service: disabled (no API key)

Application 'my-app' created successfully
=====================================
```

**What Happens:**
1. API keys are set for specified services in values
2. API keys are passed to respective vLLM servers via VLLM_API_KEY env var
3. Client services use the appropriate API key when calling each service
4. Services without API keys remain unauthenticated

**API Key Usage:**

| Environment | How API Keys Are Used |
|-------------|-------------------|
| **Podman** | API keys passed directly to vLLM via env var per service |
| **OpenShift** | API keys stored in Kubernetes Secrets, passed to vLLM via env var per service |

**Deployment Flow**:
```
1. User Provides API Keys (via --params or env vars)
2. Deploy Application:
   
   Podman:
   ├─> Pass instruct API key to instruct container env var
   ├─> Pass embedding API key to embedding container env var
   ├─> Pass reranker API key to reranker container env var
   └─> Client services use appropriate API key per service
   
   OpenShift:
   ├─> Create vllm-instruct-api-key Secret (if provided)
   ├─> Create vllm-embedding-api-key Secret (if provided)
   ├─> Create vllm-reranker-api-key Secret (if provided)
   ├─> Reference Secrets in respective InferenceServices
   ├─> Reference Secrets in client Deployments
   └─> Each service uses its own API key
```

## 5. Configuration Structure

### 5.1 values.yaml Schema

```yaml
vllm:
  instruct:
    apiKey: ""  # Default: empty (authentication disabled)
  embedding:
    apiKey: ""  # Default: empty (authentication disabled)
  reranker:
    apiKey: ""  # Default: empty (authentication disabled)
```

### 5.2 Configuration Logic

```
FOR EACH service (instruct, embedding, reranker):
  IF vllm.<service>.apiKey is set (non-empty):
      Pass API key to VLLM_API_KEY env var for that service
      Client services use API key in Authorization headers for that service
      Authentication is ENABLED for that service
  ELSE:
      Do not set VLLM_API_KEY env var for that service
      Client services do not include Authorization headers for that service
      Authentication is DISABLED for that service
```

## 6. Implementation Details

### 6.1 Server-Side (vLLM)

vLLM natively reads the `VLLM_API_KEY` environment variable for authentication without needing the `--api-key` parameter.

#### Podman Implementation

Each vLLM server conditionally sets its own API key as environment variable:

```yaml
# vllm-server.yaml.tmpl (partial - showing env additions)
spec:
  containers:
    - name: instruct
      env:
        - name: VLLM_MODEL_PATH
          value: "/models/ibm-granite/granite-3.3-8b-instruct"
        - name: AIU_WORLD_SIZE
          value: "4"
        {{- if .Values.vllm.instruct.apiKey }}
        - name: VLLM_API_KEY
          value: {{ .Values.vllm.instruct.apiKey | quote }}
        {{- end }}
      # ... rest of container spec
    
    - name: embedding
      env:
        {{- if .Values.vllm.embedding.apiKey }}
        - name: VLLM_API_KEY
          value: {{ .Values.vllm.embedding.apiKey | quote }}
        {{- end }}
      # ... rest of container spec
    
    - name: reranker
      env:
        - name: VLLM_MODEL_PATH
          value: "/models/BAAI/bge-reranker-v2-m3"
        - name: AIU_WORLD_SIZE
          value: "1"
        {{- if .Values.vllm.reranker.apiKey }}
        - name: VLLM_API_KEY
          value: {{ .Values.vllm.reranker.apiKey | quote }}
        {{- end }}
      # ... rest of container spec
```

#### OpenShift Implementation

**Step 1: Create Kubernetes Secrets (one per service, only if API key is provided)**

```yaml
# vllm-instruct-api-key-secret.yaml
{{- if .Values.vllm.instruct.apiKey }}
apiVersion: v1
kind: Secret
metadata:
  name: "vllm-instruct-api-key"
  labels:
    ai-services.io/application: {{ .Release.Name }}
    ai-services.io/template: {{ .Chart.Name }}
type: Opaque
stringData:
  apiKey: {{ .Values.vllm.instruct.apiKey | quote }}
{{- end }}
```

```yaml
# vllm-embedding-api-key-secret.yaml
{{- if .Values.vllm.embedding.apiKey }}
apiVersion: v1
kind: Secret
metadata:
  name: "vllm-embedding-api-key"
  labels:
    ai-services.io/application: {{ .Release.Name }}
    ai-services.io/template: {{ .Chart.Name }}
type: Opaque
stringData:
  apiKey: {{ .Values.vllm.embedding.apiKey | quote }}
{{- end }}
```

```yaml
# vllm-reranker-api-key-secret.yaml
{{- if .Values.vllm.reranker.apiKey }}
apiVersion: v1
kind: Secret
metadata:
  name: "vllm-reranker-api-key"
  labels:
    ai-services.io/application: {{ .Release.Name }}
    ai-services.io/template: {{ .Chart.Name }}
type: Opaque
stringData:
  apiKey: {{ .Values.vllm.reranker.apiKey | quote }}
{{- end }}
```

**Step 2: Reference Secrets in InferenceServices (as environment variable)**

vLLM natively reads the `VLLM_API_KEY` environment variable:

```yaml
# instruct-inferenceservice.yaml
spec:
  predictor:
    model:
      env:
      - name: VLLM_SPYRE_USE_CB
        value: "1"
      {{- if .Values.vllm.instruct.apiKey }}
      - name: VLLM_API_KEY
        valueFrom:
          secretKeyRef:
            name: vllm-instruct-api-key
            key: apiKey
      {{- end }}
      args:
      - '--tensor-parallel-size=4 '
      - '--max-model-len=32768 '
      - --max-num-seqs=32
      - --served-model-name=ibm-granite/granite-3.3-8b-instruct
      # ... rest of spec
```

```yaml
# embedding-inferenceservice.yaml
spec:
  predictor:
    model:
      {{- if .Values.vllm.embedding.apiKey }}
      env:
      - name: VLLM_API_KEY
        valueFrom:
          secretKeyRef:
            name: vllm-embedding-api-key
            key: apiKey
      {{- end }}
      args:
      - --served-model-name=ibm-granite/granite-embedding-278m-multilingual
      # ... rest of spec
```

```yaml
# reranker-inferenceservice.yaml
spec:
  predictor:
    model:
      {{- if .Values.vllm.reranker.apiKey }}
      env:
      - name: VLLM_API_KEY
        valueFrom:
          secretKeyRef:
            name: vllm-reranker-api-key
            key: apiKey
      {{- end }}
      args:
      - '--tensor-parallel-size=1'
      - --served-model-name=BAAI/bge-reranker-v2-m3
      # ... rest of spec
```

**Step 3: Reference Secrets in Client Deployments (FastAPI apps)**

FastAPI applications receive the API keys via environment variables and use them in Authorization headers:

```yaml
# backend-deployment.yaml
spec:
  template:
    spec:
      containers:
      - name: server
        env:
        {{- if .Values.vllm.instruct.apiKey }}
        - name: VLLM_INSTRUCT_API_KEY
          valueFrom:
            secretKeyRef:
              name: vllm-instruct-api-key
              key: apiKey
        {{- end }}
        {{- if .Values.vllm.embedding.apiKey }}
        - name: VLLM_EMBEDDING_API_KEY
          valueFrom:
            secretKeyRef:
              name: vllm-embedding-api-key
              key: apiKey
        {{- end }}
        {{- if .Values.vllm.reranker.apiKey }}
        - name: VLLM_RERANKER_API_KEY
          valueFrom:
            secretKeyRef:
              name: vllm-reranker-api-key
              key: apiKey
        {{- end }}
```

#### Behavior Matrix

| Service | API Key Status | vLLM Behavior |
|---------|----------------|---------------|
| Instruct | Set (non-empty) | Authentication enabled with instruct API key |
| Instruct | Unset (empty) | Authentication disabled |
| Embedding | Set (non-empty) | Authentication enabled with embedding API key |
| Embedding | Unset (empty) | Authentication disabled |
| Reranker | Set (non-empty) | Authentication enabled with reranker API key |
| Reranker | Unset (empty) | Authentication disabled |

### 6.2 Client-Side (FastAPI Python Services)

FastAPI applications receive service-specific API keys via environment variables and use them in Authorization headers when making requests to vLLM:

```python
import os
import requests

# Read API keys from environment variables (set from Kubernetes Secret or Podman env)
VLLM_INSTRUCT_API_KEY = os.getenv("VLLM_INSTRUCT_API_KEY", "")
VLLM_EMBEDDING_API_KEY = os.getenv("VLLM_EMBEDDING_API_KEY", "")
VLLM_RERANKER_API_KEY = os.getenv("VLLM_RERANKER_API_KEY", "")

# Use appropriate API key in Authorization header for vLLM API calls
def get_vllm_headers(service: str):
    """Get headers for vLLM API calls based on service type."""
    headers = {}
    
    if service == "instruct" and VLLM_INSTRUCT_API_KEY:
        headers["Authorization"] = f"Bearer {VLLM_INSTRUCT_API_KEY}"
    elif service == "embedding" and VLLM_EMBEDDING_API_KEY:
        headers["Authorization"] = f"Bearer {VLLM_EMBEDDING_API_KEY}"
    elif service == "reranker" and VLLM_RERANKER_API_KEY:
        headers["Authorization"] = f"Bearer {VLLM_RERANKER_API_KEY}"
    
    return headers

# Example usage in API calls
headers = get_vllm_headers("instruct")
response = requests.post(instruct_url, headers=headers, json=payload)

headers = get_vllm_headers("embedding")
response = requests.post(embedding_url, headers=headers, json=payload)

headers = get_vllm_headers("reranker")
response = requests.post(reranker_url, headers=headers, json=payload)
```

### 6.3 CLI Implementation (Go)

#### API Key Handling

No hardcoded API keys in the CLI. API keys are user-supplied via `--params`:

```go
// No hardcoded API key constants needed
// API keys come from user via:
//   --params vllm.instruct.apiKey=<value>
//   --params vllm.embedding.apiKey=<value>
//   --params vllm.reranker.apiKey=<value>
```

#### Integration Points

1. Load template values with `vllm.*.apiKey` fields (default: empty)
2. User can override via `--params vllm.<service>.apiKey=<value>`
3. Render templates with user-provided API keys if set
4. Deploy application

**Implementation Flow**:
```go
// Pseudo-code for create.go
func Create(appName, template string, params map[string]string) error {
    // 1. Load template values
    tp := templates.NewEmbedTemplateProvider(templates.EmbedOptions{Runtime: runtimeType})
    values, err := tp.LoadValues(templateName, valuesFiles, params)
    if err != nil {
        return err
    }
    
    // 2. Display information
    fmt.Println("✓ vLLM authentication status:")
    
    if values.vllm.instruct.apiKey != "" {
        fmt.Println("  - Instruct service: enabled")
    } else {
        fmt.Println("  - Instruct service: disabled (no API key)")
    }
    
    if values.vllm.embedding.apiKey != "" {
        fmt.Println("  - Embedding service: enabled")
    } else {
        fmt.Println("  - Embedding service: disabled (no API key)")
    }
    
    if values.vllm.reranker.apiKey != "" {
        fmt.Println("  - Reranker service: enabled")
    } else {
        fmt.Println("  - Reranker service: disabled (no API key)")
    }
    
    // 3. Render templates with values
    renderedTemplates := tp.RenderTemplates(templateName, values)
    
    // 4. Deploy using rendered templates
    deployApplication(appName, renderedTemplates)
}
```
