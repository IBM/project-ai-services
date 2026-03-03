package nodelabels

import (
	"context"
	"fmt"
	"strings"

	"github.com/project-ai-services/ai-services/internal/pkg/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/openshift"
	corev1 "k8s.io/api/core/v1"
)

const (
	SpyreLabel     = "ibm.com/spyre.present"
	NodeRoleWorker = "node-role.kubernetes.io/worker"
	SpyreLabelTrue = "true"
)

type NodeLabelsRule struct{}

func NewNodeLabelsRule() *NodeLabelsRule {
	return &NodeLabelsRule{}
}

func (r *NodeLabelsRule) Name() string {
	return "node-labels"
}

func (r *NodeLabelsRule) Description() string {
	return "Validates that cluster nodes have correct labels"
}

// Verify checks node labels in the cluster.
func (r *NodeLabelsRule) Verify() error {
	ctx := context.Background()

	client, err := openshift.NewOpenshiftClient()
	if err != nil {
		return fmt.Errorf("failed to create OpenShift client: %w", err)
	}

	nodeList := &corev1.NodeList{}
	if err := client.Client.List(ctx, nodeList); err != nil {
		return fmt.Errorf("failed to list cluster nodes: %w", err)
	}

	if len(nodeList.Items) == 0 {
		return fmt.Errorf("no nodes found in cluster")
	}

	failed := r.validateNodes(nodeList.Items)
	if len(failed) > 0 {
		return fmt.Errorf("node label validation failed:\n%s", strings.Join(failed, "\n"))
	}

	return nil
}

// validateNodes performs the actual node checks.
func (r *NodeLabelsRule) validateNodes(nodes []corev1.Node) []string {
	var failed []string
	for _, node := range nodes {
		labels := node.Labels
		if r.checkSpyre(labels) || r.checkWorker(labels) {
			return failed
		}
	}

	return append(failed, "no nodes with spyre and worker labels found")
}

func (r *NodeLabelsRule) checkSpyre(labels map[string]string) bool {
	if val, ok := labels[SpyreLabel]; ok && val == SpyreLabelTrue {
		return true
	}

	return false
}

func (r *NodeLabelsRule) checkWorker(labels map[string]string) bool {
	if _, ok := labels[NodeRoleWorker]; ok {
		return true
	}

	return false
}

func (r *NodeLabelsRule) Message() string {
	return "Node labels validated"
}

func (r *NodeLabelsRule) Level() constants.ValidationLevel {
	return constants.ValidationLevelError
}

func (r *NodeLabelsRule) Hint() string {
	return "Ensure nodes have correct labels"
}
