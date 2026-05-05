#!/bin/bash
# Database initialization script for digitize service
# This script is executed by the init container

set -e  # Exit on error

echo "Starting database initialization..."

# Wait for PostgreSQL to be ready (connect to default 'postgres' database)
echo "Waiting for PostgreSQL to be ready..."
until PGPASSWORD=$POSTGRES_PASSWORD psql -h "$POSTGRES_HOST" -U "$POSTGRES_USER" -d postgres -c '\q' 2>/dev/null; do
  echo "PostgreSQL is unavailable - sleeping"
  sleep 2
done

echo "PostgreSQL is ready!"

# Create database if it doesn't exist (must use 'postgres' database to create new databases)
echo "Creating database '$POSTGRES_DB' if not exists..."
PGPASSWORD=$POSTGRES_PASSWORD psql -h "$POSTGRES_HOST" -U "$POSTGRES_USER" -d postgres -tc \
  "SELECT 1 FROM pg_database WHERE datname = '$POSTGRES_DB'" | grep -q 1 || \
  PGPASSWORD=$POSTGRES_PASSWORD psql -h "$POSTGRES_HOST" -U "$POSTGRES_USER" -d postgres \
  -c "CREATE DATABASE $POSTGRES_DB"

# Verify target database is accessible
echo "Verifying database '$POSTGRES_DB' is accessible..."
until PGPASSWORD=$POSTGRES_PASSWORD psql -h "$POSTGRES_HOST" -U "$POSTGRES_USER" -d "$POSTGRES_DB" -c '\q' 2>/dev/null; do
  echo "Database '$POSTGRES_DB' not yet accessible - sleeping"
  sleep 1
done

echo "Database '$POSTGRES_DB' is accessible!"

# Run schema initialization on target database
# Note: psql automatically closes the connection when the command completes
echo "Initializing database schema..."
PGPASSWORD=$POSTGRES_PASSWORD psql -h "$POSTGRES_HOST" -U "$POSTGRES_USER" -d "$POSTGRES_DB" \
  -f /scripts/init_schema.sql

echo "✅ Database initialization completed successfully!"
# Connection is automatically closed when psql process exits

# Made with Bob
