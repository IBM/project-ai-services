package constants

import "time"

const (
	// DriverName is the PostgreSQL driver name used for database connections.
	DriverName = "pgx"
	// DefaultDBHost is the default database host.
	DefaultDBHost = "localhost"
	// DefaultDBName is the default database name for the catalog service.
	DefaultDBName = "ai_services"
	// DefaultDBPort is the default PostgreSQL port.
	DefaultDBPort = 5432
	// DefaultDBUser is the default database user.
	DefaultDBUser = "admin"
	// DefaultSSLMode is the default SSL mode for database connections.
	DefaultSSLMode = "disable"
	// DefaultMaxOpenConns is the default maximum number of open connections.
	DefaultMaxOpenConns = 25
	// DefaultMaxIdleConns is the default maximum number of idle connections.
	DefaultMaxIdleConns = 5
	// DefaultConnMaxLifetime is the default maximum lifetime of a connection.
	DefaultConnMaxLifetime = 5 * time.Minute
	// DefaultPingTimeout is the default timeout for database ping operations.
	DefaultPingTimeout = 5 * time.Second
)

// Made with Bob
