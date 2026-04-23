package helpers

import (
	"fmt"
	"os"
	"strings"

	"github.com/containers/podman/v5/pkg/specgen"
	spec "github.com/opencontainers/runtime-spec/specs-go"
	"github.com/project-ai-services/ai-services/assets"
	"github.com/project-ai-services/ai-services/internal/pkg/cli/templates"
	"github.com/project-ai-services/ai-services/internal/pkg/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/models"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/podman"
	"github.com/project-ai-services/ai-services/internal/pkg/vars"
)

func ListModels(template, appName string) ([]string, error) {
	tp := templates.NewEmbedTemplateProvider(&assets.ApplicationFS)
	tmpls, err := tp.LoadAllTemplates(template)
	if err != nil {
		return nil, fmt.Errorf("error loading templates for %s: %w", template, err)
	}

	models := func(podSpec models.PodSpec) []string {
		modelAnnotations := []string{}
		for key, value := range podSpec.Annotations {
			if strings.HasPrefix(key, constants.ModelAnnotationKey) {
				modelAnnotations = append(modelAnnotations, value)
			}
		}

		return modelAnnotations
	}

	modelList := []string{}
	for _, tmpl := range tmpls {
		ps, err := tp.LoadPodTemplateWithValues(template, tmpl.Name(), appName, nil, nil)
		if err != nil {
			return nil, fmt.Errorf("error loading pod template: %w", err)
		}
		modelList = append(modelList, models(*ps)...)
	}

	return modelList, nil
}

func DownloadModel(model, targetDir string) error {
	// check for target model directory, if not present create it
	if _, err := os.Stat(targetDir); os.IsNotExist(err) {
		err := os.MkdirAll(targetDir, os.ModePerm)
		if err != nil {
			return fmt.Errorf("failed to create target model directory: %w", err)
		}
	}
	logger.Infof("Downloading model %s to %s\n", model, targetDir)

	// Get Podman client
	runtimeClient, err := podman.NewPodmanClient()
	if err != nil {
		return fmt.Errorf("failed to create podman client: %w", err)
	}

	// Create container spec
	s := specgen.NewSpecGenerator(vars.ToolImage, false)
	terminal := true
	stdin := true
	s.Terminal = &terminal
	s.Stdin = &stdin
	s.Command = []string{
		"hf",
		"download",
		model,
		"--local-dir",
		fmt.Sprintf("/models/%s", model),
	}
	rm := true
	s.Remove = &rm

	// Convert mounts
	s.Mounts = []spec.Mount{
		{
			Type:        "bind",
			Source:      targetDir,
			Destination: "/models",
			Options:     []string{"Z"},
		},
	}

	// Run container with spec
	exitCode, err := runtimeClient.RunContainerWithSpec(s)
	if err != nil {
		return fmt.Errorf("failed to run container: %w", err)
	}

	if exitCode != 0 {
		return fmt.Errorf("model download failed with exit code %d", exitCode)
	}

	logger.Infoln("Model downloaded successfully")

	return nil
}
