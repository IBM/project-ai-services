# must-gather

`ai-services must-gather` collects debugging information from an AI Services
deployment and writes it to a timestamped directory for support and
troubleshooting.

---

## Usage

```
ai-services must-gather --runtime <podman|openshift> [flags]
```

| Flag | Short | Default | Description |
|---|---|---|---|
| `--runtime` | — | *(required)* | Runtime to target: `podman` or `openshift`. |
| `--output-dir` | `-o` | `.` | Parent directory; a `must-gather.local.<timestamp>` sub-directory is created inside. |
| `--application` | `-a` | *(all)* | Limit application pod collection to this application name. |

### Examples

```bash
# --- Podman ---

# Collect everything from all applications
ai-services must-gather --runtime podman

# Collect only pods belonging to the "rag" application
ai-services must-gather --runtime podman --application rag

# Write output to a specific directory
ai-services must-gather --runtime podman --output-dir /tmp/debug

# --- OpenShift ---

# Collect everything from all applications
ai-services must-gather --runtime openshift

# Collect only pods belonging to the "rag" application
ai-services must-gather --runtime openshift --application rag

# Write output to a specific directory
ai-services must-gather --runtime openshift --output-dir /tmp/debug
```

---

## Requirements

The following conditions must be met before running must-gather. Failing them
does not abort the command — missing pieces are logged as warnings and
collection continues with whatever is available. However, the output will be
incomplete.

### Podman

#### 1. Podman must be running and accessible

The CLI invokes `podman` directly. If the Podman socket is not reachable
(e.g. `podman machine` is stopped on macOS), must-gather **fails immediately**
— this is the only hard failure.

#### 2. Catalog must be installed for application-level data

The catalog infrastructure pods (backend, database, Caddy) carry the label
`ai-services.io/application=ai-services`. If none of those pods are found,
the following sections are **skipped entirely**:

- Application pods (inspect, logs)
- Catalog pods (inspect, logs)
- Caddyfile / caddy-autosave.json
- Catalog credentials file
- Models directory listing

System info, network, volumes, and secrets are **always collected** regardless
of catalog state.

#### 3. You must be logged in to the catalog for application data

Application pod discovery goes through the catalog REST API
(`GET /api/v1/applications`). The API requires a valid session token stored in
`~/.config/ai-services/catalog-credentials.json`.

If you are not logged in (`ai-services catalog login`), the catalog client
will fail to authenticate and application pod collection will be skipped with a
warning — all other data is still gathered.

#### 4. The catalog backend pod must be running for base-directory resolution

The base directory (`AI_SERVICES_BASE_DIR`) is read from the running catalog
backend container's environment. If the backend pod is stopped, must-gather
falls back to the default base directory. This affects:

- Caddyfile path
- Caddy autosave.json path
- Models directory path

### OpenShift

#### 1. A valid kubeconfig or in-cluster config must be present

The CLI resolves cluster credentials in this order:

1. In-cluster service account token (when running inside a pod)
2. `KUBECONFIG` environment variable
3. `~/.kube/config`

If no valid config is found, or the cluster is unreachable, must-gather
**fails immediately** — this is the only hard failure.

#### 2. Sufficient RBAC permissions are required

The authenticated user or service account must have at minimum:

| Resource | Verbs |
|---|---|
| `pods` | `get`, `list` |
| `pods/log` | `get` |
| `events` | `list` |
| `secrets` | `list` |
| `services` | `list` |
| `persistentvolumeclaims` | `list` |
| `nodes` | `list` |
| `routes.route.openshift.io` | `list` |

Missing permissions cause per-step warnings — collection continues for all
other sections.

#### 3. Catalog must be installed for application-level data

The catalog backend Deployment and its pods carry the label
`ai-services.io/application=ai-services`. If no pods matching that label are
found in the catalog namespace (`ai-services`), the following sections are
**skipped entirely**:

- Application pods (inspect, logs)
- Catalog pods (inspect, logs)
- Catalog credentials file

System info, secrets, and volumes are **always collected** regardless of
catalog state.

#### 4. You must be logged in to the catalog for application data

Application pod discovery goes through the catalog REST API
(`GET /api/v1/applications`). The API requires a valid session token stored in
`~/.config/ai-services/catalog-credentials.json`.
If you are not logged in (`ai-services catalog login`), the catalog client
will fail to authenticate and application pod collection will be skipped with a
warning — all other data is still gathered.

---

## Output structure

### Podman

```
must-gather.local.<timestamp>/
├── catalog/
│   ├── pods/
│   │   ├── ai-services--catalog/
│   │   │   ├── inspect.json          # pod inspect (sanitized)
│   │   │   ├── inspect/
│   │   │   │   └── <container>.json  # container inspect (sanitized)
│   │   │   └── logs/
│   │   │       └── <container>.log   # last 1000 lines (sanitized)
│   │   ├── ai-services--db/
│   │   └── ai-services--caddy/
│   ├── Caddyfile                     # reverse-proxy config (sanitized)
│   ├── caddy-autosave.json           # Caddy live config snapshot (sanitized)
│   └── catalog-credentials.json     # tokens redacted
├── pods/
│   └── <app-pod-name>/
│       ├── inspect.json
│       ├── inspect/
│       │   └── <container>.json
│       └── logs/
│           └── <container>.log
├── models/
│   └── models.txt                    # org/model name + size + file count
├── secrets/
│   └── secrets.json                  # secret metadata only, values never exposed
├── system/
│   ├── version.txt                   # podman version
│   ├── info.json                     # podman info (sanitized)
│   └── system-df.txt                 # podman system df
├── network/
│   └── networks.json                 # all Podman networks (sanitized)
└── volumes/
    └── volumes.json                  # all Podman volumes (sanitized)
```

