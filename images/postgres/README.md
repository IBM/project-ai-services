# PostgreSQL 18 Container Image

This directory contains a custom PostgreSQL 18 container image based on Red Hat Universal Base Image 9 (UBI 9).

> **Note**: This image is based on the [official PostgreSQL Docker images](https://github.com/docker-library/postgres) maintained by the Docker Community. The entrypoint scripts and initialization logic are adapted from that project.

## Overview

This PostgreSQL image is designed for production use with the following features:

- **Base Image**: Red Hat UBI 9
- **PostgreSQL Version**: 18
- **Architecture Support**: ppc64le
- **User Management**: Runs as non-root `postgres` user (UID/GID 26)
- **Data Directory**: `/var/lib/pgsql/18/data` (compatible with pg_ctlcluster)
- **Volume Mount**: `/var/lib/pgsql`

## Features

### Security
- Runs as non-root user by default
- Uses `gosu` for privilege de-escalation
- Configurable authentication methods via environment variables

### Initialization
- Automatic database initialization on first run
- Support for initialization scripts (`.sh`, `.sql`, `.sql.gz`, `.sql.xz`, `.sql.zst`)
- Custom initialization directory: `/docker-entrypoint-initdb.d/`

### Configuration
- Pre-configured to listen on all interfaces
- Customizable via environment variables
- Support for custom `postgresql.conf` settings

## Building the Image

### Prerequisites
- Podman or Docker installed
- Access to Red Hat UBI repositories
- Access to PostgreSQL YUM repositories

### Build Command

```bash
make build
```

Or manually:

```bash
podman build -t postgres:18 .
```

### Build for ppc64le Architecture

```bash
make build PLATFORM=ppc64le
```

## Running the Container

### Basic Usage

```bash
podman run -d \
  --name postgres \
  -e POSTGRES_PASSWORD=mysecretpassword \
  -p 5432:5432 \
  -v pgdata:/var/lib/pgsql \
  postgres:18
```

### With Custom Database and User

```bash
podman run -d \
  --name postgres \
  -e POSTGRES_DB=myapp \
  -e POSTGRES_USER=myuser \
  -e POSTGRES_PASSWORD=mypassword \
  -p 5432:5432 \
  -v pgdata:/var/lib/pgsql \
  postgres:18
```

## Environment Variables

### Required Variables

- `POSTGRES_PASSWORD`: Password for the PostgreSQL superuser (required unless using `trust` authentication)

### Optional Variables

- `POSTGRES_USER`: PostgreSQL superuser name (default: `postgres`)
- `POSTGRES_DB`: Default database name (default: same as `POSTGRES_USER`)
- `POSTGRES_INITDB_ARGS`: Additional arguments to pass to `initdb`
- `POSTGRES_INITDB_WALDIR`: Custom location for transaction log directory
- `POSTGRES_HOST_AUTH_METHOD`: Authentication method for host connections (default: `scram-sha-256` for PG 14+)

### Using Docker Secrets

For sensitive data, you can use file-based environment variables:

```bash
podman run -d \
  --name postgres \
  -e POSTGRES_PASSWORD_FILE=/run/secrets/postgres-passwd \
  -v ./secrets/postgres-passwd:/run/secrets/postgres-passwd:ro \
  postgres:18
```

## Initialization Scripts

Place initialization scripts in `/docker-entrypoint-initdb.d/`:

- `*.sh`: Shell scripts (executed or sourced based on permissions)
- `*.sql`: SQL scripts
- `*.sql.gz`: Gzipped SQL scripts
- `*.sql.xz`: XZ-compressed SQL scripts
- `*.sql.zst`: Zstandard-compressed SQL scripts

Example:

```bash
podman run -d \
  --name postgres \
  -e POSTGRES_PASSWORD=mysecretpassword \
  -v ./init-scripts:/docker-entrypoint-initdb.d:ro \
  -v pgdata:/var/lib/pgsql \
  postgres:18
```

## Data Persistence

The image uses `/var/lib/pgsql` as the volume mount point. PostgreSQL data is stored in `/var/lib/pgsql/18/data`.

This structure allows for easier upgrades using `pg_upgrade --link` without mount point boundary issues.

## Upgrading from Previous Versions

If upgrading from PostgreSQL versions < 18, you'll need to use `pg_upgrade`. The recommended approach:

1. Mount `/var/lib/pgsql` as a single volume
2. Use `pg_upgrade --link` for efficient upgrades
3. See [PostgreSQL upgrade documentation](https://www.postgresql.org/docs/current/pgupgrade.html)

## Helper Scripts

### docker-entrypoint.sh

Main entrypoint script that handles:
- Database initialization
- User and permission setup
- Initialization script processing
- Server startup

### docker-ensure-initdb.sh

Utility script for Kubernetes init containers or CI/CD:
- Ensures database is initialized
- No-op if already initialized
- Can be used as `docker-enforce-initdb.sh` to error if database exists

## Port

- **5432**: PostgreSQL server port (exposed by default)

## Signals

The container uses `SIGINT` as the stop signal, which triggers PostgreSQL's "Fast Shutdown mode":
- New connections are disallowed
- In-progress transactions are aborted
- PostgreSQL stops cleanly and flushes tables to disk

**Note**: Consider setting `--stop-timeout` to 90+ seconds for graceful shutdown of large databases.

## Health Checks

Example health check:

```bash
podman run -d \
  --name postgres \
  --health-cmd="pg_isready -U postgres" \
  --health-interval=10s \
  --health-timeout=5s \
  --health-retries=5 \
  -e POSTGRES_PASSWORD=mysecretpassword \
  postgres:18
```

## Troubleshooting

### Database Won't Start

1. Check logs: `podman logs postgres`
2. Verify permissions on data directory
3. Ensure `POSTGRES_PASSWORD` is set (unless using `trust` authentication)

### Permission Denied Errors

Ensure the volume mount has correct permissions:

```bash
podman unshare chown -R 26:26 /path/to/pgdata
```

### Old Database Format Detected

If upgrading from pre-18 versions, you'll see an error about old database format. Use `pg_upgrade` to migrate your data.

## Security Considerations

- **Never use `POSTGRES_HOST_AUTH_METHOD=trust` in production**
- Use strong passwords or certificate-based authentication
- Consider using PostgreSQL's SSL/TLS support
- Regularly update to the latest PostgreSQL patch version
- Use network policies to restrict database access

## License

See the [LICENSE](../LICENSE) file in the parent directory.

## Credits

This image is based on the [official PostgreSQL Docker images](https://github.com/docker-library/postgres) maintained by the Docker Community. We gratefully acknowledge their work in creating and maintaining the entrypoint scripts and initialization logic.

## References

- [PostgreSQL Official Documentation](https://www.postgresql.org/docs/18/)
- [PostgreSQL Docker Library](https://github.com/docker-library/postgres) - Original source for entrypoint scripts
- [Red Hat UBI](https://www.redhat.com/en/blog/introducing-red-hat-universal-base-image)