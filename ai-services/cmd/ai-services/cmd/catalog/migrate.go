package catalog

import (
	"fmt"
	"os"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog/db"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/db/migrations"
	"github.com/spf13/cobra"
)

// NewMigrateCmd returns the cobra command for database migration operations
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
	migrateCmd.PersistentFlags().IntVar(&dbPort, "db-port", 5432, "Database port")
	migrateCmd.PersistentFlags().StringVar(&dbUser, "db-user", "admim", "Database user")
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
	initCmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize the database and run all migrations",
		Long: `Initialize the catalog database by creating it if it doesn't exist
and running all pending migrations.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := getDBConfig()

			fmt.Printf("Initializing database '%s' on %s:%d...\n", cfg.DBName, cfg.Host, cfg.Port)

			// Create database if it doesn't exist
			if err := db.CreateDatabaseIfNotExists(cfg); err != nil {
				return fmt.Errorf("failed to create database: %w", err)
			}

			// Connect to the database
			database, err := db.Connect(cfg)
			if err != nil {
				return fmt.Errorf("failed to connect to database: %w", err)
			}
			defer database.Close()

			// Run migrations
			if err := migrations.RunMigrations(database); err != nil {
				return fmt.Errorf("failed to run migrations: %w", err)
			}

			fmt.Println("✓ Database initialized successfully")
			fmt.Println("✓ All migrations applied")

			return nil
		},
	}

	// Subcommand: up - Run pending migrations
	upCmd := &cobra.Command{
		Use:   "up",
		Short: "Run all pending migrations",
		Long:  `Run all pending database migrations.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := getDBConfig()

			fmt.Printf("Connecting to database '%s' on %s:%d...\n", cfg.DBName, cfg.Host, cfg.Port)

			database, err := db.Connect(cfg)
			if err != nil {
				return fmt.Errorf("failed to connect to database: %w", err)
			}
			defer database.Close()

			fmt.Println("Running migrations...")

			if err := migrations.RunMigrations(database); err != nil {
				return fmt.Errorf("failed to run migrations: %w", err)
			}

			fmt.Println("✓ All migrations applied successfully")

			return nil
		},
	}

	// Subcommand: status - Check migration status
	statusCmd := &cobra.Command{
		Use:   "status",
		Short: "Check the status of database migrations",
		Long:  `Display the current status of all database migrations.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := getDBConfig()

			fmt.Printf("Connecting to database '%s' on %s:%d...\n", cfg.DBName, cfg.Host, cfg.Port)

			database, err := db.Connect(cfg)
			if err != nil {
				return fmt.Errorf("failed to connect to database: %w", err)
			}
			defer database.Close()

			fmt.Println("\nMigration Status:")
			fmt.Println("=================")

			if err := migrations.GetMigrationStatus(database); err != nil {
				return fmt.Errorf("failed to get migration status: %w", err)
			}

			return nil
		},
	}

	// Subcommand: down - Rollback the last migration
	downCmd := &cobra.Command{
		Use:   "down",
		Short: "Rollback the most recent migration",
		Long:  `Rollback the most recently applied database migration.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := getDBConfig()

			fmt.Printf("Connecting to database '%s' on %s:%d...\n", cfg.DBName, cfg.Host, cfg.Port)

			database, err := db.Connect(cfg)
			if err != nil {
				return fmt.Errorf("failed to connect to database: %w", err)
			}
			defer database.Close()

			fmt.Println("Rolling back last migration...")

			if err := migrations.RollbackMigration(database); err != nil {
				return fmt.Errorf("failed to rollback migration: %w", err)
			}

			fmt.Println("✓ Migration rolled back successfully")

			return nil
		},
	}

	// Add subcommands
	migrateCmd.AddCommand(initCmd)
	migrateCmd.AddCommand(upCmd)
	migrateCmd.AddCommand(statusCmd)
	migrateCmd.AddCommand(downCmd)

	return migrateCmd
}

// Made with Bob
