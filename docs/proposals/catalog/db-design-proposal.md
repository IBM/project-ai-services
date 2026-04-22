# Database Design Proposal for Catalog Service

## Overview

This document outlines the database design required for the Catalog service, including database selection rationale, schema design, and entity relationships.

## Table of Contents

1. [Database Selection](#database-selection)
2. [Database Schema](#database-schema)
3. [Table Definitions](#table-definitions)
   - [Applications Table](#1-applications-table)
   - [Services Table](#2-services-table)
4. [Entity Relationship Model](#entity-relationship-model)
5. [Relationships](#relationships)
6. [Key Design Decisions](#key-design-decisions)
7. [Migration Strategy](#migration-strategy)
8. [Common Queries](#common-queries)
9. [Alternative Design: Separate Infrastructure Tables](#alternative-design-separate-infrastructure-tables)
   - [Overview](#overview-1)
   - [Infrastructure Table](#infrastructure-table)
   - [Services-Infrastructure Junction Table](#services-infrastructure-junction-table)
   - [Comparison: Unified vs Separate Design](#comparison-unified-vs-separate-design)
   - [Advantages of Separate Infrastructure Design](#advantages-of-separate-infrastructure-design-1)
   - [When to Consider Separate Design](#when-to-consider-separate-design)
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

2. **Future Integration**: User management will be handled externally (e.g., via Keycloak as our Identity Provider and Identity Access Management tool). If we adopt Keycloak, we can reuse the same PostgreSQL instance for its data storage needs. This approach avoids maintaining multiple database instances.

3. **ACID Compliance**: PostgreSQL provides strong consistency guarantees essential for catalog management.

4. **Domain-Driven Design**: Infrastructure and Services are distinct bounded contexts with different lifecycles, ownership, and scaling patterns.

## Database Schema

### Database Name

```
ai_service
```

## Table Definitions

### 1. Applications Table

**Table Name:** `applications`

| Column Name         | Data Type         | Constraints | Description |
|---------------------|-------------------|-------------|-------------|
| id                  | VARCHAR(100)      | PRIMARY KEY | Internal application identifier (immutable - used for prefixing pod names in Podman and namespace names in OpenShift) |
| deployment_name     | VARCHAR(100)      |             | Display name of the deployment |
| type                | VARCHAR(100)      |             | Application type (e.g., Digital Assistant, Summarization) |
| deployment_type     | deployment_type   | ENUM        | Type of deployment (Deployable Architecture, Services) |
| status              | Status            | ENUM        | Current status (Downloading, Deploying, Running, Deleting, Error) |
| message             | TEXT              |             | Status message or error details |
| createdby           | VARCHAR(100)      |             | User who created the application |
| created_at          | TIMESTAMPTZ       | DEFAULT NOW() | Timestamp of creation |
| updated_at          | TIMESTAMPTZ       | DEFAULT NOW() | Timestamp of last update |

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

### 2. Services Table

**Table Name:** `services`

| Column Name         | Data Type         | Constraints | Description |
|---------------------|-------------------|-------------|-------------|
| id                  | UUID              | PRIMARY KEY | Unique service identifier |
| app_id              | VARCHAR(100)      | FOREIGN KEY | References applications(id) |
| type                | VARCHAR(100)      |             | Service/Infrastructure type |
| category            | service_category  | ENUM        | Service category (Deployable Service, Infrastructure) |
| status              | Status            | ENUM        | Current status (Deploying, Running, Deleting, Error) |
| endpoints           | JSONB             |             | Array of endpoint objects with name and endpoint fields: `[{"name": "ui", "endpoint": "http://..."}, {"name": "backend", "endpoint": "http://..."}]` |
| version             | TEXT              |             | Service/Infrastructure version |
| created_at          | TIMESTAMPTZ       | DEFAULT NOW() | Timestamp of creation |
| updated_at          | TIMESTAMPTZ       | DEFAULT NOW() | Timestamp of last update |

**Custom Type:**
```sql
CREATE TYPE service_category AS ENUM (
    'Deployable Service',
    'Infrastructure'
);
```

---

## Entity Relationship Model

```
┌──────────────────┐
│  applications    │
├──────────────────┤
│ id (PK)          │
│ deployment_name  │
│ type             │
│ deployment_type  │
│ status           │
│ message          │
│ createdby        │
│ created_at       │
│ updated_at       │
└──────────────────┘
         │
         │ 1:N
         ▼
┌──────────────────┐
│    services      │
├──────────────────┤
│ id (PK)          │
│ app_id (FK)      │
│ type             │
│ category         │
│ status           │
│ endpoints        │
│ version          │
│ created_at       │
│ updated_at       │
└──────────────────┘
```

## Relationships

1. **Applications → Services**: One-to-Many
   - One application can have multiple services
   - Services reference their parent application via app_id
   - Services can be either "Deployable Service" or "Infrastructure" based on category field
   - Both application services and infrastructure components are stored in the same table

## Key Design Decisions

### 1. Natural Primary Key for Applications
The applications table uses `id` as the primary key:
- **Natural Identifier**: id is already unique and immutable
- **Meaningful References**: Foreign keys use id instead of UUID
- **Simpler Queries**: No need to join to get application identifier
- **Consistent Naming**: Used for pod/namespace prefixes in deployments
- **No UUID Overhead**: Eliminates unnecessary UUID generation and storage

### 2. UUID Primary Keys for Services
Services table uses UUID as primary key for:
- Global uniqueness
- Better distribution in distributed systems
- Security (non-sequential IDs)

### 3. Custom Types
PostgreSQL custom types (ENUM) are used for:
- **deployment_type**: Ensures only valid deployment types for applications
- **status**: Standardizes status values across tables (includes Deleting for cleanup workflows)
- **service_category**: Distinguishes between "Deployable Service" and "Infrastructure"

### 4. Application Type Field
The type field in applications table stores:
- **Application Type**: Digital Assistant, Summarization, etc.
- **Direct Classification**: No separate architectures table needed
- **Simpler Schema**: Reduces table count
- **Clear Semantics**: Type directly describes what the application does

### 5. Unified Services Table
Services and infrastructure are stored in a single table with category field:
- **Simplified Schema**: Only 2 tables (applications, services) instead of 4
- **Flexible Design**: Easy to add new service categories
- **Consistent Interface**: Same structure for all service types
- **Category-based Filtering**: Use category field to distinguish service types
- **Simpler Queries**: No need for complex joins across multiple tables

### 6. Consistent Field Sizing
- VARCHAR(100) for type fields across applications and services tables
- Provides sufficient length for descriptive type names
- Consistent sizing across similar fields

### 7. Timestamps
All tables include `created_at` and `updated_at` with `TIMESTAMPTZ` for:
- Complete audit trail
- Time-zone aware timestamps
- Automatic timestamp generation and updates
- Tracking both creation and modification times

### 10. Immutable Primary Key
The `id` field serves as both identifier and primary key:
- Immutable to ensure consistent pod naming in Podman
- Stable namespace naming in OpenShift
- Natural referential integrity in deployed resources
- Prevents accidental renames that would break deployments

## Migration Strategy

1. Create custom types first:
   - deployment_type
   - status
   - service_category

2. Create tables in dependency order:
   - applications (with id as PK)
   - services (with app_id FK and category field)

3. Add indexes for:
   - Foreign keys (app_id in services)
   - Frequently queried columns (type, status, category)
   - id is already indexed as primary key

4. Set up appropriate constraints and triggers for:
   - Automatic updated_at timestamp updates
   - Cascading deletes where appropriate
   - Check constraints for valid data

## Common Queries

### 1. Get all applications:
```sql
SELECT * FROM applications ORDER BY created_at DESC;
```

### 2. Get application with all services (deployable):
```sql
SELECT
    a.*,
    s.id as service_id,
    s.type as service_type,
    s.category as service_category,
    s.status as service_status,
    s.endpoints as service_endpoints,
    s.version as service_version
FROM applications a
LEFT JOIN services s ON a.id = s.app_id
WHERE a.id = 'my-app'
ORDER BY s.category, s.created_at;
```

### 3. Get all deployable services for an application:
```sql
SELECT * FROM services
WHERE app_id = 'my-app' AND category = 'Deployable Service'
ORDER BY created_at;
```

### 4. Get all infrastructure for an application:
```sql
SELECT * FROM services
WHERE app_id = 'my-app' AND category = 'Infrastructure'
ORDER BY created_at;
```

### 5. Get application by id (direct lookup):
```sql
SELECT * FROM applications WHERE id = 'my-app';
```

### 6. Get applications by type:
```sql
SELECT * FROM applications WHERE type = 'Digital Assistant';
```

### 7. Get all services by category:
```sql
SELECT * FROM services WHERE category = 'Infrastructure' ORDER BY created_at DESC;
```

## Alternative Design: Separate Infrastructure Tables

### Overview
This alternative approach separates services and infrastructure into distinct tables with a many-to-many relationship via a junction table. While this provides stronger separation of concerns and infrastructure reusability, it adds complexity compared to the recommended unified services table design.

### Infrastructure Table

**Table Name:** `infra`

| Column Name | Data Type    | Constraints | Description |
|-------------|--------------|-------------|-------------|
| id          | UUID         | PRIMARY KEY | Unique infrastructure identifier |
| status      | Status       | ENUM        | Current status (Deploying, Running, Deleting, Error) |
| type        | VARCHAR(100) |             | Infrastructure type (e.g., vector store, inference backend) |
| endpoints   | TEXT[]       |             | Array of infrastructure endpoints/URLs |
| version     | TEXT         |             | Infrastructure version |
| created_at  | TIMESTAMPTZ  | DEFAULT NOW() | Timestamp of creation |
| updated_at  | TIMESTAMPTZ  | DEFAULT NOW() | Timestamp of last update |

### Services-Infrastructure Junction Table

**Table Name:** `services_infra`

| Column Name | Data Type | Constraints | Description |
|-------------|-----------|-------------|-------------|
| service_id  | UUID      | PRIMARY KEY, FOREIGN KEY | References services(id) |
| infra_id    | UUID      | PRIMARY KEY, FOREIGN KEY | References infra(id) |

**Note:** This is a many-to-many relationship table with a composite primary key.

### Services Table (Modified for Separate Design)

**Table Name:** `services`

| Column Name     | Data Type    | Constraints | Description |
|-----------------|--------------|-------------|-------------|
| id              | UUID         | PRIMARY KEY | Unique service identifier |
| app_id          | VARCHAR(100) | FOREIGN KEY | References applications(id) |
| type            | VARCHAR(100) |             | Service type (e.g., Summarization, Digitization) |
| endpoints       | TEXT[]       |             | Array of service endpoints/URLs |
| version         | TEXT         |             | Service version |
| created_at      | TIMESTAMPTZ  | DEFAULT NOW() | Timestamp of creation |
| updated_at      | TIMESTAMPTZ  | DEFAULT NOW() | Timestamp of last update |

### Comparison: Unified vs Separate Design

| Aspect | Unified Services (Recommended) | Separate Infrastructure (Alternative) |
|--------|--------------------------------|--------------------------------------|
| **Schema Complexity** | ✅ 2 tables (applications, services) | ⚠️ 4 tables (applications, services, infra, services_infra) |
| **Type Safety** | ✅ ENUM-based category field | ✅ Strong - Foreign keys enforce relationships |
| **Data Integrity** | ✅ Single table, simpler validation | ✅ Database-level referential integrity |
| **Query Performance** | ✅ Simple queries, no joins needed | ⚠️ Requires joins across multiple tables |
| **Lifecycle Management** | ✅ Unified lifecycle for all service types | ✅ Independent infrastructure lifecycle |
| **Infrastructure Reusability** | ⚠️ Requires application-level logic | ✅ Explicit many-to-many via junction table |
| **Separation of Concerns** | ⚠️ Mixed in single table | ✅ Clear domain boundaries |
| **Schema Evolution** | ✅ Easy to add new categories | ⚠️ Requires migrations for new tables |
| **Orphaned Records** | ✅ Simpler to manage | ✅ Prevented by foreign keys |
| **Finding Services by Type** | ✅ Simple WHERE category clause | ⚠️ Requires filtering across tables |
| **Dependency Queries** | ✅ Simple category-based queries | ⚠️ Complex joins via junction table |
| **Infrastructure Catalog** | ⚠️ Requires filtering by category | ✅ Easy to build separate catalog |
| **Backup/Restore** | ✅ Single table backup | ⚠️ Must backup multiple related tables |
| **Access Control** | ⚠️ Same permissions for all service types | ✅ Can set different permissions per table |
| **Monitoring** | ⚠️ Requires category-based filtering | ✅ Separate metrics for services vs infra |

### Advantages of Separate Infrastructure Design
#### 1. **Infrastructure Reusability**
The many-to-many relationship via junction table enables:
- Multiple services can share the same infrastructure (e.g., multiple services using one vector store)
- Reduces infrastructure provisioning costs
- Better resource utilization

**Example:**
```
Service A (Summarization) ─┐
Service B (Digitization)  ├─→ Shared Vector DB Instance
Service C (Chat Bot)      ─┘
```

#### 2. **Strong Referential Integrity**
- Foreign keys enforce valid relationships at database level
- Prevents orphaned records automatically
- Cascading deletes can be configured for cleanup

#### 3. **Independent Lifecycle Management**
- Infrastructure can exist independently of services
- Easier to manage infrastructure upgrades and maintenance
- Can delete services without affecting shared infrastructure

#### 4. **Clear Separation of Concerns**
- Services and infrastructure are distinct domain entities
- Different teams can manage services vs infrastructure
- Easier to implement role-based access control

#### 5. **Enterprise Features**
Enables advanced enterprise requirements:
- **Infrastructure Catalog**: Browse and select from approved infrastructure
- **Cost Allocation**: Track which services use which infrastructure
- **Compliance**: Ensure services use certified/approved infrastructure
- **Governance**: Control which infrastructure can be used

### When to Consider Separate Design

The separate infrastructure design is beneficial when:
- Infrastructure needs to be shared across multiple services
- Infrastructure has a different lifecycle than services
- Need strong referential integrity and data consistency
- Enterprise features like infrastructure catalog are required
- Team size allows managing additional schema complexity

### Recommendation

**Use the unified services table design** (recommended in this proposal) because:
1. Simpler schema with only 2 tables
2. Easier to query and maintain
3. Sufficient for most use cases where infrastructure sharing is not critical
4. Category-based filtering provides adequate separation
5. Lower complexity for development and operations

The separate infrastructure design should be considered only when infrastructure reusability and independent lifecycle management are critical requirements that justify the additional schema complexity.

## Future Considerations

1. **User Management**: User authentication and authorization will be handled externally via Keycloak or similar identity management systems
2. **Audit Logging**: Consider adding `updated_at` and `updated_by` columns
3. **Soft Deletes**: May add `deleted_at` column for soft delete functionality
4. **Indexing Strategy**: Create indexes based on query patterns as they emerge
5. **Partitioning**: Consider table partitioning for large-scale deployments
7. **Dependency Validation**: Add application-level validation for infrastructure dependencies
8. **Infrastructure Versioning**: Track infrastructure version compatibility with services

## Conclusion

This database design provides a solid foundation for the Catalog service with:
- Simple and maintainable schema with 2 tables (applications, services)
- Unified services table with category-based distinction between deployable services and infrastructure
- User management handled externally (e.g., via Keycloak)
- Strong data integrity through foreign key constraints and ENUM types
- Efficient querying capabilities with proper indexing
- Flexibility to add new service categories without schema changes
- Clear application-to-services relationship (one-to-many)
- Alternative separate infrastructure design available for advanced use cases requiring infrastructure reusability
