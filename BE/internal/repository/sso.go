package repository

import (
	"context"
	"database/sql"
	"errors"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/domain"
)

var (
	// ErrSSOLinkRequired: the verified email belongs to an account created
	// before this sign-in, so it must be linked from that account's own
	// authenticated session instead of being taken over by the IdP.
	ErrSSOLinkRequired = errors.New("the account must be linked before it can sign in with SSO")
	// ErrSSOIdentityTaken: the IdP subject is already linked to another user
	// in this workspace.
	ErrSSOIdentityTaken = errors.New("this identity is already linked to another account")
)

// SSORepository persists workspace OIDC settings, flow state and identities.
type SSORepository struct {
	db *sqlx.DB
}

func NewSSORepository(db *sqlx.DB) *SSORepository {
	return &SSORepository{db: db}
}

func (r *SSORepository) HasEnabledConfig(ctx context.Context) (bool, error) {
	var enabled bool
	err := r.db.GetContext(ctx, &enabled, `SELECT EXISTS(SELECT 1 FROM workspace_sso_configs WHERE enabled)`)
	return enabled, err
}

// RequiresSSO runs for every workspace request. Read only the policy flags.
func (r *SSORepository) RequiresSSO(ctx context.Context, workspaceID uuid.UUID) (bool, error) {
	var required bool
	err := r.db.GetContext(ctx, &required, `SELECT EXISTS(SELECT 1 FROM workspace_sso_configs WHERE workspace_id=$1 AND enabled AND require_sso)`, workspaceID)
	return required, err
}

func (r *SSORepository) GetConfig(ctx context.Context, workspaceID uuid.UUID) (*domain.SSOConfig, error) {
	var config domain.SSOConfig
	err := r.db.GetContext(ctx, &config, `SELECT workspace_id, saml_metadata, issuer_url, client_id, client_secret_encrypted,
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
		 default_role, require_sso, enabled, saml_metadata)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT (workspace_id) DO UPDATE SET
			saml_metadata = EXCLUDED.saml_metadata,
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
		config.AllowedDomains, config.ProviderName, config.DefaultRole, config.RequireSSO, config.Enabled, config.SAMLMetadata,
	).Scan(&config.CreatedAt, &config.UpdatedAt)
}

func (r *SSORepository) DeleteConfig(ctx context.Context, workspaceID uuid.UUID) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM workspace_sso_configs WHERE workspace_id = $1`, workspaceID)
	return err
}

func (r *SSORepository) CreateLoginState(ctx context.Context, state *domain.SSOLoginState) error {
	return r.db.QueryRowContext(ctx, `INSERT INTO sso_login_states
		(state_hash, workspace_id, nonce, code_verifier_encrypted, bind_hash, redirect_after, link_user_id, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8) RETURNING created_at`,
		state.StateHash, state.WorkspaceID, state.Nonce, state.CodeVerifierEncrypted,
		state.BindHash, state.RedirectAfter, state.LinkUserID, state.ExpiresAt,
	).Scan(&state.CreatedAt)
}

