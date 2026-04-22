# Design Proposal: vLLM API Authentication

---

## 1. Executive Summary

**vLLM Authentication** provides simple, password-based authentication for all vLLM inference services (instruct, embedding, and reranker models). By using a single hardcoded password across all services with an opt-out mechanism, the system ensures that AI services have basic authentication by default while maintaining flexibility for development and testing scenarios.

## 2. Problem Statement

### Current State
- vLLM services (instruct, embedding, reranker) are deployed **without authentication**
- Any client with network access can consume vLLM APIs
- No access control or audit trail for API usage
- Security risk in production environments

### Requirements
1. **Secure by Default**: Authentication must be enabled automatically during application creation
2. **Simple Configuration**: Single hardcoded password used across all services
3. **Opt-Out Mechanism**: Users can disable authentication via explicit flag for development/testing
4. **Backward Compatible**: Existing deployments should continue to work
5. **Minimal Overhead**: No performance degradation or complex configuration

## 3. Solution Architecture

### 3.1 Authentication Flow

```
Client Service
      |
      | HTTP Request + Authorization: Bearer <password>
      v
vLLM Server
      |
      +--> Password Validation
            |
            +--[Valid Password]-------> Model Inference --> Response
            |
            +--[Invalid/Missing]-----> 401 Unauthorized
```

### 3.2 System Components

| Component | Role | Implementation |
|-----------|------|----------------|
| **vLLM Server** | Validates password using env var | Native vLLM support (v0.4.1+) |
| **Client Services** | Include `Authorization: Bearer <password>` header | Python utilities (misc_utils, emb_utils, llm_utils) |
| **Configuration** | Controls auth behavior with hardcoded password | values.yaml with vllm.authEnabled flag |

### 3.3 Password Architecture

**Simple Hardcoded Password:**

A single hardcoded password is used across all vLLM services:

```
Hardcoded Password (in code)
       |
       v
Used directly as API key
       |
       v
Passed as VLLM_API_KEY env var
```

**Key Properties:**
- **Simple**: Single password for all services
- **Hardcoded**: Password defined in code, not configurable by users
- **Plain Text**: No encryption or encoding
- **Shared**: Same password used by instruct, embedding, and reranker services

## 4. Feature Specification

### 4.1 Default Behavior (Authentication Enabled)

When a user creates an application **without** specifying authentication preferences:

```bash
$ ai-services application create my-app -t rag

✓ vLLM authentication enabled with password

Application 'my-app' created successfully
=====================================
```

**What Happens:**
1. `vllm.authEnabled` is `true` by default
2. Hardcoded password is used across all services (instruct, embedding, reranker)
3. Password is passed directly to vLLM servers via env var
4. Client services use the same hardcoded password in Authorization headers
5. No password storage in values file - password is hardcoded in code

**Password Usage:**

| Environment | How Password Is Used |
|-------------|-------------------|
| **Podman** | Hardcoded password passed directly to vLLM via env var |
| **OpenShift** | Hardcoded password stored in Kubernetes Secret, passed to vLLM via env var |

**Deployment Flow**:
```
1. Hardcoded Password (defined in code)
2. Deploy Application:
   
   Podman:
   ├─> Pass hardcoded password directly to vLLM env var
   └─> Client services use same hardcoded password in Authorization headers
   
   OpenShift:
   ├─> Create vllm-password Secret with hardcoded password
   ├─> Reference Secret in InferenceServices (vLLM pods)
   ├─> Reference Secret in Deployments (client pods)
   └─> All services use the same password
```

### 4.2 Disabling Authentication (Opt-Out)

For development or testing scenarios, users can disable authentication via the `--params` flag:

```bash
$ ai-services application create my-app -t rag --params vllm.authEnabled=false

⚠ vLLM authentication is disabled (not recommended for production)
✓ Application 'my-app' created successfully
```

**What Happens:**
1. `vllm.authEnabled` is set to `false` in values
2. vLLM servers start without auth enabled
3. Client services do not include Authorization headers

## 5. Configuration Structure

### 5.1 values.yaml Schema

```yaml
vllm:
  authEnabled: true  # Default: true (authentication enabled)
```

### 5.2 Hardcoded Password

The password is defined as a constant in the codebase:

```python
# In Python client code
VLLM_API_KEY = "vllm-default-password"
```

### 5.3 Configuration Logic

```
IF vllm.authEnabled == true:
    Use hardcoded password for all services
    Pass password directly to VLLM_API_KEY env var
    Client services use same hardcoded password in Authorization headers
ELSE:
    Do not use authentication
```

## 6. Implementation Details

### 6.1 Server-Side (vLLM)

vLLM natively reads this environment variable for authentication without needing the `--api-key` parameter.

#### Podman Implementation

