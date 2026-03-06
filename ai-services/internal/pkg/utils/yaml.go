package utils

import (
	"bytes"
	"fmt"
	"io"

	"github.com/project-ai-services/ai-services/internal/pkg/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/openshift"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	apiyaml "k8s.io/apimachinery/pkg/util/yaml"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	yamlDecoderBufSz = 4096
)

func ApplyYaml(c *openshift.OpenshiftClient, yaml []byte) error {
	resourceList := []*unstructured.Unstructured{}

	decoder := apiyaml.NewYAMLOrJSONDecoder(bytes.NewReader([]byte(yaml)), yamlDecoderBufSz)
	for {
		resource := unstructured.Unstructured{}
		err := decoder.Decode(&resource)
		if err == nil {
			resourceList = append(resourceList, &resource)
		} else if err == io.EOF {
			break
		} else {
			return fmt.Errorf("error decoding to unstructured %v", err.Error())
		}
	}

	for _, object := range resourceList {
		if err := applyObject(c, object); err != nil {
			return fmt.Errorf("error applying object %v", err.Error())
		}
	}

	return nil
}

// applyObject applies the desired object against the apiserver.
func applyObject(c *openshift.OpenshiftClient, object *unstructured.Unstructured) error {
	// Retrieve name, namespace, groupVersionKind from given object.
	name := object.GetName()
	namespace := object.GetNamespace()
	if name == "" {
		return fmt.Errorf("object %s has no name", object.GroupVersionKind().String())
	}

	groupVersionKind := object.GroupVersionKind()

	objDesc := fmt.Sprintf("(%s) %s/%s", groupVersionKind.String(), namespace, name)

	// Apply the k8s object with provided version kind in given namespace.
	err := c.Client.Apply(c.Ctx, client.ApplyConfigurationFromUnstructured(object), &client.ApplyOptions{FieldManager: constants.AIServices})
	if err != nil {
		return fmt.Errorf("could not create %s. Error: %v", objDesc, err.Error())
	}

	return nil
}
