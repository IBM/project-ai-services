# Proposal: MCP Sidecar for AI Services

## Problem Statement

Customers integrating with AI Services must interact directly with each service's REST API, learning
endpoint paths, constructing request schemas, and handling auth independently per service. There is
no standardised way for an AI agent or ISV application to discover capabilities and call them
without detailed knowledge of the underlying HTTP layer.

All four microservices already expose a well-documented `/openapi.json` with rich descriptions
written for Swagger UI. This proposal surfaces that existing documentation as callable MCP tools
with no changes to any service.

## Proposed Solution

A single generic MCP sidecar container image, deployed once per service, configured entirely by
environment variables. At startup it fetches the service's `/openapi.json`, registers one MCP tool
per API operation, and listens for agent connections. The agent calls tools; the sidecar forwards
requests to the real service. **Most of the core logic needs no new code.** This is a targeted
adaptation of the existing `ibmcloud-api-mcp` repository, not a from-scratch build. That said, this
is a starting point, not a constraint: where our services' behavior doesn't fit what that repo
already handles (see Async Operations below), writing new logic is the right call, not something
to avoid for the sake of keeping the diff small.

**Tool selection boundary.** The sidecar never decides which tool to call. It only receives an
already-decided `tools/call` request from the connected agent and executes it. All reasoning
about *which* tool fits a user's request happens client-side, inside the agent's own LLM, using
nothing but the tool names and descriptions we expose. This is worth stating explicitly: a reader
unfamiliar with agentic tool-use will otherwise reasonably ask whether this component is making
autonomous decisions about customer data. It is not.

The sidecar is fully integrated into the existing `ai-services` CLI. Customers who already use
`ai-services bootstrap` and `ai-services application start` to set up their environment can opt
into MCP through flags they already know, no separate tooling to learn or install.

### What "plug and play" means here

The `ibmcloud-api-mcp` repository is a complete, tested Go MCP server that parses any OpenAPI spec
and generates tools dynamically. The only work is removing six IBM Cloud-specific pieces that do
not apply to this project (IBM hostname validation, IBM Cloud region logic, IAM JWT auth). The
OpenAPI loader, schema converter, tool aggregator, HTTP transport, rate limiting, CORS, and
graceful shutdown are all kept verbatim.

**Kept unchanged:**

| Component | What it does |
|---|---|
| OpenAPI loader | Fetches + parses any `/openapi.json` |
| Schema converter | OpenAPI → MCP tool JSON Schema |
| Tool aggregator | `GetTools(tags)` + `HandleToolCall` |
| Per-operation HTTP executor | Builds request, fires to `SERVICE_URL` |
| Tag-based tool filtering | Exposes only tagged operations to the agent |
| HTTP transport, rate limiting, CORS, `/health` | Runtime server infrastructure |
| `--config` flag | Emits MCP client config JSON |

**Removed (IBM Cloud-specific):**

| Component | Reason for removal |
|---|---|
| IBM hostname validation (`*.cloud.ibm.com`) | Enforces IBM Cloud URLs; irrelevant for on-prem Power services |
| IBM prefix injection on service names | Namespacing for IBM Cloud multi-service aggregation; not applicable |
| Region server extraction + region parameter | IBM Cloud multi-region routing; not applicable |
| IAM JWT token validation | IBM Cloud IAM-specific; replaced with Bearer token passthrough |
| `ibmcloud` CLI + 1Password auth strategies | IBM-specific auth mechanisms |
| IBM Cloud Go SDK dependency | Pulled in by IAM auth; not needed after auth swap |
| Stdio transport | Not suitable for containerised sidecar deployment (see Transport Mode below) |

## Architecture

### Request Flow

```
AI Agent  (any MCP-compatible client)
  │
  │  MCP tools/list  →  receives typed tool definitions + descriptions
  │  MCP tools/call  →  calls tool with structured arguments
  ▼
MCP Sidecar  (one per service, generic image, env-var config)
  │  startup: fetches OPENAPI_URL → parses spec → registers tools
  │  runtime: reconstructs HTTP request → forwards to SERVICE_URL
  ▼
Existing Service Container  (zero changes)
  digitize :4000  ·  chatbot :5000  ·  summarize :6000  ·  similarity :7000
  ▼
Shared Infrastructure  (vLLM, vector store, Postgres, zero changes)
```

