package middleware

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/repository"
	jwtpkg "github.com/kuayle/kuayle-backend/pkg/jwt"
	"github.com/kuayle/kuayle-backend/pkg/response"
	"github.com/labstack/echo/v5"
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

// Context keys describing an interactive session.
const (
	// SessionAuthTimeKey holds the time.Time the user signed in at.
	SessionAuthTimeKey = "session_auth_time"
	// SessionPasskeyKey holds the uuid.UUID of the passkey that minted the session.
	SessionPasskeyKey = "session_passkey_id"
)

// SessionPasskeys tells whether the passkey a session was minted with still
// belongs to the user. Removing a passkey must end its sessions at once, not
// when their access tokens expire.
type SessionPasskeys interface {
	SessionPasskeyExists(ctx context.Context, passkeyID, userID uuid.UUID) (bool, error)
}

// GetSessionAuthTime returns when the session's user signed in, or the zero
// time for a personal access token or a session older than that record.
func GetSessionAuthTime(c *echo.Context) time.Time {
	at, _ := c.Get(SessionAuthTimeKey).(time.Time)
	return at
}

func Auth(jwtSecret string, patRepo repository.PersonalAccessTokenRepo, hostCookies ...bool) echo.MiddlewareFunc {
	return AuthWithPasskeys(jwtSecret, patRepo, nil, hostCookies...)
}

// AuthWithPasskeys is Auth that also rejects sessions whose passkey was removed.
func AuthWithPasskeys(jwtSecret string, patRepo repository.PersonalAccessTokenRepo, passkeys SessionPasskeys, hostCookies ...bool) echo.MiddlewareFunc {
	cookieName := "access_token"
	if len(hostCookies) > 0 && hostCookies[0] {
		cookieName = "__Host-kuayle-access"
	}
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			c.Response().Header().Set("Cache-Control", "private, no-store")
			var tokenString string

			// A bearer PAT is explicit API authentication and must not be
			// silently replaced by an ambient browser session cookie.
			authorization := c.Request().Header.Get("Authorization")
			if strings.HasPrefix(authorization, "Bearer "+domain.TokenPrefix) {
				return authenticatePAT(c, patRepo, strings.TrimPrefix(authorization, "Bearer "), next)
			}

			// Try cookie first
			cookie, err := c.Cookie(cookieName)
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

			claims, err := jwtpkg.ValidateAccessToken(tokenString, jwtSecret)
			if err != nil {
				return response.Unauthorized(c)
			}

			if claims.PasskeyID != "" {
				passkeyID, err := uuid.Parse(claims.PasskeyID)
				if err != nil || passkeys == nil {
					return response.Unauthorized(c)
				}
				exists, err := passkeys.SessionPasskeyExists(c.Request().Context(), passkeyID, claims.UserID)
				if err != nil {
					log.WithError(err).Error("failed to check the session's passkey")
					return response.InternalError(c)
				}
				if !exists {
					return response.Unauthorized(c)
				}
				c.Set(SessionPasskeyKey, passkeyID)
			}
			if claims.AuthTime > 0 {
				c.Set(SessionAuthTimeKey, time.Unix(claims.AuthTime, 0))
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
func authenticatePAT(c *echo.Context, patRepo repository.PersonalAccessTokenRepo, tokenString string, next echo.HandlerFunc) error {
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
