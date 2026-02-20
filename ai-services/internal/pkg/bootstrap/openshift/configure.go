package openshift

import (
	"context"
	"fmt"

	"github.com/project-ai-services/ai-services/assets"
	"github.com/project-ai-services/ai-services/internal/pkg/cli/templates"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/openshift"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
	"github.com/project-ai-services/ai-services/internal/pkg/spinner"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func (o *OpenshiftBootstrap) Configure() error {
	client, err := openshift.NewOpenshiftClient()
	if err != nil {
		return fmt.Errorf("failed to configure openshift cluster")
	}

	// iterate through the directory and apply the YAMLs
	if err := applyYamls(client.Ctx, client.Client); err != nil {
		return fmt.Errorf("error occurred while applying yaml: %w", err)
	}

	return nil
}

func applyYamls(ctx context.Context, c client.Client) error {
	tp := templates.NewEmbedTemplateProvider(templates.EmbedOptions{
		FS:      &assets.BootstrapFS,
		Root:    "bootstrap",
		Runtime: types.RuntimeTypeOpenShift,
	})

	yamls, err := tp.LoadYamls()
	if err != nil {
		return fmt.Errorf("error loading yamls: %w", err)
	}

	s := spinner.New("Applying YAMLs")
	s.Start(ctx)

	for _, yaml := range yamls {
		if err := utils.ApplyYaml(ctx, yaml, c); err != nil {
			s.Fail("failed to apply YAML")

			return fmt.Errorf("failed to apply YAML %s: %w", string(yaml), err)
		}
	}
	s.Stop("YAMLs Applied")

	return nil
}
