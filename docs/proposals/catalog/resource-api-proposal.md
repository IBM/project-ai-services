# Resource API Proposal

**Version:** 1.0
**Date:** May 19, 2026
**Status:** Draft

## Table of Contents

1. [Executive Summary](#1-executive-summary)
2. [Goals](#2-goals)
3. [Key Concepts](#3-key-concepts)
4. [API Specification](#4-api-specification)
   - [4.1 Base URL](#41-base-url)
   - [4.2 Authentication](#42-authentication)
   - [4.3 Endpoint](#43-endpoint)
5. [API Endpoint Details](#5-api-endpoint-details)
   - [5.1 Get System Resources](#51-get-system-resources)
6. [Implementation Details](#6-implementation-details)
   - [6.1 Podman Integration](#61-podman-integration)
   - [6.2 Resource Calculation](#62-resource-calculation)
   - [6.3 Spyre Card Detection](#63-spyre-card-detection)
7. [Response Schema](#7-response-schema)
   - [7.1 Success Response](#71-success-response)
   - [7.2 Error Response](#72-error-response)
8. [Error Handling](#8-error-handling)
9. [Resource Requirements in Deploy Options API](#9-resource-requirements-in-deploy-options-api)
   - [9.1 Overview](#91-overview)
   - [9.2 Resource Specification Format](#92-resource-specification-format)
   - [9.3 Service-Level Resources](#93-service-level-resources)
   - [9.4 Provider-Level Resources](#94-provider-level-resources)
   - [9.5 Metadata Source](#95-metadata-source)
   - [9.6 Use Cases](#96-use-case)

## 1. Executive Summary

This proposal outlines the design and implementation of a Resource API endpoint for the AI Services Catalog. The API provides real-time visibility into system resource availability, including CPU cores, memory, and Spyre accelerator cards.

## 2. Goals

1. Provide a RESTful API endpoint for querying system resources
2. Support real-time resource availability metrics
3. Include Spyre card detection and availability
4. Enable resource-aware deployment decisions
5. Maintain consistency with existing catalog API patterns
6. Support future OpenShift resource queries

## 3. Key Concepts

**CPU Resources**: Total and available CPU cores on the host system, calculated from idle percentage.

**Memory Resources**: Total and free memory in bytes on the host system.

**Spyre Cards**: IBM Power11 AI accelerator cards available for AI workload acceleration.

## 4. API Specification

### 4.1 Base URL

```
http://localhost:8080/api/v1
```

### 4.2 Authentication

The endpoint requires JWT Bearer token authentication:

```
Authorization: Bearer <access_token>
```

**Token Requirements:**

- Valid access token (not expired)
- Token not blacklisted
- User has appropriate permissions

### 4.3 Endpoint

```
GET /api/v1/resources
```

Returns current system resource availability including CPU, memory, and Spyre cards.

## 5. API Endpoint Details

### 5.1 Get System Resources

**Endpoint:** `GET /api/v1/resources`

**Description:** Retrieves real-time system resource information including CPU cores, memory, and Spyre card availability. This endpoint is designed for Podman environments and provides essential metrics for resource-aware deployment decisions.

**Authentication:** Required (JWT Bearer Token)

**Request:**

```http
GET /api/v1/resources HTTP/1.1
Host: localhost:8080
Authorization: Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...
```

**Success Response (200 OK):**

```json
{
  "cpu": {
    "total_cores": 16,
    "available_cores": 12.08
  },
  "memory": {
    "total_bytes": 68719476736,
    "available_bytes": 34359738368
  },
  "accelerators": {
    "ibm.com/spyre_pf": {
      "total": 4,
      "available": 2
    }
  }
}
```

**Response Fields:**

| Field                                     | Type    | Description                                                                       |
| ----------------------------------------- | ------- | --------------------------------------------------------------------------------- |
| `cpu.total_cores`                         | integer | Total number of CPU cores on the system                                           |
| `cpu.available_cores`                     | float   | Number of available CPU cores, calculated as `(total_cores × idle_percent) / 100` |
| `memory.total_bytes`                      | integer | Total system memory in bytes                                                      |
| `memory.available_bytes`                  | integer | Available memory in bytes that can be allocated                                   |
| `accelerators.ibm.com/spyre_pf.total`     | integer | Total number of Spyre cards detected                                              |
| `accelerators.ibm.com/spyre_pf.available` | integer | Number of Spyre cards available for allocation                                    |

**Notes:**

- `available_cores` is a floating-point value representing the effective number of idle cores that can be allocated
- Memory values are in bytes; clients should convert to GB/MB as needed
- "available" indicates resources that can be allocated to new workloads

**Error Response (401 Unauthorized):**

```json
{
  "error": "Invalid or missing access token"
}
```

**Error Response (500 Internal Server Error):**

```json
{
  "error": "Failed to get system information: connection to Podman service failed"
}
```

**Example Usage:**

```bash
# Using curl
curl -X GET http://localhost:8080/api/v1/resources \
  -H "Authorization: Bearer <access_token>"

```

**Response Examples:**

```json
{
  "cpu": {
    "total_cores": 32,
    "available_cores": 24.16
  },
  "memory": {
    "total_bytes": 137438953472,
    "available_bytes": 82463372288
  },
  "accelerators": {
    "ibm.com/spyre_pf": {
      "total": 8,
      "available": 5
    }
  }
}
```

## 6. Implementation Details

### 6.1 Podman Integration

The Resource API leverages the Podman Go bindings to retrieve system information:

**Technology Stack:**

- `github.com/containers/podman/v5/pkg/bindings/system` - Podman system info API
- Go standard library for calculations

**Data Flow:**

1. Create Podman client connection
2. Call `system.Info()` to retrieve host information
3. Extract CPU and memory metrics from response
4. Calculate free cores from idle percentage
5. Query Spyre card availability separately

**Code Location:**

- Handler: `ai-services/internal/pkg/catalog/apiserver/handlers/resources.go`
- Podman Client: `ai-services/internal/pkg/runtime/podman/podman.go`
- Spyre Helpers: `ai-services/internal/pkg/cli/helpers/`

### 6.2 Resource Calculation

**CPU Available Cores Calculation:**

The available cores metric represents the effective number of CPU cores available for new workloads:

```go
// Get idle percentage from Podman system info
idlePercent := info.Host.CPUUtilization.IdlePercent

// Calculate available cores
availableCores := (float64(totalCores) * idlePercent) / 100.0
```

**Example:**

- System with 16 cores at 75.5% idle
- Available cores = (16 × 75.5) / 100 = 12.08 cores

This metric provides a more intuitive understanding of available CPU capacity compared to raw percentage values.

**Memory Calculation:**

Memory metrics are directly obtained from Podman system info:

```go
totalBytes := info.Host.MemTotal
availableBytes := info.Host.MemFree
```

### 6.3 Spyre Card Detection

Spyre card availability is determined through helper functions:

**Detection Process:**

1. `helpers.ListSpyreCards()` - Enumerates all Spyre cards on the system
2. `helpers.FindFreeSpyreCards()` - Identifies cards not currently allocated
3. Calculate available count: `availableCards = totalCards - usedCards`

**Error Handling:**

- If Spyre card detection fails, the `accelerators` field returns with 0 values
- Errors are logged but don't fail the entire request
- Allows graceful degradation on systems without Spyre cards

**Spyre Card States:**

- **Total Cards**: All Spyre cards detected via device enumeration
- **Available Cards**: Cards not currently bound to running containers/pods that can be allocated
- **Used Cards**: Cards allocated to active AI workloads (not returned in API)

## 7. Response Schema

### 7.1 Success Response

**Schema Definition:**

```go
type ResourcesResponse struct {
    CPU          *CPUInfo                    `json:"cpu,omitempty"`
    Memory       *MemoryInfo                 `json:"memory,omitempty"`
    Accelerators map[string]*AcceleratorInfo `json:"accelerators,omitempty"`
}

type CPUInfo struct {
    TotalCores     int     `json:"total_cores"`
    AvailableCores float64 `json:"available_cores"`
}

type MemoryInfo struct {
    TotalBytes     int64 `json:"total_bytes"`
    AvailableBytes int64 `json:"available_bytes"`
}

type AcceleratorInfo struct {
    Total     int `json:"total"`
    Available int `json:"available"`
}
```

**Field Constraints:**

- All numeric fields are non-negative
- `available_cores` ≤ `total_cores`
- `available_bytes` ≤ `total_bytes`
- `accelerators.ibm.com/spyre_pf.available` ≤ `accelerators.ibm.com/spyre_pf.total`
- `accelerators` is always included with 0 values if no cards are detected

### 7.2 Error Response

**Schema Definition:**

```go
type ErrorResponse struct {
    Error string `json:"error"`
}
```

**Common Error Messages:**

- `"Invalid or missing access token"` - Authentication failure
- `"Failed to get system information: <details>"` - Podman connection error
- `"Failed to create Podman client: <details>"` - Client initialization error

## 8. Error Handling

**HTTP Status Codes:**

| Status Code               | Scenario                      | Response           |
| ------------------------- | ----------------------------- | ------------------ |
| 200 OK                    | Successful resource query     | Resource data JSON |
| 401 Unauthorized          | Missing/invalid JWT token     | Error message      |
| 500 Internal Server Error | Podman connection failure     | Error message      |
| 500 Internal Server Error | System info retrieval failure | Error message      |

**Error Scenarios:**

1. **Podman Service Unavailable:**
   - Status: 500
   - Message: "Failed to create Podman client: connection refused"
   - Cause: Podman service not running or socket inaccessible

2. **System Info Retrieval Failure:**
   - Status: 500
   - Message: "Failed to get system information: <error details>"
   - Cause: Podman API error or permission issues

3. **Authentication Failure:**
   - Status: 401
   - Message: "Invalid or missing access token"
   - Cause: Expired, invalid, or blacklisted JWT token

**Partial Failure Handling:**

If Spyre card detection fails, the API returns successfully with CPU and memory data, and Spyre cards set to 0:

```json
{
  "cpu": {
    "total_cores": 16,
    "available_cores": 12.08
  },
  "memory": {
    "total_bytes": 68719476736,
    "available_bytes": 34359738368
  },
  "accelerators": {
    "ibm.com/spyre_pf": {
      "total": 0,
      "available": 0
    }
  }
}
```

This ensures the API remains functional and provides consistent response structure even when Spyre cards are unavailable or detection fails.

## 9. Resource Requirements in Deploy Options and Application APIs

### 9.1 Overview

The Deploy Options API and Get Application by ID API (`/api/v1/applications/{id}`) have to be enhanced to include resource requirement specifications for services and component providers. This feature enables clients to understand the computational resources needed to deploy specific services and their components, facilitating resource-aware deployment decisions and capacity planning.

**Applicable APIs:**

- Deploy Options API: `/api/v1/services/{service_id}/deploy-options`
- Get Application by ID API: `/api/v1/applications/{id}`

**Key Benefits:**

- **Resource Planning**: Clients can estimate total resource requirements before deployment
- **Provider Selection**: Compare resource requirements across different providers
- **Capacity Validation**: Verify system capacity against deployment requirements
- **Cost Estimation**: Calculate infrastructure costs based on resource needs

### 9.2 Resource Specification Format

Resource requirements are specified using a standardized format with four key metrics:

```json
{
  "resources": {
    "cpu": 1,
    "memory": 4294967296,
    "accelerators": {
      "ibm.com/spyre_pf": 1
    },
    "storage": 1073741824
  }
}
```

**Resource Fields:**

| Field                           | Type    | Unit  | Description                                |
| ------------------------------- | ------- | ----- | ------------------------------------------ |
| `cpu`                           | integer | cores | Number of CPU cores required               |
| `memory`                        | integer | bytes | Memory requirement in bytes                |
| `accelerators.ibm.com/spyre_pf` | integer | count | Number of Spyre accelerator cards required |
| `storage`                       | integer | bytes | Persistent storage requirement in bytes    |

**Notes:**

- All fields are optional; omitted fields indicate no specific requirement
- Values represent minimum requirements for optimal performance
- Actual resource usage may vary based on workload and configuration
- **Memory and storage values are specified in bytes** for precision and consistency

### 9.3 Service-Level Resources

Service-level resources represent the base computational requirements for the service itself, excluding its component dependencies.

**Example - Service Deploy Options Response:**

```json
{
  "id": "chat",
  "name": "Question and Answer",
  "components": [
    {
      "type": "vector_store",
      "name": "Vector Store",
      "providers": [...]
    }
  ],
  "resources": {
    "cpu": 1,
    "memory": 4294967296,
    "accelerators": {
      "ibm.com/spyre_pf": 1
    },
    "storage": 1073741824
  }
}
```

**Service Resource Calculation:**

Total service deployment resources = Service base resources + Sum of selected component provider resources

**Example Calculation:**

```
Service (chat):              1 CPU, 4294967296 bytes memory, 1 spyre_pf, 1073741824 bytes storage
+ Vector Store (opensearch): 1 CPU, 4294967296 bytes memory, 1 spyre_pf, 1073741824 bytes storage
+ Embedding (vllm):          2 CPU, 8589934592 bytes memory, 1 spyre_pf, 2147483648 bytes storage
+ LLM (vllm):                4 CPU, 17179869184 bytes memory, 2 spyre_pf, 4294967296 bytes storage
──────────────────────────────────────────────────────────────────────────────────────────────────
Total:                       8 CPU, 34359738368 bytes memory, 5 spyre_pf, 8589934592 bytes storage
```

### 9.4 Provider-Level Resources

Component providers can specify their own resource requirements, allowing clients to compare resource needs across different provider options.

**Example - Provider with Resources:**

```json
{
  "type": "vector_store",
  "name": "Vector Store",
  "providers": [
    {
      "id": "opensearch",
      "name": "OpenSearch",
      "description": "OpenSearch vector database",
      "schema": "/api/v1/components/vector_store/providers/opensearch/params",
      "resources": {
        "cpu": 1,
        "memory": 4294967296,
        "accelerators": {
          "ibm.com/spyre_pf": 1
        },
        "storage": 1073741824
      }
    },
    {
      "id": "milvus",
      "name": "Milvus",
      "description": "Milvus vector database",
      "resources": {
        "cpu": 1,
        "memory": 4096,
        "accelerators": {
          "ibm.com/spyre_pf": 1
        },
        "storage": 1024
      }
    }
  ]
}
```

**Provider Resource Considerations:**

- **Optional Field**: Not all providers require explicit resource specifications
- **Provider Comparison**: Clients can compare resource requirements when selecting providers
- **Scaling**: Resource values represent single-instance requirements; scaling may multiply these values
- **Configuration Impact**: Actual requirements may vary based on provider-specific parameters

**Example - Provider without Resources:**

```json
{
  "id": "watsonx",
  "name": "IBM watsonx",
  "description": "IBM watsonx embedding models",
  "schema": "/api/v1/components/embedding/providers/watsonx/params"
}
```

Providers without a `resources` field typically represent:

- External/managed services (e.g., IBM watsonx cloud services)
- Components with negligible resource requirements
- Providers where resource requirements are highly variable based on configuration

### 9.5 Metadata Source

Resource specifications are read from metadata files in the catalog structure:

**Service Metadata (`metadata.yaml`):**

```yaml
id: chat
name: Question and Answer
description: Interactive Q&A service
type: service
certified_by: IBM
architectures:
  - rag
dependencies:
  - id: vector_store
  - id: embedding
  - id: llm
resources:
  cpu: 1
  memory: 4294967296 # 4 GiB in bytes
  accelerators:
    ibm.com/spyre_pf: 1
  storage: 1073741824 # 1 GiB in bytes
```

**Component Provider Metadata (`metadata.yaml`):**

```yaml
id: opensearch
name: OpenSearch
description: OpenSearch vector database
type: component
component_type: vector_store
component_name: Vector Store
resources:
  cpu: 1
  memory: 4096
  accelerators:
    ibm.com/spyre_pf: 1
  storage: 1024
```

**Metadata Location:**

- Service metadata: `catalog/services/{service_id}/metadata.yaml`
- Component provider metadata: `catalog/components/{component_type}/{provider_id}/metadata.yaml`

**Loading Process:**

1. Catalog provider reads metadata files during initialization
2. Resource specifications are parsed from YAML
3. Resources are included in deploy options API responses
4. Missing `resources` field results in omission from API response

### 9.6 Use Cases

#### Use Case 1: Pre-Deployment Resource Validation

Before deploying a service, validate that the system has sufficient resources:

```bash
# Get system resources
curl -H "Authorization: Bearer $TOKEN" \
  http://localhost:8080/api/v1/resources

# Get service deploy options with resource requirements
curl -H "Authorization: Bearer $TOKEN" \
  http://localhost:8080/api/v1/services/chat/deploy-options

# Compare available vs. required resources
# Deploy only if: available >= required
```

#### Use Case 2: Deployed Application Resource Monitoring

Check resource requirements for an already deployed application:

```bash
# Get system resources
curl -H "Authorization: Bearer $TOKEN" \
  http://localhost:8080/api/v1/resources

# Get deployed application details with resource requirements
curl -H "Authorization: Bearer $TOKEN" \
  http://localhost:8080/api/v1/applications/my-chat-app

# Response includes resource specifications for the deployed application
# Useful for capacity planning, scaling decisions, and resource optimization
```

**Example Application Response with Resources:**

```json
{
  "id": "my-chat-app",
  "name": "My Chat Application",
  "service_id": "chat",
  "status": "running",
  "resources": {
    "cpu": 8,
    "memory": 34359738368,
    "accelerators": {
      "ibm.com/spyre_pf": 5
    },
    "storage": 8589934592
  },
  "components": [
    {
      "type": "vector_store",
      "provider": "opensearch",
      "resources": {
        "cpu": 1,
        "memory": 4096,
        "accelerators": {
          "ibm.com/spyre_pf": 1
        },
        "storage": 1024
      }
    }
  ]
}
```

This integration enables intelligent deployment decisions based on real-time resource availability and service requirements, as well as ongoing monitoring and optimization of deployed applications.
