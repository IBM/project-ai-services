package models

// CreateDatasourceRequest is the request body for creating a new datasource connector.
type CreateDatasourceRequest struct {
	// Name is the unique human-readable label for this connector.
	// Must be 3–100 characters; only letters, digits, hyphens (-), and underscores (_) are
	// allowed. Duplicate-name detection is case-insensitive ("My-DB" and "my-db" conflict).
	Name string `json:"name" binding:"required,min=3,max=100"`
	// ProviderID identifies the provider implementation (e.g. "object_storage", "file_system").
	ProviderID string `json:"provider_id" binding:"required"`
	// Params holds the provider-specific configuration. Sensitive fields (format: "password"
	// in the JSON schema) are encrypted at rest; all other fields are stored in plain text.
	Params map[string]any `json:"params" binding:"required"`
	// CreatedBy is set from the auth context, never from the request body.
	CreatedBy string `json:"-"`
}

// CreateDatasourceResponse is the response body returned after a successful datasource creation.
type CreateDatasourceResponse struct {
	ID string `json:"id"`
}

// Made with Bob
