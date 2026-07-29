package middleware

import (
	"context"
	"strings"

	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/repository"
	jwtpkg "github.com/kuayle/kuayle-backend/pkg/jwt"
	"github.com/kuayle/kuayle-backend/pkg/response"
	"github.com/labstack/echo/v4"
	log "github.com/sirupsen/logrus"
)

type contextKey string

const UserIDKey contextKey = "user_id"

// Context keys set only when the request authenticates with a personal
// access token; their presence marks the caller as a PAT.
const (
	TokenScopesKey     = "token_scopes"
	TokenWorkspacesKey = "token_workspaces"
)

func Auth(jwtSecret string, patRepo repository.PersonalAccessTokenRepo) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			var tokenString string

			// Try cookie first
			cookie, err := c.Cookie("access_token")
			if err == nil && cookie.Value != "" {
				tokenString = cookie.Value
			}

			// Fallback to Authorization header
			if tokenString == "" {
				auth := c.Request().Header.Get("Authorization")
				if strings.HasPrefix(auth, "Bearer ") {
					tokenString = strings.TrimPrefix(auth, "Bearer ")
				}
			}

			if tokenString == "" {
				return response.Unauthorized(c)
			}

			if strings.HasPrefix(tokenString, domain.TokenPrefix) {
				return authenticatePAT(c, patRepo, tokenString, next)
			}

			claims, err := jwtpkg.ValidateToken(tokenString, jwtSecret)
			if err != nil {
				return response.Unauthorized(c)
			}

			c.Set(string(UserIDKey), claims.UserID)
			return next(c)
		}
	}
}

// authenticatePAT validates a personal access token and populates the same
// user_id context key as the JWT path, plus the token's scopes and workspace
// restriction, so downstream handlers need no PAT-specific logic.
func authenticatePAT(c echo.Context, patRepo repository.PersonalAccessTokenRepo, tokenString string, next echo.HandlerFunc) error {
	token, err := patRepo.GetByHash(c.Request().Context(), domain.HashToken(tokenString))
	if err != nil || token == nil || !token.Active() {
		return response.Unauthorized(c)
	}

	c.Set(string(UserIDKey), token.UserID)
	c.Set(TokenScopesKey, []string(token.Scopes))
	c.Set(TokenWorkspacesKey, []string(token.WorkspaceSlugs))

	// Best-effort usage tracking; never blocks or fails the request.
	go func() {
		if err := patRepo.UpdateLastUsed(context.Background(), token.ID); err != nil {
			log.WithError(err).Warn("failed to update personal access token last_used_at")
		}
	}()

	return next(c)
}
