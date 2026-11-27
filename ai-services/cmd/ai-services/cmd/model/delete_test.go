package model

import (
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"

	catalogClient "github.com/project-ai-services/ai-services/internal/pkg/catalog/client"
)

func TestDeleteError(t *testing.T) {
	httpErr := func(code int) error {
		return &catalogClient.HTTPError{StatusCode: code, Message: "server says no"}
	}

	assert.EqualError(t, deleteError("m", httpErr(http.StatusNotFound)), "model 'm' not found")
	assert.EqualError(t, deleteError("m", httpErr(http.StatusConflict)), "model 'm' cannot be deleted: server says no")
	assert.EqualError(t, deleteError("m", httpErr(http.StatusForbidden)), "you are not allowed to delete model 'm': server says no")
	assert.ErrorContains(t, deleteError("m", httpErr(http.StatusInternalServerError)), "failed to delete model 'm'")
	assert.ErrorContains(t, deleteError("m", errors.New("boom")), "failed to delete model: boom")
}
