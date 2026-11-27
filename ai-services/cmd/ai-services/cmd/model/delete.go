package model

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/spf13/cobra"

	catalogClient "github.com/project-ai-services/ai-services/internal/pkg/catalog/client"
	cliUtils "github.com/project-ai-services/ai-services/internal/pkg/cli/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/spinner"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
)

func newDeleteCmd() *cobra.Command {
	var autoYes bool

	cmd := &cobra.Command{
		Use:   "delete [name]",
		Short: "Delete a model",
		Long: `Deletes a model, local or remote.

A local model is undeployed in the background. A remote model is removed immediately.
A model that is used by an application cannot be deleted.

Arguments:
  [name] : Model name (required)`,
		Example: `  # Delete a model
  ai-services model delete granite-llm --runtime podman

  # Delete a model without the confirmation prompt
  ai-services model delete granite-llm -y --runtime podman`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			// Once precheck passes, silence usage for any *later* internal errors.
			cmd.SilenceUsage = true

			return runDelete(cmd.Context(), args[0], autoYes)
		},
	}

	cmd.Flags().BoolVarP(&autoYes, "yes", "y", false, "Skip the confirmation prompt")

	return cmd
}

func runDelete(ctx context.Context, name string, autoYes bool) error {
	modelClient, err := catalogClient.NewModelClient(ctx)
	if err != nil {
		return fmt.Errorf("failed to create model client: %w", err)
	}

	item, err := cliUtils.GetModelByName(ctx, modelClient, name)
	if err != nil {
		return err
	}

	if !autoYes {
		confirmed, err := utils.ConfirmAction(fmt.Sprintf("Are you sure you want to delete the model '%s'?", item.Name))
		if err != nil {
			return err
		}

		if !confirmed {
			logger.Infoln("Aborted.")

			return nil
		}
	}

	logger.Infof("Deleting model %s...\n", item.Name)
	resp, err := modelClient.DeleteModel(ctx, item.ID)
	if err != nil {
		return deleteError(item.Name, err)
	}

	// A nil response means a remote model that was removed synchronously.
	if resp == nil {
		logger.Infof("Model %s deleted successfully.\n", item.Name)

		return nil
	}

	// A local model is undeployed in the background, so poll until it is gone.
	return waitForModelDeletion(ctx, modelClient, item.ID, item.Name)
}

// waitForModelDeletion shows a spinner, updated with the model status, until the model is gone.
func waitForModelDeletion(ctx context.Context, modelClient *catalogClient.ModelClient, id, name string) error {
	waitMsg := fmt.Sprintf("Waiting for model '%s' to be deleted...", name)
	s := spinner.New(waitMsg)
	s.Start(ctx)

	err := cliUtils.WaitForDeletion(ctx, "Model", func(ctx context.Context) error {
		m, err := modelClient.GetModel(ctx, id)
		if err != nil {
			return err
		}

		s.UpdateMessage(fmt.Sprintf("%s (status: %s)", waitMsg, m.Status))

		return nil
	})
	if err != nil {
		s.Fail("Failed to verify model deletion")

		return fmt.Errorf("failed to verify model deletion: %w", err)
	}

	s.Stop(fmt.Sprintf("Model '%s' deleted successfully", name))

	return nil
}

// deleteError turns a delete API failure into a message for the user.
func deleteError(name string, err error) error {
	var httpErr *catalogClient.HTTPError
	if !errors.As(err, &httpErr) {
		return fmt.Errorf("failed to delete model: %w", err)
	}

	switch httpErr.StatusCode {
	case http.StatusNotFound:
		return fmt.Errorf("model '%s' not found", name)
	case http.StatusForbidden:
		return fmt.Errorf("you are not allowed to delete model '%s': %s", name, httpErr.Message)
	case http.StatusConflict:
		return fmt.Errorf("model '%s' cannot be deleted: %s", name, httpErr.Message)
	default:
		return fmt.Errorf("failed to delete model '%s': %w", name, err)
	}
}
