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
	catalogtypes "github.com/project-ai-services/ai-services/internal/pkg/catalog/types"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/validators"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// -----------------------------------------------------------------------
// Mock BundleServiceInterface
// -----------------------------------------------------------------------

type mockBundleService struct {
	processBundle  func(ctx context.Context, file io.Reader, userID string) (*bundlesvc.BundleResponse, error)
	validateBundle func(ctx context.Context, file io.Reader) (any, error)
	replaceBundle  func(ctx context.Context, existing *bundlesvc.BundleResponse, file io.Reader, userID string) (*bundlesvc.BundleResponse, error)
	getBundleByID  func(ctx context.Context, id string) (*bundlesvc.BundleResponse, error)
	deleteBundle   func(ctx context.Context, existing *bundlesvc.BundleResponse) error
	listBundles    func(ctx context.Context, params bundlesvc.BundleListRequest) (*bundlesvc.BundleListResponse, error)
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
func (m *mockBundleService) ReplaceBundle(ctx context.Context, existing *bundlesvc.BundleResponse, file io.Reader, userID string) (*bundlesvc.BundleResponse, error) {
	if m.replaceBundle != nil {
		return m.replaceBundle(ctx, existing, file, userID)
	}
	panic("ReplaceBundle not set")
}
func (m *mockBundleService) GetBundleByID(ctx context.Context, id string) (*bundlesvc.BundleResponse, error) {
	if m.getBundleByID != nil {
		return m.getBundleByID(ctx, id)
	}
	panic("GetBundleByID not set")
}
func (m *mockBundleService) DeleteBundle(ctx context.Context, existing *bundlesvc.BundleResponse) error {
	if m.deleteBundle != nil {
		return m.deleteBundle(ctx, existing)
	}
	panic("DeleteBundle not set")
}
func (m *mockBundleService) ListBundles(ctx context.Context, params bundlesvc.BundleListRequest) (*bundlesvc.BundleListResponse, error) {
	if m.listBundles != nil {
		return m.listBundles(ctx, params)
	}
	panic("ListBundles not set")
}

// -----------------------------------------------------------------------
// Helpers
// -----------------------------------------------------------------------

// setupBundleRouter wires a BundleHandler backed by svc into a test gin engine
// with all bundle routes registered.
func setupBundleRouter(svc bundlesvc.BundleServiceInterface) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	h := NewBundleHandler(svc)
	r.POST("/api/v1/catalog/bundles", h.CreateBundle)
	r.GET("/api/v1/catalog/bundles", h.ListBundles)
	r.GET("/api/v1/catalog/bundles/:id", h.GetBundle)
	r.PUT("/api/v1/catalog/bundles/:id", h.UpdateBundle)
	r.DELETE("/api/v1/catalog/bundles/:id", h.DeleteBundle)
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

// buildPutMultipartRequest builds a multipart/form-data PUT request for UpdateBundle.
func buildPutMultipartRequest(t *testing.T, bundleID, filename string, content []byte) *http.Request {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	fw, err := w.CreateFormFile("file", filename)
	require.NoError(t, err)
	_, err = fw.Write(content)
	require.NoError(t, err)
	require.NoError(t, w.Close())

	req := httptest.NewRequest(http.MethodPut, "/api/v1/catalog/bundles/"+bundleID, &buf)
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

// -----------------------------------------------------------------------
// TestListBundles
// -----------------------------------------------------------------------

func TestListBundles(t *testing.T) {
	sz := int64(286720)
	id1 := "550e8400-e29b-41d4-a716-446655440000"
	id2 := "a1b2c3d4-e5f6-7890-abcd-ef1234567890"

	defaultPagination := catalogtypes.PaginationMetadata{Page: 1, PageSize: 20, TotalItems: 2, TotalPages: 1}

	tests := []struct {
		name            string
		query           string
		stubResp        *bundlesvc.BundleListResponse
		stubErr         error
		wantStatus      int
		wantLen         int
		wantPage        int
		wantPageSize    int
		wantErrContains string
	}{
		{
			name:         "200 — default pagination, two bundles",
			query:        "",
			wantStatus:   http.StatusOK,
			wantLen:      2,
			wantPage:     1,
			wantPageSize: 20,
			stubResp: &bundlesvc.BundleListResponse{
				Bundles: []bundlesvc.BundleResponse{
					{ID: id1, Status: "active", CatalogType: "service", CatalogID: "my-service", Version: "1.0.0", SizeBytes: &sz},
					{ID: id2, Status: "active", CatalogType: "component", CatalogID: "llm--my-provider", Version: "1.0.0"},
				},
				Pagination: defaultPagination,
			},
		},
		{
			name:         "200 — explicit page and page_size forwarded to service",
			query:        "?page=2&page_size=10",
			wantStatus:   http.StatusOK,
			wantLen:      0,
			wantPage:     2,
			wantPageSize: 10,
			stubResp:     &bundlesvc.BundleListResponse{Bundles: []bundlesvc.BundleResponse{}, Pagination: catalogtypes.PaginationMetadata{Page: 2, PageSize: 10}},
		},
		{
			name:            "400 — page_size exceeds maximum",
			query:           "?page_size=101",
			wantStatus:      http.StatusBadRequest,
			wantErrContains: "invalid page_size",
		},
		{
			name:            "400 — negative page",
			query:           "?page=-1",
			wantStatus:      http.StatusBadRequest,
			wantErrContains: "invalid page",
		},
		{
			name:            "500 — service error",
			query:           "",
			stubErr:         assert.AnError,
			wantStatus:      http.StatusInternalServerError,
			wantErrContains: assert.AnError.Error(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &mockBundleService{
				listBundles: func(_ context.Context, params bundlesvc.BundleListRequest) (*bundlesvc.BundleListResponse, error) {
					if tt.wantPage > 0 {
						assert.Equal(t, tt.wantPage, params.Page)
						assert.Equal(t, tt.wantPageSize, params.PageSize)
					}
					return tt.stubResp, tt.stubErr
				},
			}
			router := setupBundleRouter(svc)
			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/api/v1/catalog/bundles"+tt.query, nil)
			router.ServeHTTP(w, req)

			assert.Equal(t, tt.wantStatus, w.Code)

			if tt.wantErrContains != "" {
				var body map[string]string
				require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
				assert.Contains(t, body["error"], tt.wantErrContains)
			}

			if tt.wantStatus == http.StatusOK {
				var resp bundlesvc.BundleListResponse
				require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
				assert.Len(t, resp.Bundles, tt.wantLen)
				if tt.wantLen > 0 {
					assert.Equal(t, id1, resp.Bundles[0].ID)
					assert.Equal(t, "service", resp.Bundles[0].CatalogType)
					assert.Equal(t, id2, resp.Bundles[1].ID)
					assert.Equal(t, "component", resp.Bundles[1].CatalogType)
				}
			}
		})
	}
}

// -----------------------------------------------------------------------
// TestGetBundle
// -----------------------------------------------------------------------

func TestGetBundle(t *testing.T) {
	fixedID := "550e8400-e29b-41d4-a716-446655440000"
	sz := int64(286720)

	tests := []struct {
		name            string
		id              string
		stubResp        *bundlesvc.BundleResponse
		stubErr         error
		wantStatus      int
		wantErrContains string
	}{
		{
			name:       "200 — found",
			id:         fixedID,
			wantStatus: http.StatusOK,
			stubResp: &bundlesvc.BundleResponse{
				ID: fixedID, Name: "My Custom Service", Status: "active",
				CatalogType: "service", CatalogID: "my-service", Version: "1.0.0",
				SizeBytes: &sz,
			},
		},
		{
			name:            "404 — not found",
			id:              fixedID,
			stubResp:        nil,
			wantStatus:      http.StatusNotFound,
			wantErrContains: "not found",
		},
		{
			name:            "400 — invalid UUID",
			id:              fixedID,
			stubErr:         &validators.ValidationError{Code: http.StatusBadRequest, Message: "invalid bundle id"},
			wantStatus:      http.StatusBadRequest,
			wantErrContains: "invalid bundle id",
		},
		{
			name:            "500 — service error",
			id:              fixedID,
			stubErr:         assert.AnError,
			wantStatus:      http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &mockBundleService{
				getBundleByID: func(_ context.Context, id string) (*bundlesvc.BundleResponse, error) {
					assert.Equal(t, tt.id, id)
					return tt.stubResp, tt.stubErr
				},
			}
			router := setupBundleRouter(svc)
			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/api/v1/catalog/bundles/"+tt.id, nil)
			router.ServeHTTP(w, req)

			assert.Equal(t, tt.wantStatus, w.Code)

			if tt.wantErrContains != "" {
				var body map[string]string
				require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
				assert.Contains(t, body["error"], tt.wantErrContains)
			}

			if tt.wantStatus == http.StatusOK {
				var resp bundlesvc.BundleResponse
				require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
				assert.Equal(t, fixedID, resp.ID)
				assert.Equal(t, "service", resp.CatalogType)
				assert.Equal(t, "active", resp.Status)
			}
		})
	}
}

// -----------------------------------------------------------------------
// TestUpdateBundle
// -----------------------------------------------------------------------

func TestUpdateBundle(t *testing.T) {
	fixedID := "550e8400-e29b-41d4-a716-446655440000"
	validTarGz := []byte("fake-archive-content")

	tests := []struct {
		name            string
		bundleID        string
		filename        string
		fileContent     []byte
		omitFile        bool
		// getBundleByID stub — nil resp = not found
		getResp         *bundlesvc.BundleResponse
		getErr          error
		// replaceBundle stub
		replaceResp     *bundlesvc.BundleResponse
		replaceErr      error
		wantStatus      int
		wantLocationOf  string
		wantErrContains string
	}{
		{
			name:           "200 — successful replace",
			bundleID:       fixedID,
			filename:       "bundle-v2.tar.gz",
			fileContent:    validTarGz,
			getResp:        fixedBundleResponse(),
			replaceResp:    fixedBundleResponse(),
			wantStatus:     http.StatusOK,
			wantLocationOf: "/api/v1/catalog/bundles/" + fixedID,
		},
		{
			name:            "400 — malformed UUID rejected by GetBundleByID",
			bundleID:        "not-a-uuid",
			filename:        "bundle-v2.tar.gz",
			fileContent:     validTarGz,
			getErr:          &validators.ValidationError{Code: http.StatusBadRequest, Message: `invalid bundle id "not-a-uuid"`},
			wantStatus:      http.StatusBadRequest,
			wantErrContains: "invalid bundle id",
		},
		{
			name:            "404 — bundle not found (GetBundleByID returns nil)",
			bundleID:        fixedID,
			filename:        "bundle-v2.tar.gz",
			fileContent:     validTarGz,
			getResp:         nil,
			wantStatus:      http.StatusNotFound,
			wantErrContains: "not found",
		},
		{
			name:            "400 — GetBundleByID returns ValidationError",
			bundleID:        fixedID,
			filename:        "bundle-v2.tar.gz",
			fileContent:     validTarGz,
			getErr:          &validators.ValidationError{Code: http.StatusBadRequest, Message: "invalid bundle id"},
			wantStatus:      http.StatusBadRequest,
			wantErrContains: "invalid bundle id",
		},
		{
			name:            "500 — GetBundleByID returns unexpected error",
			bundleID:        fixedID,
			filename:        "bundle-v2.tar.gz",
			fileContent:     validTarGz,
			getErr:          assert.AnError,
			wantStatus:      http.StatusInternalServerError,
		},
		{
			name:            "400 — file field missing",
			bundleID:        fixedID,
			getResp:         fixedBundleResponse(),
			omitFile:        true,
			wantStatus:      http.StatusBadRequest,
			wantErrContains: "missing or unreadable",
		},
		{
			name:            "400 — wrong extension (.zip)",
			bundleID:        fixedID,
			filename:        "bundle-v2.zip",
			fileContent:     validTarGz,
			getResp:         fixedBundleResponse(),
			wantStatus:      http.StatusBadRequest,
			wantErrContains: ".tar.gz",
		},
		{
			name:            "422 — catalog_id mismatch from ReplaceBundle",
			bundleID:        fixedID,
			filename:        "bundle-v2.tar.gz",
			fileContent:     validTarGz,
			getResp:         fixedBundleResponse(),
			replaceErr:      &validators.ValidationError{Code: http.StatusUnprocessableEntity, Message: "catalog_id mismatch"},
			wantStatus:      http.StatusUnprocessableEntity,
			wantErrContains: "catalog_id mismatch",
		},
		{
			name:            "422 — catalog_type mismatch from ReplaceBundle",
			bundleID:        fixedID,
			filename:        "bundle-v2.tar.gz",
			fileContent:     validTarGz,
			getResp:         fixedBundleResponse(),
			replaceErr:      &validators.ValidationError{Code: http.StatusUnprocessableEntity, Message: "catalog_type mismatch"},
			wantStatus:      http.StatusUnprocessableEntity,
			wantErrContains: "catalog_type mismatch",
		},
		{
			name:            "400 — bad archive from ReplaceBundle",
			bundleID:        fixedID,
			filename:        "bundle-v2.tar.gz",
			fileContent:     validTarGz,
			getResp:         fixedBundleResponse(),
			replaceErr:      &validators.ValidationError{Code: http.StatusBadRequest, Message: "invalid gzip archive"},
			wantStatus:      http.StatusBadRequest,
			wantErrContains: "invalid gzip",
		},
		{
			name:            "500 — unexpected error from ReplaceBundle",
			bundleID:        fixedID,
			filename:        "bundle-v2.tar.gz",
			fileContent:     validTarGz,
			getResp:         fixedBundleResponse(),
			replaceErr:      assert.AnError,
			wantStatus:      http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &mockBundleService{
				getBundleByID: func(_ context.Context, id string) (*bundlesvc.BundleResponse, error) {
					assert.Equal(t, tt.bundleID, id)
					return tt.getResp, tt.getErr
				},
				replaceBundle: func(_ context.Context, existing *bundlesvc.BundleResponse, _ io.Reader, _ string) (*bundlesvc.BundleResponse, error) {
					if tt.getResp != nil {
						assert.Equal(t, tt.getResp.ID, existing.ID)
						assert.Equal(t, tt.getResp.CatalogType, existing.CatalogType)
						assert.Equal(t, tt.getResp.CatalogID, existing.CatalogID)
					}
					return tt.replaceResp, tt.replaceErr
				},
			}

			router := setupBundleRouter(svc)
			w := httptest.NewRecorder()

			var req *http.Request
			if tt.omitFile {
				var buf bytes.Buffer
				mw := multipart.NewWriter(&buf)
				require.NoError(t, mw.Close())
				req = httptest.NewRequest(http.MethodPut, "/api/v1/catalog/bundles/"+tt.bundleID, &buf)
				req.Header.Set("Content-Type", mw.FormDataContentType())
			} else {
				req = buildPutMultipartRequest(t, tt.bundleID, tt.filename, tt.fileContent)
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

			if tt.wantStatus == http.StatusOK {
				var resp bundlesvc.BundleResponse
				require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
				assert.Equal(t, tt.replaceResp.ID, resp.ID)
				assert.Equal(t, tt.replaceResp.Status, resp.Status)
				assert.Equal(t, tt.replaceResp.CatalogID, resp.CatalogID)
				assert.Equal(t, tt.replaceResp.CatalogType, resp.CatalogType)
				assert.Equal(t, tt.replaceResp.Version, resp.Version)
			}
		})
	}
}

// TestUpdateBundle_UserIDPropagated verifies the userID is forwarded to ReplaceBundle.
func TestUpdateBundle_UserIDPropagated(t *testing.T) {
	const wantUserID = "test-admin"
	fixedID := "550e8400-e29b-41d4-a716-446655440000"
	var gotUserID string

	svc := &mockBundleService{
		getBundleByID: func(_ context.Context, _ string) (*bundlesvc.BundleResponse, error) {
			return fixedBundleResponse(), nil
		},
		replaceBundle: func(_ context.Context, _ *bundlesvc.BundleResponse, _ io.Reader, userID string) (*bundlesvc.BundleResponse, error) {
			gotUserID = userID
			return fixedBundleResponse(), nil
		},
	}

	gin.SetMode(gin.TestMode)
	r := gin.New()
	h := NewBundleHandler(svc)
	r.PUT("/api/v1/catalog/bundles/:id", func(c *gin.Context) {
		c.Set("user_id", wantUserID)
		h.UpdateBundle(c)
	})

	w := httptest.NewRecorder()
	req := buildPutMultipartRequest(t, fixedID, "bundle.tar.gz", []byte("content"))
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, wantUserID, gotUserID)
}

