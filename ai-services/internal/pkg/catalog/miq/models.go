package miq

// AuthResponse is returned by GET /api/auth on successful Basic-auth.
type AuthResponse struct {
	Token    string `json:"auth_token"`
	TokenTTL int    `json:"token_ttl"`
}

// Group represents one entry in the miq_groups array on a user resource.
// type Group struct {
// 	Description string `json:"description"`
// }

// UserInfo carries the identity fields the Catalog API needs from ManageIQ.
type UserInfo struct {
	// ExternalID is the ManageIQ numeric user ID (string form of the "id" field).
	//ExternalID string
	// UserName is the ManageIQ userid field.
	UserName string
	// FullName is the ManageIQ name field.
	FullName string
	// Groups is the list of miq_groups descriptions for this user.
	Groups []string
}

// miqIdentityResource is the JSON shape returned by GET /api with attributes=identity.
type miqIdentityResource struct {
	//ID       string     `json:"id"`
	UserID   string     `json:"userid"`
	Name     string     `json:"name"`
	Groups   []string    `json:"groups"`
}

// miqIdentityResponse is the top-level JSON shape for GET /api.
type miqIdentityResponse struct {
	Identity miqIdentityResource `json:"identity"`
}

// ErrorResponse is the JSON error body returned by ManageIQ on failure.
type ErrorResponse struct {
	Error struct {
		Kind    string `json:"kind"`
		Message string `json:"message"`
	} `json:"error"`
}
