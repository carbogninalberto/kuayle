package handler

import (
	"net/http"

	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/kuayle/kuayle-backend/internal/middleware"
	"github.com/kuayle/kuayle-backend/internal/service"
	"github.com/kuayle/kuayle-backend/pkg/response"
	"github.com/kuayle/kuayle-backend/pkg/validate"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
)

type TeamHandler struct {
	teamSvc *service.TeamService
}

func NewTeamHandler(teamSvc *service.TeamService) *TeamHandler {
	return &TeamHandler{teamSvc: teamSvc}
}

func (h *TeamHandler) List(c echo.Context) error {
	ws := c.Get("workspace").(*domain.Workspace)
	teams, err := h.teamSvc.ListByWorkspace(c.Request().Context(), ws.ID)
	if err != nil {
		return response.InternalError(c)
	}
	resp := make([]dto.TeamResponse, len(teams))
	for i, t := range teams {
		resp[i] = toTeamResponse(t)
	}
	return response.Success(c, http.StatusOK, resp)
}

func (h *TeamHandler) Create(c echo.Context) error {
	var req dto.CreateTeamRequest
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
	team, err := h.teamSvc.Create(c.Request().Context(), ws.ID, userID, req)
	if err != nil {
		return response.Error(c, http.StatusConflict, "CONFLICT", err.Error())
	}
	return response.Success(c, http.StatusCreated, toTeamResponse(*team))
}

func (h *TeamHandler) Get(c echo.Context) error {
	teamID, err := uuid.Parse(c.Param("teamId"))
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid team ID")
	}
	team, err := h.teamSvc.GetByID(c.Request().Context(), teamID)
	if err != nil || team == nil {
		return response.NotFound(c, "Team")
	}
	return response.Success(c, http.StatusOK, toTeamResponse(*team))
}

func (h *TeamHandler) Update(c echo.Context) error {
	teamID, err := uuid.Parse(c.Param("teamId"))
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid team ID")
	}
	var req dto.UpdateTeamRequest
	if err := c.Bind(&req); err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid request body")
	}
	team, err := h.teamSvc.Update(c.Request().Context(), teamID, req)
	if err != nil {
		return response.InternalError(c)
	}
	return response.Success(c, http.StatusOK, toTeamResponse(*team))
}

func toTeamResponse(t domain.Team) dto.TeamResponse {
	return dto.TeamResponse{
		ID:            t.ID.String(),
		Name:          t.Name,
		Key:           t.Key,
		Description:   t.Description,
		Color:         t.Color,
		Icon:          t.Icon,
		TriageEnabled: t.TriageEnabled,
		CreatedAt:     t.CreatedAt,
		UpdatedAt:     t.UpdatedAt,
	}
}
