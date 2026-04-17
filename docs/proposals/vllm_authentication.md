# Design Proposal: vLLM API Authentication

---

## 1. Executive Summary

**vLLM Authentication** provides secure, API key-based authentication for all vLLM inference services (instruct, embedding, and reranker models). By implementing automatic API key generation during application creation with an opt-out mechanism, the system ensures that AI services are secured by default while maintaining flexibility for development and testing scenarios.

## 2. Problem Statement

### Current State
- vLLM services (instruct, embedding, reranker) are deployed **without authentication**
- Any client with network access can consume vLLM APIs
- No access control or audit trail for API usage
- Security risk in production environments

### Requirements
1. **Secure by Default**: Authentication must be enabled automatically during application creation
2. **Automatic Key Generation**: API keys should be generated without manual intervention
3. **Opt-Out Mechanism**: Users can disable authentication via explicit flag for development/testing
4. **Backward Compatible**: Existing deployments should continue to work
5. **Minimal Overhead**: No performance degradation or complex configuration

## 3. Solution Architecture

### 3.1 Authentication Flow

```
Client Service
      |
      | HTTP Request + Authorization: Bearer <api-key>
      v
vLLM Server
      |
      +--> API Key Validation
            |
            +--[Valid Key]-------> Model Inference --> Response
            |
            +--[Invalid/Missing]--> 401 Unauthorized
```

### 3.2 System Components

| Component | Role | Implementation |
|-----------|------|----------------|
| **vLLM Server** | Validates API keys using `--api-key` parameter | Native vLLM support (v0.4.1+) |
| **Client Services** | Include `Authorization: Bearer <key>` header | Python utilities (misc_utils, emb_utils, llm_utils) |
| **Key Derivation** | Derives API keys from password using FIPS-compliant KDF | PBKDF2-HMAC-SHA256 (FIPS 140-2 approved) |
| **Configuration** | Controls auth behavior and stores password | values.yaml with vllm.authEnabled flag |

### 3.3 Key Derivation Architecture

**FIPS-Compliant Password-Based Key Derivation:**

Instead of generating random API keys, the system derives keys from a password using PBKDF2-HMAC-SHA256:

```
Password (from values.yaml)
       |
       v
PBKDF2-HMAC-SHA256
  - Iterations: 600,000
  - Salt: Service-specific (e.g., "vllm-instruct-v1")
  - Output: 32 bytes (256 bits)
       |
       v
Hex-encoded API Key (64 characters)
```

**Key Properties:**
- **FIPS Compliance**: PBKDF2-HMAC-SHA256 is FIPS 140-2 approved (NIST SP 800-132)
- **Deterministic**: Same password + salt always produces same key
- **Service-Specific**: Each service (instruct, embedding, reranker) uses unique salt
- **No Storage**: Keys derived on-demand at startup, never stored
- **Platform-Agnostic**: Works identically on Podman and OpenShift

## 4. Feature Specification

### 4.1 Default Behavior (Authentication Enabled)

When a user creates an application **without** specifying authentication preferences:

```bash
$ ai-services application create my-app -t rag

✓ vLLM authentication enabled with default password
✓ API keys will be derived at runtime using FIPS-compliant PBKDF2

Application 'my-app' created successfully
=====================================
```

**What Happens:**
1. CLI uses default password from values file (`vllm.password`)
2. Password is stored in plain text in values file
3. No API keys are generated or stored
4. Application is deployed using the values file with plain text password
5. At startup, each vLLM server derives its API key from password using PBKDF2
6. At startup, each client service derives API keys from password using PBKDF2
7. Derived keys are used in memory only, never written to disk

**Password Storage:**

| Environment | Storage Method | How Password Is Used |
|-------------|---------------|-------------------|
| **Podman** | Values file (plain text) | Password passed as env var, keys derived via PBKDF2 at container startup |
| **OpenShift** | Kubernetes Secret (plain text from values) | Password passed as env var, keys derived via PBKDF2 at pod startup |

**Note**: Only the password is stored (plain text). API keys are derived on-demand using FIPS-compliant PBKDF2-HMAC-SHA256.

