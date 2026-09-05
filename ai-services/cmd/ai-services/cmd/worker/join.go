package worker

import (
	"context"
	"fmt"
	"net"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	appBootstrap "github.com/project-ai-services/ai-services/cmd/ai-services/cmd/bootstrap"
	cmdcommon "github.com/project-ai-services/ai-services/cmd/ai-services/cmd/common"
	"github.com/project-ai-services/ai-services/internal/pkg/bootstrap"
	catalogUtils "github.com/project-ai-services/ai-services/internal/pkg/catalog/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/cli/helpers"
	"github.com/project-ai-services/ai-services/internal/pkg/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/vars"
	workercaddy "github.com/project-ai-services/ai-services/internal/pkg/worker/caddy"
	workerconstants "github.com/project-ai-services/ai-services/internal/pkg/worker/constants"
	workeropenshift "github.com/project-ai-services/ai-services/internal/pkg/worker/deploy/openshift"
	workerpodman "github.com/project-ai-services/ai-services/internal/pkg/worker/deploy/podman"
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

	if err := validateSkipChecksFlag(cmd); err != nil {
		return err
	}

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

func validateSkipChecksFlag(cmd *cobra.Command) error {
	if len(skipChecks) == 0 {
		return nil
	}

	validChecks := make(map[string]bool, len(bootstrap.GetRulesForRuntime()))
	for _, r := range bootstrap.GetRulesForRuntime() {
		validChecks[r.Name()] = true
	}

	for _, s := range skipChecks {
		if !validChecks[s] {
			return fmt.Errorf("invalid skip-validation value '%s' for runtime '%s'", s, vars.RuntimeFactory.GetRuntimeType())
		}
	}

	return nil
}

func doBootstrapValidate(ctx context.Context) error {
	skip := helpers.ParseSkipChecks(skipChecks)
	if len(skip) > 0 {
		logger.Warningf("Skipping validation checks (skipped: %v)\n", skipChecks)
	}

	// Create bootstrap instance based on runtime
	factory := bootstrap.NewBootstrapFactory(vars.RuntimeFactory.GetRuntimeType())

	if err := factory.Validate(ctx, skip); err != nil {
		return fmt.Errorf("bootstrap validation failed: %w", err)
	}

	return nil
}

// joinRunE provisions the worker node for the given runtime type and returns once
// the deployment is complete.
//
// After DeployWorker returns successfully, 'grpcstream' cmd should be called to open
// the long-lived CommandStream to the catalog control plane.
func joinRunE(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	gatewayAddr := args[0]

	if err := doBootstrapValidate(ctx); err != nil {
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
				BaseDir:     aiServicesDir,
				HTTPSPort:   httpsPort,
				DomainName:  domainName,
				SSLCertPath: catalogUtils.SanitizeFilePath(sslCertPath),
				SSLKeyPath:  catalogUtils.SanitizeFilePath(sslKeyPath),
				HostAliases: parseAddHosts(addHosts),
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

// configureFlags registers the flags shared by the join and grpcstream commands.
func configureFlags(c *cobra.Command) {
	initJoinCommonFlags(c)
	initJoinPodmanFlags(c)
}

func initJoinCommonFlags(c *cobra.Command) {
	c.Flags().StringVar(&token, "token", "",
		"Single-use bootstrap token issued by 'catalog worker register' (required).\n"+
			"Example: --token <uuid>\n")
	_ = c.MarkFlagRequired("token")

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

	c.Flags().StringArrayVar(&addHosts, "add-host", nil,
		"Add an extra entry to the worker pod's /etc/hosts (repeatable).\n"+
			"Format: DOMAIN:IP\n"+
			"Note: Supported for podman runtime only.\n"+
			"Example: --add-host catalog.example.com:10.20.188.75\n")
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

	return join.StartGrpcStream(ctx, rt, pr, opts)
}

func newGrpcStreamCmd() *cobra.Command {
	configureFlags(grpcStreamCmd)

	return grpcStreamCmd
}
