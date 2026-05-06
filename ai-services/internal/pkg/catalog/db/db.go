// Package db provides database connection utilities for the catalog service.
package db

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib" // PostgreSQL driver
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
)

const (
	// DriverName is the PostgreSQL driver name used for database connections.
	DriverName = "pgx"
	// DefaultDBName is the default database name for the catalog service.
	DefaultDBName = "ai_services"
	// DefaultDBPort is the default PostgreSQL port.
	DefaultDBPort = 5432
	// DefaultMaxOpenConns is the default maximum number of open connections.
	DefaultMaxOpenConns = 25
	// DefaultMaxIdleConns is the default maximum number of idle connections.
	DefaultMaxIdleConns = 5
	// DefaultConnMaxLifetime is the default maximum lifetime of a connection.
	DefaultConnMaxLifetime = 5 * time.Minute
	// DefaultPingTimeout is the default timeout for database ping operations.
	DefaultPingTimeout = 5 * time.Second
)

// Config holds database configuration parameters.
type Config struct {
	Host     string
	Port     int
	User     string
	Password string
	DBName   string
	SSLMode  string
}

// ConnectionString builds a PostgreSQL connection string from the config.
func (c *Config) ConnectionString() string {
	return fmt.Sprintf(
		"host=%s port=%d user=%s password='%s' dbname=%s sslmode=%s",
		c.Host, c.Port, c.User, c.Password, c.DBName, c.SSLMode,
	)
}

// ConnectionURL builds a PostgreSQL connection URL from the config (for pgxpool).
func (c *Config) ConnectionURL() string {
	return fmt.Sprintf(
		"postgres://%s:%s@%s:%d/%s?sslmode=%s",
		c.User, url.QueryEscape(c.Password), c.Host, c.Port, c.DBName, c.SSLMode,
	)
}

// Connect establishes a connection to the PostgreSQL database using sql.DB (for migrations).
func Connect(cfg Config) (*sql.DB, error) {
	db, err := sql.Open(DriverName, cfg.ConnectionString())
	if err != nil {
		return nil, fmt.Errorf("failed to open database connection: %w", err)
	}

	// Set connection pool settings
	db.SetMaxOpenConns(DefaultMaxOpenConns)
	db.SetMaxIdleConns(DefaultMaxIdleConns)
	db.SetConnMaxLifetime(DefaultConnMaxLifetime)

	// Verify connection
	ctx, cancel := context.WithTimeout(context.Background(), DefaultPingTimeout)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		if closeErr := db.Close(); closeErr != nil {
			return nil, fmt.Errorf("failed to ping database: %w (close error: %v)", err, closeErr)
		}

		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	return db, nil
}

// ConnectPool establishes a connection pool to the PostgreSQL database using pgxpool.
func ConnectPool(ctx context.Context, cfg Config) (*pgxpool.Pool, error) {
	poolConfig, err := pgxpool.ParseConfig(cfg.ConnectionURL())
	if err != nil {
		return nil, fmt.Errorf("failed to parse pool config: %w", err)
	}

	// Set connection pool settings
	poolConfig.MaxConns = int32(DefaultMaxOpenConns)
	poolConfig.MinConns = int32(DefaultMaxIdleConns)
	poolConfig.MaxConnLifetime = DefaultConnMaxLifetime

	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create connection pool: %w", err)
	}

	// Verify connection
	pingCtx, cancel := context.WithTimeout(ctx, DefaultPingTimeout)
	defer cancel()

	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()

		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	return pool, nil
}

// CreateDatabaseIfNotExists creates the database if it doesn't exist.
func CreateDatabaseIfNotExists(cfg Config) error {
	// Connect to postgres database to create the target database
	adminCfg := cfg
	adminCfg.DBName = "postgres"

	db, err := Connect(adminCfg)
	if err != nil {
		return fmt.Errorf("failed to connect to postgres database: %w", err)
	}
	defer func() {
		if closeErr := db.Close(); closeErr != nil {
			// Log the error but don't override the main error
			logger.Warningf("failed to close database connection: %v\n", closeErr)
		}
	}()

	// Check if database exists
	var exists bool
	query := "SELECT EXISTS(SELECT 1 FROM pg_database WHERE datname = $1)"
	err = db.QueryRow(query, cfg.DBName).Scan(&exists)
	if err != nil {
		return fmt.Errorf("failed to check if database exists: %w", err)
	}

	if exists {
		return nil
	}

	// Create database
	createQuery := fmt.Sprintf("CREATE DATABASE %s", cfg.DBName)
	_, err = db.Exec(createQuery)
	if err != nil {
		return fmt.Errorf("failed to create database: %w", err)
	}

	return nil
}

// Made with Bob
