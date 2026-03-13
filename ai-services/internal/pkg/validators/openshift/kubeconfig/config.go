package kubeconfig

import (
	"context"
	"fmt"

	"github.com/project-ai-services/ai-services/internal/pkg/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/openshift"
	authv1 "k8s.io/api/authorization/v1"
	corev1 "k8s.io/api/core/v1"
	k8sClient "sigs.k8s.io/controller-runtime/pkg/client"
)

type KubeconfigRule struct{}

func NewKubeconfigRule() *KubeconfigRule {
	return &KubeconfigRule{}
}

func (r *KubeconfigRule) Name() string {
	return "kubeconfig"
}

func (r *KubeconfigRule) Description() string {
	return "Validates cluster access"
}

// Verify checks if the kubeconfig can access the OpenShift cluster and has required permissions.
func (r *KubeconfigRule) Verify() error {
	ctx := context.Background()

	client, err := openshift.NewOpenshiftClient()
	if err != nil {
		return fmt.Errorf("failed to create openshift client: %w", err)
	}

	// Validate cluster access by listing namespaces
	ns := &corev1.Namespace{}
	key := k8sClient.ObjectKey{Name: "default"}
	if err := client.Client.Get(ctx, key, ns); err != nil {
		return fmt.Errorf("failed to get namespace %s: %w", key.Name, err)
	}

	// If wildcard checks failed, fall back to checking specific resources
	specificPermissions := getBootstrapSpecificPermissions()
	for _, perm := range specificPermissions {
		if err := r.checkPermission(ctx, client, perm); err != nil {
			return err
		}
	}

	return nil
}

// checkPermission validates a specific permission using SelfSubjectAccessReview.
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
	return "Cluster authentication validated successfully."
}

func (r *KubeconfigRule) Level() constants.ValidationLevel {
	return constants.ValidationLevelCritical
}

func (r *KubeconfigRule) Hint() string {
	return "Make sure your kubeconfig is correctly configured and that you have the necessary permissions to create namespaces, operator groups, and subscriptions in the required namespaces."
}