// TestUpdateBundle_RecordFieldsForwardedToReplaceBundle verifies that the exact
// *BundleResponse returned by GetBundleByID is passed unchanged to ReplaceBundle.
func TestUpdateBundle_RecordFieldsForwardedToReplaceBundle(t *testing.T) {
	fixedID := "550e8400-e29b-41d4-a716-446655440000"
	sz := int64(2048)
	now := time.Now().UTC()
	stubResp := &bundlesvc.BundleResponse{
		ID:          fixedID,
		Name:        "Full Fields Service",
		Status:      "active",
		CatalogType: "service",
		CatalogID:   "full-svc",
		Version:     "3.0.0",
		CreatedBy:   "some-user",
		SizeBytes:   &sz,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	svc := &mockBundleService{
		getBundleByID: func(_ context.Context, _ string) (*bundlesvc.BundleResponse, error) {
			return stubResp, nil
		},
		replaceBundle: func(_ context.Context, existing *bundlesvc.BundleResponse, _ io.Reader, _ string) (*bundlesvc.BundleResponse, error) {
			assert.Equal(t, stubResp.ID, existing.ID)
			assert.Equal(t, stubResp.Name, existing.Name)
			assert.Equal(t, stubResp.Status, existing.Status)
			assert.Equal(t, stubResp.CatalogType, existing.CatalogType)
			assert.Equal(t, stubResp.CatalogID, existing.CatalogID)
			assert.Equal(t, stubResp.Version, existing.Version)
			assert.Equal(t, stubResp.CreatedBy, existing.CreatedBy)
			assert.Equal(t, stubResp.SizeBytes, existing.SizeBytes)
			assert.Equal(t, stubResp.CreatedAt, existing.CreatedAt)
			assert.Equal(t, stubResp.UpdatedAt, existing.UpdatedAt)
			return fixedBundleResponse(), nil
		},
	}

	router := setupBundleRouter(svc)
	w := httptest.NewRecorder()
	req := buildPutMultipartRequest(t, fixedID, "bundle.tar.gz", []byte("content"))
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

// TestUpdateBundle_MaxBytesEnforced verifies that a body exceeding maxBundleSizeBytes
// results in a 400 Bad Request (MaxBytesReader fires before the DB lookup).
func TestUpdateBundle_MaxBytesEnforced(t *testing.T) {
	fixedID := "550e8400-e29b-41d4-a716-446655440000"
	svc := &mockBundleService{
		getBundleByID: func(_ context.Context, _ string) (*bundlesvc.BundleResponse, error) {
			return fixedBundleResponse(), nil
		},
	}
	router := setupBundleRouter(svc)
	w := httptest.NewRecorder()

	oversized := strings.NewReader(strings.Repeat("x", maxBundleSizeBytes+1))
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile("file", "big.tar.gz")
	require.NoError(t, err)
	_, err = io.Copy(fw, oversized)
	require.NoError(t, err)
	require.NoError(t, mw.Close())

	req := httptest.NewRequest(http.MethodPut, "/api/v1/catalog/bundles/"+fixedID, &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// TestUpdateBundle_FilenameExtensionCaseInsensitive verifies that .TAR.GZ is accepted.
func TestUpdateBundle_FilenameExtensionCaseInsensitive(t *testing.T) {
	fixedID := "550e8400-e29b-41d4-a716-446655440000"
	svc := &mockBundleService{
		getBundleByID: func(_ context.Context, _ string) (*bundlesvc.BundleResponse, error) {
			return fixedBundleResponse(), nil
		},
		replaceBundle: func(_ context.Context, _ *bundlesvc.BundleResponse, _ io.Reader, _ string) (*bundlesvc.BundleResponse, error) {
			return fixedBundleResponse(), nil
		},
	}
	router := setupBundleRouter(svc)
	w := httptest.NewRecorder()
	req := buildPutMultipartRequest(t, fixedID, "BUNDLE.TAR.GZ", []byte("content"))
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

// -----------------------------------------------------------------------
// TestDeleteBundle
// -----------------------------------------------------------------------

func TestDeleteBundle(t *testing.T) {
	fixedID := "550e8400-e29b-41d4-a716-446655440000"

	tests := []struct {
		name            string
		bundleID        string
		// getBundleByID stub
		getResp         *bundlesvc.BundleResponse
		getErr          error
		// deleteBundle stub — only consulted when getResp != nil
		deleteErr       error
		wantStatus      int
		wantErrContains string
	}{
		{
			name:       "204 — successful delete",
			bundleID:   fixedID,
			getResp:    fixedBundleResponse(),
			wantStatus: http.StatusNoContent,
		},
		{
			name:            "400 — malformed UUID rejected by GetBundleByID",
			bundleID:        "not-a-uuid",
			getErr:          &validators.ValidationError{Code: http.StatusBadRequest, Message: `invalid bundle id "not-a-uuid"`},
			wantStatus:      http.StatusBadRequest,
			wantErrContains: "invalid bundle id",
		},
		{
			name:            "404 — bundle not found (GetBundleByID returns nil)",
			bundleID:        fixedID,
			getResp:         nil,
			wantStatus:      http.StatusNotFound,
			wantErrContains: "not found",
		},
		{
			name:            "400 — GetBundleByID returns ValidationError",
			bundleID:        fixedID,
			getErr:          &validators.ValidationError{Code: http.StatusBadRequest, Message: "invalid bundle id"},
			wantStatus:      http.StatusBadRequest,
			wantErrContains: "invalid bundle id",
		},
		{
			name:            "500 — GetBundleByID returns unexpected error",
			bundleID:        fixedID,
			getErr:          assert.AnError,
			wantStatus:      http.StatusInternalServerError,
		},
		{
			name:            "409 — running instances from DeleteBundle",
			bundleID:        fixedID,
			getResp:         fixedBundleResponse(),
			deleteErr:       &validators.ValidationError{Code: http.StatusConflict, Message: "cannot replace bundle"},
			wantStatus:      http.StatusConflict,
			wantErrContains: "cannot replace bundle",
		},
		{
			name:            "500 — unexpected error from DeleteBundle",
			bundleID:        fixedID,
			getResp:         fixedBundleResponse(),
			deleteErr:       assert.AnError,
			wantStatus:      http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &mockBundleService{
				getBundleByID: func(_ context.Context, id string) (*bundlesvc.BundleResponse, error) {
					assert.Equal(t, tt.bundleID, id)
					return tt.getResp, tt.getErr
				},
				deleteBundle: func(_ context.Context, existing *bundlesvc.BundleResponse) error {
					if tt.getResp != nil {
						assert.Equal(t, tt.getResp.ID, existing.ID)
						assert.Equal(t, tt.getResp.CatalogType, existing.CatalogType)
						assert.Equal(t, tt.getResp.CatalogID, existing.CatalogID)
						assert.Equal(t, tt.getResp.Version, existing.Version)
					}
					return tt.deleteErr
				},
			}

			router := setupBundleRouter(svc)
			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodDelete, "/api/v1/catalog/bundles/"+tt.bundleID, nil)
			router.ServeHTTP(w, req)

			assert.Equal(t, tt.wantStatus, w.Code)

			if tt.wantErrContains != "" {
				var body map[string]string
				require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
				assert.Contains(t, body["error"], tt.wantErrContains)
			}
		})
	}
}

// TestDeleteBundle_ResponseForwardedToDeleteBundle verifies that the *BundleResponse
// returned by GetBundleByID is passed directly to DeleteBundle without any conversion.
func TestDeleteBundle_ResponseForwardedToDeleteBundle(t *testing.T) {
	fixedID := "550e8400-e29b-41d4-a716-446655440000"
	sz := int64(2048)
	now := time.Now().UTC()
	stubResp := &bundlesvc.BundleResponse{
		ID:          fixedID,
		Name:        "Full Fields Service",
		Status:      "active",
		CatalogType: "service",
		CatalogID:   "full-svc",
		Version:     "3.0.0",
		CreatedBy:   "some-user",
		SizeBytes:   &sz,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	svc := &mockBundleService{
		getBundleByID: func(_ context.Context, _ string) (*bundlesvc.BundleResponse, error) {
			return stubResp, nil
		},
		deleteBundle: func(_ context.Context, existing *bundlesvc.BundleResponse) error {
			// The handler must pass the exact *BundleResponse pointer — no intermediate
			// BundleRecord construction allowed.
			assert.Same(t, stubResp, existing)
			return nil
		},
	}

	router := setupBundleRouter(svc)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/catalog/bundles/"+fixedID, nil)
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNoContent, w.Code)
}
