# Design Proposal: vLLM API Authentication

---

## 1. Executive Summary

**vLLM Authentication** provides simple, API key-based authentication for the vLLM instruct inference service. Authentication is controlled by the presence of an API key supplied via environment variables - if an API key is provided, authentication is enabled; otherwise, it remains disabled. This approach ensures flexibility, security, and simplicity.

## 2. Problem Statement

### Current State
- vLLM instruct service is deployed **without authentication**
- Any client with network access can consume vLLM APIs
- No access control or audit trail for API usage
- Security risk in production environments

### Requirements
1. **Simple Configuration**: User-Provided API Key - The system authenticates using an API key supplied directly by the user
2. **Default Behavior**: Authentication disabled by default (no API key = no auth)
3. **Opt-In Mechanism**: Users enable authentication by providing API keys
4. **Simple Setup**: Single API key for instruct service
5. **Minimal Overhead**: No performance degradation or complex configuration

## 3. Solution Architecture

### 3.1 Authentication Flow

```
Client Service
      |
      | HTTP Request + Authorization: Bearer <service_api_key>
      v
vLLM Instruct Server
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
| **Client Services** | Include `Authorization: Bearer <api_key>` header | Python utilities (llm_utils) |
| **Configuration** | API key supplied via parameter or env var | values.yaml with instruct key |

### 3.3 API Key Architecture

**Instruct Service API Key:**

Users provide an API key for the vLLM instruct service:

**Key Properties:**
- **User-Controlled**: API keys provided by user, not hardcoded
- **Optional**: If no API key supplied, authentication is disabled
- **Plain Text**: No encryption or encoding

## 4. Feature Specification

### 4.1 Default Behavior (Authentication Disabled)

When a user creates an application **without** specifying API keys:

```bash
$ ai-services application create my-app -t rag

⚠ vLLM authentication is disabled (no API key provided)

Application 'my-app' created successfully
=====================================
```

**What Happens:**
1. `vllm.instruct.apiKey` field is empty/unset in values
2. vLLM instruct server starts without authentication
3. Client services do not include Authorization headers
4. No API key storage or secrets created

### 4.2 Enabling Authentication (Opt-In)

Users enable authentication by providing an API key via the `--params` flag:

#### Enable authentication for instruct service:
```bash
$ ai-services application create my-app -t rag \
  --params vllm.instruct.apiKey=instruct-key-123

✓ vLLM authentication enabled for instruct service

Application 'my-app' created successfully
=====================================
```

**What Happens:**
1. API key is set for instruct service in values
2. API key is passed to vLLM instruct server via VLLM_API_KEY env var
3. Client services use the API key when calling instruct service

**API Key Usage:**

| Environment | How API Keys Are Used |
|-------------|-------------------|
| **Podman** | API key passed directly to vLLM instruct via env var |
| **OpenShift** | API key stored in Kubernetes Secret, passed to vLLM instruct via env var |

**Deployment Flow**:
```
1. User Provides API Keys (via --params or env vars)
2. Deploy Application:
   
   Podman:
   ├─> Pass instruct API key to instruct container env var
   └─> Client services use API key for instruct service
   
   OpenShift:
   ├─> Create vllm-instruct-api-key Secret (if provided)
   ├─> Reference Secret in instruct InferenceService
   ├─> Reference Secret in client Deployments
   └─> Client services use API key for instruct service
```

## 5. Configuration Structure

### 5.1 values.yaml Schema

```yaml
vllm:
  instruct:
    apiKey: ""  # Default: empty (authentication disabled)
```

### 5.2 Configuration Logic

```
IF vllm.instruct.apiKey is set (non-empty):
    Pass API key to VLLM_API_KEY env var for instruct service
    Client services use API key in Authorization headers for instruct service
    Authentication is ENABLED for instruct service
ELSE:
    Do not set VLLM_API_KEY env var for instruct service
    Client services do not include Authorization headers for instruct service
    Authentication is DISABLED for instruct service
```

## 6. Implementation Details

### 6.1 Server-Side (vLLM)

vLLM natively reads the `VLLM_API_KEY` environment variable for authentication without needing the `--api-key` parameter.

#### Podman Implementation

The vLLM instruct server conditionally sets its API key as environment variable:

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
```

#### OpenShift Implementation

**Step 1: Create Kubernetes Secret (only if API key is provided)**

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


**Step 2: Reference Secret in InferenceService (as environment variable)**

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
```

#### Behavior Matrix

| Service | API Key Status | vLLM Behavior |
|---------|----------------|---------------|
| Instruct | Set (non-empty) | Authentication enabled with instruct API key |
| Instruct | Unset (empty) | Authentication disabled |

### 6.2 Client-Side (FastAPI Python Services)

FastAPI applications receive the instruct API key via environment variable and use it in Authorization headers when making requests to vLLM:

```python
import os
import requests

# Read API key from environment variable (set from Kubernetes Secret or Podman env)
VLLM_INSTRUCT_API_KEY = os.getenv("VLLM_INSTRUCT_API_KEY", "")

# Use API key in Authorization header for vLLM instruct API calls
def get_vllm_instruct_headers():
    """Get headers for vLLM instruct API calls."""
    headers = {}
    
    if VLLM_INSTRUCT_API_KEY:
        headers["Authorization"] = f"Bearer {VLLM_INSTRUCT_API_KEY}"
    
    return headers

# Example usage in API calls
headers = get_vllm_instruct_headers()
response = requests.post(instruct_url, headers=headers, json=payload)
```

#### QnA Service - Chat Completion Endpoint

The QnA service's `/v1/chat/completions` endpoint uses `llm_utils.py` functions to communicate with vLLM. Authentication is handled in the `llm_utils` module by adding the Authorization header to all vLLM requests.

**Implementation in `common/llm_utils.py`:**

```python
import os
import requests
from common.misc_utils import get_logger

