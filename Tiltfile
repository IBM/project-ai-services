# -*- mode: Python -*-

# =============================================================================
# Tiltfile for AI Services Catalog (Backend & Frontend) - local development
# =============================================================================
#
# Podman-based fast developer workflow with live code monitoring & hot-reloading:
#   - Runs PostgreSQL in a Podman container.
#   - Watches & live-reloads the Go Catalog Backend (recompiles on change).
#   - Watches & live-reloads the React/Vite Catalog UI (instant HMR).
#
# Usage:
#   tilt up                              # Start the local development environment
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

ui_port = int(cfg.get("frontend_port", 5173))

# The plaintext password is only known for the default hash.
admin_pass_display = DEFAULT_ADMIN_PASSWORD if admin_hash == DEFAULT_ADMIN_HASH else "<custom password>"

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
        _conn=$(podman system connection list --format '{{{{if .Default}}}}{{{{.URI}}}}|{{{{.Identity}}}}{{{{end}}}}' 2>/dev/null | head -n1)
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
# 3. Catalog UI (Vite Dev Server with Instant Hot Module Reloading)
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
