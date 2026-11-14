package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	neturl "net/url"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/handler"
	mw "github.com/kuayle/kuayle-backend/internal/middleware"
	"github.com/kuayle/kuayle-backend/internal/passkeytest"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/kuayle/kuayle-backend/internal/service"
	"github.com/labstack/echo/v5"
	log "github.com/sirupsen/logrus"
	logtest "github.com/sirupsen/logrus/hooks/test"
	"github.com/stretchr/testify/require"
)

const (
	passkeyHTTPOrigin   = "http://localhost:5173"
	passkeyHTTPPassword = "Password123!!"
)

// passkeyServer is the real route table, authentication middleware, handlers,
// services and PostgreSQL repositories behind an HTTP server.
type passkeyServer struct {
	db   *sqlx.DB
	url  string
	auth *service.AuthService
}

func newPasskeyServer(t *testing.T, opts ...service.AuthServiceOption) *passkeyServer {
	t.Helper()
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL is not set")
	}
	db, err := sqlx.Connect("pgx", url)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	const secret = "passkey-http-test-secret-0123456789abcdef"
	users := repository.NewUserRepository(db)
	opts = append(opts, service.WithPasswordReset(repository.NewPasswordResetRepository(db), nil, passkeyHTTPOrigin, false))
	authSvc := service.NewAuthService(users, repository.NewRefreshTokenRepository(db), secret, opts...)
	rp, err := service.NewPasskeyRelyingParty("", "", "Kuayle Test", passkeyHTTPOrigin)
	require.NoError(t, err)
	passkeys, err := service.NewPasskeyService(authSvc, users, repository.NewPasskeyRepository(db), rp)
	require.NoError(t, err)
	authH := handler.NewAuthHandler(authSvc, false, mw.NewLoginThrottle(5, time.Minute), nil)

	e := echo.New()
	middleware := testMiddleware(mw.AuthWithPasskeys(secret, repository.NewPersonalAccessTokenRepository(db), passkeys))
	registerRoutes(e, &appHandlers{
		auth:    authH,
		passkey: handler.NewPasskeyHandler(passkeys, authH),
		config:  handler.NewConfigHandler(true).WithPasskeys(passkeys.Enabled()),
	}, middleware)
	server := httptest.NewServer(e)
	t.Cleanup(server.Close)
	return &passkeyServer{db: db, url: server.URL, auth: authSvc}
}

// passkeyBrowser is one browser profile: its own cookies.
type passkeyBrowser struct {
	t      *testing.T
	s      *passkeyServer
	client *http.Client
	origin string
}

func (s *passkeyServer) browser(t *testing.T) *passkeyBrowser {
	jar, err := cookiejar.New(nil)
	require.NoError(t, err)
	return &passkeyBrowser{t: t, s: s, client: &http.Client{Jar: jar}, origin: passkeyHTTPOrigin}
}

type passkeyReply struct {
	Status int
	Data   json.RawMessage
	Code   string
}

func (b *passkeyBrowser) do(method, path string, body any) passkeyReply {
	b.t.Helper()
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		require.NoError(b.t, err)
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequest(method, b.s.url+path, reader)
	require.NoError(b.t, err)
	req.Header.Set("Content-Type", "application/json")
	if b.origin != "" && method != http.MethodGet {
		req.Header.Set("Origin", b.origin)
	}
	resp, err := b.client.Do(req)
	require.NoError(b.t, err)
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	require.NoError(b.t, err)
	var failure struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	_ = json.Unmarshal(raw, &failure)
	return passkeyReply{Status: resp.StatusCode, Data: raw, Code: failure.Error.Code}
}

// signUp creates an account through the public endpoint and leaves the browser signed in.
func (b *passkeyBrowser) signUp() (email string, userID uuid.UUID) {
	b.t.Helper()
	email = "passkey-" + uuid.NewString() + "@example.test"
	reply := b.do(http.MethodPost, "/api/auth/register", map[string]string{"email": email, "password": passkeyHTTPPassword, "name": "Passkey Test"})
	require.Equal(b.t, http.StatusCreated, reply.Status, "%s", reply.Code)
	var user struct {
		ID uuid.UUID `json:"id"`
	}
	require.NoError(b.t, json.Unmarshal(reply.Data, &user))
	b.t.Cleanup(func() { _, _ = b.s.db.Exec(`DELETE FROM users WHERE id = $1`, user.ID) })
	return email, user.ID
}

