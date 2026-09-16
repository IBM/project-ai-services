package helm

import (
	"context"
	"fmt"

	"github.com/project-ai-services/ai-services/internal/pkg/cli/templates"
	"github.com/project-ai-services/ai-services/internal/pkg/spinner"
	"helm.sh/helm/v4/pkg/chart"
)

// LoadChart loads the named Helm chart from the embedded template provider.
// It shows a spinner during the operation and returns the parsed chart data
// or an error if the chart cannot be found or parsed.
func LoadChart(ctx context.Context, tp templates.Template, name string) (chart.Charter, error) {
	s := spinner.New(fmt.Sprintf("Loading the Helm chart for %s...", name))

	s.Start(ctx)
	chart, err := tp.LoadChart(name)
	if err != nil {
		s.Fail("failed to load the Helm chart")

		return nil, fmt.Errorf("failed to load the chart: %w", err)
	}
	s.Stop("Loaded the Helm chart successfully")

	return chart, nil
}
