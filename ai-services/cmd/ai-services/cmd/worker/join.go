package worker

import (
	"context"
	"fmt"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/spf13/cobra"

	cmdcommon "github.com/project-ai-services/ai-services/cmd/ai-services/cmd/common"
	catalogUtils "github.com/project-ai-services/ai-services/internal/pkg/catalog/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
	workerdeploy "github.com/project-ai-services/ai-services/internal/pkg/worker/deploy"
	"github.com/project-ai-services/ai-services/internal/pkg/worker/join"
)

const defaultJoinHTTPSPort = 443

// Flag variables for the worker join command.
var (
	token       string
	runtimeType string
	baseDir     string
	httpsPort   int
	domainName  string
	sslCertPath string
	sslKeyPath  string
)

var cmd = &cobra.Command{
	Use:   "join <gateway>",
	Short: "Join this node to the catalog as a worker",
	Long: `Deploys the Caddy reverse-proxy on this node, registers with the catalog
gRPC worker-gateway using the bootstrap token, and holds the connection open.

<gateway> is the host:port of the catalog gRPC worker-gateway.

The command runs until interrupted (Ctrl-C). Heartbeats are sent every
30 seconds so the control plane knows this worker is alive.

Obtain a token first by running on the catalog node:

  ai-services catalog worker register <name>`,
	Example: `  # Minimal — required argument + flag only
  ai-services worker join catalog.example.com:9090 --token <bootstrap-token>

  # Custom base directory and HTTPS port
  ai-services worker join catalog.example.com:9090 \
      --token      <bootstrap-token> \
      --basedir    /data/ai-services \
      --https-port 8443

  # Custom SSL certificate
  ai-services worker join catalog.example.com:9090 \
      --token    <bootstrap-token> \
      --ssl-cert /path/to/cert.pem \
      --ssl-key  /path/to/key.pem`,
	Args:    cobra.ExactArgs(1),
	PreRunE: joinPreRunE,
	RunE:    joinRunE,
}

func joinPreRunE(cmd *cobra.Command, _ []string) error {
	cmd.SilenceUsage = true

	if err := cmdcommon.InitAndValidateRuntimeFlag(runtimeType); err != nil {
		return err
	}

	if httpsPort < 1 || httpsPort > 65535 {
		return fmt.Errorf("invalid HTTPS port %d: must be between 1 and 65535", httpsPort)
	}

	return utils.ValidateSSLFlags(sslCertPath, sslKeyPath, domainName)
}

func joinRunE(_ *cobra.Command, args []string) error {
	aiServicesDir, err := utils.ValidateBaseDir(baseDir)
	if err != nil {
		return fmt.Errorf("invalid base directory %q: %w", baseDir, err)
	}

	if err := utils.CreateDir(filepath.Join(aiServicesDir, "models")); err != nil {
		return fmt.Errorf("failed to create model directory: %w", err)
	}

	opts := join.Options{
		GatewayAddr: args[0],
		Token:       token,
		RuntimeType: types.RuntimeType(runtimeType),
		Setup: workerdeploy.Options{
			BaseDir:     aiServicesDir,
			HTTPSPort:   httpsPort,
			DomainName:  domainName,
			SSLCertPath: catalogUtils.SanitizeFilePath(sslCertPath),
			SSLKeyPath:  catalogUtils.SanitizeFilePath(sslKeyPath),
		},
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	return join.Run(ctx, opts)
}

// configureFlags registers the flags shared by the join and grpcserver
// commands: --token (required), --runtime, --basedir, --https-port,
// --ssl-cert, and --ssl-key.
func configureFlags(c *cobra.Command) {
	c.Flags().StringVar(&token, "token", "",
		"Single-use bootstrap token issued by 'catalog worker register' (required).\n"+
			"Example: --token <uuid>\n")
	_ = c.MarkFlagRequired("token")

	cmdcommon.ConfigureRuntimeFlag(c, &runtimeType)

	c.Flags().StringVar(&baseDir, "basedir", "",
		"Base directory for AI services data (models, caddy, etc.) on this worker.\n"+
			"Defaults to "+constants.DefaultBaseDir+" when not specified.\n"+
			"Note: Supported for podman runtime only.\n"+
			"Example: --basedir /var/lib/ai-services\n")

	c.Flags().IntVar(&httpsPort, "https-port", defaultJoinHTTPSPort,
		"Custom HTTPS port to expose the service endpoints externally.\n"+
			"Note: Supported for podman runtime only.\n"+
			"Example: --https-port 8443\n")

	c.Flags().StringVar(&domainName, "domain-name", "",
		"Custom domain name for self-signed certificates.\n"+
			"If not provided, uses wildcard DNS format: <service>.<ip>.nip.io\n"+
			"If a custom SSL certificate/key pair is provided, the domain is extracted from the certificate and this flag is ignored.\n"+
			"Note: Supported for podman runtime only.\n"+
			"Example: --domain-name example.com\n")

	c.Flags().StringVar(&sslCertPath, "ssl-cert", "",
		"Path to user-provided SSL certificate (optional).\n"+
			"Must be used together with --ssl-key.\n"+
			"Certificate must contain wildcard SAN entry (e.g., *.example.com).\n"+
			"Note: Supported for podman runtime only.\n"+
			"Example: --ssl-cert /path/to/cert.pem\n")

	c.Flags().StringVar(&sslKeyPath, "ssl-key", "",
		"Path to user-provided SSL private key (optional).\n"+
			"Must be used together with --ssl-cert.\n"+
			"Note: Supported for podman runtime only.\n"+
			"Example: --ssl-key /path/to/key.pem\n")
}

func newJoinCmd() *cobra.Command {
	configureFlags(cmd)

	return cmd
}

var grpcStreamCmd = &cobra.Command{
	Use:    "grpcstream <gateway>",
	Short:  "Connect to the catalog gRPC worker-gateway",
	Hidden: true,
	Args:   cobra.ExactArgs(1),
	PreRunE: func(cmd *cobra.Command, _ []string) error {
		cmd.SilenceUsage = true

		return cmdcommon.InitAndValidateRuntimeFlag(runtimeType)
	},
	RunE: grpcStreamRunE,
}

func grpcStreamRunE(_ *cobra.Command, args []string) error {
	opts := join.Options{
		GatewayAddr: args[0],
		Token:       token,
		RuntimeType: types.RuntimeType(runtimeType),
		Setup: workerdeploy.Options{
			BaseDir:     baseDir,
			HTTPSPort:   httpsPort,
			DomainName:  domainName,
			SSLCertPath: catalogUtils.SanitizeFilePath(sslCertPath),
			SSLKeyPath:  catalogUtils.SanitizeFilePath(sslKeyPath),
		},
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	return join.RunGrpcStream(ctx, opts)
}

func newGrpcStreamCmd() *cobra.Command {
	configureFlags(grpcStreamCmd)

	return grpcStreamCmd
}