### Per-Service Configuration

The same image runs for every service. Each instance is configured by two required environment
variables and an optional tag filter that hides internal endpoints (health, metrics) from the agent.

| Service | `OPENAPI_URL` | `SERVICE_URL` | Recommended `TAGS` |
|---|---|---|---|
| digitize | `http://digitize:4000/openapi.json` | `http://digitize:4000` | `ingestion,jobs` |
| chatbot | `http://chatbot:5000/openapi.json` | `http://chatbot:5000` | `chat,retrieval` |
| summarize | `http://summarize:6000/openapi.json` | `http://summarize:6000` | `summarization,jobs` |
| similarity | `http://similarity:7000/openapi.json` | `http://similarity:7000` | `similarity` |

Customers deploy sidecars only for the services they run. Two services = two sidecar containers.

### Transport Mode

The sidecar runs in **HTTP transport mode only** (`--http` flag). Stdio transport — the default in
the upstream binary — requires the MCP client to fork the process directly, which is not viable for
a containerised sidecar. HTTP transport is the only mode that makes sense here: the sidecar binds
to a port, the CLI resolves that port when generating `~/mcp.json`, and the agent connects over the
network. The stdio code path is removed entirely rather than left as an unused option.

### Tool Name Prefixing

Tool names are prefixed based on the startup configuration of each sidecar instance. Because each
sidecar is started independently with its own `OPENAPI_URL` and `SERVICE_URL`, the prefix is
derived from those properties at startup — not hardcoded per service. This means tool names are
stable and scoped to their originating service, regardless of whether an MCP host namespaces
servers automatically. This also eliminates the tool name collision risk identified under Open
Questions (for example, both `digitize` and `summarize` might independently generate a `list_jobs`
operation ID from their OpenAPI specs; prefixing produces `digitize_list_jobs` and
`summarize_list_jobs` instead).

### Async Operations — digitize & summarize

`summarize` can return a result two ways: right away, or as a "submit now, check back later" job.
`digitize` only has the check-back-later kind. OCR and chunking take real time.

**Solution:** keep it as two tools with prescriptive descriptions. The
`submit` tool description explicitly instructs the agent: *"Call `<prefix>_get_job` with the
returned `job_id` until the status field is `complete`."* This is cheaper to build, keeps the
sidecar stateless, and avoids long-held open HTTP connections. It depends on the agent following
the instruction, but a well-written description is sufficient in practice. No blocking wrapper
logic is needed; this is not a place where new code beyond what `ibmcloud-api-mcp` already handles
is required.

### Tool Schema Stability Across Restarts

Every time the sidecar restarts, it re-reads that service's API docs from scratch and rebuilds its
tools from whatever it finds *right now*. If someone changes those docs later (renames a field,
restructures a request), the tool just quietly changes shape the next time the sidecar happens to
restart. No warning, nothing to check beforehand. An ISV's agent built around the old shape could
break with no notice.

Not something we need to fully solve for v1, but at minimum, we should log when a tool's shape
changed between restarts, so it's visible instead of a silent surprise.

### Keeping the Generated Config in Sync

`application mcp config` is a manual, on-demand command today. This bites in both directions:

- **Adding a service:** `application start rag --mcp` brings the new sidecar up automatically:
  `OPENAPI_URL`/`SERVICE_URL` are resolved from the application's own known config, no manual env
  vars. But the agent host's `~/mcp.json` still only lists whatever was there before. The new
  capability is invisible to the agent until someone remembers to re-run `mcp config` and hand the
  updated file over again.
- **Removing a service:** the mirror problem. See Sidecar Lifecycle on Delete below.

**Recommendation:** have `application start --mcp` and `application mcp stop` auto-regenerate
`~/mcp.json` as part of the same command, rather than leaving regeneration as a separate manual
step. This resolves both directions with one design decision instead of two, and removes a step a
customer could otherwise easily forget.

### Sidecar Lifecycle on Delete

`application delete <name>` already performs a cascade delete: it lists every pod carrying that
application's `AppName` namespace label and force-deletes them all. It does not delete named
resources one at a time. This means the MCP sidecar does **not** need new deletion logic of its
own. As long as the sidecar container is provisioned under the same `AppName` label as the rest of
that application's pods (the Phase 3 template wiring already covers this), the existing cascade
delete sweeps it up for free.

