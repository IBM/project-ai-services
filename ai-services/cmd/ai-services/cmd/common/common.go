// Package common provides CLI helpers shared across all top-level commands
// (catalog, worker, bootstrap, etc.).
package common

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/project-ai-services/ai-services/internal/pkg/bootstrap"
	"github.com/project-ai-services/ai-services/internal/pkg/cli/helpers"
	"github.com/project-ai-services/ai-services/internal/pkg/cli/flagvalidator"
	"github.com/project-ai-services/ai-services/internal/pkg/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/vars"
)

const (
	ResetPasswordFlag   = "reset-password"
	ResetPodmanAuthFlag = "reset-podman-auth"
	ResetSSLCertFlag    = "reset-certificate"
)

// InitAndValidateRuntimeFlag validates the runtime flag value, initialises
// vars.RuntimeFactory, and checks platform support. It must be called in
// PreRunE before any code that reads vars.RuntimeFactory.
func InitAndValidateRuntimeFlag(runtimeType string) error {
	rt := types.RuntimeType(runtimeType)
	if !rt.Valid() {
		return fmt.Errorf("invalid runtime type: %s (must be 'podman' or 'openshift'). Please specify runtime using --runtime flag", runtimeType)
	}

	vars.RuntimeFactory = runtime.NewRuntimeFactory(rt)
	logger.Debugf("Using runtime: %s\n", rt)

	if err := utils.CheckPodmanPlatformSupport(rt); err != nil {
		return err
	}

	return validateRuntimeType(rt)
}

// ConfigureRuntimeFlag registers the --runtime / -r flag on cmd and marks it
// required. Use this in every command that accepts a runtime type.
func ConfigureRuntimeFlag(cmd *cobra.Command, runtimeType *string) {
	cmd.Flags().StringVarP(runtimeType, constants.RuntimeFlag, "r", "",
		fmt.Sprintf("runtime to use (options: %s, %s) (required)", types.RuntimeTypePodman, types.RuntimeTypeOpenShift))
	_ = cmd.MarkFlagRequired(constants.RuntimeFlag)
}

func validateRuntimeType(runtimeType types.RuntimeType) error {
	switch runtimeType {
	case types.RuntimeTypePodman, types.RuntimeTypeOpenShift:
		return nil
	default:
		return fmt.Errorf("unsupported runtime type: %s", runtimeType)
	}
}