### OpenShift

```
must-gather.local.<timestamp>/
├── catalog/
│   ├── pods/
│   │   └── <pod-name>/
│   │       ├── inspect.json                    # full corev1.Pod object (sanitized)
│   │       ├── inspect/
│   │       │   └── <container>.json            # container spec + status (sanitized)
│   │       └── logs/
│   │           └── <container>.log             # last 1000 lines (sanitized)
│   └── catalog-credentials.json               # tokens redacted
├── pods/
│   └── <app-pod-name>/
│       ├── inspect.json
│       ├── inspect/
│       │   └── <container>.json
│       └── logs/
│           └── <container>.log
├── events/
│   └── events.json                             # K8s Events per app namespace (sanitized)
├── inference-services/
│   └── <app-namespace>-inference-services.json # KServe InferenceService CRs (sanitized)
├── secrets/
│   ├── catalog-secrets.json                   # K8s Secrets from ai-services ns (metadata only)
│   └── <app-namespace>-secrets.json           # K8s Secrets from each app ns (metadata only)
├── system/
│   ├── version.json                            # cluster server version
│   └── nodes.json                              # node list (sanitized)
├── network/
│   ├── services.json                           # K8s Services per app namespace (sanitized)
│   └── routes.json                             # OpenShift Routes per app namespace (sanitized)
└── volumes/
    ├── catalog-pvcs.json                       # PVCs from ai-services ns (sanitized)
    └── <app-namespace>-pvcs.json              # PVCs from each app ns (sanitized)
```

---

## Collection flow

### Podman

```
must-gather --runtime podman
  │
  ├─► Connect to Podman             [HARD FAIL if unreachable]
  │
  ├─► Create output directory
  │
  ├─► Check catalog installed?      (label: ai-services.io/application=ai-services)
  │     │
  │     ├─ YES ─► Resolve base directory from catalog backend pod env
  │     │           (falls back to default if pod is stopped)
  │     │
  │     ├─ YES ─► Collect catalog artifacts
  │     │           ├─ Catalog pods (backend + db + caddy): inspect + logs
  │     │           ├─ Caddyfile
  │     │           ├─ caddy-autosave.json
  │     │           └─ catalog-credentials.json (tokens redacted)
  │     │
  │     ├─ YES ─► Collect application pods          [requires catalog login]
  │     │           ├─ List apps via catalog API
  │     │           │   ├─ --application given → warn if not found, skip
  │     │           │   └─ no --application    → collect all apps
  │     │           └─ Per app: pod inspect + container inspect + logs
  │     │
  │     ├─ YES ─► Collect models info
  │     │           └─ Directory listing + sizes from <BaseDir>/models/
  │     │
  │     └─ NO  ─► Skip all catalog-dependent steps (warning logged)
  │
  ├─► Collect system info           (always)
  │     ├─ podman version
  │     ├─ podman info
  │     └─ podman system df
  │
  ├─► Collect network info          (always)
  │     └─ podman network ls
  │
  ├─► Collect secret metadata       (always)
  │     └─ podman secret ls (metadata only, values inaccessible by design)
  │
  └─► Collect volume info           (always)
        └─ podman volume ls
```

### OpenShift

```
must-gather --runtime openshift
  │
  ├─► Connect to cluster (kubeconfig / in-cluster)   [HARD FAIL if unreachable]
  │
  ├─► Create output directory
  │
  ├─► Check catalog installed?   (label: ai-services.io/application=ai-services
  │                                in catalog namespace "ai-services")
  │     │
  │     ├─ YES ─► Collect catalog pods (inspect + container inspect + logs)
  │     │
  │     ├─ YES ─► Collect catalog-credentials.json (tokens redacted)
  │     │
  │     ├─ YES ─► Collect application pods          [requires catalog login]
  │     │           ├─ List apps via catalog API
  │     │           │   ├─ --application given → warn if not found, skip
  │     │           │   └─ no --application    → collect all apps
  │     │           ├─ Derive app namespace from UUID (ai-services-<8 chars>)
  │     │           └─ Per app: pod inspect + container inspect + logs
  │     │
  │     └─ NO  ─► Skip catalog-dependent steps (warning logged)
  │
  ├─► Collect system info           (always, from catalog namespace)
  │     ├─ cluster server version
  │     └─ node list
  │
  ├─► Collect catalog namespace secrets/PVCs   (always)
  │     ├─ secrets/catalog-secrets.json
  │     └─ volumes/catalog-pvcs.json
  │
  └─► Per app namespace (derived from catalog API)   (always)
        ├─ Collect events → events/events.json
        ├─ Collect secrets → secrets/<app-namespace>-secrets.json
        ├─ Collect network → network/services.json + routes.json
        ├─ Collect PVCs → volumes/<app-namespace>-pvcs.json
        └─ Collect InferenceServices → inference-services/<app-namespace>-inference-services.json
```

