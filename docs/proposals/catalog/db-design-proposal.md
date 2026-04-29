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
   - [Token Blacklist Table](#4-token-blacklist-table)
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
| deployment_type     | deployment_type   | ENUM        | Type of deployment (architecture, service) |
| status              | Status            | ENUM        | Current status (Downloading, Deploying, Running, Deleting, Error) |
| message             | TEXT              |             | Status message or error details |
| createdby           | VARCHAR(100)      |             | User who created the application |
| created_at          | TIMESTAMPTZ       | DEFAULT NOW() | Timestamp of creation |
| updated_at          | TIMESTAMPTZ       | DEFAULT NOW() | Timestamp of last update |

**Custom Types:**

```sql
CREATE TYPE deployment_type AS ENUM (
    'architecture',
    'service'
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
| type                | VARCHAR(100)      |             | Service type (e.g., Summarization, Digitization, Vector Store, Inference Backend) |
| status              | Status            | ENUM        | Current status (Deploying, Running, Deleting, Error) |
| endpoints           | JSONB             |             | Array of endpoint objects with name and endpoint fields: `[{"name": "ui", "endpoint": "http://..."}, {"name": "backend", "endpoint": "http://..."}]` |
| version             | TEXT              |             | Service version |
| created_at          | TIMESTAMPTZ       | DEFAULT NOW() | Timestamp of creation |
| updated_at          | TIMESTAMPTZ       | DEFAULT NOW() | Timestamp of last update |

---

### 3. Service Dependencies Table

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

### 4. Tokens Table

**Table Name:** `tokens`

This table manages both revoked access tokens (blacklist) and active refresh tokens. Tokens are stored as SHA-256 hashes for security. Access tokens are blacklisted on logout, while refresh tokens are stored as active sessions and deleted on logout or expiry.

| Column Name         | Data Type         | Constraints | Description |
|---------------------|-------------------|-------------|-------------|
| id                  | SERIAL            | PRIMARY KEY | Auto-incrementing unique identifier |
| token_hash          | VARCHAR(64)       | NOT NULL, UNIQUE | SHA-256 hash of the JWT token (hex-encoded, 64 characters) |
| token_type          | VARCHAR(20)       | NOT NULL    | Token type: "access_blacklist" or "refresh_active" |
| user_id             | VARCHAR(100)      | NOT NULL    | User ID associated with the token |
| expires_at          | TIMESTAMPTZ       | NOT NULL    | Token expiry timestamp |
| created_at          | TIMESTAMPTZ       | DEFAULT NOW() | When the token was created/blacklisted |

**Indexes:**
- Primary key index on `id`
- Unique index on `token_hash` for fast lookup

**Token Type Values:**
- `access_blacklist`: Revoked access tokens (blacklist approach)
- `refresh_active`: Active refresh tokens (whitelist approach)

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
|
|
┌──────────────────┐
│ token_blacklist  │
├──────────────────┤
│ id (PK)          │
│ token (UNIQUE)   │
│ token_type       │
│ user_id          │
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

3. **Token Blacklist**: Independent table
   - Stores revoked tokens for authentication middleware
   - Self-contained for security and performance
   - Tokens are automatically cleaned up after expiry

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

### 4. Application Type Field
The type field in applications table stores:
- **Application Type**: Digital Assistant, Summarization, etc.
- **Direct Classification**: No separate architectures table needed
- **Simpler Schema**: Reduces table count
- **Clear Semantics**: Type directly describes what the application does

### 5. Tokens Table
The tokens table provides dual-purpose token management with enhanced security:
- **Database-backed**: Replaces in-memory implementation for multi-instance support
- **SERIAL Primary Key**: Auto-incrementing integer for simplicity and performance
- **Hashed Storage**: Tokens stored as SHA-256 hashes (64-character hex strings)
- **Unique Hash Constraint**: Ensures no duplicate entries and enables fast lookups
- **Dual Purpose Design**:
  - **Access Tokens**: Blacklist approach (token_type='access_blacklist') - revoked tokens stored
  - **Refresh Tokens**: Whitelist approach (token_type='refresh_active') - active refresh tokens stored
- **User Tracking**: Includes user_id for audit and user-specific token management
- **Automatic Expiry**: Tokens stored only until their natural expiry time
- **Created Timestamp**: Tracks when token was created/blacklisted for audit purposes
- **Security**: Even if database is compromised, actual tokens cannot be extracted

### 6. Unified Services Table
All services (including infrastructure components like vector stores, databases, inference backends) are stored in a single table:
- **Simplified Schema**: 4 tables (applications, services, service_dependencies, token_blacklist)
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

### 8. Consistent Field Sizing
- VARCHAR(100) for type fields across applications and services tables
- Provides sufficient length for descriptive type names
- Consistent sizing across similar fields

### 9. Timestamps
Applications and services tables include `created_at` and `updated_at` with `TIMESTAMPTZ` for:
- Complete audit trail
- Time-zone aware timestamps
- Automatic timestamp generation and updates
- Tracking both creation and modification times
- Note: service_dependencies and token_blacklist tables intentionally exclude created_at/updated_at for minimal design

### 10. Immutable Primary Key
The `id` field serves as both identifier and primary key:
- Immutable to ensure consistent pod naming in Podman
- Stable namespace naming in OpenShift
- Natural referential integrity in deployed resources
- Prevents accidental renames that would break deployments

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
WHERE a.id = 'my-app'
ORDER BY s.created_at;
```

### 3. Get all services for an application:
```sql
SELECT * FROM services
WHERE app_id = 'my-app'
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
    a.deployment_name,
    consumer.id as consumer_service_id,
    consumer.type as consumer_service_type,
    provider.id as provider_service_id,
    provider.type as provider_service_type
FROM applications a
JOIN services consumer ON a.id = consumer.app_id
LEFT JOIN service_dependencies sd ON consumer.id = sd.consumer_service_id
LEFT JOIN services provider ON sd.provider_service_id = provider.id
WHERE a.id = 'my-app'
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
SELECT * FROM applications WHERE id = 'my-app';
```

### 9. Get applications by type:
```sql
SELECT * FROM applications WHERE type = 'Digital Assistant';
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
- Simple and maintainable schema with 4 tables (applications, services, service_dependencies, tokens)
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
- Dual-purpose tokens table for access token blacklist and refresh token whitelist
- Tokens stored as SHA-256 hashes for enhanced security
- Database-backed token management replacing in-memory implementation
- Server-side session management via refresh token storage
