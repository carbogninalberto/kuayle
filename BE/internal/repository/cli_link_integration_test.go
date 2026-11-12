package repository

import (
	"context"

	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
)

func cliLinkTestDB(t *testing.T) (*sqlx.DB, context.Context) {
	t.Helper()
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is not set")
	}
	db, err := sqlx.Connect("pgx", databaseURL)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	return db, ctx
}

func createCLILinkTestUser(t *testing.T, db *sqlx.DB, ctx context.Context) *domain.User {
	t.Helper()
	user := &domain.User{ID: uuid.New(), Email: uuid.NewString() + "@example.com", Name: "CLI link owner", PasswordHash: "hash"}
	require.NoError(t, NewUserRepository(db).Create(ctx, user))
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM users WHERE id = $1`, user.ID) })
	return user
}

func insertCLILink(t *testing.T, repo *CLILinkRepository, ctx context.Context, userID uuid.UUID, maxUses int, expiresAt time.Time) (*domain.CLIConnectionLink, string) {
	t.Helper()
	code, hash, prefix, err := domain.GenerateCLILinkCode()
	require.NoError(t, err)
	link := &domain.CLIConnectionLink{
		ID: uuid.New(), UserID: userID, Name: "CLI", CodeHash: hash, CodePrefix: prefix,
		Scopes: []string{domain.PermIssuesRead}, ExpiresAt: expiresAt, MaxUses: maxUses,
	}
	require.NoError(t, repo.Create(ctx, link))
	return link, code
}

func redeemCLILink(repo *CLILinkRepository, ctx context.Context, code string) (*domain.PersonalAccessToken, string, error) {
	var plaintext string
	token, err := repo.Redeem(ctx, domain.HashToken(code), func(link *domain.CLIConnectionLink) (*domain.PersonalAccessToken, error) {
		var hash, prefix string
		plaintext, hash, prefix, _ = domain.GenerateToken()
		return &domain.PersonalAccessToken{
			ID: uuid.New(), UserID: link.UserID, Name: "CLI", TokenHash: hash, TokenPrefix: prefix,
			Scopes: link.Scopes, WorkspaceSlugs: link.WorkspaceSlugs, ExpiresAt: link.TokenExpiresAt,
			ConnectionLinkID: &link.ID,
		}, nil
	})
	return token, plaintext, err
}

func TestCLILinkRepositoryPostgres(t *testing.T) {
	db, ctx := cliLinkTestDB(t)
	user := createCLILinkTestUser(t, db, ctx)
	repo := NewCLILinkRepository(db)
	patRepo := NewPersonalAccessTokenRepository(db)

	t.Run("single use", func(t *testing.T) {
		_, code := insertCLILink(t, repo, ctx, user.ID, 1, time.Now().Add(time.Hour))
		token, plaintext, err := redeemCLILink(repo, ctx, code)
		require.NoError(t, err)
		require.NotNil(t, token.ConnectionLinkID)
		saved, err := patRepo.GetByHash(ctx, domain.HashToken(plaintext))
		require.NoError(t, err)
		require.NotNil(t, saved)
		require.True(t, saved.Active())

		_, _, err = redeemCLILink(repo, ctx, code)
		require.ErrorIs(t, err, ErrCLILinkUsed)

		// Revoking the link after redemption does not revoke the minted token.
		require.NoError(t, repo.Revoke(ctx, *token.ConnectionLinkID, user.ID))
		saved, err = patRepo.GetByHash(ctx, domain.HashToken(plaintext))
		require.NoError(t, err)
		require.True(t, saved.Active())
		_, _, err = redeemCLILink(repo, ctx, code)
		require.ErrorIs(t, err, ErrCLILinkRevoked)

		// Revoking the token itself ends its access.
		require.NoError(t, patRepo.Revoke(ctx, saved.ID, user.ID))
		saved, err = patRepo.GetByHash(ctx, domain.HashToken(plaintext))
		require.NoError(t, err)
		require.False(t, saved.Active())
	})

	t.Run("expired and revoked", func(t *testing.T) {
		_, expired := insertCLILink(t, repo, ctx, user.ID, 1, time.Now().Add(-time.Minute))
		_, _, err := redeemCLILink(repo, ctx, expired)
		require.ErrorIs(t, err, ErrCLILinkExpired)

		link, revoked := insertCLILink(t, repo, ctx, user.ID, 1, time.Now().Add(time.Hour))
		require.NoError(t, repo.Revoke(ctx, link.ID, user.ID))
		_, _, err = redeemCLILink(repo, ctx, revoked)
		require.ErrorIs(t, err, ErrCLILinkRevoked)

		_, _, err = redeemCLILink(repo, ctx, domain.CLILinkPrefix+"doesnotexist")
		require.ErrorIs(t, err, ErrCLILinkInvalid)
	})

	t.Run("concurrent redemption mints exactly one token", func(t *testing.T) {
		_, code := insertCLILink(t, repo, ctx, user.ID, 1, time.Now().Add(time.Hour))
		var wg sync.WaitGroup
		results := make(chan error, 2)
		for i := 0; i < 2; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, _, err := redeemCLILink(repo, ctx, code)
				results <- err
			}()
		}
		wg.Wait()
		close(results)
		var successes, used int
		for err := range results {
			switch {
			case err == nil:
				successes++
			case err == ErrCLILinkUsed:
				used++
			default:
				require.NoError(t, err)
			}
		}
		require.Equal(t, 1, successes)
		require.Equal(t, 1, used)
	})

	t.Run("password reset revokes open links and tokens", func(t *testing.T) {
		link, code := insertCLILink(t, repo, ctx, user.ID, 5, time.Now().Add(time.Hour))
		_, plaintext, err := redeemCLILink(repo, ctx, code)
		require.NoError(t, err)

		resetHash := uuid.NewString()
		_, err = db.ExecContext(ctx, `INSERT INTO password_reset_tokens (id, user_id, token_hash, expires_at) VALUES ($1,$2,$3,NOW() + INTERVAL '1 hour')`,
			uuid.New(), user.ID, resetHash)
		require.NoError(t, err)
		require.NoError(t, NewPasswordResetRepository(db).Reset(ctx, resetHash, "new-hash"))

		stored, err := repo.GetByHash(ctx, link.CodeHash)
		require.NoError(t, err)
		require.NotNil(t, stored.RevokedAt)
		saved, err := patRepo.GetByHash(ctx, domain.HashToken(plaintext))
		require.NoError(t, err)
		require.False(t, saved.Active())
		_, _, err = redeemCLILink(repo, ctx, code)
		require.ErrorIs(t, err, ErrCLILinkRevoked)
	})
}