**What needs verifying in Phase 3, not assumed:** confirm the sidecar is actually tagged with the
same `AppName` label when it's created via `--mcp`. If it isn't, `application delete` would walk
right past it, leaving an orphaned sidecar pointing at a service that no longer exists, silently
erroring on every call an agent makes to it.

### Sidecar Lifecycle on Stop/Start (temporary, not delete)

`application stop <name>` is reversible: the service isn't gone, just not running, so the
cascade-delete mechanism above doesn't apply here. This is a distinct case worth a distinct rule,
kept deliberately simple rather than adding new state to track:

- **Stopping always tears the sidecar down too, unconditionally, no flag required.** A sidecar
  left running against a stopped service produces only confusing failures for the agent
  (connection refused / timeout on every call) instead of the tool cleanly not being offered.
  There is no scenario where leaving it up is preferable, so this isn't a decision the customer
  needs to make. The config is regenerated at the same time, same as the add/delete cases above.
- **Starting only brings the sidecar back if `--mcp` is passed on that specific command.** No
  attempt to remember whether MCP was enabled before. That would require persisting and keeping
  in sync a piece of state whose only purpose is saving the customer from retyping one flag. That's
  a worse trade than it sounds: real ongoing complexity (where the state lives, what happens if it
  disagrees with reality) purchased for a trivial convenience.

**The rule in one sentence, applying uniformly to add / stop / start / delete, with no new state
anywhere:** the sidecar exists if and only if the most recently run relevant command included
`--mcp`; stopping or deleting the service always takes the sidecar down with it. This is
comprehensive in the sense that actually matters here: no dangling sidecars, no silent failures,
nothing that can drift out of sync, without introducing any new moving parts beyond what's
already proposed.

**On the remaining awareness gap:** a customer who restarts a previously MCP-enabled service
without `--mcp` won't see an error. The service just runs normally without its sidecar, exactly as
it did while stopped. Nothing breaks, but the customer may not expect that. The preferred approach
is **documentation, not additional runtime machinery**: this behavior, with worked examples, belongs
in the customer-facing docs so anyone who hits it has a clear reference. That's a better trade
than adding state or logic to preempt every possible point of confusion, which risks compromising
the thing we're actually trying to deliver (something simple and easy to operate).

One candidate worth discussing with the dev lead, not yet decided: have `application start <name>`
print a static, unconditional reminder when run without `--mcp`, for example: *"Started without
MCP. Run with `--mcp` to enable AI agent tool access for this service."* Same message every time,
no memory of prior state required, so it doesn't conflict with the rule above. Documentation should
be the primary answer regardless of whether this ships.

## CLI Integration

The sidecar is surfaced through the existing `ai-services` CLI rather than as a standalone binary.
This keeps the customer experience in one place: the same tool used for bootstrapping and
application management also controls MCP.

### `application start` — opt in with a flag

The existing `application start` command gains an `--mcp` flag. When present, the CLI starts the
service sidecar container alongside the application container automatically. The customer does not
need to know or set the environment variables manually. The CLI resolves `OPENAPI_URL` and
`SERVICE_URL` from the application's known configuration.

In the examples below, `my-rag-app` is the customer-assigned name given to the application when it
was created — not the template name. A customer who deployed the RAG template and named it
`my-rag-app` would use that name in all commands.

```bash
# Start an application with its MCP sidecars (customer-assigned name)
ai-services application start my-rag-app --runtime podman --mcp

# Start without MCP (existing behaviour, unchanged)
ai-services application start my-rag-app --runtime podman
```

### `application mcp` — dedicated subcommand

A new `application mcp` subcommand provides direct control over MCP sidecar lifecycle independent
of the main application, and exposes config generation.

```bash
# Start MCP sidecars for all services in an application (uses customer-assigned name)
ai-services application mcp start my-rag-app --runtime podman

# Start MCP sidecars for specific services only
ai-services application mcp start my-rag-app --pod chatbot --pod summarize --runtime podman

# Stop the MCP sidecars without stopping the application
ai-services application mcp stop my-rag-app --runtime podman

# Emit an MCP client config file for all running sidecars
ai-services application mcp config --runtime podman --output ~/mcp.json
```

Because `mcp start` targets all services in an application at once, individual sidecars can
succeed or fail independently. The command attempts each launch separately, reports a result per
service, and exits non-zero if any failed:

