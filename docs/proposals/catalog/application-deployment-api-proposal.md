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
     - 5.2.1 [List Applications](#521-list-applications)
     - 5.2.2 [Get Application Details](#522-get-application-details)
     - 5.2.3 [Create Application](#523-create-application)
     - 5.2.4 [Update Application](#524-update-application)
     - 5.2.5 [Delete Application](#525-delete-application)
     - 5.2.6 [Get Pod/Container Health Status](#526-get-podcontainer-health-status)
   - 5.3 [Catalog Endpoints](#53-catalog-endpoints)
     - 5.3.1 [List Available Architectures](#531-list-available-architectures)
     - 5.3.2 [Get Architecture Details](#532-get-architecture-details)
     - 5.3.3 [List Available Services](#533-list-available-services)
     - 5.3.4 [Get Service Details](#534-get-service-details)
     - 5.3.5 [Get Service Custom Parameters](#535-get-service-custom-parameters)
6. [Error Handling](#6-error-handling)
   - 6.1 [Error Response Format](#61-error-response-format)
   - 6.2 [HTTP Status Codes](#62-http-status-codes)

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

#### 5.2.1 List Applications

**Endpoint:** `GET /api/v1/applications`

**Description:** Retrieves a list of all applications for the authenticated user.

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
1. Validate the incoming JWT token from Authorization header
2. Execute database query to fetch all deployments from the applications table
3. Map the database response to the response struct
4. Return the mapped response with appropriate HTTP status code (200)

---

#### 5.2.2 Get Application Details

**Endpoint:** `GET /api/v1/applications/{appName}`

**Description:** Retrieves detailed information about a specific application.

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
      "type": "QA-Chatbot",
      "endpoints": [
        {
          "type": "ui",
          "url": "https://rag-production-chat-ui.apps.cluster.example.com"
        },
        {
          "type": "backend",
          "url": "https://rag-production-chat-api.apps.cluster.example.com"
        }
      ],
      "version": "1.0.0",
      "created_at": "2026-04-15T10:31:00Z",
      "updated_at": "2026-04-15T10:35:00Z"
    },
    {
      "id": "234b5678-c90d-12e3-f456-789012345def",
      "type": "Summary",
      "endpoints": [
        {
          "type": "backend",
          "url": "https://rag-production-summarization-api.apps.cluster.example.com"
        }
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
        {
          "type": "backend",
          "url": "http://localhost:8081"
        }
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
| type | string | Service type (e.g., "QA-Chatbot", "Summary", "Digitization") |
| endpoints | array | Array of endpoint objects |
| version | string | Service version |
| created_at | string | Creation timestamp |
| updated_at | string | Last update timestamp |

**Endpoint Object:**
| Field | Type | Description |
|-------|------|-------------|
| type | string | Endpoint type: "ui" or "backend" |
| url | string | Endpoint URL |

**Error Responses:**
- `401 Unauthorized` - Invalid or missing access token
- `404 Not Found` - Application not found
- `403 Forbidden` - User doesn't have access to this application
- `500 Internal Server Error` - Server error

**Implementation Notes:**
1. Validate the incoming JWT token from Authorization header
2. Execute database query on applications table using `app_name` as the filter
3. Perform JOIN with services table to fetch associated services
4. Map the database response to the response struct including nested services
5. Return the mapped response with appropriate HTTP status code

---

#### 5.2.3 Create Application

**Endpoint:** `POST /api/v1/applications`

**Description:** Creates a new application (architecture or service).

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
1. Validate the incoming JWT token from Authorization header
2. Validate `app_name` uniqueness by querying the applications table
3. Validate `app_name` format: alphanumeric with hyphens, 3-50 characters
4. Validate that the template (e.g., "Digital Assistant") is a valid template in the catalog
5. Create database records in all required tables with status "Downloading":
   - Insert record in applications table
   - Insert corresponding records in services table
6. Initiate async deployment job
7. Return immediately with 202 Accepted
8. Use background worker for actual deployment
9. Update database based on deployment success/failure:
   - On success: Update status to "Running" and populate endpoints in services table
   - On failure: Update status to "Error" with appropriate error message

---

#### 5.2.4 Update Application

**Endpoint:** `PUT /api/v1/applications/{appName}`

**Description:** Updates the display name of an existing application.

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
1. Validate the incoming JWT token from Authorization header
2. Validate the new `deployment_name` format and length (3-100 chars)
3. Execute database UPDATE query on applications table to update the `deployment_name` field using `app_name` as the filter
4. Fetch the complete updated application object from the database
5. Return the entire application object with updated `deployment_name` and `updated_at` timestamp

---

#### 5.2.5 Delete Application

**Endpoint:** `DELETE /api/v1/applications/{appName}`

**Description:** Deletes an application and all associated resources.

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

### 5.3 Catalog Endpoints

#### 5.3.1 List Available Architectures

**Endpoint:** `GET /api/v1/architectures`

**Description:** Retrieves a list of all available architecture templates.

**Request Headers:**
```
Authorization: Bearer <access_token>
```

**Request Body:** None

**Response (200 OK):**
```json
[
  {
    "name": "Digital Assistant",
    "description": "Complete RAG architecture with QA chatbot, summarization, and digitization services",
    "services": ["QA-Chatbot", "Summary", "Digitization"]
  }
]
```

**Response Schema:**
| Field | Type | Description |
|-------|------|-------------|
| name | string | Architecture template name |
| description | string | Description of the architecture |
| services | array | Array of service names included in this architecture |

**Error Responses:**
- `401 Unauthorized` - Invalid or missing access token
- `500 Internal Server Error` - Server error

**Implementation Notes:**
- **TODO:** exploring on asset structure to support granular deployments

---

#### 5.3.2 Get Architecture Details

**Endpoint:** `GET /api/v1/architectures?name="Digital Assistant"`

**Description:** Retrieves detailed information about a specific architecture template.

**Implementation Notes:**
- **TODO:** exploring on asset structure to support granular deployments

---

#### 5.3.3 List Available Services

**Endpoint:** `GET /api/v1/services`

**Description:** Retrieves a list of all available service templates.

**Request Headers:**
```
Authorization: Bearer <access_token>
```

**Request Body:** None

**Response (200 OK):**
```json
[
  {
    "name": "QA-Chatbot",
    "description": "Question-answering chatbot service with RAG capabilities",
    "reference_architectures": ["Digital Assistant"]
  },
  {
    "name": "Summary",
    "description": "Document summarization service",
    "reference_architectures": ["Digital Assistant"]
  },
  {
    "name": "Digitization",
    "description": "Document digitization and processing service",
    "reference_architectures": ["Digital Assistant"]
  }
]
```

**Response Schema:**
| Field | Type | Description |
|-------|------|-------------|
| name | string | Service template name |
| description | string | Description of the service |
| reference_architectures | array | Array of architecture names that include this service |

**Error Responses:**
- `401 Unauthorized` - Invalid or missing access token
- `500 Internal Server Error` - Server error

**Implementation Notes:**
- **TODO:** exploring on asset structure to support granular deployments

---

#### 5.3.4 Get Service Details

**Endpoint:** `GET /api/v1/services?name="Summarization"`

**Description:** Retrieves detailed information about a specific service template.

**Implementation Notes:**
- **TODO:** exploring on asset structure to support granular deployments

---

#### 5.3.5 Get Service Custom Parameters

**Endpoint:** `GET /api/v1/services/params?name="Digital Assistant"`

**Description:** Retrieves custom parameters schema for a specific architecture/service template. Returns JSON Schema format that UI can use to generate dynamic forms with validation.

**Request Headers:**
```
Authorization: Bearer <access_token>
```

**Query Parameters:**
| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| name | string | Yes | Architecture or service template name |

**Request Body:** None

**Response (200 OK):**
```json
{
  "$schema": "https://json-schema.org/draft-07/schema#",
  "type": "object",
  "properties": {
    "opensearch": {
      "type": "object",
      "properties": {
        "memoryLimit": {
          "type": "string",
          "pattern": "^[0-9]+(Ki|Mi|Gi|Ti|Pi|Ei)$",
          "description": "Memory limit for OpenSearch (e.g., 2Gi, 4Gi)"
        },
        "storage": {
          "type": "string",
          "pattern": "^[0-9]+(Ki|Mi|Gi|Ti|Pi|Ei)$",
          "description": "Storage size for OpenSearch (e.g., 10Gi, 20Gi)"
        },
        "auth": {
          "type": "object",
          "properties": {
            "password": {
              "type": "string",
              "minLength": 15,
              "allOf": [
                {
                  "pattern": ".*[a-z].*",
                  "description": "Must contain at least one lowercase letter"
                },
                {
                  "pattern": ".*[A-Z].*",
                  "description": "Must contain at least one uppercase letter"
                },
                {
                  "pattern": ".*[0-9].*",
                  "description": "Must contain at least one digit"
                },
                {
                  "pattern": ".*[@$!%*?&#^()_+\\-=\\[\\]{};':\"\\\\|,.<>/`~].*",
                  "description": "Must contain at least one special character"
                }
              ],
              "description": "Password must be at least 15 characters and contain at least one uppercase letter, one lowercase letter, one digit, and one special character"
            }
          }
        }
      }
    }
  }
}
```

**Response Schema:**
Returns a JSON Schema (draft-07) object that defines:
- Parameter structure and types
- Validation rules (patterns, minLength, allOf, etc.)
- Descriptions for each field
- Nested object properties

**UI Integration:**
The JSON Schema response can be directly consumed by form libraries such as:
- `react-jsonschema-form` / `@rjsf/core` (React)
- `vue-form-generator` (Vue)
- `angular-schema-form` (Angular)

These libraries will automatically:
- Generate form fields based on schema types
- Apply validation rules (pattern matching, length constraints)
- Display field descriptions and error messages
- Handle nested objects and complex structures

**Error Responses:**
- `400 Bad Request` - Invalid or missing name parameter
- `401 Unauthorized` - Invalid or missing access token
- `404 Not Found` - Template not found
- `500 Internal Server Error` - Server error

**Implementation Notes:**
- Read the values.schema.json file from the template's asset directory
- Return the schema as-is without modification
- UI libraries can consume this standard JSON Schema format directly
- **TODO:** finalizing on Implementation Notes

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
