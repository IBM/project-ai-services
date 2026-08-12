package catalog

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"strconv"
	"time"

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
	"github.com/spf13/cobra"
)

const (
	defaultRandomSecretKeyLength    = 32
	connectorEncryptionKeyByteLen   = 32
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

// initApplicationService sets up the database pool, repositories, sync service,
// and application service. The caller is responsible for closing the pool and
// stopping the sync service via the returned cleanup function.
func initApplicationService(ctx context.Context) (
	appSvc apirepository.ApplicationServiceInterface,
	blacklist *apirepository.DBTokenBlacklist,
	cleanup func(),
	err error,
) {
	dbConfig, err := loadDBConfig()
	if err != nil {
		return nil, nil, nil, err
	}

	pool, err := db.ConnectPool(ctx, dbConfig)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to connect to database: %w", err)
	}
	logger.Infoln("Connected to database successfully")

	tokenBlacklistRepo := repository.NewTokenBlacklistRepository(pool)
	bl := apirepository.NewDBTokenBlacklist(tokenBlacklistRepo)

	applicationRepo := repository.NewApplicationRepository(pool)
	serviceRepo := repository.NewServiceRepository(pool)
	componentRepo := repository.NewComponentRepository(pool)
	serviceDependencyRepo := repository.NewServiceDependencyRepository(pool)

	syncService, err := sync.NewSyncService(
		applicationRepo, serviceRepo, componentRepo, serviceDependencyRepo,
		sync.DefaultSyncInterval,
	)
	if err != nil {
		pool.Close()
		bl.Stop()

		return nil, nil, nil, fmt.Errorf("failed to initialize sync service: %w", err)
	}
	syncService.Start(ctx)

	catalogProvider, err := catalog.NewCatalogProvider()
	if err != nil {
		syncService.Stop(ctx)
		pool.Close()
		bl.Stop()

		return nil, nil, nil, fmt.Errorf("failed to initialize catalog provider: %w", err)
	}

	svc := apirepository.NewApplicationService(applicationRepo, serviceRepo, componentRepo, serviceDependencyRepo, catalogProvider, vars.RuntimeFactory.GetRuntimeType())

	cleanup = func() {
		syncService.Stop(ctx)
		bl.Stop()
		pool.Close()
	}

	return svc, bl, cleanup, nil
}

// runAPIServer initializes and starts the API server with the provided configuration.
func runAPIServer(port int, accessTTL, refreshTTL time.Duration, adminUser, adminPassHash string, workerGatewayPort int) error {
	secretKey, err := getOrGenerateSecretKey()
	if err != nil {
		return err
	}

	if _, err := loadConnectorEncryptionKey(); err != nil {
		return err
	}

	ctx := context.Background()
	applicationService, blacklist, cleanup, err := initApplicationService(ctx)
	if err != nil {
		return err
	}
	defer cleanup()

	userRepo := apirepository.NewInMemoryUserRepoWithAdminHash("uid_1", adminUser, "Admin", adminPassHash)
	tokenMgr := auth.NewTokenManager(secretKey, accessTTL, refreshTTL)
	authSvc := auth.NewAuthService(userRepo, tokenMgr, blacklist)

	return apiserver.NewAPIserver(apiserver.APIServerOptions{
		Port:               port,
		AuthService:        authSvc,
		TokenManager:       tokenMgr,
		Blacklist:          blacklist,
		ApplicationService: applicationService,
	}).Start()
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
