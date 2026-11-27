package model

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/models"
	catalogClient "github.com/project-ai-services/ai-services/internal/pkg/catalog/client"
	cliUtils "github.com/project-ai-services/ai-services/internal/pkg/cli/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
)

const emptyValue = "-"

func newInfoCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "info [name]",
		Short: "Display model details",
		Long: `Displays the details of a model, local or remote.

Arguments:
  [name] : Model name (required)`,
		Example: `  # Display model information
  ai-services model info granite-llm --runtime podman`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			// Once precheck passes, silence usage for any *later* internal errors.
			cmd.SilenceUsage = true

			return runInfo(cmd.Context(), args[0])
		},
	}
}

func runInfo(ctx context.Context, name string) error {
	modelClient, err := catalogClient.NewModelClient(ctx)
	if err != nil {
		return fmt.Errorf("failed to create model client: %w", err)
	}

	item, err := cliUtils.GetModelByName(ctx, modelClient, name)
	if err != nil {
		return err
	}

	detail, err := modelClient.GetModel(ctx, item.ID)
	if err != nil {
		return fmt.Errorf("failed to get model: %w", err)
	}

	logger.Infoln(renderModelInfo(detail))

	return nil
}

// renderModelInfo formats the model detail as aligned key/value lines.
func renderModelInfo(d *models.ModelDetail) string {
	var b strings.Builder

	field := func(key, value string) {
		fmt.Fprintf(&b, "%-20s%s\n", key+":", orDash(value))
	}

	field("Name", d.Name)
	field("ID", d.ID)
	field("Type", d.ComponentType.Name)
	field("Deployment Type", string(d.DeploymentType))
	field("Provider", d.Provider.Name)
	field("Model", d.Model)
	field("Worker", workerString(d))
	field("Status", d.Status)
	field("Message", d.Message)
	field("Connected Services", fmt.Sprintf("%d", d.ConnectedServices))
	field("Created At", formatTime(d.CreatedAt))
	field("Updated At", formatTime(d.UpdatedAt))

	if len(d.Params) > 0 {
		b.WriteString("Parameters:\n")
		for _, k := range slices.Sorted(maps.Keys(d.Params)) {
			fmt.Fprintf(&b, "  %s: %v\n", k, d.Params[k])
		}
	}

	if endpoints := nonEmptyEndpoints(d.Endpoints); len(endpoints) > 0 {
		b.WriteString("Endpoints:\n")
		for _, e := range endpoints {
			fmt.Fprintf(&b, "  - %s: %s\n", e.Type, e.URL)
		}
	}

	if len(d.Applications) > 0 {
		b.WriteString("Applications:\n")
		for _, a := range d.Applications {
			fmt.Fprintf(&b, "  - %s\n", a.Name)
		}
	}

	return strings.TrimRight(b.String(), "\n")
}

func workerString(d *models.ModelDetail) string {
	if d.Worker == nil {
		return ""
	}

	return fmt.Sprintf("%s (%s)", d.Worker.Name, d.Worker.RuntimeType)
}

func nonEmptyEndpoints(in []models.ModelEndpoint) []models.ModelEndpoint {
	out := make([]models.ModelEndpoint, 0, len(in))
	for _, e := range in {
		if e.URL != "" {
			out = append(out, e)
		}
	}

	return out
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}

	return t.Format(time.RFC3339)
}

func orDash(s string) string {
	if s == "" {
		return emptyValue
	}

	return s
}
