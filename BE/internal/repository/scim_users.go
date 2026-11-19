package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
)

type scimUserRow struct {
	ID        uuid.UUID       `db:"user_id"`
	Profile   json.RawMessage `db:"profile"`
	Active    bool            `db:"active"`
	CreatedAt time.Time       `db:"created_at"`
	UpdatedAt time.Time       `db:"updated_at"`
}

func (row scimUserRow) resource() (domain.SCIMUser, error) {
	user := domain.SCIMUser{ID: row.ID, Active: row.Active, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt}
	err := json.Unmarshal(row.Profile, &user.SCIMUserProfile)
	return user, err
}

func (r *SCIMTransaction) User(ctx context.Context, id uuid.UUID) (domain.SCIMUser, error) {
	var row scimUserRow
	err := r.tx.GetContext(ctx, &row, `SELECT user_id, profile, active, created_at, updated_at
		FROM scim_users WHERE workspace_id=$1 AND user_id=$2 AND NOT deleted`, r.workspaceID, id)
	if err != nil {
		return domain.SCIMUser{}, err
	}
	return row.resource()
}

func (r *SCIMTransaction) Users(ctx context.Context, query domain.SCIMQuery) ([]domain.SCIMUser, int, error) {
	const where = ` FROM scim_users WHERE workspace_id=$1 AND NOT deleted AND CASE $2::text
		WHEN '' THEN true
		WHEN 'username' THEN lower(profile->>'userName')=lower($3)
		WHEN 'externalid' THEN profile->>'externalId'=$3
		WHEN 'emails.value' THEN EXISTS (
			SELECT 1 FROM jsonb_array_elements(profile->'emails') AS email WHERE lower(email->>'value')=lower($3))
		WHEN 'workemail' THEN EXISTS (
			SELECT 1 FROM jsonb_array_elements(profile->'emails') AS email WHERE email->>'type'='work' AND lower(email->>'value')=lower($3))
		END`
	var total int
	if err := r.tx.GetContext(ctx, &total, `SELECT count(*)`+where, r.workspaceID, query.Attribute, query.Value); err != nil {
		return nil, 0, err
	}
	var rows []scimUserRow
	err := r.tx.SelectContext(ctx, &rows, `SELECT user_id, profile, active, created_at, updated_at`+where+
		` ORDER BY created_at, user_id LIMIT $4 OFFSET $5`, r.workspaceID, query.Attribute, query.Value, query.Count, query.Start-1)
	if err != nil {
		return nil, 0, err
	}
	users := make([]domain.SCIMUser, 0, len(rows))
	for _, row := range rows {
		user, err := row.resource()
		if err != nil {
			return nil, 0, err
		}
		users = append(users, user)
	}
	return users, total, nil
}

func (r *SCIMTransaction) CreateUser(ctx context.Context, user domain.SCIMUser, email, name, passwordHash string) (domain.SCIMUser, error) {
	var existing bool
	if err := r.tx.GetContext(ctx, &existing, `SELECT EXISTS (SELECT 1 FROM users WHERE lower(email)=lower($1))`, email); err != nil {
		return user, err
	}
	if existing {
		return user, ErrSCIMConflict
	}
	profile, err := json.Marshal(user.SCIMUserProfile)
	if err != nil {
		return user, err
	}
	user.ID = uuid.New()
	if _, err := r.tx.ExecContext(ctx, `INSERT INTO users (id,email,name,display_name,password_hash)
		VALUES ($1,$2,$3,$3,$4)`, user.ID, email, name, passwordHash); err != nil {
		return user, err
	}
	if err := r.tx.QueryRowContext(ctx, `INSERT INTO scim_users (workspace_id,user_id,profile,active)
		VALUES ($1,$2,$3,$4) RETURNING created_at, updated_at`,
		r.workspaceID, user.ID, profile, user.Active).Scan(&user.CreatedAt, &user.UpdatedAt); err != nil {
		return user, err
	}
	if user.Active {
		err = r.addMember(ctx, user.ID)
	}
	return user, err
}

