# Local Development with Tilt

The root [`Tiltfile`](../../Tiltfile) runs the AI Services Catalog (PostgreSQL, backend API, UI) locally with live reload.

> **Local development only.** Every credential and key listed below is a publicly documented default. Never use them on a shared or real environment.

To run the RAG chatbot on macOS (independent of the catalog), see [RAG on macOS with Tilt](tilt-rag-local-development.md).

## Prerequisites

- [Tilt](https://docs.tilt.dev/install.html), Go, Node.js/npm
- Podman, with a running machine on macOS (`podman machine start`)

## Quick start

```bash
tilt up
```

Open the Tilt UI at http://localhost:10350, then:

| Service | URL |
|---------|-----|
| Catalog UI | http://localhost:5173 |
| Backend health | http://localhost:8080/health |
| Backend version | http://localhost:8080/api/v1/version |

## Default credentials

### Catalog login

| Field | Value |
|-------|-------|
| Username | `admin` |
| Password | `admin123` |

The Tiltfile passes only the PBKDF2 hash of the password (`--admin-password-hash`), not the plaintext. The default hash is `DEFAULT_ADMIN_HASH` in the [`Tiltfile`](../../Tiltfile).

Log in through the UI, or through the API:

```bash
curl -s -X POST http://localhost:8080/api/v1/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"admin123"}'
```

### PostgreSQL

| Field | Value |
|-------|-------|
| Host / port | `localhost:5432` |
| User | `postgres` |
| Password | `postgres` |
| Database | `ai_services` |

```bash
psql "postgresql://postgres:postgres@localhost:5432/ai_services"
```

### Encryption keys

| Setting | Default |
|---------|---------|
| `DB_ENCRYPTION_KEY` | `01234567890123456789012345678901` |
| `MTLS_ENCRYPTION_KEY` | `01234567890123456789012345678901` |

## Overriding defaults

Pass any option after `--`, for example `tilt up -- --backend_port=9090 --db_password=secret`.

| Option | Default |
|--------|---------|
| `db_user` / `db_password` / `db_name` | `postgres` / `postgres` / `ai_services` |
| `db_image` | `postgres:16-alpine` |
| `db_port` | `5432` |
| `backend_port` | `8080` |
| `worker_gateway_port` | `9191` |
| `frontend_port` | `5173` |
| `admin_username` | `admin` |
| `admin_password_hash` | hash of `admin123` |
| `db_encryption_key` / `mtls_encryption_key` | `0123456789...` (32 chars) |
| `backend_runtime` | `podman` |
| `domain_suffix` | `localhost` |

### Setting a different admin password

Generate a PBKDF2 hash (format `iterations.salt.hash`) and pass it:

```bash
python3 - <<'EOF'
import hashlib, base64, os, getpass
pw = getpass.getpass("New admin password: ").encode()
salt = os.urandom(16)
h = hashlib.pbkdf2_hmac("sha256", pw, salt, 100000, 32)
b = lambda x: base64.b64encode(x).decode().rstrip("=")
print(f"100000.{b(salt)}.{b(h)}")
EOF

tilt up -- --admin_password_hash='<hash>'
```

## What the Tiltfile sets up for macOS

The catalog's podman runtime targets `linux/ppc64le`. For local development the Tiltfile exports these variables for the backend:

| Variable | Purpose |
|----------|---------|
| `AI_SERVICES_SKIP_PLATFORM_CHECK=true` | Bypasses the `linux/ppc64le` platform guard. |
| `CONTAINER_HOST`, `CONTAINER_SSHKEY` | Taken from your default `podman system connection`, so the backend reaches the Podman VM. Skipped if `CONTAINER_HOST` is already set. |
| `DOMAIN_SUFFIX` | Used for the gateway server certificate SAN. |
| `GATEWAY_PKI_DIR` | `.tilt/gateway-pki` (gitignored), because `/data` is read-only on macOS. |

## Troubleshooting

| Symptom | Cause / fix |
|---------|-------------|
| `cluster is not accessible ... connection refused` | Backend started with `--runtime=openshift` and a stale kubeconfig. Use the default `backend_runtime=podman`. |
| `podman runtime is only supported on linux/ppc64le` | `AI_SERVICES_SKIP_PLATFORM_CHECK` is not set. Use the Tiltfile, which sets it. |
| `mkdir /data/...: read-only file system` | A path is hard-coded under `/data`. Override it by env var, as done for `GATEWAY_PKI_DIR`. |
| `/health` readiness probe failing | The backend exited. Check the `catalog-backend` log in the Tilt UI. |
| `catalog-db` not ready | Check that the Podman machine is running (`podman machine list`). |
