package handlers

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/middleware"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/repository"
	bundlesvc "github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/services/bundle"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/validators"
)

const (
	// maxBundleSizeBytes is the maximum allowed compressed (.tar.gz) upload size (20 MB).
	// The uncompressed on-disk limit is enforced separately by maxExtractedFileSize in
	// archive.go (50 MB). A 20 MB compressed ceiling is sufficient because catalog bundles
	// consist mainly of YAML and Go templates which compress at 5–10×.
	maxBundleSizeBytes = 20 * 1024 * 1024
)

// BundleHandler handles catalog bundle creation, replacement, deletion, and listing.
type BundleHandler struct {
	bundleService bundlesvc.BundleServiceInterface
}

// NewBundleHandler creates a new BundleHandler backed by the given BundleServiceInterface.
func NewBundleHandler(svc bundlesvc.BundleServiceInterface) *BundleHandler {
	return &BundleHandler{bundleService: svc}
}

// CreateBundle godoc
//
//	@Summary		Create a new catalog bundle
//	@Description	Uploads a .tar.gz archive and creates a new bundle. id, type, and version are read from metadata.yaml inside the archive.
//	@Tags			Bundles
//	@Accept			multipart/form-data
//	@Produce		json
//	@Security		BearerAuth
//	@Param			file	formData	file	true	".tar.gz archive containing the catalog item assets"
//	@Success		201		{object}	bundlesvc.BundleResponse
//	@Failure		400		{object}	ErrorResponse	"Missing file, wrong content-type, exceeds size limit, or metadata.yaml malformed"
//	@Failure		401		{object}	ErrorResponse	"Unauthorized"
//	@Failure		403		{object}	ErrorResponse	"Forbidden — admin role required"
//	@Failure		409		{object}	ErrorResponse	"Conflict — bundle with same catalog_id already exists"
//	@Failure		422		{object}	ErrorResponse	"Unprocessable Entity — validation failed"
//	@Router			/catalog/bundles [post]
func (h *BundleHandler) CreateBundle(c *gin.Context) {
	// Enforce MAX_BUNDLE_SIZE before form parsing.
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxBundleSizeBytes)

	file, header, err := c.Request.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "missing or unreadable 'file' field: " + err.Error()})

		return
	}
	defer func() { _ = file.Close() }()

	if !strings.HasSuffix(strings.ToLower(header.Filename), bundlesvc.BundleFileExtension) {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "file must be a " + bundlesvc.BundleFileExtension + " archive"})

		return
	}

	userID := c.GetString(middleware.CtxUserIDKey)

	resp, err := h.bundleService.ProcessBundle(c.Request.Context(), file, userID)
	if err != nil {
		h.mapServiceError(c, err)

		return
	}

	c.Header("Location", fmt.Sprintf("/api/v1/catalog/bundles/%s", resp.ID))
	c.JSON(http.StatusCreated, resp)
}

// ValidateBundle godoc
//
//	@Summary		Validate a bundle without storing it
//	@Description	Validates a .tar.gz archive without writing a DB row or reloading CatalogProvider.
//	@Tags			Bundles
//	@Accept			multipart/form-data
//	@Produce		json
//	@Security		BearerAuth
//	@Param			file	formData	file	true	".tar.gz archive to validate"
//	@Success		200		{object}	bundlesvc.ServiceValidationResult
//	@Failure		400		{object}	ErrorResponse
//	@Failure		401		{object}	ErrorResponse
//	@Failure		403		{object}	ErrorResponse
//	@Failure		422		{object}	ErrorResponse	"Validation failed"
//	@Router			/catalog/bundles/validate [post]
func (h *BundleHandler) ValidateBundle(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxBundleSizeBytes)

	file, header, err := c.Request.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "missing or unreadable 'file' field: " + err.Error()})

		return
	}
	defer func() { _ = file.Close() }()

	if !strings.HasSuffix(strings.ToLower(header.Filename), bundlesvc.BundleFileExtension) {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "file must be a " + bundlesvc.BundleFileExtension + " archive"})

		return
	}

	result, err := h.bundleService.ValidateBundle(c.Request.Context(), file)
	if err != nil {
		h.mapServiceError(c, err)

		return
	}

	c.JSON(http.StatusOK, result)
}

