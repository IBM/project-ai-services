package utils

import (
	"bytes"
	"context"
	"fmt"
	"io"

	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	apiyaml "k8s.io/apimachinery/pkg/util/yaml"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	yamlDecoderBufSz = 4096
)

func ApplyYaml(ctx context.Context, yaml []byte, c client.Client) error {
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
		if err := applyObject(ctx, object, c); err != nil {
			return fmt.Errorf("error applying object %v", err.Error())
		}
	}

	return nil
}

// applyObject applies the desired object against the apiserver.
func applyObject(ctx context.Context, object *unstructured.Unstructured, client client.Client) error {
	// Retrieve name, namespace, groupVersionKind from given object.
	name := object.GetName()
	namespace := object.GetNamespace()
	if name == "" {
		return fmt.Errorf("object %s has no name", object.GroupVersionKind().String())
	}

	groupVersionKind := object.GroupVersionKind()

	objDesc := fmt.Sprintf("(%s) %s/%s", groupVersionKind.String(), namespace, name)

	// Create the k8s object with provided version kind in given namespace.
	err := client.Create(ctx, object)
	if err != nil {
		if errors.IsAlreadyExists(err) {
			logger.Infof("%s already exists", objDesc, logger.VerbosityLevelDebug)

			return nil
		}

		return fmt.Errorf("could not create %s. Error: %v", objDesc, err.Error())
	}

	return nil
}
