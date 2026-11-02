# -*- mode: Python -*-

# =============================================================================
# Tiltfile for AI Services Catalog (Backend & Frontend) - local development
# =============================================================================
#
# Podman-based fast developer workflow with live code monitoring & hot-reloading:
#   - Runs PostgreSQL in a Podman container.
#   - Watches & live-reloads the Go Catalog Backend (recompiles on change).
#   - Watches & live-reloads the React/Vite Catalog UI (instant HMR).
#   - Runs a worker natively on the Mac that talks to the Podman VM and joins the catalog.
#
# Usage:
#   tilt up                              # Start the local development environment
#   tilt up -- --enable_worker=false     # Skip the local Mac worker
#   tilt up -- --mock_workers=podman,openshift   # Also onboard mocked podman/openshift workers
#   tilt up -- --backend_port=9090       # Custom ports or parameters
#   tilt down                            # Stop and remove local state (.tilt/, catalog-db)
#
# See docs/dev-guides/tilt-local-development.md
# =============================================================================

# -----------------------------------------------------------------------------
# Configuration Flags
# -----------------------------------------------------------------------------
# Database configuration
config.define_string("db_user", args=False)
config.define_string("db_password", args=False)
config.define_string("db_name", args=False)
config.define_string("db_image", args=False)
config.define_string("db_port", args=False)

# Backend configuration
config.define_string("backend_port", args=False)
config.define_string("worker_gateway_port", args=False)
config.define_string("admin_username", args=False)
config.define_string("admin_password_hash", args=False)
config.define_string("db_encryption_key", args=False)
config.define_string("mtls_encryption_key", args=False)
config.define_string("backend_runtime", args=False)
config.define_string("domain_suffix", args=False)

# Local worker configuration
config.define_string("enable_worker", args=False)
config.define_string("worker_name", args=False)
config.define_string("mock_workers", args=False)
config.define_string("admin_password", args=False)

# Frontend configuration
config.define_string("frontend_port", args=False)

cfg = config.parse()

# -----------------------------------------------------------------------------
# Cleanup on `tilt down`
# -----------------------------------------------------------------------------
# `tilt down` only deletes Kubernetes objects; it does not clean up files created
# by local_resource processes. Tilt evaluates this Tiltfile for `tilt down` too,
# so remove the local dev state (gateway PKI certs/keys etc.) and the DB container here.
if config.tilt_subcommand == "down":
    local("rm -rf .tilt; podman rm -f catalog-db >/dev/null 2>&1 || true", quiet=True)

# -----------------------------------------------------------------------------
# Settings (local development defaults only - never use on a shared environment)
# -----------------------------------------------------------------------------
# Default admin password is 'admin123'; this is its PBKDF2 hash.
DEFAULT_ADMIN_PASSWORD = "admin123"
DEFAULT_ADMIN_HASH = "100000.MTIzNDU2Nzg5MDEyMzQ1Ng.QgDPIpRaiS2jwOOXZMBOiphQnQKHf++99TMpYzDn3ak"

db_port = int(cfg.get("db_port", 5432))
db_user = str(cfg.get("db_user", "postgres"))
db_pass = str(cfg.get("db_password", "postgres"))
db_name = str(cfg.get("db_name", "ai_services"))
db_img = str(cfg.get("db_image", "postgres:16-alpine"))

backend_port = int(cfg.get("backend_port", 8080))
worker_port = int(cfg.get("worker_gateway_port", 9191))
admin_user = str(cfg.get("admin_username", "admin"))
admin_hash = str(cfg.get("admin_password_hash", DEFAULT_ADMIN_HASH))
db_enc_key = str(cfg.get("db_encryption_key", "01234567890123456789012345678901"))
mtls_enc_key = str(cfg.get("mtls_encryption_key", "01234567890123456789012345678901"))
runtime_flag = str(cfg.get("backend_runtime", "podman"))
domain_suffix = str(cfg.get("domain_suffix", "localhost"))

enable_worker = str(cfg.get("enable_worker", "true")) == "true"
worker_name = str(cfg.get("worker_name", "mac-worker"))

# Comma-separated runtime types for mocked workers, e.g. "podman,openshift".
mock_runtimes = [r.strip() for r in str(cfg.get("mock_workers", "")).split(",") if r.strip()]
for _r in mock_runtimes:
    if _r not in ["podman", "openshift"]:
        fail("--mock_workers: unsupported runtime '{}' (use podman and/or openshift)".format(_r))

ui_port = int(cfg.get("frontend_port", 5173))

# The plaintext password is only known for the default hash.
admin_pass_display = DEFAULT_ADMIN_PASSWORD if admin_hash == DEFAULT_ADMIN_HASH else "<custom password>"

