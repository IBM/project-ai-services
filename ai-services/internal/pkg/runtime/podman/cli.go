package podman

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strings"

	"github.com/project-ai-services/ai-services/internal/pkg/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
)

const (
	startFlagTrue  = "--start=true"
	startFlagFalse = "--start=false"
)

var (
	publishFlag = "--publish=%s"
)

func RunPodmanKubePlay(body io.Reader, opts map[string]string) ([]types.Pod, error) {
	cmdName := "podman"

	cmd := exec.Command(cmdName, buildCmdArgs(opts)...)

	cmd.Stdin = body

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	// Run the command
	err := cmd.Run()
	if err != nil {
		return nil, fmt.Errorf("failed to execute podman kube play: %w. StdErr: %v", err, cmd.Stderr)
	}

	//  Extract ALL Pod IDs from the output
	podIDs := extractPodIDsFromOutput(stdout.String())

	result := make([]types.Pod, 0, len(podIDs))

	// Iterate over ALL extracted Pod IDs to get container information
	for _, podID := range podIDs {
		// Run podman ps, filtering by the specific pod ID
		cmdPs := exec.Command("podman", "ps", "-a", "--filter", fmt.Sprintf("pod=%s", podID), "--format", "json")
		outputPs, errPs := cmdPs.Output()
		if errPs != nil {
			return nil, fmt.Errorf("error executing podman ps for pod %s: %v", podID, errPs)
		}

		// Parse the JSON output
		var containers []types.Container
		if err := json.Unmarshal(outputPs, &containers); err != nil {
			return nil, fmt.Errorf("error executing podman ps for pod %s: %v", podID, err)
		}

		pod := types.Pod{ID: podID, Containers: containers}
		result = append(result, pod)
	}

	return result, nil
}

// Helper function to extract podIds from RunKubePlay stdout.
func extractPodIDsFromOutput(output string) []string {
	lines := strings.Split(output, "\n")
	ids := make([]string, 0, len(lines))
	for _, line := range lines {
		if strings.HasPrefix(line, "Pod") {
			// Skip line with Pod prefix
			continue
		}
		if strings.HasPrefix(line, "Container") {
			// Break if we encounter Container prefix as it means we have collected the podIDs
			break
		}
		// Read all the pod ids
		id := strings.TrimSpace(line)
		ids = append(ids, id)
	}

	return ids
}

func buildCmdArgs(opts map[string]string) []string {
	cmdArgs := []string{"kube", "play"}

	if v, ok := opts["start"]; ok {
		switch v {
		case constants.PodStartOff:
			cmdArgs = append(cmdArgs, startFlagFalse)
		case constants.PodStartOn:
			cmdArgs = append(cmdArgs, startFlagTrue)
		default:
			// by default go with start set to true
			cmdArgs = append(cmdArgs, startFlagTrue)
		}
	}

	if v, ok := opts["publish"]; ok {
		portMappings := strings.Split(v, ",")
		for _, portMapping := range portMappings {
			if portMapping != "" {
				cmdArgs = append(cmdArgs, fmt.Sprintf(publishFlag, portMapping))
			}
		}
	}

	return append(cmdArgs, "-")
}
