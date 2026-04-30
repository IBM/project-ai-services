# Database Design Proposal for Catalog Service

## Overview

This document outlines the database design required for the Catalog service, including database selection rationale, schema design, and entity relationships.

## Table of Contents

1. [Database Selection](#database-selection)
2. [Database Schema](#database-schema)
3. [Table Definitions](#table-definitions)
   - [Applications Table](#1-applications-table)
   - [Services Table](#2-services-table)
   - [Service Dependencies Table](#3-service-dependencies-table)
   - [Tokens Blacklist Table](#4-tokens-blacklist-table)
4. [Entity Relationship Model](#entity-relationship-model)
5. [Relationships](#relationships)
6. [Key Design Decisions](#key-design-decisions)
7. [Migration Strategy](#migration-strategy)
8. [Common Queries](#common-queries)
9. [Future Considerations](#future-considerations)
10. [Security Considerations](#security-considerations)
11. [Conclusion](#conclusion)

## Database Selection

### Considerations

We evaluated the following database options:

- **PostgreSQL** - Relational Database
- **MongoDB** - NoSQL Document-based Database  
- **Redis** - In-memory cache (primarily for caching frequent data, can be used for persistence but not recommended as best practice)

### Decision: PostgreSQL

We have chosen **PostgreSQL** as our database for the following reasons:

1. **Relational Model Fit**: The Catalog service has clear relationships between Applications, Services, and Service Dependencies, which perfectly models the relational SQL structure with tables.

2. **Future Integration**: User management will be handled externally (e.g., via Keycloak as our Identity Provider and Identity Access Management tool). If we adopt Keycloak, we can reuse the same PostgreSQL instance for its data storage needs. This approach avoids maintaining multiple database instances.

3. **ACID Compliance**: PostgreSQL provides strong consistency guarantees essential for catalog management.

4. **Domain-Driven Design**: Services can have dependencies on other services, requiring a clear relationship model to track these dependencies.

## Database Schema

### Database Name

```
ai_services
```

## Table Definitions

### 1. Applications Table

**Table Name:** `applications`

| Column Name         | Data Type         | Constraints | Description |
|---------------------|-------------------|-------------|-------------|
| id                  | UUID              | PRIMARY KEY | Unique application identifier |
| name                | VARCHAR(100)      |             | Application name |
| template            | VARCHAR(100)      |             | Architecture/service template ID (e.g., rag, summarize, digitize) |
| status              | Status            | ENUM        | Current status (Downloading, Deploying, Running, Deleting, Error) |
| message             | TEXT              |             | Status message or error details |
| created_by          | VARCHAR(100)      |             | User who created the application |
| created_at          | TIMESTAMPTZ       | DEFAULT NOW() | Timestamp of creation |
| updated_at          | TIMESTAMPTZ       | DEFAULT NOW() | Timestamp of last update |

**Custom Types:**

```sql
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
| app_id              | UUID              | FOREIGN KEY | References applications(id) |
| type                | VARCHAR(100)      |             | Service type (e.g., Summarization, Digitization, Vector Store, Inference Backend) |
| status              | Status            | ENUM        | Current status (Deploying, Running, Deleting, Error) |
| endpoints           | JSONB             |             | Array of endpoint objects with name and endpoint fields: `[{"name": "ui", "endpoint": "http://..."}, {"name": "backend", "endpoint": "http://..."}]` |
| version             | TEXT              |             | Service version |
| created_at          | TIMESTAMPTZ       | DEFAULT NOW() | Timestamp of creation |
| updated_at          | TIMESTAMPTZ       | DEFAULT NOW() | Timestamp of last update |

---

### 3. Service Dependencies Table (Taking it up at the end of Q2)

**Table Name:** `service_dependencies`

This table tracks which services depend on (use) other services, enabling a many-to-many relationship between services.

| Column Name         | Data Type         | Constraints | Description |
|---------------------|-------------------|-------------|-------------|
| consumer_service_id | UUID              | PRIMARY KEY, FOREIGN KEY | References services(id) - The service that uses another service |
| provider_service_id | UUID              | PRIMARY KEY, FOREIGN KEY | References services(id) - The service being used |

**Composite Primary Key:** (consumer_service_id, provider_service_id)

**Foreign Key Constraints:**
- consumer_service_id references services(id) ON DELETE CASCADE
- provider_service_id references services(id) ON DELETE CASCADE

**Example Usage:**
```
Summarization Service (consumer) → Vector Store Service (provider)
Chat Bot Service (consumer) → Inference Backend Service (provider)
Digitization Service (consumer) → Vector Store Service (provider)
```

### 4. Tokens Blacklist Table

**Table Name:** `tokens_blacklist`

This table manages blacklisted tokens for both access and refresh tokens. Tokens are stored as SHA-256 hashes for security and are added to the blacklist on logout or revocation.

| Column Name         | Data Type         | Constraints | Description |
|---------------------|-------------------|-------------|-------------|
| id                  | SERIAL            | PRIMARY KEY | Auto-incrementing unique identifier |
| token_hash          | VARCHAR(64)       | NOT NULL, UNIQUE | SHA-256 hash of the JWT token (hex-encoded, 64 characters) |
| token_type          | TokenType         | ENUM, NOT NULL | Token type: "access" or "refresh" |
| expires_at          | TIMESTAMPTZ       | NOT NULL    | Token expiry timestamp |

**Custom Types:**

```sql
CREATE TYPE token_type AS ENUM (
    'access',
    'refresh'
);
```

**Indexes:**
- Primary key index on `id`
- Unique index on `token_hash` for fast lookup

**Security Note:**
- Tokens are hashed using SHA-256 before storage
- Only the hash is stored, not the actual token
- When checking a token, hash it first then query the database
- This prevents token extraction even if the database is compromised

---

## Entity Relationship Model

```
┌──────────────────┐
│  applications    │
├──────────────────┤
│ id (PK)          │
│ name             │
│ template         │
│ status           │
│ message          │
│ created_by       │
│ created_at       │
│ updated_at       │
└──────────────────┘
         │
         │ 1:N
         ▼
┌──────────────────┐              ┌─────────────────────────┐
│    services      │              │ service_dependencies    │
├──────────────────┤              ├─────────────────────────┤
│ id (PK)          │◄─────────────┤ consumer_service_id (FK)│
│ app_id (FK)      │              │ provider_service_id (FK)│
│ type             │◄─────────────┤                         │
│ status           │              └─────────────────────────┘
│ endpoints        │                        │
│ version          │                        │ M:N
│ created_at       │                        │
│ updated_at       │                        │
└──────────────────┘◄───────────────────────┘


┌──────────────────┐
│tokens_blacklist  │
├──────────────────┤
│ id (PK)          │
│ token_hash       │
│ token_type       │
│ expires_at       │
└──────────────────┘
```

## Relationships

1. **Applications → Services**: One-to-Many
   - One application can have multiple services
   - Services reference their parent application via app_id
   - All services (deployable services and infrastructure components) are stored in the same table

2. **Services → Services**: Many-to-Many (via service_dependencies)
   - Services can depend on other services
   - The service_dependencies table tracks which service uses which service
   - consumer_service_id: The service that requires/uses another service
   - provider_service_id: The service being used/depended upon
   - Enables tracking of service relationships and dependencies
   - Supports scenarios like: multiple services sharing a vector store, or a service using multiple backend services

3. **Tokens Blacklist**: Independent table
   - Stores revoked tokens for authentication middleware
   - Self-contained for security and performance
   - Tokens are automatically cleaned up after expiry

## Key Design Decisions

### 1. UUID Primary Key for Applications
The applications table uses UUID as the primary key:
- **Global Uniqueness**: Ensures unique identifiers across distributed systems
- **Security**: Non-sequential IDs prevent enumeration attacks
- **Immutable Identifier**: UUID remains constant throughout application lifecycle
- **Consistent with Services**: Aligns with services table design
- **Standard Practice**: Follows industry best practices for distributed systems

### 2. UUID Primary Keys for Services
Services table uses UUID as primary key for:
- Global uniqueness
- Better distribution in distributed systems
- Security (non-sequential IDs)

### 3. Custom Types
PostgreSQL custom types (ENUM) are used for:
- **status**: Standardizes status values across tables (includes Deleting for cleanup workflows)

### 4. Application Template Field
The template field in applications table stores:
- **Architecture/Service ID**: Stores the identifier of the architecture or service template (e.g., rag, summarize, digitize)
- **Direct Reference**: Template corresponds to the ID of the architecture/service being deployed
- **Simpler Schema**: Single template field replaces type and deployment_type columns
- **Clear Identification**: Template directly identifies which architecture or service is being deployed

### 5. Tokens Blacklist Table
The tokens_blacklist table provides token revocation management with enhanced security:
- **Database-backed**: Replaces in-memory implementation for multi-instance support
- **SERIAL Primary Key**: Auto-incrementing integer for simplicity and performance
- **Hashed Storage**: Tokens stored as SHA-256 hashes (64-character hex strings)
- **Unique Hash Constraint**: Ensures no duplicate entries and enables fast lookups
- **Blacklist Approach**: Stores revoked tokens for both access and refresh token types
- **Token Type Enum**: Uses PostgreSQL ENUM type with values 'access' and 'refresh'
- **Automatic Expiry**: Tokens stored only until their natural expiry time
- **Minimal Design**: Only essential fields for token validation
- **Security**: Even if database is compromised, actual tokens cannot be extracted

### 6. Unified Services Table
All services (including infrastructure components like vector stores, databases, inference backends) are stored in a single table:
- **Simplified Schema**: 4 tables (applications, services, service_dependencies, tokens)
- **Flexible Design**: Easy to add new service types
- **Consistent Interface**: Same structure for all service types
- **Type-based Filtering**: Use type field to distinguish service types
- **Explicit Dependency Tracking**: Separate service_dependencies table tracks service relationships

### 7. Service Dependencies Table
The service_dependencies table provides explicit many-to-many relationship tracking:
- **Minimal Design**: Only 2 columns (consumer_service_id, provider_service_id)
- **Composite Primary Key**: Ensures unique service-to-service relationships
- **Cascade Deletes**: Automatically cleans up dependencies when services are deleted
- **Clear Semantics**: Consumer (service that uses) and Provider (service being used)
- **No Metadata**: Intentionally minimal - additional fields can be added later if needed
- **Bidirectional Queries**: Easy to find both dependencies and dependents

### 8. UUID Consistency
- UUID used for all primary keys (applications, services)
- UUID used for all foreign key references (app_id, user_id)
- Provides global uniqueness and security across the system
- Consistent data type for identifiers throughout the schema

### 9. Timestamps
Applications and services tables include `created_at` and `updated_at` with `TIMESTAMPTZ` for:
- Complete audit trail
- Time-zone aware timestamps
- Automatic timestamp generation and updates
- Tracking both creation and modification times
- Note: service_dependencies and tokens_blacklist tables intentionally exclude timestamps for minimal design

### 10. Immutable UUID Primary Key
The `id` field (UUID) serves as the primary key:
- Immutable to ensure consistent references across the system
- UUID provides stable, globally unique identifiers
- Prevents accidental ID collisions in distributed deployments
- Maintains referential integrity across all related tables

## Common Queries

### 1. Get all applications:
```sql
SELECT * FROM applications ORDER BY created_at DESC;
```

### 2. Get application with all services:
```sql
SELECT
    a.*,
    s.id as service_id,
    s.type as service_type,
    s.status as service_status,
    s.endpoints as service_endpoints,
    s.version as service_version
FROM applications a
LEFT JOIN services s ON a.id = s.app_id
WHERE a.id = 'application-uuid-here'
ORDER BY s.created_at;
```

### 3. Get all services for an application:
```sql
SELECT * FROM services
WHERE app_id = 'application-uuid-here'
ORDER BY created_at;
```

### 4. Get all dependencies for a specific service:
```sql
SELECT s.*
FROM services s
JOIN service_dependencies sd ON s.id = sd.provider_service_id
WHERE sd.consumer_service_id = 'service-uuid-here';
```

### 5. Get all services that depend on a specific service:
```sql
SELECT s.*
FROM services s
JOIN service_dependencies sd ON s.id = sd.consumer_service_id
WHERE sd.provider_service_id = 'provider-service-uuid-here';
```

### 6. Get complete dependency graph for an application:
```sql
SELECT
    a.id as app_id,
    a.name,
    consumer.id as consumer_service_id,
    consumer.type as consumer_service_type,
    provider.id as provider_service_id,
    provider.type as provider_service_type
FROM applications a
JOIN services consumer ON a.id = consumer.app_id
LEFT JOIN service_dependencies sd ON consumer.id = sd.consumer_service_id
LEFT JOIN services provider ON sd.provider_service_id = provider.id
WHERE a.id = 'application-uuid-here'
ORDER BY consumer.type, provider.type;
```

### 7. Find shared services (services used by multiple consumers):
```sql
SELECT
    provider.id,
    provider.type,
    provider.status,
    COUNT(DISTINCT sd.consumer_service_id) as consumer_count
FROM services provider
JOIN service_dependencies sd ON provider.id = sd.provider_service_id
GROUP BY provider.id, provider.type, provider.status
HAVING COUNT(DISTINCT sd.consumer_service_id) > 1
ORDER BY consumer_count DESC;
```

### 8. Get application by id (direct lookup):
```sql
SELECT * FROM applications WHERE id = 'application-uuid-here';
```

### 9. Get applications by template:
```sql
SELECT * FROM applications WHERE template = 'rag';
```

### 10. Get all services by type:
```sql
SELECT * FROM services WHERE type = 'Vector Store' ORDER BY created_at DESC;
```

### 11. Check if a service has any dependencies:
```sql
SELECT EXISTS(
    SELECT 1 FROM service_dependencies
    WHERE consumer_service_id = 'service-uuid-here'
) as has_dependencies;
```

## Future Considerations

1. **User Management**: User authentication and authorization will be handled externally via Keycloak or similar identity management systems
2. **Audit Logging**: Consider adding `updated_at` and `updated_by` columns
3. **Soft Deletes**: May add `deleted_at` column for soft delete functionality
4. **Indexing Strategy**: Create indexes based on query patterns as they emerge
5. **Partitioning**: Consider table partitioning for large-scale deployments
6. **Dependency Validation**: Add application-level validation for service dependencies to prevent circular dependencies
7. **Service Versioning**: Track service version compatibility with dependent services
8. **Dependency Metadata**: Consider adding metadata to service_dependencies table (e.g., required vs optional, version constraints)

## Conclusion

This database design provides a solid foundation for the Catalog service with:
- Simple and maintainable schema with 4 tables (applications, services, service_dependencies, tokens_blacklist)
- Unified services table storing all service types (deployable services and infrastructure components)
- Explicit service dependency tracking through service_dependencies junction table
- Support for many-to-many service relationships (services can depend on multiple services, and be used by multiple services)
- User management handled externally (e.g., via Keycloak)
- Strong data integrity through foreign key constraints and ENUM types
- Efficient querying capabilities with proper indexing
- Flexibility to add new service types without schema changes
- Clear application-to-services relationship (one-to-many)
- Clear service-to-service dependency tracking (many-to-many)
- Enables tracking of shared services and dependency graphs
- Tokens blacklist table for revoked access and refresh tokens
- Tokens stored as SHA-256 hashes for enhanced security
- Database-backed token management replacing in-memory implementation
- Minimal token blacklist design with only essential fields
