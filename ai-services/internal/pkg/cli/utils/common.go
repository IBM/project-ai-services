package utils

import (
	"context"
	"fmt"
	"strings"
	"time"

	catalogClient "github.com/project-ai-services/ai-services/internal/pkg/catalog/client"
	catalogConstants "github.com/project-ai-services/ai-services/internal/pkg/catalog/constants"
	catalogTypes "github.com/project-ai-services/ai-services/internal/pkg/catalog/types"
	"github.com/project-ai-services/ai-services/internal/pkg/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
)

// FetchApplications retrieves either all applications or a specific application by name.
// If appName is empty, it fetches all applications. Otherwise, it fetches the specified application.
func FetchApplications(ctx context.Context, appClient *catalogClient.ApplicationClient, appName string) ([]catalogTypes.Application, error) {
	if appName == "" {
		// Fetch all applications when no specific name is provided
		applicationList, err := GetAllApps(ctx, appClient)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch all applications: %w", err)
		}

		return applicationList, nil
	}

	// Fetch specific application by name
	application, err := GetAppByName(ctx, appClient, appName)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch application '%s': %w", appName, err)
	}

	return []catalogTypes.Application{*application}, nil
}

// BuildPodRowFromAPI builds a table row from API response data.
func BuildPodRowFromAPI(appName string, pod catalogTypes.Pod, wideOutput bool) []string {
	status := getPodStatusFromAPI(pod)

	// If wide option flag is not set, return appName, podName and status only
	if !wideOutput {
		return []string{appName, pod.PodName, status}
	}

	containerNames := getContainerNamesFromAPI(pod)

	// Parse the Created string and convert to TimeAgo format
	created := "N/A"
	if pod.Created != "" {
		// Try to parse the Created timestamp
		parsedTime, err := time.Parse(catalogConstants.RFC3339WithTimezone, pod.Created)
		if err == nil {
			created = utils.TimeAgo(parsedTime)
		} else {
			// If parsing fails, use the original string
			created = pod.Created
		}
	}

	return []string{
		appName,
		pod.PodID[:12],
		pod.PodName,
		status,
		created,
		strings.Join(containerNames, ", "),
	}
}

// getPodStatusFromAPI determines the pod status from API response.
func getPodStatusFromAPI(pod catalogTypes.Pod) string {
	status := string(pod.Status)

	// If the pod is running, check if it's healthy
	if strings.ToLower(status) == "running" {
		if pod.Healthy {
			status += fmt.Sprintf(" (%s)", constants.Ready)
		} else {
			status += fmt.Sprintf(" (%s)", constants.NotReady)
		}
	}

	return status
}

// getContainerNamesFromAPI extracts container names with their status from API response.
func getContainerNamesFromAPI(pod catalogTypes.Pod) []string {
	containerNames := make([]string, 0, len(pod.Containers))
	for _, container := range pod.Containers {
		health := constants.NotReady
		if container.Healthy {
			health = constants.Ready
		}
		containerNames = append(containerNames, fmt.Sprintf("%s (%s)", container.Name, health))
	}

	return containerNames
}

func GetPodsFromApplicationsPS(ctx context.Context, appName string) ([]types.Pod, error) {
	var pods []types.Pod //nolint: prealloc
	appClient, err := catalogClient.NewApplicationClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create application client: %w", err)
	}

	app, err := GetAppByName(ctx, appClient, appName)
	if err != nil {
		return nil, err
	}

	psResp, err := appClient.GetApplicationPS(ctx, app.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch application: %w", err)
	}

	// add components to the list of pods
	for _, resp := range psResp.Components {
		pod := types.Pod{
			ID:   resp.PodName,
			Name: resp.PodName,
		}
		pods = append(pods, pod)
	}

	// add services to the list of pods
	for _, resp := range psResp.Services {
		pod := types.Pod{
			ID:   resp.PodName,
			Name: resp.PodName,
		}
		pods = append(pods, pod)
	}

	return pods, nil
}

// ConfirmUninstall prompts the user to confirm uninstall and logs pods to be deleted.Add a comment on  lines R21 to R41Add diff commentMarkdown input:  edit mode selected.WritePreviewAdd a suggestionHeadingBold(command b) command⌘ bBItalic(command i) command⌘ iIQuote(command shift right angle bracket) command⌘ shift⇧ right angle bracket>Code(command e) command⌘ eELink(command k) command⌘ kKUnordered list(command 8) command⌘ 88Numbered list(command shift ampersand) command⌘ shift⇧ ampersand&Task list(command shift l) command⌘ shift⇧ lLMentionReferenceMore itemsSaved repliesAdd FilesPaste, drop, or click to add filesCancelCommentStart a review
func ConfirmUninstall(ctx context.Context, pods []types.Pod, autoYes bool) (bool, error) {
	if autoYes {
		return true, nil
	}

	// Print pods to be deleted
	logger.InfofCtx(ctx, "Below are the list of pods to be deleted")
	for _, pod := range pods {
		logger.InfofCtx(ctx, "\t-> %s\n", pod.Name)
	}

	// Confirm Uninstall
	confirmed, err := utils.ConfirmAction("\nDo you want to continue?")
	if err != nil {
		return false, fmt.Errorf("failed to get confirmation: %w", err)
	}

	if !confirmed {
		logger.InfolnCtx(ctx, "Uninstall cancelled")

		return false, nil
	}

	return true, nil
}
