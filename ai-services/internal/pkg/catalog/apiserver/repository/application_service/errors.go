package applicationservice

import (
	"context"
	"fmt"
	"net/http"

	"github.com/google/uuid"
	dbmodels "github.com/project-ai-services/ai-services/internal/pkg/catalog/db/models"
	dbrepo "github.com/project-ai-services/ai-services/internal/pkg/catalog/db/repository"
)

// Error message constants for application operations.
const (
	// ErrMsgApplicationNotFound is returned when an application does not exist.
	ErrMsgApplicationNotFound = "application does not exist"

	// ErrMsgUserNotOwner is returned when a user does not own the application.
	ErrMsgUserNotOwner = "user does not own this application"

	// ErrMsgApplicationAlreadyDeleting is returned when an application is already being deleted.
	ErrMsgApplicationAlreadyDeleting = "application is already being deleted"

	// ErrMsgApplicationNameExists is returned when an application with the given name already exists.
	ErrMsgApplicationNameExists = "application with name '%s' already exists"
)

// LoadAppWithOwnership fetches an application by ID and enforces that userID matches the
// stored created_by value. Returns a *ValidationError (404 when not found, 403 when the
// caller is not the owner). Pass an empty userID to skip the ownership check.
func LoadAppWithOwnership(ctx context.Context, repo dbrepo.ApplicationRepository, applicationID uuid.UUID, userID string) (*dbmodels.Application, error) {
	app, err := repo.GetByID(ctx, applicationID)
	if err != nil {
		return nil, fmt.Errorf("failed to load application: %w", err)
	}

	if app == nil {
		return nil, &ValidationError{
			Code:    http.StatusNotFound,
			Message: ErrMsgApplicationNotFound,
		}
	}

	if userID != "" && app.CreatedBy != userID {
		return nil, &ValidationError{
			Code:    http.StatusForbidden,
			Message: ErrMsgUserNotOwner,
		}
	}

	return app, nil
}
