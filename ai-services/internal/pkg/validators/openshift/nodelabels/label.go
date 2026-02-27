package nodelabels

import (
	"context"
	"fmt"
	"strings"

	"github.com/project-ai-services/ai-services/internal/pkg/constants"
	openshiftconst "github.com/project-ai-services/ai-services/internal/pkg/constants/openshift"
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
	return "Validates node architecture, OS, and at least one node with Spyre."
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
	hasSpyreNode := false

	for _, node := range nodes {
		labels := node.Labels
		if err := r.checkArch(node.Name, labels); err != nil {
			failed = append(failed, err.Error())
		}
		if err := r.checkOS(node.Name, labels); err != nil {
			failed = append(failed, err.Error())
		}
		if r.checkSpyre(labels) {
			hasSpyreNode = true
		}
	}

	if !hasSpyreNode {
		failed = append(failed, " - no node with ibm.com/spyre.present=true found")
	}

	return failed
}

func (r *NodeLabelsRule) checkArch(nodeName string, labels map[string]string) error {
	if labels[openshiftconst.NodeArchLabel] != openshiftconst.NodeArch {
		return fmt.Errorf("  - %s must have %s=%s", nodeName, openshiftconst.NodeArchLabel, openshiftconst.NodeArch)
	}

	return nil
}

func (r *NodeLabelsRule) checkOS(nodeName string, labels map[string]string) error {
	if labels[openshiftconst.NodeOSLabel] != openshiftconst.NodeOSRHEL {
		return fmt.Errorf("  - %s must have %s=%s", nodeName, openshiftconst.NodeOSLabel, openshiftconst.NodeOSRHEL)
	}

	return nil
}

func (r *NodeLabelsRule) checkSpyre(labels map[string]string) bool {
	if val, ok := labels[openshiftconst.SpyreLabel]; ok && val == openshiftconst.SpyreLabelTrue {
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
	return "Ensure nodes have correct arch, OS, and at least one Spyre node"
}
