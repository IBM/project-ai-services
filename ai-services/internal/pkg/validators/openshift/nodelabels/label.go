package nodelabels

import (
	"context"
	"fmt"
	"strings"

	"github.com/project-ai-services/ai-services/internal/pkg/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/openshift"
	corev1 "k8s.io/api/core/v1"
)

type NodeLabelsRule struct{}

func NewNodeLabelsRule() *NodeLabelsRule {
	return &NodeLabelsRule{}
}

func (r *NodeLabelsRule) Name() string {
	return "node-labels"
}

func (r *NodeLabelsRule) Description() string {
	return "Validates node architecture (ppc64le) and OS (rhel)"
}

// Verify checks if all nodes in the OpenShift cluster have the required labels.
func (r *NodeLabelsRule) Verify() error {
	ctx := context.Background()

	client, err := openshift.NewOpenshiftClient()
	if err != nil {
		return fmt.Errorf("failed to create openshift client: %w", err)
	}

	nodeList := &corev1.NodeList{}
	if err := client.Client.List(ctx, nodeList); err != nil {
		return fmt.Errorf("failed to list cluster nodes: %w", err)
	}

	if len(nodeList.Items) == 0 {
		return fmt.Errorf("no nodes found in cluster")
	}

	var failed []string
	hasWorkerNode := false

	// Validate each node's labels for architecture and OS, and check for worker nodes.
	for _, node := range nodeList.Items {
		labels := node.Labels

		if labels["kubernetes.io/arch"] != "ppc64le" {
			failed = append(failed,
				fmt.Sprintf("  - %s must have kubernetes.io/arch=ppc64le", node.Name))
		}

		if labels["node.openshift.io/os_id"] != "rhel" {
			failed = append(failed,
				fmt.Sprintf("  - %s must have node.openshift.io/os_id=rhel", node.Name))
		}

		if _, isWorker := labels["node-role.kubernetes.io/worker"]; isWorker {
			hasWorkerNode = true
		}
	}

	if !hasWorkerNode {
		failed = append(failed, "  - no worker nodes found in cluster")
	}

	if len(failed) > 0 {
		return fmt.Errorf("node label validation failed:\n%s",
			strings.Join(failed, "\n"))
	}

	return nil
}

func (r *NodeLabelsRule) Message() string {
	return "Node labels validated for ppc arch and rhel os"
}

func (r *NodeLabelsRule) Level() constants.ValidationLevel {
	return constants.ValidationLevelError
}

func (r *NodeLabelsRule) Hint() string {
	return "Ensure all nodes have kubernetes.io/arch=ppc64le and node.openshift.io/os_id=rhel"
}
