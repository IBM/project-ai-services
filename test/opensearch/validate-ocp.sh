#!/usr/bin/env bash
# =============================================================================
# validate-ocp.sh — Validate an OpenSearch image on an OpenShift (OCP) cluster
#
# Deploys OpenSearch on an OCP cluster (using temporary resources or the OpenShift
# Helm chart/manifests), port-forwards to localhost, and runs the complete
# validation suite (cluster health, version matching, k-NN, neural/hybrid search,
# delete_by_query, etc.).
#
# Prerequisites:
#   - `oc` or `kubectl` CLI logged into target OCP cluster
#   - `curl` and `python3`
#
# Usage:
#   ./validate-ocp.sh [--image <image>] [--version <version>] [--namespace <ns>]
#                     [--port <local-port>] [--timeout <seconds>] [--keep]
#
# Flags:
#   --image       <ref>    Image to deploy & test (default: icr.io/ai-services-private/opensearch:3.8.0-ppc64le-14)
#   --version     <ver>    OpenSearch version to assert (default: 3.8.0)
#   --namespace   <ns>     OCP namespace/project to use (default: opensearch-validate-<timestamp>)
#   --port        <port>   Local port for port-forwarding (default: 19200)
#   --timeout     <sec>    Timeout in seconds for pod startup and readiness (default: 300)
#   --pull-secret <name>   Name of an existing Kubernetes secret (type=kubernetes.io/dockerconfigjson)
#                          in the *source* namespace used to pull images from the private registry.
#                          If omitted, the script tries 'icr-pull-secret' in the 'default' namespace.
#   --keep                 Keep the OCP namespace and resources running after test
# =============================================================================

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# ---------------------------------------------------------------------------
# Defaults
# ---------------------------------------------------------------------------
OPENSEARCH_VERSION="3.8.0"
IMAGE_NAME=""
NAMESPACE=""
LOCAL_PORT="19200"
TIMEOUT_SECS=300
KEEP_RESOURCES=false
CUSTOM_NAMESPACE=false
CREATED_NAMESPACE=false
PULL_SECRET_NAME=""
PULL_SECRET_SOURCE_NS="default"

# Unique test run ID to label all created resources
RUN_ID="os-val-$$"
LABEL_KEY="ai-services.io/validation-run"
LABEL_VALUE="${RUN_ID}"

# System namespaces that must NEVER be deleted or modified
PROTECTED_NAMESPACES=(
    "default"
    "kube-system"
    "kube-public"
    "kube-node-lease"
    "openshift"
    "openshift-apiserver"
    "openshift-authentication"
    "openshift-config"
    "openshift-config-managed"
    "openshift-console"
    "openshift-controller-manager"
    "openshift-dns"
    "openshift-etcd"
    "openshift-image-registry"
    "openshift-infra"
    "openshift-ingress"
    "openshift-ingress-operator"
    "openshift-kube-apiserver"
    "openshift-kube-apiserver-operator"
    "openshift-kube-controller-manager"
    "openshift-kube-controller-manager-operator"
    "openshift-kube-scheduler"
    "openshift-kube-scheduler-operator"
    "openshift-machine-api"
    "openshift-machine-config-operator"
    "openshift-marketplace"
    "openshift-monitoring"
    "openshift-multus"
    "openshift-network-diagnostics"
    "openshift-network-operator"
    "openshift-node"
    "openshift-operator-lifecycle-manager"
    "openshift-operators"
    "openshift-ovn-kubernetes"
    "openshift-sdn"
    "openshift-service-ca"
    "openshift-storage"
)

OS_USER="admin"
OS_PASS="AiServices@12345"