func (b *passkeyBrowser) signIn(email string) {
	b.t.Helper()
	reply := b.do(http.MethodPost, "/api/auth/login", map[string]string{"email": email, "password": passkeyHTTPPassword})
	require.Equal(b.t, http.StatusOK, reply.Status, "%s", reply.Code)
}

// addPasskey runs the registration ceremony and returns the stored passkey's ID.
func (b *passkeyBrowser) addPasskey(authenticator *passkeytest.Authenticator, name string) uuid.UUID {
	b.t.Helper()
	begin := b.do(http.MethodPost, "/api/auth/passkey/register/begin", map[string]string{})
	require.Equal(b.t, http.StatusOK, begin.Status, "%s", begin.Code)
	credential, err := authenticator.Create(begin.Data)
	require.NoError(b.t, err)
	done := b.do(http.MethodPost, "/api/auth/passkey/register/complete", map[string]any{"name": name, "credential": json.RawMessage(credential)})
	require.Equal(b.t, http.StatusCreated, done.Status, "%s", done.Code)
	var passkey struct {
		ID uuid.UUID `json:"id"`
	}
	require.NoError(b.t, json.Unmarshal(done.Data, &passkey))
	return passkey.ID
}

// assertion starts a sign-in and returns the authenticator's answer, not yet sent.
func (b *passkeyBrowser) assertion(authenticator *passkeytest.Authenticator) map[string]any {
	b.t.Helper()
	begin := b.do(http.MethodPost, "/api/auth/passkey/login/begin", nil)
	require.Equal(b.t, http.StatusOK, begin.Status, "%s", begin.Code)
	credential, err := authenticator.Get(begin.Data)
	require.NoError(b.t, err)
	return map[string]any{"credential": json.RawMessage(credential)}
}

func (b *passkeyBrowser) passkeySignIn(authenticator *passkeytest.Authenticator) passkeyReply {
	b.t.Helper()
	return b.do(http.MethodPost, "/api/auth/passkey/login/complete", b.assertion(authenticator))
}

func (b *passkeyBrowser) signedIn() bool {
	return b.do(http.MethodGet, "/api/auth/me", nil).Status == http.StatusOK
}

type listedPasskey struct {
	ID         uuid.UUID  `json:"id"`
	Name       string     `json:"name"`
	Current    bool       `json:"current"`
	LastUsedAt *time.Time `json:"last_used_at"`
}

func (b *passkeyBrowser) passkeys() []listedPasskey {
	b.t.Helper()
	reply := b.do(http.MethodGet, "/api/auth/passkey/credentials", nil)
	require.Equal(b.t, http.StatusOK, reply.Status, "%s", reply.Code)
	var out []listedPasskey
	require.NoError(b.t, json.Unmarshal(reply.Data, &out))
	return out
}

