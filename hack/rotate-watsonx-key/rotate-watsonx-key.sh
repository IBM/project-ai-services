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
#   export WATSONX_APIKEY_NEW="<your-new-api-key>"
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

# ── Discover slug from running pod ────────────────────────────────────────────
echo "→ Discovering LLM pod for application '${APP_NAME}'..."
LLM_POD=$(podman pod ps --format "{{.Name}}" | grep "^llm-" | head -1)
if [[ -z "${LLM_POD}" ]]; then
    echo "Error: No running LLM pod found. Is the application deployed?" >&2
    exit 1
fi

SLUG="${LLM_POD#llm-}"
SECRET_NAME="watsonx-secret-${SLUG}"
CONTAINER_NAME="${LLM_POD}-litellm"
echo "  Pod:    ${LLM_POD}"
echo "  Slug:   ${SLUG}"
echo "  Secret: ${SECRET_NAME}"

# ── Discover TemplateID from catalog DB ───────────────────────────────────────
echo "→ Looking up TemplateID from catalog DB..."
TEMPLATE_ID=$(podman exec ai-services--db-postgresql \
    psql -U postgres -d ai_services -tAc \
    "SELECT DISTINCT c.id FROM components c
     JOIN service_dependencies sd ON sd.dependency_id = c.id
     JOIN services s ON s.id = sd.service_id
     JOIN applications a ON a.id = s.app_id
     WHERE a.name = '${APP_NAME}' AND c.type = 'llm' AND c.provider = 'watsonx'
     LIMIT 1;")

if [[ -z "${TEMPLATE_ID}" ]]; then
    echo "Error: Could not find LLM component for app '${APP_NAME}' in catalog DB." >&2
    exit 1
fi
echo "  TemplateID: ${TEMPLATE_ID}"

# ── Discover values from running container ────────────────────────────────────
echo "→ Reading configuration from running container..."

get_env() {
    podman inspect "${CONTAINER_NAME}" \
        --format '{{range .Config.Env}}{{.}}{{"\n"}}{{end}}' \
        | grep "^${1}=" | cut -d= -f2-
}

WATSONX_PROJECT_ID=$(get_env "WATSONX_PROJECT_ID")
WATSONX_URL=$(get_env "WATSONX_URL")
INSTRUCT_MODEL_NAME=$(get_env "INSTRUCT_MODEL_NAME")
IMAGE=$(podman inspect "${CONTAINER_NAME}" --format '{{.Config.Image}}')

echo "  Image:              ${IMAGE}"
echo "  WATSONX_PROJECT_ID: ${WATSONX_PROJECT_ID}"
echo "  WATSONX_URL:        ${WATSONX_URL}"
echo "  INSTRUCT_MODEL:     ${INSTRUCT_MODEL_NAME}"

# ── Render templates ──────────────────────────────────────────────────────────
echo "→ Rendering templates..."
YAML_FILE=$(mktemp /tmp/llm-rotate-XXXXXX.yaml)
trap 'rm -f "${YAML_FILE}"' EXIT INT TERM

render_template() {
    sed \
        -e "s|{{ .InstanceSlug }}|${SLUG}|g" \
        -e "s|{{ .TemplateID }}|${TEMPLATE_ID}|g" \
        -e "s|{{ .Values.watsonxApiKey }}|${WATSONX_APIKEY_NEW}|g" \
        -e "s|{{ .Values.watsonxProjectId }}|${WATSONX_PROJECT_ID}|g" \
        -e "s|{{ .Values.watsonxUrl }}|${WATSONX_URL}|g" \
        -e "s|{{ .Values.model }}|${INSTRUCT_MODEL_NAME}|g" \
        -e "s|{{ .Values.image }}|${IMAGE}|g" \
        "$1"
}

{
    render_template "${SECRET_TMPL}"
    echo "---"
    render_template "${SERVER_TMPL}"
} > "${YAML_FILE}"

# ── Rotate ────────────────────────────────────────────────────────────────────
echo "→ Stopping pod ${LLM_POD}..."
podman pod stop "${LLM_POD}"

echo "→ Removing pod ${LLM_POD}..."
podman pod rm "${LLM_POD}"

echo "→ Removing old secret ${SECRET_NAME}..."
podman secret rm "${SECRET_NAME}"

echo "→ Replaying pod with new key..."
podman kube play "${YAML_FILE}"

# ── Verify ────────────────────────────────────────────────────────────────────
echo "→ Verifying new key..."
sleep 3
ACTIVE_KEY=$(podman exec "${CONTAINER_NAME}" cat /etc/secret/watsonx-secret/apiKey 2>/dev/null || echo "")
EXPECTED_PREFIX="${WATSONX_APIKEY_NEW:0:8}"
ACTIVE_PREFIX="${ACTIVE_KEY:0:8}"
if [[ "${ACTIVE_PREFIX}" == "${EXPECTED_PREFIX}" ]]; then
    echo "✓ Key rotated successfully (mounted key starts with: ${ACTIVE_PREFIX}...)."
else
    echo "⚠ Warning: mounted key does not match expected value. Check container logs." >&2
fi

echo "✓ Done. Test the application via the Q&A interface to confirm end-to-end functionality."
