package application

import (
	"fmt"
	"strings"

	"github.com/project-ai-services/ai-services/internal/pkg/application"
	appTypes "github.com/project-ai-services/ai-services/internal/pkg/application/types"
	catalogClient "github.com/project-ai-services/ai-services/internal/pkg/catalog/client"
	appFlags "github.com/project-ai-services/ai-services/internal/pkg/cli/constants/application"
	"github.com/project-ai-services/ai-services/internal/pkg/cli/flagvalidator"
	cliUtils "github.com/project-ai-services/ai-services/internal/pkg/cli/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/vars"
	"github.com/spf13/cobra"
)

var (
	output         string
	experimentalPs bool
)

func isOutputWide() bool {
	return strings.ToLower(output) == "wide"
}

var psCmd = &cobra.Command{
	Use:   "ps [name]",
	Short: "Lists all or specified running application(s)",
	Long: `Retrieves information about all the running applications if no name is provided
Lists information about a specific application if the name is provided
Arguments
  [name]: Application name (optional)
`,
	Args: cobra.MaximumNArgs(1),
	PreRunE: func(cmd *cobra.Command, args []string) error {
		// Build and run flag validator
		flagValidator := buildPsFlagValidator()

		return flagValidator.Validate(cmd)
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		// Once precheck passes, silence usage for any *later* internal errors.
		cmd.SilenceUsage = true

		var applicationName string
		if len(args) > 0 {
			applicationName = args[0]
		}

		rt := vars.RuntimeFactory.GetRuntimeType()
		opts := appTypes.ListOptions{
			ApplicationName: applicationName,
			OutputWide:      isOutputWide(),
		}

		// When experimentalTemplates is true and runtime is podman, use experimental catalog ps api
		// For openshift runtime, always use the older/stable code path regardless of experimental flag
		if experimentalPs && rt == types.RuntimeTypePodman {
			return renderApplicationPS(opts)
		}

		// Create application instance using factory
		factory := application.NewFactory(rt)
		app, err := factory.Create(applicationName)
		if err != nil {
			return fmt.Errorf("failed to create application instance: %w", err)
		}

		_, err = app.List(opts)
		if err != nil {
			return fmt.Errorf("failed to fetch application: %w", err)
		}

		return nil
	},
}

func init() {
	initPsCommonFlags()
}

func initPsCommonFlags() {
	psCmd.Flags().BoolVar(
		&experimentalPs,
		"experimental",
		false,
		"Include experimental application templates",
	)

	psCmd.Flags().StringVarP(
		&output,
		appFlags.Ps.Output,
		"o",
		"",
		"Output format (e.g., wide)",
	)
}

// buildPsFlagValidator creates and configures the flag validator for the ps command.
func buildPsFlagValidator() *flagvalidator.FlagValidator {
	runtimeType := vars.RuntimeFactory.GetRuntimeType()

	builder := flagvalidator.NewFlagValidatorBuilder(runtimeType)

	// Register common flags
	builder.
		AddCommonFlag(appFlags.Ps.Output, nil)

	return builder.Build()
}

// renderApplicationPS retrieves and processes the PS information for multiple application IDs.
// It fetches the process status for each application using the catalog API and prints the results in tabular format.
func renderApplicationPS(opts appTypes.ListOptions) error {
	appClient, err := catalogClient.NewApplicationClient()
	if err != nil {
		return fmt.Errorf("failed to create application client: %w", err)
	}

	applicationList, err := cliUtils.FetchApplications(appClient, opts.ApplicationName)
	if err != nil {
		return err
	}

	if len(applicationList) == 0 {
		logger.Warningln("No Application found")

		return nil
	}

	// Create table writer
	printer := utils.NewTableWriter()
	defer printer.CloseTableWriter()

	// Set table headers based on output format
	setApplicationPSTableHeaders(printer, opts.OutputWide)

	// Process each application ID
	for _, app := range applicationList {
		// Get PS information for the application
		psResp, err := appClient.GetApplicationPS(app.ID)
		if err != nil {
			return fmt.Errorf("failed to fetch application: %w", err)
		}

		// Process services pods
		for _, pod := range psResp.Services {
			rows := cliUtils.BuildPodRowFromAPI(psResp.Name, pod, opts.OutputWide)
			printer.AppendRow(rows...)
		}

		// Process components pods
		for _, pod := range psResp.Components {
			rows := cliUtils.BuildPodRowFromAPI(psResp.Name, pod, opts.OutputWide)
			printer.AppendRow(rows...)
		}
	}

	return nil
}

// setApplicationPSTableHeaders sets the table headers based on output format.
func setApplicationPSTableHeaders(printer *utils.Printer, outputWide bool) {
	if outputWide {
		printer.SetHeaders("APPLICATION NAME", "POD ID", "POD NAME", "STATUS", "CREATED", "CONTAINERS")
	} else {
		printer.SetHeaders("APPLICATION NAME", "POD NAME", "STATUS")
	}
}
