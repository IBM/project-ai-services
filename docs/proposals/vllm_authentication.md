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

**What Happens:**
1. `vllm.instruct.apiKey` field is empty/unset in values
2. vLLM instruct server starts without authentication
3. Client services do not include Authorization headers
4. No API key storage or secrets created

### 4.2 Enabling Authentication (Opt-In)

Users enable authentication by providing an API key via the `--params` flag:

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

The QnA service's `/v1/chat/completions` endpoint should validate vLLM authentication before doing any retrieval or generation work. The check is performed by calling `GET /v1/models` using the same Authorization header that will be used for the later chat completion request. If the auth check succeeds, the normal QnA flow continues. If it fails, the endpoint returns early with an authentication error for vLLM.

**Implementation in `common/llm_utils.py`:**

```python
import os
import time
import requests
import common.misc_utils as misc_utils
from common.misc_utils import get_logger
from common.retry_utils import retry_on_transient_error

logger = get_logger("LLM")

# Read instruct API key from environment variable
VLLM_INSTRUCT_API_KEY = os.getenv("VLLM_INSTRUCT_API_KEY", "")

def get_vllm_headers():
    """Get headers for vLLM API calls, including auth if configured."""
    headers = {
        "accept": "application/json",
        "Content-type": "application/json",
    }

    if VLLM_INSTRUCT_API_KEY:
        headers["Authorization"] = f"Bearer {VLLM_INSTRUCT_API_KEY}"
        logger.debug("Using vLLM API key for authentication")

    return headers


@retry_on_transient_error(max_retries=3, initial_delay=1.0, backoff_multiplier=2.0)
def query_vllm_models(llm_endpoint):
    """Used both for listing models and as an auth/availability preflight check."""
    if misc_utils.SESSION is None:
        raise RuntimeError("LLM session not initialized. Call create_llm_session() first.")

    logger.debug("Querying VLLM models for auth validation")
    response = misc_utils.SESSION.get(
        f"{llm_endpoint}/v1/models",
        headers=get_vllm_headers(),
    )
    response.raise_for_status()
    return response.json()


def validate_vllm_auth(llm_endpoint):
    """
    Validate that the configured credentials can access vLLM.
    Returns True on success, raises requests.HTTPError on failure.
    """
    query_vllm_models(llm_endpoint)
    return True


def query_vllm_payload(question, documents, llm_endpoint, llm_model, stop_words, max_new_tokens, temperature, stream, lang):
    # ... existing context and prompt logic ...

    headers = get_vllm_headers()
    payload = {
        "messages": [{"role": "user", "content": prompt}],
        "model": llm_model,
        "max_tokens": max_new_tokens,
        "repetition_penalty": 1.1,
        "temperature": temperature,
        "stop": stop_words,
        "stream": stream,
    }
    if stream:
        payload["stream_options"] = {"include_usage": True}
    return headers, payload


@retry_on_transient_error(max_retries=3, initial_delay=1.0, backoff_multiplier=2.0)
def query_vllm_non_stream(question, documents, llm_endpoint, llm_model, stop_words, max_new_tokens, temperature, perf_stat_dict, lang):
    if misc_utils.SESSION is None:
        raise RuntimeError("LLM session not initialized. Call create_llm_session() first.")

    headers, payload = query_vllm_payload(
        question, documents, llm_endpoint, llm_model,
        stop_words, max_new_tokens, temperature, False, lang
    )

    start_time = time.time()
    response = misc_utils.SESSION.post(
        f"{llm_endpoint}/v1/chat/completions",
        json=payload,
        headers=headers,
        stream=False,
    )
    perf_stat_dict["inference_time"] = time.time() - start_time
    response.raise_for_status()
    return response.json()


def query_vllm_stream(question, documents, llm_endpoint, llm_model, stop_words, max_new_tokens, temperature, perf_stat_dict, lang):
    if misc_utils.SESSION is None:
        raise RuntimeError("LLM session not initialized. Call create_llm_session() first.")

    headers, payload = query_vllm_payload(
        question, documents, llm_endpoint, llm_model,
        stop_words, max_new_tokens, temperature, True, lang
    )

    with misc_utils.SESSION.post(
        f"{llm_endpoint}/v1/chat/completions",
        json=payload,
        headers=headers,
        stream=True,
    ) as r:
        r.raise_for_status()
        # ... rest of streaming logic ...
```

**Implementation in `chatbot/app.py`:**

```python
import requests
from common.llm_utils import query_vllm_stream, query_vllm_non_stream, query_vllm_models, validate_vllm_auth

@app.post("/v1/chat/completions")
async def chat_completion(req: ChatCompletionRequest) -> ChatCompletionResponse | StreamingResponse:
    if not req.messages:
        APIError.raise_error(ErrorCode.EMPTY_INPUT, "messages can't be empty")

    query = req.messages[0].content
    if not query or not query.strip():
        APIError.raise_error(ErrorCode.EMPTY_INPUT, "Query cannot be empty")

    if vectorstore is None:
        await ensure_vectorstore_initialized()

    try:
        emb_model = emb_model_dict['emb_model']
        emb_endpoint = emb_model_dict['emb_endpoint']
        emb_max_tokens = emb_model_dict['max_tokens']
        llm_model = llm_model_dict['llm_model']
        llm_endpoint = llm_model_dict['llm_endpoint']
        reranker_model = reranker_model_dict['reranker_model']
        reranker_endpoint = reranker_model_dict['reranker_endpoint']

        # Step 1: validate vLLM auth first
        try:
            await asyncio.to_thread(validate_vllm_auth, llm_endpoint)
        except requests.HTTPError as e:
            auth_message = "Authentication failed while connecting to vLLM."
            if e.response is not None:
                auth_message = f"{auth_message} Response status: {e.response.status_code}"
            APIError.raise_error(ErrorCode.INVALID_PARAMETER, auth_message)

        # Step 2: continue with existing QnA flow only after auth succeeds
        is_valid, error_msg = await asyncio.to_thread(validate_query_length, query, emb_endpoint)
        if not is_valid:
            if req.stream:
                async def stream_query_length_error():
                    message = "Your query is too long. Please shorten it and try again."
                    yield f"data: {json.dumps({'choices': [{'delta': {'content': message}}]})}\n\n"
                return StreamingResponse(stream_query_length_error(), media_type="text/event-stream")
            APIError.raise_error(ErrorCode.INVALID_PARAMETER, error_msg)

        lang = detect_language(query)

        docs, perf_stat_dict = await asyncio.to_thread(
            search_only,
            query,
            emb_model, emb_endpoint, emb_max_tokens,
            reranker_model, reranker_endpoint,
            settings.num_chunks_post_search,
            settings.num_chunks_post_reranker,
            vectorstore=vectorstore
        )

        # ... existing docs-not-found, concurrency, stream/non-stream logic ...
    except Exception as e:
        APIError.raise_error(ErrorCode.INTERNAL_SERVER_ERROR, repr(e))
```

**Behavior Summary:**

1. Build vLLM headers from `VLLM_INSTRUCT_API_KEY`
2. Call `GET {llm_endpoint}/v1/models`
3. If response is `200 OK`, continue with retrieval + reranking + `/v1/chat/completions`
4. If response is `401/403` or another auth-related failure, stop immediately and return an auth error to the client
5. This avoids doing unnecessary QnA work when vLLM credentials are invalid
