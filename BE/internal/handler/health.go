package handler

import (
	"net/http"

	"github.com/jmoiron/sqlx"
	"github.com/labstack/echo/v4"
)

type HealthHandler struct {
	db *sqlx.DB
}

func NewHealthHandler(db *sqlx.DB) *HealthHandler {
	return &HealthHandler{db: db}
}

func (h *HealthHandler) Health(c echo.Context) error {
	return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
}

func (h *HealthHandler) Ready(c echo.Context) error {
	if err := h.db.Ping(); err != nil {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{"status": "not ready"})
	}
	return c.JSON(http.StatusOK, map[string]string{"status": "ready"})
}