# ---------------------------------------------------------------------------
# Parse Arguments
# ---------------------------------------------------------------------------
while [[ $# -gt 0 ]]; do
    case "$1" in
        --image)        IMAGE_NAME="$2";         shift 2 ;;
        --version)      OPENSEARCH_VERSION="$2"; shift 2 ;;
        --namespace)    NAMESPACE="$2"; CUSTOM_NAMESPACE=true; shift 2 ;;
        --port)         LOCAL_PORT="$2";         shift 2 ;;
        --timeout)      TIMEOUT_SECS="$2";       shift 2 ;;
        --pull-secret)  PULL_SECRET_NAME="$2";   shift 2 ;;
        --keep)         KEEP_RESOURCES=true;     shift   ;;
        -h|--help)
            sed -ne '/^#/!q;s/^#//;p' "$0"
            exit 0
            ;;
        *)
            echo "Unknown option: $1" >&2
            echo "Run '$0 --help' for usage." >&2
            exit 1
            ;;
    esac
done

if [[ -z "${IMAGE_NAME}" ]]; then
    IMAGE_NAME="icr.io/ai-services-private/opensearch:3.8.0-ppc64le-14"
fi

if [[ -z "${NAMESPACE}" ]]; then
    NAMESPACE="os-val-$(date +%s)"
fi

# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------
log()  { echo -e "\n\033[1;36m>>> [$(date '+%H:%M:%S')] $*\033[0m"; }
ok()   { echo -e "    \033[1;32m✔ $*\033[0m"; }
warn() { echo -e "    \033[1;33m⚠ $*\033[0m"; }
fail() { echo -e "    \033[1;31m✘ $*\033[0m" >&2; FAILURES=$(( FAILURES + 1 )); }
FAILURES=0

CLI="oc"
if ! command -v oc &>/dev/null; then
    if command -v kubectl &>/dev/null; then
        CLI="kubectl"
    else
        echo "ERROR: Neither 'oc' nor 'kubectl' command was found in PATH." >&2
        exit 1
    fi
fi

# ---------------------------------------------------------------------------
# Cleanup handler
# ---------------------------------------------------------------------------
PF_PID=""

cleanup() {
    echo ""
    log "Initiating safe cleanup..."

    # 1. Stop background port-forward process
    if [[ -n "${PF_PID}" ]] && kill -0 "${PF_PID}" 2>/dev/null; then
        echo "Stopping port-forward (PID: ${PF_PID})..."
        kill "${PF_PID}" 2>/dev/null || true
    fi

    if [[ "${KEEP_RESOURCES}" == true ]]; then
        warn "Resources left running in namespace '${NAMESPACE}' (--keep was set)."
        warn "To inspect: ${CLI} get all -n ${NAMESPACE} -l ${LABEL_KEY}=${LABEL_VALUE}"
        return
    fi

    # 2. Delete ONLY resources tagged with our unique run label
    echo "Cleaning up resources created during validation (label: ${LABEL_KEY}=${LABEL_VALUE})..."
    ${CLI} delete statefulset,service,secret,pod,pvc -n "${NAMESPACE}" -l "${LABEL_KEY}=${LABEL_VALUE}" --ignore-not-found &>/dev/null || true
    # Also clean up PVCs created by volumeClaimTemplates (not labelled automatically by StatefulSet)
    ${CLI} delete pvc -n "${NAMESPACE}" -l "app=opensearch-validate" --ignore-not-found &>/dev/null || true

    # 3. Only delete namespace if WE created it AND it is not protected
    if [[ "${CREATED_NAMESPACE}" == true ]]; then
        is_protected=false
        for prot in "${PROTECTED_NAMESPACES[@]}"; do
            if [[ "${NAMESPACE}" == "${prot}" || "${NAMESPACE}" =~ ^openshift- || "${NAMESPACE}" =~ ^kube- ]]; then
                is_protected=true
                break
            fi
        done

        if [[ "${is_protected}" == false && "${NAMESPACE}" =~ ^os-val- ]]; then
            echo "Deleting temporary test namespace '${NAMESPACE}'..."
            ${CLI} delete namespace "${NAMESPACE}" --wait=false &>/dev/null || true
        else
            warn "Namespace '${NAMESPACE}' is protected or pre-existing — skipping namespace deletion."
        fi
    fi

    ok "Cleanup completed safely without touching external resources."
}
trap cleanup EXIT

