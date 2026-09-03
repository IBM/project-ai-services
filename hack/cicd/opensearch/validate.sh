#!/usr/bin/env bash
# =============================================================================
# validate.sh — smoke-test an ai-services OpenSearch container image
#
# Validates that the image works correctly for the ai-services RAG use case:
#   - SSL + basic auth enabled (matches production; security plugin ON)
#   - Hybrid search pipeline (normalization-processor, min_max, arithmetic_mean)
#   - k-NN index with HNSW/lucene/cosinesimil, ef_construction=128, m=24
#   - Dense, sparse, and hybrid search queries
#   - delete_by_query on metadata.doc_id (used by remove_docs_from_index)
#   - Bulk document indexing (chunk_id, embedding, text, metadata fields)
#
# This mirrors the exact configuration in:
#   services/common/opensearch.py  (_create_pipeline, _setup_index, search)
#   ai-services/assets/components/vector_db/opensearch/openshift/
#
# Usage:
#   ./validate.sh [--image <image>] [--version <version>] [--port <port>]
#                 [--timeout <seconds>] [--keep]
#
#   --image    <ref>   Full image ref to validate
#                      Default: icr.io/ai-services-private/opensearch:<version>-ppc64le-extended
#   --version  <ver>   OpenSearch version string to assert (e.g. 3.8.0)
#                      Default: 3.8.0
#   --port     <port>  Host port to bind OpenSearch on
#                      Default: 9299
#   --timeout  <sec>   Seconds to wait for OpenSearch to become healthy
#                      Default: 180
#   --keep             Leave the container running after the run (for inspection)
#
# Exit codes:
#   0  All checks passed
#   1  One or more checks failed
# =============================================================================

set -euo pipefail

# ---------------------------------------------------------------------------
# Defaults
# ---------------------------------------------------------------------------
OPENSEARCH_VERSION="3.8.0"
IMAGE_NAME=""
HOST_PORT="9299"
STARTUP_TIMEOUT=180
KEEP_CONTAINER=false

# Credentials that match the production secret pattern
# (security plugin is ON — same as the statefulset)
# Note: OpenSearch 3.x rejects passwords that contain the username ("admin"),
# so the password must not include that substring.
OS_USER="admin"
OS_PASS="S3cur3#Srvc2024"   # strong: upper+lower+digit+special, no username substring

# ---------------------------------------------------------------------------
# Argument parsing
# ---------------------------------------------------------------------------
while [[ $# -gt 0 ]]; do
    case "$1" in
        --image)    IMAGE_NAME="$2";           shift 2 ;;
        --version)  OPENSEARCH_VERSION="$2";   shift 2 ;;
        --port)     HOST_PORT="$2";            shift 2 ;;
        --timeout)  STARTUP_TIMEOUT="$2";      shift 2 ;;
        --keep)     KEEP_CONTAINER=true;       shift   ;;
        *)
            echo "Unknown option: $1" >&2
            echo "Usage: $0 [--image <ref>] [--version <ver>] [--port <port>] [--timeout <sec>] [--keep]" >&2
            exit 1
            ;;
    esac
done

if [[ -z "${IMAGE_NAME}" ]]; then
    IMAGE_NAME="icr.io/ai-services-private/opensearch:${OPENSEARCH_VERSION}-ppc64le-extended"
fi

BASE_URL="https://127.0.0.1:${HOST_PORT}"
AUTH="-u ${OS_USER}:${OS_PASS} --insecure"   # matches verify_certs=False in the Python client
CONTAINER_NAME="opensearch-validate-$$"

# Embedding dimension used throughout the test — must be consistent across
# index creation, document indexing, and search queries.
EMBED_DIM=4

# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------
log()  { echo -e "\n\033[1;36m>>> [$(date '+%H:%M:%S')] $*\033[0m"; }
ok()   { echo -e "    \033[1;32m✔ $*\033[0m"; }
warn() { echo -e "    \033[1;33m⚠ $*\033[0m"; }
fail() { echo -e "    \033[1;31m✘ $*\033[0m" >&2; FAILURES=$(( FAILURES + 1 )); }
FAILURES=0

# curl wrapper: always use auth + insecure TLS (mirrors verify_certs=False)
os_curl() { curl -s ${AUTH} "$@"; }

check_http() {
    local label="$1" expected_code="$2"; shift 2
    local code
    code=$(curl -s -o /dev/null -w "%{http_code}" ${AUTH} "$@" || echo "000")
    if [[ "${code}" == "${expected_code}" ]]; then
        ok "${label}: HTTP ${code}"
    else
        fail "${label}: expected HTTP ${expected_code}, got ${code}"
    fi
}

json_field() {
    # json_field <url> <python_expression>
    python3 -c "import sys,json; d=json.load(sys.stdin); print(${2})" \
        < <(os_curl "$1")
}

# ---------------------------------------------------------------------------
# Cleanup — runs on EXIT
# ---------------------------------------------------------------------------
cleanup() {
    if [[ "${KEEP_CONTAINER}" == true ]]; then
        warn "Container '${CONTAINER_NAME}' left running (--keep was set)."
        warn "Stop it: podman stop ${CONTAINER_NAME} && podman rm ${CONTAINER_NAME}"
        return
    fi
    echo ""
    log "Cleaning up container '${CONTAINER_NAME}'..."
    podman stop  "${CONTAINER_NAME}" &>/dev/null || true
    podman rm -f "${CONTAINER_NAME}" &>/dev/null || true
    ok "Container removed."
}
trap cleanup EXIT

# ---------------------------------------------------------------------------
# Pre-flight: image must exist locally
# ---------------------------------------------------------------------------
log "Verifying image exists: ${IMAGE_NAME}"
if ! podman image exists "${IMAGE_NAME}"; then
    echo "Image '${IMAGE_NAME}' not found locally." >&2
    echo "Pull or build it first (see build.sh), then re-run." >&2
    exit 1
fi
ok "Image found."

# ---------------------------------------------------------------------------
# Start the container
# Security plugin is intentionally ON (DISABLE_SECURITY_PLUGIN is NOT set)
# to match the production statefulset which sets OPENSEARCH_INITIAL_ADMIN_PASSWORD.
# ---------------------------------------------------------------------------
log "Starting container '${CONTAINER_NAME}' (security plugin ENABLED)..."
podman run -d \
    --name "${CONTAINER_NAME}" \
    --ulimit nofile=65536:65536 \
    --ulimit nproc=4096:4096 \
    -e "discovery.type=single-node" \
    -e "cluster.name=opensearch-cluster" \
    -e "OPENSEARCH_INITIAL_ADMIN_PASSWORD=${OS_PASS}" \
    -e "OPENSEARCH_JAVA_OPTS=-Xms1g -Xmx1g" \
    -p "127.0.0.1:${HOST_PORT}:9200" \
    "${IMAGE_NAME}"
ok "Container started."

# ---------------------------------------------------------------------------
# Wait for OpenSearch to become healthy (HTTPS with auth)
# ---------------------------------------------------------------------------
log "Waiting for OpenSearch to become healthy (timeout: ${STARTUP_TIMEOUT}s)..."
elapsed=0
until os_curl -o /dev/null -w "%{http_code}" \
        "${BASE_URL}/_cluster/health" 2>/dev/null | grep -qE '^200$'; do
    if [[ "${elapsed}" -ge "${STARTUP_TIMEOUT}" ]]; then
        echo ""
        echo "ERROR: OpenSearch did not become healthy within ${STARTUP_TIMEOUT} seconds." >&2
        podman logs "${CONTAINER_NAME}" >&2 || true
        exit 1
    fi
    printf "  ... waiting (%ds elapsed)\r" "${elapsed}"
    sleep 5
    elapsed=$(( elapsed + 5 ))