**Key Derivation & Deployment Flow**:
```
1. Store Password (plain text in values file)
2. Deploy Application:
   
   Podman:
   ├─> Read plain text password from values file
   ├─> Pass password as environment variable to containers
   └─> Each container derives its API key using PBKDF2 at startup
   
   OpenShift:
   ├─> Create vllm-password Secret from plain text values
   ├─> Reference Secret in InferenceServices (vLLM pods)
   ├─> Reference Secret in Deployments (client pods)
   └─> Each pod derives API keys using PBKDF2 at startup
```

**Key Derivation Process (Runtime):**
```
For each service (instruct, embedding, reranker):
1. Read plain text password from environment variable
2. Use service-specific salt (e.g., "vllm-instruct-v1")
3. Apply PBKDF2-HMAC-SHA256 (600,000 iterations)
4. Generate 32-byte key
5. Hex-encode to 64-character API key
6. Use key for authentication (vLLM server or client)
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
2. No API keys are generated
3. vLLM servers start without `--api-key` parameter
4. Client services do not include Authorization headers

### 4.3 Using Custom Passwords

Users can provide their own passwords for each service via `--params`:

```bash
# Set password for all services
$ ai-services application create my-app -t rag \
    --params instruct.password=my-instruct-pass,embedding.password=my-embed-pass,reranker.password=my-rerank-pass

# Set password for specific service only
$ ai-services application create my-app -t rag \
    --params instruct.password=custom-instruct-password
```

**What Happens:**
1. CLI receives custom passwords as plain text via `--params`
2. Passwords are passed directly to template rendering 
3. Templates are rendered with custom passwords
4. At runtime, each service derives its API key from its password using PBKDF2

## 5. Configuration Structure

### 5.1 values.yaml Schema

```yaml
vllm:
  authEnabled: true  # Default: true (authentication enabled)
  
  # PBKDF2 parameters (FIPS 140-2 compliant)
  kdf:
    algorithm: "PBKDF2-HMAC-SHA256"
    iterations: 600000  # NIST recommended minimum
    keyLength: 32  # 256 bits

**Note**: Each service has its own password stored in plain text. API keys are never stored - they are derived at runtime using PBKDF2 with the password as cipher text input.

**Service-Specific Salts** (hardcoded in implementation):
- Instruct: `"vllm-instruct-v1"`
- Embedding: `"vllm-embedding-v1"`
- Reranker: `"vllm-reranker-v1"`

### 5.2 Configuration Logic

```
IF vllm.authEnabled == true:
    FOR each service (instruct, embedding, reranker):
        IF service.password is empty:
            Use default password from values.yaml
        ELSE:
            Use user-provided password (from --params)
        
        Store password in plain text in values file
        
        At Runtime:
            Read plain text password from environment variable
            Derive API key using PBKDF2:
                - Password: plain text password (cipher text input)
                - Salt: service-specific (e.g., "vllm-instruct-v1")
                - Iterations: 600,000
                - Hash: SHA-256
                - Output: 32 bytes → hex-encoded (64 chars)
            Use derived key for authentication
ELSE:
    Do not use authentication
```

## 6. Implementation Details

### 6.1 Server-Side (vLLM)

#### PBKDF2 Key Derivation Helper Script

Both Podman and OpenShift use the same shell-based key derivation:

```bash
#!/bin/sh
# derive_vllm_key.sh - FIPS-compliant key derivation using OpenSSL

derive_key() {
    local password="$1"
    local salt="$2"
    local iterations=600000
    local keylen=32
    
    # Use OpenSSL's PBKDF2 (FIPS 140-2 approved)
    # Output: 32 bytes hex-encoded = 64 characters
    echo -n "$password" | openssl enc -pbkdf2 \
        -pass stdin \
        -S "$(echo -n "$salt" | xxd -p)" \
        -iter $iterations \
        -md sha256 \
        -P 2>/dev/null | grep "key=" | cut -d'=' -f2
}

# Usage: derive_key "password" "salt"
```

#### Podman Implementation

Each vLLM server reads its own plain text password and derives API key at startup:

