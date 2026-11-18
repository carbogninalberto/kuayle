package repository

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/domain"
)

// Named migration actions (KUA-181) are configured on a Dev Machine by a
// workspace owner, an admin or the machine's creator. The action holds the
// command (which references the connection secret only as
// $KUAYLE_MIGRATION_SECRET) and the encrypted secret itself. The assistant
// resolves an action by name among the user's usable machines and never
// receives the secret; the machine manager reads it here when the run's
// container is created.

// UserMigrationAction is one migration action of the user's usable machines,
// with the machine's name and state, for the assistant's prompt and refusals.
// The encrypted secret is never selected for it.
type UserMigrationAction struct {
	domain.MigrationAction
	MachineName   string `json:"machine_name" db:"machine_name"`
	MachineStatus string `json:"machine_status" db:"machine_status"`
}

// userMigrationActionColumns selects an action with its machine's name and
// state; encrypted_secret is deliberately absent.
const userMigrationActionColumns = `a.id,a.workspace_id,a.machine_id,a.name,a.description,a.command,'' AS encrypted_secret,
  a.encryption_key_version,a.created_by_user_id,a.created_at,a.updated_at,m.name AS machine_name,m.status AS machine_status`

// MigrationActionsForUser lists the migration actions configured on the
// user's usable machines, by action name then machine name.
func (r *AssistantCommandRepository) MigrationActionsForUser(ctx context.Context, ws, user uuid.UUID) ([]UserMigrationAction, error) {
	actions := []UserMigrationAction{}
	err := r.db.SelectContext(ctx, &actions, `SELECT `+userMigrationActionColumns+` FROM dev_machine_migration_actions a
  JOIN dev_machines m ON m.id=a.machine_id
  WHERE a.workspace_id=$1 AND `+usableMachine+` ORDER BY a.name, m.name`, ws, user)
	return actions, err
}

// MigrationAction returns one action by name on a machine the user owns and
// may use, with its encrypted secret, or ErrMigrationActionNotFound.
func (r *AssistantCommandRepository) MigrationAction(ctx context.Context, ws, user, machine uuid.UUID, name string) (*domain.MigrationAction, error) {
	action := &domain.MigrationAction{}
	err := r.db.GetContext(ctx, action, `SELECT a.* FROM dev_machine_migration_actions a
  JOIN dev_machines m ON m.id=a.machine_id AND m.id=$3
  WHERE a.workspace_id=$1 AND a.name=$4 AND `+usableMachine, ws, user, machine, strings.TrimSpace(name))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrMigrationActionNotFound
	}
	return action, err
}

// MigrationActionSecret returns the encrypted secret of an action the machine
// runs, for the manager, which decrypts it with the instance key. The run's
// own authority has already checked that the requester owns the machine.
func (r *AssistantCommandRepository) MigrationActionSecret(ctx context.Context, ws, machine uuid.UUID, name string) (string, error) {
	var encrypted string
	err := r.db.GetContext(ctx, &encrypted, `SELECT a.encrypted_secret FROM dev_machine_migration_actions a
  WHERE a.workspace_id=$1 AND a.machine_id=$2 AND a.name=$3`, ws, machine, strings.TrimSpace(name))
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrMigrationActionNotFound
	}
	return encrypted, err
}

// MigrationActionSecret is the machine-side authority form: the manager's
// store is the DevMachineRepository, so the same read is delegated to the
// command repository above.
func (r *DevMachineRepository) MigrationActionSecret(ctx context.Context, ws, machine uuid.UUID, name string) (string, error) {
	return NewAssistantCommandRepository(r.db).MigrationActionSecret(ctx, ws, machine, name)
}

// migrationActionVisible admits the machine's creator and current workspace
// owners and admins; their membership is pinned (FOR SHARE) by the caller's
// transaction on writes.
const migrationActionVisible = `EXISTS(SELECT 1 FROM dev_machines m WHERE m.id=a.machine_id AND m.workspace_id=$1 AND m.created_by_user_id=$3)
  OR EXISTS(SELECT 1 FROM workspace_members wm WHERE wm.workspace_id=$1 AND wm.user_id=$3 AND wm.role IN ('owner','admin'))`

// ListMachineMigrationActions lists one machine's migration actions for the
// machine's creator or a workspace owner or admin.
func (r *AssistantCommandRepository) ListMachineMigrationActions(ctx context.Context, ws, machine, user uuid.UUID) ([]domain.MigrationAction, error) {
	actions := []domain.MigrationAction{}
	err := r.db.SelectContext(ctx, &actions, `SELECT a.* FROM dev_machine_migration_actions a
  WHERE a.workspace_id=$1 AND a.machine_id=$2 AND `+migrationActionVisible+` ORDER BY a.name`, ws, machine, user)
	return actions, err
}