// ConsumeLoginState deletes and returns the state in one statement, so a
// callback cannot be replayed even under concurrent requests.
func (r *SSORepository) ConsumeLoginState(ctx context.Context, stateHash string) (*domain.SSOLoginState, error) {
	var state domain.SSOLoginState
	err := r.db.GetContext(ctx, &state, `DELETE FROM sso_login_states WHERE state_hash = $1
		RETURNING state_hash, workspace_id, nonce, code_verifier_encrypted, bind_hash, redirect_after, link_user_id, expires_at, created_at`, stateHash)
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

// ProvisionIdentity creates the account of a first SSO sign-in, links the
// identity and adds the workspace membership. A pre-existing account must
// link itself unless it was provisioned by SCIM in this same workspace.
// Concurrent first sign-ins of the same subject converge on one account.
func (r *SSORepository) ProvisionIdentity(ctx context.Context, params SSOProvisionParams) (*domain.User, error) {
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	// Serialize first sign-ins for one identity so the identity and email lookups
	// cannot straddle another provisioner's commit.
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`,
		"sso-provision:"+params.WorkspaceID.String()+":"+params.Issuer+":"+params.Subject); err != nil {
		return nil, err
	}

	// A concurrent request may have committed this exact identity between the
	// service's lookup and this transaction. That is a returning identity, not
	// an existing account being claimed.
	var racer domain.UserIdentity
	err = tx.GetContext(ctx, &racer, `SELECT * FROM user_identities
		WHERE workspace_id = $1 AND issuer = $2 AND subject = $3`,
		params.WorkspaceID, params.Issuer, params.Subject)
	if err == nil {
		var user domain.User
		if err := tx.GetContext(ctx, &user, `SELECT * FROM users WHERE id = $1`, racer.UserID); err != nil {
			return nil, err
		}
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		return &user, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}

	var existing domain.User
	err = tx.GetContext(ctx, &existing, `SELECT id FROM users WHERE lower(email) = lower($1)
		ORDER BY created_at LIMIT 1`, params.Email)
	if err == nil {
		user, err := linkSCIMIdentity(ctx, tx, existing.ID, params)
		if err != nil {
			return nil, err
		}
		return user, tx.Commit()
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}

	var user domain.User
	// ON CONFLICT DO NOTHING keeps the transaction usable when another first
	// sign-in creates the same account concurrently; a unique violation would
	// otherwise abort the whole transaction.
	err = tx.GetContext(ctx, &user, `INSERT INTO users (id, email, name, display_name, password_hash)
		VALUES ($1, $2, $3, $3, $4) ON CONFLICT DO NOTHING RETURNING *`,
		uuid.New(), params.Email, params.Name, params.PasswordHash)
	if errors.Is(err, sql.ErrNoRows) {
		// Another request created the account. Only accept it when the same
		// identity already points at it: that is this same first sign-in
		// committed twice, not an existing account being claimed.
		var identity domain.UserIdentity
		identityErr := tx.GetContext(ctx, &identity, `SELECT * FROM user_identities
			WHERE workspace_id = $1 AND issuer = $2 AND subject = $3`,
			params.WorkspaceID, params.Issuer, params.Subject)
		if errors.Is(identityErr, sql.ErrNoRows) {
			return nil, ErrSSOLinkRequired
		}
		if identityErr != nil {
			return nil, identityErr
		}
		if identityErr := tx.GetContext(ctx, &user, `SELECT * FROM users WHERE id = $1`, identity.UserID); identityErr != nil {
			return nil, identityErr
		}
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		return &user, nil
	}
	if err != nil {
		return nil, err
	}

	if err := insertProvisionedIdentity(ctx, tx, user.ID, params); err != nil {
		return nil, err
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

func linkSCIMIdentity(ctx context.Context, tx *sqlx.Tx, userID uuid.UUID, params SSOProvisionParams) (*domain.User, error) {
	// Only a same-workspace, still-active, SCIM-created account may accept its
	// first verified IdP subject by email. Serialize with SCIM deprovisioning.
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock_shared(610046,0)`); err != nil {
		return nil, err
	}
	var user domain.User
	err := tx.GetContext(ctx, &user, `SELECT u.* FROM users u JOIN scim_users su ON su.user_id=u.id
		JOIN workspace_members wm ON wm.workspace_id=su.workspace_id AND wm.user_id=u.id
		WHERE su.workspace_id=$1 AND u.id=$2 AND su.active AND NOT su.deleted FOR UPDATE OF su`, params.WorkspaceID, userID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrSSOLinkRequired
	}
	if err != nil {
		return nil, err
	}
	var linked bool
	err = tx.GetContext(ctx, &linked, `SELECT EXISTS (SELECT 1 FROM user_identities
		WHERE workspace_id=$1 AND user_id=$2 AND issuer=$3 AND subject=$4)`,
		params.WorkspaceID, userID, params.Issuer, params.Subject)
	if err != nil || linked {
		return &user, err
	}
	err = tx.GetContext(ctx, &linked, `SELECT EXISTS (SELECT 1 FROM user_identities
		WHERE workspace_id=$1 AND user_id=$2)`, params.WorkspaceID, userID)
	if err != nil {
		return nil, err
	}
	if linked {
		return nil, ErrSSOLinkRequired
	}
	if err := insertProvisionedIdentity(ctx, tx, user.ID, params); err != nil {
		return nil, err
	}
	return &user, nil
}

func insertProvisionedIdentity(ctx context.Context, tx *sqlx.Tx, userID uuid.UUID, params SSOProvisionParams) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO user_identities
		(id, user_id, workspace_id, provider, issuer, subject, email, last_login_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, NOW())`,
		uuid.New(), userID, params.WorkspaceID, params.Provider, params.Issuer, params.Subject, params.Email)
	return err
}

// LinkIdentity binds an IdP subject to an account that already proved its own
// credentials. It never adds workspace membership.
func (r *SSORepository) LinkIdentity(ctx context.Context, userID, workspaceID uuid.UUID, provider, issuer, subject, email string) (*domain.UserIdentity, error) {
	var identity domain.UserIdentity
	err := r.db.GetContext(ctx, &identity, `INSERT INTO user_identities
		(id, user_id, workspace_id, provider, issuer, subject, email, last_login_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, NOW())
		ON CONFLICT (workspace_id, issuer, subject) DO NOTHING
		RETURNING *`,
		uuid.New(), userID, workspaceID, provider, issuer, subject, email)
	if errors.Is(err, sql.ErrNoRows) {
		// The subject is already linked. Rebinding it to another account is
		// forbidden; the same account may link it again.
		var existing domain.UserIdentity
		if err := r.db.GetContext(ctx, &existing, `SELECT * FROM user_identities
			WHERE workspace_id = $1 AND issuer = $2 AND subject = $3`,
			workspaceID, issuer, subject); err != nil {
			return nil, err
		}
		if existing.UserID != userID {
			return nil, ErrSSOIdentityTaken
		}
		return &existing, nil
	}
	if err != nil {
		return nil, err
	}
	return &identity, nil
}

// HasIdentity reports whether the user has an external identity in the
// workspace, which is what require-SSO may block a password sign-in for.
func (r *SSORepository) HasIdentity(ctx context.Context, userID, workspaceID uuid.UUID) (bool, error) {
	var exists bool
	err := r.db.GetContext(ctx, &exists, `SELECT EXISTS(
		SELECT 1 FROM user_identities WHERE user_id = $1 AND workspace_id = $2)`, userID, workspaceID)
	return exists, err
}

func (r *SSORepository) UserWorkspaceRoles(ctx context.Context, userID uuid.UUID) ([]domain.SSOWorkspaceRole, error) {
	var roles []domain.SSOWorkspaceRole
	err := r.db.SelectContext(ctx, &roles, `SELECT wm.workspace_id, w.slug, wm.role
		FROM workspace_members wm JOIN workspaces w ON w.id = wm.workspace_id
		WHERE wm.user_id = $1 ORDER BY w.slug`, userID)
	return roles, err
}
