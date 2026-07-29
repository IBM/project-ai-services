package openshift

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/project-ai-services/ai-services/internal/pkg/logger"
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

var (
	// thanosOnce ensures the shared HTTP client and bearer token are initialised
	// exactly once for the lifetime of the process — consistent with the
	// clientsOnce / catalogOnce pattern used elsewhere in the codebase.
	thanosOnce   sync.Once
	thanosHTTP   *http.Client
	thanosToken  string
)

// initThanosClient builds the shared HTTP client and reads the SA bearer token
// from the filesystem. Called exactly once via thanosOnce.
func initThanosClient(ctx context.Context) {
	thanosOnce.Do(func() {
		thanosHTTP = &http.Client{
			Timeout: thanosHTTPTimeout,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{
					InsecureSkipVerify: true, //nolint:gosec // in-cluster self-signed cert
					MinVersion:         tls.VersionTLS12,
				},
			},
		}

		if data, err := os.ReadFile(serviceAccountTokenPath); err == nil {
			thanosToken = strings.TrimSpace(string(data))
		} else {
			logger.WarninglnCtx(ctx, "No Thanos bearer token found; requests will be unauthenticated")
		}
	})
}

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

	if len(result.Data.Result) == 0 {
		return 0, fmt.Errorf("thanos returned empty result set for query")
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
func queryThanos(ctx context.Context, query string) (float64, error) {
	initThanosClient(ctx)

	u, err := url.Parse(defaultThanosURL + "/api/v1/query")
	if err != nil {
		return 0, fmt.Errorf("invalid thanos URL: %w", err)
	}

	q := u.Query()
	q.Set("query", query)
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return 0, fmt.Errorf("failed to build thanos request: %w", err)
	}

	if thanosToken != "" {
		req.Header.Set("Authorization", "Bearer "+thanosToken)
	}

	resp, err := thanosHTTP.Do(req)
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
