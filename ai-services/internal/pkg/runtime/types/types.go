package types

type Pod struct {
	ID         string
	Name       string
	Status     string
	Labels     map[string]string
	Containers []Container
}

type Container struct {
	ID     string `json:"ID"`
	Name   string
	Status string
}

type Image struct {
	RepoTags    []string
	RepoDigests []string
}
