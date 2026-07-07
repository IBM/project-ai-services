package miq

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net/http"

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

// GetUserByToken calls GET /api/users with the supplied MIQ token as X-Auth-Token
// and returns the caller's identity and group membership.
//
// Validated against ManageIQ API v4.4.0-pre at https://9.20.202.144:8443.
func (c *HTTPClient) GetUserByToken(ctx context.Context, miqToken string) (*UserInfo, error) {
	var result miqUsersResponse
	var errResp ErrorResponse

	resp, err := c.http.R().
		SetContext(ctx).
		SetHeader("X-Auth-Token", miqToken).
		SetQueryParams(map[string]string{
			"expand":     "resources",
			"attributes": "userid,name,miq_groups",
		}).
		SetResult(&result).
		SetError(&errResp).
		Get("/api/users")
	if err != nil {
		return nil, fmt.Errorf("miq: request failed: %w", err)
	}

	if resp.StatusCode() == http.StatusUnauthorized {
		return nil, ErrUnauthorized
	}
	if resp.IsError() {
		return nil, fmt.Errorf("miq: unexpected status %d: %s", resp.StatusCode(), errResp.Error.Message)
	}
	if len(result.Resources) == 0 {
		return nil, ErrUnauthorized
	}

	u := result.Resources[0]
	groups := make([]string, 0, len(u.MIQGroups))
	for _, g := range u.MIQGroups {
		if g.Description != "" {
			groups = append(groups, g.Description)
		}
	}

	return &UserInfo{
		ExternalID: u.ID,
		UserName:   u.UserID,
		FullName:   u.Name,
		Groups:     groups,
	}, nil
}