```yaml
# vllm-server.yaml.tmpl
{{- if .Values.vllm.authEnabled }}
containers:
  - name: instruct
    environment:
      - VLLM_INSTRUCT_PASSWORD={{ .Values.instruct.password }}
      - VLLM_SERVICE_SALT=vllm-instruct-v1
    command:
      - sh
      - -c
      - |
        # Derive API key using PBKDF2 (FIPS-compliant)
        # Password is used directly as cipher text input
        export VLLM_API_KEY=$(echo -n "$VLLM_INSTRUCT_PASSWORD" | openssl enc -pbkdf2 \
          -pass stdin \
          -S "$(echo -n "$VLLM_SERVICE_SALT" | xxd -p)" \
          -iter 600000 \
          -md sha256 \
          -P 2>/dev/null | grep "key=" | cut -d'=' -f2)
        
        # Start vLLM with derived key
        vllm serve --api-key ${VLLM_API_KEY} \
          --model ${VLLM_MODEL_PATH} \
          --port 8000
  
  - name: embedding
    environment:
      - VLLM_EMBEDDING_PASSWORD={{ .Values.embedding.password }}
      - VLLM_SERVICE_SALT=vllm-embedding-v1
    command:
      - sh
      - -c
      - |
        export VLLM_API_KEY=$(echo -n "$VLLM_EMBEDDING_PASSWORD" | openssl enc -pbkdf2 \
          -pass stdin \
          -S "$(echo -n "$VLLM_SERVICE_SALT" | xxd -p)" \
          -iter 600000 \
          -md sha256 \
          -P 2>/dev/null | grep "key=" | cut -d'=' -f2)
        
        vllm serve --api-key ${VLLM_API_KEY} \
          --model ${VLLM_MODEL_PATH} \
          --port 8001
  
  - name: reranker
    environment:
      - VLLM_RERANKER_PASSWORD={{ .Values.reranker.password }}
      - VLLM_SERVICE_SALT=vllm-reranker-v1
    command:
      - sh
      - -c
      - |
        export VLLM_API_KEY=$(echo -n "$VLLM_RERANKER_PASSWORD" | openssl enc -pbkdf2 \
          -pass stdin \
          -S "$(echo -n "$VLLM_SERVICE_SALT" | xxd -p)" \
          -iter 600000 \
          -md sha256 \
          -P 2>/dev/null | grep "key=" | cut -d'=' -f2)
        
        vllm serve --api-key ${VLLM_API_KEY} \
          --model ${VLLM_MODEL_PATH} \
          --port 8002
{{- end }}
```

#### OpenShift Implementation

**Step 1: Create Kubernetes Secret** (similar to opensearch-credentials-secret.yaml)

```yaml
# vllm-passwords-secret.yaml
apiVersion: v1
kind: Secret
metadata:
  name: "vllm-passwords"
  labels:
    ai-services.io/application: {{ .Release.Name }}
    ai-services.io/template: {{ .Chart.Name }}
type: Opaque
stringData:
  # Each service has its own password stored in plain text from values
  instruct-password: {{ .Values.instruct.password | quote }}
  embedding-password: {{ .Values.embedding.password | quote }}
  reranker-password: {{ .Values.reranker.password | quote }}
```

**Note**: The Secret stores plain text passwords for each service. API keys are derived at runtime.

**Step 2: Reference Secret in InferenceServices**

```yaml
# instruct-inferenceservice.yaml
spec:
  predictor:
    model:
      env:
      - name: VLLM_INSTRUCT_PASSWORD
        valueFrom:
          secretKeyRef:
            name: vllm-passwords
            key: instruct-password
      - name: VLLM_SERVICE_SALT
        value: "vllm-instruct-v1"
      command:
      - sh
      - -c
      args:
      - |
        # Derive API key using PBKDF2 (FIPS-compliant)
        # Password is used directly as cipher text input
        export VLLM_API_KEY=$(echo -n "$VLLM_INSTRUCT_PASSWORD" | openssl enc -pbkdf2 \
          -pass stdin \
          -S "$(echo -n "$VLLM_SERVICE_SALT" | xxd -p)" \
          -iter 600000 \
          -md sha256 \
          -P 2>/dev/null | grep "key=" | cut -d'=' -f2)
        
        # Start vLLM with derived key
        vllm serve --api-key=${VLLM_API_KEY} \
          --model /mnt/models/ibm-granite/granite-3.3-8b-instruct \
          --tensor-parallel-size=4 \
          --max-model-len=32768
```

```yaml
# embedding-inferenceservice.yaml
spec:
  predictor:
    model:
      env:
      - name: VLLM_EMBEDDING_PASSWORD
        valueFrom:
          secretKeyRef:
            name: vllm-passwords
            key: embedding-password
      - name: VLLM_SERVICE_SALT
        value: "vllm-embedding-v1"
      # ... similar PBKDF2 derivation ...
```

