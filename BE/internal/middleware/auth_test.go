package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	jwtpkg "github.com/kuayle/kuayle-backend/pkg/jwt"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakePATRepo struct {
	byHash   map[string]*domain.PersonalAccessToken
	lastUsed chan uuid.UUID
}

func newFakePATRepo() *fakePATRepo {
	return &fakePATRepo{byHash: map[string]*domain.PersonalAccessToken{}, lastUsed: make(chan uuid.UUID, 1)}
}

func (r *fakePATRepo) add(plaintext string, token *domain.PersonalAccessToken) {
	r.byHash[domain.HashToken(plaintext)] = token
}

func (r *fakePATRepo) Create(ctx context.Context, token *domain.PersonalAccessToken) error {
	return nil
}

func (r *fakePATRepo) GetByHash(ctx context.Context, hash string) (*domain.PersonalAccessToken, error) {
	return r.byHash[hash], nil
}

func (r *fakePATRepo) ListByUser(ctx context.Context, userID uuid.UUID) ([]domain.PersonalAccessToken, error) {
	return nil, nil
}

func (r *fakePATRepo) Revoke(ctx context.Context, id, userID uuid.UUID) error {
	return nil
}

func (r *fakePATRepo) UpdateLastUsed(ctx context.Context, id uuid.UUID) error {
	select {
	case r.lastUsed <- id:
	default:
	}
	return nil
}

func runAuth(t *testing.T, repo *fakePATRepo, req *http.Request) (*httptest.ResponseRecorder, echo.Context) {
	t.Helper()
	e := echo.New()
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	handler := Auth("test-secret", repo)(func(c echo.Context) error {
		return c.NoContent(http.StatusNoContent)
	})
	require.NoError(t, handler(c))
	return rec, c
}

func bearerRequest(token string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
	req.Header.Set(echo.HeaderAuthorization, "Bearer "+token)
	return req
}

func TestAuthPATSuccess(t *testing.T) {
	repo := newFakePATRepo()
	userID := uuid.New()
	future := time.Now().Add(time.Hour)
	repo.add("kuayle_pat_abc123", &domain.PersonalAccessToken{
		ID:             uuid.New(),
		UserID:         userID,
		Scopes:         []string{"issues:read"},
		WorkspaceSlugs: []string{"acme"},
		ExpiresAt:      &future,
	})

	rec, c := runAuth(t, repo, bearerRequest("kuayle_pat_abc123"))

	assert.Equal(t, http.StatusNoContent, rec.Code)
	assert.Equal(t, userID, c.Get(string(UserIDKey)))
	assert.Equal(t, []string{"issues:read"}, c.Get(TokenScopesKey))
	assert.Equal(t, []string{"acme"}, c.Get(TokenWorkspacesKey))
	select {
	case <-repo.lastUsed:
	case <-time.After(time.Second):
		t.Fatal("expected last_used_at update")
	}
}

func TestAuthPATViaCookie(t *testing.T) {
	repo := newFakePATRepo()
	userID := uuid.New()
	repo.add("kuayle_pat_cookie", &domain.PersonalAccessToken{
		ID:     uuid.New(),
		UserID: userID,
		Scopes: []string{"issues:read"},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
	req.AddCookie(&http.Cookie{Name: "access_token", Value: "kuayle_pat_cookie"})

	rec, c := runAuth(t, repo, req)

	assert.Equal(t, http.StatusNoContent, rec.Code)
	assert.Equal(t, userID, c.Get(string(UserIDKey)))
	assert.Equal(t, []string{"issues:read"}, c.Get(TokenScopesKey))
}

func TestAuthPATRevoked(t *testing.T) {
	repo := newFakePATRepo()
	now := time.Now()
	repo.add("kuayle_pat_revoked", &domain.PersonalAccessToken{ID: uuid.New(), UserID: uuid.New(), RevokedAt: &now})

	rec, _ := runAuth(t, repo, bearerRequest("kuayle_pat_revoked"))

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestAuthPATExpired(t *testing.T) {
	repo := newFakePATRepo()
	past := time.Now().Add(-time.Hour)
	repo.add("kuayle_pat_expired", &domain.PersonalAccessToken{ID: uuid.New(), UserID: uuid.New(), ExpiresAt: &past})

	rec, _ := runAuth(t, repo, bearerRequest("kuayle_pat_expired"))

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestAuthPATUnknown(t *testing.T) {
	rec, _ := runAuth(t, newFakePATRepo(), bearerRequest("kuayle_pat_doesnotexist"))

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestAuthBearerNonPATFallsThroughToJWT(t *testing.T) {
	rec, _ := runAuth(t, newFakePATRepo(), bearerRequest("not-a-pat-token"))

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestAuthJWTUnaffected(t *testing.T) {
	userID := uuid.New()
	jwt, err := jwtpkg.GenerateAccessToken(userID, "test-secret")
	require.NoError(t, err)

	rec, c := runAuth(t, newFakePATRepo(), bearerRequest(jwt))

	assert.Equal(t, http.StatusNoContent, rec.Code)
	assert.Equal(t, userID, c.Get(string(UserIDKey)))
	assert.Nil(t, c.Get(TokenScopesKey), "JWT requests must not be marked as PAT")
	assert.Nil(t, c.Get(TokenWorkspacesKey))
}