func (r *SCIMTransaction) UpdateUser(ctx context.Context, user domain.SCIMUser, wasActive bool, accountName string) (domain.SCIMUser, error) {
	profile, err := json.Marshal(user.SCIMUserProfile)
	if err != nil {
		return user, err
	}
	err = r.tx.GetContext(ctx, &user.UpdatedAt, `UPDATE scim_users SET profile=$3, active=$4, updated_at=clock_timestamp()
		WHERE workspace_id=$1 AND user_id=$2 RETURNING updated_at`, r.workspaceID, user.ID, profile, user.Active)
	if err != nil {
		return user, err
	}
	switch {
	case !user.Active:
		err = r.deactivate(ctx, user.ID, wasActive)
	case !wasActive && user.Active:
		err = r.addMember(ctx, user.ID)
	}
	if err == nil && accountName != "" {
		_, err = r.tx.ExecContext(ctx, `UPDATE users SET name=$2, display_name=$2, updated_at=clock_timestamp()
			WHERE id=$1`, user.ID, accountName)
	}
	return user, err
}

func (r *SCIMTransaction) addMember(ctx context.Context, userID uuid.UUID) error {
	_, err := r.tx.ExecContext(ctx, `INSERT INTO workspace_members (workspace_id,user_id,role) VALUES ($1,$2,'member')
		ON CONFLICT (workspace_id,user_id) DO NOTHING`, r.workspaceID, userID)
	return err
}

func (r *SCIMTransaction) deactivate(ctx context.Context, userID uuid.UUID, wasActive bool) error {
	var role string
	err := r.tx.GetContext(ctx, &role, `SELECT role FROM workspace_members WHERE workspace_id=$1 AND user_id=$2 FOR UPDATE`,
		r.workspaceID, userID)
	if errors.Is(err, sql.ErrNoRows) {
		if !wasActive {
			return nil
		}
	} else if err != nil {
		return err
	}
	var owner bool
	err = r.tx.GetContext(ctx, &owner, `SELECT COALESCE(owner_id=$2, false) FROM workspaces WHERE id=$1`, r.workspaceID, userID)
	if err != nil {
		return err
	}
	if owner || role == domain.RoleOwner {
		return ErrSCIMOwner
	}
	if err := removeWorkspaceMember(ctx, r.tx, r.workspaceID, userID); err != nil {
		return err
	}
	if _, err := r.tx.ExecContext(ctx, `WITH removed AS (
		DELETE FROM scim_group_members WHERE workspace_id=$1 AND user_id=$2 RETURNING group_id
	)
	UPDATE scim_groups SET updated_at=clock_timestamp()
	WHERE workspace_id=$1 AND id IN (SELECT group_id FROM removed)`, r.workspaceID, userID); err != nil {
		return err
	}
	// Advance the version before revoking credentials so concurrent issuance
	// from an already authenticated session cannot recreate them.
	for _, query := range []string{
		`UPDATE users SET session_version=session_version+1 WHERE id=$1`,
		`DELETE FROM refresh_tokens WHERE user_id=$1`,
		`UPDATE cli_connection_links SET revoked_at=NOW() WHERE user_id=$1 AND revoked_at IS NULL`,
		`UPDATE personal_access_tokens SET revoked_at=NOW() WHERE user_id=$1 AND revoked_at IS NULL`,
	} {
		if _, err := r.tx.ExecContext(ctx, query, userID); err != nil {
			return err
		}
	}
	return nil
}

func (r *SCIMTransaction) DeleteUser(ctx context.Context, user domain.SCIMUser) error {
	if _, err := r.tx.ExecContext(ctx, `UPDATE scim_users SET active=false, deleted=true, updated_at=clock_timestamp()
		WHERE workspace_id=$1 AND user_id=$2`, r.workspaceID, user.ID); err != nil {
		return err
	}
	return r.deactivate(ctx, user.ID, user.Active)
}