```
chatbot     started
summarize   started
similarity  FAILED   port 3003 already in use
digitize    FAILED   container image not found
```

The customer can then re-run with `--pod` to target only the failed ones without restarting the
ones already running. Regardless of partial failures, `~/mcp.json` is regenerated at the end of
the command to reflect only the sidecars that are actually running. The agent never receives a
config entry pointing at a sidecar that did not start.

The `mcp config` subcommand reads which applications are currently running, resolves each sidecar
endpoint, and writes a single JSON file the customer can hand directly to their agent host. This
removes the need for any manual configuration.

### How this fits the bootstrap flow

A customer setting up from scratch follows the same sequence they already know, with MCP as an
opt-in step. The name used in each command (`my-rag-app` below) is whatever name the customer
chose when creating the application — not the template name.

```
ai-services bootstrap configure --runtime podman
    ↓
ai-services catalog configure --runtime podman
    ↓
ai-services application start my-rag-app --runtime podman --mcp   ← new flag, optional
    ↓
ai-services application mcp config --output ~/mcp.json            ← point agent here
```

No new top-level command is added. MCP lives under `application`, which is already where
customers manage running services.

## Implementation Details

### Components Reused (verbatim, no changes)

| File | What it does |
|---|---|
| `internal/openapi/loader.go` | Fetches + parses any `/openapi.json` |
| `internal/openapi/convert.go` | Converts OpenAPI schemas → MCP tool JSON Schema |
| `internal/tool/aggregator.go` | Tool registry + call routing |
| `internal/tool/provider.go` | Per-operation HTTP executor → `SERVICE_URL` |
| `internal/server/http.go` | HTTP transport, rate limiting, CORS, `/health` |
| `internal/config/config.go` | Emits MCP client config JSON |

### Files Modified (removing IBM Cloud-specific logic)

| File | Change |
|---|---|
| `cmd/ibmcloud-api-mcp/main.go` | Remove IBM hostname validation; rename binary |
| `internal/openapi/interface.go` | Remove IBM name-prefixing + region-server logic |
| `internal/tool/provider.go` | Remove the `region` input parameter it adds to every tool |
| `internal/server/http.go` | Swap IAM JWT validation for Bearer token passthrough; remove stdio entry point |
| `internal/authenticator/` | Remove `cli.go`, `op.go`; keep `env.go`, `passthrough.go`, `api_key.go` |
| `go.mod` | Rename module; drop `github.com/IBM/go-sdk-core/v5` |

### New Files

| File | Purpose |
|---|---|
| `Containerfile` | UBI9 build, `ppc64le` support for Power/Spyre |
| `.golangci.yml` | Same linters as `ai-services/.golangci.yml` |
| `Makefile` | Adapted upstream Makefile, `ppc64le` cross-compile target |

### CLI additions to `ai-services`

- **`cmd/ai-services/cmd/application/mcp.go`:** new `application mcp` subcommand group with
  `start`, `stop`, and `config` subcommands. Follows the same Cobra pattern used by all other
  `application` subcommands.
- **`application start`:** add `--mcp` boolean flag. When set, the start flow also launches the
  MCP sidecar container before returning.

### Note on OpenAPI Description Quality

The sidecar uses the `summary=` and `description=` fields already written for Swagger UI as MCP
tool descriptions. These are in good shape across all four services today, digitize included.
There is no linting rule that enforces their presence on new endpoints.

This is worth treating as higher priority than "nice to have someday." The failure mode isn't
abstract: someone adds a new endpoint under deadline pressure, skips `summary=`/`description=`
(nothing today stops them), and the generated tool ships with an empty or auto-derived description.
An ISV's agent either never discovers the new capability exists, or picks it for the wrong request
and produces a bad result. Because nothing fails in CI, engineering has no signal this happened
until a customer notices. Given the fix is a single lint rule addition (`ruff D103/D400/D401` in
`services/common/pyproject.toml`), it's included in Phase 1 below rather than deferred to Future
Work.

### Delivery Phases

The implementation splits into three independent stages. A separate execution plan will cover
task-level detail; the phases here describe scope boundaries. **The hard engineering work is in
Phase 1, not saved for last.** Phase 2 is packaging what already works; Phase 3 is wiring a
proven thing into the existing CLI and deployment templates.

