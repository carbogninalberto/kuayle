package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/handler"
	mw "github.com/kuayle/kuayle-backend/internal/middleware"
	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/require"
)

func TestSignInTokenExchangeRouteIsRateLimited(t *testing.T) {
	e := echo.New()
	middleware := testMiddleware(noopMiddleware())
	middleware.authRateLimit = mw.RateLimit(0, 1)
	registerRoutes(e, &appHandlers{auth: handler.NewAuthHandler(nil, false, nil, nil)}, middleware)
	for _, status := range []int{http.StatusBadRequest, http.StatusTooManyRequests} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/auth/sign-in-token/acme", strings.NewReader("{"))
		req.Header.Set("Content-Type", "application/json")
		e.ServeHTTP(rec, req)
		require.Equal(t, status, rec.Code)
	}
}

func TestTelegramSignInTokenExchange(t *testing.T) {
	s := newPasskeyServer(t)
	browser := s.browser(t)
	_, userID := browser.signUp()
	other := s.browser(t)
	_, _ = other.signUp()
	ws := uuid.New()
	slug := "sign-in-" + ws.String()
	_, err := s.db.Exec(`INSERT INTO workspaces(id,name,slug,owner_id) VALUES($1,'Test',$2,$3)`, ws, slug, userID)
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = s.db.Exec(`DELETE FROM workspaces WHERE id=$1`, ws) })
	_, err = s.db.Exec(`INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,'owner')`, ws, userID)
	require.NoError(t, err)
	_, err = s.db.Exec(`INSERT INTO telegram_settings(workspace_id,bot_id,token_encrypted,webhook_secret,enabled,allowed_user_ids)
 VALUES($1,12345,'fixture','fixture',true,'{67890}')`, ws)
	require.NoError(t, err)
	_, err = s.db.Exec(`INSERT INTO telegram_links(workspace_id,user_id,telegram_user_id) VALUES($1,$2,67890)`, ws, userID)
	require.NoError(t, err)

	token := strings.Repeat("A", 43)
	hash := sha256.Sum256([]byte(token))
	_, err = s.db.Exec(`INSERT INTO sign_in_tokens(user_id,workspace_id,token_hash,purpose)
 VALUES($1,$2,$3,'telegram')`, userID, ws, hex.EncodeToString(hash[:]))
	require.NoError(t, err)
	path := "/api/auth/sign-in-token/" + slug
	body := map[string]string{"token": token}
	anonymous := s.browser(t)
	require.Equal(t, http.StatusUnauthorized, anonymous.do(http.MethodPost, path+"-other", body).Status)
	require.Equal(t, http.StatusUnauthorized, other.do(http.MethodPost, path, body).Status)
	// The refresh cookie alone also protects the other account.
	base := mustURL(t, s.url)
	other.client.Jar.SetCookies(base, []*http.Cookie{{Name: "access_token", Path: "/", MaxAge: -1}})
	require.Equal(t, http.StatusUnauthorized, other.do(http.MethodPost, path, body).Status)

	// Reproduce the session revocation done by SCIM. The browser still has
	// its old cookies, and the user's live Telegram binding can sign in again.
	_, err = s.db.Exec(`WITH revoked AS (UPDATE users SET session_version=session_version+1 WHERE id=$1 RETURNING id)
 DELETE FROM refresh_tokens WHERE user_id IN (SELECT id FROM revoked)`, userID)
	require.NoError(t, err)
	require.Equal(t, http.StatusUnauthorized, browser.do(http.MethodGet, "/api/auth/me", nil).Status)

	req, err := http.NewRequest(http.MethodPost, s.url+path, strings.NewReader(fmt.Sprintf(`{"token":%q}`, token)))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	res, err := browser.client.Do(req)
	require.NoError(t, err)
	defer res.Body.Close()
	require.Equal(t, http.StatusNoContent, res.StatusCode)
	require.Equal(t, "no-referrer", res.Header.Get("Referrer-Policy"))
	cookies := res.Cookies()
	require.Len(t, cookies, 2)
	require.Equal(t, "access_token", cookies[0].Name)
	require.Equal(t, "/", cookies[0].Path)
	require.Equal(t, "refresh_token", cookies[1].Name)
	require.Equal(t, "/api/auth", cookies[1].Path)
	for _, cookie := range cookies {
		require.NotEmpty(t, cookie.Value)
		require.True(t, cookie.HttpOnly)
		require.Equal(t, http.SameSiteStrictMode, cookie.SameSite)
	}
	me := browser.do(http.MethodGet, "/api/auth/me", nil)
	require.Equal(t, http.StatusOK, me.Status)
	var signedIn struct{ ID uuid.UUID }
	require.NoError(t, json.Unmarshal(me.Data, &signedIn))
	require.Equal(t, userID, signedIn.ID)
	require.Equal(t, http.StatusOK, browser.do(http.MethodPost, "/api/auth/refresh", nil).Status)
	require.Equal(t, http.StatusOK, browser.do(http.MethodGet, "/api/auth/me", nil).Status)
	require.Equal(t, http.StatusUnauthorized, s.browser(t).do(http.MethodPost, path, body).Status)

	_, err = s.db.Exec(`UPDATE sign_in_tokens SET used_at=NULL, created_at=now()-interval '11 minutes', expires_at=now()-interval '1 minute'
 WHERE workspace_id=$1`, ws)
	require.NoError(t, err)
	require.Equal(t, http.StatusUnauthorized, s.browser(t).do(http.MethodPost, path, body).Status)

	_, err = s.db.Exec(`UPDATE sign_in_tokens SET created_at=now(), expires_at=now()+interval '10 minutes'
 WHERE workspace_id=$1`, ws)
	require.NoError(t, err)
	left, right := s.browser(t), s.browser(t)
	results := make(chan int, 2)
	go func() { results <- left.do(http.MethodPost, path, body).Status }()
	go func() { results <- right.do(http.MethodPost, path, body).Status }()
	require.ElementsMatch(t, []int{http.StatusNoContent, http.StatusUnauthorized}, []int{<-results, <-results})

	_, err = s.db.Exec(`DELETE FROM telegram_links WHERE workspace_id=$1 AND user_id=$2`, ws, userID)
	require.NoError(t, err)
	var count int
	require.NoError(t, s.db.Get(&count, `SELECT count(*) FROM sign_in_tokens WHERE workspace_id=$1`, ws))
	require.Zero(t, count)
}