# The worker resource logs in to the catalog API to request its bootstrap token, so it needs the
# plaintext password. Pass --admin_password when a custom --admin_password_hash is used.
worker_admin_pass = str(cfg.get("admin_password", DEFAULT_ADMIN_PASSWORD if admin_hash == DEFAULT_ADMIN_HASH else ""))

# -----------------------------------------------------------------------------
# Quick links (shown in the header of the "quick-links" resource in the Tilt UI)
# -----------------------------------------------------------------------------
local_resource(
    name="quick-links",
    cmd="""
        echo "Catalog UI : http://localhost:{ui}"
        echo "API Swagger: http://localhost:{api}/swagger/index.html"
        echo "API Health : http://localhost:{api}/health"
        echo "Login      : {user} / {password}"
        echo "(local development credentials only)"
    """.format(ui=ui_port, api=backend_port, user=admin_user, password=admin_pass_display),
    labels=["quick-links"],
    links=[
        link("http://localhost:{}".format(ui_port), "Catalog UI (login: {} / {})".format(admin_user, admin_pass_display)),
        link("http://localhost:{}/swagger/index.html".format(backend_port), "API Swagger"),
        link("http://localhost:{}/health".format(backend_port), "API Health"),
        link("http://localhost:{}/api/v1/version".format(backend_port), "API Version"),
    ],
)

# -------------------------------------------------------------------------
# 1. PostgreSQL Database Service via Podman
# -------------------------------------------------------------------------
# Starts PostgreSQL in a managed Podman container
local_resource(
    name="catalog-db",
    serve_cmd="""
        podman stop catalog-db 2>/dev/null || true
        podman rm -f catalog-db 2>/dev/null || true
        podman run --rm --name catalog-db \
            -p {port}:5432 \
            -e POSTGRES_USER={user} \
            -e POSTGRES_PASSWORD={password} \
            -e POSTGRES_DB={dbname} \
            {image}
    """.format(
        port=db_port,
        user=db_user,
        password=db_pass,
        dbname=db_name,
        image=db_img,
    ),
    labels=["catalog", "database"],
    readiness_probe=probe(
        initial_delay_secs=2,
        period_secs=3,
        exec=exec_action(["podman", "exec", "catalog-db", "pg_isready", "-U", db_user, "-d", db_name]),
    ),
)

# -------------------------------------------------------------------------
# 2. Catalog Backend (Go API Server with Live Code Watching & Migration)
# -------------------------------------------------------------------------
# Watches Go files, runs DB migration, and starts the catalog apiserver
backend_script = """
    set -e
    echo "==> Building and running AI Services Catalog Backend..."
    cd ai-services

    # Ensure database is reachable and run migrations
    echo "==> Running DB migrations..."
    go run ./cmd/ai-services catalog dbmigrate init \
        --db-host=localhost \
        --db-port={db_port} \
        --db-user={db_user} \
        --db-password={db_pass} \
        --db-name={db_name} \
        --db-sslmode=disable || true

    # Resolve the Podman API socket (needed on macOS where Podman runs in a VM)
    if [ -z "$CONTAINER_HOST" ]; then
        _conn=$(podman system connection list --format '{{{{if .Default}}}}{{{{.URI}}}}|{{{{.Identity}}}}{{{{end}}}}' 2>/dev/null | grep . | head -n1)
        if [ -n "$_conn" ]; then
            export CONTAINER_HOST="${{_conn%%|*}}"
            export CONTAINER_SSHKEY="${{_conn##*|}}"
        fi
    fi
    export DOMAIN_SUFFIX="{domain_suffix}"
    # Local dev only: allow the podman runtime on non-ppc64le hosts (e.g. macOS)
    export AI_SERVICES_SKIP_PLATFORM_CHECK="true"
    # Local dev only: /data is read-only on macOS, so keep gateway PKI in a repo-local dir
    export GATEWAY_PKI_DIR="$(pwd)/../.tilt/gateway-pki"

    # Start the Catalog API server
    echo "==> Starting Catalog APIServer on port {backend_port}..."
    export DB_HOST="localhost"
    export DB_PORT="{db_port}"
    export DB_USER="{db_user}"
    export DB_PASSWORD="{db_pass}"
    export DB_NAME="{db_name}"
    export DB_ENCRYPTION_KEY="{db_enc_key}"
    export MTLS_ENCRYPTION_KEY="{mtls_enc_key}"
    export GIN_MODE="debug"
    export AI_SERVICES_LOG_LEVEL="debug"
    export WORKER_GATEWAY_PORT="{worker_port}"
    export LOCAL_WORKER="false"

    exec go run ./cmd/ai-services catalog apiserver \
        --port={backend_port} \
        --admin-username={admin_user} \
        --admin-password-hash="{admin_hash}" \
        --runtime={runtime_flag} \
        --workergateway-port={worker_port}
""".format(
    db_port=db_port,
    db_user=db_user,
    db_pass=db_pass,
    db_name=db_name,
    db_enc_key=db_enc_key,
    mtls_enc_key=mtls_enc_key,
    backend_port=backend_port,
    worker_port=worker_port,
    admin_user=admin_user,
    admin_hash=admin_hash,
    runtime_flag=runtime_flag,
    domain_suffix=domain_suffix,
)

