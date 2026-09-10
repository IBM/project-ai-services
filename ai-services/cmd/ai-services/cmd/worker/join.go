package worker

import (
	"context"
	"fmt"
	"net"
	"strings"

	"github.com/spf13/cobra"

	appBootstrap "github.com/project-ai-services/ai-services/cmd/ai-services/cmd/bootstrap"
	cmdcommon "github.com/project-ai-services/ai-services/cmd/ai-services/cmd/common"
	catalogUtils "github.com/project-ai-services/ai-services/internal/pkg/catalog/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/cli/flagvalidator"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/vars"
	workercaddy "github.com/project-ai-services/ai-services/internal/pkg/worker/caddy"
	workercommon "github.com/project-ai-services/ai-services/internal/pkg/worker/common"
	workerconstants "github.com/project-ai-services/ai-services/internal/pkg/worker/constants"
	workerdeploy "github.com/project-ai-services/ai-services/internal/pkg/worker/deploy"

	"github.com/project-ai-services/ai-services/internal/pkg/worker/join"
	workertypes "github.com/project-ai-services/ai-services/internal/pkg/worker/types"
)

const (
	defaultJoinHTTPSPort = 443
	hostAliasSplitParts  = 2
)

// Flag variables for the worker join command.
var (
	// common flags.
	token       string
	runtimeType string
	skipChecks  []string

	// podman flags.
	baseDir     string
	httpsPort   int
	domainName  string
	sslCertPath string
	sslKeyPath  string
	addHosts    []string
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
      --ssl-key  /path/to/key.pem

  # Skip specific bootstrap validation checks
  ai-services worker join catalog.example.com:9090 \
      --token           <bootstrap-token> \
      --skip-validation rhn,power`,
	Args:    cobra.ExactArgs(1),
	PreRunE: joinPreRunE,
	RunE:    joinRunE,
}

func joinPreRunE(cmd *cobra.Command, _ []string) error {
	cmd.SilenceUsage = true

	if err := cmdcommon.InitAndValidateRuntimeFlag(runtimeType); err != nil {
		return err
	}

<<<<<<< HEAD
	if err := cmdcommon.ValidateSkipChecksFlag(cmd); err != nil {
		return err
	}

=======
	// Reject runtime-scoped flags early.
	if err := buildWorkerFlagValidator().Validate(cmd); err != nil {
		return err
	}

	return validateWorkerJoinFlags(cmd.Context())
}

// buildWorkerFlagValidator registers every worker join flag with its runtime scope.
func buildWorkerFlagValidator() *flagvalidator.FlagValidator {
	return cmdcommon.BuildFlagValidator(
		[]string{cmdcommon.TokenFlag},
		[]string{cmdcommon.BaseDirFlag, cmdcommon.HTTPSPortFlag, cmdcommon.DomainNameFlag, cmdcommon.SSLCertFlag, cmdcommon.SSLKeyFlag},
		nil,
	)
}

func validateWorkerJoinFlags(ctx context.Context) error {
	if token == "" {
		return fmt.Errorf("required flag(s) %q not set", cmdcommon.TokenFlag)
	}

>>>>>>> 5cc23ee6 (Supporting reset flags)
	if httpsPort < 1 || httpsPort > 65535 {
		return fmt.Errorf("invalid HTTPS port %d: must be between 1 and 65535", httpsPort)
	}

	if err := utils.ValidateSSLFlags(sslCertPath, sslKeyPath, domainName); err != nil {
		return err
	}

	for _, h := range addHosts {
		if err := validateAddHost(h); err != nil {
			return err
		}
	}

	return checkNotLocalWorker(ctx)
}

// checkNotLocalWorker returns an error when this node is co-located with the
// catalog control plane, which means it cannot be managed as a standalone worker.
func checkNotLocalWorker(ctx context.Context) error {
	rtType := vars.RuntimeFactory.GetRuntimeType()
	rt, err := runtime.CreateRuntime(rtType, workerconstants.WorkerAppName)
	if err != nil {
		return fmt.Errorf("worker join: init runtime: %w", err)
	}

	var localWorker bool

	switch rtType {
	case types.RuntimeTypeOpenShift:
		localWorker, err = workercommon.IsOpenShiftLocalWorker(ctx, rt)
	default:
		localWorker, err = workercommon.IsPodmanLocalWorker(ctx, rt)
	}

	if err != nil {
		return fmt.Errorf("could not determine LOCAL_WORKER from catalog pod: %w", err)
	}
	if localWorker {
		return fmt.Errorf("the worker is already co-located with the control plane and cannot be joined independently")
	}

	return nil
}

// validateAddHost checks that an --add-host value has the form DOMAIN:IP.
func validateAddHost(h string) error {
	parts := strings.SplitN(h, ":", hostAliasSplitParts)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return fmt.Errorf("invalid --add-host value %q: expected DOMAIN:IP", h)
	}

	if net.ParseIP(parts[1]) == nil {
		return fmt.Errorf("invalid --add-host value %q: %q is not a valid IP address", h, parts[1])
	}

	return nil
}

// parseAddHosts converts the raw --add-host strings into HostAlias structs,
// merging multiple hostnames that share the same IP into a single entry.
func parseAddHosts(raw []string) []workertypes.HostAlias {
	byIP := make(map[string][]string, len(raw))
	order := make([]string, 0, len(raw))

	for _, h := range raw {
		parts := strings.SplitN(h, ":", hostAliasSplitParts)
		domain, ip := parts[0], parts[1]

		if _, seen := byIP[ip]; !seen {
			order = append(order, ip)
		}

		byIP[ip] = append(byIP[ip], domain)
	}

	aliases := make([]workertypes.HostAlias, 0, len(order))
	for _, ip := range order {
		aliases = append(aliases, workertypes.HostAlias{IP: ip, Hostnames: byIP[ip]})
	}

	return aliases
}

// joinRunE provisions the worker node for the given runtime type and returns once
// the deployment is complete.
//
// After DeployWorker returns successfully, 'grpcstream' cmd should be called to open
// the long-lived CommandStream to the catalog control plane.
func joinRunE(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	sslCertPath := catalogUtils.SanitizeFilePath(sslCertPath)
	sslKeyPath := catalogUtils.SanitizeFilePath(sslKeyPath)

	gatewayAddr := args[0]

<<<<<<< HEAD
	if err := cmdcommon.DoBootstrapValidate(ctx, skipChecks); err != nil {
		return err
	}

	switch types.RuntimeType(runtimeType) {
	case types.RuntimeTypePodman:
		aiServicesDir, err := utils.ValidateBaseDir(baseDir)
		if err != nil {
			return fmt.Errorf("invalid base directory %q: %w", baseDir, err)
		}

		if err := utils.CreateDir(filepath.Join(aiServicesDir, "models")); err != nil {
			return fmt.Errorf("failed to create model directory: %w", err)
		}

		opts := workertypes.PodmanWorkerOptions{
			WorkerConnectionOptions: workertypes.WorkerConnectionOptions{
				GatewayAddr: gatewayAddr,
				Token:       token,
			},
			Setup: workertypes.Options{
				CommonWorkerOptions: workertypes.CommonWorkerOptions{
					HostAliases: parseAddHosts(addHosts),
				},
				BaseDir:     aiServicesDir,
				HTTPSPort:   httpsPort,
				DomainName:  domainName,
				SSLCertPath: catalogUtils.SanitizeFilePath(sslCertPath),
				SSLKeyPath:  catalogUtils.SanitizeFilePath(sslKeyPath),
			},
		}

		// Setup worker node
		if err := workerpodman.DeployWorker(ctx, opts); err != nil {
			return fmt.Errorf("failed to deploy worker: %w", err)
		}
	case types.RuntimeTypeOpenShift:
		opts := workertypes.OpenshiftWorkerOptions{
			WorkerConnectionOptions: workertypes.WorkerConnectionOptions{
				GatewayAddr: gatewayAddr,
				Token:       token,
			},
=======
	return workerdeploy.DeployWorker(ctx, workertypes.DeployOpts{
		WorkerConnectionOptions: workertypes.WorkerConnectionOptions{
			Token:       token,
			GatewayAddr: gatewayAddr,
		},
		Options: workertypes.Options{
>>>>>>> 5cc23ee6 (Supporting reset flags)
			CommonWorkerOptions: workertypes.CommonWorkerOptions{
				HostAliases: parseAddHosts(addHosts),
			},
			BaseDir:     baseDir,
			HTTPSPort:   httpsPort,
			DomainName:  domainName,
			SSLCertPath: sslCertPath,
			SSLKeyPath:  sslKeyPath,
		},
		RuntimeType: runtimeType,
	})
}

// configureFlags registers the flags shared by the join and grpcstream
// commands: --token, --runtime, --basedir, --https-port,
// --ssl-cert, and --ssl-key.
// requireToken controls whether --token is marked as a required flag.
func configureFlags(c *cobra.Command, requireToken bool) {
	c.Flags().StringVar(&token, cmdcommon.TokenFlag, "",
		"Single-use bootstrap token issued by 'catalog worker register' (required).\n"+
			"Example: --token <uuid>\n")
	if requireToken {
		_ = c.MarkFlagRequired(cmdcommon.TokenFlag)
	}

	cmdcommon.ConfigureRuntimeFlag(c, &runtimeType)

	skipCheckDesc := appBootstrap.BuildSkipFlagDescription()
	c.Flags().StringSliceVar(&skipChecks, "skip-validation", []string{}, skipCheckDesc)
}

func initJoinPodmanFlags(c *cobra.Command) {
	c.Flags().StringVar(&baseDir, "basedir", "",
		"Base directory for AI services data (models, caddy, etc.) on this worker.\n"+
			"Defaults to "+constants.DefaultBaseDir+" when not specified.\n"+
			"Note: Supported for podman runtime only.\n"+
			"Example: --basedir /var/lib/ai-services\n")
	cmdcommon.ConfigurePodmanDeployFlags(c, &baseDir, &httpsPort, defaultJoinHTTPSPort, &sslCertPath, &sslKeyPath, &domainName)

	c.Flags().StringArrayVar(&addHosts, cmdcommon.AddHostFlag, nil,
		"Add an extra entry to the worker pod's /etc/hosts (repeatable).\n"+
			"Format: DOMAIN:IP\n"+
			"Note: Supported for podman runtime only.\n"+
			"Example: --add-host catalog-worker-gateway.example.com:10.20.188.75\n")
}

func newJoinCmd() *cobra.Command {
	configureFlags(cmd, true)

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

// grpcStreamRunE starts the long-lived gRPC CommandStream for the worker.
// It is called inside the worker pod after the deploy step (Run) has completed.
//
// For Podman workers it initialises the Podman runtime and builds the local
// Caddy proxy router before opening the stream.
//
// For OpenShift workers it initialises the runtime scoped to the worker
// namespace and opens the stream, since routing is handled natively by the platform.
func grpcStreamRunE(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	gatewayAddr := args[0]

	var pr *workercaddy.ProxyRouter
	var rt runtime.Runtime

	switch types.RuntimeType(runtimeType) {
	case types.RuntimeTypePodman:
		var err error
		rt, err = runtime.CreateRuntime(types.RuntimeTypePodman, "")
		if err != nil {
			return fmt.Errorf("worker grpcstream: init runtime: %w", err)
		}

		// ── Build Caddy proxy router (Podman only) ──────────────────────
		// Must happen after Setup so the Caddy pod is running and its admin port
		// is discoverable. For OpenShift workers routes are managed natively.
		pr, err = workercaddy.NewProxyRouter(ctx)
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

	err := join.StartGrpcStream(ctx, rt, pr, opts)
	if err != nil {
		logger.ErrorfCtx(ctx, "%s: %v\n", workerconstants.WorkerJoinErr, err)

		return fmt.Errorf("%s: %w", workerconstants.WorkerJoinErr, err)
	}

	return nil
}

func newGrpcStreamCmd() *cobra.Command {
	configureFlags(grpcStreamCmd, false)

	return grpcStreamCmd
}