// UpdateBundle godoc
//
//	@Summary		Replace an existing bundle
//	@Description	Replaces the bundle identified by id with a new archive. catalog_id and catalog_type are resolved from the DB record.
//	@Tags			Bundles
//	@Accept			multipart/form-data
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id		path		string	true	"Internal bundle UUID"
//	@Param			file	formData	file	true	"Replacement .tar.gz archive"
//	@Success		200		{object}	bundlesvc.BundleResponse
//	@Failure		400		{object}	ErrorResponse
//	@Failure		401		{object}	ErrorResponse
//	@Failure		403		{object}	ErrorResponse
//	@Failure		404		{object}	ErrorResponse	"Bundle not found"
//	@Failure		422		{object}	ErrorResponse	"catalog_id or catalog_type mismatch, or validation failed"
//	@Router			/catalog/bundles/{id} [put]
func (h *BundleHandler) UpdateBundle(c *gin.Context) {
	// Enforce MAX_BUNDLE_SIZE before any further parsing.
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxBundleSizeBytes)

	bundleID := c.Param("id")

	existing, err := h.bundleService.GetBundleByID(c.Request.Context(), bundleID)
	if err != nil {
		h.mapServiceError(c, err)

		return
	}
	if existing == nil {
		c.JSON(http.StatusNotFound, ErrorResponse{Error: fmt.Sprintf("bundle %q not found", bundleID)})

		return
	}

	file, header, err := c.Request.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "missing or unreadable 'file' field: " + err.Error()})

		return
	}
	defer func() { _ = file.Close() }()

	if !strings.HasSuffix(strings.ToLower(header.Filename), bundlesvc.BundleFileExtension) {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "file must be a " + bundlesvc.BundleFileExtension + " archive"})

		return
	}

	userID := c.GetString(middleware.CtxUserIDKey)

	resp, err := h.bundleService.ReplaceBundle(c.Request.Context(), existing, file, userID)
	if err != nil {
		h.mapServiceError(c, err)

		return
	}

	c.Header("Location", fmt.Sprintf("/api/v1/catalog/bundles/%s", resp.ID))
	c.JSON(http.StatusOK, resp)
}

// DeleteBundle godoc
//
//	@Summary		Delete a bundle
//	@Description	Marks the bundle deleting, removes the on-disk directory, reloads CatalogProvider, and removes the DB row.
//	@Tags			Bundles
//	@Security		BearerAuth
//	@Param			id	path	string	true	"Internal bundle UUID"
//	@Success		204	"No Content"
//	@Failure		401	{object}	ErrorResponse
//	@Failure		403	{object}	ErrorResponse
//	@Failure		404	{object}	ErrorResponse	"Bundle not found"
//	@Router			/catalog/bundles/{id} [delete]
func (h *BundleHandler) DeleteBundle(c *gin.Context) {
	bundleID := c.Param("id")

	existing, err := h.bundleService.GetBundleByID(c.Request.Context(), bundleID)
	if err != nil {
		h.mapServiceError(c, err)

		return
	}
	if existing == nil {
		c.JSON(http.StatusNotFound, ErrorResponse{Error: fmt.Sprintf("bundle %q not found", bundleID)})

		return
	}

	if err := h.bundleService.DeleteBundle(c.Request.Context(), existing); err != nil {
		h.mapServiceError(c, err)

		return
	}

	c.Status(http.StatusNoContent)
}

// ListBundles godoc
//
//	@Summary		List all bundles
//	@Description	Returns a paginated list of all registered bundles ordered by created_at DESC.
//	@Tags			Bundles
//	@Produce		json
//	@Security		BearerAuth
//	@Param			page		query		int	false	"Page number (1-indexed)"				default(1)
//	@Param			page_size	query		int	false	"Number of items per page (max: 100)"	default(20)
//	@Success		200			{object}	bundlesvc.BundleListResponse
//	@Failure		400			{object}	ErrorResponse	"Invalid pagination parameters"
//	@Failure		401			{object}	ErrorResponse
//	@Failure		500			{object}	ErrorResponse
//	@Router			/catalog/bundles [get]
func (h *BundleHandler) ListBundles(c *gin.Context) {
	page, _ := strconv.Atoi(c.Query("page"))
	pageSize, _ := strconv.Atoi(c.Query("page_size"))

	page, pageSize, err := repository.ValidatePaginationParams(page, pageSize)
	if err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})

		return
	}

	resp, err := h.bundleService.ListBundles(c.Request.Context(), bundlesvc.BundleListRequest{
		Page:     page,
		PageSize: pageSize,
	})
	if err != nil {
		h.mapServiceError(c, err)

		return
	}

	c.JSON(http.StatusOK, resp)
}

// GetBundle godoc
//
//	@Summary		Get a bundle by ID
//	@Description	Returns the status and metadata for a specific bundle.
//	@Tags			Bundles
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id	path		string	true	"Internal bundle UUID"
//	@Success		200	{object}	bundlesvc.BundleResponse
//	@Failure		401	{object}	ErrorResponse
//	@Failure		404	{object}	ErrorResponse	"Bundle not found"
//	@Router			/catalog/bundles/{id} [get]
func (h *BundleHandler) GetBundle(c *gin.Context) {
	bundleID := c.Param("id")

	resp, err := h.bundleService.GetBundleByID(c.Request.Context(), bundleID)
	if err != nil {
		h.mapServiceError(c, err)

		return
	}

	if resp == nil {
		c.JSON(http.StatusNotFound, ErrorResponse{Error: fmt.Sprintf("bundle %q not found", bundleID)})

		return
	}

	c.JSON(http.StatusOK, resp)
}

// mapServiceError translates a validators.ValidationError into the appropriate
// HTTP status, and falls back to 500 for all other errors.
func (h *BundleHandler) mapServiceError(c *gin.Context, err error) {
	if valErr, ok := err.(*validators.ValidationError); ok {
		c.JSON(valErr.Code, ErrorResponse{Error: valErr.Message})

		return
	}
	c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
}
