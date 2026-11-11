# Local Development with Tilt

The root [`Tiltfile`](../../Tiltfile) runs the AI Services Catalog (PostgreSQL, backend API, a local worker, UI) locally with live reload.

> **Local development only.** Every credential and key listed below is a publicly documented default. Never use them on a shared or real environment.

To run the RAG chatbot on macOS (independent of the catalog), see [RAG on macOS with Tilt](tilt-rag-local-development.md).

## Prerequisites

- [Tilt](https://docs.tilt.dev/install.html), Go, Node.js/npm, `curl`, `jq`
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
| `enable_worker` | `true` (set `false` to skip the local worker) |
| `worker_name` | `mac-worker` |
| `mock_workers` | _(empty)_ comma-separated runtime types of mocked workers to onboard: `podman`, `openshift` or `podman,openshift` |
| `admin_password` | `admin123` (plaintext, only needed with a custom `admin_password_hash`) |

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

## Local worker (`catalog-worker`)

The `catalog-worker` resource runs `ai-services worker grpcstream` **natively on the Mac** (not as a pod). It reaches Podman through the VM's SSH socket (`CONTAINER_HOST` / `CONTAINER_SSHKEY`), so workloads it deploys land in your Podman machine. On each (re)start it:

1. logs in to the catalog API as the admin user,
2. deletes any stale worker with the same name (a `ready` worker cannot be re-registered),
3. requests a fresh single-use bootstrap token (`POST /api/v1/workers`),
4. joins the gateway (`catalog-worker-gateway.localhost:9191`, which resolves to `127.0.0.1` on macOS).

If the stream ends with `Unauthenticated`, the loop re-registers automatically. If the backend is merely restarted, the worker reconnects with its stored mTLS credentials.

| Variable | Purpose |
|----------|---------|
| `WORKER_TLS_DIR` | `.tilt/worker-tls` (gitignored), because `/data` is read-only on macOS. |
| `AI_SERVICES_BASE_DIR` | `.tilt/worker-data` (gitignored). |
| `CADDY_ADMIN_URL` | Dummy value; no Caddy runs locally. |

Verify with `ai-services catalog worker list` or `GET /api/v1/workers`.

**Limitations (dev only):**

- No Caddy proxy runs locally, so `PROXY_ROUTE` commands fail.
- Spyre/vfio devices do not exist in the Podman VM, so Spyre discovery logs `lspci: executable file not found` (harmless).
- Service images built for `ppc64le` will not run in an arm64 VM.

## Mocked workers (`--mock_workers`)

To exercise the catalog's worker handling without a Podman VM, an OpenShift cluster, Spyre cards, Caddy or Helm, onboard mocked workers:

```bash
tilt up -- --mock_workers=podman,openshift
```

This adds a `catalog-worker-mock-podman` and/or `catalog-worker-mock-openshift` resource. They use the same register-and-join loop as `catalog-worker`, under the names `mock-podman` / `mock-openshift`, and appear in `GET /api/v1/workers` as `ready` with the matching `runtime_type`. The flag can be combined with `--enable_worker=false` to run only mocks.

The CLI is run with `AI_SERVICES_MOCK_RUNTIME=true`, which swaps in an in-memory runtime (`internal/pkg/runtime/mock`). Nothing is executed; state lives in the worker process and is lost on restart:

| Area | Mock behaviour |
|------|----------------|
| Pods | `kube play` YAML is parsed. Each `Pod` becomes a `Running` pod with an infra container plus its spec containers, healthy if it has a liveness probe. `Secret` documents are recorded as secrets. `start=off` creates the pod as `Exited`. |
| System info | Fixed 16 CPUs, 64 GiB memory and 8 Spyre cards, all free. |
| Helm (`openshift`) | Install and uninstall are recorded in memory. Each release with a template ID gets one healthy placeholder pod labelled `ai-services.io/template`, so the catalog sync finds it. `WAIT_INFERENCE_SERVICE` returns immediately. |
| Caddy (`podman`) | `PROXY_ROUTE` is served by an in-memory route table. |
| Models, HTTP tunnel | `DOWNLOAD_MODEL` is a no-op and `HTTP_PROXY` returns an empty `200`. |
| Routes, CRDs, PVCs, namespaces | OpenShift mock returns empty results. The podman mock returns `unsupported method`, as the real Podman client does. |

Because nothing runs, application services deployed to a mock worker show as running but serve no traffic. The mock is only enabled by the environment variable, so production binaries behave as before.

## Troubleshooting

| Symptom | Cause / fix |
|---------|-------------|
| `cluster is not accessible ... connection refused` | Backend started with `--runtime=openshift` and a stale kubeconfig. Use the default `backend_runtime=podman`. |
| `podman runtime is only supported on linux/ppc64le` | `AI_SERVICES_SKIP_PLATFORM_CHECK` is not set. Use the Tiltfile, which sets it. |
| `mkdir /data/...: read-only file system` | A path is hard-coded under `/data`. Override it by env var, as done for `GATEWAY_PKI_DIR`. |
| `/health` readiness probe failing | The backend exited. Check the `catalog-backend` log in the Tilt UI. |
| `catalog-db` not ready | Check that the Podman machine is running (`podman machine list`). |
| `catalog-worker`: `dial unix /run/user/<uid>/podman/podman.sock` | `CONTAINER_HOST` was not resolved. Run `podman system connection list` and make sure a default connection exists. |
| `catalog-worker`: `login failed` / `ERROR: custom --admin_password_hash` | The worker needs the plaintext password: `tilt up -- --admin_password_hash=<hash> --admin_password=<plaintext>`. |
| `catalog-worker`: `worker ... is already registered and ready` | Another process holds the name. Change it with `--worker_name=<name>` or run `ai-services catalog worker deregister <name>`. |
