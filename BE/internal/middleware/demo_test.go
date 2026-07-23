package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
)

func TestDevMachineDemoGuard(t *testing.T) {
	sysAdminID := uuid.New()
	normalID := uuid.New()

	t.Run("allows when predicate returns true", func(t *testing.T) {
		e := echo.New()
		req := httptest.NewRequest(http.MethodGet, "/dev-machines", nil)
		rec := httptest.NewRecorder()
		ctx := e.NewContext(req, rec)
		ctx.Set(string(UserIDKey), sysAdminID)
		mw := DevMachineDemoGuard(func(id uuid.UUID) bool { return id == sysAdminID })
		h := mw(func(c echo.Context) error { return c.NoContent(http.StatusNoContent) })

		err := h(ctx)

		assert.NoError(t, err)
		assert.Equal(t, http.StatusNoContent, rec.Code)
	})

	t.Run("blocks when predicate returns false", func(t *testing.T) {
		e := echo.New()
		req := httptest.NewRequest(http.MethodGet, "/dev-machines", nil)
		rec := httptest.NewRecorder()
		ctx := e.NewContext(req, rec)
		ctx.Set(string(UserIDKey), normalID)
		mw := DevMachineDemoGuard(func(id uuid.UUID) bool { return id == sysAdminID })
		h := mw(func(c echo.Context) error { return c.NoContent(http.StatusNoContent) })

		err := h(ctx)

		assert.NoError(t, err)
		assert.Equal(t, http.StatusForbidden, rec.Code)
	})

	t.Run("nil predicate fails closed", func(t *testing.T) {
		e := echo.New()
		req := httptest.NewRequest(http.MethodGet, "/dev-machines", nil)
		rec := httptest.NewRecorder()
		ctx := e.NewContext(req, rec)
		ctx.Set(string(UserIDKey), normalID)
		mw := DevMachineDemoGuard(nil)
		h := mw(func(c echo.Context) error { return c.NoContent(http.StatusNoContent) })

		err := h(ctx)

		assert.NoError(t, err)
		assert.Equal(t, http.StatusForbidden, rec.Code)
	})

	t.Run("blocks when no user in context", func(t *testing.T) {
		e := echo.New()
		req := httptest.NewRequest(http.MethodGet, "/dev-machines", nil)
		rec := httptest.NewRecorder()
		ctx := e.NewContext(req, rec)
		mw := DevMachineDemoGuard(func(id uuid.UUID) bool { return true })
		h := mw(func(c echo.Context) error { return c.NoContent(http.StatusNoContent) })

		err := h(ctx)

		assert.NoError(t, err)
		assert.Equal(t, http.StatusForbidden, rec.Code)
	})
}
