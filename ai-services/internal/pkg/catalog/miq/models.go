package miq

// AuthResponse is returned by GET /api/auth on successful Basic-auth.
type AuthResponse struct {
	Token    string `json:"auth_token"`
	TokenTTL int    `json:"token_ttl"`
}

// UserInfo carries the identity fields the Catalog API needs from ManageIQ.
type UserInfo struct {
	// ExternalID is the ManageIQ numeric user ID extracted from identity.user_href.
	ExternalID string
	// UserName is the ManageIQ userid field.
	UserName string
	// FullName is the ManageIQ name field.
	FullName string
	// Groups is the list of group descriptions for this user.
	Groups []string
}

// miqIdentity is the "identity" block returned by GET /api?attributes=identity.
type miqIdentity struct {
	UserID   string   `json:"userid"`
	Name     string   `json:"name"`
	UserHref string   `json:"user_href"`
	Groups   []string `json:"groups"`
}

// miqIdentityResponse is the top-level JSON shape for GET /api?attributes=identity.
type miqIdentityResponse struct {
	Identity miqIdentity `json:"identity"`
}

// ErrorResponse is the JSON error body returned by ManageIQ on failure.
type ErrorResponse struct {
	Error struct {
		Kind    string `json:"kind"`
		Message string `json:"message"`
	} `json:"error"`
}