logger = get_logger("LLM")

# Read instruct API key from environment variable
VLLM_INSTRUCT_API_KEY = os.getenv("VLLM_INSTRUCT_API_KEY", "")

def get_vllm_headers():
    """
    Get headers for vLLM API calls, including authentication if configured.
    
    Returns:
        dict: Headers dictionary with Authorization header if API key is set
    """
    headers = {
        "accept": "application/json",
        "Content-type": "application/json"
    }
    
    # Add Authorization header if API key is configured
    if VLLM_INSTRUCT_API_KEY:
        headers["Authorization"] = f"Bearer {VLLM_INSTRUCT_API_KEY}"
        logger.debug("Using vLLM API key for authentication")
    
    return headers

# Update existing functions to use get_vllm_headers()

def query_vllm_payload(question, documents, llm_endpoint, llm_model, stop_words, max_new_tokens, temperature,
                stream, lang):
    # ... existing context and prompt logic ...
    
    # Use the new header function
    headers = get_vllm_headers()
    
    payload = {
        "messages": [{"role": "user", "content": prompt}],
        "model": llm_model,
        "max_tokens": max_new_tokens,
        "repetition_penalty": 1.1,
        "temperature": temperature,
        "stop": stop_words,
        "stream": stream
    }
    if stream:
        payload["stream_options"] = {"include_usage": True}
    return headers, payload

@retry_on_transient_error(max_retries=3, initial_delay=1.0, backoff_multiplier=2.0)
def query_vllm_non_stream(question, documents, llm_endpoint, llm_model, stop_words, max_new_tokens, temperature, perf_stat_dict, lang):
    if misc_utils.SESSION is None:
        raise RuntimeError("LLM session not initialized. Call create_llm_session() first.")

    headers, payload = query_vllm_payload(question, documents, llm_endpoint, llm_model, stop_words, max_new_tokens, temperature, False, lang)
    
    # Headers now include Authorization if API key is set
    start_time = time.time()
    response = misc_utils.SESSION.post(f"{llm_endpoint}/v1/chat/completions", json=payload, headers=headers, stream=False)
    request_time = time.time() - start_time
    perf_stat_dict["inference_time"] = request_time
    response.raise_for_status()
    response_json = response.json()
    # ... rest of function ...

def query_vllm_stream(question, documents, llm_endpoint, llm_model, stop_words, max_new_tokens, temperature, perf_stat_dict, lang):
    if misc_utils.SESSION is None:
        raise RuntimeError("LLM session not initialized. Call create_llm_session() first.")

    headers, payload = query_vllm_payload(question, documents, llm_endpoint, llm_model, stop_words, max_new_tokens,
                                          temperature, True, lang)
    try:
        # Headers now include Authorization if API key is set
        with misc_utils.SESSION.post(f"{llm_endpoint}/v1/chat/completions", json=payload, headers=headers, stream=True) as r:
            # ... rest of streaming logic ...

@retry_on_transient_error(max_retries=3, initial_delay=1.0, backoff_multiplier=2.0)
def query_vllm_models(llm_endpoint):
    if misc_utils.SESSION is None:
        raise RuntimeError("LLM session not initialized. Call create_llm_session() first.")

    logger.debug('Querying VLLM models')
    headers = get_vllm_headers()
    response = misc_utils.SESSION.get(f"{llm_endpoint}/v1/models", headers=headers)
    response.raise_for_status()
    return response.json()

@retry_on_transient_error(max_retries=3, initial_delay=1.0, backoff_multiplier=2.0)
def classify_single_text(prompt, gen_model, llm_endpoint):
    if misc_utils.SESSION is None:
        raise RuntimeError("LLM session not initialized. Call create_llm_session() first.")

    headers = get_vllm_headers()
    payload = {
        "model": gen_model,
        "messages": [{"role": "user", "content": prompt}],
        "temperature": 0,
        "max_tokens": 3,
    }
    response = misc_utils.SESSION.post(f"{llm_endpoint}/v1/chat/completions", json=payload, headers=headers)
    response.raise_for_status()
    # ... rest of function ...

@retry_on_transient_error(max_retries=3, initial_delay=1.0, backoff_multiplier=2.0)
def query_vllm_summarize(llm_endpoint, messages, model, max_tokens, temperature):
    if misc_utils.SESSION is None:
        raise RuntimeError("LLM session not initialized. Call create_llm_session() first.")

    headers = get_vllm_headers()
    stop_words = [w for w in settings.summarization_stop_words.split(",") if w]
    payload = {
        "messages": messages,
        "model": model,
        "max_tokens": max_tokens,
        "temperature": temperature,
    }
    if stop_words:
        payload["stop"] = stop_words

    response = misc_utils.SESSION.post(
        f"{llm_endpoint}/v1/chat/completions",
        json=payload,
        headers=headers,
        stream=False,
    )
    response.raise_for_status()
    # ... rest of function ...
```

### 6.3 CLI Implementation (Go)

#### API Key Handling

No hardcoded API keys in the CLI. API keys are user-supplied via `--params`:

```go
// No hardcoded API key constants needed
// API key comes from user via:
//   --params vllm.instruct.apiKey=<value>
```

#### Integration Points

1. Load template values with `vllm.instruct.apiKey` field (default: empty)
2. User can override via `--params vllm.instruct.apiKey=<value>`
3. Render templates with user-provided API key if set
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
    
    // 3. Render templates with values
    renderedTemplates := tp.RenderTemplates(templateName, values)
    
    // 4. Deploy using rendered templates
    deployApplication(appName, renderedTemplates)
}
```
