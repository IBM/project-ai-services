# must-gather

`ai-services must-gather` collects debugging information from an AI Services
Podman deployment and writes it to a timestamped directory for support and
troubleshooting.

---

## Usage

```
ai-services must-gather --runtime podman [flags]
```

| Flag | Short | Default | Description |
|---|---|---|---|
| `--runtime` | — | *(required)* | Runtime to target. Only `podman` is supported today. |
| `--output-dir` | `-o` | `.` | Parent directory; a `must-gather.local.<timestamp>` sub-directory is created inside. |
| `--application` | `-a` | *(all)* | Limit application pod collection to this application name. |

### Examples

```bash
# Collect everything from all applications
ai-services must-gather --runtime podman

# Collect only pods belonging to the "rag" application
ai-services must-gather --runtime podman --application rag

# Write output to a specific directory
ai-services must-gather --runtime podman --output-dir /tmp/debug
```

---

## Requirements

The following conditions must be met before running must-gather. Failing them
does not abort the command — missing pieces are logged as warnings and
collection continues with whatever is available. However, the output will be
incomplete.

### 1. Podman must be running and accessible

The CLI invokes `podman` directly. If the Podman socket is not reachable
(e.g. `podman machine` is stopped on macOS), must-gather **fails immediately**
— this is the only hard failure.

### 2. Catalog must be installed for application-level data

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

### 3. You must be logged in to the catalog for application data

Application pod discovery goes through the catalog REST API
(`GET /api/v1/applications`). The API requires a valid session token stored in
`~/.config/ai-services/catalog-credentials.json`.

If you are not logged in (`ai-services catalog login`), the catalog client
will fail to authenticate and application pod collection will be skipped with a
warning — all other data is still gathered.

### 4. The catalog backend pod must be running for base-directory resolution

The base directory (`AI_SERVICES_BASE_DIR`) is read from the running catalog
backend container's environment. If the backend pod is stopped, must-gather
falls back to the default base directory. This affects:

- Caddyfile path
- Caddy autosave.json path
- Models directory path

---

## Output structure

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

---

## Collection flow

```
must-gather
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
  │     └─ podman secret inspect (metadata only, values inaccessible by design)
  │
  └─► Collect volume info           (always)
        └─ podman volume ls
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

> **Note:** Podman secret *values* are never accessible via the CLI after
> creation — `collectSecretInfo` only records metadata (name, ID, driver,
> creation time), so that step is safe by design.

---

## Corner cases

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

---

## Known limitations / pending work

The following items are known gaps in the current implementation:

1. **OpenShift runtime not supported.** The command accepts `--runtime openshift` but immediately returns an error. OpenShift collection (oc adm must-gather integration, namespace scoping, RBAC) is not yet implemented.

2. **Hardcoded log line limit.** Container logs are capped at the last **1000 lines** (`--tail 1000`). There is no flag to change this. Long-running containers or those that crashed after many restarts may have the relevant error cut off.

3. **No permission / privilege checks.** The command does not verify upfront whether the current OS user has permission to run `podman` or read the base directory. Failures surface as per-step warnings rather than an early, actionable error.

4. **Application list pagination not exhausted.** `GetAllApps` and `GetAppByName` call `ListApplications` with no pagination parameters, returning only the first 20 applications (server default). Deployments with more than 20 applications will have the remainder silently missed.

5. **No archive / compression.** Output is written as a plain directory tree. There is no built-in option to produce a `.tar.gz` for easy sharing with support.

6. **No redaction of plain-text log content.** The sanitizer covers `KEY=VALUE` lines in logs, but secrets that appear mid-sentence (e.g. in a stack trace or error message) are not redacted.

7. **Catalog credentials file inclusion.** The credentials file is included in the output (tokens redacted). If the sanitizer regex misses a new field name introduced by a future API change, a live token could be included in the bundle.

8. **No timeout on individual `podman` invocations.** A hung `podman logs` or `podman inspect` call will block the entire gather indefinitely.
