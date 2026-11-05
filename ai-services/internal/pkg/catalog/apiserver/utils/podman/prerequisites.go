package podman

import (
	"context"
	"fmt"

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
// For remote workers the command is forwarded over the gRPC stream; the worker resolves
// AI_SERVICES_BASE_DIR from its own environment to find the models directory. For local
// deployments helpers.DownloadModelContainer is called directly with the local models path.
// It is shared by the application and model deployment paths.
func DownloadModels(ctx context.Context, rt runtime.Runtime, models []string) error {
	remoteRT, isRemote := rt.(*remoteruntime.RemoteRuntime)
	modelsPath := utils.GetModelsPath()

	for _, modelName := range models {
		logger.InfofCtx(ctx, "Downloading model: %s\n", modelName)

		if isRemote {
			if _, err := remoteRT.Send(ctx, workerpb.CommandType_COMMAND_TYPE_DOWNLOAD_MODEL, payload.DownloadModel{
				Model: modelName,
			}); err != nil {
				return fmt.Errorf("failed to download model %s: %w", modelName, err)
			}

			continue
		}

		if err := helpers.DownloadModelContainer(ctx, modelName, modelsPath); err != nil {
			return fmt.Errorf("failed to download model %s: %w", modelName, err)
		}
	}

	return nil
}
