package models

import v1 "github.com/containers/podman/v5/pkg/k8s.io/api/core/v1"

type PodSpec struct {
	v1.Pod
}