done
echo ""
ok "OpenSearch is healthy after ${elapsed}s."

# ---------------------------------------------------------------------------
# CHECK 1 — Cluster health is green or yellow
# ---------------------------------------------------------------------------
log "CHECK 1 — Cluster health"
health_status=$(json_field "${BASE_URL}/_cluster/health" "d['status']")
if [[ "${health_status}" == "green" || "${health_status}" == "yellow" ]]; then
    ok "Cluster health: ${health_status}"
else
    fail "Cluster health: expected green or yellow, got '${health_status}'"
fi

# ---------------------------------------------------------------------------
# CHECK 2 — Reported version matches expected
# ---------------------------------------------------------------------------
log "CHECK 2 — OpenSearch version"
reported_version=$(json_field "${BASE_URL}/" "d['version']['number']")
if [[ "${reported_version}" == "${OPENSEARCH_VERSION}" ]]; then
    ok "Version: ${reported_version}"
else
    fail "Version: expected '${OPENSEARCH_VERSION}', got '${reported_version}'"
fi

# ---------------------------------------------------------------------------
# CHECK 3 — Required plugins are installed
# These are the plugins actually exercised by services/common/opensearch.py:
#   opensearch-knn          → knn_vector mapping + k-NN search
#   opensearch-ml           → normalization-processor pipeline
#   opensearch-neural-search → hybrid query type
# ---------------------------------------------------------------------------
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

# ---------------------------------------------------------------------------
# CHECK 4 — Hybrid search pipeline (mirrors _create_pipeline in opensearch.py)
#
# Pipeline: normalization-processor
#   normalization:  min_max
#   combination:    arithmetic_mean  weights=[0.3, 0.7]
# ---------------------------------------------------------------------------
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

