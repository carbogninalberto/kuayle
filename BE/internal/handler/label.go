package handler

import (
	"errors"
	"net/http"

	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/kuayle/kuayle-backend/internal/service"
	"github.com/kuayle/kuayle-backend/pkg/response"
	"github.com/kuayle/kuayle-backend/pkg/validate"
	"github.com/google/uuid"
	"github.com/labstack/echo/v5"
)

type LabelHandler struct {
	labelSvc *service.LabelService
}

func NewLabelHandler(labelSvc *service.LabelService) *LabelHandler {
	return &LabelHandler{labelSvc: labelSvc}
}

func (h *LabelHandler) List(c *echo.Context) error {
	ws := c.Get("workspace").(*domain.Workspace)
	var projectID *string
	if raw := c.QueryParam("project_id"); raw != "" {
		projectID = &raw
	}
	labels, err := h.labelSvc.ListByWorkspace(c.Request().Context(), ws.ID, projectID)
	if err != nil {
		return labelError(c, err)
	}
	resp := make([]dto.LabelResponse, len(labels))
	for i, l := range labels {
		resp[i] = toLabelResponse(l)
	}
	return response.Success(c, http.StatusOK, resp)
}

func (h *LabelHandler) Create(c *echo.Context) error {
	var req dto.CreateLabelRequest
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
	label, err := h.labelSvc.Create(c.Request().Context(), ws.ID, req)
	if err != nil {
		return labelError(c, err)
	}
	return response.Success(c, http.StatusCreated, toLabelResponse(*label))
}

func labelError(c *echo.Context, err error) error {
	if errors.Is(err, service.ErrLabelProject) {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", err.Error())
	}
	if errors.Is(err, service.ErrLabelNameTaken) {
		return response.Error(c, http.StatusConflict, "CONFLICT", err.Error())
	}
	return response.InternalError(c)
}

func (h *LabelHandler) Update(c *echo.Context) error {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid label ID")
	}
	var req dto.UpdateLabelRequest
	if err := c.Bind(&req); err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid request body")
	}
	ws := c.Get("workspace").(*domain.Workspace)
	label, err := h.labelSvc.Update(c.Request().Context(), ws.ID, id, req)
	if err != nil {
		return labelError(c, err)
	}
	return response.Success(c, http.StatusOK, toLabelResponse(*label))
}

func (h *LabelHandler) Delete(c *echo.Context) error {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid label ID")
	}
	ws := c.Get("workspace").(*domain.Workspace)
	if err := h.labelSvc.Delete(c.Request().Context(), ws.ID, id); err != nil {
		return response.InternalError(c)
	}
	return response.Success(c, http.StatusOK, map[string]string{"status": "deleted"})
}
