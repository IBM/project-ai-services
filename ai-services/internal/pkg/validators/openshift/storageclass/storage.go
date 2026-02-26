package storageclass

import (
	"context"
	"fmt"

	"github.com/project-ai-services/ai-services/internal/pkg/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/openshift"
	storagev1 "k8s.io/api/storage/v1"
)

const (
	StorageClassDefaultAnnotation = "storageclass.kubernetes.io/is-default-class"
	StorageClassDefaultValue      = "true"
)

type StorageClassRule struct{}

func NewStorageClassRule() *StorageClassRule {
	return &StorageClassRule{}
}

func (r *StorageClassRule) Name() string {
	return "default-sc"
}

func (r *StorageClassRule) Description() string {
	return "Validates that a default StorageClass exists"
}

// Verify checks if a default StorageClass exists.
func (r *StorageClassRule) Verify() error {
	ctx := context.Background()

	client, err := openshift.NewOpenshiftClient()
	if err != nil {
		return fmt.Errorf("failed to create openshift client: %w", err)
	}

	scList := &storagev1.StorageClassList{}
	if err := client.Client.List(ctx, scList); err != nil {
		return fmt.Errorf("failed to list storage classes: %w", err)
	}

	if len(scList.Items) == 0 {
		return fmt.Errorf("no storage classes found in cluster")
	}

	for _, sc := range scList.Items {
		val, exists := sc.Annotations[StorageClassDefaultAnnotation]
		if exists && val == StorageClassDefaultValue {
			return nil
		}
	}

	return fmt.Errorf("no default StorageClass found")
}

func (r *StorageClassRule) Message() string {
	return "Default storage class validated"
}

func (r *StorageClassRule) Level() constants.ValidationLevel {
	return constants.ValidationLevelError
}

func (r *StorageClassRule) Hint() string {
	return "Ensure a StorageClass is marked as default using annotation: storageclass.kubernetes.io/is-default-class=true"
}
