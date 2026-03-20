package handler

import (
	"net/http"

	"github.com/carbon/carbon-backend/internal/domain"
	"github.com/carbon/carbon-backend/internal/dto"
	"github.com/carbon/carbon-backend/internal/service"
	"github.com/carbon/carbon-backend/pkg/response"
	"github.com/carbon/carbon-backend/pkg/validate"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
)

type TeamStatusHandler struct {
	statusSvc *service.TeamStatusService
}

func NewTeamStatusHandler(statusSvc *service.TeamStatusService) *TeamStatusHandler {
	return &TeamStatusHandler{statusSvc: statusSvc}
}

func (h *TeamStatusHandler) List(c echo.Context) error {
	teamIDStr := c.Param("teamId")
	teamID, err := uuid.Parse(teamIDStr)
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid team ID")
	}

	statuses, err := h.statusSvc.List(c.Request().Context(), teamID)
	if err != nil {
		return response.InternalError(c)
	}

	resp := make([]dto.TeamStatusResponse, len(statuses))
	for i, s := range statuses {
		resp[i] = toTeamStatusResponse(s)
	}
	return response.Success(c, http.StatusOK, resp)
}

func (h *TeamStatusHandler) Create(c echo.Context) error {
	var req dto.CreateTeamStatusRequest
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

	teamIDStr := c.Param("teamId")
	teamID, err := uuid.Parse(teamIDStr)
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid team ID")
	}

	status, err := h.statusSvc.Create(c.Request().Context(), teamID, req)
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", err.Error())
	}
	return response.Success(c, http.StatusCreated, toTeamStatusResponse(*status))
}

func (h *TeamStatusHandler) Update(c echo.Context) error {
	var req dto.UpdateTeamStatusRequest
	if err := c.Bind(&req); err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid request body")
	}

	idStr := c.Param("statusId")
	id, err := uuid.Parse(idStr)
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid status ID")
	}

	status, err := h.statusSvc.Update(c.Request().Context(), id, req)
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", err.Error())
	}
	return response.Success(c, http.StatusOK, toTeamStatusResponse(*status))
}

func (h *TeamStatusHandler) Delete(c echo.Context) error {
	idStr := c.Param("statusId")
	id, err := uuid.Parse(idStr)
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid status ID")
	}

	if err := h.statusSvc.Delete(c.Request().Context(), id); err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", err.Error())
	}
	return response.Success(c, http.StatusOK, map[string]string{"status": "deleted"})
}

func toTeamStatusResponse(s domain.TeamStatus) dto.TeamStatusResponse {
	return dto.TeamStatusResponse{
		ID:        s.ID.String(),
		TeamID:    s.TeamID.String(),
		Name:      s.Name,
		Slug:      s.Slug,
		Category:  string(s.Category),
		Color:     s.Color,
		Position:  s.Position,
		IsDefault: s.IsDefault,
		CreatedAt: s.CreatedAt,
		UpdatedAt: s.UpdatedAt,
	}
}
