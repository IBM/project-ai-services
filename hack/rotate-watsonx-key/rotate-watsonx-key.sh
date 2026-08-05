#!/bin/bash
# rotate-watsonx-key.sh
#
# Rotates the Watsonx API key for an AI Services RAG application on the Podman
# runtime without deleting the application or its data.
#
# USAGE:
#   export WATSONX_APIKEY_NEW="<new-key>"
#   ./hack/rotate-watsonx-key/rotate-watsonx-key.sh <app-name>
#
# EXAMPLE:
#   export WATSONX_APIKEY_NEW="iamApiKey-..."
#   ./hack/rotate-watsonx-key/rotate-watsonx-key.sh rag-test

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
TEMPLATE_DIR="${SCRIPT_DIR}/../../ai-services/assets/components/llm/watsonx/podman/templates"
SECRET_TMPL="${TEMPLATE_DIR}/watsonx-secret.yaml.tmpl"
SERVER_TMPL="${TEMPLATE_DIR}/watsonx-server.yaml.tmpl"

# ── Args ──────────────────────────────────────────────────────────────────────
APP_NAME="${1:-}"
if [[ -z "${APP_NAME}" ]]; then
    echo "Usage: $0 <app-name>" >&2
    exit 1
fi

if [[ -z "${WATSONX_APIKEY_NEW:-}" ]]; then
    echo "Error: WATSONX_APIKEY_NEW is not set. Export the new API key before running." >&2
    exit 1
fi

if [[ ! -f "${SECRET_TMPL}" || ! -f "${SERVER_TMPL}" ]]; then
    echo "Error: Template files not found under ${TEMPLATE_DIR}" >&2
    exit 1
fi

# ── Discover LLM pod ─────────────────────────────────────────────────────────
# `ai-services application ps` writes its table to stderr via klog, so 2>&1
# captures it. grep -oE extracts the pod name directly regardless of column
# position or padding.
echo "→ Discovering LLM pod for application '${APP_NAME}'..."

LLM_POD=$(ai-services application ps "${APP_NAME}" --runtime podman 2>&1 \
    | grep -oE 'llm-[a-z0-9]+' \
    | head -1)

if [[ -z "${LLM_POD}" ]]; then
    echo "Error: No LLM pod found for application '${APP_NAME}'." \
         "Is the application deployed and running?" >&2
    exit 1
fi

# Derive slug: pod name is always "llm-<slug>"
SLUG="${LLM_POD#llm-}"
SECRET_NAME="watsonx-secret-${SLUG}"

# The litellm container inside the pod is named "litellm" in the template;
# Podman prefixes it with the pod name: "<pod-name>-<container-name>"
CONTAINER_NAME="${LLM_POD}-litellm"

echo "  Pod:       ${LLM_POD}"
echo "  Slug:      ${SLUG}"
echo "  Secret:    ${SECRET_NAME}"
echo "  Container: ${CONTAINER_NAME}"

# ── Validate this is a Watsonx pod ───────────────────────────────────────────
# All three Podman LLM providers (watsonx, vllm-cpu, vllm-spyre) name their pod
# "llm-<slug>". The ai-services.io/secret label distinguishes them:
#   watsonx  → watsonx-secret-<slug>
#   vllm-*   → vllm-secret-<slug>  (or absent when no API key is configured)
# Running this script against a vLLM pod would delete the wrong secret and
# replay it with the Watsonx templates, corrupting the deployment.
POD_SECRET_LABEL=$(podman pod inspect "${LLM_POD}" \
    --format '{{index .Labels "ai-services.io/secret"}}' 2>/dev/null || true)

if [[ "${POD_SECRET_LABEL}" != "${SECRET_NAME}" ]]; then
    echo "Error: Pod '${LLM_POD}' is not a Watsonx pod." \
         "(ai-services.io/secret='${POD_SECRET_LABEL}', expected '${SECRET_NAME}')" >&2
    exit 1
fi

# ── Discover TemplateID via podman pod inspect ────────────────────────────────
echo "→ Reading TemplateID from pod label..."
TEMPLATE_ID=$(podman pod inspect "${LLM_POD}" \
    --format '{{index .Labels "ai-services.io/template"}}')

if [[ -z "${TEMPLATE_ID}" ]]; then
    echo "Error: Pod '${LLM_POD}' has no ai-services.io/template label." \
         "The catalog will lose track of this pod." >&2
    exit 1
fi
echo "  TemplateID: ${TEMPLATE_ID}"

# ── Discover config values from the running container ────────────────────────
echo "→ Reading configuration from running container '${CONTAINER_NAME}'..."

get_env() {
    podman inspect "${CONTAINER_NAME}" \
        --format '{{range .Config.Env}}{{.}}{{"\n"}}{{end}}' \
        | grep "^${1}=" | cut -d= -f2-
}

