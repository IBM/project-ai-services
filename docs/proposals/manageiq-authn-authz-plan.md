# ManageIQ AuthN/AuthZ Integration Plan

## Top-Level Overview

**Goal:** Extend the Catalog API Server to support ManageIQ as an Identity Provider (IdP) alongside
the existing local admin authentication. ManageIQ integration is opt-in: when `--manageiq-url` is
not configured the server falls back to local admin credentials. Two authentication flows are
planned; **only Flow B is currently implemented**:

- **Flow A — Interactive login (⚠️ Not yet implemented):** A user supplies username + password; the Catalog API authenticates against ManageIQ, fetches their groups, and issues an internal JWT.
- **Flow B — ManageIQ token passthrough (✅ Implemented):** An external product such as **IBM Power Mission Control** already holds a ManageIQ token from its own session and sends it directly to the Catalog API. The Catalog API validates the token against ManageIQ, resolves the identity and groups, and issues an internal JWT. IBM Power Mission Control does nothing beyond forwarding the token. 

**Scope:**

- Go Catalog API Server (`ai-services/internal/pkg/catalog/`)
- A new ManageIQ HTTP client package
- Extended JWT claims with no roles or group membership
- New `POST /api/v1/auth/token` endpoint for the token passthrough flow (Flow B)

**Non-Goals:**

- Python AI services (chatbot, digitize, similarity) — out of scope for this phase
- Replacing the token blacklist mechanism (it stays PostgreSQL-backed)
- OIDC discovery / full OAuth2 PKCE flow (covered in a follow-on if needed)
- ManageIQ token generation or management — that is IBM Power Mission Control's responsibility

**Integration Strategy (Phase 1):** Two-call Token Introspection
Both flows converge on the same two MIQ API calls for identity resolution (validated against
`https://9.20.202.144:8443`, API v4.4.0-pre):

1. `GET /api/auth` — (Flow A only) validates credentials and returns a bearer token + `token_ttl`
2. `GET /api?attributes=identity` with `X-Auth-Token: miq_token` — fetches caller identity and group membership; numeric user ID extracted from `identity.user_href`

Note: `GET /api/auth?requester_type=ui` only echoes a refreshed token; it does **not** return user identity or groups on this version.

---

## Architecture Diagrams

### Current State — Local Admin Authentication

```mermaid
graph TD
    CLI[CLI / UI Client] -->|POST /api/v1/auth/login\nusername+password| CatalogAPI[Catalog API Server\nGo / Gin]
    CatalogAPI -->|PBKDF2 verify| InMemoryRepo[In-Memory User Repo\nsingle admin user]
    CatalogAPI -->|Issues| JWT[JWT Access + Refresh\nHS256 / 15min / 24hr]
    JWT -->|Bearer token| ProtectedRoutes[Protected Routes\n/applications /catalog /resources]
    CatalogAPI -->|Blacklist check| TokenBlacklist[(PostgreSQL\ntokens_blacklist)]
```

---

### Target State — ManageIQ as Identity Provider

```mermaid
graph TD
    User[User / CLI / UI] -->|Credentials| MIQ[ManageIQ\nIdentity Provider]
    MIQ -->|User Identity + Groups| CatalogAPI[Catalog API Server\nGo / Gin]
    CatalogAPI -->|Token Introspection| MIQ

    CatalogAPI -->|Issues internal JWT| JWT[Internal JWT]
    JWT -->|AuthMiddleware validates| ProtectedRoutes[Protected Routes]

    subgraph RBAC [Role-Based Access Control   ⚠️ not yet implemented]
        ProtectedRoutes -->|role = admin| AdminOps[Deploy Applications\nManage Catalog]
        ProtectedRoutes -->|role = operator| OperatorOps[Deploy + Read]
        ProtectedRoutes -->|role = viewer| ViewOps[Read Catalog\nView Applications]
    end

    CatalogAPI -->|Blacklist check| TokenBlacklist[(PostgreSQL\ntokens_blacklist)]
```