---

## Sensitive data handling

All collected files are sanitised before being written. The sanitizer redacts
values for any key matching these patterns (case-insensitive):

| Pattern | Examples caught |
|---|---|
| `*passw(or)?d*` | `password`, `passwd`, `db_password` |
| `*secret*` | `secret`, `client_secret` |
| `*token*` | `access_token`, `refresh_token`, `authToken` |
| `*api?key*` | `api_key`, `apiKey`, `api-key` |
| `*access?key*` | `access_key`, `accessKey` |
| `*private?key*` | `private_key`, `privateKey` |
| `*credential*` | `credentials`, `db_credentials` |
| `*auth*` | `Authorization`, `x-auth-token`, `oauth_auth` |
| `*cert*` | `certificate`, `tls_cert` |

Redacted values are replaced with `[REDACTED]`. Sanitization applies to:
- JSON files: recursive key-based replacement
- Plain-text / log files: line-by-line `KEY=VALUE` pattern replacement

> **Note (Podman):** Podman secret *values* are never accessible via the CLI after
> creation — `collectSecretInfo` only records metadata (name, ID, driver,
> creation time), so that step is safe by design.

> **Note (OpenShift):** Kubernetes Secret `data` and `stringData` fields are
> stripped at the struct level before the file is written — the sanitizer is a
> second line of defence, not the primary guard. Only metadata (name, type,
> labels, annotations, creation timestamp) is included in the secrets files.

---

## Corner cases

### Podman

| Situation | Behaviour |
|---|---|
| Catalog not installed (no catalog pods) | Catalog artifacts, application pods, and models are all skipped with a single warning. System/network/secrets/volumes are still collected. |
| Not logged in to catalog | Catalog client creation fails; application pod collection is skipped with a warning. Catalog pods and other sections are still collected. |
| Catalog backend pod stopped | Base directory falls back to default. Caddyfile and models paths may be wrong if a custom base dir was used at install time. |
| `--application` given, app not found | Warning is printed; gather continues and collects everything else. |
| `--application` given, catalog API error | Warning is printed with the HTTP error; other sections still run. |
| Output directory not writable | Hard fail — `createOutputDir` returns an error and the command exits non-zero. |
| Individual file write failure | Warning only; collection of remaining files continues. |
| Infra/pause containers | Automatically skipped — any container whose name ends in `-infra` is excluded from log and inspect collection. |
| Models directory missing | Warning logged; rest of collection continues. |
| Caddy config files missing | Per-file warning; collection continues. |

### OpenShift

| Situation | Behaviour |
|---|---|
| Cluster unreachable / no kubeconfig | Hard fail — command exits non-zero immediately. |
| Insufficient RBAC (e.g. cannot list pods) | Per-step warning; all other sections continue. |
| Catalog not installed (no catalog pods in `ai-services` namespace) | Catalog artifacts and application pods are skipped with a single warning. System/secrets/volumes are still collected from the catalog namespace. |
| Not logged in to catalog | Application pod collection is skipped with a warning. Catalog pods and other sections are still collected. |
| `--application` given, app not found | Warning is printed; gather continues and collects everything else. |
| No applications found in catalog | Application pods skipped with a warning; all other collection continues. |
| Output directory not writable | Hard fail — `createOutputDir` returns an error and the command exits non-zero. |
| Individual file write failure | Warning only; collection of remaining files continues. |

---

## Known limitations / pending work

The following items are known gaps in the current implementation:

1. **Hardcoded log line limit.** Container logs are capped at the last **1000 lines** (`--tail 1000`). There is no flag to change this. Long-running containers or those that crashed after many restarts may have the relevant error cut off.

2. **No permission / privilege checks upfront.** The command does not verify upfront whether the current user has permission to run `podman` (Podman) or the necessary RBAC rights (OpenShift). Failures surface as per-step warnings rather than an early, actionable error.

3. **Application list pagination not exhausted.** `GetAllApps` and `GetAppByName` call `ListApplications` with no pagination parameters, returning only the first 20 applications (server default). Deployments with more than 20 applications will have the remainder silently missed.

4. **No archive / compression.** Output is written as a plain directory tree. There is no built-in option to produce a `.tar.gz` for easy sharing with support.

5. **No redaction of plain-text log content.** The sanitizer covers `KEY=VALUE` lines in logs, but secrets that appear mid-sentence (e.g. in a stack trace or error message) are not redacted.

6. **Catalog credentials file inclusion.** The credentials file is included in the output (tokens redacted). If the sanitizer regex misses a new field name introduced by a future API change, a live token could be included in the bundle.

7. **No timeout on individual API / CLI invocations.** A hung `podman logs` / `podman inspect` call (Podman) or a stalled `GetLogs` stream (OpenShift) will block the entire gather indefinitely.
