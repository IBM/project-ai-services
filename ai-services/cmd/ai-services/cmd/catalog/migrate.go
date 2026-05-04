package catalog

import (
	"fmt"
	"os"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog/db"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/db/migrations"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/spf13/cobra"
)

// NewMigrateCmd returns the cobra command for database migration operations.
func NewMigrateCmd() *cobra.Command {
	var (
		dbHost     string
		dbPort     int
		dbUser     string
		dbPassword string
		dbName     string
		dbSSLMode  string
	)

	migrateCmd := &cobra.Command{
		Use:   "migrate",
		Short: "Manage database migrations for the catalog service",
		Long: `Manage database migrations for the catalog service.
This command provides subcommands to initialize the database, run migrations,
check migration status, and rollback migrations.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	// Add persistent flags for database connection
	migrateCmd.PersistentFlags().StringVar(&dbHost, "db-host", "localhost", "Database host")
	migrateCmd.PersistentFlags().IntVar(&dbPort, "db-port", db.DefaultDBPort, "Database port")
	migrateCmd.PersistentFlags().StringVar(&dbUser, "db-user", "admin", "Database user")
	migrateCmd.PersistentFlags().StringVar(&dbPassword, "db-password", "", "Database password")
	migrateCmd.PersistentFlags().StringVar(&dbName, "db-name", db.DefaultDBName, "Database name")
	migrateCmd.PersistentFlags().StringVar(&dbSSLMode, "db-sslmode", "disable", "Database SSL mode (disable, require, verify-ca, verify-full)")

	// Helper function to get database config from flags
	getDBConfig := func() db.Config {
		// Check for environment variables if password not provided
		password := dbPassword
		if password == "" {
			password = os.Getenv("DB_PASSWORD")
		}

		return db.Config{
			Host:     dbHost,
			Port:     dbPort,
			User:     dbUser,
			Password: password,
			DBName:   dbName,
			SSLMode:  dbSSLMode,
		}
	}

	// Subcommand: init - Initialize database and run all migrations
	initCmd := createInitCmd(getDBConfig)

	// Subcommand: up - Run pending migrations
	upCmd := createUpCmd(getDBConfig)
	upCmd.Hidden = true

	// Subcommand: status - Check migration status
	statusCmd := createStatusCmd(getDBConfig)
	statusCmd.Hidden = true

	// Subcommand: down - Rollback the last migration
	downCmd := createDownCmd(getDBConfig)
	downCmd.Hidden = true

	// Add subcommands
	migrateCmd.AddCommand(initCmd)
	migrateCmd.AddCommand(upCmd)
	migrateCmd.AddCommand(statusCmd)
	migrateCmd.AddCommand(downCmd)

	return migrateCmd
}

// createInitCmd creates the init subcommand.
func createInitCmd(getDBConfig func() db.Config) *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Initialize the database and run all migrations",
		Long: `Initialize the catalog database by creating it if it doesn't exist
and running all pending migrations.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := getDBConfig()

			logger.Infof("Initializing database '%s' on %s:%d...\n", cfg.DBName, cfg.Host, cfg.Port)

			// Create database if it doesn't exist
			if err := db.CreateDatabaseIfNotExists(cfg); err != nil {
				return fmt.Errorf("failed to create database: %w", err)
			}

			// Connect to the database
			database, err := db.Connect(cfg)
			if err != nil {
				return fmt.Errorf("failed to connect to database: %w", err)
			}
			defer func() {
				if closeErr := database.Close(); closeErr != nil {
					logger.Errorf("failed to close database connection: %v", closeErr)
				}
			}()

			// Run migrations
			if err := migrations.RunMigrations(database); err != nil {
				return fmt.Errorf("failed to run migrations: %w", err)
			}

			logger.Infoln("✓ Database initialized successfully")
			logger.Infoln("✓ All migrations applied")

			return nil
		},
	}
}

// createUpCmd creates the up subcommand.
func createUpCmd(getDBConfig func() db.Config) *cobra.Command {
	return &cobra.Command{
		Use:   "up",
		Short: "Run all pending migrations",
		Long:  `Run all pending database migrations.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := getDBConfig()

			logger.Infof("Connecting to database '%s' on %s:%d...\n", cfg.DBName, cfg.Host, cfg.Port)

			database, err := db.Connect(cfg)
			if err != nil {
				return fmt.Errorf("failed to connect to database: %w", err)
			}
			defer func() {
				if closeErr := database.Close(); closeErr != nil {
					logger.Errorf("failed to close database connection: %v", closeErr)
				}
			}()

			logger.Infoln("Running migrations...")

			if err := migrations.RunMigrations(database); err != nil {
				return fmt.Errorf("failed to run migrations: %w", err)
			}

			logger.Infoln("✓ All migrations applied successfully")

			return nil
		},
	}
}

// createStatusCmd creates the status subcommand.
func createStatusCmd(getDBConfig func() db.Config) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Check the status of database migrations",
		Long:  `Display the current status of all database migrations.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := getDBConfig()

			logger.Infof("Connecting to database '%s' on %s:%d...\n", cfg.DBName, cfg.Host, cfg.Port)

			database, err := db.Connect(cfg)
			if err != nil {
				return fmt.Errorf("failed to connect to database: %w", err)
			}
			defer func() {
				if closeErr := database.Close(); closeErr != nil {
					logger.Errorf("failed to close database connection: %v", closeErr)
				}
			}()

			logger.Infoln("\nMigration Status:")
			logger.Infoln("=================")

			if err := migrations.GetMigrationStatus(database); err != nil {
				return fmt.Errorf("failed to get migration status: %w", err)
			}

			return nil
		},
	}
}

// createDownCmd creates the down subcommand.
func createDownCmd(getDBConfig func() db.Config) *cobra.Command {
	return &cobra.Command{
		Use:   "down",
		Short: "Rollback the most recent migration",
		Long:  `Rollback the most recently applied database migration.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := getDBConfig()

			logger.Infof("Connecting to database '%s' on %s:%d...\n", cfg.DBName, cfg.Host, cfg.Port)

			database, err := db.Connect(cfg)
			if err != nil {
				return fmt.Errorf("failed to connect to database: %w", err)
			}
			defer func() {
				if closeErr := database.Close(); closeErr != nil {
					logger.Errorf("failed to close database connection: %v", closeErr)
				}
			}()

			logger.Infoln("Rolling back last migration...")

			if err := migrations.RollbackMigration(database); err != nil {
				return fmt.Errorf("failed to rollback migration: %w", err)
			}

			logger.Infoln("✓ Migration rolled back successfully")

			return nil
		},
	}
}

// Made with Bob
