package handler

import (
	"errors"
	"net/http"

	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/kuayle/kuayle-backend/internal/middleware"
	"github.com/kuayle/kuayle-backend/internal/service"
	"github.com/kuayle/kuayle-backend/pkg/response"
	"github.com/kuayle/kuayle-backend/pkg/validate"
	"github.com/labstack/echo/v4"
)

type PreferencesHandler struct {
	prefsSvc *service.PreferencesService
}

func NewPreferencesHandler(prefsSvc *service.PreferencesService) *PreferencesHandler {
	return &PreferencesHandler{prefsSvc: prefsSvc}
}

func (h *PreferencesHandler) Get(c echo.Context) error {
	userID := middleware.GetUserID(c)
	prefs, err := h.prefsSvc.Get(c.Request().Context(), userID)
	if err != nil {
		return response.InternalError(c)
	}
	return response.Success(c, http.StatusOK, toPreferencesResponse(prefs))
}

func (h *PreferencesHandler) Update(c echo.Context) error {
	var req dto.UpdatePreferencesRequest
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

	userID := middleware.GetUserID(c)
	prefs, err := h.prefsSvc.Update(c.Request().Context(), userID, req)
	if err != nil {
		if errors.Is(err, service.ErrInvalidPreferences) {
			return response.Error(c, http.StatusBadRequest, "INVALID_PREFERENCES", err.Error())
		}
		return response.InternalError(c)
	}
	return response.Success(c, http.StatusOK, toPreferencesResponse(prefs))
}

func toPreferencesResponse(prefs *domain.UserPreferences) dto.UserPreferencesResponse {
	overrides := make(map[string]dto.WorkflowSortOverride, len(prefs.TeamWorkflowSortOverrides))
	for key, override := range prefs.TeamWorkflowSortOverrides {
		overrides[key] = dto.WorkflowSortOverride{
			Mode:              override.Mode,
			WorkflowSortOrder: []string(override.WorkflowSortOrder),
		}
	}

	return dto.UserPreferencesResponse{
		FontSize:                  prefs.FontSize,
		PointerCursors:            prefs.PointerCursors,
		ThemeMode:                 prefs.ThemeMode,
		LightTheme:                prefs.LightTheme,
		DarkTheme:                 prefs.DarkTheme,
		WorkflowSortMode:          prefs.WorkflowSortMode,
		WorkflowSortOrder:         []string(prefs.WorkflowSortOrder),
		TeamWorkflowSortOverrides: overrides,
		RecentDueDates:            []string(prefs.RecentDueDates),
		IssuesGroupBy:             prefs.IssuesGroupBy,
	}
}
