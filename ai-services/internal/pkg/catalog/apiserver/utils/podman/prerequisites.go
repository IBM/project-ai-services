package podman

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/project-ai-services/ai-services/internal/pkg/cli/helpers"
	"github.com/project-ai-services/ai-services/internal/pkg/image"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime"
	remoteruntime "github.com/project-ai-services/ai-services/internal/pkg/runtime/remote"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/worker/payload"
	workerpb "github.com/project-ai-services/ai-services/internal/pkg/worker/proto"
)

// PullImages pulls only the missing images from the provided list using the given runtime.
// Images that are already present are skipped. It is shared by the application and model
// deployment paths.
func PullImages(ctx context.Context, rt runtime.Runtime, images []string) error {
	if len(images) == 0 {
		return nil
	}

	imgHelper := &image.Images{Runtime: rt}
	if err := imgHelper.IfNotPresent(ctx, images); err != nil {
		return fmt.Errorf("failed to pull images: %w", err)
	}

	return nil
}

// DownloadModels downloads the given models to the models directory of the target host.
//
// For remote workers the download is split into two gRPC commands to avoid
// timing out the stream on large models:
//  1. COMMAND_TYPE_DOWNLOAD_MODEL — launches the tools container and returns
//     the container ID immediately.
//  2. COMMAND_TYPE_CHECK_MODEL_DOWNLOAD — waits on the worker until the container
//     exits (respects ctx for cancellation).
//
// For local deployments helpers.DownloadModelContainer is called directly with the
// local models path. It is shared by the application and model deployment paths.
func DownloadModels(ctx context.Context, rt runtime.Runtime, models []string) error {
	if remoteRT, ok := rt.(*remoteruntime.RemoteRuntime); ok {
		return downloadModelsRemote(ctx, remoteRT, models)
	}

	modelsPath := utils.GetModelsPath()

	for _, modelName := range models {
		logger.InfofCtx(ctx, "Downloading model: %s\n", modelName)

		if err := helpers.DownloadModelContainer(ctx, modelName, modelsPath); err != nil {
			return fmt.Errorf("failed to download model %s: %w", modelName, err)
		}
	}

	return nil
}

// downloadModelsRemote starts each model download container on the remote worker
// (step 1) and begins waiting for it to complete (step 2) as soon as it has started.
//
// Starts and waits share one cancellable context: if the caller cancels (e.g. the
// deployment is deleted), a start fails, or any download fails, every in-flight wait
// is cancelled so the worker stops the remaining download containers.
func downloadModelsRemote(ctx context.Context, rt *remoteruntime.RemoteRuntime, models []string) error {
	waitCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	var (
		wg       sync.WaitGroup
		once     sync.Once
		firstErr error
	)

	fail := func(err error) {
		once.Do(func() {
			firstErr = err
			cancel()
		})
	}

	for _, modelName := range models {
		logger.InfofCtx(ctx, "Downloading model: %s\n", modelName)

		res, err := rt.Send(waitCtx, workerpb.CommandType_COMMAND_TYPE_DOWNLOAD_MODEL, payload.DownloadModel{
			Model: modelName,
		})
		if err != nil {
			fail(fmt.Errorf("failed to start model download %s: %w", modelName, err))

			break
		}

		var containerID string
		if err := json.Unmarshal(res.GetData(), &containerID); err != nil {
			fail(fmt.Errorf("failed to decode container ID for model %s: %w", modelName, err))

			break
		}

		wg.Add(1)

		go func() {
			defer wg.Done()

			if _, err := rt.Send(waitCtx, workerpb.CommandType_COMMAND_TYPE_CHECK_MODEL_DOWNLOAD, payload.CheckModelDownload{
				ContainerID: containerID,
			}); err != nil {
				fail(fmt.Errorf("model download failed for %s: %w", modelName, err))
			}
		}()
	}

	wg.Wait()

	return firstErr
}
