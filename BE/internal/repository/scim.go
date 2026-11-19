package repository

import (
	"context"
	"database/sql"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/domain"
)

var (
	ErrSCIMUnauthorized = errors.New("invalid SCIM bearer token")
	ErrSCIMConflict     = errors.New("SCIM resource conflicts with an existing account or resource")
	ErrSCIMMember       = errors.New("group members must be active SCIM users in this workspace")
	ErrSCIMOwner        = errors.New("a workspace owner cannot be deprovisioned")
)

type SCIMRepository struct {
	db *sqlx.DB
}

func NewSCIMRepository(db *sqlx.DB) *SCIMRepository {
	return &SCIMRepository{db: db}
}

func (r *SCIMRepository) TokenStatus(ctx context.Context, workspaceID uuid.UUID) (domain.SCIMTokenStatus, error) {
	var status domain.SCIMTokenStatus
	err := r.db.GetContext(ctx, &status, `SELECT token_hash IS NOT NULL AS enabled, rotated_at
		FROM workspace_scim_configs WHERE workspace_id=$1`, workspaceID)
	if errors.Is(err, sql.ErrNoRows) {
		err = nil
	}
	return status, err
}

func (r *SCIMRepository) RotateToken(ctx context.Context, workspaceID uuid.UUID, hash string) error {
	_, err := r.db.ExecContext(ctx, `INSERT INTO workspace_scim_configs(workspace_id, token_hash, rotated_at)
		VALUES ($1,$2,clock_timestamp()) ON CONFLICT (workspace_id) DO UPDATE
		SET token_hash=EXCLUDED.token_hash, rotated_at=EXCLUDED.rotated_at`, workspaceID, hash)
	return err
}

func (r *SCIMRepository) RevokeToken(ctx context.Context, workspaceID uuid.UUID) error {
	_, err := r.db.ExecContext(ctx, `UPDATE workspace_scim_configs SET token_hash=NULL WHERE workspace_id=$1`, workspaceID)
	return err
}

// SCIMTransaction holds the workspace's SCIM config lock through each mutation,
// so PATCH, token rotations and team mapping changes are serialized.
type SCIMTransaction struct {
	tx          *sqlx.Tx
	workspaceID uuid.UUID
}

func (r *SCIMRepository) WithToken(ctx context.Context, slug, hash string, write bool, fn func(*SCIMTransaction) error) error {
	return r.transact(ctx, write, func(tx *sqlx.Tx) (uuid.UUID, error) {
		lock := " FOR SHARE OF cfg"
		if write {
			lock = " FOR UPDATE OF cfg"
		}
		var id uuid.UUID
		err := tx.GetContext(ctx, &id, `SELECT cfg.workspace_id FROM workspace_scim_configs cfg
			JOIN workspaces w ON w.id=cfg.workspace_id WHERE w.slug=$1 AND cfg.token_hash=$2`+lock, slug, hash)
		if errors.Is(err, sql.ErrNoRows) {
			err = ErrSCIMUnauthorized
		}
		return id, err
	}, fn)
}

func (r *SCIMRepository) WithWorkspace(ctx context.Context, workspaceID uuid.UUID, fn func(*SCIMTransaction) error) error {
	return r.transact(ctx, true, func(tx *sqlx.Tx) (uuid.UUID, error) {
		var id uuid.UUID
		err := tx.GetContext(ctx, &id, `SELECT workspace_id FROM workspace_scim_configs WHERE workspace_id=$1 FOR UPDATE`, workspaceID)
		return id, err
	}, fn)
}

func (r *SCIMRepository) transact(ctx context.Context, write bool, identify func(*sqlx.Tx) (uuid.UUID, error), fn func(*SCIMTransaction) error) error {
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if write {
		// Workspace and team mutations use the same privacy barrier before locking rows.
		if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock_shared(610046,0)`); err != nil {
			return err
		}
	}
	id, err := identify(tx)
	if err != nil {
		return err
	}
	if err := fn(&SCIMTransaction{tx: tx, workspaceID: id}); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return ErrSCIMConflict
		}
		return err
	}
	return tx.Commit()
}

func (r *SCIMTransaction) WorkspaceID() uuid.UUID {
	return r.workspaceID
}
