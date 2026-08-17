package catalog

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/project-ai-services/ai-services/cmd/ai-services/cmd/catalog/common"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver"
	apirepository "github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/repository"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/services/auth"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/services/sync"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/db"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/db/repository"
	pkgconstants "github.com/project-ai-services/ai-services/internal/pkg/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/vars"
	workerregistry "github.com/project-ai-services/ai-services/internal/pkg/worker/registry"
	"github.com/spf13/cobra"
)

const (
	defaultRandomSecretKeyLength  = 32
	connectorEncryptionKeyByteLen = 32
)

// loadDBConfig loads database configuration from environment variables.
func loadDBConfig() (db.Config, error) {
	portStr := utils.GetEnv("DB_PORT", strconv.Itoa(constants.DefaultDBPort))
	dbPort, err := strconv.Atoi(portStr)
	if err != nil {
		return db.Config{}, fmt.Errorf("invalid DB_PORT value '%s': %w", portStr, err)
	}

	dbConfig := db.Config{
		Host:     utils.GetEnv("DB_HOST", constants.DefaultDBHost),
		Port:     dbPort,
		User:     utils.GetEnv("DB_USER", constants.DefaultDBUser),
		Password: os.Getenv("DB_PASSWORD"),
		DBName:   utils.GetEnv("DB_NAME", constants.DefaultDBName),
		SSLMode:  utils.GetEnv("DB_SSLMODE", constants.DefaultSSLMode),
	}

	if dbConfig.Password == "" {
		return db.Config{}, fmt.Errorf("DB_PASSWORD environment variable is required")
	}

	return dbConfig, nil
}

// getOrGenerateSecretKey retrieves the JWT secret key from environment or generates a random one.
func getOrGenerateSecretKey() (string, error) {
	secretKey := os.Getenv("AUTH_JWT_SECRET")
	if len(secretKey) == 0 {
		logger.DebuglnCtx(context.Background(), "** WARNING: AUTH_JWT_SECRET environment variable not set. This is not recommended for production use. **")
		byteSecretKey, err := auth.GenerateRandomSecretKey(defaultRandomSecretKeyLength)
		if err != nil {
			return "", err
		}
		secretKey = string(byteSecretKey)
	}

	return secretKey, nil
}

// loadConnectorEncryptionKey reads the CONNECTOR_ENCRYPTION_KEY environment variable,
// base64-decodes it, and validates that it is exactly 32 bytes (required for AES-256).
// Returns an error on startup if the key is absent or malformed so that misconfigurations
// are caught immediately rather than at first use.
func loadConnectorEncryptionKey() ([]byte, error) {
	encoded := os.Getenv(string(pkgconstants.ConnectorEncryptionKey))
	if encoded == "" {
		return nil, fmt.Errorf("%s environment variable is required", pkgconstants.ConnectorEncryptionKey)
	}

	key, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("failed to base64-decode %s: %w", pkgconstants.ConnectorEncryptionKey, err)
	}

	if len(key) != connectorEncryptionKeyByteLen {
		return nil, fmt.Errorf("%s must decode to exactly %d bytes (AES-256), got %d",
			pkgconstants.ConnectorEncryptionKey, connectorEncryptionKeyByteLen, len(key))
	}

	return key, nil
}

// buildAPIServerOptions wires all service dependencies and returns the options
// needed to start the API server. pool.Close() and the returned cleanup func
// must be called by the caller.
func buildAPIServerOptions(ctx context.Context, pool *pgxpool.Pool, secretKey, adminUser, adminPassHash string, accessTTL, refreshTTL time.Duration, workerGatewayPort int) (apiserver.APIServerOptions, func(), error) {
	userRepo := apirepository.NewInMemoryUserRepoWithAdminHash("uid_1", adminUser, "Admin", adminPassHash)
	tokenBlacklistRepo := repository.NewTokenBlacklistRepository(pool)
	blacklist := apirepository.NewDBTokenBlacklist(tokenBlacklistRepo)

	// Initialize repositories
	appRepo := repository.NewApplicationRepository(pool)
	svcRepo := repository.NewServiceRepository(pool)
	compRepo := repository.NewComponentRepository(pool)
	svcDepRepo := repository.NewServiceDependencyRepository(pool)

	// Initialize sync service for background DB-Pod synchronization
	// TODO: implement sync service on remote machines
	syncService, err := sync.NewSyncService(appRepo, svcRepo, compRepo, svcDepRepo, sync.DefaultSyncInterval)
	if err != nil {
		return apiserver.APIServerOptions{}, nil, fmt.Errorf("failed to initialize sync service: %w", err)
	}
	syncService.Start(ctx)

	catalogProvider, err := catalog.NewCatalogProvider()
	if err != nil {
		syncService.Stop(ctx)

		return apiserver.APIServerOptions{}, nil, fmt.Errorf("failed to initialize catalog provider: %w", err)
	}

	tokenMgr := auth.NewTokenManager(secretKey, accessTTL, refreshTTL)
	workerRepo := repository.NewWorkerRepository(pool)
	workerReg := workerregistry.New(workerRepo)

	opts := apiserver.APIServerOptions{
		Port:               0, // set by caller
		AuthService:        auth.NewAuthService(userRepo, tokenMgr, blacklist),
		TokenManager:       tokenMgr,
		Blacklist:          blacklist,
		ApplicationService: apirepository.NewApplicationService(appRepo, svcRepo, compRepo, svcDepRepo, catalogProvider, vars.RuntimeFactory.GetRuntimeType()),
		WorkerGatewayPort:  workerGatewayPort,
		WorkerRegistry:     workerReg,
		WorkerTokenStore:   workerregistry.NewTokenStore(),
		WorkerRepository:   workerRepo,
	}
	cleanup := func() {
		blacklist.Stop()
		syncService.Stop(ctx)
	}

	return opts, cleanup, nil
}

