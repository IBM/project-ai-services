package common

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

// PaginationInfo holds page metadata for list responses.
type PaginationInfo struct {
	Total  int `json:"total"`
	Limit  int `json:"limit"`
	Offset int `json:"offset"`
}

// HTTPConfigForTimeout returns an HTTP client config with the given timeout and no connection pooling.
func HTTPConfigForTimeout(timeout time.Duration) HTTPClientConfig {
	return HTTPClientConfig{
		Timeout:            timeout,
		InsecureSkipVerify: true,
		PoolConnections:    false,
	}
}

// DeleteURL sends a DELETE request and accepts 200 or 204 as success.
func DeleteURL(ctx context.Context, rawURL string, config HTTPClientConfig) error {
	_, statusCode, err := DoDELETE(ctx, rawURL, config)
	if err != nil {
		return err
	}

	if statusCode != http.StatusOK && statusCode != http.StatusNoContent {
		return fmt.Errorf("unexpected status %d", statusCode)
	}

	return nil
}

// AppendQueryParams returns rawURL with the non-empty params added as an encoded query string.
func AppendQueryParams(rawURL string, params map[string]string) string {
	query := url.Values{}

	for key, val := range params {
		if val != "" {
			query.Set(key, val)
		}
	}

	if len(query) == 0 {
		return rawURL
	}

	return rawURL + "?" + query.Encode()
}

// GetJSON sends a GET request, requires HTTP 200 and decodes the JSON body into a T.
func GetJSON[T any](ctx context.Context, rawURL string, config HTTPClientConfig) (*T, error) {
	respBody, statusCode, err := DoGET(ctx, rawURL, config)
	if err != nil {
		return nil, err
	}

	if statusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d: %s", statusCode, string(respBody))
	}

	var out T
	if err := json.Unmarshal(respBody, &out); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &out, nil
}

// FieldHasValue reports whether data has key set to a non-nil, non-empty-string value.
func FieldHasValue(data map[string]interface{}, key string) bool {
	val, ok := data[key]
	if !ok || val == nil {
		return false
	}

	if s, isStr := val.(string); isStr {
		return s != ""
	}

	return true
}

// AllFieldsPresent reports whether every key in expected exists in data.
// When one is missing it returns that key and false.
func AllFieldsPresent(data map[string]interface{}, expected []string) (string, bool) {
	for _, key := range expected {
		if _, ok := data[key]; !ok {
			return key, false
		}
	}

	return "", true
}