func TestPasskeyHTTP(t *testing.T) {
	s := newPasskeyServer(t)

	t.Run("the instance announces passkeys", func(t *testing.T) {
		reply := s.browser(t).do(http.MethodGet, "/api/config", nil)
		require.Equal(t, http.StatusOK, reply.Status)
		require.JSONEq(t, `{"registration_enabled":true,"passkeys_enabled":true,"sso_enabled_instance":false}`, string(reply.Data))
	})

	t.Run("register a passkey, sign out, sign in with it alone", func(t *testing.T) {
		b := s.browser(t)
		email, userID := b.signUp()
		authenticator := passkeytest.New(passkeyHTTPOrigin)
		passkeyID := b.addPasskey(authenticator, "Laptop")
		require.Equal(t, http.StatusOK, b.do(http.MethodPost, "/api/auth/logout", nil).Status)
		require.False(t, b.signedIn())

		reply := b.passkeySignIn(authenticator)
		require.Equal(t, http.StatusOK, reply.Status, "%s", reply.Code)
		var user struct {
			ID    uuid.UUID `json:"id"`
			Email string    `json:"email"`
		}
		require.NoError(t, json.Unmarshal(reply.Data, &user))
		require.Equal(t, userID, user.ID)
		require.Equal(t, email, user.Email)
		require.True(t, b.signedIn())

		listed := b.passkeys()
		require.Len(t, listed, 1)
		require.Equal(t, listedPasskey{ID: passkeyID, Name: "Laptop", Current: true, LastUsedAt: listed[0].LastUsedAt}, listed[0])
		require.NotNil(t, listed[0].LastUsedAt)

		// The session is an ordinary refresh-token session tied to the passkey,
		// and stays tied to it through rotation.
		var tied int
		require.NoError(t, s.db.Get(&tied, `SELECT COUNT(*) FROM refresh_tokens WHERE user_id = $1 AND passkey_credential_id = $2 AND authenticated_at IS NOT NULL`, userID, passkeyID))
		require.Equal(t, 1, tied)
		require.Equal(t, http.StatusOK, b.do(http.MethodPost, "/api/auth/refresh", nil).Status)
		require.NoError(t, s.db.Get(&tied, `SELECT COUNT(*) FROM refresh_tokens WHERE user_id = $1 AND passkey_credential_id = $2`, userID, passkeyID))
		require.Equal(t, 1, tied)
		require.True(t, b.passkeys()[0].Current)

		renamed := b.do(http.MethodPatch, "/api/auth/passkey/credentials/"+passkeyID.String(), map[string]string{"name": "Work laptop"})
		require.Equal(t, http.StatusOK, renamed.Status, "%s", renamed.Code)
		require.Equal(t, "Work laptop", b.passkeys()[0].Name)
	})

	t.Run("removing a passkey ends the sessions it minted and no others", func(t *testing.T) {
		password := s.browser(t)
		email, userID := password.signUp()
		first, second := passkeytest.New(passkeyHTTPOrigin), passkeytest.New(passkeyHTTPOrigin)
		firstID := password.addPasskey(first, "First")
		password.addPasskey(second, "Second")

		firstSession, secondSession := s.browser(t), s.browser(t)
		require.Equal(t, http.StatusOK, firstSession.passkeySignIn(first).Status)
		require.Equal(t, http.StatusOK, secondSession.passkeySignIn(second).Status)

		// A different account cannot remove it.
		outsider := s.browser(t)
		outsider.signUp()
		require.Equal(t, http.StatusNotFound, outsider.do(http.MethodDelete, "/api/auth/passkey/credentials/"+firstID.String(), nil).Status)
		require.True(t, firstSession.signedIn())

		// A sign-in with the passkey is under way elsewhere when it is removed.
		pending := s.browser(t)
		pendingAssertion := pending.assertion(first)

		require.Equal(t, http.StatusOK, secondSession.do(http.MethodDelete, "/api/auth/passkey/credentials/"+firstID.String(), nil).Status)

		// Its access token is still within its lifetime, yet it is refused at once.
		require.False(t, firstSession.signedIn())
		require.Equal(t, http.StatusUnauthorized, firstSession.do(http.MethodPost, "/api/auth/refresh", nil).Status)
		require.True(t, secondSession.signedIn())
		require.Equal(t, http.StatusOK, secondSession.do(http.MethodPost, "/api/auth/refresh", nil).Status)
		require.True(t, password.signedIn())
		require.Equal(t, http.StatusOK, password.do(http.MethodPost, "/api/auth/refresh", nil).Status)

		reply := pending.do(http.MethodPost, "/api/auth/passkey/login/complete", pendingAssertion)
		require.Equal(t, http.StatusUnauthorized, reply.Status)
		require.Equal(t, "PASSKEY_UNKNOWN", reply.Code)
		require.False(t, pending.signedIn())

		listed := password.passkeys()
		require.Len(t, listed, 1)
		require.Equal(t, "Second", listed[0].Name)
		require.False(t, listed[0].Current, "the password session was not minted by a passkey")
		_ = email
		_ = userID
	})

	t.Run("a challenge is single-use and bound to the browser that asked for it", func(t *testing.T) {
		b := s.browser(t)
		_, userID := b.signUp()
		authenticator := passkeytest.New(passkeyHTTPOrigin)

		begin := b.do(http.MethodPost, "/api/auth/passkey/register/begin", map[string]string{})
		require.Equal(t, http.StatusOK, begin.Status)
		credential, err := authenticator.Create(begin.Data)
		require.NoError(t, err)
		body := map[string]any{"name": "Key", "credential": json.RawMessage(credential)}

		// Another signed-in browser of the same user has no handle for it.
		other := s.browser(t)
		other.client.Jar.SetCookies(mustURL(t, s.url), b.client.Jar.Cookies(mustURL(t, s.url))) // session cookies only: the challenge cookie is path-scoped
		require.True(t, other.signedIn())
		reply := other.do(http.MethodPost, "/api/auth/passkey/register/complete", body)
		require.Equal(t, http.StatusBadRequest, reply.Status)
		require.Equal(t, "PASSKEY_CHALLENGE_EXPIRED", reply.Code)

		require.Equal(t, http.StatusCreated, b.do(http.MethodPost, "/api/auth/passkey/register/complete", body).Status)
		reply = b.do(http.MethodPost, "/api/auth/passkey/register/complete", body)
		require.Equal(t, http.StatusBadRequest, reply.Status, "a registration response cannot be replayed")
		require.Equal(t, "PASSKEY_CHALLENGE_EXPIRED", reply.Code)
		var stored, pending int
		require.NoError(t, s.db.Get(&stored, `SELECT COUNT(*) FROM passkey_credentials WHERE user_id = $1`, userID))
		require.Equal(t, 1, stored)
		require.NoError(t, s.db.Get(&pending, `SELECT COUNT(*) FROM passkey_challenges WHERE user_id = $1`, userID))
		require.Zero(t, pending, "nothing of the ceremony is left behind")

		// The same assertion sent many times at once opens exactly one session.
		login := s.browser(t)
		assertion := login.assertion(authenticator)
		statuses := make([]int, 8)
		var wg sync.WaitGroup
		for i := range statuses {
			wg.Add(1)
			go func() {
				defer wg.Done()
				statuses[i] = login.do(http.MethodPost, "/api/auth/passkey/login/complete", assertion).Status
			}()
		}
		wg.Wait()
		succeeded := 0
		for _, status := range statuses {
			if status == http.StatusOK {
				succeeded++
			} else {
				require.Equal(t, http.StatusBadRequest, status)
			}
		}
		require.Equal(t, 1, succeeded)
		var sessions int
		require.NoError(t, s.db.Get(&sessions, `SELECT COUNT(*) FROM refresh_tokens WHERE user_id = $1 AND passkey_credential_id IS NOT NULL`, userID))
		require.Equal(t, 1, sessions)

		// An assertion captured in transit is useless from anywhere else.
		victim := s.browser(t)
		captured := victim.assertion(authenticator)
		thief := s.browser(t)
		reply = thief.do(http.MethodPost, "/api/auth/passkey/login/complete", captured)
		require.Equal(t, http.StatusBadRequest, reply.Status)
		require.False(t, thief.signedIn())

		// An expired challenge is refused even with the right browser and passkey.
		late := s.browser(t)
		lateAssertion := late.assertion(authenticator)
		_, err = s.db.Exec(`UPDATE passkey_challenges SET expires_at = NOW() - INTERVAL '1 second' WHERE kind = 'login' AND expires_at > NOW()`)
		require.NoError(t, err)
		reply = late.do(http.MethodPost, "/api/auth/passkey/login/complete", lateAssertion)
		require.Equal(t, http.StatusBadRequest, reply.Status)
		require.Equal(t, "PASSKEY_CHALLENGE_EXPIRED", reply.Code)
	})

	t.Run("adding a passkey from an old session needs the password again", func(t *testing.T) {
		b := s.browser(t)
		_, userID := b.signUp()
		_, err := s.db.Exec(`UPDATE refresh_tokens SET authenticated_at = NOW() - INTERVAL '1 hour' WHERE user_id = $1`, userID)
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, b.do(http.MethodPost, "/api/auth/refresh", nil).Status)

		reply := b.do(http.MethodPost, "/api/auth/passkey/register/begin", map[string]string{})
		require.Equal(t, http.StatusForbidden, reply.Status)
		require.Equal(t, "REAUTH_REQUIRED", reply.Code)
		reply = b.do(http.MethodPost, "/api/auth/passkey/register/begin", map[string]string{"password": "Not-the-password-1"})
		require.Equal(t, http.StatusForbidden, reply.Status)
		require.Equal(t, "INVALID_PASSWORD", reply.Code)
		require.True(t, b.signedIn(), "a wrong password does not end the session")
		reply = b.do(http.MethodPost, "/api/auth/passkey/register/begin", map[string]string{"password": passkeyHTTPPassword})
		require.Equal(t, http.StatusOK, reply.Status, "%s", reply.Code)

		// Guessing the password through this endpoint locks like sign-in does.
		for i := 0; i < 5; i++ {
			b.do(http.MethodPost, "/api/auth/passkey/register/begin", map[string]string{"password": "Not-the-password-1"})
		}
		reply = b.do(http.MethodPost, "/api/auth/passkey/register/begin", map[string]string{"password": passkeyHTTPPassword})
		require.Equal(t, http.StatusTooManyRequests, reply.Status)

		// Anonymous callers and sessions from before sign-in time was recorded get nothing.
		require.Equal(t, http.StatusUnauthorized, s.browser(t).do(http.MethodPost, "/api/auth/passkey/register/begin", map[string]string{}).Status)
		require.Equal(t, http.StatusUnauthorized, s.browser(t).do(http.MethodGet, "/api/auth/passkey/credentials", nil).Status)
	})

	// A fresh session's password is not compared, so sending one must not wipe
	// the count of wrong guesses made from an old session of the same account.
	t.Run("a fresh session cannot clear the password lock", func(t *testing.T) {
		fresh := s.browser(t)
		email, userID := fresh.signUp()
		old := s.browser(t)
		old.signIn(email)
		_, err := s.db.Exec(`UPDATE refresh_tokens SET authenticated_at = NOW() - INTERVAL '1 hour' WHERE user_id = $1`, userID)
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, old.do(http.MethodPost, "/api/auth/refresh", nil).Status)

		wrong := map[string]string{"password": "Not-the-password-1"}
		for i := 0; i < 4; i++ {
			require.Equal(t, "INVALID_PASSWORD", old.do(http.MethodPost, "/api/auth/passkey/register/begin", wrong).Code)
		}
		reply := fresh.do(http.MethodPost, "/api/auth/passkey/register/begin", wrong)
		require.Equal(t, http.StatusOK, reply.Status, "a fresh session needs no password, whatever it sends: %s", reply.Code)
		require.Equal(t, "INVALID_PASSWORD", old.do(http.MethodPost, "/api/auth/passkey/register/begin", wrong).Code)
		reply = old.do(http.MethodPost, "/api/auth/passkey/register/begin", map[string]string{"password": passkeyHTTPPassword})
		require.Equal(t, http.StatusTooManyRequests, reply.Status, "the fifth wrong guess locks; the fresh session did not reset the count")
		require.Equal(t, http.StatusOK, fresh.do(http.MethodPost, "/api/auth/passkey/register/begin", wrong).Status, "the lock does not apply where no password is compared")
	})

	t.Run("a cloned authenticator and an unknown passkey are refused", func(t *testing.T) {
		b := s.browser(t)
		_, userID := b.signUp()
		authenticator := passkeytest.New(passkeyHTTPOrigin)
		b.addPasskey(authenticator, "Security key")
		clone := authenticator.Clone()
		require.Equal(t, http.StatusOK, s.browser(t).passkeySignIn(authenticator).Status)

		logs := logtest.NewGlobal()
		t.Cleanup(logs.Reset)
		logged := func(event, reason string) *log.Entry {
			for _, entry := range logs.AllEntries() {
				if entry.Data["event"] == event && (reason == "" || entry.Data["reason"] == reason) {
					return entry
				}
			}
			return nil
		}

		attacker := s.browser(t)
		reply := attacker.passkeySignIn(clone) // counter 1 again
		require.Equal(t, http.StatusUnauthorized, reply.Status)
		require.Equal(t, "PASSKEY_INVALID", reply.Code)
		require.False(t, attacker.signedIn())
		entry := logged("auth.passkey_clone_suspected", "")
		require.NotNil(t, entry, "a counter regression is written to the audit log")
		require.Equal(t, userID, entry.Data["user_id"])
		require.Equal(t, "stored counter 1, received 1", entry.Data["detail"])
		var count int
		require.NoError(t, s.db.Get(&count, `SELECT sign_count FROM passkey_credentials WHERE user_id = $1`, userID))
		require.Equal(t, 1, count)

		stranger := passkeytest.New(passkeyHTTPOrigin)
		_, err := stranger.Create([]byte(`{"publicKey":{"challenge":"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA","rp":{"id":"localhost"},"user":{"id":"AAAA"}}}`))
		require.NoError(t, err)
		reply = attacker.passkeySignIn(stranger)
		require.Equal(t, http.StatusUnauthorized, reply.Status)
		require.Equal(t, "PASSKEY_UNKNOWN", reply.Code)
		require.NotNil(t, logged("auth.passkey_login_failed", "unknown_credential"), "and so is an unknown credential")
	})

	t.Run("a passkey works without a user handle and only from the configured origin", func(t *testing.T) {
		b := s.browser(t)
		b.signUp()
		authenticator := passkeytest.New(passkeyHTTPOrigin)
		b.addPasskey(authenticator, "Key")

		authenticator.OmitUserHandle = true
		require.Equal(t, http.StatusOK, s.browser(t).passkeySignIn(authenticator).Status)

		// Reached through an address the relying party does not cover, e.g. a
		// proxy's internal name: refused up front with a reason.
		elsewhere := s.browser(t)
		elsewhere.origin = "http://kuayle.internal:8080"
		reply := elsewhere.do(http.MethodPost, "/api/auth/passkey/login/begin", nil)
		require.Equal(t, http.StatusBadRequest, reply.Status)
		require.Equal(t, "PASSKEY_ORIGIN_MISMATCH", reply.Code)

		// Forwarded headers never change which origin is accepted.
		spoof := s.browser(t)
		begin := spoof.do(http.MethodPost, "/api/auth/passkey/login/begin", nil)
		require.Equal(t, http.StatusOK, begin.Status)
		phished := authenticator.Clone()
		phished.Origin = "https://evil.example.test"
		credential, err := phished.Get(begin.Data)
		require.NoError(t, err)
		req, err := http.NewRequest(http.MethodPost, s.url+"/api/auth/passkey/login/complete", bytes.NewReader(mustMarshal(t, map[string]any{"credential": json.RawMessage(credential)})))
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Forwarded-Host", "evil.example.test")
		req.Header.Set("X-Forwarded-Proto", "https")
		req.Host = "evil.example.test"
		resp, err := spoof.client.Do(req)
		require.NoError(t, err)
		_ = resp.Body.Close()
		require.Equal(t, http.StatusBadRequest, resp.StatusCode)
		require.False(t, spoof.signedIn())
	})

	t.Run("an account without a usable password keeps its last passkey", func(t *testing.T) {
		b := s.browser(t)
		_, userID := b.signUp()
		first, second := passkeytest.New(passkeyHTTPOrigin), passkeytest.New(passkeyHTTPOrigin)
		firstID, secondID := b.addPasskey(first, "First"), b.addPasskey(second, "Second")
		_, err := s.db.Exec(`UPDATE users SET password_hash = '!' WHERE id = $1`, userID)
		require.NoError(t, err)

		require.Equal(t, http.StatusOK, b.do(http.MethodDelete, "/api/auth/passkey/credentials/"+firstID.String(), nil).Status)
		reply := b.do(http.MethodDelete, "/api/auth/passkey/credentials/"+secondID.String(), nil)
		require.Equal(t, http.StatusConflict, reply.Status)
		require.Equal(t, "LAST_PASSKEY", reply.Code)
		require.Len(t, b.passkeys(), 1)
		require.Equal(t, http.StatusOK, s.browser(t).passkeySignIn(second).Status, "and can still sign in with it")
	})

	t.Run("resetting the password removes the account's passkeys", func(t *testing.T) {
		b := s.browser(t)
		_, userID := b.signUp()
		authenticator := passkeytest.New(passkeyHTTPOrigin)
		b.addPasskey(authenticator, "Key")
		passkeySession := s.browser(t)
		require.Equal(t, http.StatusOK, passkeySession.passkeySignIn(authenticator).Status)

		digest := sha256.Sum256([]byte("reset-token-" + userID.String()))
		issued, err := repository.NewPasswordResetRepository(s.db).Issue(context.Background(), &repository.PasswordResetToken{
			ID: uuid.New(), UserID: userID, TokenHash: hex.EncodeToString(digest[:]), ExpiresAt: time.Now().Add(time.Hour),
		}, 0)
		require.NoError(t, err)
		require.True(t, issued)
		reply := s.browser(t).do(http.MethodPost, "/api/auth/reset-password", map[string]string{"token": "reset-token-" + userID.String(), "password": "New-Password-456!"})
		require.Equal(t, http.StatusOK, reply.Status, "%s", reply.Code)

		require.False(t, passkeySession.signedIn())
		reply = s.browser(t).passkeySignIn(authenticator)
		require.Equal(t, http.StatusUnauthorized, reply.Status)
		require.Equal(t, "PASSKEY_UNKNOWN", reply.Code)
	})
}

