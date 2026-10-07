package helpers

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/containers/podman/v5/libpod/define"
	"github.com/containers/podman/v5/pkg/bindings/containers"
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

// modelDownloadPollInterval is how often WaitForModelDownload re-checks container state.
const modelDownloadPollInterval = 10 * time.Second

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

func DownloadModel(ctx context.Context, model, targetDir string) error {
	// check for target model directory
	fileInfo, err := os.Stat(targetDir)
	if err != nil {
		return fmt.Errorf("cannot access directory: %s, err: %w", targetDir, err)
	}

	// verify it's a directory
	if !fileInfo.IsDir() {
		return fmt.Errorf("path is not a directory: %s", targetDir)
	}

	// check if user has write permissions to the directory
	// try to create a temporary file to verify write access
	testFile := targetDir + "/.write_test"
	f, err := os.Create(testFile)
	if err != nil {
		return fmt.Errorf("user does not have write permission to directory: %s, err: %w", targetDir, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("failed to close test file: %w", err)
	}
	if err := os.Remove(testFile); err != nil {
		return fmt.Errorf("failed to remove test file: %w", err)
	}

	return DownloadModelContainer(ctx, model, targetDir)
}

// StartModelDownloadContainer launches the tools container that downloads model
// and returns the container ID immediately without waiting for the download to
// finish. The caller is responsible for polling completion via WaitForModelDownload.
func StartModelDownloadContainer(ctx context.Context, model, targetDir string) (string, error) {
	absTargetDir, err := filepath.Abs(targetDir)
	if err != nil {
		return "", fmt.Errorf("failed to resolve absolute path for %s: %w", targetDir, err)
	}

	logger.InfofCtx(ctx, "Downloading model %s to %s\n", model, targetDir)

	// Get Podman client
	runtimeClient, err := podman.NewPodmanClient()
	if err != nil {
		return "", fmt.Errorf("failed to create podman client: %w", err)
	}

	// Create container spec
	s := specgen.NewSpecGenerator(vars.ToolImage, false)
	terminal := true
	stdin := true
	s.Terminal = &terminal
	s.Stdin = &stdin
	s.Command = []string{"hf", "download", model, "--local-dir", fmt.Sprintf("/models/%s", model)}
	// Do not auto-remove: the container must remain inspectable until the caller
	// has polled its exit code via WaitForModelDownload.
	s.Mounts = []spec.Mount{
		{
			Type:        "bind",
			Source:      absTargetDir,
			Destination: "/models",
			Options:     []string{"Z"},
		},
	}

	containerID, err := runtimeClient.StartContainerWithSpec(ctx, s)
	if err != nil {
		return "", fmt.Errorf("failed to start model download container: %w", err)
	}

	return containerID, nil
}

// WaitForModelDownload polls the container identified by containerID until it
// exits, then returns an error if the exit code was non-zero.
// It respects ctx cancellation so it integrates cleanly with deployment timeouts.
func WaitForModelDownload(ctx context.Context, model, containerID string) error {
	runtimeClient, err := podman.NewPodmanClient()
	if err != nil {
		return fmt.Errorf("failed to create podman client: %w", err)
	}

	for {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("model download wait cancelled: %w", err)
		}

		data, err := containers.Inspect(runtimeClient.Context, containerID, nil)
		if err != nil {
			// Container may have been removed after exiting — treat as not found.
			return nil
		}

		if !data.State.Running {
			return checkDownloadExitCode(ctx, runtimeClient, model, containerID, data)
		}

		// Container is still running — wait before polling again.
		select {
		case <-ctx.Done():
			return fmt.Errorf("model download wait cancelled: %w", ctx.Err())
		case <-time.After(modelDownloadPollInterval):
		}
	}
}

// checkDownloadExitCode removes the stopped container and returns an error if
// the download failed.
func checkDownloadExitCode(ctx context.Context, runtimeClient *podman.PodmanClient, model, containerID string, data *define.InspectContainerData) error {
	exitCode := data.State.ExitCode
	containerErr := data.State.Error
	oomKilled := data.State.OOMKilled

	// Remove the container now that we've collected its state.
	_, _ = containers.Remove(runtimeClient.Context, containerID, nil)

	if exitCode != 0 {
		msg := fmt.Sprintf("model %s download failed with exit code %d", model, exitCode)
		if oomKilled {
			msg += " (OOM killed)"
		}
		if containerErr != "" {
			msg += ": " + containerErr
		}

		return fmt.Errorf("%s", msg)
	}

	logger.InfolnCtx(ctx, "Model downloaded successfully")

	return nil
}

// DownloadModelContainer is a convenience wrapper that starts the download
// container and immediately waits for it to finish. It is used by the local
// (non-worker) deployment path and the standalone `model download` CLI command.
func DownloadModelContainer(ctx context.Context, model, targetDir string) error {
	containerID, err := StartModelDownloadContainer(ctx, model, targetDir)
	if err != nil {
		return err
	}

	return WaitForModelDownload(ctx, model, containerID)
}