local_resource(
    name="catalog-backend",
    serve_cmd=backend_script,
    deps=[
        "ai-services/cmd",
        "ai-services/internal",
        "ai-services/assets",
        "ai-services/go.mod",
        "ai-services/go.sum",
    ],
    ignore=[
        "ai-services/bin",
        "ai-services/tests",
        "ai-services/docs",
    ],
    resource_deps=["catalog-db"],
    labels=["catalog", "backend"],
    links=[
        link("http://localhost:{}/health".format(backend_port), "API Health Check"),
        link("http://localhost:{}/api/v1/version".format(backend_port), "API Version"),
    ],
    readiness_probe=probe(
        initial_delay_secs=3,
        period_secs=5,
        http_get=http_get_action(port=backend_port, path="/health"),
    ),
)

# -------------------------------------------------------------------------
# 3. Local Worker (runs natively on the Mac, talks to the Podman VM)
# -------------------------------------------------------------------------
# Runs `ai-services worker grpcstream` on the host. It reaches Podman through the VM's SSH socket
# (CONTAINER_HOST / CONTAINER_SSHKEY, same as the backend) so no worker pod is deployed.
# On every (re)start it deregisters any stale worker of the same name, requests a fresh single-use
# bootstrap token from the catalog API, and joins the gateway. If the stream exits (e.g. the
# backend restarted and lost its in-memory registry) it loops and re-registers.
#
# Limitations: no Caddy proxy runs locally, so PROXY_ROUTE commands fail; Spyre/vfio devices
# are not available in the VM.
#
# worker_script() below is shared by the real worker and the mocked workers (--mock_workers).

# Environment that lets the CLI reach the Podman VM (real worker only).
PODMAN_ENV_SNIPPET = """
    # Resolve the Podman API socket (podman runs in a VM on macOS)
    if [ -z "${CONTAINER_HOST:-}" ]; then
        _conn=$(podman system connection list --format '{{if .Default}}{{.URI}}|{{.Identity}}{{end}}' 2>/dev/null | grep . | head -n1)
        if [ -n "$_conn" ]; then
            export CONTAINER_HOST="${_conn%%|*}"
            export CONTAINER_SSHKEY="${_conn##*|}"
        fi
    fi
    if [ -f "$HOME/.config/containers/auth.json" ]; then
        export REGISTRY_AUTH_FILE="$HOME/.config/containers/auth.json"
    fi
    # No local Caddy: only needed so the proxy router can be constructed.
    export CADDY_ADMIN_URL="http://localhost:2019"
"""

# Mock workers need no Podman VM, cluster, Caddy, Spyre or Helm: the CLI serves every command
# from an in-memory runtime (AI_SERVICES_MOCK_RUNTIME) and only reports the given runtime type.
MOCK_ENV_SNIPPET = """
    export AI_SERVICES_MOCK_RUNTIME="true"
"""


