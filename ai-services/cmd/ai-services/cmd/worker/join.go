package worker

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	cmdcommon "github.com/project-ai-services/ai-services/cmd/ai-services/cmd/common"
	catalogUtils "github.com/project-ai-services/ai-services/internal/pkg/catalog/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
	workercaddy "github.com/project-ai-services/ai-services/internal/pkg/worker/caddy"
	workerconstants "github.com/project-ai-services/ai-services/internal/pkg/worker/constants"
	workeropenshift "github.com/project-ai-services/ai-services/internal/pkg/worker/deploy/openshift"
	workerpodman "github.com/project-ai-services/ai-services/internal/pkg/worker/deploy/podman"
	"github.com/project-ai-services/ai-services/internal/pkg/worker/join"
	workertypes "github.com/project-ai-services/ai-services/internal/pkg/worker/types"
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

func joinRunE(cmd *cobra.Command, args []string) error {
	return configureWorker(cmd.Context(), args[0])
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

// configureWorker handles the deploy/setup phase of the worker join workflow.
// It provisions the worker node for the given runtime type and returns once
// the deployment is complete.
//
// After Run returns successfully, RunGrpcStream should be called to open
// the long-lived CommandStream to the catalog control plane.
func configureWorker(ctx context.Context, gatewayAddr string) error {
	switch types.RuntimeType(runtimeType) {
	case types.RuntimeTypePodman:
		opts := workertypes.PodmanWorkerOptions{
			WorkerConnectionOptions: workertypes.WorkerConnectionOptions{
				GatewayAddr: gatewayAddr,
				Token:       token,
			},
			Setup: workertypes.Options{
				BaseDir:     baseDir,
				HTTPSPort:   httpsPort,
				DomainName:  domainName,
				SSLCertPath: catalogUtils.SanitizeFilePath(sslCertPath),
				SSLKeyPath:  catalogUtils.SanitizeFilePath(sslKeyPath),
			},
		}

		// Setup worker node
		if err := workerpodman.DeployWorker(ctx, opts); err != nil {
			return fmt.Errorf("worker join: setup: %w", err)
		}
	case types.RuntimeTypeOpenShift:
		opts := workertypes.OpenshiftWorkerOptions{
			WorkerConnectionOptions: workertypes.WorkerConnectionOptions{
				GatewayAddr: gatewayAddr,
				Token:       token,
			},
		}
		if err := workeropenshift.DeployWorker(ctx, opts); err != nil {
			return fmt.Errorf("worker join: failed to install worker helm chart: %w", err)
		}
	default:
		return fmt.Errorf("unsupported runtime type: %s", runtimeType)
	}

	return nil
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
	return runGrpcStream(cmd.Context(), args[0])
}

func newGrpcStreamCmd() *cobra.Command {
	configureFlags(grpcStreamCmd)

	return grpcStreamCmd
}

// runGrpcStream starts the long-lived gRPC CommandStream for the worker.
// It is called inside the worker pod after the deploy step (Run) has completed.
//
// For Podman workers it computes the domain suffix from the TLS credentials,
// builds the metadata map that the control plane needs to route traffic back to
// this node, and initialises the Podman runtime before opening the stream.
//
// For OpenShift workers it initialises the runtime scoped to the worker
// namespace and opens the stream with an empty metadata map, since routing is
// handled natively by the platform.
func runGrpcStream(ctx context.Context, gatewayAddr string) error {
	var pr *workercaddy.ProxyRouter
	var rt runtime.Runtime
	var meta = make(map[string]string)

	switch types.RuntimeType(runtimeType) {
	case types.RuntimeTypePodman:
		domainSuffix, err := utils.ComputeDomainSuffix(sslCertPath, sslKeyPath, domainName)
		if err != nil {
			return err
		}
		meta = map[string]string{
			workerconstants.MetaKeyBaseDir:      baseDir,
			workerconstants.MetaKeyDomainSuffix: domainSuffix,
			workerconstants.MetaKeyHTTPSPort:    fmt.Sprintf("%d", httpsPort),
		}

		rt, err = runtime.CreateRuntime(types.RuntimeTypePodman, "")
		if err != nil {
			return fmt.Errorf("worker grpcstream: init runtime: %w", err)
		}

		// ── Build Caddy proxy router (Podman only) ──────────────────────
		// Must happen after Setup so the Caddy pod is running and its admin port
		// is discoverable. For OpenShift workers routes are managed natively.
		pr, err = workercaddy.New(ctx, rt)
		if err != nil {
			return fmt.Errorf("worker grpcstream: init local Caddy manager: %w", err)
		}
	case types.RuntimeTypeOpenShift:
		var err error
		rt, err = runtime.CreateRuntime(types.RuntimeTypeOpenShift, workerconstants.WorkerAppName)
		if err != nil {
			return fmt.Errorf("worker grpcstream: init runtime: %w", err)
		}

	default:
		return fmt.Errorf("unsupported runtime type: %s", runtimeType)
	}

	opts := workertypes.GrpcStreamOptions{
		WorkerConnectionOptions: workertypes.WorkerConnectionOptions{
			GatewayAddr: gatewayAddr,
			Token:       token,
		},
	}
	if err := join.StartGrpcStream(ctx, rt, pr, opts, meta); err != nil {
		return fmt.Errorf("worker grpcstream: setup: %w", err)
	}

	return nil
}