All vLLM servers use the same hardcoded password set as environment variable in the `env` section:

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
        {{- if .Values.vllm.authEnabled }}
        - name: VLLM_API_KEY
          value: "vllm-default-password"
        {{- end }}
      # ... rest of container spec
    
    - name: embedding
      {{- if .Values.vllm.authEnabled }}
      env:
        - name: VLLM_API_KEY
          value: "vllm-default-password"
      {{- end }}
      # ... rest of container spec
    
    - name: reranker
      env:
        - name: VLLM_MODEL_PATH
          value: "/models/BAAI/bge-reranker-v2-m3"
        - name: AIU_WORLD_SIZE
          value: "1"
        {{- if .Values.vllm.authEnabled }}
        - name: VLLM_API_KEY
          value: "vllm-default-password"
        {{- end }}
      # ... rest of container spec
```

#### OpenShift Implementation

**Step 1: Create Kubernetes Secret**

```yaml
# vllm-password-secret.yaml
{{- if .Values.vllm.authEnabled }}
apiVersion: v1
kind: Secret
metadata:
  name: "vllm-password"
  labels:
    ai-services.io/application: {{ .Release.Name }}
    ai-services.io/template: {{ .Chart.Name }}
type: Opaque
stringData:
  password: "vllm-default-password"
{{- end }}
```

**Step 2: Reference Secret in InferenceServices (as environment variable)**

vLLM natively reads the `VLLM_API_KEY` environment variable:

```yaml
# instruct-inferenceservice.yaml
spec:
  predictor:
    model:
      {{- if .Values.vllm.authEnabled }}
      env:
      - name: VLLM_SPYRE_USE_CB
        value: "1"
      - name: VLLM_API_KEY
        valueFrom:
          secretKeyRef:
            name: vllm-password
            key: password
      {{- else }}
      env:
      - name: VLLM_SPYRE_USE_CB
        value: "1"
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
      {{- if .Values.vllm.authEnabled }}
      env:
      - name: VLLM_API_KEY
        valueFrom:
          secretKeyRef:
            name: vllm-password
            key: password
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
      {{- if .Values.vllm.authEnabled }}
      env:
      - name: VLLM_API_KEY
        valueFrom:
          secretKeyRef:
            name: vllm-password
            key: password
      {{- end }}
      args:
      - '--tensor-parallel-size=1'
      - --served-model-name=BAAI/bge-reranker-v2-m3
      # ... rest of spec
```

**Step 3: Reference Secret in Client Deployments (FastAPI apps)**

FastAPI applications receive the password via environment variable and use it in Authorization headers:

```yaml
# backend-deployment.yaml
{{- if .Values.vllm.authEnabled }}
spec:
  template:
    spec:
      containers:
      - name: server
        env:
        - name: VLLM_API_KEY
          valueFrom:
            secretKeyRef:
              name: vllm-password
              key: password
{{- end }}
```

#### Behavior Matrix

| authEnabled | vLLM Behavior |
|-------------|---------------|
| true | Authentication enabled with hardcoded password |
| false | Authentication disabled |

### 6.2 Client-Side (FastAPI Python Services)

FastAPI applications receive the password via environment variable and use it in Authorization headers when making requests to vLLM:

```python
import os
import requests

# Read password from environment variable (set from Kubernetes Secret or Podman env)
VLLM_API_KEY = os.getenv("VLLM_API_KEY", "")

# Use password in Authorization header for vLLM API calls
def get_vllm_headers():
    """Get headers for vLLM API calls."""
    headers = {}
    if VLLM_API_KEY:
        headers["Authorization"] = f"Bearer {VLLM_API_KEY}"
    return headers

# Example usage in API calls
headers = get_vllm_headers()
response = requests.post(vllm_url, headers=headers, json=payload)
```

**Implementation Notes**:
- Password read from `VLLM_API_KEY` environment variable
- Environment variable populated from Kubernetes Secret (OpenShift) or template (Podman)
- Same password used for all vLLM services (instruct, embedding, reranker)
- Authorization header only added when password is present
- No hardcoding in Python code - password comes from deployment configuration

### 6.3 CLI Implementation (Go)

#### Password Handling

```go
package auth

const (
    // Hardcoded password for all vLLM services
    VLLMPassword = "vllm-default-password"
)
```

**Note**:
- Single hardcoded password for all services
- No password encoding or transformation
- Password is embedded in the code, not configurable via --params

#### Integration Points

1. Load template values with `vllm.authEnabled` flag (default: true)
2. Render templates with hardcoded password when authEnabled is true
3. Deploy application

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
    if values.vllm.authEnabled {
        fmt.Println("✓ vLLM authentication enabled with hardcoded password")
        fmt.Println("  All services will use the same password")
    } else {
        fmt.Println("⚠ vLLM authentication is disabled")
    }
    
    // 3. Render templates with values
    renderedTemplates := tp.RenderTemplates(templateName, values)
    
    // 4. Deploy using rendered templates
    deployApplication(appName, renderedTemplates)
}
```

## 7. Implementation Summary

**Advantages:**
1. ✅ Simple implementation - no cryptography
2. ✅ Single hardcoded password for all services
3. ✅ No password management complexity
4. ✅ Works identically on Podman and OpenShift
5. ✅ Minimal code changes required
6. ✅ Easy to understand and maintain

**Trade-offs:**
- ⚠️ Password is hardcoded and visible in code
- ⚠️ Same password used across all services
- ⚠️ Password transmitted in plain text
- ⚠️ Not suitable for production security requirements
