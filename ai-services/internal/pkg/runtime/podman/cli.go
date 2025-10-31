package podman

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strings"
)

func RunPodmanKubePlay(body io.Reader) (*KubePlayOutput, error) {
	cmdName := "podman"
	cmdArgs := []string{"kube", "play", "-"}

	cmd := exec.Command(cmdName, cmdArgs...)

	cmd.Stdin = body

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	// Run the command
	err := cmd.Run()
	if err != nil {
		return nil, fmt.Errorf("failed to execute podman kube play: %w", err)
	}

	//  Extract ALL Pod IDs from the output
	podIDs := extractPodIDsFromOutput(stdout.String())

	var result KubePlayOutput

	// Iterate over ALL extracted Pod IDs to get container information
	for _, podID := range podIDs {
		// Run podman ps, filtering by the specific pod ID
		cmdPs := exec.Command("podman", "ps", "-a", "--filter", fmt.Sprintf("pod=%s", podID), "--format", "json")
		outputPs, errPs := cmdPs.Output()
		if errPs != nil {
			return nil, fmt.Errorf("error executing podman ps for pod %s: %v", podID, errPs)
		}

		// Parse the JSON output
		var containers []Container
		if err := json.Unmarshal(outputPs, &containers); err != nil {
			return nil, fmt.Errorf("error executing podman ps for pod %s: %v", podID, errPs)
		}

		pod := Pod{ID: podID, Containers: containers}
		result.Pods = append(result.Pods, pod)
	}

	return &result, nil
}

// Helper function to extract podIds from RunKubePlay stdout
func extractPodIDsFromOutput(output string) []string {
	var ids []string
	lines := strings.SplitSeq(output, "\n")
	for line := range lines {
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
