package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	bundlesvc "github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/services/bundle"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/validators"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// -----------------------------------------------------------------------
// Mock BundleServiceInterface
// -----------------------------------------------------------------------

type mockBundleService struct {
	processBundle func(ctx context.Context, file io.Reader, userID string) (*bundlesvc.BundleResponse, error)
	validateBundle func(ctx context.Context, file io.Reader) (any, error)
}

func (m *mockBundleService) ProcessBundle(ctx context.Context, file io.Reader, userID string) (*bundlesvc.BundleResponse, error) {
	return m.processBundle(ctx, file, userID)
}
func (m *mockBundleService) ValidateBundle(ctx context.Context, file io.Reader) (any, error) {
	if m.validateBundle != nil {
		return m.validateBundle(ctx, file)
	}
	panic("ValidateBundle not set")
}
func (m *mockBundleService) ReplaceBundle(_ context.Context, _ *bundlesvc.BundleRecord, _ io.Reader, _ string) (*bundlesvc.BundleResponse, error) {
	panic("ReplaceBundle not set")
}
func (m *mockBundleService) GetByBundleID(_ context.Context, _ string) (*bundlesvc.BundleRecord, error) {
	panic("GetByBundleID not set")
}
func (m *mockBundleService) GetBundleByID(_ context.Context, _ string) (*bundlesvc.BundleResponse, error) {
	panic("GetBundleByID not set")
}
func (m *mockBundleService) DeleteBundle(_ context.Context, _ *bundlesvc.BundleRecord) error {
	panic("DeleteBundle not set")
}
func (m *mockBundleService) ListBundles(_ context.Context) (*bundlesvc.BundleListResponse, error) {
	panic("ListBundles not set")
}

// -----------------------------------------------------------------------
// Helpers
// -----------------------------------------------------------------------

// setupBundleRouter wires a BundleHandler backed by svc into a test gin engine.
func setupBundleRouter(svc bundlesvc.BundleServiceInterface) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	h := NewBundleHandler(svc)
	r.POST("/api/v1/catalog/bundles", h.CreateBundle)
	return r
}

// buildMultipartRequest builds a multipart/form-data POST request with a single
// "file" field whose filename and content are supplied by the caller.
func buildMultipartRequest(t *testing.T, filename string, content []byte) *http.Request {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	fw, err := w.CreateFormFile("file", filename)
	require.NoError(t, err)
	_, err = fw.Write(content)
	require.NoError(t, err)
	require.NoError(t, w.Close())

	req := httptest.NewRequest(http.MethodPost, "/api/v1/catalog/bundles", &buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	return req
}

// fixedBundleResponse returns a deterministic BundleResponse for use in stubs.
func fixedBundleResponse() *bundlesvc.BundleResponse {
	sz := int64(1024)
	return &bundlesvc.BundleResponse{
		ID:          "550e8400-e29b-41d4-a716-446655440000",
		Name:        "My Custom Service",
		Status:      "active",
		CatalogType: "service",
		CatalogID:   "my-service",
		Version:     "1.0.0",
		CreatedBy:   "admin",
		SizeBytes:   &sz,
		CreatedAt:   time.Date(2026, 5, 12, 9, 14, 2, 0, time.UTC),
		UpdatedAt:   time.Date(2026, 5, 12, 9, 14, 2, 0, time.UTC),
	}
}

// -----------------------------------------------------------------------
// TestCreateBundle
// -----------------------------------------------------------------------

func TestCreateBundle(t *testing.T) {
	validTarGz := []byte("fake-archive-content") // content; service validates internally

	tests := []struct {
		name           string
		filename       string
		fileContent    []byte
		omitFile       bool
		stubErr        error
		stubResp       *bundlesvc.BundleResponse
		wantStatus     int
		wantLocationOf string // non-empty → assert Location header contains this substring
		wantErrContains string
	}{
		{
			name:           "201 created — service bundle",
			filename:       "my-bundle.tar.gz",
			fileContent:    validTarGz,
			stubResp:       fixedBundleResponse(),
			wantStatus:     http.StatusCreated,
			wantLocationOf: "/api/v1/catalog/bundles/550e8400-e29b-41d4-a716-446655440000",
		},
		{
			name:        "400 — file field missing",
			omitFile:    true,
			wantStatus:  http.StatusBadRequest,
			wantErrContains: "missing or unreadable",
		},
		{
			name:        "400 — wrong extension (.zip)",
			filename:    "my-bundle.zip",
			fileContent: validTarGz,
			wantStatus:  http.StatusBadRequest,
			wantErrContains: ".tar.gz",
		},
		{
			name:        "400 — wrong extension (no extension)",
			filename:    "my-bundle",
			fileContent: validTarGz,
			wantStatus:  http.StatusBadRequest,
			wantErrContains: ".tar.gz",
		},
		{
			name:            "409 — conflict returned by service",
			filename:        "my-bundle.tar.gz",
			fileContent:     validTarGz,
			stubErr:         &validators.ValidationError{Code: http.StatusConflict, Message: "bundle already exists"},
			wantStatus:      http.StatusConflict,
			wantErrContains: "bundle already exists",
		},
		{
			name:            "422 — validation error returned by service",
			filename:        "my-bundle.tar.gz",
			fileContent:     validTarGz,
			stubErr:         &validators.ValidationError{Code: http.StatusUnprocessableEntity, Message: "'id' is required"},
			wantStatus:      http.StatusUnprocessableEntity,
			wantErrContains: "'id' is required",
		},
		{
			name:            "400 — bad request error from service",
			filename:        "my-bundle.tar.gz",
			fileContent:     validTarGz,
			stubErr:         &validators.ValidationError{Code: http.StatusBadRequest, Message: "invalid gzip archive"},
			wantStatus:      http.StatusBadRequest,
			wantErrContains: "invalid gzip",
		},
		{
			name:        "500 — unexpected service error",
			filename:    "my-bundle.tar.gz",
			fileContent: validTarGz,
			stubErr:     assert.AnError,
			wantStatus:  http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &mockBundleService{
				processBundle: func(_ context.Context, _ io.Reader, _ string) (*bundlesvc.BundleResponse, error) {
					return tt.stubResp, tt.stubErr
				},
			}
			router := setupBundleRouter(svc)
			w := httptest.NewRecorder()

			var req *http.Request
			if tt.omitFile {
				// Send a multipart body with no "file" field at all.
				var buf bytes.Buffer
				mw := multipart.NewWriter(&buf)
				require.NoError(t, mw.Close())
				req = httptest.NewRequest(http.MethodPost, "/api/v1/catalog/bundles", &buf)
				req.Header.Set("Content-Type", mw.FormDataContentType())
			} else {
				req = buildMultipartRequest(t, tt.filename, tt.fileContent)
			}

			router.ServeHTTP(w, req)

			assert.Equal(t, tt.wantStatus, w.Code)

			if tt.wantLocationOf != "" {
				assert.Equal(t, tt.wantLocationOf, w.Header().Get("Location"))
			}

			if tt.wantErrContains != "" {
				var body map[string]string
				require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
				assert.Contains(t, body["error"], tt.wantErrContains)
			}

			// On success, assert the response body matches the stub.
			if tt.wantStatus == http.StatusCreated {
				var resp bundlesvc.BundleResponse
				require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
				assert.Equal(t, tt.stubResp.ID, resp.ID)
				assert.Equal(t, tt.stubResp.Status, resp.Status)
				assert.Equal(t, tt.stubResp.CatalogID, resp.CatalogID)
				assert.Equal(t, tt.stubResp.CatalogType, resp.CatalogType)
				assert.Equal(t, tt.stubResp.Version, resp.Version)
			}
		})
	}
}