```yaml
# reranker-inferenceservice.yaml
spec:
  predictor:
    model:
      env:
      - name: VLLM_RERANKER_PASSWORD
        valueFrom:
          secretKeyRef:
            name: vllm-passwords
            key: reranker-password
      - name: VLLM_SERVICE_SALT
        value: "vllm-reranker-v1"
      # ... similar PBKDF2 derivation ...
```

**Step 3: Reference Secret in Client Deployments**

```yaml
# backend-deployment.yaml
spec:
  template:
    spec:
      containers:
      - name: server
        env:
        - name: VLLM_INSTRUCT_PASSWORD
          valueFrom:
            secretKeyRef:
              name: vllm-passwords
              key: instruct-password
        - name: VLLM_EMBEDDING_PASSWORD
          valueFrom:
            secretKeyRef:
              name: vllm-passwords
              key: embedding-password
        - name: VLLM_RERANKER_PASSWORD
          valueFrom:
            secretKeyRef:
              name: vllm-passwords
              key: reranker-password
```

**Note**: Client services will use each service's plain text password to derive the corresponding keys in their application code (see section 6.2).

#### Behavior Matrix

| authEnabled | password | vLLM Behavior |
|-------------|----------|---------------|
| true | present | Authentication enabled, keys derived from password |
| true | empty | Authentication enabled, keys derived from default password |
| false | present | Authentication disabled (password ignored) |
| false | empty | Authentication disabled |

### 6.2 Client-Side (Python Services)

All Python services load plain text passwords for each service, derive API keys using PBKDF2, and include Authorization headers:

```python
import os
import hashlib

def derive_vllm_key(password: str, salt: str, iterations: int = 600000) -> str:
    """
    Derive API key using PBKDF2-HMAC-SHA256 (FIPS 140-2 approved).
    
    Args:
        password: The password (cipher text) to derive key from
        salt: Service-specific salt (e.g., "vllm-instruct-v1")
        iterations: Number of PBKDF2 iterations (default: 600,000)
    
    Returns:
        Hex-encoded API key (64 characters)
    """
    key = hashlib.pbkdf2_hmac(
        'sha256',
        password.encode('utf-8'),
        salt.encode('utf-8'),
        iterations,
        dklen=32  # 256 bits
    )
    return key.hex()

def load_vllm_keys():
    """Load plain text passwords and derive API keys for all vLLM services."""
    # Read plain text passwords from environment variables
    instruct_password = os.getenv("VLLM_INSTRUCT_PASSWORD")
    embedding_password = os.getenv("VLLM_EMBEDDING_PASSWORD")
    reranker_password = os.getenv("VLLM_RERANKER_PASSWORD")
    
    # Derive keys for each service using its password as cipher text
    instruct_key = derive_vllm_key(instruct_password, "vllm-instruct-v1") if instruct_password else None
    embedding_key = derive_vllm_key(embedding_password, "vllm-embedding-v1") if embedding_password else None
    reranker_key = derive_vllm_key(reranker_password, "vllm-reranker-v1") if reranker_password else None
    
    return instruct_key, embedding_key, reranker_key

# Load keys at module initialization
VLLM_INSTRUCT_API_KEY, VLLM_EMBEDDING_API_KEY, VLLM_RERANKER_API_KEY = load_vllm_keys()

# Add to headers when making API calls
if VLLM_EMBEDDING_API_KEY:
    headers["Authorization"] = f"Bearer {VLLM_EMBEDDING_API_KEY}"
```

**Security Notes**:
- Each service's password is read directly from its own environment variable (plain text)
- Passwords are used as cipher text input to PBKDF2-HMAC-SHA256 (FIPS 140-2 approved)
- Derived keys exist only in process memory
- No keys are written to disk
- Python's `hashlib.pbkdf2_hmac` uses OpenSSL's FIPS-validated implementation when available

### 6.3 CLI Implementation (Go)

#### Password Handling (No Encoding, No Key Generation)

```go
package auth

const (
    // Default passwords for vLLM services
    DefaultInstructPassword  = "default-instruct-password"
    DefaultEmbeddingPassword = "default-embedding-password"
    DefaultRerankerPassword  = "default-reranker-password"
)
```

