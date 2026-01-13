package runtime

type Pod struct {
	ID         string
	Name       string
	Status     string
	Labels     map[string]string
	Containers []Container
}

type Container struct {
	ID     string
	Name   string
	Status string
}
