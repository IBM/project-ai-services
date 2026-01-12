package httpclient

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"
)

const defaultTimeoutSeconds = 30 // default HTTP client timeout in seconds.

type HTTPClient struct {
	BaseURL    string
	HTTPClient *http.Client
}

// NewHTTPClient creates a new HTTPClient with base URL from env or default.
func NewHTTPClient() *HTTPClient {
	baseURL := os.Getenv("AI_SERVICES_BASE_URL")
	if baseURL == "" {
		baseURL = "http://localhost:8080"
	}

	return &HTTPClient{
		BaseURL: baseURL,
		HTTPClient: &http.Client{
			Timeout: time.Duration(defaultTimeoutSeconds) * time.Second,
		},
	}
}

func (c *HTTPClient) buildURL(path string) string {
	return fmt.Sprintf("%s%s", c.BaseURL, path)
}

// Get performs an HTTP GET request.
func (c *HTTPClient) Get(path string) (*http.Response, error) {
	return c.HTTPClient.Get(c.buildURL(path))
}

// Post performs an HTTP POST request with JSON body.
func (c *HTTPClient) Post(path string, body interface{}) (*http.Response, error) {
	b, _ := json.Marshal(body)

	return c.HTTPClient.Post(c.buildURL(path), "application/json", bytes.NewBuffer(b))
}

// Put performs an HTTP PUT request with JSON body.
func (c *HTTPClient) Put(path string, body interface{}) (*http.Response, error) {
	b, _ := json.Marshal(body)
	req, _ := http.NewRequest("PUT", c.buildURL(path), bytes.NewBuffer(b))
	req.Header.Set("Content-Type", "application/json")

	return c.HTTPClient.Do(req)
}

// Delete performs an HTTP DELETE request.
func (c *HTTPClient) Delete(path string) (*http.Response, error) {
	req, _ := http.NewRequest("DELETE", c.buildURL(path), nil)

	return c.HTTPClient.Do(req)
}