WATSONX_PROJECT_ID=$(get_env "WATSONX_PROJECT_ID")
WATSONX_URL=$(get_env "WATSONX_URL")
INSTRUCT_MODEL_NAME=$(get_env "INSTRUCT_MODEL_NAME")
IMAGE=$(podman inspect "${CONTAINER_NAME}" --format '{{.Config.Image}}')

if [[ -z "${WATSONX_PROJECT_ID}" || -z "${WATSONX_URL}" \
      || -z "${INSTRUCT_MODEL_NAME}" || -z "${IMAGE}" ]]; then
    echo "Error: Could not read one or more required values from container." \
         "Check that '${CONTAINER_NAME}' is running." >&2
    exit 1
fi

echo "  Image:              ${IMAGE}"
echo "  WATSONX_PROJECT_ID: ${WATSONX_PROJECT_ID}"
echo "  WATSONX_URL:        ${WATSONX_URL}"
echo "  INSTRUCT_MODEL:     ${INSTRUCT_MODEL_NAME}"

# ── Render templates to a secure temp file ───────────────────────────────────
echo "→ Rendering pod templates..."
YAML_FILE=$(mktemp /tmp/llm-rotate-XXXXXX.yaml)
chmod 600 "${YAML_FILE}"
# ROTATION_STARTED is set just before the first destructive step.
# Before that: any exit deletes the YAML (API key must not be left on disk).
# After that: failure preserves the YAML so the operator can recover with:
#   podman kube play <file>
ROTATION_STARTED=0
cleanup_yaml() {
    local exit_code=$?
    if [[ ${exit_code} -eq 0 || ${ROTATION_STARTED} -eq 0 ]]; then
        rm -f "${YAML_FILE}"
    else
        echo "⚠ Rendered manifest preserved for manual recovery:" >&2
        echo "    podman kube play ${YAML_FILE}" >&2
    fi
}
trap cleanup_yaml EXIT
trap 'exit 130' INT TERM

render_template() {
    # Use @ as the sed delimiter so that values containing | or / (e.g. image
    # references and URLs) don't break the substitution.
    sed \
        -e "s@{{ .InstanceSlug }}@${SLUG}@g" \
        -e "s@{{ .TemplateID }}@${TEMPLATE_ID}@g" \
        -e "s@{{ .Values.watsonxApiKey }}@${WATSONX_APIKEY_NEW}@g" \
        -e "s@{{ .Values.watsonxProjectId }}@${WATSONX_PROJECT_ID}@g" \
        -e "s@{{ .Values.watsonxUrl }}@${WATSONX_URL}@g" \
        -e "s@{{ .Values.model }}@${INSTRUCT_MODEL_NAME}@g" \
        -e "s@{{ .Values.image }}@${IMAGE}@g" \
        "$1"
}

{
    render_template "${SECRET_TMPL}"
    echo "---"
    render_template "${SERVER_TMPL}"
} > "${YAML_FILE}"

# ── Rotate: stop pod → remove pod → remove secret → replay ───────────────────
# Podman secrets are immutable: UpdateSecret returns "unsupported method".
# The pod must be recreated (not just restarted) because LiteLLM reads the
# key once at startup via `export WATSONX_APIKEY=$(cat ...)`.
ROTATION_STARTED=1
echo "→ Stopping pod '${LLM_POD}'..."
podman pod stop "${LLM_POD}"

echo "→ Removing pod '${LLM_POD}'..."
podman pod rm "${LLM_POD}"

echo "→ Removing old Podman secret '${SECRET_NAME}'..."
podman secret rm "${SECRET_NAME}"

echo "→ Replaying pod with new key (podman kube play)..."
podman kube play "${YAML_FILE}"

# ── Verify ────────────────────────────────────────────────────────────────────
echo "→ Waiting for container to start..."
sleep 5
ACTIVE_KEY=$(podman exec "${CONTAINER_NAME}" cat /etc/secret/watsonx-secret/apiKey 2>/dev/null || echo "")
EXPECTED_PREFIX="${WATSONX_APIKEY_NEW:0:8}"
ACTIVE_PREFIX="${ACTIVE_KEY:0:8}"

if [[ "${ACTIVE_PREFIX}" == "${EXPECTED_PREFIX}" ]]; then
    echo "✓ Key rotated successfully (mounted key starts with: ${ACTIVE_PREFIX}...)."
else
    echo "⚠ Warning: mounted key prefix '${ACTIVE_PREFIX}' does not match expected" \
         "'${EXPECTED_PREFIX}'. Check container logs:" >&2
    echo "    podman logs ${CONTAINER_NAME}" >&2
fi

echo ""
echo "✓ Done. Verify end-to-end functionality via the Q&A interface."
echo "  To confirm LiteLLM is healthy:"
echo "    podman exec ${CONTAINER_NAME} curl -s http://localhost:8000/health/readiness"