# ---------------------------------------------------------------------------
# Pre-flight: Check Cluster Connectivity
# ---------------------------------------------------------------------------
log "Checking cluster connection with ${CLI}..."
if ! ${CLI} cluster-info &>/dev/null; then
    echo "ERROR: Unable to connect to OpenShift cluster using '${CLI}'." >&2
    echo "Please run 'oc login' or configure your KUBECONFIG." >&2
    exit 1
fi
ok "Connected to cluster."

# ---------------------------------------------------------------------------
# Namespace Setup & Safety Check
# ---------------------------------------------------------------------------
log "Setting up namespace '${NAMESPACE}'..."

# Safeguard check against deploying to system / protected namespaces
for prot in "${PROTECTED_NAMESPACES[@]}"; do
    if [[ "${NAMESPACE}" == "${prot}" || "${NAMESPACE}" =~ ^openshift- || "${NAMESPACE}" =~ ^kube- ]]; then
        echo "ERROR: Namespace '${NAMESPACE}' is a protected cluster/system namespace." >&2
        echo "Refusing to run tests in protected namespaces on a shared cluster." >&2
        exit 1
    fi
done

if ! ${CLI} get namespace "${NAMESPACE}" &>/dev/null; then
    ${CLI} create namespace "${NAMESPACE}"
    CREATED_NAMESPACE=true
    # Label the namespace itself so it can be identified
    ${CLI} label namespace "${NAMESPACE}" "${LABEL_KEY}=${LABEL_VALUE}" --overwrite &>/dev/null || true
    ok "Created temporary namespace '${NAMESPACE}'."
else
    ok "Using existing namespace '${NAMESPACE}' (will only touch test-labeled resources)."
fi

# ---------------------------------------------------------------------------
# Copy image pull secret into the test namespace
# ---------------------------------------------------------------------------
if [[ -z "${PULL_SECRET_NAME}" ]]; then
    PULL_SECRET_NAME="icr-pull-secret"
fi

log "Ensuring image pull secret '${PULL_SECRET_NAME}' is available in namespace '${NAMESPACE}'..."

if ${CLI} get secret "${PULL_SECRET_NAME}" -n "${NAMESPACE}" &>/dev/null; then
    ok "Pull secret '${PULL_SECRET_NAME}' already present in '${NAMESPACE}'."
elif ${CLI} get secret "${PULL_SECRET_NAME}" -n "${PULL_SECRET_SOURCE_NS}" &>/dev/null; then
    ${CLI} get secret "${PULL_SECRET_NAME}" -n "${PULL_SECRET_SOURCE_NS}" -o json \
        | python3 -c "
import sys, json
s = json.load(sys.stdin)
# strip metadata fields that must not be copied
for key in ('resourceVersion', 'uid', 'creationTimestamp', 'namespace',
            'annotations', 'managedFields', 'ownerReferences'):
    s.get('metadata', {}).pop(key, None)
s['metadata']['namespace'] = '${NAMESPACE}'
print(json.dumps(s))
" | ${CLI} apply -n "${NAMESPACE}" -f - &>/dev/null
    ok "Copied pull secret '${PULL_SECRET_NAME}' from '${PULL_SECRET_SOURCE_NS}' to '${NAMESPACE}'."
else
    warn "Pull secret '${PULL_SECRET_NAME}' not found in '${PULL_SECRET_SOURCE_NS}' or '${NAMESPACE}'."
    warn "Image pull may fail if the registry requires authentication."
    warn "Provide an existing secret name with --pull-secret <name> or create it first:"
    warn "  oc create secret docker-registry ${PULL_SECRET_NAME} \\"
    warn "    --docker-server=icr.io \\"
    warn "    --docker-username=iamapikey \\"
    warn "    --docker-password=<ICR_API_KEY> \\"
    warn "    -n ${PULL_SECRET_SOURCE_NS}"
fi

