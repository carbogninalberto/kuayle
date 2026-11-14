package repository

import (
	"context"
	"database/sql"
	"errors"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/domain"
)

// SSORepository persists workspace OIDC settings, flow state and identities.
type SSORepository struct {
	db *sqlx.DB
}

func NewSSORepository(db *sqlx.DB) *SSORepository {
	return &SSORepository{db: db}
}

func (r *SSORepository) GetConfig(ctx context.Context, workspaceID uuid.UUID) (*domain.SSOConfig, error) {
	var config domain.SSOConfig
	err := r.db.GetContext(ctx, &config, `SELECT workspace_id, issuer_url, client_id, client_secret_encrypted,
		allowed_domains, provider_name, default_role, require_sso, enabled, created_at, updated_at
		FROM workspace_sso_configs WHERE workspace_id = $1`, workspaceID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &config, nil
}

func (r *SSORepository) SaveConfig(ctx context.Context, config *domain.SSOConfig) error {
	return r.db.QueryRowContext(ctx, `INSERT INTO workspace_sso_configs
		(workspace_id, issuer_url, client_id, client_secret_encrypted, allowed_domains, provider_name,
		 default_role, require_sso, enabled)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (workspace_id) DO UPDATE SET
			issuer_url = EXCLUDED.issuer_url,
			client_id = EXCLUDED.client_id,
			client_secret_encrypted = EXCLUDED.client_secret_encrypted,
			allowed_domains = EXCLUDED.allowed_domains,
			provider_name = EXCLUDED.provider_name,
			default_role = EXCLUDED.default_role,
			require_sso = EXCLUDED.require_sso,
			enabled = EXCLUDED.enabled,
			updated_at = NOW()
		RETURNING created_at, updated_at`,
		config.WorkspaceID, config.IssuerURL, config.ClientID, config.ClientSecretEncrypted,
		config.AllowedDomains, config.ProviderName, config.DefaultRole, config.RequireSSO, config.Enabled,
	).Scan(&config.CreatedAt, &config.UpdatedAt)
}

func (r *SSORepository) DeleteConfig(ctx context.Context, workspaceID uuid.UUID) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM workspace_sso_configs WHERE workspace_id = $1`, workspaceID)
	return err
}

func (r *SSORepository) CreateLoginState(ctx context.Context, state *domain.SSOLoginState) error {
	return r.db.QueryRowContext(ctx, `INSERT INTO sso_login_states
		(state_hash, workspace_id, nonce, code_verifier_encrypted, bind_hash, redirect_after, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING created_at`,
		state.StateHash, state.WorkspaceID, state.Nonce, state.CodeVerifierEncrypted,
		state.BindHash, state.RedirectAfter, state.ExpiresAt,
	).Scan(&state.CreatedAt)
}

// ConsumeLoginState deletes and returns the state in one statement, so a
// callback cannot be replayed even under concurrent requests.
func (r *SSORepository) ConsumeLoginState(ctx context.Context, stateHash string) (*domain.SSOLoginState, error) {
	var state domain.SSOLoginState
	err := r.db.GetContext(ctx, &state, `DELETE FROM sso_login_states WHERE state_hash = $1
		RETURNING state_hash, workspace_id, nonce, code_verifier_encrypted, bind_hash, redirect_after, expires_at, created_at`, stateHash)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &state, nil
}

func (r *SSORepository) DeleteExpiredLoginStates(ctx context.Context) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM sso_login_states WHERE expires_at < NOW()`)
	return err
}

func (r *SSORepository) GetIdentity(ctx context.Context, workspaceID uuid.UUID, issuer, subject string) (*domain.UserIdentity, error) {
	var identity domain.UserIdentity
	err := r.db.GetContext(ctx, &identity, `SELECT * FROM user_identities
		WHERE workspace_id = $1 AND issuer = $2 AND subject = $3`, workspaceID, issuer, subject)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &identity, nil
}

func (r *SSORepository) TouchIdentity(ctx context.Context, id uuid.UUID) error {
	_, err := r.db.ExecContext(ctx, `UPDATE user_identities SET last_login_at = NOW() WHERE id = $1`, id)
	return err
}

// ProvisionIdentity resolves the user of an SSO sign-in: an existing identity
// owner, an existing account with the same verified email, or a newly created
// account, then links the identity and adds the workspace membership. All of
// it commits or rolls back together, and concurrent first sign-ins cannot
// create duplicate identities.
func (r *SSORepository) ProvisionIdentity(ctx context.Context, params SSOProvisionParams) (*domain.User, error) {
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	var user domain.User
	err = tx.GetContext(ctx, &user, `SELECT * FROM users WHERE lower(email) = lower($1)
		ORDER BY created_at LIMIT 1 FOR UPDATE`, params.Email)
	if errors.Is(err, sql.ErrNoRows) {
		// ON CONFLICT DO NOTHING keeps the transaction usable when another
		// first sign-in creates the same account concurrently; a unique
		// violation would otherwise abort the whole transaction.
		err = tx.GetContext(ctx, &user, `INSERT INTO users (id, email, name, display_name, password_hash)
			VALUES ($1, $2, $3, $3, $4) ON CONFLICT DO NOTHING RETURNING *`,
			uuid.New(), params.Email, params.Name, params.PasswordHash)
		if errors.Is(err, sql.ErrNoRows) {
			// Another request won the insert; lock and use its account.
			err = tx.GetContext(ctx, &user, `SELECT * FROM users WHERE lower(email) = lower($1)
				ORDER BY created_at LIMIT 1 FOR UPDATE`, params.Email)
		}
	}
	if err != nil {
		return nil, err
	}

	var identity domain.UserIdentity
	err = tx.GetContext(ctx, &identity, `INSERT INTO user_identities
		(id, user_id, workspace_id, provider, issuer, subject, email, last_login_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, NOW())
		ON CONFLICT (workspace_id, issuer, subject) DO UPDATE SET
			last_login_at = NOW(), email = EXCLUDED.email
		RETURNING *`,
		uuid.New(), user.ID, params.WorkspaceID, params.Provider, params.Issuer, params.Subject, params.Email)
	if err != nil {
		return nil, err
	}

	// An identity bound by a concurrent request wins; never rebind it.
	if identity.UserID != user.ID {
		if err := tx.GetContext(ctx, &user, `SELECT * FROM users WHERE id = $1`, identity.UserID); err != nil {
			return nil, err
		}
	}

	if _, err := tx.ExecContext(ctx, `INSERT INTO workspace_members (workspace_id, user_id, role)
		VALUES ($1, $2, $3) ON CONFLICT (workspace_id, user_id) DO NOTHING`,
		params.WorkspaceID, user.ID, params.DefaultRole); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &user, nil
}

func (r *SSORepository) UserWorkspaceRoles(ctx context.Context, userID uuid.UUID) ([]domain.SSOWorkspaceRole, error) {
	var roles []domain.SSOWorkspaceRole
	err := r.db.SelectContext(ctx, &roles, `SELECT wm.workspace_id, w.slug, wm.role
		FROM workspace_members wm JOIN workspaces w ON w.id = wm.workspace_id
		WHERE wm.user_id = $1 ORDER BY w.slug`, userID)
	return roles, err
}