# Verify the pipeline was stored
pipeline_tech=$(os_curl "${BASE_URL}/_search/pipeline/hybrid_pipeline" \
    | python3 -c "
import sys, json
d = json.load(sys.stdin)
proc = d['hybrid_pipeline']['phase_results_processors'][0]
norm = proc['normalization-processor']
print(norm['normalization']['technique'])
" 2>/dev/null || echo "unknown")
if [[ "${pipeline_tech}" == "min_max" ]]; then
    ok "Pipeline normalization technique: ${pipeline_tech}"
else
    fail "Pipeline normalization technique: expected 'min_max', got '${pipeline_tech}'"
fi

# ---------------------------------------------------------------------------
# CHECK 5 — k-NN index creation (mirrors _setup_index in opensearch.py)
#
# Settings:
#   index.knn = true
#   knn.algo_param.ef_search = 100
#   number_of_shards = 1
#   auto_expand_replicas = 0-all
# Mapping:
#   chunk_id:  long
#   embedding: knn_vector / hnsw / lucene / cosinesimil / ef_construction=128 / m=24
#   text:      text / standard analyzer
#   metadata:  dynamic=true with keyword/integer/date sub-fields
# ---------------------------------------------------------------------------
log "CHECK 5 — k-NN index with HNSW/lucene/cosinesimil mapping"
TEST_INDEX="ai-services-validate-$$"

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

# ---------------------------------------------------------------------------
# CHECK 6 — Bulk document indexing (mirrors insert_chunks / helpers.bulk)
#
# Three documents, each with the full production schema:
#   chunk_id, embedding, text, metadata.{filename,doc_id,type,source,language}
# doc_id "doc-aaa" used for delete_by_query test later.
# ---------------------------------------------------------------------------
log "CHECK 6 — Bulk document indexing (chunk_id + knn_vector + metadata)"

BULK_BODY='{"index":{"_index":"'"${TEST_INDEX}"'","_id":"1001"}}
{"chunk_id":1001,"embedding":[0.1,0.2,0.3,0.4],"text":"OpenSearch hybrid search for AI services","metadata":{"filename":"guide.pdf","doc_id":"doc-aaa","type":"text","source":"Chapter 1","language":"en","page_number":1,"chunk_index":0,"total_chunks":3}}
{"index":{"_index":"'"${TEST_INDEX}"'","_id":"1002"}}
{"chunk_id":1002,"embedding":[0.5,0.6,0.7,0.8],"text":"k-NN vector search using lucene engine","metadata":{"filename":"guide.pdf","doc_id":"doc-aaa","type":"text","source":"Chapter 2","language":"en","page_number":2,"chunk_index":1,"total_chunks":3}}
{"index":{"_index":"'"${TEST_INDEX}"'","_id":"1003"}}
{"chunk_id":1003,"embedding":[0.9,0.1,0.2,0.3],"text":"Normalization processor for hybrid scoring","metadata":{"filename":"other.pdf","doc_id":"doc-bbb","type":"text","source":"Section 1","language":"en","page_number":1,"chunk_index":0,"total_chunks":1}}
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

# ---------------------------------------------------------------------------
# CHECK 7 — Dense (k-NN) search
# Mirrors mode="dense" in search() — uses knn query with language filter
# ---------------------------------------------------------------------------
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

# ---------------------------------------------------------------------------
# CHECK 8 — Sparse (BM25) search
# Mirrors mode="sparse" in search() — bool/must match with language filter
# ---------------------------------------------------------------------------
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

# ---------------------------------------------------------------------------
# CHECK 9 — Hybrid search with pipeline
# Mirrors mode="hybrid" in search() — uses hybrid query + search_pipeline param
# This is the default mode and the primary RAG search path.
# ---------------------------------------------------------------------------
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

# ---------------------------------------------------------------------------
# CHECK 10 — delete_by_query on metadata.doc_id
# Mirrors remove_docs_from_index() and delete_document_by_id() in opensearch.py
# ---------------------------------------------------------------------------
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

# Verify doc-bbb still exists after targeted deletion
remaining=$(os_curl "${BASE_URL}/${TEST_INDEX}/_count" \
    | python3 -c "import sys,json; print(json.load(sys.stdin)['count'])" 2>/dev/null || echo "-1")
if [[ "${remaining}" -eq 1 ]]; then
    ok "Remaining document count after deletion: ${remaining} (doc-bbb intact)."
else
    fail "Expected 1 remaining document after deletion, got ${remaining}"
fi

# Cleanup test index
os_curl -X DELETE "${BASE_URL}/${TEST_INDEX}" -o /dev/null || true

# ---------------------------------------------------------------------------
# CHECK 11 — Node info: architecture and JVM
# ---------------------------------------------------------------------------
log "CHECK 11 — Node info"
arch=$(json_field "${BASE_URL}/_nodes/_local/os" \
    "list(d['nodes'].values())[0]['os']['arch']" 2>/dev/null || echo "unknown")
jvm_version=$(json_field "${BASE_URL}/_nodes/_local/jvm" \
    "list(d['nodes'].values())[0]['jvm']['version']" 2>/dev/null || echo "unknown")
ok "OS arch    : ${arch}"
ok "JVM version: ${jvm_version}"

# ---------------------------------------------------------------------------
# Result summary
# ---------------------------------------------------------------------------
echo ""
log "Validation complete."
echo ""
if [[ "${FAILURES}" -eq 0 ]]; then
    echo -e "\033[1;32m  ✔ All checks passed for ${IMAGE_NAME}\033[0m"
    echo ""
    exit 0
else
    echo -e "\033[1;31m  ✘ ${FAILURES} check(s) FAILED for ${IMAGE_NAME}\033[0m"
    echo ""
    if [[ "${KEEP_CONTAINER}" == false ]]; then
        echo "  Tip: re-run with --keep to leave the container running for investigation."
    fi
    exit 1
fi