# ---------------------------------------------------------------------------
# Deploy OpenSearch on OCP
# ---------------------------------------------------------------------------
log "Deploying OpenSearch (Image: ${IMAGE_NAME})..."

# The image Dockerfile sets chown $UID:0 + chmod g+rwX on $OPENSEARCH_HOME at build
# time, making /usr/share/opensearch/config group-writable for GID 0 — the group OpenShift
# always assigns regardless of UID. No init container or config volume overlay is needed.

cat <<EOF | ${CLI} apply -n "${NAMESPACE}" -f -
apiVersion: v1
kind: Secret
metadata:
  name: opensearch-credentials
  labels:
    ${LABEL_KEY}: "${LABEL_VALUE}"
    app: opensearch-validate
type: Opaque
stringData:
  username: "${OS_USER}"
  password: "${OS_PASS}"
---
apiVersion: v1
kind: Service
metadata:
  name: opensearch-headless
  labels:
    ${LABEL_KEY}: "${LABEL_VALUE}"
    app: opensearch-validate
spec:
  clusterIP: None
  selector:
    app: opensearch-validate
    ${LABEL_KEY}: "${LABEL_VALUE}"
  ports:
    - name: os-client-port
      port: 9200
      targetPort: 9200
    - name: os-metrics-port
      port: 9600
      targetPort: 9600
---
apiVersion: v1
kind: Service
metadata:
  name: opensearch
  labels:
    ${LABEL_KEY}: "${LABEL_VALUE}"
    app: opensearch-validate
spec:
  selector:
    app: opensearch-validate
    ${LABEL_KEY}: "${LABEL_VALUE}"
  ports:
    - name: os-client-port
      port: 9200
      targetPort: 9200
    - name: os-metrics-port
      port: 9600
      targetPort: 9600
---
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: opensearch
  labels:
    ${LABEL_KEY}: "${LABEL_VALUE}"
    app: opensearch-validate
spec:
  replicas: 1
  serviceName: opensearch-headless
  podManagementPolicy: Parallel
  persistentVolumeClaimRetentionPolicy:
    whenScaled: Delete
  selector:
    matchLabels:
      app: opensearch-validate
      ${LABEL_KEY}: "${LABEL_VALUE}"
  template:
    metadata:
      labels:
        app: opensearch-validate
        ${LABEL_KEY}: "${LABEL_VALUE}"
    spec:
      containers:
        - name: opensearch
          image: "${IMAGE_NAME}"
          imagePullPolicy: Always
          env:
            - name: cluster.name
              value: "opensearch-cluster"
            - name: node.name
              valueFrom:
                fieldRef:
                  apiVersion: v1
                  fieldPath: metadata.name
            - name: cluster.initial_cluster_manager_nodes
              value: "opensearch-0"
            - name: discovery.seed_hosts
              value: "opensearch-headless"
            - name: network.host
              value: "0.0.0.0"
            - name: OPENSEARCH_INITIAL_ADMIN_PASSWORD
              valueFrom:
                secretKeyRef:
                  name: opensearch-credentials
                  key: password
            - name: OPENSEARCH_JAVA_OPTS
              value: "-Xms4g -Xmx4g -XX:MaxDirectMemorySize=2g -XX:-UseSuperWord -XX:TieredStopAtLevel=1"
          ports:
            - name: os-client-port
              containerPort: 9200
            - name: os-metrics-port
              containerPort: 9600
          volumeMounts:
            - name: data
              mountPath: /usr/share/opensearch/data
          resources:
            requests:
              cpu: "2"
              memory: "8Gi"
            limits:
              cpu: "2"
              memory: "8Gi"
          startupProbe:
            tcpSocket:
              port: 9200
            initialDelaySeconds: 5
            periodSeconds: 10
            timeoutSeconds: 3
            failureThreshold: 30
          livenessProbe:
            tcpSocket:
              port: 9200
            periodSeconds: 20
            timeoutSeconds: 3
            failureThreshold: 10
          readinessProbe:
            tcpSocket:
              port: 9200
            periodSeconds: 5
            timeoutSeconds: 3
            failureThreshold: 3
      imagePullSecrets:
        - name: "${PULL_SECRET_NAME}"
  volumeClaimTemplates:
    - metadata:
        name: data
        labels:
          ${LABEL_KEY}: "${LABEL_VALUE}"
          app: opensearch-validate
      spec:
        accessModes:
          - ReadWriteMany
        resources:
          requests:
            storage: "10Gi"
