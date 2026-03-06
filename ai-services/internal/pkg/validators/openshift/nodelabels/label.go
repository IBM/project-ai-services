package nodelabels

import (
	"fmt"
	"strings"

	"github.com/project-ai-services/ai-services/internal/pkg/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/openshift"
	corev1 "k8s.io/api/core/v1"
)

const (
	NodeRoleSpyre  = "node-role.kubernetes.io/spyre"
	NodeRoleWorker = "node-role.kubernetes.io/worker"
)

type NodeLabelsRule struct {
	client *openshift.OpenshiftClient
}

func NewNodeLabelsRule(client *openshift.OpenshiftClient) *NodeLabelsRule {
	return &NodeLabelsRule{
		client: client,
	}
}

func (r *NodeLabelsRule) Name() string {
	return "node-labels"
}

func (r *NodeLabelsRule) Description() string {
	return "Validates that cluster nodes have correct labels"
}

// Verify checks node labels in the cluster.
func (r *NodeLabelsRule) Verify() error {
	ctx := r.client.Ctx

	if r.client == nil {
		return fmt.Errorf("openshift client is not initialized")
	}

	nodeList := &corev1.NodeList{}
	if err := r.client.Client.List(ctx, nodeList); err != nil {
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
		_, hasSpyre := labels[NodeRoleSpyre]
		_, hasWorker := labels[NodeRoleWorker]

		if hasSpyre && hasWorker {
			return failed
		}
	}

	// No node had both labels
	return append(failed, "no nodes with spyre and worker labels found")
}

func (r *NodeLabelsRule) Message() string {
	return "Node labels validated"
}

func (r *NodeLabelsRule) Level() constants.ValidationLevel {
	return constants.ValidationLevelError
}

func (r *NodeLabelsRule) Hint() string {
	return "Ensure at least one worker node has the spyre role"
}