// ValidateSkipChecksFlag validates the skip-validation flag for the current runtime.
func ValidateSkipChecksFlag(cmd *cobra.Command) error {
	skipChecks, err := cmd.Flags().GetStringSlice("skip-validation")
	if err != nil {
		return err
	}
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

// DoBootstrapValidate runs the bootstrap validation checks for the active runtime, skipping any requested checks.
func DoBootstrapValidate(ctx context.Context, skipChecks []string) error {
	skip := helpers.ParseSkipChecks(skipChecks)
	if len(skip) > 0 {
		logger.Warningf("Skipping validation checks (skipped: %v)\n", skipChecks)
	}

	factory := bootstrap.NewBootstrapFactory(vars.RuntimeFactory.GetRuntimeType())
	if err := factory.Validate(ctx, skip); err != nil {
		return fmt.Errorf("bootstrap validation failed: %w", err)
	}

	return nil
}

func ValidateResetFlag(cmd *cobra.Command, flagName string, skipFlags ...string) error {
	// Check that no configuration parameters are provided with reset flag
	var invalidFlags []string
	cmd.Flags().Visit(func(f *pflag.Flag) {
		if f.Name == flagName || f.Name == constants.RuntimeFlag {
			// Skip reset flag and runtime parameter
			return
		}
		for _, skip := range skipFlags {
			if f.Name == skip {
				return
			}
		}
		invalidFlags = append(invalidFlags, "--"+f.Name)
	})
	if len(invalidFlags) > 0 {
		return fmt.Errorf("the following flags cannot be used with --%s: %v", flagName, invalidFlags)
	}

	return nil
}

// ConfigurePodmanDeployFlags registers the --basedir, --https-port, --ssl-cert,
// --ssl-key, and --domain-name flags on cmd. It is shared by commands that deploy
// a podman-backed service (e.g. "worker join" and "catalog configure").
// defaultHTTPSPort is used as the default value for --https-port.
func ConfigurePodmanDeployFlags(cmd *cobra.Command, baseDir *string, httpsPort *int, defaultHTTPSPort int, sslCertPath, sslKeyPath, domainName *string) {
	cmd.Flags().StringVar(baseDir, BaseDirFlag, "",
		"Base directory for AI services data (models, caddy).\n"+
			"Note: Supported for podman runtime only.\n"+
			"Example: --basedir /var/lib/ai-services\n")

	cmd.Flags().IntVar(httpsPort, HTTPSPortFlag, defaultHTTPSPort,
		"Custom HTTPS port to expose the service endpoints externally.\n"+
			"Note: Supported for podman runtime only.\n"+
			"Example: --https-port 8443\n")

	cmd.Flags().StringVar(domainName, DomainNameFlag, "",
		"Custom domain name for self-signed certificates.\n"+
			"If not provided, uses wildcard DNS format: <service>.<ip>.nip.io\n"+
			"If a custom SSL certificate/key pair is provided, the domain is extracted from the certificate and this flag is ignored.\n"+
			"Note: Supported for podman runtime only.\n"+
			"Example: --domain-name example.com\n")

	ConfigureSSLFlags(cmd, sslCertPath, sslKeyPath)
}

func ConfigureSSLFlags(cmd *cobra.Command, sslCertPath, sslKeyPath *string) {
	cmd.Flags().StringVar(sslCertPath, SSLCertFlag, "",
		"Path to user-provided SSL certificate (optional).\n"+
			"Must be used together with --ssl-key.\n"+
			"Certificate must contain wildcard SAN entry (e.g., *.example.com).\n"+
			"Note: Supported for podman runtime only.\n"+
			"Example: --ssl-cert /path/to/cert.pem\n")

	cmd.Flags().StringVar(sslKeyPath, SSLKeyFlag, "",
		"Path to user-provided SSL private key (optional).\n"+
			"Must be used together with --ssl-cert.\n"+
			"Note: Supported for podman runtime only.\n"+
			"Example: --ssl-key /path/to/key.pem\n")
}

// ConfigurePodmanResetFlags registers the --reset-podman-auth and --reset-certificate
// boolean flags on cmd. It is shared by commands that support both reset operations
// (e.g. "worker reset" and "catalog configure").
func ConfigurePodmanResetFlags(cmd *cobra.Command, resetPodmanAuth, resetCertificate *bool) {
	cmd.Flags().BoolVar(
		resetPodmanAuth,
		ResetPodmanAuthFlag,
		false,
		"Reset podman authentication using the system's current auth.json.\n"+
			"Note: Supported for podman runtime only.\n",
	)

	cmd.Flags().BoolVar(
		resetCertificate,
		ResetSSLCertFlag,
		false,
		"Reset the Caddy SSL certificates by loading new custom certificates.\n"+
			"Requires --ssl-cert and --ssl-key flags to specify the new certificate files.\n"+
			"This will reload the certificates in Caddy without restarting the pod.\n"+
			"Note: Supported for podman runtime only.\n",
	)
}

func ValidateResetCertificateFlags(cmd *cobra.Command, flagName, sslCertPath, sslKeyPath, domainName string, skipFlags ...string) error {
	// Require SSL certificate flags with reset-certificate
	if sslCertPath == "" || sslKeyPath == "" {
		return fmt.Errorf("--ssl-cert and --ssl-key are required when using --reset-certificate")
	}

	if err := utils.ValidateSSLFlags(sslCertPath, sslKeyPath, domainName); err != nil {
		return err
	}

	// Check that no other configuration parameters are provided with reset-certificate flag
	// Allow ssl-cert and ssl-key since they are required for this operation
	allSkipFlags := append([]string{"ssl-cert", "ssl-key"}, skipFlags...)

	return ValidateResetFlag(cmd, flagName, allSkipFlags...)
}

// BuildFlagValidator is a generic helper that constructs a FlagValidator from
// the provided per-scope flag name slices.
func BuildFlagValidator(commonFlags, podmanFlags, openShiftFlags []string) *flagvalidator.FlagValidator {
	rt := vars.RuntimeFactory.GetRuntimeType()
	builder := flagvalidator.NewFlagValidatorBuilder(rt)

	for _, f := range commonFlags {
		builder.AddCommonFlag(f, nil)
	}
	for _, f := range podmanFlags {
		builder.AddPodmanFlag(f, nil)
	}
	for _, f := range openShiftFlags {
		builder.AddOpenShiftFlag(f, nil)
	}

	return builder.Build()
}
