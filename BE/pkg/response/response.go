package response

import (
	"net/http"

	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/dto"
	"github.com/labstack/echo/v5"
)

func Success(c *echo.Context, status int, data interface{}) error {
	return c.JSON(status, data)
}

func Error(c *echo.Context, status int, code string, message string) error {
	return c.JSON(status, dto.ErrorResponse{
		Error: dto.ErrorBody{
			Code:    code,
			Message: message,
		},
	})
}

func ValidationError(c *echo.Context, details []dto.ErrorDetail) error {
	return c.JSON(http.StatusBadRequest, dto.ErrorResponse{
		Error: dto.ErrorBody{
			Code:    "VALIDATION_ERROR",
			Message: "Request validation failed",
			Details: details,
		},
	})
}

func NotFound(c *echo.Context, resource string) error {
	return Error(c, http.StatusNotFound, "NOT_FOUND", resource+" not found")
}

func Forbidden(c *echo.Context) error {
	return Error(c, http.StatusForbidden, "FORBIDDEN", "You do not have permission to perform this action")
}

func Unauthorized(c *echo.Context) error {
	return Error(c, http.StatusUnauthorized, "UNAUTHORIZED", "Authentication required")
}

// SSORequired refuses a password/passkey session in a workspace that requires
// SSO and names the workspace to start the SSO flow from.
func SSORequired(c *echo.Context, workspaceSlug string) error {
	return c.JSON(http.StatusForbidden, dto.ErrorResponse{Error: dto.ErrorBody{
		Code:          "SSO_REQUIRED",
		Message:       "This workspace requires single sign-on",
		WorkspaceSlug: workspaceSlug,
	}})
}

// SSOSessionScoped refuses an SSO-minted session outside the workspace whose
// IdP minted it, or on a durable/account-level action.
func SSOSessionScoped(c *echo.Context) error {
	return Error(c, http.StatusForbidden, "SSO_SESSION_SCOPED", "This session is limited to the workspace it signed in through")
}

// MergedWork refuses to start another agent run on an issue whose work was
// already merged and names that pull request, so the person can confirm and
// send the request again (KUA-193). Only session routes return it.
func MergedWork(c *echo.Context, message string, pullRequest domain.MergedPullRequest) error {
	type body struct {
		dto.ErrorBody
		PullRequest domain.MergedPullRequest `json:"pull_request"`
	}
	return c.JSON(http.StatusConflict, struct {
		Error body `json:"error"`
	}{body{ErrorBody: dto.ErrorBody{Code: "MERGED_WORK", Message: message}, PullRequest: pullRequest}})
}

func InternalError(c *echo.Context) error {
	return Error(c, http.StatusInternalServerError, "INTERNAL_ERROR", "An internal error occurred")
}
