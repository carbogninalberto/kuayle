package repository

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
)

// The test database must have all migrations applied. INVITE_TEST_DATABASE_URL
// can override the shared DATABASE_URL used by backend CI.
func TestWorkspaceInviteLinkRepositoryPostgres(t *testing.T) {
	databaseURL := os.Getenv("INVITE_TEST_DATABASE_URL")
	if databaseURL == "" {
		databaseURL = os.Getenv("DATABASE_URL")
	}
	if databaseURL == "" {
		t.Skip("DATABASE_URL and INVITE_TEST_DATABASE_URL are not set")
	}
	db, err := sqlx.Connect("pgx", databaseURL)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	repo := NewWorkspaceInviteLinkRepository(db)
	userRepo := NewUserRepository(db)
	owner := &domain.User{ID: uuid.New(), Email: uuid.NewString() + "@example.com", Name: "Owner", PasswordHash: "hash"}
	require.NoError(t, userRepo.Create(ctx, owner))
	ws := &domain.Workspace{ID: uuid.New(), Name: "Invite tests", Slug: uuid.NewString(), OwnerID: owner.ID}
	require.NoError(t, NewWorkspaceRepository(db).Create(ctx, ws))
	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM workspaces WHERE id = $1`, ws.ID)
		_, _ = db.Exec(`DELETE FROM users WHERE id = $1`, owner.ID)
	})
	newUser := func(t *testing.T) *domain.User {
		user := &domain.User{ID: uuid.New(), Email: uuid.NewString() + "@example.com", Name: "Invited", DisplayName: "Invited", PasswordHash: "hash"}
		t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM users WHERE id = $1`, user.ID) })
		return user
	}
	newLink := func(t *testing.T, maxUses *int) *domain.WorkspaceInviteLink {
		link := &domain.WorkspaceInviteLink{ID: uuid.New(), WorkspaceID: ws.ID, TokenHash: uuid.NewString(), Role: domain.RoleGuest, CreatedBy: owner.ID, ExpiresAt: time.Now().Add(time.Hour), MaxUses: maxUses}
		require.NoError(t, repo.Create(ctx, link))
		return link
	}

	t.Run("concurrent signup consumes only the remaining slot", func(t *testing.T) {
		maxUses := 1
		link := newLink(t, &maxUses)
		const attempts = 12
		users := make([]*domain.User, attempts)
		results := make(chan error, attempts)
		var wg sync.WaitGroup
		for i := range users {
			users[i] = newUser(t)
			wg.Add(1)
			go func(user *domain.User) {
				defer wg.Done()
				_, err := repo.Join(ctx, link.ID, user.ID, user)
				results <- err
			}(users[i])
		}
		wg.Wait()
		close(results)
		successes := 0
		for err := range results {
			if err == nil {
				successes++
			} else {
				require.ErrorIs(t, err, ErrInviteLinkExhausted)
			}
		}
		require.Equal(t, 1, successes)
		saved, err := repo.GetByID(ctx, link.ID)
		require.NoError(t, err)
		require.Equal(t, 1, saved.UseCount)
		accounts := 0
		for _, user := range users {
			savedUser, err := userRepo.GetByID(ctx, user.ID)
			require.NoError(t, err)
			if savedUser == nil {
				continue
			}
			accounts++
			member, err := NewWorkspaceRepository(db).GetMember(ctx, ws.ID, user.ID)
			require.NoError(t, err)
			require.NotNil(t, member)
			require.Equal(t, domain.RoleGuest, member.Role)
		}
		require.Equal(t, 1, accounts, "losing registrations must leave no accounts")
	})

	t.Run("concurrent joins of the same user consume one use", func(t *testing.T) {
		link := newLink(t, nil)
		user := newUser(t)
		require.NoError(t, userRepo.Create(ctx, user))
		var wg sync.WaitGroup
		results := make(chan error, 12)
		for i := 0; i < 12; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				role, err := repo.Join(ctx, link.ID, user.ID, nil)
				if err == nil && role != domain.RoleGuest {
					err = errors.New("wrong membership role")
				}
				results <- err
			}()
		}
		wg.Wait()
		close(results)
		for err := range results {
			require.NoError(t, err)
		}
		saved, err := repo.GetByID(ctx, link.ID)
		require.NoError(t, err)
		require.Equal(t, 1, saved.UseCount)
	})

	t.Run("concurrent different invites preserve membership without consuming twice", func(t *testing.T) {
		user := newUser(t)
		require.NoError(t, userRepo.Create(ctx, user))
		links := []*domain.WorkspaceInviteLink{newLink(t, nil), newLink(t, nil)}
		results := make(chan error, len(links))
		for _, link := range links {
			go func(link *domain.WorkspaceInviteLink) {
				_, err := repo.Join(ctx, link.ID, user.ID, nil)
				results <- err
			}(link)
		}
		for range links {
			require.NoError(t, <-results)
		}
		uses := 0
		for _, link := range links {
			saved, err := repo.GetByID(ctx, link.ID)
			require.NoError(t, err)
			uses += saved.UseCount
		}
		require.Equal(t, 1, uses)
	})

	for _, tc := range []struct {
		name, update string
		expected     error
	}{
		{"expired", "expires_at = clock_timestamp() - interval '1 second'", ErrInviteLinkExpired},
		{"revoked", "revoked_at = clock_timestamp()", ErrInviteLinkRevoked},
		{"exhausted", "max_uses = 1, use_count = 1", ErrInviteLinkExhausted},
	} {
		t.Run(tc.name+" rejects signup and remains idempotent for existing admins", func(t *testing.T) {
			link := newLink(t, nil)
			_, err := db.Exec(`UPDATE workspace_invite_links SET `+tc.update+` WHERE id = $1`, link.ID)
			require.NoError(t, err)
			user := newUser(t)
			_, err = repo.Join(ctx, link.ID, user.ID, user)
			require.ErrorIs(t, err, tc.expected)
			saved, err := userRepo.GetByID(ctx, user.ID)
			require.NoError(t, err)
			require.Nil(t, saved)
			require.NoError(t, userRepo.Create(ctx, user))
			require.NoError(t, NewWorkspaceRepository(db).AddMember(ctx, &domain.WorkspaceMember{WorkspaceID: ws.ID, UserID: user.ID, Role: domain.RoleAdmin}))
			before, err := repo.GetByID(ctx, link.ID)
			require.NoError(t, err)
			role, err := repo.Join(ctx, link.ID, user.ID, nil)
			require.NoError(t, err)
			require.Equal(t, domain.RoleAdmin, role)
			after, err := repo.GetByID(ctx, link.ID)
			require.NoError(t, err)
			require.Equal(t, before.UseCount, after.UseCount)
		})
	}

	t.Run("duplicate email and failed membership leave usage unchanged", func(t *testing.T) {
		link := newLink(t, nil)
		user := newUser(t)
		user.Email = owner.Email
		_, err := repo.Join(ctx, link.ID, user.ID, user)
		require.ErrorIs(t, err, ErrDuplicateEmail)
		_, err = repo.Join(ctx, link.ID, uuid.New(), nil)
		require.Error(t, err, "missing users must fail the membership foreign key")
		saved, err := repo.GetByID(ctx, link.ID)
		require.NoError(t, err)
		require.Zero(t, saved.UseCount)
	})

	t.Run("expiry is checked after waiting for the row lock", func(t *testing.T) {
		link := newLink(t, nil)
		user := newUser(t)
		tx, err := db.BeginTxx(ctx, nil)
		require.NoError(t, err)
		defer func() { _ = tx.Rollback() }()
		_, err = tx.Exec(`UPDATE workspace_invite_links SET expires_at = clock_timestamp() + interval '100 milliseconds' WHERE id = $1`, link.ID)
		require.NoError(t, err)
		results := make(chan error, 1)
		go func() { _, err := repo.Join(ctx, link.ID, user.ID, user); results <- err }()
		// Hold the lock past expiration. The waiting transaction's NOW() can be stale.
		time.Sleep(200 * time.Millisecond)
		require.NoError(t, tx.Commit())
		require.ErrorIs(t, <-results, ErrInviteLinkExpired)
		saved, err := userRepo.GetByID(ctx, user.ID)
		require.NoError(t, err)
		require.Nil(t, saved)
	})
}
