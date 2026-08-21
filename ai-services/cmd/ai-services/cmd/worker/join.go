package worker

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	cmdcommon "github.com/project-ai-services/ai-services/cmd/ai-services/cmd/common"
	"github.com/project-ai-services/ai-services/internal/pkg/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/podman"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
	workercaddy "github.com/project-ai-services/ai-services/internal/pkg/worker/caddy"
	"github.com/project-ai-services/ai-services/internal/pkg/worker/join"
)

const defaultJoinHTTPSPort = "443"

func newJoinCmd() *cobra.Command {
	var (
		token       string
		runtimeType string
		baseDir     string
		httpsPort   string
	)

	cmd := &cobra.Command{
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
      --https-port 8443`,
		Args:    cobra.ExactArgs(1),
		PreRunE: joinPreRunE(&runtimeType),
		RunE:    joinRunE(&token, &runtimeType, &baseDir, &httpsPort),
	}

	addJoinFlags(cmd, &token, &runtimeType, &baseDir, &httpsPort)

	return cmd
}

func joinPreRunE(runtimeType *string) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, _ []string) error {
		cmd.SilenceUsage = true

		return cmdcommon.InitAndValidateRuntimeFlag(*runtimeType)
	}
}

func joinRunE(token, runtimeType, baseDir, httpsPort *string) func(*cobra.Command, []string) error {
	return func(_ *cobra.Command, args []string) error {
		rt, err := podman.NewPodmanClient()
		if err != nil {
			return fmt.Errorf("init podman client: %w", err)
		}

		opts := join.Options{
			GatewayAddr: args[0],
			Token:       *token,
			RuntimeType: types.RuntimeType(*runtimeType),
			Caddy: workercaddy.SetupOptions{
				BaseDir:   *baseDir,
				HTTPSPort: *httpsPort,
			},
		}

		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()

		logger.Infoln("Starting worker join — press Ctrl-C to stop.")

		return join.Run(ctx, rt, opts)
	}
}

func addJoinFlags(cmd *cobra.Command, token, runtimeType, baseDir, httpsPort *string) {
	cmd.Flags().StringVar(token, "token", "",
		"Single-use bootstrap token issued by 'catalog worker register' (required).\n"+
			"Example: --token <uuid>\n")
	_ = cmd.MarkFlagRequired("token")

	cmdcommon.ConfigureRuntimeFlag(cmd, runtimeType)

	cmd.Flags().StringVar(baseDir, "basedir", constants.DefaultBaseDir,
		"Base directory for AI services data (models, caddy) on this worker.\n"+
			"Note: Supported for podman runtime only.\n"+
			"Example: --basedir /var/lib/ai-services\n")

	cmd.Flags().StringVar(httpsPort, "https-port", defaultJoinHTTPSPort,
		"Custom HTTPS port to expose the service endpoints externally.\n"+
			"Note: Supported for podman runtime only.\n"+
			"Example: --https-port 8443\n")
}
