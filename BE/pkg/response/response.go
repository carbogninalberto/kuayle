package response

import (
	"net/http"

	"github.com/carbon/carbon-backend/internal/dto"
	"github.com/labstack/echo/v4"
)

func Success(c echo.Context, status int, data interface{}) error {
	return c.JSON(status, data)
}

func Error(c echo.Context, status int, code string, message string) error {
	return c.JSON(status, dto.ErrorResponse{
		Error: dto.ErrorBody{
			Code:    code,
			Message: message,
		},
	})
}

func ValidationError(c echo.Context, details []dto.ErrorDetail) error {
	return c.JSON(http.StatusBadRequest, dto.ErrorResponse{
		Error: dto.ErrorBody{
			Code:    "VALIDATION_ERROR",
			Message: "Request validation failed",
			Details: details,
		},
	})
}

func NotFound(c echo.Context, resource string) error {
	return Error(c, http.StatusNotFound, "NOT_FOUND", resource+" not found")
}

func Forbidden(c echo.Context) error {
	return Error(c, http.StatusForbidden, "FORBIDDEN", "You do not have permission to perform this action")
}

func Unauthorized(c echo.Context) error {
	return Error(c, http.StatusUnauthorized, "UNAUTHORIZED", "Authentication required")
}

func InternalError(c echo.Context) error {
	return Error(c, http.StatusInternalServerError, "INTERNAL_ERROR", "An internal error occurred")
}