// Password sign-in, refresh rotation and invite-only registration behave as
// before on an instance where passkeys are in use.
func TestPasswordAuthUnchangedAlongsidePasskeysHTTP(t *testing.T) {
	open := newPasskeyServer(t)
	b := open.browser(t)
	email, userID := b.signUp()
	b.addPasskey(passkeytest.New(passkeyHTTPOrigin), "Key")
	require.Equal(t, http.StatusOK, b.do(http.MethodPost, "/api/auth/logout", nil).Status)

	wrong := b.do(http.MethodPost, "/api/auth/login", map[string]string{"email": email, "password": "Wrong-Password-123"})
	require.Equal(t, http.StatusUnauthorized, wrong.Status)
	require.Equal(t, "INVALID_CREDENTIALS", wrong.Code)
	b.signIn(email)
	require.True(t, b.signedIn())

	// Rotation: the presented refresh token is replaced and cannot be used twice.
	base := mustURL(t, open.url+"/api/auth/refresh")
	var before string
	for _, cookie := range b.client.Jar.Cookies(base) {
		if cookie.Name == "refresh_token" {
			before = cookie.Value
		}
	}
	require.NotEmpty(t, before)
	require.Equal(t, http.StatusOK, b.do(http.MethodPost, "/api/auth/refresh", nil).Status)
	replay := open.browser(t)
	replay.client.Jar.SetCookies(base, []*http.Cookie{{Name: "refresh_token", Value: before, Path: "/api/auth"}})
	require.Equal(t, http.StatusUnauthorized, replay.do(http.MethodPost, "/api/auth/refresh", nil).Status)
	require.True(t, b.signedIn())
	var passwordSessions int
	require.NoError(t, open.db.Get(&passwordSessions, `SELECT COUNT(*) FROM refresh_tokens WHERE user_id = $1 AND passkey_credential_id IS NULL`, userID))
	require.Equal(t, 1, passwordSessions)

	// Invite-only: no account without an invite, passkeys or not.
	closed := newPasskeyServer(t, service.WithRegistrationDisabled(true))
	reply := closed.browser(t).do(http.MethodPost, "/api/auth/register", map[string]string{
		"email": "passkey-" + uuid.NewString() + "@example.test", "password": passkeyHTTPPassword, "name": "No invite",
	})
	require.Equal(t, http.StatusForbidden, reply.Status)
	require.Equal(t, "REGISTRATION_DISABLED", reply.Code)
	existing := closed.browser(t)
	existing.signIn(email)
	require.True(t, existing.signedIn(), "existing accounts still sign in with their password")
}

func mustMarshal(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	require.NoError(t, err)
	return data
}

func mustURL(t *testing.T, raw string) *neturl.URL {
	t.Helper()
	parsed, err := neturl.Parse(raw)
	require.NoError(t, err)
	return parsed
}
