package openshift

import (
	"fmt"
	"maps"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/project-ai-services/ai-services/internal/pkg/logger"
)

// GetTemplate retrieves an OpenShift Template from the cluster.
func GetTemplate(name, namespace string) (*unstructured.Unstructured, error) {
	ocClient, err := NewOpenshiftClient()
	if err != nil {
		return nil, fmt.Errorf("failed to create OpenShift client: %w", err)
	}

	return getTemplate(ocClient, name, namespace)
}

// ProcessTemplate processes an OpenShift Template.
func ProcessTemplate(template *unstructured.Unstructured) ([]unstructured.Unstructured, error) {
	return processTemplate(template)
}

// ApplyObjects applies the processed template objects to the cluster
// This is an idempotent operation - it will create objects if they don't exist,
// or update them if they do exist.
func ApplyObjects(objects []unstructured.Unstructured, namespace string) error {
	return ApplyObjectsWithLabels(objects, namespace, nil)
}

// ApplyObjectsWithLabels applies the processed template objects to the cluster with additional labels
// This is an idempotent operation - it will create objects if they don't exist,
// or update them if they do exist. Additional labels are merged with existing labels.
func ApplyObjectsWithLabels(objects []unstructured.Unstructured, namespace string, labels map[string]string) error {
	ocClient, err := NewOpenshiftClient()
	if err != nil {
		return fmt.Errorf("failed to create OpenShift client: %w", err)
	}

	return applyProcessedObjectsWithLabels(ocClient, objects, namespace, labels)
}

// getTemplate retrieves an OpenShift Template from the cluster.
func getTemplate(ocClient *OpenshiftClient, name, namespace string) (*unstructured.Unstructured, error) {
	template := &unstructured.Unstructured{}
	template.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "template.openshift.io",
		Version: "v1",
		Kind:    "Template",
	})

	err := ocClient.Client.Get(ocClient.Ctx, client.ObjectKey{
		Name:      name,
		Namespace: namespace,
	}, template)

	if err != nil {
		return nil, fmt.Errorf("failed to get template: %w", err)
	}

	return template, nil
}

// processTemplate processes an OpenShift Template.
func processTemplate(template *unstructured.Unstructured) ([]unstructured.Unstructured, error) {
	// Get the objects from the template
	objects, found, err := unstructured.NestedSlice(template.Object, "objects")
	if err != nil {
		return nil, fmt.Errorf("failed to get template objects: %w", err)
	}
	if !found {
		return nil, fmt.Errorf("template has no objects")
	}

	// Process each object
	processedObjects := make([]unstructured.Unstructured, 0, len(objects))
	for _, obj := range objects {
		objMap, ok := obj.(map[string]any)
		if !ok {
			continue
		}

		u := unstructured.Unstructured{Object: objMap}
		processedObjects = append(processedObjects, u)
	}

	return processedObjects, nil
}

// applyProcessedObjectsWithLabels applies the processed template objects to the cluster with additional labels
// This is an idempotent operation - it will create objects if they don't exist,
// or update them if they do exist. Additional labels are merged with existing labels.
func applyProcessedObjectsWithLabels(ocClient *OpenshiftClient, objects []unstructured.Unstructured, namespace string, labels map[string]string) error {
	for _, obj := range objects {
		// Set the namespace if not already set
		if obj.GetNamespace() == "" {
			obj.SetNamespace(namespace)
		}

		// Add additional labels if provided
		if len(labels) > 0 {
			existingLabels := obj.GetLabels()
			if existingLabels == nil {
				existingLabels = make(map[string]string)
			}
			// Merge labels (additional labels take precedence)
			maps.Copy(existingLabels, labels)
			obj.SetLabels(existingLabels)
		}

		// Check if the object already exists
		existing := &unstructured.Unstructured{}
		existing.SetGroupVersionKind(obj.GroupVersionKind())

		err := ocClient.Client.Get(ocClient.Ctx, client.ObjectKey{
			Name:      obj.GetName(),
			Namespace: obj.GetNamespace(),
		}, existing)
		if err != nil && client.IgnoreNotFound(err) != nil {
			return fmt.Errorf("failed to check if object exists %s/%s: %w", obj.GetKind(), obj.GetName(), err)
		}

		// Handle object not found - create it
		if client.IgnoreNotFound(err) == nil {
			if createErr := ocClient.Client.Create(ocClient.Ctx, &obj); createErr != nil {
				return fmt.Errorf("failed to create object %s/%s: %w", obj.GetKind(), obj.GetName(), createErr)
			}
			logger.Infof("Created %s/%s\n", obj.GetKind(), obj.GetName())

			continue
		}

		// Object exists, update it using Update
		// We preserve the resource version from the existing object
		obj.SetResourceVersion(existing.GetResourceVersion())
		if updateErr := ocClient.Client.Update(ocClient.Ctx, &obj); updateErr != nil {
			return fmt.Errorf("failed to update object %s/%s: %w", obj.GetKind(), obj.GetName(), updateErr)
		}
		logger.Infof("Updated %s/%s\n", obj.GetKind(), obj.GetName())
	}

	return nil
}

// Made with Bob