// runAPIServer initializes and starts the API server with the provided configuration.
func runAPIServer(port int, accessTTL, refreshTTL time.Duration, adminUser, adminPassHash string, workerGatewayPort int) error {
	secretKey, err := getOrGenerateSecretKey()
	if err != nil {
		return err
	}

	dbConfig, err := loadDBConfig()
	if err != nil {
		return err
	}

	if _, err := loadConnectorEncryptionKey(); err != nil {
		return err
	}

	// Use a signal-aware context so that SIGINT/SIGTERM cancel the context,
	// which stops the gateway sweeper and triggers gRPC GracefulStop.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	pool, err := db.ConnectPool(ctx, dbConfig)
	if err != nil {
		return fmt.Errorf("failed to connect to database: %w", err)
	}
	defer pool.Close()
	logger.Infoln("Connected to database successfully")

	opts, cleanup, err := buildAPIServerOptions(ctx, pool, secretKey, adminUser, adminPassHash, accessTTL, refreshTTL, workerGatewayPort)
	if err != nil {
		return err
	}
	defer cleanup()

	opts.Port = port

	return apiserver.NewAPIserver(opts).Start(ctx)
}

func NewAPIServerCmd() *cobra.Command {
	var (
		port                   = 8080
		defaultAccessTokenTTL  = time.Minute * 15
		defaultRefreshTokenTTL = time.Hour * 24 * 1
		adminUserName          string
		adminPasswordHash      string
		runtimeType            string
		workerGatewayPort      int
	)

	apiserverCmd := &cobra.Command{
		Use:   "apiserver",
		Short: "Manage AI Services API server",
		Long:  `Start the AI Services API server to provide REST endpoints for managing applications, services, and authentication.`,
		Example: `  # Start the API server with default settings
	 ai-services catalog apiserver --admin-password-hash <PASSWORD_HASH> --runtime podman

	 # Start the API server on a custom port
	 ai-services catalog apiserver --port 9090 --admin-password-hash <PASSWORD_HASH> --runtime podman

	 # Start with custom admin username
	 ai-services catalog apiserver --admin-username myadmin --admin-password-hash <PASSWORD_HASH> --runtime podman

	 # Start with custom token TTL settings
	 ai-services catalog apiserver --access-token-ttl 30m --refresh-token-ttl 48h --admin-password-hash <PASSWORD_HASH> --runtime podman

	 # Start with all custom settings
	 ai-services catalog apiserver --port 9090 --admin-username myadmin --admin-password-hash <PASSWORD_HASH> --access-token-ttl 30m --refresh-token-ttl 48h --runtime podman

Note:
  - Requires database connection via environment variables (DB_HOST, DB_PORT, DB_USER, DB_PASSWORD, DB_NAME)
  - AUTH_JWT_SECRET environment variable is recommended for production use`,
		PreRunE: func(cmd *cobra.Command, args []string) error {
			return common.InitAndValidateRuntimeFlag(runtimeType)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAPIServer(port, defaultAccessTokenTTL, defaultRefreshTokenTTL, adminUserName, adminPasswordHash, workerGatewayPort)
		},
	}

	apiserverCmd.Flags().IntVarP(&port, "port", "p", port, "Port for the API server to listen on")
	apiserverCmd.Flags().DurationVarP(&defaultAccessTokenTTL, "access-token-ttl", "", defaultAccessTokenTTL, "Time-to-live for access tokens")
	apiserverCmd.Flags().DurationVarP(&defaultRefreshTokenTTL, "refresh-token-ttl", "", defaultRefreshTokenTTL, "Time-to-live for refresh tokens")
	apiserverCmd.Flags().StringVar(&adminUserName, "admin-username", "admin", "Username for the default admin user")
	apiserverCmd.Flags().StringVar(&adminPasswordHash, "admin-password-hash", "", "Precomputed hash of the password for the default admin user")
	apiserverCmd.Flags().IntVar(&workerGatewayPort, "workergateway-port", defaultWorkerGatewayPort, "Port for the gRPC worker gateway (always active, default 9090)")
	common.ConfigureRuntimeFlag(apiserverCmd, &runtimeType)

	return apiserverCmd
}
