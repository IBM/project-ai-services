package catalog

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver"
	apirepository "github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/repository"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/services/auth"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/db"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/db/repository"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/vars"
	"github.com/spf13/cobra"
)

func NewAPIServerCmd() *cobra.Command {
	const (
		defaultRandomSecretKeyLength int = 32
	)
	var (
		port                   = 8080
		defaultAccessTokenTTL  = time.Minute * 15
		defaultRefreshTokenTTL = time.Hour * 24 * 1
		adminUserName          string
		adminPasswordHash      string
		runtimeType            string
	)
	apiserverCmd := &cobra.Command{
		Use:   "apiserver",
		Short: "Manage AI Services API server",
		Long:  `The apiserver command allows you to manage the AI Services API server, including starting, stopping, and checking the status of the server.`,
		PreRunE: func(cmd *cobra.Command, args []string) error {
			// Validate and set runtime
			rt := types.RuntimeType(runtimeType)
			if !rt.Valid() {
				return fmt.Errorf("invalid runtime type: %s (must be '%s' or '%s')", runtimeType, types.RuntimeTypePodman, types.RuntimeTypeOpenShift)
			}

			vars.RuntimeFactory = runtime.NewRuntimeFactory(rt)
			logger.Infof("Using runtime: %s\n", rt, logger.VerbosityLevelDebug)

			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			secretKey := os.Getenv("AUTH_JWT_SECRET")
			if len(secretKey) == 0 {
				fmt.Println("** WARNING: AUTH_JWT_SECRET environment variable not set. This is not recommended for production use. **")
				// generate a random secret key if not provided via environment variable
				byteSecretKey, err := auth.GenerateRandomSecretKey(defaultRandomSecretKeyLength)
				if err != nil {
					return err
				}
				secretKey = string(byteSecretKey)
			}

			// Load database configuration from environment variables
			portStr := utils.GetEnv("DB_PORT", strconv.Itoa(db.DefaultDBPort))
			dbPort, err := strconv.Atoi(portStr)
			if err != nil {
				return fmt.Errorf("invalid DB_PORT value '%s': %w", portStr, err)
			}

			dbConfig := db.Config{
				Host:     utils.GetEnv("DB_HOST", db.DefaultDBHost),
				Port:     dbPort,
				User:     utils.GetEnv("DB_USER", db.DefaultDBUser),
				Password: os.Getenv("DB_PASSWORD"),
				DBName:   utils.GetEnv("DB_NAME", db.DefaultDBName),
				SSLMode:  utils.GetEnv("DB_SSLMODE", db.DefaultSSLMode),
			}

			// Validate required password
			if dbConfig.Password == "" {
				return fmt.Errorf("DB_PASSWORD environment variable is required")
			}

			// Connect to database
			ctx := context.Background()
			pool, err := db.ConnectPool(ctx, dbConfig)
			if err != nil {
				return fmt.Errorf("failed to connect to database: %w", err)
			}
			defer pool.Close()

			logger.Infoln("Connected to database successfully")

			// Repositories
			userRepo := apirepository.NewInMemoryUserRepoWithAdminHash("uid_1", adminUserName, "Admin", adminPasswordHash)
			tokenBlacklistRepo := repository.NewTokenBlacklistRepository(pool)
			blacklist := apirepository.NewDBTokenBlacklist(tokenBlacklistRepo)
			defer blacklist.Stop()

			// JWT manager
			tokenMgr := auth.NewTokenManager(secretKey, defaultAccessTokenTTL, defaultRefreshTokenTTL)
			authSvc := auth.NewAuthService(userRepo, tokenMgr, blacklist)

			return apiserver.NewAPIserver(apiserver.APIServerOptions{Port: port, AuthService: authSvc, TokenManager: tokenMgr, Blacklist: blacklist}).Start()
		},
	}
	apiserverCmd.Flags().IntVarP(&port, "port", "p", port, "Port for the API server to listen on")
	apiserverCmd.Flags().DurationVarP(&defaultAccessTokenTTL, "access-token-ttl", "", defaultAccessTokenTTL, "Time-to-live for access tokens")
	apiserverCmd.Flags().DurationVarP(&defaultRefreshTokenTTL, "refresh-token-ttl", "", defaultRefreshTokenTTL, "Time-to-live for refresh tokens")
	apiserverCmd.Flags().StringVar(&adminUserName, "admin-username", "admin", "Username for the default admin user")
	apiserverCmd.Flags().StringVar(&adminPasswordHash, "admin-password-hash", "", "Precomputed hash of the password for the default admin user")
	apiserverCmd.Flags().StringVar(&runtimeType, "runtime", string(types.RuntimeTypePodman), fmt.Sprintf("Runtime to use (options: %s, %s)", types.RuntimeTypePodman, types.RuntimeTypeOpenShift))

	return apiserverCmd
}
