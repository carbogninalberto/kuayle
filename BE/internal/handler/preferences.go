package handler

import (
	"net/http"

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
	return response.Success(c, http.StatusOK, dto.UserPreferencesResponse{
		FontSize:       prefs.FontSize,
		PointerCursors: prefs.PointerCursors,
		ThemeMode:      prefs.ThemeMode,
		LightTheme:     prefs.LightTheme,
		DarkTheme:      prefs.DarkTheme,
	})
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
		return response.InternalError(c)
	}
	return response.Success(c, http.StatusOK, dto.UserPreferencesResponse{
		FontSize:       prefs.FontSize,
		PointerCursors: prefs.PointerCursors,
		ThemeMode:      prefs.ThemeMode,
		LightTheme:     prefs.LightTheme,
		DarkTheme:      prefs.DarkTheme,
	})
}
