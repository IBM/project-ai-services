package worker

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	cmdcommon "github.com/project-ai-services/ai-services/cmd/ai-services/cmd/common"
	catalogUtils "github.com/project-ai-services/ai-services/internal/pkg/catalog/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
	workerdeploy "github.com/project-ai-services/ai-services/internal/pkg/worker/deploy"
	"github.com/project-ai-services/ai-services/internal/pkg/worker/join"
)

var (
	grpcConnectToken       string
	grpcConnectRuntimeType string
	grpcConnectBaseDir     string
	grpcConnectHTTPSPort   int
	grpcConnectSSLCertPath string
	grpcConnectSSLKeyPath  string
)

var grpcConnectCmd = &cobra.Command{
	Use:    "grpc-connect <gateway>",
	Short:  "Connect to the catalog gRPC worker-gateway",
	Hidden: true,
	Args:   cobra.ExactArgs(1),
	PreRunE: func(cmd *cobra.Command, _ []string) error {
		cmd.SilenceUsage = true

		return cmdcommon.InitAndValidateRuntimeFlag(grpcConnectRuntimeType)
	},
	RunE: grpcConnectRunE,
}

func grpcConnectRunE(_ *cobra.Command, args []string) error {
	opts := join.Options{
		GatewayAddr: args[0],
		Token:       grpcConnectToken,
		RuntimeType: types.RuntimeType(grpcConnectRuntimeType),
		Setup: workerdeploy.Options{
			BaseDir:     grpcConnectBaseDir,
			HTTPSPort:   grpcConnectHTTPSPort,
			SSLCertPath: catalogUtils.SanitizeFilePath(grpcConnectSSLCertPath),
			SSLKeyPath:  catalogUtils.SanitizeFilePath(grpcConnectSSLKeyPath),
		},
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	return join.GrpcConnect(ctx, opts)
}

func newGrpcConnectCmd() *cobra.Command {
	grpcConnectCmd.Flags().StringVar(&grpcConnectToken, "token", "",
		"Single-use bootstrap token issued by 'catalog worker register' (required).\n"+
			"Example: --token <uuid>\n")
	_ = grpcConnectCmd.MarkFlagRequired("token")

	cmdcommon.ConfigureRuntimeFlag(grpcConnectCmd, &grpcConnectRuntimeType)

	grpcConnectCmd.Flags().StringVar(&grpcConnectBaseDir, "basedir", "",
		"Base directory for AI services data (models, caddy, etc.) on this worker.\n"+
			"Example: --basedir /var/lib/ai-services\n")

	grpcConnectCmd.Flags().IntVar(&grpcConnectHTTPSPort, "https-port", defaultJoinHTTPSPort,
		"Custom HTTPS port to expose the service endpoints externally.\n"+
			"Example: --https-port 8443\n")

	grpcConnectCmd.Flags().StringVar(&grpcConnectSSLCertPath, "ssl-cert", "",
		"Path to user-provided SSL certificate (optional).\n"+
			"Must be used together with --ssl-key.\n"+
			"Example: --ssl-cert /path/to/cert.pem\n")

	grpcConnectCmd.Flags().StringVar(&grpcConnectSSLKeyPath, "ssl-key", "",
		"Path to user-provided SSL private key (optional).\n"+
			"Must be used together with --ssl-cert.\n"+
			"Example: --ssl-key /path/to/key.pem\n")

	return grpcConnectCmd
}
