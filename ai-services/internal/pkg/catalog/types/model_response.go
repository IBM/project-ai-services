package types

// ModelPSResponse represents the response for process and pod status of a model.
type ModelPSResponse struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	DeploymentType string `json:"deployment_type"`
	Status         string `json:"status,omitempty"`
	Message        string `json:"message,omitempty"`
	ComponentType  string `json:"component_type,omitempty"`
	ProviderID     string `json:"provider_id,omitempty"`
	Model          string `json:"model,omitempty"`
	WorkerName     string `json:"worker_name,omitempty"`
	Namespace      string `json:"namespace,omitempty"`
	RuntimeType    string `json:"runtime_type,omitempty"`
	Pods           []Pod  `json:"pods"`
}
