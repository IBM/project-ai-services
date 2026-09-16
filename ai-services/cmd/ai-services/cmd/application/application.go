package application

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/project-ai-services/ai-services/cmd/ai-services/cmd/application/image"
	"github.com/project-ai-services/ai-services/cmd/ai-services/cmd/application/model"
	catalogClient "github.com/project-ai-services/ai-services/internal/pkg/catalog/client"
	cliUtils "github.com/project-ai-services/ai-services/internal/pkg/cli/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime"
	runtimeTypes "github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
	"github.com/project-ai-services/ai-services/internal/pkg/vars"
)

var (
	hiddenTemplates bool
	// Runtime type flag for application command. Optional — commands that operate
	// on an existing application (start, stop, logs, backup, restore) derive the
	// runtime from the application's Worker record when this is left empty.
	runtimeType string
)

// ApplicationCmd represents the application command.
var ApplicationCmd = &cobra.Command{
	Use:   "application",
	Short: "Deploy and monitor the applications",
	Long:  `The application command helps you deploy and monitor the applications`,
	// PersistentPreRunE initialises vars.RuntimeFactory only when --runtime is supplied.
	// Sub-commands that need the runtime (templates, create, ps, delete, info) enforce
	// the flag themselves. Sub-commands that can derive the runtime from the application's
	// Worker record (start, stop, logs, backup, restore) call resolveRuntimeForApp in RunE.
	// Platform support (linux/ppc64le) is checked only by sub-commands that actually
	// launch Podman work (create --legacy, bootstrap), not here.
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		cmd.SilenceUsage = true

		if runtimeType == "" {
			return nil
		}

		rt := runtimeTypes.RuntimeType(runtimeType)
		if !rt.Valid() {
			return fmt.Errorf("invalid runtime type: %s (must be 'podman' or 'openshift'). Please specify runtime using --runtime flag", runtimeType)
		}

		vars.RuntimeFactory = runtime.NewRuntimeFactory(rt)
		logger.Debugf("Using runtime: %s\n", rt)

		return nil
	},
}

func init() {
	ApplicationCmd.AddCommand(templatesCmd)
	ApplicationCmd.AddCommand(createCmd)
	ApplicationCmd.AddCommand(psCmd)
	ApplicationCmd.AddCommand(deleteCmd)
	ApplicationCmd.AddCommand(image.ImageCmd)
	ApplicationCmd.AddCommand(stopCmd)
	ApplicationCmd.AddCommand(startCmd)
	ApplicationCmd.AddCommand(infoCmd)
	ApplicationCmd.AddCommand(logsCmd)
	ApplicationCmd.AddCommand(model.ModelCmd)
	ApplicationCmd.AddCommand(restoreCmd)
	ApplicationCmd.AddCommand(backupCmd)

	// --runtime is optional: commands that operate on an existing application
	// (start, stop, logs, backup, restore) can resolve the runtime from the
	// application's Worker record. Commands that need a runtime up-front
	// (templates, create, ps, delete, info) enforce it themselves.
	ApplicationCmd.PersistentFlags().StringVarP(&runtimeType, "runtime", "r", "",
		fmt.Sprintf("runtime to use (options: %s, %s)", runtimeTypes.RuntimeTypePodman, runtimeTypes.RuntimeTypeOpenShift))

	ApplicationCmd.PersistentFlags().StringVar(&vars.ToolImage, "tool-image", vars.ToolImage, "Tool image to use for downloading the model(only for the development purpose)")
	ApplicationCmd.PersistentFlags().BoolVar(&hiddenTemplates, "hidden", false, "Show hidden templates")
	_ = ApplicationCmd.PersistentFlags().MarkHidden("tool-image")
	_ = ApplicationCmd.PersistentFlags().MarkHidden("hidden")
}

// resolveRuntimeForApp returns the RuntimeType to use for an operation on an
// existing application.
//
// When --runtime is explicitly provided (existingRuntime != ""), it uses that value
// (which has already been validated by InitAndValidateOptionalRuntimeFlag in the
// parent PersistentPreRunE). Otherwise it queries the catalog for the named
// application and reads the runtime type from its Worker record.
//
// If neither source yields a valid runtime type an error is returned that tells
// the user to supply --runtime explicitly.
func resolveRuntimeForApp(ctx context.Context, appName, existingRuntime string) (runtimeTypes.RuntimeType, error) {
	if existingRuntime != "" {
		// --runtime was given and already validated — vars.RuntimeFactory is set.
		return vars.RuntimeFactory.GetRuntimeType(), nil
	}

	// No --runtime flag: ask the catalog for the application's Worker.
	appClient, err := catalogClient.NewApplicationClient(ctx)
	if err != nil {
		return "", fmt.Errorf("catalog is unavailable (%w)", err)
	}

	app, err := cliUtils.GetAppByName(ctx, appClient, appName)
	if err != nil {
		return "", fmt.Errorf("application %q not found in catalog (%w)", appName, err)
	}

	fullApp, err := appClient.GetApplication(ctx, app.ID)
	if err != nil {
		return "", fmt.Errorf("could not fetch application details (%w)", err)
	}

	if fullApp.Worker != nil && fullApp.Worker.RuntimeType != "" {
		rt := runtimeTypes.RuntimeType(fullApp.Worker.RuntimeType)
		// Initialise vars.RuntimeFactory so any downstream code that reads it
		// (e.g. flagvalidator, legacy paths) sees a consistent value.
		vars.RuntimeFactory = runtime.NewRuntimeFactory(rt)

		return rt, nil
	}

	return "", fmt.Errorf("could not determine runtime for application %q: Worker record missing or has no runtime_type; please supply --runtime explicitly", appName)
}
