package handler

import (
	"net/http"

	"github.com/carbon/carbon-backend/internal/domain"
	"github.com/carbon/carbon-backend/internal/dto"
	"github.com/carbon/carbon-backend/internal/middleware"
	"github.com/carbon/carbon-backend/internal/service"
	"github.com/carbon/carbon-backend/pkg/response"
	"github.com/carbon/carbon-backend/pkg/validate"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
)

type ViewHandler struct {
	viewSvc *service.ViewService
}

func NewViewHandler(viewSvc *service.ViewService) *ViewHandler {
	return &ViewHandler{viewSvc: viewSvc}
}

func (h *ViewHandler) List(c echo.Context) error {
	ws := c.Get("workspace").(*domain.Workspace)
	userID := middleware.GetUserID(c)

	views, err := h.viewSvc.List(c.Request().Context(), ws.ID, userID)
	if err != nil {
		return response.InternalError(c)
	}

	resp := make([]dto.ViewResponse, len(views))
	for i, v := range views {
		resp[i] = toViewResponse(v)
	}

	return response.Success(c, http.StatusOK, resp)
}

func (h *ViewHandler) Create(c echo.Context) error {
	var req dto.CreateViewRequest
	if err := c.Bind(&req); err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid request body")
	}
	if err := validate.Struct(&req); err != nil {
		details := make([]dto.ErrorDetail, 0)
		for _, e := range validate.FormatErrors(err) {
			details = append(details, dto.ErrorDetail{Field: e["field"], Message: e["message"]})
		}
		return response.ValidationError(c, details)
	}

	ws := c.Get("workspace").(*domain.Workspace)
	userID := middleware.GetUserID(c)

	view, err := h.viewSvc.Create(c.Request().Context(), ws.ID, userID, req)
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", err.Error())
	}

	return response.Success(c, http.StatusCreated, toViewResponse(*view))
}

func (h *ViewHandler) Get(c echo.Context) error {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid view ID")
	}

	view, err := h.viewSvc.GetByID(c.Request().Context(), id)
	if err != nil || view == nil {
		return response.NotFound(c, "View")
	}

	return response.Success(c, http.StatusOK, toViewResponse(*view))
}

func (h *ViewHandler) Update(c echo.Context) error {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid view ID")
	}

	var req dto.UpdateViewRequest
	if err := c.Bind(&req); err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid request body")
	}

	userID := middleware.GetUserID(c)

	view, err := h.viewSvc.Update(c.Request().Context(), id, userID, req)
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", err.Error())
	}

	return response.Success(c, http.StatusOK, toViewResponse(*view))
}

func (h *ViewHandler) Delete(c echo.Context) error {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid view ID")
	}

	userID := middleware.GetUserID(c)

	if err := h.viewSvc.Delete(c.Request().Context(), id, userID); err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", err.Error())
	}

	return response.Success(c, http.StatusOK, map[string]string{"status": "deleted"})
}

func toViewResponse(v domain.View) dto.ViewResponse {
	return dto.ViewResponse{
		ID:          v.ID.String(),
		WorkspaceID: v.WorkspaceID.String(),
		CreatorID:   v.CreatorID.String(),
		Name:        v.Name,
		Description: v.Description,
		Filters:     v.Filters,
		IsShared:    v.IsShared,
		CreatedAt:   v.CreatedAt,
		UpdatedAt:   v.UpdatedAt,
	}
}
