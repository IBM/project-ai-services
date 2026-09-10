package worker

import (
	"fmt"

	"github.com/spf13/cobra"

	cmdcommon "github.com/project-ai-services/ai-services/cmd/ai-services/cmd/common"
	catalogUtils "github.com/project-ai-services/ai-services/internal/pkg/catalog/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/cli/flagvalidator"
	"github.com/project-ai-services/ai-services/internal/pkg/constants"
	workerpodman "github.com/project-ai-services/ai-services/internal/pkg/worker/deploy/podman"
)

var (
	// Reset podman auth secret for worker reset command.
	resetPodmanAuthFlag bool
	// Reset certificate flag for worker reset command.
	resetCertificateFlag bool
)

var resetCmd = &cobra.Command{
	Use:   "reset",
	Short: "Reset this worker node",
	Long: `Reset a deployed worker node without re-joining the catalog.

Supported operations:
  --reset-podman-auth    Re-applies the host's current auth.json to the worker pod.
  --reset-certificate    Replaces the Caddy SSL certificate with new cert/key files.

Note: Supported for podman runtime only.`,
	Example: `  # Reset podman authentication
  ai-services worker reset --runtime podman --reset-podman-auth

  # Replace SSL certificate
  ai-services worker reset --runtime podman --reset-certificate \
      --ssl-cert /path/to/cert.pem --ssl-key /path/to/key.pem`,
	Args: cobra.NoArgs,
	PreRunE: func(cmd *cobra.Command, _ []string) error {
		cmd.SilenceUsage = true

		if err := cmdcommon.InitAndValidateRuntimeFlag(runtimeType); err != nil {
			return err
		}

		if err := buildWorkerResetFlagValidator().Validate(cmd); err != nil {
			return err
		}

		if resetPodmanAuthFlag {
			return cmdcommon.ValidateResetFlag(cmd, constants.ResetPodmanAuthFlag)
		} else if resetCertificateFlag {
			return cmdcommon.ValidateResetCertificateFlags(cmd, constants.ResetSSLCertFlag, sslCertPath, sslKeyPath, "")
		}

		return fmt.Errorf("at least one of --reset-podman-auth or --reset-certificate must be specified")
	},
	RunE: func(cmd *cobra.Command, _ []string) error {
		ctx := cmd.Context()

		if resetPodmanAuthFlag {
			return workerpodman.ResetPodmanAuth(ctx)
		} else if resetCertificateFlag {
			return workerpodman.ResetWorkerCertificate(ctx,
				catalogUtils.SanitizeFilePath(sslCertPath),
				catalogUtils.SanitizeFilePath(sslKeyPath),
			)
		}

		return nil
	},
}

// buildWorkerResetFlagValidator registers reset flags with their runtime scope.
func buildWorkerResetFlagValidator() *flagvalidator.FlagValidator {
	return cmdcommon.BuildFlagValidator(
		nil,
		[]string{constants.ResetPodmanAuthFlag, constants.ResetSSLCertFlag, constants.SSLCertFlag, constants.SSLKeyFlag},
		nil,
	)
}

func newResetCmd() *cobra.Command {
	initWorkerResetFlags(resetCmd)

	return resetCmd
}

func initWorkerResetFlags(c *cobra.Command) {
	cmdcommon.ConfigureRuntimeFlag(c, &runtimeType)
	cmdcommon.ConfigurePodmanResetFlags(c, &resetPodmanAuthFlag, &resetCertificateFlag)

	cmdcommon.ConfigureSSLFlags(c, &sslCertPath, &sslKeyPath)
}