// TestCreateBundle_FilenameExtensionCaseInsensitive verifies that .TAR.GZ
// (uppercase) is also rejected — only lowercase .tar.gz is accepted.
func TestCreateBundle_FilenameExtensionCaseInsensitive(t *testing.T) {
	// .TAR.GZ lowercases to .tar.gz → should pass the extension check.
	svc := &mockBundleService{
		processBundle: func(_ context.Context, _ io.Reader, _ string) (*bundlesvc.BundleResponse, error) {
			return fixedBundleResponse(), nil
		},
	}
	router := setupBundleRouter(svc)
	w := httptest.NewRecorder()
	req := buildMultipartRequest(t, "MY-BUNDLE.TAR.GZ", []byte("content"))
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusCreated, w.Code)
}

// TestCreateBundle_MaxBytesEnforced verifies that a body exceeding maxBundleSizeBytes
// (20 MB compressed) results in a 400 Bad Request (http.MaxBytesReader causes FormFile to error).
func TestCreateBundle_MaxBytesEnforced(t *testing.T) {
	svc := &mockBundleService{}
	router := setupBundleRouter(svc)
	w := httptest.NewRecorder()

	// Build a body larger than 20 MB. MaxBytesReader triggers on read, not on the
	// Content-Length header, so we actually stream the bytes through the multipart writer.
	oversized := strings.NewReader(strings.Repeat("x", maxBundleSizeBytes+1))
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile("file", "big.tar.gz")
	require.NoError(t, err)
	_, err = io.Copy(fw, oversized)
	require.NoError(t, err)
	require.NoError(t, mw.Close())

	req := httptest.NewRequest(http.MethodPost, "/api/v1/catalog/bundles", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// TestCreateBundle_UserIDPropagated verifies that the userID extracted from the
// Gin context is forwarded to ProcessBundle.
func TestCreateBundle_UserIDPropagated(t *testing.T) {
	const wantUserID = "test-admin"
	var gotUserID string

	svc := &mockBundleService{
		processBundle: func(_ context.Context, _ io.Reader, userID string) (*bundlesvc.BundleResponse, error) {
			gotUserID = userID
			return fixedBundleResponse(), nil
		},
	}

	gin.SetMode(gin.TestMode)
	r := gin.New()
	h := NewBundleHandler(svc)
	// Inject the user ID into the context the same way AuthMiddleware would.
	r.POST("/api/v1/catalog/bundles", func(c *gin.Context) {
		c.Set("user_id", wantUserID)
		h.CreateBundle(c)
	})

	w := httptest.NewRecorder()
	req := buildMultipartRequest(t, "bundle.tar.gz", []byte("content"))
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)
	assert.Equal(t, wantUserID, gotUserID)
}
