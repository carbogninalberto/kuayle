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

// Runs against the migrated database supplied by the normal backend CI job.
func TestSSORepositoryPostgres(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is not set")
	}
	db, err := sqlx.Connect("pgx", databaseURL)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)

	owner := &domain.User{ID: uuid.New(), Email: uuid.NewString() + "@example.test", Name: "SSO owner", PasswordHash: "hash"}
	require.NoError(t, NewUserRepository(db).Create(ctx, owner))
	workspace := &domain.Workspace{ID: uuid.New(), Name: "SSO test", Slug: "sso-" + uuid.NewString()[:8], OwnerID: owner.ID}
	workspaceRepo := NewWorkspaceRepository(db)
	require.NoError(t, workspaceRepo.Create(ctx, workspace))
	require.NoError(t, workspaceRepo.AddMember(ctx, &domain.WorkspaceMember{WorkspaceID: workspace.ID, UserID: owner.ID, Role: domain.RoleOwner}))
	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM workspaces WHERE id = $1`, workspace.ID)
		_, _ = db.Exec(`DELETE FROM users WHERE id = $1`, owner.ID)
	})

	repo := NewSSORepository(db)

	config, err := repo.GetConfig(ctx, workspace.ID)
	require.NoError(t, err)
	require.Nil(t, config)

	saved := &domain.SSOConfig{
		WorkspaceID: workspace.ID, IssuerURL: "https://idp.example", ClientID: "client",
		ClientSecretEncrypted: "encrypted-secret", AllowedDomains: []string{"example.test"},
		ProviderName: "Example IdP", DefaultRole: domain.RoleGuest, RequireSSO: true, Enabled: true,
	}
	require.NoError(t, repo.SaveConfig(ctx, saved))
	require.False(t, saved.CreatedAt.IsZero())
	config, err = repo.GetConfig(ctx, workspace.ID)
	require.NoError(t, err)
	require.Equal(t, "https://idp.example", config.IssuerURL)
	require.Equal(t, []string{"example.test"}, []string(config.AllowedDomains))
	require.Equal(t, domain.RoleGuest, config.DefaultRole)
	require.True(t, config.RequireSSO)
	required, err := repo.RequiresSSO(ctx, workspace.ID)
	require.NoError(t, err)
	require.True(t, required)

	saved.ClientID = "updated-client"
	saved.SAMLMetadata = "<EntityDescriptor/>"
	saved.RequireSSO = false
	require.NoError(t, repo.SaveConfig(ctx, saved))
	config, err = repo.GetConfig(ctx, workspace.ID)
	require.NoError(t, err)
	require.Equal(t, "updated-client", config.ClientID)
	require.Equal(t, saved.SAMLMetadata, config.SAMLMetadata)
	require.False(t, config.RequireSSO)
	required, err = repo.RequiresSSO(ctx, workspace.ID)
	require.NoError(t, err)
	require.False(t, required)

	// Flow state is consumed exactly once.
	state := &domain.SSOLoginState{
		StateHash: uuid.NewString(), WorkspaceID: workspace.ID, Nonce: "nonce",
		CodeVerifierEncrypted: "verifier", BindHash: "bind", RedirectAfter: "/acme/inbox",
		ExpiresAt: time.Now().Add(time.Minute),
	}
	require.NoError(t, repo.CreateLoginState(ctx, state))
	consumed, err := repo.ConsumeLoginState(ctx, state.StateHash)
	require.NoError(t, err)
	require.NotNil(t, consumed)
	require.Equal(t, workspace.ID, consumed.WorkspaceID)
	require.Equal(t, "/acme/inbox", consumed.RedirectAfter)
	again, err := repo.ConsumeLoginState(ctx, state.StateHash)
	require.NoError(t, err)
	require.Nil(t, again)
	expired := &domain.SSOLoginState{
		StateHash: uuid.NewString(), WorkspaceID: workspace.ID, Nonce: "nonce",
		CodeVerifierEncrypted: "verifier", BindHash: "bind", ExpiresAt: time.Now().Add(-time.Minute),
	}
	require.NoError(t, repo.CreateLoginState(ctx, expired))
	require.NoError(t, repo.DeleteExpiredLoginStates(ctx))

	// JIT creates the account, identity and membership together.
	jitEmail := uuid.NewString() + "@example.test"
	user, err := repo.ProvisionIdentity(ctx, SSOProvisionParams{
		WorkspaceID: workspace.ID, Issuer: "https://idp.example", Subject: "subject-1",
		Provider: domain.ProviderOIDC, Email: jitEmail, Name: "JIT Person",
		DefaultRole: domain.RoleGuest, PasswordHash: "random-hash",
	})
	require.NoError(t, err)
	require.Equal(t, jitEmail, user.Email)
	require.Equal(t, "JIT Person", user.Name)
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM users WHERE id = $1`, user.ID) })

	identity, err := repo.GetIdentity(ctx, workspace.ID, "https://idp.example", "subject-1")
	require.NoError(t, err)
	require.NotNil(t, identity)
	require.Equal(t, user.ID, identity.UserID)
	member, err := workspaceRepo.GetMember(ctx, workspace.ID, user.ID)
	require.NoError(t, err)
	require.Equal(t, domain.RoleGuest, member.Role)

	// A second sign-in with the same identity reuses the account.
	againUser, err := repo.ProvisionIdentity(ctx, SSOProvisionParams{
		WorkspaceID: workspace.ID, Issuer: "https://idp.example", Subject: "subject-1",
		Provider: domain.ProviderOIDC, Email: jitEmail, Name: "JIT Person",
		DefaultRole: domain.RoleMember, PasswordHash: "other-hash",
	})
	require.NoError(t, err)
	require.Equal(t, user.ID, againUser.ID)
	member, err = workspaceRepo.GetMember(ctx, workspace.ID, user.ID)
	require.NoError(t, err)
	require.Equal(t, domain.RoleGuest, member.Role, "an existing membership keeps its role")

	// A verified email that already has an account is never auto-linked: the
	// sign-in must come from that account's own authenticated link flow.
	_, err = repo.ProvisionIdentity(ctx, SSOProvisionParams{
		WorkspaceID: workspace.ID, Issuer: "https://idp.example", Subject: "subject-2",
		Provider: domain.ProviderOIDC, Email: owner.Email, Name: "Owner via SSO",
		DefaultRole: domain.RoleMember, PasswordHash: "other-hash",
	})
	require.ErrorIs(t, err, ErrSSOLinkRequired)
	stillUnlinked, err := repo.GetIdentity(ctx, workspace.ID, "https://idp.example", "subject-2")
	require.NoError(t, err)
	require.Nil(t, stillUnlinked)

	// The account's own link flow binds the subject without adding membership.
	linkedIdentity, err := repo.LinkIdentity(ctx, owner.ID, workspace.ID, domain.ProviderOIDC, "https://idp.example", "subject-2", owner.Email)
	require.NoError(t, err)
	require.Equal(t, owner.ID, linkedIdentity.UserID)
	member, err = workspaceRepo.GetMember(ctx, workspace.ID, owner.ID)
	require.NoError(t, err)
	require.Equal(t, domain.RoleOwner, member.Role, "linking never changes an existing role")
	hasIdentity, err := repo.HasIdentity(ctx, owner.ID, workspace.ID)
	require.NoError(t, err)
	require.True(t, hasIdentity)

	// The same subject cannot be rebound to another account.
	other := &domain.User{ID: uuid.New(), Email: uuid.NewString() + "@example.test", Name: "Other", PasswordHash: "hash"}
	require.NoError(t, NewUserRepository(db).Create(ctx, other))
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM users WHERE id = $1`, other.ID) })
	_, err = repo.LinkIdentity(ctx, other.ID, workspace.ID, domain.ProviderOIDC, "https://idp.example", "subject-2", other.Email)
	require.ErrorIs(t, err, ErrSSOIdentityTaken)

	// Linking is idempotent for the same account.
	linkedAgain, err := repo.LinkIdentity(ctx, owner.ID, workspace.ID, domain.ProviderOIDC, "https://idp.example", "subject-2", owner.Email)
	require.NoError(t, err)
	require.Equal(t, owner.ID, linkedAgain.UserID)

	roles, err := repo.UserWorkspaceRoles(ctx, user.ID)
	require.NoError(t, err)
	require.Len(t, roles, 1)
	require.Equal(t, workspace.Slug, roles[0].Slug)
	require.Equal(t, domain.RoleGuest, roles[0].Role)

	// Two concurrent first sign-ins of the same subject converge on one
	// account instead of failing the transaction with a unique violation.
	concurrentEmail := uuid.NewString() + "@example.test"
	users := make(chan *domain.User, 2)
	failures := make(chan error, 2)
	var wait sync.WaitGroup
	for index := 0; index < 2; index++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			concurrentUser, err := repo.ProvisionIdentity(ctx, SSOProvisionParams{
				WorkspaceID: workspace.ID, Issuer: "https://idp.example", Subject: "subject-concurrent",
				Provider: domain.ProviderOIDC, Email: concurrentEmail, Name: "Concurrent Person",
				DefaultRole: domain.RoleMember, PasswordHash: "other-hash",
			})
			if err != nil {
				failures <- err
				return
			}
			users <- concurrentUser
		}()
	}
	wait.Wait()
	close(users)
	close(failures)
	for err := range failures {
		require.NoError(t, err)
	}
	var concurrentID uuid.UUID
	for concurrentUser := range users {
		if concurrentID == uuid.Nil {
			concurrentID = concurrentUser.ID
			t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM users WHERE id = $1`, concurrentID) })
			continue
		}
		require.Equal(t, concurrentID, concurrentUser.ID, "concurrent sign-ins must share one account")
	}
	require.NotEqual(t, uuid.Nil, concurrentID)

	require.NoError(t, repo.TouchIdentity(ctx, identity.ID))
	require.NoError(t, repo.DeleteConfig(ctx, workspace.ID))
	config, err = repo.GetConfig(ctx, workspace.ID)
	require.NoError(t, err)
	require.Nil(t, config)
}

