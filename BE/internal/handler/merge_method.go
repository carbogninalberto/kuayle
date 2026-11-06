package handler

import (
	"context"
	"errors"
	"net/http"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/kuayle/kuayle-backend/internal/middleware"
	"github.com/kuayle/kuayle-backend/internal/service"
	"github.com/kuayle/kuayle-backend/pkg/audit"
	"github.com/kuayle/kuayle-backend/pkg/response"
	"github.com/labstack/echo/v5"
)

type mergeMethodService interface {
	MergeMethodSettings(ctx context.Context, ws uuid.UUID) (map[string]string, error)
	SetMergeMethod(ctx context.Context, ws uuid.UUID, repository, method string) error
}

// MergeMethodHandler serves the per-repository merge method of the development settings.
type MergeMethodHandler struct{ service mergeMethodService }

func NewMergeMethodHandler(service mergeMethodService) *MergeMethodHandler {
	return &MergeMethodHandler{service: service}
}

// List returns the configured merge method by repository full name.
func (h *MergeMethodHandler) List(c *echo.Context) error {
	methods, err := h.service.MergeMethodSettings(c.Request().Context(), middleware.GetWorkspace(c).ID)
	if err != nil {
		return response.Error(c, http.StatusInternalServerError, "INTERNAL_ERROR", "Could not read the merge methods")
	}
	return response.Success(c, http.StatusOK, methods)
}

// Update sets or clears the merge method of a repository linked to the workspace.
func (h *MergeMethodHandler) Update(c *echo.Context) error {
	var request dto.MergeMethodRequest
	if !bindAndValidate(c, &request) {
		return nil
	}
	ws := middleware.GetWorkspace(c).ID
	err := h.service.SetMergeMethod(c.Request().Context(), ws, request.Repository, request.MergeMethod)
	switch {
	case errors.Is(err, service.ErrRepositoryNotLinked):
		return response.Error(c, http.StatusNotFound, "NOT_FOUND", "That repository is not linked to this workspace")
	case err != nil:
		return response.Error(c, http.StatusInternalServerError, "INTERNAL_ERROR", "Could not save the merge method")
	}
	audit.Log("merge_method.updated", middleware.GetUserID(c), map[string]interface{}{"workspace_id": ws, "repository": request.Repository, "merge_method": request.MergeMethod})
	return response.Success(c, http.StatusOK, map[string]string{"repository": request.Repository, "merge_method": request.MergeMethod})
}
