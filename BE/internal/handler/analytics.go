package handler

import (
	"net/http"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/kuayle/kuayle-backend/pkg/response"
	"github.com/labstack/echo/v4"
)

type AnalyticsHandler struct {
	repo *repository.AnalyticsRepository
}

func NewAnalyticsHandler(repo *repository.AnalyticsRepository) *AnalyticsHandler {
	return &AnalyticsHandler{repo: repo}
}

func (h *AnalyticsHandler) Overview(c echo.Context) error {
	ws := c.Get("workspace").(*domain.Workspace)
	var params dto.AnalyticsScopeParams
	if err := c.Bind(&params); err != nil {
		return response.Error(c, http.StatusBadRequest, "INVALID_PARAMS", "Invalid parameters")
	}
	if params.TeamID != "" {
		if _, err := uuid.Parse(params.TeamID); err != nil {
			return response.Error(c, http.StatusBadRequest, "INVALID_PARAMS", "invalid team_id")
		}
	}

	overview, err := h.repo.Overview(c.Request().Context(), ws.ID.String(), params.TeamID)
	if err != nil {
		return response.InternalError(c)
	}

	return response.Success(c, http.StatusOK, overview)
}

func (h *AnalyticsHandler) IssueDistribution(c echo.Context) error {
	ws := c.Get("workspace").(*domain.Workspace)
	var params dto.AnalyticsScopeParams
	if err := c.Bind(&params); err != nil {
		return response.Error(c, http.StatusBadRequest, "INVALID_PARAMS", "Invalid parameters")
	}
	if params.TeamID != "" {
		if _, err := uuid.Parse(params.TeamID); err != nil {
			return response.Error(c, http.StatusBadRequest, "INVALID_PARAMS", "invalid team_id")
		}
	}

	dist, err := h.repo.Distribution(c.Request().Context(), ws.ID.String(), params.TeamID)
	if err != nil {
		return response.InternalError(c)
	}

	return response.Success(c, http.StatusOK, dist)
}

func (h *AnalyticsHandler) Insights(c echo.Context) error {
	ws := c.Get("workspace").(*domain.Workspace)

	var params dto.AnalyticsInsightsParams
	if err := c.Bind(&params); err != nil {
		return response.Error(c, http.StatusBadRequest, "INVALID_PARAMS", "Invalid parameters")
	}

	if err := repository.ValidateInsightParams(&params); err != nil {
		return response.Error(c, http.StatusBadRequest, "INVALID_PARAMS", err.Error())
	}

	result, err := h.repo.Insights(c.Request().Context(), ws.ID.String(), &params)
	if err != nil {
		return response.InternalError(c)
	}

	return response.Success(c, http.StatusOK, result)
}

func (h *AnalyticsHandler) Burnup(c echo.Context) error {
	ws := c.Get("workspace").(*domain.Workspace)

	var params dto.AnalyticsBurnupParams
	if err := c.Bind(&params); err != nil {
		return response.Error(c, http.StatusBadRequest, "INVALID_PARAMS", "Invalid parameters")
	}

	if err := repository.ValidateBurnupParams(&params); err != nil {
		return response.Error(c, http.StatusBadRequest, "INVALID_PARAMS", err.Error())
	}

	result, err := h.repo.Burnup(c.Request().Context(), ws.ID.String(), &params)
	if err != nil {
		return response.InternalError(c)
	}

	return response.Success(c, http.StatusOK, result)
}