// The SSO workspace that minted a token round-trips, and a connection link
// passes its own on to the token it mints.
func TestTokenSSOWorkspacePostgres(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is not set")
	}
	db, err := sqlx.Connect("pgx", databaseURL)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)

	user := &domain.User{ID: uuid.New(), Email: uuid.NewString() + "@example.test", Name: "Token user", PasswordHash: "hash"}
	require.NoError(t, NewUserRepository(db).Create(ctx, user))
	workspace := &domain.Workspace{ID: uuid.New(), Name: "Token SSO", Slug: "tok-" + uuid.NewString()[:8], OwnerID: user.ID}
	require.NoError(t, NewWorkspaceRepository(db).Create(ctx, workspace))
	t.Cleanup(func() {
		_, _ = db.Exec(`DELETE FROM workspaces WHERE id = $1`, workspace.ID)
		_, _ = db.Exec(`DELETE FROM users WHERE id = $1`, user.ID)
	})

	tokenRepo := NewPersonalAccessTokenRepository(db)
	for _, ssoWorkspace := range []*uuid.UUID{&workspace.ID, nil} {
		_, hash, prefix, err := domain.GenerateToken()
		require.NoError(t, err)
		token := &domain.PersonalAccessToken{ID: uuid.New(), UserID: user.ID, Name: "t", TokenHash: hash, TokenPrefix: prefix, Scopes: []string{domain.PermIssuesRead}, SSOWorkspaceID: ssoWorkspace}
		require.NoError(t, tokenRepo.Create(ctx, token))
		got, err := tokenRepo.GetByHash(ctx, hash)
		require.NoError(t, err)
		require.Equal(t, ssoWorkspace, got.SSOWorkspaceID)
	}

	linkRepo := NewCLILinkRepository(db)
	_, codeHash, codePrefix, err := domain.GenerateCLILinkCode()
	require.NoError(t, err)
	link := &domain.CLIConnectionLink{
		ID: uuid.New(), UserID: user.ID, Name: "cli", CodeHash: codeHash, CodePrefix: codePrefix, Scopes: []string{domain.PermIssuesRead},
		WorkspaceSlugs: []string{workspace.Slug}, ExpiresAt: time.Now().Add(time.Minute), MaxUses: 1,
		SSOWorkspaceID: &workspace.ID,
	}
	require.NoError(t, linkRepo.Create(ctx, link))
	stored, err := linkRepo.GetByHash(ctx, codeHash)
	require.NoError(t, err)
	require.Equal(t, &workspace.ID, stored.SSOWorkspaceID)

	_, tokenHash, tokenPrefix, err := domain.GenerateToken()
	require.NoError(t, err)
	_, err = linkRepo.Redeem(ctx, codeHash, func(current *domain.CLIConnectionLink) (*domain.PersonalAccessToken, error) {
		return &domain.PersonalAccessToken{
			ID: uuid.New(), UserID: current.UserID, Name: "cli token", TokenHash: tokenHash, TokenPrefix: tokenPrefix, Scopes: current.Scopes,
			WorkspaceSlugs: current.WorkspaceSlugs, ConnectionLinkID: &current.ID, SSOWorkspaceID: current.SSOWorkspaceID,
		}, nil
	})
	require.NoError(t, err)
	minted, err := tokenRepo.GetByHash(ctx, tokenHash)
	require.NoError(t, err)
	require.Equal(t, &workspace.ID, minted.SSOWorkspaceID)
}
