package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/domain"
)

// InstanceConfigRepository stores the sysadmin overrides of runtime
// configuration (instance_config_overrides), one row per variable. Callers
// validate names and values against the config registry.
type InstanceConfigRepository struct {
	db *sqlx.DB
}

func NewInstanceConfigRepository(db *sqlx.DB) *InstanceConfigRepository {
	return &InstanceConfigRepository{db: db}
}

func (r *InstanceConfigRepository) ListOverrides(ctx context.Context) ([]domain.InstanceConfigOverride, error) {
	rows := []domain.InstanceConfigOverride{}
	err := r.db.SelectContext(ctx, &rows, `SELECT name, value, updated_by, updated_at FROM instance_config_overrides ORDER BY name`)
	return rows, err
}

// SetOverride saves value for name, replacing an earlier override.
func (r *InstanceConfigRepository) SetOverride(ctx context.Context, name, value string, actor uuid.UUID) (*domain.InstanceConfigOverride, error) {
	var row domain.InstanceConfigOverride
	err := r.db.GetContext(ctx, &row, `INSERT INTO instance_config_overrides (name, value, updated_by, updated_at)
		VALUES ($1, $2, $3, now())
		ON CONFLICT (name) DO UPDATE SET value = EXCLUDED.value, updated_by = EXCLUDED.updated_by, updated_at = EXCLUDED.updated_at
		RETURNING name, value, updated_by, updated_at`, name, value, actor)
	if err != nil {
		return nil, err
	}
	return &row, nil
}

