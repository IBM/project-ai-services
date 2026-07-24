package openshift

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	// defaultThanosURL is the in-cluster Thanos Querier service address.
	defaultThanosURL = "https://thanos-querier.openshift-monitoring.svc.cluster.local:9091"

	// serviceAccountTokenPath is the projected SA token when running in-cluster.
	serviceAccountTokenPath = "/var/run/secrets/kubernetes.io/serviceaccount/token"

	// thanosHTTPTimeout is the per-request timeout for Thanos queries.
	thanosHTTPTimeout = 15 * time.Second

	// thanosResponseSizeLimit caps the Thanos response body to 1 MiB.
	thanosResponseSizeLimit = 1 << 20

	// thanosValueLen is the expected length of each result Value tuple [timestamp, value].
	thanosValueLen = 2
)

// thanosResponse is the JSON envelope returned by /api/v1/query.
type thanosResponse struct {
	Status string `json:"status"`
	Data   struct {
		ResultType string `json:"resultType"`
		Result     []struct {
			Metric map[string]string `json:"metric"`
			Value  [2]any            `json:"value"` // [timestamp, "stringValue"]
		} `json:"result"`
	} `json:"data"`
	Error string `json:"error"`
}

// buildThanosHTTPClient returns an HTTP client configured for the in-cluster
// Thanos Querier (TLS 1.2+, self-signed cert accepted, fixed timeout).
func buildThanosHTTPClient() *http.Client {
	return &http.Client{
		Timeout: thanosHTTPTimeout,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true, //nolint:gosec // in-cluster self-signed cert
				MinVersion:         tls.VersionTLS12,
			},
		},
	}
}

// parseThanosResponse unmarshals the Thanos JSON body, validates the status,
// and sums all result values into a single float64.
func parseThanosResponse(body []byte) (float64, error) {
	var result thanosResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return 0, fmt.Errorf("failed to parse thanos response: %w", err)
	}

	if result.Status != "success" {
		return 0, fmt.Errorf("thanos returned non-success status %q: %s", result.Status, result.Error)
	}

	var total float64

	for _, r := range result.Data.Result {
		if len(r.Value) < thanosValueLen {
			continue
		}

		s, ok := r.Value[1].(string)
		if !ok {
			continue
		}

		v, err := strconv.ParseFloat(s, 64)
		if err != nil {
			continue
		}

		total += v
	}

	return total, nil
}

// queryThanos executes an instant PromQL query against the Thanos Querier and
// returns the result as a float64. For vector results all values are summed,
// which is appropriate for both cluster-wide and filtered per-pod aggregations.
func queryThanos(query string) (float64, error) {
	u, err := url.Parse(defaultThanosURL + "/api/v1/query")
	if err != nil {
		return 0, fmt.Errorf("invalid thanos URL: %w", err)
	}

	q := u.Query()
	q.Set("query", query)
	u.RawQuery = q.Encode()

	req, err := http.NewRequest(http.MethodGet, u.String(), nil)
	if err != nil {
		return 0, fmt.Errorf("failed to build thanos request: %w", err)
	}

	token := loadThanosToken()
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := buildThanosHTTPClient().Do(req)
	if err != nil {
		return 0, fmt.Errorf("thanos query failed: %w", err)
	}

	defer func() {
		_ = resp.Body.Close()
	}()

	body, err := io.ReadAll(io.LimitReader(resp.Body, thanosResponseSizeLimit))
	if err != nil {
		return 0, fmt.Errorf("failed to read thanos response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("thanos returned HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	return parseThanosResponse(body)
}

// loadThanosToken returns the bearer token for Thanos from the in-cluster
// projected service-account token file. Returns an empty string when the file
// is not present (e.g. out-of-cluster development).
func loadThanosToken() string {
	if data, err := os.ReadFile(serviceAccountTokenPath); err == nil {
		return strings.TrimSpace(string(data))
	}

	return ""
}
