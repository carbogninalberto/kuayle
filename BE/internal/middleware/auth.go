package middleware

import (
	"context"
	"strings"
	"time"

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
			c.Response().Header().Set("Cache-Control", "private, no-store")
			var tokenString string

			// A bearer PAT is explicit API authentication and must not be
			// silently replaced by an ambient browser session cookie.
			authorization := c.Request().Header.Get("Authorization")
			if strings.HasPrefix(authorization, "Bearer "+domain.TokenPrefix) {
				return authenticatePAT(c, patRepo, strings.TrimPrefix(authorization, "Bearer "), next)
			}

			// Try cookie first
			cookie, err := c.Cookie("access_token")
			if err == nil && cookie.Value != "" {
				if strings.HasPrefix(cookie.Value, domain.TokenPrefix) {
					return response.Unauthorized(c)
				}
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
			c.SetRequest(c.Request().WithContext(domain.WithAccess(c.Request().Context(), domain.Access{UserID: claims.UserID})))
			return next(c)
		}
	}
}

// authenticatePAT validates a personal access token and populates the same
// user_id context key as the JWT path, plus the token's scopes and workspace
// restriction, so downstream handlers need no PAT-specific logic.
func authenticatePAT(c echo.Context, patRepo repository.PersonalAccessTokenRepo, tokenString string, next echo.HandlerFunc) error {
	if patRepo == nil {
		return response.Unauthorized(c)
	}
	token, err := patRepo.GetByHash(c.Request().Context(), domain.HashToken(tokenString))
	if err != nil || token == nil || !token.Active() {
		return response.Unauthorized(c)
	}

	c.Set(string(UserIDKey), token.UserID)
	c.SetRequest(c.Request().WithContext(domain.WithAccess(c.Request().Context(), domain.Access{UserID: token.UserID})))
	c.Set(TokenScopesKey, []string(token.Scopes))
	c.Set(TokenWorkspacesKey, []string(token.WorkspaceSlugs))

	// Best-effort usage tracking; never blocks or fails the request.
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := patRepo.UpdateLastUsed(ctx, token.ID); err != nil {
			log.WithError(err).Warn("failed to update personal access token last_used_at")
		}
	}()

	return next(c)
}