EOF

ok "OpenSearch manifests applied."

# ---------------------------------------------------------------------------
# Wait for Pod rollout & Readiness
# ---------------------------------------------------------------------------
log "Waiting for OpenSearch pod to be ready (timeout: ${TIMEOUT_SECS}s)..."

set +e
${CLI} rollout status statefulset/opensearch -n "${NAMESPACE}" --timeout="${TIMEOUT_SECS}s"
ROLLOUT_EXIT=$?
set -e

if [[ "${ROLLOUT_EXIT}" -ne 0 ]]; then
    echo "ERROR: OpenSearch rollout failed or timed out." >&2
    echo "--- Pod status ---"
    ${CLI} get pods -n "${NAMESPACE}" >&2 || true
    echo "--- Pod logs ---"
    ${CLI} logs statefulset/opensearch -c opensearch -n "${NAMESPACE}" --tail=100 >&2 || true
    exit 1
fi
ok "OpenSearch pod is Ready."

# ---------------------------------------------------------------------------
# Setup Port-Forwarding
# ---------------------------------------------------------------------------
log "Setting up port-forward on 127.0.0.1:${LOCAL_PORT} -> opensearch:9200..."

${CLI} port-forward svc/opensearch "${LOCAL_PORT}:9200" -n "${NAMESPACE}" &>/dev/null &
PF_PID=$!

# Wait for port-forward to be responsive AND for OpenSearch to return a parseable
# JSON body on /_cluster/health.  A 200 HTTP status can arrive while OpenSearch is
# still initialising and returns an empty or partial body; the validation checks
# would then fail with a JSON parse error.  We keep polling until json.load succeeds.
AUTH="-u ${OS_USER}:${OS_PASS} --insecure"
BASE_URL="https://127.0.0.1:${LOCAL_PORT}"

os_ready=false
for i in {1..60}; do
    http_code=$(curl -s ${AUTH} -o /tmp/os_health_$$.json -w "%{http_code}" \
        "${BASE_URL}/_cluster/health" 2>/dev/null || echo "000")
    if [[ "${http_code}" == "200" ]]; then
        # Verify the body is valid JSON with a 'status' field
        if python3 -c "import sys,json; d=json.load(open('/tmp/os_health_$$.json')); assert 'status' in d" 2>/dev/null; then
            os_ready=true
            break
        fi
    fi
    sleep 3
done
rm -f /tmp/os_health_$$.json

if [[ "${os_ready}" != "true" ]]; then
    echo "ERROR: OpenSearch did not return a valid cluster health response on ${BASE_URL}." >&2
    ${CLI} logs statefulset/opensearch -c opensearch -n "${NAMESPACE}" --tail=50 >&2 || true
    exit 1
fi
ok "Port-forward established and OpenSearch API is ready."

# ---------------------------------------------------------------------------
# Run Functional & Compatibility Validation (validate.sh logic)
# ---------------------------------------------------------------------------
os_curl() { curl -s ${AUTH} "$@"; }

json_field() {
    python3 -c "import sys,json; d=json.load(sys.stdin); print(${2})" \
        < <(os_curl "$1")
}

log "Running OpenSearch health and functionality validations..."

# 1. Cluster Health
log "CHECK 1 — Cluster health"
health_status=$(json_field "${BASE_URL}/_cluster/health" "d['status']")
if [[ "${health_status}" == "green" || "${health_status}" == "yellow" ]]; then
    ok "Cluster health: ${health_status}"
else
    fail "Cluster health: expected green or yellow, got '${health_status}'"