func validateMigrationActionInput(action *domain.MigrationAction) error {
	if !domain.ValidMigrationActionName(action.Name) {
		return ErrConversationInvalid
	}
	action.Name = strings.TrimSpace(action.Name)
	action.Description = strings.TrimSpace(action.Description)
	if action.Description == "" || len([]rune(action.Description)) > domain.MigrationDescriptionMaxRunes {
		return ErrConversationInvalid
	}
	if !domain.ValidMigrationCommand(action.Command) {
		return ErrConversationInvalid
	}
	action.Command = strings.TrimSpace(action.Command)
	if action.EncryptedSecret == "" {
		return ErrConversationInvalid
	}
	return nil
}

// lockMigrationActionAdmin pins the machine row and the caller's membership
// until commit and checks that they may configure this machine's migration
// actions: its creator or a current workspace owner or admin.
func lockMigrationActionAdmin(ctx context.Context, tx *sqlx.Tx, ws, machine, user uuid.UUID) error {
	var owner *uuid.UUID
	err := tx.GetContext(ctx, &owner, `SELECT m.created_by_user_id FROM dev_machines m WHERE m.id=$3 AND m.workspace_id=$1 FOR SHARE`, ws, machine)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrMigrationActionNotFound
	}
	if err != nil {
		return err
	}
	if owner != nil && *owner == user {
		return nil
	}
	var role string
	err = tx.GetContext(ctx, &role, `SELECT role FROM workspace_members WHERE workspace_id=$1 AND user_id=$2 FOR SHARE`, ws, user)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrMigrationActionForbidden
	}
	if err != nil {
		return err
	}
	if role != domain.RoleOwner && role != domain.RoleAdmin {
		return ErrMigrationActionForbidden
	}
	return nil
}

// CreateMachineMigrationAction stores a new action. Its name must be unused
// on the machine; the caller's authority is re-checked in the transaction.
func (r *AssistantCommandRepository) CreateMachineMigrationAction(ctx context.Context, ws, machine, user uuid.UUID, action *domain.MigrationAction) (*domain.MigrationAction, error) {
	if err := validateMigrationActionInput(action); err != nil {
		return nil, err
	}
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = lockMigrationActionAdmin(ctx, tx, ws, machine, user); err != nil {
		return nil, err
	}
	created := &domain.MigrationAction{}
	err = tx.GetContext(ctx, created, `WITH inserted AS (INSERT INTO dev_machine_migration_actions(workspace_id,machine_id,name,description,command,encrypted_secret,encryption_key_version,created_by_user_id)
  VALUES($1,$2,$3,$4,$5,$6,1,$7) RETURNING *) SELECT * FROM inserted`,
		ws, machine, action.Name, action.Description, action.Command, action.EncryptedSecret, user)
	if err != nil {
		return nil, migrationActionWriteError(err)
	}
	return created, tx.Commit()
}

// UpdateMachineMigrationAction changes one action's name, description and
// command, and its secret only when a new encrypted one is given. The
// caller's authority is re-checked in the transaction.
func (r *AssistantCommandRepository) UpdateMachineMigrationAction(ctx context.Context, ws, machine, actionID, user uuid.UUID, action *domain.MigrationAction, newSecret bool) (*domain.MigrationAction, error) {
	if err := validateMigrationActionInput(action); err != nil {
		return nil, err
	}
	if !newSecret {
		action.EncryptedSecret = ""
	}
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = lockMigrationActionAdmin(ctx, tx, ws, machine, user); err != nil {
		return nil, err
	}
	updated := &domain.MigrationAction{}
	query := `UPDATE dev_machine_migration_actions SET name=$4,description=$5,command=$6,
  encrypted_secret=CASE WHEN $7::text <> '' THEN $7 ELSE encrypted_secret END,updated_at=now()
  WHERE id=$1 AND workspace_id=$2 AND machine_id=$3 RETURNING *`
	err = tx.GetContext(ctx, updated, query, actionID, ws, machine, action.Name, action.Description, action.Command, action.EncryptedSecret)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrMigrationActionNotFound
	}
	if err != nil {
		return nil, migrationActionWriteError(err)
	}
	return updated, tx.Commit()
}

// DeleteMachineMigrationAction removes one action; its queued runs have
// already copied the command, and a run that has not started fails safely
// because the secret is gone. The caller's authority is re-checked in the
// transaction.
func (r *AssistantCommandRepository) DeleteMachineMigrationAction(ctx context.Context, ws, machine, actionID, user uuid.UUID) error {
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = lockMigrationActionAdmin(ctx, tx, ws, machine, user); err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `DELETE FROM dev_machine_migration_actions WHERE id=$1 AND workspace_id=$2 AND machine_id=$3`, actionID, ws, machine)
	if err != nil {
		return err
	}
	if n, _ := result.RowsAffected(); n == 0 {
		return ErrMigrationActionNotFound
	}
	return tx.Commit()
}

// migrationActionWriteError maps a write's unique violation to its repository
// error; the only unique index is the per-machine action name.
func migrationActionWriteError(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == "dev_machine_migration_actions_name" {
		return ErrMigrationActionConflict
	}
	return err
}