def worker_script(name, rt, mock, tls_dir):
    return """
    set -u
    cd ai-services
    ROOT="$(cd .. && pwd)"
    API="http://localhost:{backend_port}"
    NAME="{worker_name}"
    PASS="{worker_pass}"
    BIN="$ROOT/.tilt/bin/ai-services-{worker_name}"

    if [ -z "$PASS" ]; then
        echo "ERROR: custom --admin_password_hash in use; pass --admin_password=<plaintext> so the worker can log in."
        exit 1
    fi

    echo "==> Building ai-services CLI for worker '$NAME'..."
    mkdir -p "$ROOT/.tilt/bin"
    go build -o "$BIN" ./cmd/ai-services || exit 1
{env_snippet}
    export AI_SERVICES_SKIP_PLATFORM_CHECK="true"
    export AI_SERVICES_LOG_LEVEL="debug"
    export MTLS_ENCRYPTION_KEY="{mtls_enc_key}"
    export DOMAIN_SUFFIX="{domain_suffix}"
    export WORKER_TLS_DIR="$ROOT/.tilt/{tls_dir}"
    export AI_SERVICES_BASE_DIR="$ROOT/.tilt/{tls_dir}-data"
    export CADDY_HTTPS_PORT="443"
    mkdir -p "$AI_SERVICES_BASE_DIR/models"

    while true; do
        echo "==> Logging in to the catalog API..."
        ACCESS=$(curl -fsS -X POST "$API/api/v1/auth/login" \
            -H 'Content-Type: application/json' \
            -d "$(jq -nc --arg u '{admin_user}' --arg p "$PASS" '{{username:$u,password:$p}}')" | jq -r .access_token)
        if [ -z "$ACCESS" ] || [ "$ACCESS" = "null" ]; then
            echo "login failed, retrying in 5s..."; sleep 5; continue
        fi

        # Remove any stale registration with the same name (a 'ready' worker cannot be re-registered).
        ID=$(curl -fsS -H "Authorization: Bearer $ACCESS" "$API/api/v1/workers?page=1&page_size=100" \
            | jq -r --arg n "$NAME" '.data[] | select((.name|ascii_downcase)==($n|ascii_downcase)) | .id')
        if [ -n "$ID" ]; then
            curl -fsS -X DELETE -H "Authorization: Bearer $ACCESS" "$API/api/v1/workers/$ID" >/dev/null
        fi

        echo "==> Registering worker '$NAME'..."
        RESP=$(curl -fsS -X POST "$API/api/v1/workers" \
            -H "Authorization: Bearer $ACCESS" -H 'Content-Type: application/json' \
            -d "$(jq -nc --arg n "$NAME" '{{worker_name:$n}}')") || {{ echo "register failed, retrying in 5s..."; sleep 5; continue; }}
        TOKEN=$(echo "$RESP" | jq -r .token)
        GATEWAY=$(echo "$RESP" | jq -r .gateway_address)

        # Credentials from a previous token are useless after re-registration.
        rm -rf "$WORKER_TLS_DIR"

        echo "==> Joining gateway $GATEWAY as '$NAME' (runtime={runtime})..."
        "$BIN" worker grpcstream "$GATEWAY" --token="$TOKEN" --runtime={runtime}
        echo "worker stream exited; re-registering in 5s..."
        sleep 5
    done
""".format(
        backend_port=backend_port,
        worker_name=name,
        worker_pass=worker_admin_pass,
        admin_user=admin_user,
        mtls_enc_key=mtls_enc_key,
        domain_suffix=domain_suffix,
        runtime=rt,
        tls_dir=tls_dir,
        env_snippet=MOCK_ENV_SNIPPET if mock else PODMAN_ENV_SNIPPET,
    )


WORKER_DEPS = [
    "ai-services/cmd",
    "ai-services/internal",
    "ai-services/assets",
    "ai-services/go.mod",
    "ai-services/go.sum",
]
WORKER_IGNORE = [
    "ai-services/bin",
    "ai-services/tests",
    "ai-services/docs",
]

if enable_worker:
    local_resource(
        name="catalog-worker",
        serve_cmd=worker_script(worker_name, "podman", False, "worker-tls"),
        deps=WORKER_DEPS,
        ignore=WORKER_IGNORE,
        resource_deps=["catalog-backend"],
        labels=["catalog", "worker"],
    )

# Mocked workers: one resource per requested runtime type (podman and/or openshift).
for mock_rt in mock_runtimes:
    mock_name = "mock-" + mock_rt
    local_resource(
        name="catalog-worker-" + mock_name,
        serve_cmd=worker_script(mock_name, mock_rt, True, "worker-tls-" + mock_name),
        deps=WORKER_DEPS,
        ignore=WORKER_IGNORE,
        resource_deps=["catalog-backend"],
        labels=["catalog", "worker"],
    )


# -------------------------------------------------------------------------
# 4. Catalog UI (Vite Dev Server with Instant Hot Module Reloading)
# -------------------------------------------------------------------------
# Runs Vite in development mode with HMR and automatic proxying of /api to Backend
ui_script = """
    set -e
    echo "==> Starting Catalog UI (Vite Dev Server)..."
    cd ui/catalog
    if [ ! -d "node_modules" ]; then
        echo "Installing npm dependencies..."
        npm install
    fi
    exec npm run dev -- --host 0.0.0.0 --port {ui_port}
""".format(ui_port=ui_port)

local_resource(
    name="catalog-ui",
    serve_cmd=ui_script,
    deps=[
        "ui/catalog/src",
        "ui/catalog/public",
        "ui/catalog/index.html",
        "ui/catalog/vite.config.ts",
        "ui/catalog/package.json",
    ],
    ignore=[
        "ui/catalog/node_modules",
        "ui/catalog/dist",
        "ui/catalog/.vite",
    ],
    resource_deps=["catalog-backend"],
    labels=["catalog", "frontend"],
    links=[
        link("http://localhost:{}".format(ui_port), "Catalog UI Dashboard"),
    ],
    readiness_probe=probe(
        initial_delay_secs=3,
        period_secs=5,
        http_get=http_get_action(port=ui_port, path="/"),
    ),
)
