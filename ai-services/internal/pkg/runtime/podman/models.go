package podman

type Container struct {
	ID string `json:"ID"`
}

type Pod struct {
	ID         string
	Containers []Container
}

type KubePlayOutput struct {
	Pods []Pod
}
