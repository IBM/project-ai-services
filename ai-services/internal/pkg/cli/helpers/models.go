package helpers

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/project-ai-services/ai-services/internal/pkg/cli/templates"
	"github.com/project-ai-services/ai-services/internal/pkg/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/models"
	"github.com/project-ai-services/ai-services/internal/pkg/vars"
)

func ListModels(template, appName string) ([]string, error) {
	tp := templates.NewEmbedTemplateProvider(templates.EmbedOptions{})
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
	command := "podman"
	// All arguments must be passed as a slice of strings
	args := []string{
		"run",
		"-ti",
		"-v",
		fmt.Sprintf("%s:/models:Z", targetDir),
		vars.ToolImage,
		"hf",
		"download",
		model,
		"--local-dir",
		fmt.Sprintf("/models/%s", model),
	}
	cmd := exec.Command(command, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	err := cmd.Run()
	if err != nil {
		return fmt.Errorf("failed to execute command: %w", err)
	}
	logger.Infoln("Model downloaded successfully")

	return nil
}
