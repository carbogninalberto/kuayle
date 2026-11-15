package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
)

// ErrMachineNotRunning means the machine is not running, so nothing can run
// in its developer container.
var ErrMachineNotRunning = errors.New("the machine is not running")

// GetEnvironmentCheck returns the machine's cached environment check, or nil.
func (r *DevMachineRepository) GetEnvironmentCheck(ctx context.Context, machineID uuid.UUID) (*domain.DevMachineEnvironmentCheck, error) {
	var check domain.DevMachineEnvironmentCheck
	err := r.db.GetContext(ctx, &check, `SELECT * FROM dev_machine_environment_checks WHERE machine_id=$1`, machineID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &check, nil
}

// UpsertPackageIndexProbe records what a probe of containerID found. A row of
// another container is replaced whole, refresh included: it described a
// container that is gone.
func (r *DevMachineRepository) UpsertPackageIndexProbe(ctx context.Context, machineID, workspaceID uuid.UUID, containerID, state string, mtime *time.Time) error {
	_, err := r.db.ExecContext(ctx, `INSERT INTO dev_machine_environment_checks
		(machine_id, workspace_id, container_id, package_index_state, package_index_mtime, checked_at)
		SELECT m.id, m.workspace_id, $3, $4, $5, now() FROM dev_machines m WHERE m.id=$1 AND m.workspace_id=$2
		ON CONFLICT (machine_id) DO UPDATE SET
			package_index_state=EXCLUDED.package_index_state,
			package_index_mtime=EXCLUDED.package_index_mtime,
			checked_at=EXCLUDED.checked_at,
			refresh_state=CASE WHEN dev_machine_environment_checks.container_id=EXCLUDED.container_id
				THEN dev_machine_environment_checks.refresh_state ELSE 'idle' END,
			refresh_exit_code=CASE WHEN dev_machine_environment_checks.container_id=EXCLUDED.container_id
				THEN dev_machine_environment_checks.refresh_exit_code END,
			refresh_output=CASE WHEN dev_machine_environment_checks.container_id=EXCLUDED.container_id
				THEN dev_machine_environment_checks.refresh_output END,
			refresh_started_at=CASE WHEN dev_machine_environment_checks.container_id=EXCLUDED.container_id
				THEN dev_machine_environment_checks.refresh_started_at END,
			refresh_finished_at=CASE WHEN dev_machine_environment_checks.container_id=EXCLUDED.container_id
				THEN dev_machine_environment_checks.refresh_finished_at END,
			container_id=EXCLUDED.container_id`,
		machineID, workspaceID, containerID, state, mtime)
	return err
}

// RequestPackageIndexRefresh queues one package index refresh of the running
// machine's developer container containerID for userID. It returns false
// without queueing when a refresh of that container is already queued or
// running, and ErrMachineNotRunning when the machine is not running.
func (r *DevMachineRepository) RequestPackageIndexRefresh(ctx context.Context, workspaceID, machineID uuid.UUID, containerID string, userID uuid.UUID) (bool, error) {
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var generation int64
	err = tx.GetContext(ctx, &generation, `SELECT generation FROM dev_machines
		WHERE id=$1 AND workspace_id=$2 AND status='running' AND delete_requested_at IS NULL FOR SHARE`, machineID, workspaceID)
	if errors.Is(err, sql.ErrNoRows) {
		return false, ErrMachineNotRunning
	}
	if err != nil {
		return false, err
	}
	var check domain.DevMachineEnvironmentCheck
	err = tx.GetContext(ctx, &check, `SELECT * FROM dev_machine_environment_checks WHERE machine_id=$1 FOR UPDATE`, machineID)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		// Not probed yet: the row starts unknown and stale, so the next
		// scan probes it.
		_, err = tx.ExecContext(ctx, `INSERT INTO dev_machine_environment_checks
			(machine_id, workspace_id, container_id, package_index_state, checked_at, refresh_state, refresh_started_at)
			VALUES ($1,$2,$3,'unknown','epoch','queued',now())`, machineID, workspaceID, containerID)
	case err != nil:
		return false, err
	case check.ContainerID == containerID && (check.RefreshState == domain.PackageIndexRefreshQueued || check.RefreshState == domain.PackageIndexRefreshRunning):
		return false, nil
	case check.ContainerID != containerID:
		_, err = tx.ExecContext(ctx, `UPDATE dev_machine_environment_checks SET container_id=$2,
			package_index_state='unknown', package_index_mtime=NULL, checked_at='epoch',
			refresh_state='queued', refresh_exit_code=NULL, refresh_output=NULL, refresh_started_at=now(), refresh_finished_at=NULL
			WHERE machine_id=$1`, machineID, containerID)
	default:
		_, err = tx.ExecContext(ctx, `UPDATE dev_machine_environment_checks SET
			refresh_state='queued', refresh_exit_code=NULL, refresh_output=NULL, refresh_started_at=now(), refresh_finished_at=NULL
			WHERE machine_id=$1`, machineID)
	}
	if err != nil {
		return false, err
	}
	op := &domain.DevMachineOperation{
		WorkspaceID: workspaceID, MachineID: machineID, Action: domain.DevMachineOpRefreshPackageIndex,
		Status: domain.DevMachineOpStatusPending, Generation: generation,
		IdempotencyKey: "refresh-package-index:" + uuid.NewString(), RequestedByUserID: &userID, MaxAttempts: 1,
	}
	if err = enqueueOperation(ctx, tx, op); err != nil {
		return false, err
	}
	return true, tx.Commit()
}

// SetPackageIndexRefresh records the refresh of containerID: running starts
// it, succeeded and failed finish it with the exit code and output tail. It
// changes nothing once the row describes another container. An empty
// containerID only finishes a refresh still queued or running, whichever
// container it was for: its operation ended without recording a result.
func (r *DevMachineRepository) SetPackageIndexRefresh(ctx context.Context, machineID uuid.UUID, containerID, state string, exitCode *int, output *string) error {
	_, err := r.db.ExecContext(ctx, `UPDATE dev_machine_environment_checks SET refresh_state=$3::text,
		refresh_exit_code=$4, refresh_output=$5,
		refresh_started_at=CASE WHEN $3::text='running' THEN now() ELSE refresh_started_at END,
		refresh_finished_at=CASE WHEN $3::text IN ('succeeded','failed') THEN now() END
		WHERE machine_id=$1 AND (container_id=$2::text OR ($2::text='' AND refresh_state IN ('queued','running')))`,
		machineID, containerID, state, exitCode, output)
	return err
}

// DeleteEnvironmentCheck forgets the machine's environment check: its
// developer container stopped or is gone.
func (r *DevMachineRepository) DeleteEnvironmentCheck(ctx context.Context, machineID uuid.UUID) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM dev_machine_environment_checks WHERE machine_id=$1`, machineID)
	return err
}
