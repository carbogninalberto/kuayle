package middleware

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	jwtpkg "github.com/kuayle/kuayle-backend/pkg/jwt"
	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/require"
)

type stubSessionPasskeys struct {
	owner map[uuid.UUID]uuid.UUID
	err   error
}

func (s stubSessionPasskeys) SessionPasskeyExists(_ context.Context, passkeyID, userID uuid.UUID) (bool, error) {
	return s.owner[passkeyID] == userID, s.err
}

// An access token minted by a passkey sign-in is honoured only while that
// passkey still belongs to the user, even though the token has not expired.
func TestAuthRejectsSessionsOfRemovedPasskeys(t *testing.T) {
	const secret = "middleware-passkey-test-secret-0123456789"
	user, passkey, removed := uuid.New(), uuid.New(), uuid.New()
	signedInAt := time.Now().Add(-3 * time.Minute).Truncate(time.Second)
	token := func(passkeyID *uuid.UUID) string {
		value, err := jwtpkg.GenerateSessionAccessToken(user, secret, jwtpkg.Session{AuthenticatedAt: signedInAt, PasskeyID: passkeyID})
		require.NoError(t, err)
		return value
	}
	store := stubSessionPasskeys{owner: map[uuid.UUID]uuid.UUID{passkey: user}}

	call := func(mw echo.MiddlewareFunc, accessToken string) (int, *echo.Context) {
		e := echo.New()
		req := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
		req.AddCookie(&http.Cookie{Name: "access_token", Value: accessToken})
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		require.NoError(t, mw(func(c *echo.Context) error { return c.NoContent(http.StatusNoContent) })(c))
		return rec.Code, c
	}

	status, c := call(AuthWithPasskeys(secret, nil, store), token(&passkey))
	require.Equal(t, http.StatusNoContent, status)
	require.Equal(t, user, GetUserID(c))
	require.Equal(t, passkey, c.Get(SessionPasskeyKey))
	require.True(t, signedInAt.Equal(GetSessionAuthTime(c)))

	status, _ = call(AuthWithPasskeys(secret, nil, store), token(&removed))
	require.Equal(t, http.StatusUnauthorized, status)

	// Without a passkey store a passkey session cannot be vouched for.
	status, _ = call(Auth(secret, nil), token(&passkey))
	require.Equal(t, http.StatusUnauthorized, status)

	// A failing lookup never lets the request through.
	status, _ = call(AuthWithPasskeys(secret, nil, stubSessionPasskeys{owner: store.owner, err: errors.New("database down")}), token(&passkey))
	require.Equal(t, http.StatusInternalServerError, status)

	// Password sessions need no lookup and carry their sign-in time.
	status, c = call(Auth(secret, nil), token(nil))
	require.Equal(t, http.StatusNoContent, status)
	require.Nil(t, c.Get(SessionPasskeyKey))
	require.True(t, signedInAt.Equal(GetSessionAuthTime(c)))

	// Tokens from before the claim existed have no sign-in time.
	legacy, err := jwtpkg.GenerateAccessToken(user, secret)
	require.NoError(t, err)
	status, c = call(Auth(secret, nil), legacy)
	require.Equal(t, http.StatusNoContent, status)
	require.True(t, GetSessionAuthTime(c).IsZero())
}
