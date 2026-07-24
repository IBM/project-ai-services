package miq

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net/http"
	"path"

	"github.com/go-resty/resty/v2"
)

// ErrUnauthorized is returned when ManageIQ rejects the token with a 401.
var ErrUnauthorized = errors.New("unauthorized: invalid or expired ManageIQ token")

// Client defines the ManageIQ operations needed by the Catalog API.
type Client interface {
	// GetUserByToken validates miqToken against ManageIQ and returns the
	// caller's identity and group membership. Returns ErrUnauthorized if
	// ManageIQ responds with 401.
	GetUserByToken(ctx context.Context, miqToken string) (*UserInfo, error)
}

// HTTPClient is the production implementation of Client.
type HTTPClient struct {
	http *resty.Client
}

// NewHTTPClient creates a ManageIQ HTTP client.
// Set insecureSkipTLS=true when the ManageIQ instance uses a self-signed cert.
func NewHTTPClient(baseURL string, insecureSkipTLS bool) *HTTPClient {
	r := resty.New().
		SetBaseURL(baseURL).
		SetTLSClientConfig(&tls.Config{InsecureSkipVerify: insecureSkipTLS}). //nolint:gosec
		SetHeader("Accept", "application/json")

	return &HTTPClient{http: r}
}

// GetUserByToken calls GET /api?attributes=identity with the supplied MIQ token
// as X-Auth-Token and returns the caller's identity and group membership.
// The numeric user ID is extracted from the identity.user_href path segment.
//
// Validated against ManageIQ API v4.4.0-pre at https://9.20.202.144:8443.
func (c *HTTPClient) GetUserByToken(ctx context.Context, miqToken string) (*UserInfo, error) {
	var result miqIdentityResponse
	var errResp ErrorResponse

	resp, err := c.http.R().
		SetContext(ctx).
		SetHeader("X-Auth-Token", miqToken).
		SetQueryParam("attributes", "identity").
		SetResult(&result).
		SetError(&errResp).
		Get("/api")
	if err != nil {
		return nil, fmt.Errorf("miq: request failed: %w", err)
	}

	if resp.StatusCode() == http.StatusUnauthorized {
		return nil, ErrUnauthorized
	}
	if resp.IsError() {
		return nil, fmt.Errorf("miq: unexpected status %d: %s", resp.StatusCode(), errResp.Error.Message)
	}
	if result.Identity.UserID == "" {
		return nil, ErrUnauthorized
	}

	// Extract numeric user ID from user_href, e.g. ".../api/users/1" → "1".
	externalID := path.Base(result.Identity.UserHref)

	return &UserInfo{
		ExternalID: externalID,
		UserName:   result.Identity.UserID,
		FullName:   result.Identity.Name,
		Groups:     result.Identity.Groups,
	}, nil
}
