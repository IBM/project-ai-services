# Database Initialization Scripts

This directory contains scripts for initializing the PostgreSQL database schema for the digitize service.

## Files

### 1. `init_schema.sql`
SQL script that creates the database schema including:
- **Tables**: `jobs` and `documents` with proper constraints
- **Indexes**: Optimized for common query patterns
- **Triggers**: Automatic `updated_at` timestamp management
- **Permissions**: Grant necessary privileges to `digitize_user`

**Features:**
- Idempotent design using `IF NOT EXISTS` clauses
- Safe to run multiple times without errors
- Compatible with PostgreSQL 13+

### 2. `init_db.sh`
Bash script that orchestrates database initialization:
- Waits for PostgreSQL to be ready
- Creates the database if it doesn't exist
- Executes the schema initialization SQL
- Provides clear status messages

**Environment Variables Required:**
- `POSTGRES_HOST` - PostgreSQL server hostname
- `POSTGRES_PORT` - PostgreSQL server port (default: 5432)
- `POSTGRES_DB` - Database name to create/use
- `POSTGRES_USER` - PostgreSQL username
- `POSTGRES_PASSWORD` - PostgreSQL password

### 3. `setup_db_scripts.sh`
Helper script to copy initialization scripts to the deployment location.

**Usage:**
```bash
cd spyre-rag/src/digitize/db/scripts
./setup_db_scripts.sh [APP_NAME]
```

**Environment Variables:**
- `RUNTIME` - Runtime environment: `podman` (default) or `openshift`
- `SCRIPTS_BASE_PATH` - Custom base path for OpenShift (default: `/mnt/db-scripts`)

**Examples:**

For Podman (default):
```bash
cd spyre-rag/src/digitize/db/scripts
./setup_db_scripts.sh my-rag-app
# Copies to: /var/lib/ai-services/applications/my-rag-app/db-scripts/
```

For OpenShift:
```bash
cd spyre-rag/src/digitize/db/scripts
RUNTIME=openshift ./setup_db_scripts.sh my-rag-app
# Copies to: /mnt/db-scripts/
```

For OpenShift with custom path:
```bash
cd spyre-rag/src/digitize/db/scripts
RUNTIME=openshift SCRIPTS_BASE_PATH=/data/db-scripts ./setup_db_scripts.sh my-rag-app
# Copies to: /data/db-scripts/
```

## Integration with Podman Deployments

The database initialization is triggered automatically via an **init container** in the digitize pod deployment.

### Init Container Configuration

The init container is defined in the digitize deployment manifests:
- `ai-services/assets/applications/rag/podman/templates/digitize.yaml.tmpl`
- `ai-services/assets/applications/rag-cpu/podman/templates/digitize.yaml.tmpl`
- `ai-services/assets/applications/rag-dev/podman/templates/digitize.yaml.tmpl`

```yaml
spec:
  initContainers:
    - name: digitize-db-init
      image: "{{ .Values.postgres.image }}"
      command: ["/bin/sh", "/scripts/init_db.sh"]
      env:
        - name: POSTGRES_HOST
          value: "{{ .AppName }}--postgres"
        - name: POSTGRES_PORT
          value: "5432"
        - name: POSTGRES_DB
          value: "{{ .Values.postgres.database }}"
        - name: POSTGRES_USER
          value: "{{ .Values.postgres.user }}"
        - name: POSTGRES_PASSWORD
          value: "{{ .Values.postgres.password }}"
      volumeMounts:
        - name: db-init-scripts
          mountPath: /scripts
          readOnly: true
```

### Execution Flow

1. **PostgreSQL Pod Starts** - The postgres pod must be running first
2. **Init Container Runs** - Before the digitize containers start
3. **Database Initialization** - Creates schema if needed
4. **Main Containers Start** - Digitize service starts with database ready

### Volume Mount

The scripts are mounted from the host filesystem:

```yaml
volumes:
  - name: db-init-scripts
    hostPath:
      path: "/var/lib/ai-services/applications/{{ .AppName }}/db-scripts"
      type: DirectoryOrCreate
```

## Deployment Steps

### 1. Setup Scripts (One-time)

Before deploying the application, copy the initialization scripts:

```bash
cd spyre-rag/src/digitize/db/scripts
./setup_db_scripts.sh my-app-name
```

### 2. Deploy PostgreSQL

Deploy the PostgreSQL pod first:

```bash
podman play kube postgres.yaml
```

### 3. Deploy Digitize Service

Deploy the digitize service (init container will run automatically):

```bash
podman play kube digitize.yaml
```

The init container will:
- Wait for PostgreSQL to be ready
- Create the database if it doesn't exist
- Initialize the schema (tables, indexes, triggers)
- Exit successfully, allowing main containers to start

## Manual Database Verification

After deployment, verify the database initialization:

```bash
# Connect to PostgreSQL
podman exec -it <app-name>--postgres-postgres psql -U digitize_user -d digitize_metadata

# List tables
\dt

# Check table structure
\d jobs
\d documents

# Verify indexes
\di

# Exit
\q
```

## Troubleshooting

### Init Container Fails

Check init container logs:
```bash
podman logs <app-name>--digitize --container digitize-db-init
```

### Common Issues

1. **PostgreSQL not ready**: Init container waits up to 60 seconds
2. **Permission denied**: Ensure scripts have execute permissions
3. **Connection refused**: Verify PostgreSQL pod is running and accessible
4. **Scripts not found**: Run `setup_db_scripts.sh` to copy scripts

### Re-running Initialization

The scripts are idempotent and safe to re-run:

```bash
# Restart the digitize pod to re-run init container
podman pod restart <app-name>--digitize
```

## Schema Updates

To update the schema:

1. Modify `init_schema.sql` with new changes
2. Use `ALTER TABLE` statements with `IF NOT EXISTS` checks
3. Run `setup_db_scripts.sh` from `spyre-rag/src/digitize/db/scripts/` to update deployed scripts
4. Restart the digitize pod to apply changes

## Database Configuration

Database connection parameters are configured in `values.yaml`:

```yaml
postgres:
  image: icr.io/ai-services-private/postgres:18
  memoryLimit: 2Gi
  database: "digitize_metadata"
  user: "digitize_user"
  password: "DigitizeDB@12345"  # Change in production!
```

## Security Considerations

- **Change default password** in production environments
- Store passwords in secrets management systems
- Use TLS/SSL for database connections in production
- Restrict database user permissions to minimum required
- Regularly backup the database

## References

- Design Document: `docs/proposals/digitize_metadata_db_migration.md`
- Implementation Status: `docs/proposals/postgresql_implementation_status.md`
- PostgreSQL Documentation: https://www.postgresql.org/docs/