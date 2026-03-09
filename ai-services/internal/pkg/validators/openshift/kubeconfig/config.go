package kubeconfig

import (
	"context"
	"fmt"

	"github.com/project-ai-services/ai-services/internal/pkg/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/openshift"
	authv1 "k8s.io/api/authorization/v1"
	corev1 "k8s.io/api/core/v1"
)

type KubeconfigRule struct{}

func NewKubeconfigRule() *KubeconfigRule {
	return &KubeconfigRule{}
}

func (r *KubeconfigRule) Name() string {
	return "kubeconfig"
}

func (r *KubeconfigRule) Description() string {
	return "Validates cluster access and operator installation permissions"
}

// Verify checks if the kubeconfig can access the OpenShift cluster and has required permissions.
func (r *KubeconfigRule) Verify() error {
	ctx := context.Background()

	client, err := openshift.NewOpenshiftClient()
	if err != nil {
		return fmt.Errorf("failed to create openshift client: %w", err)
	}

	// Validate cluster access by listing namespaces
	if err := client.Client.List(ctx, &corev1.NamespaceList{}); err != nil {
		return fmt.Errorf("failed to connect to cluster: %w", err)
	}

	// First, try checking with wildcard permissions (more efficient for cluster-admin users)
	wildcardPermissions := getWildcardPermissions()
	allWildcardsPassed := true
	for _, perm := range wildcardPermissions {
		if err := r.checkPermission(ctx, client, perm); err != nil {
			allWildcardsPassed = false
			break
		}
	}

	// If wildcard checks passed, user has sufficient permissions
	if allWildcardsPassed {
		return nil
	}

	// If wildcard checks failed, fall back to checking specific resources
	specificPermissions := getSpecificPermissions()
	for _, perm := range specificPermissions {
		if err := r.checkPermission(ctx, client, perm); err != nil {
			return err
		}
	}

	return nil
}

// checkPermission validates a specific permission using SelfSubjectAccessReview
func (r *KubeconfigRule) checkPermission(ctx context.Context, client *openshift.OpenshiftClient, perm permissionCheck) error {
	review := &authv1.SelfSubjectAccessReview{
		Spec: authv1.SelfSubjectAccessReviewSpec{
			ResourceAttributes: &authv1.ResourceAttributes{
				Group:     perm.group,
				Resource:  perm.resource,
				Verb:      perm.verb,
				Namespace: perm.namespace,
			},
		},
	}

	if err := client.Client.Create(ctx, review); err != nil {
		return fmt.Errorf("failed to validate permission for %s/%s in namespace %s: %w", perm.resource, perm.verb, perm.namespace, err)
	}

	if review.Status.Denied || !review.Status.Allowed {
		if perm.namespace != "" {
			return fmt.Errorf("user does not have permission to %s %s in namespace %s", perm.verb, perm.resource, perm.namespace)
		}
		return fmt.Errorf("user does not have permission to %s %s", perm.verb, perm.resource)
	}

	return nil
}

func (r *KubeconfigRule) Message() string {
	return "Cluster authentication and operator permissions validated"
}

func (r *KubeconfigRule) Level() constants.ValidationLevel {
	return constants.ValidationLevelError
}

func (r *KubeconfigRule) Hint() string {
	return "Make sure your kubeconfig is correctly configured and that you have the necessary permissions to create namespaces, operator groups, and subscriptions in the required namespaces."
}
