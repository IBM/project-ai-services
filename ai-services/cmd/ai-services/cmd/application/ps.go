package application

import (
	"fmt"
	"strings"

	"github.com/containers/podman/v5/pkg/domain/entities/types"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
	"github.com/spf13/cobra"

	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/podman"
)

var output string

func init() {
	psCmd.Flags().StringVarP(
		&output,
		"output",
		"o",
		"",
		"Output format (e.g., wide)",
	)
}

func isOutputWide() bool {
	return strings.ToLower(output) == "wide"
}

var psCmd = &cobra.Command{
	Use:   "ps [name]",
	Short: "Lists all the running applications",
	Long:  `Retrieves information about all the running applications if no name is provided`,
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		var applicationName string
		if len(args) > 0 {
			applicationName = args[0]
		}

		// podman connectivity
		runtimeClient, err := podman.NewPodmanClient()
		if err != nil {
			return fmt.Errorf("failed to connect to podman: %w", err)
		}

		listFilters := map[string][]string{}
		if applicationName != "" {
			listFilters["label"] = []string{fmt.Sprintf("ai-services.io/application=%s", applicationName)}
		}

		resp, err := runtimeClient.ListPods(listFilters)
		if err != nil {
			return fmt.Errorf("failed to list pods: %w", err)
		}

		// TODO: Avoid doing the type assertion and importing types package from podman

		var pods []*types.ListPodsReport
		if val, ok := resp.([]*types.ListPodsReport); ok {
			pods = val
		}

		if len(pods) == 0 && applicationName != "" {
			logger.Infof("No Pods found for the given application name: %s", applicationName)
			return nil
		}

		p := utils.NewTableWriter()
		defer p.CloseTableWriter()

		if isOutputWide() {
			p.SetHeaders("APPLICATION NAME", "POD ID", "POD NAME", "STATUS", "EXPOSED")
		} else {
			p.SetHeaders("APPLICATION NAME", "POD NAME", "STATUS")
		}

		for _, pod := range pods {
			if fetchPodNameFromLabels(pod.Labels) == "" {
				//Skip pods which are not linked to ai-services
				continue
			}
			podPorts := []string{}
			pInfo, err := runtimeClient.InspectPod(pod.Id)
			if err != nil {
				continue
			}

			if pInfo.InfraConfig != nil && pInfo.InfraConfig.PortBindings != nil {
				for _, ports := range pInfo.InfraConfig.PortBindings {
					for _, port := range ports {
						podPorts = append(podPorts, port.HostPort)
					}
				}
			}

			if len(podPorts) == 0 {
				podPorts = []string{"none"}
			}

			if isOutputWide() {
				p.AppendRow(
					fetchPodNameFromLabels(pod.Labels),
					pod.Id[:12],
					pod.Name,
					pod.Status,
					strings.Join(podPorts, ", "),
				)
			} else {
				p.AppendRow(
					fetchPodNameFromLabels(pod.Labels),
					pod.Name,
					pod.Status,
				)
			}
		}
		return nil
	},
}

func fetchPodNameFromLabels(labels map[string]string) string {
	return labels["ai-services.io/application"]
}