// DeleteOverride removes the override of name; there may be none.
func (r *InstanceConfigRepository) DeleteOverride(ctx context.Context, name string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM instance_config_overrides WHERE name = $1`, name)
	return err
}

type restartRunRow struct {
	ID           uuid.UUID  `db:"id"`
	Status       string     `db:"status"`
	RequestedBy  *uuid.UUID `db:"requested_by"`
	RequestedAt  time.Time  `db:"requested_at"`
	Changes      []byte     `db:"changes"`
	Previous     []byte     `db:"previous"`
	BootAttempts int        `db:"boot_attempts"`
	AppliedAt    *time.Time `db:"applied_at"`
	FinishedAt   *time.Time `db:"finished_at"`
	Failure      string     `db:"failure"`
}

const restartRunColumns = `id, status, requested_by, requested_at, changes, previous, boot_attempts, applied_at, finished_at, failure`

func (row restartRunRow) run() (*domain.InstanceRestartRun, error) {
	run := &domain.InstanceRestartRun{
		ID: row.ID, Status: row.Status, RequestedBy: row.RequestedBy, RequestedAt: row.RequestedAt,
		BootAttempts: row.BootAttempts, AppliedAt: row.AppliedAt, FinishedAt: row.FinishedAt, Failure: row.Failure,
	}
	if err := json.Unmarshal(row.Changes, &run.Changes); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(row.Previous, &run.Previous); err != nil {
		return nil, err
	}
	return run, nil
}

// ActiveRestartRun returns the run waiting for its outcome, or nil.
func (r *InstanceConfigRepository) ActiveRestartRun(ctx context.Context) (*domain.InstanceRestartRun, error) {
	return r.restartRun(ctx, `SELECT `+restartRunColumns+` FROM instance_config_restart_runs WHERE status IN ('restarting', 'applying')`)
}

// LatestRestartRun returns the most recent run, or nil.
func (r *InstanceConfigRepository) LatestRestartRun(ctx context.Context) (*domain.InstanceRestartRun, error) {
	return r.restartRun(ctx, `SELECT `+restartRunColumns+` FROM instance_config_restart_runs ORDER BY requested_at DESC LIMIT 1`)
}

func (r *InstanceConfigRepository) restartRun(ctx context.Context, query string) (*domain.InstanceRestartRun, error) {
	var row restartRunRow
	if err := r.db.GetContext(ctx, &row, query); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return row.run()
}

// BeginRestartRun records a run and stores its overrides in one transaction:
// set values replace the override, removed names return to the environment.
// It fails with ErrRestartRunActive while another run waits for its outcome.
func (r *InstanceConfigRepository) BeginRestartRun(ctx context.Context, run domain.InstanceRestartRun, set map[string]string, remove []string) (*domain.InstanceRestartRun, error) {
	changes, err := json.Marshal(run.Changes)
	if err != nil {
		return nil, err
	}
	previous, err := json.Marshal(run.Previous)
	if err != nil {
		return nil, err
	}
	var created *domain.InstanceRestartRun
	err = r.inTx(ctx, func(tx *sqlx.Tx) error {
		var row restartRunRow
		if err := tx.GetContext(ctx, &row, `INSERT INTO instance_config_restart_runs (status, requested_by, changes, previous)
			VALUES ('restarting', $1, $2, $3) RETURNING `+restartRunColumns, run.RequestedBy, changes, previous); err != nil {
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && pgErr.Code == "23505" {
				return domain.ErrRestartRunActive
			}
			return err
		}
		if err := writeOverrides(ctx, tx, set, remove, run.RequestedBy); err != nil {
			return err
		}
		created, err = row.run()
		return err
	})
	return created, err
}

// MarkRestartBoot counts a start of the API with the run's values: the first
// moves it from restarting to applying. It returns the updated run, or nil
// when the run is no longer active.
func (r *InstanceConfigRepository) MarkRestartBoot(ctx context.Context, id uuid.UUID) (*domain.InstanceRestartRun, error) {
	var row restartRunRow
	err := r.db.GetContext(ctx, &row, `UPDATE instance_config_restart_runs
		SET status = 'applying', boot_attempts = boot_attempts + 1, applied_at = COALESCE(applied_at, now())
		WHERE id = $1 AND status IN ('restarting', 'applying') RETURNING `+restartRunColumns, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return row.run()
}

// ConfirmRestartRun marks an applying run healthy. It reports whether the run
// was still applying.
func (r *InstanceConfigRepository) ConfirmRestartRun(ctx context.Context, id uuid.UUID) (bool, error) {
	result, err := r.db.ExecContext(ctx, `UPDATE instance_config_restart_runs SET status = 'confirmed', finished_at = now()
		WHERE id = $1 AND status = 'applying'`, id)
	if err != nil {
		return false, err
	}
	n, err := result.RowsAffected()
	return n > 0, err
}

// RollbackRestartRun puts back the overrides the run replaced and marks it
// rolled back, in one transaction. It reports whether the run was still
// active; the overrides of other variables are not touched.
func (r *InstanceConfigRepository) RollbackRestartRun(ctx context.Context, id uuid.UUID, failure string) (bool, error) {
	rolled := false
	err := r.inTx(ctx, func(tx *sqlx.Tx) error {
		var row restartRunRow
		err := tx.GetContext(ctx, &row, `UPDATE instance_config_restart_runs SET status = 'rolled_back', finished_at = now(), failure = $2
			WHERE id = $1 AND status IN ('restarting', 'applying') RETURNING `+restartRunColumns, id, failure)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		run, err := row.run()
		if err != nil {
			return err
		}
		set, remove := map[string]string{}, []string{}
		for name, value := range run.Previous {
			if value == nil {
				remove = append(remove, name)
			} else {
				set[name] = *value
			}
		}
		rolled = true
		return writeOverrides(ctx, tx, set, remove, nil)
	})
	return rolled, err
}

func writeOverrides(ctx context.Context, tx *sqlx.Tx, set map[string]string, remove []string, actor *uuid.UUID) error {
	for name, value := range set {
		if _, err := tx.ExecContext(ctx, `INSERT INTO instance_config_overrides (name, value, updated_by, updated_at)
			VALUES ($1, $2, $3, now())
			ON CONFLICT (name) DO UPDATE SET value = EXCLUDED.value, updated_by = EXCLUDED.updated_by, updated_at = EXCLUDED.updated_at`,
			name, value, actor); err != nil {
			return err
		}
	}
	for _, name := range remove {
		if _, err := tx.ExecContext(ctx, `DELETE FROM instance_config_overrides WHERE name = $1`, name); err != nil {
			return err
		}
	}
	return nil
}

func (r *InstanceConfigRepository) inTx(ctx context.Context, fn func(*sqlx.Tx) error) error {
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}
