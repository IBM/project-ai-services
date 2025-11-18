package application

import (
	"fmt"
	"strings"

	"github.com/containers/podman/v5/pkg/domain/entities/types"
	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/jedib0t/go-pretty/v6/text"
	"github.com/project-ai-services/ai-services/internal/pkg/cli/helpers"
	"github.com/spf13/cobra"

	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/podman"
)

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

		p := helpers.NewTableWriter()
		defer p.CloseTableWriter()

		t := p.GetTableWriter()
		t.AppendHeader(table.Row{"Application Name", "Pod ID", "Pod Name", "Status", "Exposed"})
		t.SetColumnConfigs([]table.ColumnConfig{
			{Number: 4, Align: text.AlignCenter},
		})

		for _, pod := range pods {
			podPorts := []string{}
			pInfo, err := runtimeClient.InspectPod(pod.Id)
			if err != nil {
				continue
			}

			if pInfo.InfraConfig == nil || pInfo.InfraConfig.PortBindings == nil {
				continue
			}

			for _, ports := range pInfo.InfraConfig.PortBindings {
				for _, port := range ports {
					podPorts = append(podPorts, port.HostPort)
				}
			}
			if len(podPorts) == 0 {
				podPorts = append(podPorts, "none")
			}
			t.AppendRow(table.Row{fetchPodNameFromLabels(pod.Labels), pod.Id, pod.Name, pod.Status, strings.Join(podPorts, ", ")})
		}
		return nil
	},
}

func fetchPodNameFromLabels(labels map[string]string) string {
	return labels["ai-services.io/application"]
}
