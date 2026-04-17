# Application Deployment API Proposal

**Version:** 1.0
**Date:** April 17, 2026
**Status:** Draft

## Table of Contents

1. [Executive Summary](#1-executive-summary)
2. [Background and Motivation](#2-background-and-motivation)
   - 2.1 [Current State](#21-current-state)
   - 2.2 [Problem Statement](#22-problem-statement)
   - 2.3 [Goals](#23-goals)
3. [Architecture Overview](#3-architecture-overview)
   - 3.1 [Key Concepts](#31-key-concepts)
   - 3.2 [System Components](#32-system-components)
4. [API Specification](#4-api-specification)
   - 4.1 [Base URL](#41-base-url)
   - 4.2 [Authentication](#42-authentication)
   - 4.3 [Endpoint Categories](#43-endpoint-categories)
5. [API Endpoint Details](#5-api-endpoint-details)
   - 5.1 [Authentication Endpoints](#51-authentication-endpoints)
     - 5.1.1 [Login](#511-login)
     - 5.1.2 [Refresh Token](#512-refresh-token)
     - 5.1.3 [Logout](#513-logout)
     - 5.1.4 [Get Current User](#514-get-current-user)
   - 5.2 [Application Management Endpoints](#52-application-management-endpoints)
     - 5.2.1 [List Deployments](#521-list-deployments)
     - 5.2.2 [Get Deployment Details](#522-get-deployment-details)
     - 5.2.3 [Create Deployment](#523-create-deployment)
     - 5.2.4 [Update Deployment](#524-update-deployment)
     - 5.2.5 [Delete Deployment](#525-delete-deployment)
     - 5.2.6 [Get Pod/Container Health Status](#526-get-podcontainer-health-status)
6. [Error Handling](#6-error-handling)
   - 6.1 [Error Response Format](#61-error-response-format)
   - 6.2 [HTTP Status Codes](#62-http-status-codes)
7. [API Usage Examples](#7-api-usage-examples)

## 1. Executive Summary

This proposal outlines the design and implementation of a comprehensive REST API for managing application deployments in the AI Services Catalog. The API will enable users to deploy, monitor, and manage AI service applications through a unified interface, supporting both individual services and complete architectures across multiple runtime environments (Podman and OpenShift).

## 2. Background and Motivation

### 2.1 Current State
The AI Services Catalog currently provides various AI services (chat, summarization, digitization) that can be deployed independently. However, there is no unified API for managing these deployments programmatically.

### 2.2 Problem Statement
Users need a standardized way to:
- Deploy AI services individually or as complete architectures
- Monitor deployment status and health
- Manage service configurations
- Access service endpoints
- Handle authentication and authorization

### 2.3 Goals
1. Provide a RESTful API for application lifecycle management
2. Support both architecture-level (multiple services) and service-level deployments
3. Enable multi-runtime support (Podman and OpenShift)
4. Implement secure authentication and authorization


## 3. Architecture Overview

### 3.1 Key Concepts

**Architecture**: A collection of multiple services that work together as a cohesive application (e.g., RAG architecture includes chat, summarization, and digitization services).

**Service**: An individual AI service that can be deployed standalone (e.g., summarization service, chat service).

**Runtime**: The deployment environment (Podman for local/development, OpenShift for production/cluster).

### 3.2 Backend System Components

```
┌─────────────────────────────────────────────────────────────┐
│                        API Gateway                           │
│                   (http://localhost:8080)                    │
└─────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────┐
│                   Catalog Management                         │
│  ┌────────────────────────────────────────────────────────┐ │
│  │         Auth Middleware (JWT Bearer Token)             │ │
│  └────────────────────────────────────────────────────────┘ │
│  ┌────────────────────────────────────────────────────────┐ │
│  │            Application Management                      │ │
│  └────────────────────────────────────────────────────────┘ │
│  ┌────────────────────────────────────────────────────────┐ │
│  │            Application Assets                          │ │
│  └────────────────────────────────────────────────────────┘ │
└─────────────────────────────────────────────────────────────┘
                    │                   │
                    ▼                   ▼
      ┌──────────────────────┐   ┌──────────────────────┐
      │Database (PostgreSQL) │   │Runtime Orchestrators │
      └──────────────────────┘   │  ┌────────────────┐  │
                                 │  │ Podman         │  │
                                 │  │ OpenShift      │  │
                                 │  └────────────────┘  │
                                 └──────────────────────┘
```

## 4. API Specification

### 4.1 Base URL
```
http://localhost:8080/api/v1
```

### 4.2 Authentication

All endpoints (except `/auth/*`) require JWT Bearer token authentication:
```
Authorization: Bearer <access_token>
```

**Token Lifecycle:**
- Access tokens expire after 15 minutes
- Refresh tokens valid for 7 days
- Token blacklisting on logout

### 4.3 Endpoint Categories

#### Authentication Endpoints
- `POST /api/v1/auth/login` - User login
- `POST /api/v1/auth/refresh` - Refresh access token
- `POST /api/v1/auth/logout` - User logout
- `GET /api/v1/auth/me` - Get current user info

#### Application Management Endpoints
- `GET /api/v1/applications` - List all deployments
- `GET /api/v1/applications/{appName}` - Get deployment details
- `POST /api/v1/applications` - Create new deployment
- `PUT /api/v1/applications/{appName}` - Update deployment
- `DELETE /api/v1/applications/{appName}` - Delete deployment
- `GET /api/v1/applications/{appName}/ps` - Get pod/container health status

#### Catalog Endpoints
- `GET /api/v1/architectures` - List available architectures
- `GET /api/v1/architectures?name="Digital Assistant"` - Get architecture details

- `GET /api/v1/services` - List available services
- `GET /api/v1/services?name="Summarization"` - Get service details
- `GET /api/v1/services/params?name="Summarization"` - Get service custom params


## 5. API Endpoint Details

This section provides detailed specifications for each API endpoint, including request/response schemas and implementation notes.

### 5.1 Authentication Endpoints

#### 5.1.1 Login

**Endpoint:** `POST /api/v1/auth/login`

**Description:** Authenticates a user and returns JWT tokens for subsequent API calls.

**Request Headers:**
```
Content-Type: application/json
```

**Request Body:**
```json
{
  "username": "admin",
  "password": "password"
}
```

**Request Schema:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| username | string | Yes | User's username |
| password | string | Yes | User's password |

**Response (200 OK):**
```json
{
  "access_token": "eyJhbGc...",
  "refresh_token": "eyJhbGc...",
  "token_type": "Bearer",
  "expires_in": 900
}
```

**Response Schema:**
| Field | Type | Description |
|-------|------|-------------|
| access_token | string | JWT access token for API authentication |
| refresh_token | string | JWT refresh token for obtaining new access tokens |
| token_type | string | Token type (always "Bearer") |
| expires_in | integer | Access token expiration time in seconds (900 = 15 minutes) |

**Error Responses:**
- `401 Unauthorized` - Invalid credentials
- `400 Bad Request` - Missing or invalid request body

**Implementation Notes:**
- Passwords must be hashed using bcrypt or argon2
- Access tokens expire after 15 minutes
- Refresh tokens are valid for 7 days
- Failed login attempts should be rate-limited
- Consider implementing account lockout after multiple failed attempts

---

#### 5.1.2 Refresh Token

**Endpoint:** `POST /api/v1/auth/refresh`

**Description:** Obtains a new access token using a valid refresh token.

**Request Headers:**
```
Content-Type: application/json
```

**Request Body:**
```json
{
  "refresh_token": "eyJhbGc..."
}
```

**Request Schema:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| refresh_token | string | Yes | Valid refresh token from login |

**Response (200 OK):**
```json
{
  "access_token": "eyJhbGc...",
  "refresh_token": "eyJhbGc...",
  "token_type": "Bearer",
  "expires_in": 900
}
```

**Response Schema:**
| Field | Type | Description |
|-------|------|-------------|
| access_token | string | New JWT access token |
| refresh_token | string | New JWT refresh token |
| token_type | string | Token type (always "Bearer") |
| expires_in | integer | Access token expiration time in seconds |

**Error Responses:**
- `401 Unauthorized` - Invalid or expired refresh token
- `400 Bad Request` - Missing refresh token

**Implementation Notes:**
- Refresh tokens should be rotated on each use
- Old refresh tokens should be invalidated after rotation
- Implement refresh token blacklisting for logout functionality

---

#### 5.1.3 Logout

**Endpoint:** `POST /api/v1/auth/logout`

**Description:** Invalidates the current access and refresh tokens.

**Request Headers:**
```
Authorization: Bearer <access_token>
```

**Request Body:** None

**Response (200 OK):**
```json
{
  "message": "Successfully logged out"
}
```

**Response Schema:**
| Field | Type | Description |
|-------|------|-------------|
| message | string | Success message |

**Error Responses:**
- `401 Unauthorized` - Invalid or missing access token

**Implementation Notes:**
- Add both access and refresh tokens to blacklist
- Blacklist should persist until token expiration
- Consider using Redis for efficient token blacklisting
- Clean up expired tokens from blacklist periodically

---

#### 5.1.4 Get Current User

**Endpoint:** `GET /api/v1/auth/me`

**Description:** Returns information about the currently authenticated user.

**Request Headers:**
```
Authorization: Bearer <access_token>
```

**Request Body:** None

**Response (200 OK):**
```json
{
  "id": "uid_1",
  "username": "admin",
  "name": "Administrator"
}
```

**Response Schema:**
| Field | Type | Description |
|-------|------|-------------|
| id | string | Unique user identifier |
| username | string | User's username |
| name | string | User's display name |

**Error Responses:**
- `401 Unauthorized` - Invalid or missing access token

**Implementation Notes:**
- Extract user information from JWT token claims
- Do not expose sensitive information (password hash, etc.)
- Consider caching user information to reduce database queries

---

### 5.2 Application Management Endpoints

#### 5.2.1 List Deployments

**Endpoint:** `GET /api/v1/applications`

**Description:** Retrieves a list of all application deployments for the authenticated user.

**Request Headers:**
```
Authorization: Bearer <access_token>
```

**Query Parameters:**
| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| limit | integer | No | Number of results to return (default: 50, max: 100) |
| offset | integer | No | Pagination offset (default: 0) |

**Request Body:** None

**Response (200 OK):**
```json
[
  {
    "app_name": "rag-production",
    "deployment_name": "RAG Production",
    "deployment_type": "Architecture",
    "type": "Digital Assistant",
    "status": "Running",
    "message": "All services are operational",
    "created_at": "2026-04-15T10:30:00Z",
    "updated_at": "2026-04-15T10:35:00Z"
  },
  {
    "app_name": "summarization-dev",
    "deployment_name": "Summarization Dev",
    "deployment_type": "Service",
    "type": "Summary",
    "status": "Running",
    "message": "Service deployed successfully",
    "created_at": "2026-04-15T11:00:00Z",
    "updated_at": "2026-04-15T11:05:00Z"
  }
]
```

**Response Schema:**
| Field | Type | Description |
|-------|------|-------------|
| app_name | string | Application name (Primary Key, immutable, used for resource naming) |
| deployment_name | string | User-friendly display name of the deployment |
| deployment_type | string | Type of deployment: "Architecture" or "Service" |
| type | string | Application type: "Digital Assistant" for architectures, "Summary" for summarization services |
| status | string | Current status: "Downloading", "Deploying", "Running", "Deleting", "Error" |
| message | string | Status message or error details |
| created_at | string | ISO 8601 timestamp of creation |
| updated_at | string | ISO 8601 timestamp of last update |

**Error Responses:**
- `401 Unauthorized` - Invalid or missing access token
- `500 Internal Server Error` - Server error

**Implementation Notes:**
- Results should be ordered by `created_at` DESC by default
- Implement cursor-based pagination for large result sets
- Consider caching frequently accessed lists
- Filter by user ownership based on JWT token claims

---

#### 5.2.2 Get Deployment Details

**Endpoint:** `GET /api/v1/applications/{appName}`

**Description:** Retrieves detailed information about a specific application deployment.

**Request Headers:**
```
Authorization: Bearer <access_token>
```

**Path Parameters:**
| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| appName | string | Yes | Application name (app_name field) |

**Request Body:** None

**Response (200 OK) - Deployable Architecture:**
```json
{
  "app_name": "rag-production",
  "deployment_name": "RAG Production",
  "deployment_type": "Architecture",
  "type": "Digital Assistant",
  "status": "Running",
  "message": "All services are operational",
  "created_at": "2026-04-15T10:30:00Z",
  "updated_at": "2026-04-15T10:35:00Z",
  "services": [
    {
      "id": "789a0123-b45c-67d8-e901-234567890abc",
      "type": "Chat",
      "endpoints": [
        "https://rag-production-chat-ui.apps.cluster.example.com",
        "https://rag-production-chat-api.apps.cluster.example.com"
      ],
      "version": "1.0.0",
      "created_at": "2026-04-15T10:31:00Z",
      "updated_at": "2026-04-15T10:35:00Z"
    },
    {
      "id": "234b5678-c90d-12e3-f456-789012345def",
      "type": "Summary",
      "endpoints": [
        "https://rag-production-summarization-api.apps.cluster.example.com"
      ],
      "version": "1.0.0",
      "created_at": "2026-04-15T10:32:00Z",
      "updated_at": "2026-04-15T10:35:00Z"
    }
  ]
}
```

**Response (200 OK) - Services Deployment:**
```json
{
  "app_name": "summarization-dev",
  "deployment_name": "Summarization Dev",
  "deployment_type": "Service",
  "type": "Summary",
  "status": "Running",
  "message": "Service deployed successfully",
  "created_at": "2026-04-15T11:00:00Z",
  "updated_at": "2026-04-15T11:05:00Z",
  "services": [
    {
      "id": "567c8901-d23e-45f6-g789-012345678hij",
      "type": "Summary",
      "endpoints": [
        "http://localhost:8081"
      ],
      "version": "1.0.0",
      "created_at": "2026-04-15T11:00:00Z",
      "updated_at": "2026-04-15T11:05:00Z"
    }
  ]
}
```

**Response Schema:**

**Application Level:**
| Field | Type | Description |
|-------|------|-------------|
| app_name | string | Application name (Primary Key, immutable) |
| deployment_name | string | User-friendly display name of the deployment |
| deployment_type | string | "Architecture" or "Service" |
| type | string | Application type: "Digital Assistant" for architectures, "Summary" for summarization services |
| status | string | Current status (Downloading, Deploying, Running, Deleting, Error) |
| message | string | Status message or error details |
| created_at | string | Creation timestamp |
| updated_at | string | Last update timestamp |
| services | array | Array of service objects |

**Service Object:**
| Field | Type | Description |
|-------|------|-------------|
| id | string | Unique service identifier (UUID) |
| type | string | Service type (e.g., "Chat", "Summary", "Digitization") |
| endpoints | array | Array of service endpoint URLs |
| version | string | Service version |
| created_at | string | Creation timestamp |
| updated_at | string | Last update timestamp |

**Error Responses:**
- `401 Unauthorized` - Invalid or missing access token
- `404 Not Found` - Application not found
- `403 Forbidden` - User doesn't have access to this application
- `500 Internal Server Error` - Server error

**Implementation Notes:**
- Verify user ownership before returning details
- Include real-time status from runtime environment
- Cache endpoint URLs to reduce runtime queries
- For OpenShift, query route objects for endpoint URLs
- For Podman, query container inspect for port mappings

---

#### 5.2.3 Create Deployment

**Endpoint:** `POST /api/v1/applications`

**Description:** Creates a new application deployment (architecture or service).

**Request Headers:**
```
Authorization: Bearer <access_token>
Content-Type: application/json
```

**Request Body:**
```json
{
  "app_name": "rag-production",
  "deployment_name": "RAG Production",
  "deployment_type": "Architecture",
  "template": "Digital Assistant"
}
```

**Request Schema:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| app_name | string | Yes | Application name (3-50 chars, alphanumeric with hyphens, will be Primary Key) |
| deployment_name | string | Yes | User-friendly display name (3-100 chars) |
| deployment_type | string | Yes | Deployment type: "Architecture" or "Service" |
| template | string | Yes | Template name (e.g., "Digital Assistant", "Summary") |

**Response (202 Accepted):**
```json
{
  "app_name": "rag-production",
  "deployment_name": "RAG Production",
  "deployment_type": "Architecture",
  "status": "Downloading",
  "message": "Deployment initiated successfully",
  "created_at": "2026-04-15T10:30:00Z",
  "updated_at": "2026-04-15T10:30:00Z"
}
```

**Response Schema:**
| Field | Type | Description |
|-------|------|-------------|
| app_name | string | Application name (Primary Key, immutable) |
| deployment_name | string | User-friendly display name |
| deployment_type | string | Deployment type |
| status | string | Initial status ("Downloading") |
| message | string | Status message |
| created_at | string | Creation timestamp |
| updated_at | string | Last update timestamp |

**Error Responses:**
- `400 Bad Request` - Invalid request body or validation errors
- `401 Unauthorized` - Invalid or missing access token
- `409 Conflict` - Application name already exists
- `422 Unprocessable Entity` - Configuration validation failed
- `500 Internal Server Error` - Server error

**Implementation Notes:**
- Validate `app_name` uniqueness (must be unique, serves as Primary Key)
- Validate `app_name` format: alphanumeric with hyphens, 3-50 characters
- Validate template exists in catalog
- Create database record with status "Downloading"
- Initiate async deployment job
- Return immediately with 202 Accepted
- Use background worker for actual deployment
- The `app_name` is used for prefixing pod names (Podman) and namespace names (OpenShift)

---

#### 5.2.4 Update Deployment

**Endpoint:** `PUT /api/v1/applications/{appName}`

**Description:** Updates the display name of an existing application deployment.

**Request Headers:**
```
Authorization: Bearer <access_token>
Content-Type: application/json
```

**Path Parameters:**
| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| appName | string | Yes | Application name (app_name field) |

**Request Body:**
```json
{
  "deployment_name": "RAG Production Updated"
}
```

**Request Schema:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| deployment_name | string | Yes | Updated display name (3-100 chars) |

**Response (200 OK):**
```json
{
  "app_name": "rag-production",
  "deployment_name": "RAG Production Updated",
  "deployment_type": "Architecture",
  "type": "Digital Assistant",
  "status": "Running",
  "message": "Deployment name updated successfully",
  "updated_at": "2026-04-15T11:00:00Z"
}
```

**Response Schema:**
| Field | Type | Description |
|-------|------|-------------|
| app_name | string | Application name (Primary Key, unchanged) |
| deployment_name | string | Updated display name |
| deployment_type | string | Deployment type |
| type | string | Application type |
| status | string | Current status |
| message | string | Status message |
| updated_at | string | Update timestamp |

**Error Responses:**
- `400 Bad Request` - Invalid request body or name validation failed
- `401 Unauthorized` - Invalid or missing access token
- `403 Forbidden` - User doesn't own this application
- `404 Not Found` - Application not found
- `500 Internal Server Error` - Server error

**Implementation Notes:**
- Only allow updates when status is "running" or "error"
- Validate configuration changes against template schema
- Perform rolling update for zero-downtime (OpenShift)
- Update database record with new configuration
- Initiate async update job
- Cannot update: name, type, template, runtime (immutable fields)
- Log configuration changes for audit trail

---

#### 5.2.5 Delete Deployment

**Endpoint:** `DELETE /api/v1/applications/{appName}`

**Description:** Deletes an application deployment and all associated resources.

**Request Headers:**
```
Authorization: Bearer <access_token>
```

**Path Parameters:**
| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| appName | string | Yes | Application name (app_name field) |

**Query Parameters:**
| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| skip-cleanup | boolean | No | If true, skips data cleanup (default: false) |

**Request Body:** None

**Response (202 Accepted):**
```json
{
  "app_name": "rag-production",
  "status": "deleting",
  "message": "Deletion initiated successfully"
}
```

**Response Schema:**
| Field | Type | Description |
|-------|------|-------------|
| app_name | string | Application name (Primary Key) |
| status | string | Status (deleting) |
| message | string | Status message |

**Error Responses:**
- `401 Unauthorized` - Invalid or missing access token
- `403 Forbidden` - User doesn't own this application
- `404 Not Found` - Application not found
- `409 Conflict` - Application is already being deleted
- `500 Internal Server Error` - Server error

**Implementation Notes:**
- Update database status to "deleting"
- Initiate async deletion job
- Delete in order: services → infrastructure → namespace/pods
- Handle partial deletion failures gracefully
- If skip-cleanup=true, preserve application data (documents, embeddings, etc.)
- If skip-cleanup=false (default), clean up all application data
- Clean up database records after successful deletion
- Implement soft delete with `deleted_at` timestamp
- Keep audit trail of deleted applications
- For OpenShift: delete namespace and all resources
- For Podman: stop and remove all containers

---

#### 5.2.6 Get Pod/Container Health Status

**Endpoint:** `GET /api/v1/applications/{appName}/ps`

**Description:** Retrieves health status of all pods/containers in the deployment.

**Request Headers:**
```
Authorization: Bearer <access_token>
```

**Path Parameters:**
| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| appName | string | Yes | Application name (app_name field) |

**Request Body:** None

**Response (200 OK):**
```json
{
  "app_name": "rag-production",
  "pods": [
    {
      "pod_id": "a1b2c3d4e5f6",
      "pod_name": "rag-production-chat-ui",
      "status": "Running (Ready)",
      "created": "2d5h",
      "exposed": "8080, 8081",
      "containers": [
        {
          "name": "chat-ui",
          "status": "Ready"
        },
        {
          "name": "nginx",
          "status": "Ready"
        }
      ]
    },
    {
      "pod_id": "b2c3d4e5f6g7",
      "pod_name": "rag-production-chat-api",
      "status": "Running (Ready)",
      "created": "2d5h",
      "exposed": "8082",
      "containers": [
        {
          "name": "chat-api",
          "status": "Ready"
        }
      ]
    },
    {
      "pod_id": "c3d4e5f6g7h8",
      "pod_name": "rag-production-summarization-api",
      "status": "Running (NotReady)",
      "created": "2d5h",
      "exposed": "8083",
      "containers": [
        {
          "name": "summarization-api",
          "status": "starting"
        }
      ]
    }
  ]
}
```

**Response Schema:**
| Field | Type | Description |
|-------|------|-------------|
| app_name | string | Application name (Primary Key) |
| pods | array | Array of pod objects |

**Pod Object Schema:**
| Field | Type | Description |
|-------|------|-------------|
| pod_id | string | Pod ID (first 12 characters) |
| pod_name | string | Pod name |
| status | string | Pod status with health indicator (e.g., "Running (Ready)", "Running (NotReady)") |
| created | string | Time since pod creation (e.g., "2d5h", "30m") |
| exposed | string | Comma-separated list of exposed ports or "none" |
| containers | array | Array of container objects within the pod |

**Container Object Schema:**
| Field | Type | Description |
|-------|------|-------------|
| name | string | Container name |
| status | string | Container status (Ready, running, starting, exited, etc.) |

**Error Responses:**
- `401 Unauthorized` - Invalid or missing access token
- `403 Forbidden` - User doesn't own this application
- `404 Not Found` - Application not found
- `500 Internal Server Error` - Server error
- `503 Service Unavailable` - Cannot connect to runtime

**Implementation Notes:**
- Use the same output format for both Podman and OpenShift runtimes
- Pod status includes health indicator: "Running (Ready)" when all containers are healthy, "Running (NotReady)" when some containers are unhealthy
- Container status shows health check results: "Ready" for healthy containers, actual status (starting, exited, etc.) for others
- Filter pods by application label: `ai-services.io/application=<app_name>`
- For OpenShift: query pods using Kubernetes API
- For Podman: use `podman pod ps` and `podman pod inspect`
- Cache results for 5-10 seconds to reduce API calls
- Handle cases where runtime is temporarily unavailable
- Return partial results if some pods are inaccessible

---

## 6. Error Handling

### 6.1 Error Response Format
```json
{
  "error": "error_code",
  "message": "Human-readable error message",
  "details": [
    {
      "field": "field_name",
      "message": "Field-specific error"
    }
  ]
}
```

### 6.2 HTTP Status Codes
- `200 OK` - Successful request
- `202 Accepted` - Async operation initiated
- `400 Bad Request` - Invalid request
- `401 Unauthorized` - Authentication required
- `404 Not Found` - Resource not found
- `409 Conflict` - Resource conflict (e.g., duplicate name)
- `422 Unprocessable Entity` - Validation failed
- `500 Internal Server Error` - Server error

## 7. API Usage Examples

### Example 1: Deploy RAG Architecture
```bash
# 1. Login
curl -X POST http://localhost:8080/api/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{"username":"admin","password":"password"}'

# Response: {"access_token": "eyJhbGc...", ...}

# 2. List Available Architectures
curl -X GET http://localhost:8080/api/v1/architectures \
  -H "Authorization: Bearer <token>"

# 3. Deploy RAG Architecture
curl -X POST http://localhost:8080/api/v1/applications \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{
    "app_name": "rag-production",
    "deployment_name": "RAG Production",
    "deployment_type": "Architecture",
    "template": "Digital Assistant"
  }'

# Response: {"app_name": "rag-production", "deployment_name": "RAG Production", "status": "Downloading", ...}

# 4. Check Deployment Status
curl -X GET http://localhost:8080/api/v1/applications/rag-production \
  -H "Authorization: Bearer <token>"
```

### Example 2: Deploy Single Service
```bash
# 1. Deploy Summarization Service
curl -X POST http://localhost:8080/api/v1/applications \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{
    "app_name": "summarization-dev",
    "deployment_name": "Summarization Dev",
    "deployment_type": "Service",
    "template": "Summary"
  }'
```

---