fi

# 2. OpenSearch Version
log "CHECK 2 — OpenSearch version"
reported_version=$(json_field "${BASE_URL}/" "d['version']['number']")
if [[ "${reported_version}" == "${OPENSEARCH_VERSION}" ]]; then
    ok "Version: ${reported_version}"
else
    fail "Version: expected '${OPENSEARCH_VERSION}', got '${reported_version}'"
fi

# 3. Required Plugins
log "CHECK 3 — Required plugins (knn, ml, neural-search)"
plugins_json=$(os_curl "${BASE_URL}/_cat/plugins?format=json")
plugin_names=$(python3 -c \
    "import sys,json; [print(p['component']) for p in json.loads(sys.stdin.read())]" \
    <<< "${plugins_json}" 2>/dev/null || echo "")

REQUIRED_PLUGINS=(
    "opensearch-knn"
    "opensearch-ml"
    "opensearch-neural-search"
)
for plugin in "${REQUIRED_PLUGINS[@]}"; do
    if echo "${plugin_names}" | grep -qF "${plugin}"; then
        ok "Plugin present: ${plugin}"
    else
        fail "Plugin missing: ${plugin}"
    fi
done

# 4. Hybrid Search Pipeline
log "CHECK 4 — Hybrid search pipeline (normalization-processor)"
PIPELINE_BODY='{
  "description": "Post-processor for hybrid search",
  "phase_results_processors": [
    {
      "normalization-processor": {
        "normalization": {"technique": "min_max"},
        "combination": {
          "technique": "arithmetic_mean",
          "parameters": {"weights": [0.3, 0.7]}
        }
      }
    }
  ]
}'

pipeline_code=$(curl -s -o /dev/null -w "%{http_code}" ${AUTH} \
    -X PUT "${BASE_URL}/_search/pipeline/hybrid_pipeline" \
    -H "Content-Type: application/json" \
    -d "${PIPELINE_BODY}")
if [[ "${pipeline_code}" == "200" ]]; then
    ok "Hybrid search pipeline created."
else
    fail "Hybrid pipeline PUT returned HTTP ${pipeline_code}"
fi

# 5. k-NN Index Creation
log "CHECK 5 — k-NN index with HNSW/lucene/cosinesimil mapping"
TEST_INDEX="ocp-val-index-$$"
EMBED_DIM=4

INDEX_BODY="{
  \"settings\": {
    \"index\": {
      \"knn\": true,
      \"knn.algo_param.ef_search\": 100,
      \"number_of_shards\": 1,
      \"auto_expand_replicas\": \"0-all\"
    }
  },
  \"mappings\": {
    \"properties\": {
      \"chunk_id\": {\"type\": \"long\"},
      \"embedding\": {
        \"type\": \"knn_vector\",
        \"dimension\": ${EMBED_DIM},
        \"method\": {
          \"name\": \"hnsw\",
          \"space_type\": \"cosinesimil\",
          \"engine\": \"lucene\",
          \"parameters\": {
            \"ef_construction\": 128,
            \"m\": 24
          }
        }
      },
      \"text\": {\"type\": \"text\", \"analyzer\": \"standard\"},
      \"metadata\": {
        \"dynamic\": \"true\",
        \"properties\": {
          \"filename\":    {\"type\": \"keyword\"},
          \"doc_id\":      {\"type\": \"keyword\"},
          \"type\":        {\"type\": \"keyword\"},
          \"source\":      {\"type\": \"keyword\"},
          \"language\":    {\"type\": \"keyword\"},
          \"page_number\": {\"type\": \"integer\"},
          \"chunk_index\": {\"type\": \"integer\"},
          \"total_chunks\":{\"type\": \"integer\"},
          \"created_at\":  {\"type\": \"date\"}
        }
      }
    }
  }
}"

create_code=$(curl -s -o /dev/null -w "%{http_code}" ${AUTH} \
    -X PUT "${BASE_URL}/${TEST_INDEX}" \
    -H "Content-Type: application/json" \
    -d "${INDEX_BODY}")
