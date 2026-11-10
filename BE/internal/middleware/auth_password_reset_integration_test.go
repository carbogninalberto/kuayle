package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/require"
)

// A password reset is how an account is recovered from someone who knows the
// old password, so API tokens minted before it must stop authenticating.
// Runs against the migrated database supplied by the normal backend CI job.
func TestPasswordResetRevokesPersonalAccessTokensPostgres(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is not set")
	}
	db, err := sqlx.Connect("pgx", databaseURL)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)

	users := repository.NewUserRepository(db)
	patRepo := repository.NewPersonalAccessTokenRepository(db)
	resetRepo := repository.NewPasswordResetRepository(db)
	newAccount := func(name string) (*domain.User, string) {
		user := &domain.User{ID: uuid.New(), Email: uuid.NewString() + "@example.com", Name: name, PasswordHash: "hash"}
		require.NoError(t, users.Create(ctx, user))
		t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM users WHERE id = $1`, user.ID) })
		plaintext, hash, prefix, err := domain.GenerateToken()
		require.NoError(t, err)
		pat := &domain.PersonalAccessToken{ID: uuid.New(), UserID: user.ID, Name: name, TokenHash: hash, TokenPrefix: prefix, Scopes: []string{domain.PermIssuesRead}}
		require.NoError(t, patRepo.Create(ctx, pat))
		return user, plaintext
	}
	status := func(bearer string) int {
		e := echo.New()
		e.GET("/whoami", func(c *echo.Context) error { return c.NoContent(http.StatusNoContent) }, Auth("unused-fixture-jwt-key", patRepo))
		req := httptest.NewRequest(http.MethodGet, "/whoami", nil)
		req.Header.Set("Authorization", "Bearer "+bearer)
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		return rec.Code
	}

	user, userPAT := newAccount("Resetting user")
	_, otherPAT := newAccount("Bystander")
	require.Equal(t, http.StatusNoContent, status(userPAT))
	require.Equal(t, http.StatusNoContent, status(otherPAT))

	digest := uuid.NewString()
	issued, err := resetRepo.Issue(ctx, &repository.PasswordResetToken{ID: uuid.New(), UserID: user.ID, TokenHash: digest, ExpiresAt: time.Now().Add(time.Hour)}, 0)
	require.NoError(t, err)
	require.True(t, issued)

	// A failed reset must not revoke anything.
	require.Error(t, resetRepo.Reset(ctx, uuid.NewString(), "new-hash"))
	require.Equal(t, http.StatusNoContent, status(userPAT))

	require.NoError(t, resetRepo.Reset(ctx, digest, "new-hash"))
	require.Equal(t, http.StatusUnauthorized, status(userPAT), "a token created before the reset must be rejected")
	require.Equal(t, http.StatusNoContent, status(otherPAT), "other users' tokens are unaffected")

	listed, err := patRepo.ListByUser(ctx, user.ID)
	require.NoError(t, err)
	require.Empty(t, listed)

	// Tokens created after the reset work normally.
	plaintext, hash, prefix, err := domain.GenerateToken()
	require.NoError(t, err)
	require.NoError(t, patRepo.Create(ctx, &domain.PersonalAccessToken{ID: uuid.New(), UserID: user.ID, Name: "after reset", TokenHash: hash, TokenPrefix: prefix, Scopes: []string{domain.PermIssuesRead}}))
	require.Equal(t, http.StatusNoContent, status(plaintext))
}
