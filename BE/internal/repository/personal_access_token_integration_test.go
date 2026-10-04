package repository

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
)

// Runs against the migrated database supplied by the normal backend CI job.
func TestPersonalAccessTokenRepositoryPostgres(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is not set")
	}
	db, err := sqlx.Connect("pgx", databaseURL)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	owner := &domain.User{ID: uuid.New(), Email: uuid.NewString() + "@example.com", Name: "PAT owner", PasswordHash: "hash"}
	require.NoError(t, NewUserRepository(db).Create(ctx, owner))
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM users WHERE id = $1`, owner.ID) })
	repo := NewPersonalAccessTokenRepository(db)

	for _, tc := range []struct {
		name  string
		slugs []string
	}{
		{"all workspaces", nil},
		{"explicit workspaces", []string{"allowed"}},
		{"stored empty restriction", []string{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			plaintext, hash, prefix, err := domain.GenerateToken()
			require.NoError(t, err)
			expires := time.Now().UTC().Add(time.Hour).Truncate(time.Microsecond)
			token := &domain.PersonalAccessToken{
				ID: uuid.New(), UserID: owner.ID, Name: tc.name, TokenHash: hash,
				TokenPrefix: prefix, Scopes: []string{domain.PermIssuesRead, domain.PermAssetsRead},
				WorkspaceSlugs: tc.slugs, ExpiresAt: &expires,
			}
			require.NoError(t, repo.Create(ctx, token))
			require.False(t, token.CreatedAt.IsZero())
			saved, err := repo.GetByHash(ctx, domain.HashToken(plaintext))
			require.NoError(t, err)
			require.NotNil(t, saved)
			require.Equal(t, token.UserID, saved.UserID)
			require.Equal(t, []string(token.Scopes), []string(saved.Scopes))
			require.Equal(t, tc.slugs, []string(saved.WorkspaceSlugs), "NULL and empty arrays must stay distinguishable")
			require.True(t, saved.ExpiresAt.Equal(expires))
			require.True(t, saved.Active())
			require.NoError(t, repo.UpdateLastUsed(ctx, token.ID))
			saved, err = repo.GetByHash(ctx, hash)
			require.NoError(t, err)
			require.NotNil(t, saved.LastUsedAt)

			// Guessing another user's token ID cannot revoke it or reveal ownership.
			require.ErrorIs(t, repo.Revoke(ctx, token.ID, uuid.New()), sql.ErrNoRows)
			require.NoError(t, repo.Revoke(ctx, token.ID, owner.ID))
			require.ErrorIs(t, repo.Revoke(ctx, token.ID, owner.ID), sql.ErrNoRows)
			saved, err = repo.GetByHash(ctx, hash)
			require.NoError(t, err)
			require.False(t, saved.Active())
			listed, err := repo.ListByUser(ctx, owner.ID)
			require.NoError(t, err)
			for _, item := range listed {
				require.NotEqual(t, token.ID, item.ID)
			}
		})
	}
	missing, err := repo.GetByHash(ctx, domain.HashToken("missing"))
	require.NoError(t, err)
	require.Nil(t, missing)
}