if [[ "${create_code}" == "200" ]]; then
    ok "k-NN index created: ${TEST_INDEX}"
else
    fail "k-NN index create returned HTTP ${create_code}"
fi

# 6. Bulk Indexing
log "CHECK 6 — Bulk document indexing"
BULK_BODY='{"index":{"_index":"'"${TEST_INDEX}"'","_id":"1001"}}
{"chunk_id":1001,"embedding":[0.1,0.2,0.3,0.4],"text":"OpenSearch hybrid search for AI services on OCP","metadata":{"filename":"guide.pdf","doc_id":"doc-aaa","type":"text","source":"Chapter 1","language":"en","page_number":1,"chunk_index":0,"total_chunks":3}}
{"index":{"_index":"'"${TEST_INDEX}"'","_id":"1002"}}
{"chunk_id":1002,"embedding":[0.5,0.6,0.7,0.8],"text":"k-NN vector search using lucene engine on OpenShift","metadata":{"filename":"guide.pdf","doc_id":"doc-aaa","type":"text","source":"Chapter 2","language":"en","page_number":2,"chunk_index":1,"total_chunks":3}}
{"index":{"_index":"'"${TEST_INDEX}"'","_id":"1003"}}
{"chunk_id":1003,"embedding":[0.9,0.1,0.2,0.3],"text":"Normalization processor for hybrid scoring in cloud","metadata":{"filename":"other.pdf","doc_id":"doc-bbb","type":"text","source":"Section 1","language":"en","page_number":1,"chunk_index":0,"total_chunks":1}}
'

bulk_response=$(os_curl -X POST "${BASE_URL}/_bulk?refresh=true" \
    -H "Content-Type: application/x-ndjson" \
    --data-binary "${BULK_BODY}")

bulk_errors=$(python3 -c \
    "import sys,json; d=json.loads(sys.stdin.read()); print(d.get('errors', True))" \
    <<< "${bulk_response}" 2>/dev/null || echo "true")

if [[ "${bulk_errors}" == "False" || "${bulk_errors}" == "false" ]]; then
    ok "Bulk insert: 3 documents indexed successfully."
else
    fail "Bulk insert reported errors: ${bulk_response}"
fi

# 7. Dense Search
log "CHECK 7 — Dense (k-NN) search"
DENSE_QUERY="{
  \"size\": 2,
  \"_source\": [\"chunk_id\", \"text\", \"metadata\"],
  \"query\": {
    \"knn\": {
      \"embedding\": {
        \"vector\": [0.1, 0.2, 0.3, 0.4],
        \"k\": 9,
        \"filter\": {\"term\": {\"metadata.language\": \"en\"}}
      }
    }
  }
}"

dense_hits=$(os_curl -X POST "${BASE_URL}/${TEST_INDEX}/_search" \
    -H "Content-Type: application/json" \
    -d "${DENSE_QUERY}" \
    | python3 -c "import sys,json; d=json.load(sys.stdin); print(d['hits']['total']['value'])" \
    2>/dev/null || echo "0")

if [[ "${dense_hits}" -ge 1 ]]; then
    ok "Dense search: ${dense_hits} hit(s) returned."
else
    fail "Dense search: expected ≥1 hit, got ${dense_hits}"
fi

# 8. Sparse Search
log "CHECK 8 — Sparse (BM25) search"
SPARSE_QUERY='{
  "size": 2,
  "_source": ["chunk_id", "text", "metadata"],
  "query": {
    "bool": {
      "must": [{"match": {"text": "hybrid"}}],
      "filter": [{"term": {"metadata.language": "en"}}]
    }
  }
}'

sparse_hits=$(os_curl -X POST "${BASE_URL}/${TEST_INDEX}/_search" \
    -H "Content-Type: application/json" \
    -d "${SPARSE_QUERY}" \
    | python3 -c "import sys,json; d=json.load(sys.stdin); print(d['hits']['total']['value'])" \
    2>/dev/null || echo "0")