**Phase 1 — Local validation.** Fork `ibmcloud-api-mcp` into `mcp/` as a new Go module and apply
the six targeted removals. Run the binary locally against a running chatbot service using stdio
transport and connect an MCP client to confirm tools appear and calls work. No container, no CLI
integration, no ICR push. This phase proves the core concept before any infrastructure work begins.
In parallel, add the `ruff D103/D400/D401` docstring lint rule to `services/common/pyproject.toml`.
This is low effort, and it protects the description quality this whole proposal depends on before
any service is exposed to a real ISV. This phase should also produce a decision on the Async
Operations question above, validated directly against `summarize`.

**Phase 2 — Container and local deployment.** Write the `Containerfile`, wire into the existing
`Makefile`, add `.golangci.yml`, and verify `go test ./...` passes. Build and run the container
locally with Podman alongside existing service containers. Publish to ICR only after the container
is verified locally, not before.

**Phase 3 — CLI integration.** Add `application mcp start/stop/config` subcommands and the
`--mcp` flag on `application start` to the `ai-services` CLI. Wire sidecar startup into the
Podman and OpenShift deployment templates. All four services, including digitize, ship together
once the Async Operations decision is resolved in Phase 1.

## Verification Plan

1. **Unit tests:** `go test ./...` passes after module rename and IBM code removal.
2. **Tool generation:** `--config` against chatbot produces valid MCP client JSON with tools
   `chat_completion` and `similarity_search` and non-empty descriptions.
3. **Tag filtering:** `--tag chat,retrieval` excludes `get_health` and `get_v1_models`.
4. **End-to-end call:** agent calls `chat_completion` → sidecar forwards to chatbot → real response.
5. **Container:** `podman build` succeeds; container starts and `/health` returns `{"status":"ok"}`.
6. **CLI — start with flag:** `application start rag --mcp` launches both application and sidecar
   containers; `application ps` shows both running.
7. **CLI — config output:** `application mcp config` emits valid MCP client JSON.
8. **Regression:** existing `application start` without `--mcp` is unchanged; all existing service
   tests pass; no service containers are modified.

## Future Work

- **Thin proxy gateway:** for ISVs requiring a single MCP endpoint, a lightweight aggregator
  merges `tools/list` from all per-service sidecars and routes calls. Not required for v1 since
  most MCP hosts support multiple servers natively.
- **Per-tool-call observability:** structured logging per `tools/call` (tool name, argument
  summary, duration, success/failure), distinct from generic HTTP access logs. Without this,
  diagnosing "the agent said it couldn't find X" means manually correlating raw request logs with
  no notion of which tool or which customer's agent was involved.
- **Schema change detection:** diff the regenerated tool schema against the previous run at
  startup and log/alert when it changes, so the drift described under Tool Schema Stability above
  is visible rather than silent.
- **OpenShift support:** Phase 3 covers Podman; OpenShift deployment template wiring follows the
  same pattern used by all other application deployments in `ai-services/assets/`.

## Open Questions

- **Description quality in practice:** did thin or missing OpenAPI descriptions ever visibly hurt
  tool-selection accuracy with `ibmcloud-api-mcp`, or was this a non-issue in practice?
- **Reuse expectations:** is `ibmcloud-api-mcp` realistically usable close to as-is for this fork,
  or is there friction to expect beyond the removals already identified?
- **Aggregation shape:** `ibmcloud-api-mcp` fronts many IBM Cloud APIs behind one server; this
  proposal instead runs one generic sidecar per service. Any operational downsides to the
  per-service approach already encountered that the team should know about going in?
- **Delete-cascade assumption:** do the existing Podman/OpenShift deployment templates already
  reliably apply the same `AppName` label to companion/sidecar-style containers, or is that
  untested territory to validate directly in Phase 3?
- **Sidecar exposure boundary:** the proposal covers auth from the sidecar to the real service
  (Bearer token passthrough), but not who's allowed to connect to the sidecar itself. If an agent
  reaches it over the network rather than purely localhost, what stops an unauthorized party from
  also connecting and calling tools?
- **Startup ordering:** if `application start --mcp` brings the service and its sidecar up at the
  same time, should the sidecar retry with backoff when the service isn't ready yet to serve its
  OpenAPI spec, rather than failing outright on first boot?
