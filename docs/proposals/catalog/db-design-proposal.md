# Database Design Proposal for Catalog Service

## Overview

This document outlines the database design required for the Catalog service, including database selection rationale, schema design, and entity relationships.

## Table of Contents

1. [Database Selection](#database-selection)
2. [Database Schema](#database-schema)
3. [Table Definitions](#table-definitions)
   - [Users Table](#1-users-table)
   - [Applications Table](#2-applications-table)
   - [Architectures Table](#3-architectures-table)
   - [Services Table](#4-services-table)
   - [Infrastructure Table](#5-infrastructure-table)
   - [Services-Infrastructure Junction Table](#6-services-infrastructure-junction-table)
4. [Entity Relationship Model](#entity-relationship-model)
5. [Relationships](#relationships)
6. [Key Design Decisions](#key-design-decisions)
7. [Migration Strategy](#migration-strategy)
8. [Common Queries](#common-queries)
9. [Alternative Design: Unified Services Table](#alternative-design-unified-services-table)
   - [Overview](#overview-1)
   - [Alternative Services Table](#alternative-services-table)
   - [Comparison: Separate vs Unified Design](#comparison-separate-vs-unified-design)
   - [Limitations of Unified Design](#limitations-of-unified-design)
   - [When to Consider Unified Design](#when-to-consider-unified-design)
   - [Recommendation](#recommendation)
10. [Future Considerations](#future-considerations)
11. [Security Considerations](#security-considerations)
12. [Advantages of Separate Infrastructure Design](#advantages-of-separate-infrastructure-design)
13. [Conclusion](#conclusion)

## Database Selection

### Considerations

We evaluated the following database options:

- **PostgreSQL** - Relational Database
- **MongoDB** - NoSQL Document-based Database  
- **Redis** - In-memory cache (primarily for caching frequent data, can be used for persistence but not recommended as best practice)

### Decision: PostgreSQL

We have chosen **PostgreSQL** as our database for the following reasons:

1. **Relational Model Fit**: The Catalog service has clear relationships between Deployable Architectures, Services, and Infrastructure components, which perfectly models the relational SQL structure with tables.

2. **Future Integration**: In upcoming releases, if we adopt Keycloak as our Identity Provider and Identity Access Management tool, we can reuse the same PostgreSQL instance. This approach avoids maintaining multiple database instances.

3. **ACID Compliance**: PostgreSQL provides strong consistency guarantees essential for catalog management.

4. **Domain-Driven Design**: Infrastructure and Services are distinct bounded contexts with different lifecycles, ownership, and scaling patterns.

## Database Schema

### Database Name

```
ai_service
```

## Table Definitions

### 1. Users Table

**Table Name:** `users`

| Column Name | Data Type    | Constraints | Description |
|-------------|--------------|-------------|-------------|
| id          | UUID         | PRIMARY KEY | Unique user identifier |
| username    | VARCHAR(100) |             | User's username |
| password    | TEXT         |             | Encrypted password |

---

### 2. Applications Table

**Table Name:** `applications`

| Column Name       | Data Type         | Constraints | Description |
|-------------------|-------------------|-------------|-------------|
| id                | UUID              | PRIMARY KEY | Unique application identifier |
| name              | VARCHAR(100)      |             | Display name of the application |
| app_name          | VARCHAR(100)      | UNIQUE      | Internal application name (immutable - used for prefixing pod names in Podman and namespace names in OpenShift) |
| deployment_type   | deployment_type   | ENUM        | Type of deployment (Deployable Architecture, Services) |
| status            | Status            | ENUM        | Current status (Downloading, Deploying, Running, Deleting, Error) |
| message           | TEXT              |             | Status message or error details |
| created_at        | TIMESTAMPTZ       | DEFAULT NOW() | Timestamp of creation |
| updated_at        | TIMESTAMPTZ       | DEFAULT NOW() | Timestamp of last update |

**Custom Types:**

```sql
CREATE TYPE deployment_type AS ENUM (
    'Deployable Architecture',
    'Services'
);

CREATE TYPE status AS ENUM (
    'Downloading',
    'Deploying',
    'Running',
    'Deleting',
    'Error'
);
```

---

### 3. Architectures Table

**Table Name:** `architectures`

| Column Name     | Data Type       | Constraints | Description |
|-----------------|-----------------|-------------|-------------|
| id              | UUID            | PRIMARY KEY | Unique architecture identifier |
| application_id  | UUID            | FOREIGN KEY | References applications(id) |
| type            | VARCHAR(100)    |             | Architecture type (e.g., Digital Assistant, Deep Process Integration) |
| resources       | resource_config |             | Resource configuration |
| created_at      | TIMESTAMPTZ     | DEFAULT NOW() | Timestamp of creation |
| updated_at      | TIMESTAMPTZ     | DEFAULT NOW() | Timestamp of last update |

**Custom Type:**

```sql
CREATE TYPE resource_config AS (
    cores text,
    memory text,
    storage text,
    spyre_io text
);
```

---

### 4. Services Table

**Table Name:** `services`

| Column Name     | Data Type       | Constraints | Description |
|-----------------|-----------------|-------------|-------------|
| id              | UUID            | PRIMARY KEY | Unique service identifier |
| application_id  | UUID            | FOREIGN KEY | References applications(id) |
| architecture_id | UUID            | FOREIGN KEY | References architectures(id) |
| type            | VARCHAR(100)    |             | Service type (e.g., Summarization, Digitization) |
| endpoints       | TEXT[]          |             | Array of service endpoints/URLs |
| resources       | resource_config |             | Resource configuration |
| version         | TEXT            |             | Service version |
| created_at      | TIMESTAMPTZ     | DEFAULT NOW() | Timestamp of creation |
| updated_at      | TIMESTAMPTZ     | DEFAULT NOW() | Timestamp of last update |

---

### 5. Infrastructure Table

**Table Name:** `infra`

| Column Name | Data Type       | Constraints | Description |
|-------------|-----------------|-------------|-------------|
| id          | UUID            | PRIMARY KEY | Unique infrastructure identifier |
| status      | Status          | ENUM        | Current status (Deploying, Running, Deleting, Error) |
| type        | VARCHAR(100)    |             | Infrastructure type (e.g., vector store, inference backend) |
| endpoints   | TEXT[]          |             | Array of infrastructure endpoints/URLs |
| resources   | resource_config |             | Resource configuration |
| version     | TEXT            |             | Infrastructure version |
| created_at  | TIMESTAMPTZ     | DEFAULT NOW() | Timestamp of creation |
| updated_at  | TIMESTAMPTZ     | DEFAULT NOW() | Timestamp of last update |

---

### 6. Services-Infrastructure Junction Table

**Table Name:** `services_infra`

| Column Name | Data Type | Constraints | Description |
|-------------|-----------|-------------|-------------|
| service_id  | UUID      | PRIMARY KEY, FOREIGN KEY | References services(id) |
| infra_id    | UUID      | PRIMARY KEY, FOREIGN KEY | References infra(id) |

**Note:** This is a many-to-many relationship table with a composite primary key.

---

## Entity Relationship Model

```
┌─────────────┐
│   users     │
└─────────────┘

┌──────────────────┐
│  applications    │
├──────────────────┤
│ id (PK)          │
│ name             │
│ app_name         │
│ deployment_type  │
│ status           │
│ message          │
│ created_at       │
└──────────────────┘
         │
         │ 1:N
         ▼
┌──────────────────┐
│  architectures   │
├──────────────────┤
│ id (PK)          │
│ application_id(FK)│
│ type             │
│ resources        │
│ created_at       │
└──────────────────┘
         │
         │ 1:N
         ▼
┌──────────────────┐
│    services      │
├──────────────────┤
│ id (PK)          │
│ application_id(FK)│
│ architecture_id(FK)│
│ type             │
│ resources        │
│ created_at       │
│ version          │
│ properties       │
└──────────────────┘
         │
         │ M:N
         ▼
┌──────────────────┐
│ services_infra   │
├──────────────────┤
│ service_id (PK,FK)│
│ infra_id (PK,FK) │
└──────────────────┘
         │
         │ M:N
         ▼
┌──────────────────┐
│      infra       │
├──────────────────┤
│ id (PK)          │
│ status           │
│ type             │
│ resources        │
│ created_at       │
│ version          │
│ properties       │
└──────────────────┘
```

## Relationships

1. **Applications → Architectures**: One-to-Many
   - One application can have multiple architectures

2. **Applications → Services**: One-to-Many
   - One application can have multiple services

3. **Architectures → Services**: One-to-Many
   - One architecture can have multiple services

4. **Services ↔ Infrastructure**: Many-to-Many
   - Services can use multiple infrastructure components
   - Infrastructure components can be shared across multiple services
   - Implemented via the `services_infra` junction table

## Key Design Decisions

### 1. UUID Primary Keys
All tables use UUID as primary keys for:
- Global uniqueness
- Better distribution in distributed systems
- Security (non-sequential IDs)

### 2. Custom Types
PostgreSQL custom types (ENUM) are used for:
- **deployment_type**: Ensures only valid deployment types for applications
- **status**: Standardizes status values across tables (includes Deleting for cleanup workflows)
- **resource_config**: Structured resource configuration

### 3. Separate Infrastructure Table
Infrastructure is separated from services for:
- **Clear Separation of Concerns**: Different lifecycles, ownership, and scaling patterns
- **Strong Type Safety**: Foreign keys enforce referential integrity
- **Better Normalization**: Follows 3NF database design principles
- **Infrastructure Reusability**: Explicit many-to-many relationship allows sharing
- **Independent Management**: Infrastructure can be managed, upgraded, and monitored separately

### 4. Many-to-Many Relationship
The `services_infra` junction table enables:
- Multiple services to share the same infrastructure (e.g., multiple services using one vector store)
- Services to depend on multiple infrastructure components
- Clean separation between service and infrastructure lifecycles
- Easy querying of dependencies in both directions

### 5. JSONB for Properties
The `properties` column uses JSONB for:
- Flexible schema for service/infrastructure-specific data
- Efficient querying and indexing
- Storage of endpoints, models, and other dynamic attributes
- No schema migrations needed for new property types

### 6. Consistent Field Sizing
- VARCHAR(100) for type fields in both architectures and services tables
- Provides sufficient length for descriptive type names
- Consistent sizing across similar fields

### 7. Timestamps
All tables include `created_at` with `TIMESTAMPTZ` for:
- Audit trail
- Time-zone aware timestamps
- Automatic timestamp generation

### 8. Immutable Fields
The `app_name` field in the applications table is immutable to ensure:
- Consistent pod naming in Podman
- Stable namespace naming in OpenShift
- Referential integrity in deployed resources

## Migration Strategy

1. Create custom types first:
   - deployment_type
   - status
   - resource_config

2. Create tables in dependency order:
   - users
   - applications
   - architectures
   - services
   - infra
   - services_infra (junction table)

3. Add indexes for:
   - Foreign keys (application_id, architecture_id, service_id, infra_id)
   - Frequently queried columns (type, status)
   - JSONB fields using GIN indexes for efficient querying

4. Set up appropriate constraints and triggers

## Common Queries

### 1. Get all applications:
```sql
SELECT * FROM applications ORDER BY created_at DESC;
```

### 2. Get application with architectures, services, and infrastructure (for Deployable Architecture):
```sql
SELECT
    a.*,
    arch.id as arch_id, arch.type as arch_type, arch.resources as arch_resources,
    s.id as service_id, s.type as service_type, s.version as service_version,
    i.id as infra_id, i.type as infra_type, i.status as infra_status
FROM applications a
LEFT JOIN architectures arch ON a.id = arch.application_id
LEFT JOIN services s ON arch.id = s.architecture_id
LEFT JOIN services_infra si ON s.id = si.service_id
LEFT JOIN infra i ON si.infra_id = i.id
WHERE a.id = 'application-uuid' AND a.deployment_type = 'Deployable Architecture';
```

### 3. Get application with services and infrastructure (for Services deployment type):
```sql
SELECT
    a.*,
    s.id as service_id, s.type as service_type, s.version as service_version,
    i.id as infra_id, i.type as infra_type, i.status as infra_status
FROM applications a
LEFT JOIN services s ON a.id = s.application_id
LEFT JOIN services_infra si ON s.id = si.service_id
LEFT JOIN infra i ON si.infra_id = i.id
WHERE a.id = 'application-uuid' AND a.deployment_type = 'Services';
```

## Alternative Design: Unified Services Table

### Overview
This alternative approach combines the `services`, `infra`, and `services_infra` tables into a single unified `services` table. This section documents this approach and compares its limitations with the recommended separate infrastructure design.

### Alternative Services Table

**Table Name:** `services`

| Column Name         | Data Type         | Constraints | Description |
|---------------------|-------------------|-------------|-------------|
| id                  | UUID              | PRIMARY KEY | Unique service identifier |
| application_id      | UUID              | FOREIGN KEY | References applications(id) |
| architecture_id     | UUID              | FOREIGN KEY | References architectures(id), nullable |
| type                | VARCHAR(100)      |             | Service/Infrastructure type |
| category            | service_category  | ENUM        | Service category (Application Service, Infrastructure) |
| status              | Status            | ENUM        | Current status (Deploying, Running, Deleting, Error) |
| resources           | resource_config   |             | Resource configuration |
| created_at          | TIMESTAMPTZ       | DEFAULT NOW() | Timestamp of creation |
| version             | TEXT              |             | Service/Infrastructure version |
| properties          | JSONB             |             | Additional properties (endpoints, models, credentials) |
| infrastructure_deps | JSONB             |             | Array of infrastructure service dependencies |

**Custom Type:**
```sql
CREATE TYPE service_category AS ENUM (
    'Application Service',
    'Infrastructure'
);
```

**Infrastructure Dependencies JSONB Structure:**
```json
{
  "dependencies": [
    {
      "service_id": "uuid-of-vector-store",
      "type": "vector_store",
      "required": true
    },
    {
      "service_id": "uuid-of-inference-backend",
      "type": "inference_backend",
      "required": true
    }
  ]
}
```

### Comparison: Separate vs Unified Design

| Aspect | Separate Infrastructure (Recommended) | Unified Services (Alternative) |
|--------|--------------------------------------|--------------------------------|
| **Schema Complexity** | 6 tables (more complex) | 4 tables (simpler) |
| **Type Safety** | ✅ Strong - Foreign keys enforce relationships | ⚠️ Weak - JSONB dependencies, no FK enforcement |
| **Data Integrity** | ✅ Database-level referential integrity | ⚠️ Application-level validation required |
| **Query Performance** | ✅ Indexed foreign keys, efficient joins | ⚠️ JSONB queries slower, GIN indexes needed |
| **Lifecycle Management** | ✅ Independent infrastructure lifecycle | ❌ Mixed lifecycle complicates management |
| **Infrastructure Reusability** | ✅ Explicit many-to-many via junction table | ⚠️ Implicit via JSONB, harder to query |
| **Separation of Concerns** | ✅ Clear domain boundaries | ❌ Mixed concerns in single table |
| **Schema Evolution** | ⚠️ Requires migrations for new fields | ✅ JSONB allows flexible schema |
| **Orphaned Records** | ✅ Prevented by foreign keys | ❌ Possible if JSONB references deleted services |
| **Circular Dependencies** | ✅ Can be prevented at DB level | ❌ Must be checked in application code |
| **Finding Shared Infra** | ✅ Simple COUNT query on junction table | ⚠️ Complex JSONB aggregation queries |
| **Dependency Queries** | ✅ Standard SQL joins | ⚠️ JSONB path queries with LATERAL joins |
| **Infrastructure Catalog** | ✅ Easy to build separate catalog | ⚠️ Requires filtering by category |
| **Backup/Restore** | ✅ Can backup infrastructure separately | ⚠️ All-or-nothing approach |
| **Access Control** | ✅ Can set different permissions per table | ⚠️ Same permissions for all service types |
| **Monitoring** | ✅ Separate metrics for services vs infra | ⚠️ Requires category-based filtering |

### Limitations of Unified Design

#### 1. **No Infrastructure Reusability (Critical Limitation)**
The 1:1 relationship via `parent_service_id` means:
- **Each infrastructure can only be bound to ONE service**
- Cannot share a vector database across multiple services
- Cannot share an inference model across multiple applications
- Must provision duplicate infrastructure for each service that needs it

**Real-world Impact:**
```
Service A (Summarization) → Vector DB Instance 1
Service B (Digitization)  → Vector DB Instance 2  (Duplicate!)
Service C (Chat Bot)      → Vector DB Instance 3  (Duplicate!)
```

Instead of:
```
Service A (Summarization) ─┐
Service B (Digitization)  ├─→ Shared Vector DB Instance
Service C (Chat Bot)      ─┘
```

#### 2. **Cannot Leverage Existing Customer Infrastructure**
Many customers already have:
- Existing vector databases (OpenSearch, Pinecone, Weaviate)
- Running inference models/endpoints
- Established ML infrastructure

**Problems:**
- Cannot reference pre-existing infrastructure in the catalog
- Forces customers to provision new infrastructure even when they have suitable ones
- No way to "bring your own infrastructure" (BYOI)
- Increases costs and deployment complexity

#### 3. **Poor UI/UX for Infrastructure Selection**
The 1:1 model prevents building user-friendly interfaces:

**Cannot implement:**
- Dropdown to select from available vector databases
- List of running inference models to choose from
- Infrastructure marketplace/catalog
- "Use existing" vs "Create new" infrastructure options

**UI Flow Limitation:**
```
❌ Cannot do this:
1. User creates a service
2. UI shows: "Select Vector Database"
   - [Existing VDB 1] ← Already running
   - [Existing VDB 2] ← Already running
   - [Create New VDB]
3. User selects existing VDB

✅ Forced to do this:
1. User creates a service
2. System automatically creates new dedicated infrastructure
3. No choice, no reuse
```

#### 4. **Weak Referential Integrity**
```sql
-- In unified design with JSONB dependencies, this can happen:
DELETE FROM services WHERE id = 'infra-uuid';
-- Other services still reference this in their infrastructure_deps JSONB
-- No database error, creates orphaned references
```

#### 5. **Complex Dependency Queries**
```sql
-- Finding all services using an infrastructure requires complex JSONB queries:
SELECT s.*
FROM services s
CROSS JOIN LATERAL jsonb_array_elements(s.infrastructure_deps->'dependencies') AS dep
WHERE (dep->>'service_id')::uuid = 'infra-uuid';

-- vs. simple join in separate design:
SELECT s.* FROM services s
JOIN services_infra si ON s.id = si.service_id
WHERE si.infra_id = 'infra-uuid';
```

#### 6. **Resource Waste and Cost Implications**
- **Duplicate Infrastructure**: Each service gets its own infrastructure
- **Higher Costs**: 10 services = 10 vector DBs instead of 1 shared
- **Resource Inefficiency**: Underutilized infrastructure instances
- **Operational Overhead**: Managing many small instances vs few large ones

**Cost Example:**
```
Separate Design:
- 1 Shared Vector DB: $500/month
- 10 Services using it: $500/month total

Unified Design:
- 10 Dedicated Vector DBs: $500 × 10 = $5,000/month
- 10x cost increase!
```

#### 7. **Mixed Lifecycle Management**
- Infrastructure typically has longer lifecycle than application services
- Upgrades, backups, and monitoring become more complex
- Cannot easily separate infrastructure operations from service operations
- Deleting a service forces deletion of its infrastructure (even if others could use it)

#### 8. **No Database-Level Validation for JSONB Dependencies**
- Cannot enforce that infrastructure_deps references valid service IDs
- Cannot prevent circular dependencies at database level
- Must implement all validation in application code
- Risk of data inconsistency

#### 9. **Scalability Concerns**
- Cannot scale infrastructure independently from services
- Cannot implement infrastructure pooling or load balancing
- Difficult to implement multi-tenancy for infrastructure
- No way to track infrastructure utilization across services

#### 10. **Enterprise Requirements Not Met**
Enterprise customers typically need:
- **Infrastructure Catalog**: Browse and select from approved infrastructure
- **Cost Allocation**: Track which services use which infrastructure
- **Compliance**: Ensure services use certified/approved infrastructure
- **Governance**: Control which infrastructure can be used
- **Chargeback**: Bill teams based on infrastructure usage

The unified design makes these requirements difficult or impossible to implement.

### When to Consider Unified Design

The unified design might be acceptable if:
- Infrastructure is rarely shared across services (1:1 relationship)
- Application is small-scale with limited infrastructure
- Schema flexibility is more important than data integrity
- Team prefers simpler schema over stronger guarantees
- Infrastructure lifecycle matches service lifecycle

### Recommendation

**Use the separate infrastructure design** (recommended in this proposal) because:
1. Infrastructure and services have fundamentally different lifecycles
2. Database-level integrity prevents data corruption
3. Better performance for relationship queries
4. Aligns with industry patterns (Kubernetes, cloud providers)
5. Easier to scale and maintain long-term
6. Supports future features like infrastructure marketplace

The additional complexity of 2 extra tables is justified by the significant benefits in data integrity, performance, and maintainability.

## Future Considerations

1. **Keycloak Integration**: Schema can be extended to integrate with Keycloak's user management
2. **Audit Logging**: Consider adding `updated_at` and `updated_by` columns
3. **Soft Deletes**: May add `deleted_at` column for soft delete functionality
4. **Indexing Strategy**: Create indexes based on query patterns as they emerge
5. **Partitioning**: Consider table partitioning for large-scale deployments
7. **Dependency Validation**: Add application-level validation for infrastructure dependencies
8. **Infrastructure Versioning**: Track infrastructure version compatibility with services

## Conclusion

This database design provides a solid foundation for the Catalog service with:
- Clear relational structure with proper separation of concerns
- Strong data integrity through foreign key constraints
- Efficient querying capabilities with proper indexing
- Scalability for growth and infrastructure sharing
- Alignment with industry best practices and cloud-native patterns
- Flexibility for future enhancements through JSONB properties
- Support for complex service-infrastructure relationships (if we want to seperate it out)
