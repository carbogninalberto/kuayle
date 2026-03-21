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

type FavoriteHandler struct {
	favSvc *service.FavoriteService
}

func NewFavoriteHandler(favSvc *service.FavoriteService) *FavoriteHandler {
	return &FavoriteHandler{favSvc: favSvc}
}

func (h *FavoriteHandler) List(c echo.Context) error {
	ws := c.Get("workspace").(*domain.Workspace)
	userID := middleware.GetUserID(c)
	favs, err := h.favSvc.List(c.Request().Context(), ws.ID, userID)
	if err != nil {
		return response.InternalError(c)
	}
	resp := make([]dto.FavoriteResponse, len(favs))
	for i, f := range favs {
		resp[i] = dto.FavoriteResponse{
			ID:         f.ID.String(),
			EntityType: f.EntityType,
			EntityID:   f.EntityID.String(),
			Position:   f.Position,
			CreatedAt:  f.CreatedAt,
		}
	}
	return response.Success(c, http.StatusOK, resp)
}

func (h *FavoriteHandler) Create(c echo.Context) error {
	var req dto.CreateFavoriteRequest
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
	fav, err := h.favSvc.Create(c.Request().Context(), ws.ID, userID, req)
	if err != nil {
		return response.InternalError(c)
	}
	return response.Success(c, http.StatusCreated, dto.FavoriteResponse{
		ID:         fav.ID.String(),
		EntityType: fav.EntityType,
		EntityID:   fav.EntityID.String(),
		Position:   fav.Position,
		CreatedAt:  fav.CreatedAt,
	})
}

func (h *FavoriteHandler) Delete(c echo.Context) error {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return response.Error(c, http.StatusBadRequest, "BAD_REQUEST", "Invalid ID")
	}
	if err := h.favSvc.Delete(c.Request().Context(), id); err != nil {
		return response.InternalError(c)
	}
	return response.Success(c, http.StatusOK, map[string]string{"status": "deleted"})
}