**Note**:
- No random key generation - keys are derived at runtime
- No password encoding - passwords stored in plain text
- Each service has its own password
- Passwords are used directly as cipher text input to PBKDF2
- Key derivation happens in containers/pods using PBKDF2

#### Integration Points

1. Load template values with default passwords for each service
2. Check `vllm.authEnabled` value (default: true)
3. Merge `--params` with template values (params override defaults)
4. Render templates with merged values
5. Deploy application (passwords will be used as cipher text for key derivation at runtime)

**Implementation Flow**:
```go
// Pseudo-code for create.go
func Create(appName, template string, params map[string]string) error {
    // 1. Load template values (includes default passwords)
    tp := templates.NewEmbedTemplateProvider(templates.EmbedOptions{Runtime: runtimeType})
    values, err := tp.LoadValues(templateName, valuesFiles, params)
    if err != nil {
        return err
    }
    
    // 2. Params are already merged into values by LoadValues()
    // No need to manually handle password params - they override defaults automatically
    
    // 3. Display information (no keys to show)
    if values.vllm.authEnabled {
        fmt.Println("✓ vLLM authentication enabled")
        fmt.Println("  API keys will be derived at runtime using FIPS-compliant PBKDF2")
        
        // Show which passwords were customized
        if _, exists := params["instruct.password"]; exists {
            fmt.Println("  Using custom password for instruct service")
        }
        if _, exists := params["embedding.password"]; exists {
            fmt.Println("  Using custom password for embedding service")
        }
        if _, exists := params["reranker.password"]; exists {
            fmt.Println("  Using custom password for reranker service")
        }
    }
    
    // 4. Render templates with merged values (includes passwords)
    renderedTemplates := tp.RenderTemplates(templateName, values)
    
    // 5. Deploy using rendered templates
    deployApplication(appName, renderedTemplates)
}
```

**Note**: No values file is written back. The `--params` are merged with template values during rendering and used directly for deployment.

## 7. Implementation Approaches Summary

**Advantages:**
1. ✅ FIPS 140-2 compliant (NIST SP 800-132)
2. ✅ No application logic for key generation
3. ✅ Keys derived on-demand, never stored
4. ✅ Works identically on Podman and OpenShift
5. ✅ Simple password management (single value)
6. ✅ Deterministic (same password → same keys)
7. ✅ Service isolation (unique salts)

**Implementation Locations:**
- **Init Container**: Not needed - derivation is fast (<1 second)
- **Startup Command**: ✅ Recommended - derive keys in container/pod startup script
- **Application Code**: ✅ For Python clients - derive in application initialization

**Why Startup Command:**
- Simple shell script using OpenSSL (universally available)
- No additional containers or complexity
- Keys derived just-in-time before vLLM starts
- Works in both Podman and OpenShift without changes

## 8. Next Steps

### Implementation Phases

**Phase 1: Update Configuration Schema**
- [ ] Update values.yaml to include `vllm.password` and `vllm.kdf` sections
- [ ] Remove `instruct.apiKey`, `embedding.apiKey`, `reranker.apiKey` fields
- [ ] Set default password (Base64-encoded)

**Phase 2: Update Deployment Templates**
- [ ] Podman: Update vllm-server.yaml.tmpl with PBKDF2 derivation script
- [ ] OpenShift: Create vllm-password-secret.yaml
- [ ] OpenShift: Update InferenceService templates with PBKDF2 derivation
- [ ] OpenShift: Update client Deployment templates with password reference

**Phase 3: Update CLI**
- [ ] Remove random key generation code
- [ ] Remove password encoding/decoding (use plain text)
- [ ] Update application creation flow to handle plain text passwords
- [ ] Update user messaging (no keys to display)

**Phase 4: Update Client Services**
- [ ] Add PBKDF2 key derivation function to Python utilities
- [ ] Update llm_utils, emb_utils to derive keys from password
- [ ] Test key derivation consistency

**Phase 5: Testing**
- [ ] Unit tests for PBKDF2 implementation
- [ ] Integration tests for Podman deployment
- [ ] Integration tests for OpenShift deployment
- [ ] FIPS compliance validation
- [ ] Performance benchmarking

**Phase 6: Documentation**
- [ ] Update user documentation
- [ ] Add security best practices guide
- [ ] Document password rotation procedure
- [ ] Add troubleshooting guide