---

### Integration Flow A — Interactive Login ⚠️ Not yet implemented

```mermaid
sequenceDiagram
    actor User
    participant MIQ as ManageIQ
    participant CatalogAPI as Catalog API

    User->>CatalogAPI: POST /api/v1/auth/login\nusername + password
    CatalogAPI->>MIQ: GET /api/auth\nBasic-auth username:password
    MIQ-->>CatalogAPI: auth_token + token_ttl=600s

    CatalogAPI->>MIQ: GET /api?attributes=identity\nX-Auth-Token: miq_token
    MIQ-->>CatalogAPI: identity.userid + identity.name + identity.user_href + identity.groups

    CatalogAPI->>CatalogAPI: Map miq_groups to internal role\nadmin/operator/viewer
    CatalogAPI-->>User: Internal JWT with role embedded\nTTL capped at 9m

    User->>CatalogAPI: GET /api/v1/applications\nAuthorization: Bearer internal-jwt
    CatalogAPI->>CatalogAPI: AuthMiddleware validates JWT\n+ checks blacklist + enforces role
    CatalogAPI-->>User: Response
```

---

### Integration Flow B — IBM Power Mission Control Token Passthrough

IBM Power Mission Control already holds a ManageIQ token from its own session. It forwards
that token as-is to the Catalog API. The Catalog API validates it against ManageIQ, resolves
the identity and groups, and issues an internal JWT. IBM PMC does nothing beyond sending the token.

```mermaid
sequenceDiagram
    participant PMC as IBM Power Mission Control
    participant MIQ as ManageIQ
    participant CatalogAPI as Catalog API

    note over PMC: PMC already holds a MIQ token\nfrom its own session
    PMC->>CatalogAPI: POST /api/v1/auth/token\nAuthorization: Bearer miq_token

    CatalogAPI->>MIQ: GET /api?attributes=identity\nX-Auth-Token: miq_token
    MIQ-->>CatalogAPI: 200 identity.userid + identity.name + identity.user_href + identity.groups<br/>OR 4xx or 500 if token invalid/expired or server errors

    CatalogAPI-->>PMC: Internal JWT pair\nTTL capped at 9m

    PMC->>CatalogAPI: GET /api/v1/applications\nAuthorization: Bearer internal-jwt
    CatalogAPI->>CatalogAPI: AuthMiddleware validates JWT
    CatalogAPI-->>PMC: Response
```

---

### Architectural Layers Affected

```mermaid
graph LR
    subgraph L1 [1 - MIQ Client]
        A[ManageIQ HTTP Client\nGetUserByToken\nErrUnauthorized / ManageIQError]
    end

    subgraph L2 [2 - Auth Service]
        B[LoginWithToken - Flow B IBM PMC passthrough\nseeds in-memory user repo]
    end

    subgraph L3 [3 - Auth Handler]
        C[POST /auth/token\nreturns JWT pair]
    end

    L1 --> L2 --> L3
```

---

### RBAC Role Hierarchy ⚠️ Not yet implemented

```mermaid
graph TD
    MIQRoles["miq_groups description field
    from GET /api?attributes=identity"] --> Mapping["Role Mapping Config
    MIQ_ROLE_ADMIN_GROUPS
    MIQ_ROLE_OPERATOR_GROUPS"]

    Mapping -->|EvmGroup-super_administrator| AdminRole([admin])
    Mapping -->|EvmGroup-administrator| AdminRole
    Mapping -->|EvmGroup-operator| OperatorRole([operator])
    Mapping -->|EvmGroup-auditor or unknown| ViewerRole([viewer])

    AdminRole -->|Full access| AllEndpoints["All API Endpoints
    CRUD Applications
    Manage Catalog"]
    OperatorRole -->|Read and Deploy| DeployEndpoints["Applications CRUD
    Catalog Read"]
    ViewerRole -->|Read only| ReadEndpoints["GET Endpoints Only"]
```

---