if [[ "${sparse_hits}" -ge 1 ]]; then
    ok "Sparse search: ${sparse_hits} hit(s) returned."
else
    fail "Sparse search: expected ≥1 hit, got ${sparse_hits}"
fi

# 9. Hybrid Search with Pipeline
log "CHECK 9 — Hybrid search with normalization pipeline"
HYBRID_QUERY="{
  \"size\": 2,
  \"_source\": [\"chunk_id\", \"text\", \"metadata\"],
  \"query\": {
    \"hybrid\": {
      \"queries\": [
        {
          \"knn\": {
            \"embedding\": {
              \"vector\": [0.1, 0.2, 0.3, 0.4],
              \"k\": 9,
              \"filter\": {\"term\": {\"metadata.language\": \"en\"}}
            }
          }
        },
        {
          \"bool\": {
            \"must\": [{\"match\": {\"text\": \"hybrid\"}}],
            \"filter\": [{\"term\": {\"metadata.language\": \"en\"}}]
          }
        }
      ]
    }
  }
}"

hybrid_hits=$(os_curl -X POST \
    "${BASE_URL}/${TEST_INDEX}/_search?search_pipeline=hybrid_pipeline" \
    -H "Content-Type: application/json" \
    -d "${HYBRID_QUERY}" \
    | python3 -c "import sys,json; d=json.load(sys.stdin); print(d['hits']['total']['value'])" \
    2>/dev/null || echo "0")

if [[ "${hybrid_hits}" -ge 1 ]]; then
    ok "Hybrid search: ${hybrid_hits} hit(s) returned via hybrid_pipeline."
else
    fail "Hybrid search: expected ≥1 hit, got ${hybrid_hits}"
fi

# 10. delete_by_query
log "CHECK 10 — delete_by_query on metadata.doc_id"
DELETE_QUERY='{
  "query": {
    "terms": {"metadata.doc_id": ["doc-aaa"]}
  }
}'

delete_response=$(os_curl -X POST \
    "${BASE_URL}/${TEST_INDEX}/_delete_by_query?refresh=true&conflicts=proceed" \
    -H "Content-Type: application/json" \
    -d "${DELETE_QUERY}")

deleted_count=$(python3 -c \
    "import sys,json; d=json.load(sys.stdin); print(d.get('deleted', 0))" \
    <<< "${delete_response}" 2>/dev/null || echo "0")

if [[ "${deleted_count}" -eq 2 ]]; then
    ok "delete_by_query: deleted ${deleted_count} chunk(s) for doc-aaa (expected 2)."
else
    fail "delete_by_query: expected 2 deleted, got ${deleted_count}"
fi

# 11. Node and Environment info
log "CHECK 11 — Node info on OCP"
arch=$(json_field "${BASE_URL}/_nodes/_local/os" \
    "list(d['nodes'].values())[0]['os']['arch']" 2>/dev/null || echo "unknown")
jvm_version=$(json_field "${BASE_URL}/_nodes/_local/jvm" \
    "list(d['nodes'].values())[0]['jvm']['version']" 2>/dev/null || echo "unknown")
ok "Node arch    : ${arch}"
ok "JVM version  : ${jvm_version}"

# Cleanup test index
os_curl -X DELETE "${BASE_URL}/${TEST_INDEX}" -o /dev/null || true

# ---------------------------------------------------------------------------
# Summary
# ---------------------------------------------------------------------------
echo ""
log "Validation completed on OCP namespace: ${NAMESPACE}"
echo ""
if [[ "${FAILURES}" -eq 0 ]]; then
    echo -e "\033[1;32m  ✔ All OCP validation checks passed successfully for ${IMAGE_NAME}\033[0m"
    echo ""
    exit 0
else
    echo -e "\033[1;31m  ✘ ${FAILURES} check(s) FAILED on OCP for ${IMAGE_NAME}\033[0m"
    echo ""
    exit 1
fi
