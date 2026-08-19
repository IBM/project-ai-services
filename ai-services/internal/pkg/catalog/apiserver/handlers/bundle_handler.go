package handlers

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/middleware"
	bundlesvc "github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/services/bundle"
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
	// TODO: enforce MAX_BUNDLE_SIZE via http.MaxBytesReader
	// TODO: read the "file" form field; return 400 if missing or not .tar.gz
	// TODO: extract userID from context via middleware.CtxUserIDKey
	// TODO: call h.bundleService.ProcessBundle(c.Request.Context(), file, userID)
	// TODO: map ValidationError.Code to 409 / 422 / 500
	// TODO: set Location header and return 201 with BundleResponse body
	userID := c.GetString(middleware.CtxUserIDKey)
	_ = userID
	c.JSON(http.StatusNotImplemented, gin.H{"error": "not implemented"})
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
	// TODO: read the "file" form field; return 400 if missing or not .tar.gz
	// TODO: call h.bundleService.ValidateBundle(c.Request.Context(), file)
	// TODO: serialise the returned ValidationResult (ServiceValidationResult or ComponentValidationResult) as 200
	// TODO: map ValidationError.Code to 422
	c.JSON(http.StatusNotImplemented, gin.H{"error": "not implemented"})
}

// UpdateBundle godoc
//
//	@Summary		Replace an existing bundle
//	@Description	Replaces the bundle identified by bundle_id with a new archive. catalog_id and catalog_type are resolved from the DB record.
//	@Tags			Bundles
//	@Accept			multipart/form-data
//	@Produce		json
//	@Security		BearerAuth
//	@Param			bundle_id	path		string	true	"Internal bundle UUID"
//	@Param			file		formData	file	true	"Replacement .tar.gz archive"
//	@Success		200			{object}	bundlesvc.BundleResponse
//	@Failure		400			{object}	ErrorResponse
//	@Failure		401			{object}	ErrorResponse
//	@Failure		403			{object}	ErrorResponse
//	@Failure		404			{object}	ErrorResponse	"Bundle not found"
//	@Failure		422			{object}	ErrorResponse	"catalog_id or catalog_type mismatch, or validation failed"
//	@Router			/catalog/bundles/{bundle_id} [put]
func (h *BundleHandler) UpdateBundle(c *gin.Context) {
	// TODO: read bundle_id path param
	// TODO: call h.bundleService.GetByBundleID — return 404 if nil
	// TODO: read the "file" form field; return 400 if missing or not .tar.gz
	// TODO: extract userID from context via middleware.CtxUserIDKey
	// TODO: call h.bundleService.ReplaceBundle(c.Request.Context(), existing, file, userID)
	// TODO: set Location header and return 200 with updated BundleResponse
	bundleID := c.Param("bundle_id")
	_ = fmt.Sprintf("/api/v1/catalog/bundles/%s", bundleID)
	c.JSON(http.StatusNotImplemented, gin.H{"error": "not implemented"})
}

// DeleteBundle godoc
//
//	@Summary		Delete a bundle
//	@Description	Marks the bundle deleting, removes the on-disk directory, reloads CatalogProvider, and removes the DB row.
//	@Tags			Bundles
//	@Security		BearerAuth
//	@Param			bundle_id	path	string	true	"Internal bundle UUID"
//	@Success		204			"No Content"
//	@Failure		401			{object}	ErrorResponse
//	@Failure		403			{object}	ErrorResponse
//	@Failure		404			{object}	ErrorResponse	"Bundle not found"
//	@Router			/catalog/bundles/{bundle_id} [delete]
func (h *BundleHandler) DeleteBundle(c *gin.Context) {
	// TODO: read bundle_id path param
	// TODO: call h.bundleService.GetByBundleID — return 404 if nil
	// TODO: call h.bundleService.DeleteBundle(c.Request.Context(), existing)
	// TODO: return 204 on success
	c.Status(http.StatusNotImplemented)
}

// ListBundles godoc
//
//	@Summary		List all bundles
//	@Description	Returns all registered bundles ordered by created_at DESC.
//	@Tags			Bundles
//	@Produce		json
//	@Security		BearerAuth
//	@Success		200	{object}	bundlesvc.BundleListResponse
//	@Failure		401	{object}	ErrorResponse
//	@Failure		500	{object}	ErrorResponse
//	@Router			/catalog/bundles [get]
func (h *BundleHandler) ListBundles(c *gin.Context) {
	// TODO: call h.bundleService.ListBundles(c.Request.Context())
	// TODO: return 200 with BundleListResponse
	c.JSON(http.StatusNotImplemented, gin.H{"error": "not implemented"})
}

// GetBundle godoc
//
//	@Summary		Get a bundle by ID
//	@Description	Returns the status and metadata for a specific bundle.
//	@Tags			Bundles
//	@Produce		json
//	@Security		BearerAuth
//	@Param			bundle_id	path		string	true	"Internal bundle UUID"
//	@Success		200			{object}	bundlesvc.BundleResponse
//	@Failure		401			{object}	ErrorResponse
//	@Failure		404			{object}	ErrorResponse	"Bundle not found"
//	@Router			/catalog/bundles/{bundle_id} [get]
func (h *BundleHandler) GetBundle(c *gin.Context) {
	// TODO: read bundle_id path param
	// TODO: call h.bundleService.GetBundleByID(c.Request.Context(), bundleID)
	// TODO: return 404 if nil, else 200 with BundleResponse
	c.JSON(http.StatusNotImplemented, gin.H{"error": "not implemented"})
}
