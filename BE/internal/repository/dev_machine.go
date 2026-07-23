package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jmoiron/sqlx"
	"github.com/kuayle/kuayle-backend/internal/domain"
)

type DevMachineStore interface {
	CreateBundle(context.Context, *domain.DevMachine, []domain.DevMachineAgentProvider, []domain.DevMachineService, []domain.DevMachineVolume, []domain.DevMachineEnvVar, []domain.DevMachineToken, *domain.DevMachineOperation) error
	GetMachine(context.Context, uuid.UUID, uuid.UUID) (*domain.DevMachine, error)
	GetMachineForUser(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) (*domain.DevMachine, error)
	GetMachineInternal(context.Context, uuid.UUID) (*domain.DevMachine, error)
	ListMachines(context.Context, uuid.UUID, string, *uuid.UUID, int, int) ([]domain.DevMachine, int, error)
	ListMachinesForUser(context.Context, uuid.UUID, uuid.UUID, string, *uuid.UUID, int, int) ([]domain.DevMachine, int, error)
	CountActiveMachines(context.Context, uuid.UUID, *uuid.UUID) (int, error)
	GetOperationByIdempotency(context.Context, uuid.UUID, uuid.UUID, string) (*domain.DevMachineOperation, error)
	SetDesiredAndEnqueue(context.Context, uuid.UUID, uuid.UUID, domain.DevMachineStatus, *domain.DevMachineOperation) error
	GetPolicy(context.Context, uuid.UUID) (*domain.DevMachineWorkspacePolicy, error)
	UpsertPolicy(context.Context, *domain.DevMachineWorkspacePolicy) error
	ListServices(context.Context, uuid.UUID, uuid.UUID) ([]domain.DevMachineService, error)
	GetService(context.Context, uuid.UUID, uuid.UUID, string) (*domain.DevMachineService, error)
	GetProvider(context.Context, uuid.UUID, uuid.UUID, string) (*domain.DevMachineAgentProvider, error)
	ListProviders(context.Context, uuid.UUID, uuid.UUID) ([]domain.DevMachineAgentProvider, error)
	ListEnvVarsInternal(context.Context, uuid.UUID, *string, string) ([]domain.DevMachineEnvVar, error)
	ListRuntimeCredentials(context.Context, uuid.UUID) ([]domain.DevMachineRuntimeCredential, error)
	CreateAgentRun(context.Context, *domain.DevMachineAgentRun, *domain.DevMachineOperation) error
	GetAgentRun(context.Context, uuid.UUID, uuid.UUID) (*domain.DevMachineAgentRun, error)
	GetAgentRunForUser(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) (*domain.DevMachineAgentRun, error)
	ListAgentRuns(context.Context, uuid.UUID, *uuid.UUID, int, int) ([]domain.DevMachineAgentRun, int, error)
	ListAgentRunsForUser(context.Context, uuid.UUID, uuid.UUID, *uuid.UUID, int, int) ([]domain.DevMachineAgentRun, int, error)
	CountAgentRunsSince(context.Context, uuid.UUID, time.Time) (int, error)
	HasActiveAgentRun(context.Context, uuid.UUID) (bool, error)
	CancelAgentRun(context.Context, uuid.UUID, uuid.UUID, *domain.DevMachineOperation) error
	CreateEvent(context.Context, *domain.DevMachineEvent) error
	AuthenticateMachineToken(context.Context, string, string) (*domain.DevMachineToken, *domain.DevMachine, error)
	CreateLogChunk(context.Context, *domain.DevMachineLogChunk) error
	ListEvents(context.Context, uuid.UUID, uuid.UUID, int64, int) ([]domain.DevMachineEvent, error)
	ListLogs(context.Context, uuid.UUID, uuid.UUID, *uuid.UUID, int64, int) ([]domain.DevMachineLogChunk, error)
	CreateAccessTicket(context.Context, *domain.DevMachineAccessTicket) error
	ListResourceSamples(context.Context, uuid.UUID, uuid.UUID, int) ([]domain.DevMachineResourceSample, error)
	CreateGitRef(context.Context, *domain.DevMachineGitRef) error
	MachineNameExists(context.Context, uuid.UUID, string) (bool, error)
	MachineNameExistsForUser(context.Context, uuid.UUID, uuid.UUID, string) (bool, error)
	UpdateMachinePreferences(context.Context, uuid.UUID, uuid.UUID, *bool) (*domain.DevMachine, error)
	UpdateMachinePreferencesForUser(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, *bool) (*domain.DevMachine, error)
	TouchMachineActivity(context.Context, uuid.UUID, time.Time) error
	RequestPermanentDelete(context.Context, uuid.UUID, uuid.UUID, *uuid.UUID) (*domain.DevMachineOperation, error)
	BulkPurgeMachines(context.Context, uuid.UUID, []uuid.UUID, time.Time, bool, bool) (int, error)
	ListScopeSettings(context.Context, uuid.UUID) ([]domain.DevMachineScopeSetting, error)
	GetScopeSetting(context.Context, uuid.UUID, *uuid.UUID, *uuid.UUID, *uuid.UUID) (*domain.DevMachineScopeSetting, error)
	UpsertScopeSetting(context.Context, *domain.DevMachineScopeSetting) error
	DeleteScopeSetting(context.Context, uuid.UUID, *uuid.UUID, *uuid.UUID, *uuid.UUID) error
	ScopeResourceExists(context.Context, uuid.UUID, string, *uuid.UUID) (bool, error)
	GetLinkedRepository(context.Context, uuid.UUID, uuid.UUID) (*domain.GitHubRepoModel, error)
	GetLinkedRepositoryByFullName(context.Context, uuid.UUID, string) (*domain.GitHubRepoModel, error)
	GetIssueDevelopmentContext(context.Context, uuid.UUID, uuid.UUID) (*domain.Issue, error)
	GetProjectDevelopmentContext(context.Context, uuid.UUID, uuid.UUID) (*domain.Project, error)
	ListEnvironments(context.Context, uuid.UUID) ([]domain.DevMachineEnvironment, error)
	GetEnvironment(context.Context, uuid.UUID, uuid.UUID) (*domain.DevMachineEnvironment, error)
	CreateEnvironment(context.Context, *domain.DevMachineEnvironment, *domain.DevMachineOperation) error
	RequestEnvironmentDeletion(context.Context, uuid.UUID, uuid.UUID) error
	ListDeleteRequestedEnvironments(context.Context, int) ([]domain.DevMachineEnvironment, error)
	DeleteEnvironment(context.Context, uuid.UUID, uuid.UUID) error
	ListPermanentDeleteRequests(context.Context, int) ([]domain.DevMachine, error)
	GetCheckout(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) (*domain.DevMachineCheckout, error)
	GetCheckoutInternal(context.Context, uuid.UUID) (*domain.DevMachineCheckout, error)
	ListCheckouts(context.Context, uuid.UUID, uuid.UUID) ([]domain.DevMachineCheckout, error)
	CreateCheckout(context.Context, *domain.DevMachineCheckout, *domain.DevMachineOperation) error
	UpdateCheckoutState(context.Context, uuid.UUID, string, *string) error
	CreateTerminalSession(context.Context, *domain.DevMachineTerminalSession) error
	ListTerminalSessions(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) ([]domain.DevMachineTerminalSession, error)
	RequestTerminalSessionClose(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, uuid.UUID, *domain.DevMachineOperation) (*domain.DevMachineTerminalSession, error)
	ListAgentRunSteps(context.Context, uuid.UUID) ([]domain.DevMachineAgentRunStep, error)
	ListAgentRunEvents(context.Context, uuid.UUID, int64, int) ([]domain.DevMachineEvent, error)
	ListAgentRunLogs(context.Context, uuid.UUID, int64, int) ([]domain.DevMachineLogChunk, error)
}

var ErrIdempotencyKeyConflict = errors.New("idempotency key was already used for another operation")
var ErrCheckoutMachineConflict = errors.New("machine is not running or uses another repository")
var ErrActiveAgentRun = errors.New("machine has an active agent run")
var ErrMachineStateConflict = errors.New("machine state changed while queuing operation")
var ErrMachineQuota = errors.New("dev machine quota exceeded")
var ErrMachineNameConflict = errors.New("dev machine name already exists")
var ErrEnvironmentInUse = errors.New("development environment is in use")
var ErrEnvironmentUnavailable = errors.New("development environment is not available")
var ErrEnvironmentInvalidState = errors.New("development environment cannot be deleted in its current state")
var ErrEnvironmentDeletionConflict = errors.New("development environment build is still active")

type DevMachineRepository struct {
	db *sqlx.DB
}

func NewDevMachineRepository(db *sqlx.DB) *DevMachineRepository {
	return &DevMachineRepository{db: db}
}

func (r *DevMachineRepository) CreateBundle(
	ctx context.Context,
	machine *domain.DevMachine,
	providers []domain.DevMachineAgentProvider,
	services []domain.DevMachineService,
	volumes []domain.DevMachineVolume,
	envVars []domain.DevMachineEnvVar,
	tokens []domain.DevMachineToken,
	operation *domain.DevMachineOperation,
) error {
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1::text, 0))`, machine.WorkspaceID); err != nil {
		return err
	}
	if machine.EnvironmentID != nil {
		var ready bool
		if err := tx.GetContext(ctx, &ready, `SELECT EXISTS(SELECT 1 FROM dev_machine_environments
			WHERE workspace_id=$1 AND id=$2 AND status='ready')`, machine.WorkspaceID, machine.EnvironmentID); err != nil {
			return err
		}
		if !ready {
			return ErrEnvironmentUnavailable
		}
	}
	var maxWorkspace, maxUser, workspaceCount, userCount int
	if err := tx.QueryRowContext(ctx, `SELECT max_concurrent_machines, max_machines_per_user
		FROM dev_machine_workspace_policies WHERE workspace_id=$1 AND enabled`, machine.WorkspaceID).Scan(&maxWorkspace, &maxUser); err != nil {
		return err
	}
	if err := tx.GetContext(ctx, &workspaceCount, `SELECT COUNT(*) FROM dev_machines WHERE workspace_id=$1
		AND (status NOT IN ('destroyed','failed','expired','stopped') OR desired_status='running')`, machine.WorkspaceID); err != nil {
		return err
	}
	if err := tx.GetContext(ctx, &userCount, `SELECT COUNT(*) FROM dev_machines WHERE workspace_id=$1 AND created_by_user_id=$2
		AND (status NOT IN ('destroyed','failed','expired','stopped') OR desired_status='running')`, machine.WorkspaceID, machine.CreatedByUserID); err != nil {
		return err
	}
	if workspaceCount >= maxWorkspace || userCount >= maxUser {
		return ErrMachineQuota
	}
	if machine.ProjectID != nil {
		var exists bool
		if err := tx.GetContext(ctx, &exists, `SELECT EXISTS(SELECT 1 FROM projects WHERE id=$1 AND workspace_id=$2)`, machine.ProjectID, machine.WorkspaceID); err != nil {
			return err
		}
		if !exists {
			return fmt.Errorf("invalid project_id for workspace")
		}
	}
	if machine.IssueID != nil {
		var exists bool
		if err := tx.GetContext(ctx, &exists, `SELECT EXISTS(SELECT 1 FROM issues WHERE id=$1 AND workspace_id=$2)`, machine.IssueID, machine.WorkspaceID); err != nil {
			return err
		}
		if !exists {
			return fmt.Errorf("invalid issue_id for workspace")
		}
	}

	const machineQuery = `INSERT INTO dev_machines (
		id, workspace_id, project_id, issue_id, created_by_user_id, routing_key, name,
		status, desired_status, generation, repo_url, repo_provider, repo_owner, repo_name,
		base_branch, working_branch, machine_size, cpu_millis, memory_mb, disk_gb, pids_limit,
		max_runtime_minutes, services_config, labels, expires_at, environment_id,
		repository_affinity_id, keep_running, environment_builder, last_activity_at
	) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,$25,$26,$27,$28,$29,$30)
	RETURNING created_at, updated_at`
	if err := tx.QueryRowContext(ctx, machineQuery,
		machine.ID, machine.WorkspaceID, machine.ProjectID, machine.IssueID, machine.CreatedByUserID,
		machine.RoutingKey, machine.Name, machine.Status, machine.DesiredStatus, machine.Generation,
		machine.RepoURL, machine.RepoProvider, machine.RepoOwner, machine.RepoName, machine.BaseBranch,
		machine.WorkingBranch, machine.MachineSize, machine.CPUMillis, machine.MemoryMB, machine.DiskGB,
		machine.PidsLimit, machine.MaxRuntimeMinutes, machine.ServicesConfig, machine.Labels, machine.ExpiresAt,
		machine.EnvironmentID, machine.RepositoryAffinityID, machine.KeepRunning, machine.EnvironmentBuilder, machine.LastActivityAt,
	).Scan(&machine.CreatedAt, &machine.UpdatedAt); err != nil {
		var postgresError *pgconn.PgError
		if errors.As(err, &postgresError) && postgresError.Code == "23505" && postgresError.ConstraintName == "idx_dev_machines_workspace_name" {
			return ErrMachineNameConflict
		}
		return err
	}

	for i := range providers {
		provider := &providers[i]
		if err := tx.QueryRowContext(ctx, `INSERT INTO dev_machine_agent_providers
			(id, machine_id, provider_id, display_name, image_ref, supported_modes, required_secrets, config, enabled, is_custom)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) RETURNING created_at, updated_at`,
			provider.ID, provider.MachineID, provider.ProviderID, provider.DisplayName, provider.ImageRef,
			provider.SupportedModes, provider.RequiredSecrets, provider.Config, provider.Enabled, provider.IsCustom,
		).Scan(&provider.CreatedAt, &provider.UpdatedAt); err != nil {
			return err
		}
	}

	for i := range services {
		service := &services[i]
		if err := tx.QueryRowContext(ctx, `INSERT INTO dev_machine_services
			(id, machine_id, service_type, service_key, container_name, image_ref, internal_host, internal_port, status, health_status)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) RETURNING created_at, updated_at`,
			service.ID, service.MachineID, service.ServiceType, service.ServiceKey, service.ContainerName,
			service.ImageRef, service.InternalHost, service.InternalPort, service.Status, service.HealthStatus,
		).Scan(&service.CreatedAt, &service.UpdatedAt); err != nil {
			return err
		}
	}

	for i := range volumes {
		volume := &volumes[i]
		if err := tx.QueryRowContext(ctx, `INSERT INTO dev_machine_volumes
			(id, machine_id, volume_type, runtime_name, mount_path, size_limit_bytes)
			VALUES ($1,$2,$3,$4,$5,$6) RETURNING created_at`,
			volume.ID, volume.MachineID, volume.VolumeType, volume.RuntimeName, volume.MountPath, volume.SizeLimitBytes,
		).Scan(&volume.CreatedAt); err != nil {
			return err
		}
	}

	for i := range envVars {
		envVar := &envVars[i]
		if err := tx.QueryRowContext(ctx, `INSERT INTO dev_machine_env_vars
			(id, machine_id, provider_id, target_service, name, encrypted_value, encryption_key_version, is_secret, expires_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9) RETURNING created_at`,
			envVar.ID, envVar.MachineID, envVar.ProviderID, envVar.TargetService, envVar.Name,
			envVar.EncryptedValue, envVar.EncryptionKeyVersion, envVar.IsSecret, envVar.ExpiresAt,
		).Scan(&envVar.CreatedAt); err != nil {
			return err
		}
	}

	for i := range tokens {
		token := &tokens[i]
		if err := tx.QueryRowContext(ctx, `INSERT INTO dev_machine_tokens
			(id, machine_id, agent_run_id, token_hash, scopes, expires_at)
			VALUES ($1,$2,$3,$4,$5,$6) RETURNING created_at`, token.ID, token.MachineID,
			token.AgentRunID, token.TokenHash, token.Scopes, token.ExpiresAt,
		).Scan(&token.CreatedAt); err != nil {
			return err
		}
	}

	if err := enqueueOperation(ctx, tx, operation); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *DevMachineRepository) GetMachine(ctx context.Context, workspaceID, machineID uuid.UUID) (*domain.DevMachine, error) {
	var machine domain.DevMachine
	err := r.db.GetContext(ctx, &machine, `SELECT * FROM dev_machines WHERE workspace_id=$1 AND id=$2`, workspaceID, machineID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &machine, err
}

func (r *DevMachineRepository) GetMachineForUser(ctx context.Context, workspaceID, machineID, userID uuid.UUID) (*domain.DevMachine, error) {
	var machine domain.DevMachine
	err := r.db.GetContext(ctx, &machine, `SELECT * FROM dev_machines
		WHERE workspace_id=$1 AND id=$2 AND created_by_user_id=$3`, workspaceID, machineID, userID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &machine, err
}

func (r *DevMachineRepository) GetMachineInternal(ctx context.Context, machineID uuid.UUID) (*domain.DevMachine, error) {
	var machine domain.DevMachine
	err := r.db.GetContext(ctx, &machine, `SELECT * FROM dev_machines WHERE id=$1`, machineID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &machine, err
}

func (r *DevMachineRepository) GetMachineByRoutingKey(ctx context.Context, routingKey string) (*domain.DevMachine, error) {
	var machine domain.DevMachine
	err := r.db.GetContext(ctx, &machine, `SELECT * FROM dev_machines WHERE routing_key=$1`, routingKey)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &machine, err
}

func (r *DevMachineRepository) ListMachines(ctx context.Context, workspaceID uuid.UUID, status string, issueID *uuid.UUID, limit, offset int) ([]domain.DevMachine, int, error) {
	where := `workspace_id=$1 AND delete_requested_at IS NULL AND ($2='' OR status::text=$2) AND ($3::uuid IS NULL OR issue_id=$3 OR EXISTS (
		SELECT 1 FROM dev_machine_checkouts checkout WHERE checkout.machine_id=dev_machines.id AND checkout.issue_id=$3
	))`
	var total int
	if err := r.db.GetContext(ctx, &total, `SELECT COUNT(*) FROM dev_machines WHERE `+where, workspaceID, status, issueID); err != nil {
		return nil, 0, err
	}
	var machines []domain.DevMachine
	if err := r.db.SelectContext(ctx, &machines, `SELECT * FROM dev_machines WHERE `+where+` ORDER BY created_at DESC, id DESC LIMIT $4 OFFSET $5`, workspaceID, status, issueID, limit, offset); err != nil {
		return nil, 0, err
	}
	if machines == nil {
		machines = []domain.DevMachine{}
	}
	return machines, total, nil
}

func (r *DevMachineRepository) ListMachinesForUser(ctx context.Context, workspaceID, userID uuid.UUID, status string, issueID *uuid.UUID, limit, offset int) ([]domain.DevMachine, int, error) {
	where := `workspace_id=$1 AND created_by_user_id=$2 AND delete_requested_at IS NULL AND ($3='' OR status::text=$3) AND ($4::uuid IS NULL OR issue_id=$4 OR EXISTS (
		SELECT 1 FROM dev_machine_checkouts checkout WHERE checkout.machine_id=dev_machines.id AND checkout.issue_id=$4
	))`
	var total int
	if err := r.db.GetContext(ctx, &total, `SELECT COUNT(*) FROM dev_machines WHERE `+where, workspaceID, userID, status, issueID); err != nil {
		return nil, 0, err
	}
	var machines []domain.DevMachine
	if err := r.db.SelectContext(ctx, &machines, `SELECT * FROM dev_machines WHERE `+where+` ORDER BY created_at DESC, id DESC LIMIT $5 OFFSET $6`, workspaceID, userID, status, issueID, limit, offset); err != nil {
		return nil, 0, err
	}
	if machines == nil {
		machines = []domain.DevMachine{}
	}
	return machines, total, nil
}

func (r *DevMachineRepository) CountActiveMachines(ctx context.Context, workspaceID uuid.UUID, userID *uuid.UUID) (int, error) {
	var count int
	err := r.db.GetContext(ctx, &count, `SELECT COUNT(*) FROM dev_machines
		WHERE workspace_id=$1 AND ($2::uuid IS NULL OR created_by_user_id=$2)
		AND (status NOT IN ('destroyed','failed','expired','stopped') OR desired_status='running')`, workspaceID, userID)
	return count, err
}

func (r *DevMachineRepository) GetOperationByIdempotency(ctx context.Context, workspaceID, machineID uuid.UUID, key string) (*domain.DevMachineOperation, error) {
	var operation domain.DevMachineOperation
	err := r.db.GetContext(ctx, &operation, `SELECT * FROM dev_machine_operations
		WHERE workspace_id=$1 AND machine_id=$2 AND idempotency_key=$3`, workspaceID, machineID, key)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &operation, err
}

func (r *DevMachineRepository) SetDesiredAndEnqueue(ctx context.Context, workspaceID, machineID uuid.UUID, desired domain.DevMachineStatus, operation *domain.DevMachineOperation) error {
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1::text, 0))`, workspaceID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1::text, 0))`, machineID); err != nil {
		return err
	}
	var existing domain.DevMachineOperation
	err = tx.GetContext(ctx, &existing, `SELECT * FROM dev_machine_operations WHERE workspace_id=$1 AND machine_id=$2 AND idempotency_key=$3`, workspaceID, machineID, operation.IdempotencyKey)
	if err == nil {
		if existing.Action != operation.Action || !sameOptionalUUID(existing.RequestedByUserID, operation.RequestedByUserID) {
			return ErrIdempotencyKeyConflict
		}
		*operation = existing
		return tx.Commit()
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	var status domain.DevMachineStatus
	var creatorID *uuid.UUID
	var environmentID *uuid.UUID
	var generation int64
	if err := tx.QueryRowContext(ctx, `SELECT status, generation, created_by_user_id, environment_id
		FROM dev_machines WHERE workspace_id=$1 AND id=$2 FOR UPDATE`, workspaceID, machineID).
		Scan(&status, &generation, &creatorID, &environmentID); err != nil {
		return err
	}
	if generation >= operation.Generation {
		return ErrMachineStateConflict
	}
	var snapshotActive bool
	if err := tx.GetContext(ctx, &snapshotActive, `SELECT EXISTS(SELECT 1 FROM dev_machine_operations
		WHERE machine_id=$1 AND action='snapshot_environment' AND status IN ('pending','leased'))`, machineID); err != nil {
		return err
	}
	if snapshotActive {
		return ErrMachineStateConflict
	}
	if operation.Action == domain.DevMachineOpPause || operation.Action == domain.DevMachineOpStop || operation.Action == domain.DevMachineOpTeardown {
		var active bool
		if err := tx.GetContext(ctx, &active, `SELECT EXISTS(SELECT 1 FROM dev_machine_agent_runs
			WHERE machine_id=$1 AND status IN ('queued','starting','running','waiting_input'))`, machineID); err != nil {
			return err
		}
		if active {
			return ErrActiveAgentRun
		}
	}
	if desired == domain.DevMachineStatusRunning {
		if environmentID != nil {
			var ready bool
			if err := tx.GetContext(ctx, &ready, `SELECT EXISTS(SELECT 1 FROM dev_machine_environments
				WHERE workspace_id=$1 AND id=$2 AND status='ready')`, workspaceID, environmentID); err != nil {
				return err
			}
			if !ready {
				return ErrEnvironmentUnavailable
			}
		}
		if status == domain.DevMachineStatusStopped || status == domain.DevMachineStatusFailed {
			var maxWorkspace, maxUser, workspaceCount, userCount int
			if err := tx.QueryRowContext(ctx, `SELECT max_concurrent_machines, max_machines_per_user
				FROM dev_machine_workspace_policies WHERE workspace_id=$1 AND enabled`, workspaceID).Scan(&maxWorkspace, &maxUser); err != nil {
				return err
			}
			if err := tx.GetContext(ctx, &workspaceCount, `SELECT COUNT(*) FROM dev_machines WHERE workspace_id=$1
				AND (status NOT IN ('destroyed','failed','expired','stopped') OR desired_status='running')`, workspaceID); err != nil {
				return err
			}
			if err := tx.GetContext(ctx, &userCount, `SELECT COUNT(*) FROM dev_machines WHERE workspace_id=$1 AND created_by_user_id=$2
				AND (status NOT IN ('destroyed','failed','expired','stopped') OR desired_status='running')`, workspaceID, creatorID); err != nil {
				return err
			}
			if workspaceCount >= maxWorkspace || userCount >= maxUser {
				return ErrMachineQuota
			}
		}
	}
	result, err := tx.ExecContext(ctx, `UPDATE dev_machines SET desired_status=$1, generation=$2
		WHERE workspace_id=$3 AND id=$4 AND generation<$2`, desired, operation.Generation, workspaceID, machineID)
	if err != nil {
		return err
	}
	if rows, _ := result.RowsAffected(); rows != 1 {
		return sql.ErrNoRows
	}
	if err := enqueueOperation(ctx, tx, operation); err != nil {
		return err
	}
	return tx.Commit()
}

func sameOptionalUUID(left, right *uuid.UUID) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func enqueueOperation(ctx context.Context, tx *sqlx.Tx, operation *domain.DevMachineOperation) error {
	if operation.ID == uuid.Nil {
		operation.ID = uuid.New()
	}
	if operation.MaxAttempts == 0 {
		operation.MaxAttempts = 5
	}
	return tx.QueryRowContext(ctx, `INSERT INTO dev_machine_operations
		(id, machine_id, agent_run_id, checkout_id, environment_id, workspace_id, action, status, generation, idempotency_key,
		 requested_by_user_id, max_attempts)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
		ON CONFLICT (machine_id, idempotency_key) DO UPDATE SET idempotency_key=EXCLUDED.idempotency_key
		RETURNING id, status, attempts, available_at, created_at, updated_at`,
		operation.ID, operation.MachineID, operation.AgentRunID, operation.CheckoutID, operation.EnvironmentID, operation.WorkspaceID, operation.Action,
		operation.Status, operation.Generation, operation.IdempotencyKey, operation.RequestedByUserID, operation.MaxAttempts,
	).Scan(&operation.ID, &operation.Status, &operation.Attempts, &operation.AvailableAt, &operation.CreatedAt, &operation.UpdatedAt)
}

func (r *DevMachineRepository) LeaseOperations(ctx context.Context, owner string, limit int, duration time.Duration) ([]domain.DevMachineOperation, error) {
	var operations []domain.DevMachineOperation
	err := r.db.SelectContext(ctx, &operations, `WITH ranked AS (
		SELECT o.id, ROW_NUMBER() OVER (
			PARTITION BY o.machine_id
			ORDER BY CASE WHEN o.action IN ('teardown','cancel_agent','terminate_terminal') THEN 0 ELSE 1 END,
				o.generation DESC, o.created_at
		) AS rn
		FROM dev_machine_operations o
		WHERE ((o.status='pending' AND o.available_at <= NOW())
		   OR (o.status='leased' AND o.lease_expires_at < NOW()))
		AND (o.action IN ('teardown','cancel_agent','terminate_terminal') OR NOT EXISTS (
			SELECT 1 FROM dev_machine_operations leased
			WHERE leased.machine_id=o.machine_id AND leased.id<>o.id
			AND leased.status='leased' AND leased.lease_expires_at >= NOW()
		))
	), candidates AS (
		SELECT o.id FROM ranked r
		JOIN dev_machine_operations o ON o.id=r.id
		JOIN dev_machines m ON m.id=o.machine_id
		WHERE r.rn=1
		ORDER BY CASE WHEN o.action IN ('teardown','cancel_agent','terminate_terminal') THEN 0 ELSE 1 END,
			o.generation DESC, o.created_at
		LIMIT $1 FOR UPDATE OF o, m SKIP LOCKED
	) UPDATE dev_machine_operations o SET status='leased', lease_owner=$2,
		lease_expires_at=NOW()+make_interval(secs => $3), attempts=attempts+1
	FROM candidates c WHERE o.id=c.id RETURNING o.*`, limit, owner, int(duration.Seconds()))
	return operations, err
}

func (r *DevMachineRepository) RenewOperationLease(ctx context.Context, id uuid.UUID, owner string, duration time.Duration) error {
	result, err := r.db.ExecContext(ctx, `UPDATE dev_machine_operations SET lease_expires_at=NOW()+make_interval(secs => $3)
		WHERE id=$1 AND status='leased' AND lease_owner=$2`, id, owner, int(duration.Seconds()))
	if err != nil {
		return err
	}
	if rows, _ := result.RowsAffected(); rows != 1 {
		return fmt.Errorf("operation lease lost")
	}
	return nil
}

func (r *DevMachineRepository) EnqueueInternalOperation(ctx context.Context, operation *domain.DevMachineOperation) error {
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := enqueueOperation(ctx, tx, operation); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *DevMachineRepository) CompleteOperation(ctx context.Context, id uuid.UUID, owner string) error {
	result, err := r.db.ExecContext(ctx, `UPDATE dev_machine_operations SET status='completed', completed_at=NOW(), lease_owner=NULL, lease_expires_at=NULL
		WHERE id=$1 AND status='leased' AND lease_owner=$2`, id, owner)
	if err != nil {
		return err
	}
	if rows, _ := result.RowsAffected(); rows != 1 {
		return fmt.Errorf("operation lease lost")
	}
	return nil
}

func (r *DevMachineRepository) FailOperation(ctx context.Context, id uuid.UUID, owner, code, message string, retryable bool) (bool, error) {
	var retry bool
	err := r.db.QueryRowContext(ctx, `UPDATE dev_machine_operations SET
		status=CASE WHEN $5 AND attempts < max_attempts THEN 'pending'::dev_machine_operation_status ELSE 'failed'::dev_machine_operation_status END,
		error_code=$3, error_message=$4, lease_owner=NULL, lease_expires_at=NULL,
		available_at=NOW() + make_interval(secs => LEAST(300, attempts * attempts * 5)),
		completed_at=CASE WHEN $5 AND attempts < max_attempts THEN NULL ELSE NOW() END
		WHERE id=$1 AND lease_owner=$2
		RETURNING status='pending'`, id, owner, code, message, retryable).Scan(&retry)
	return retry, err
}

func (r *DevMachineRepository) SetMachineState(ctx context.Context, machineID uuid.UUID, status domain.DevMachineStatus, networkName, volumeName *string, errorCode, errorMessage *string) error {
	_, err := r.db.ExecContext(ctx, `UPDATE dev_machines SET status=$1::dev_machine_status, docker_network_name=COALESCE($2,docker_network_name),
		workspace_volume_name=COALESCE($3,workspace_volume_name), last_error_code=$4, last_error_message=$5,
		started_at=CASE WHEN $1::dev_machine_status='running' THEN COALESCE(started_at,NOW()) ELSE started_at END,
		stopped_at=CASE WHEN $1::dev_machine_status='stopped' THEN NOW() ELSE stopped_at END,
		teardown_at=CASE WHEN $1::dev_machine_status='tearing_down' THEN NOW() ELSE teardown_at END,
		destroyed_at=CASE WHEN $1::dev_machine_status='destroyed' THEN NOW() ELSE destroyed_at END
		WHERE id=$6`, status, networkName, volumeName, errorCode, errorMessage, machineID)
	return err
}

func (r *DevMachineRepository) SetMachineStateForOperation(ctx context.Context, machineID uuid.UUID, generation int64, allowStale bool, status domain.DevMachineStatus, networkName, volumeName *string, errorCode, errorMessage *string) (bool, error) {
	result, err := r.db.ExecContext(ctx, `UPDATE dev_machines SET status=$1::dev_machine_status, docker_network_name=COALESCE($2,docker_network_name),
		workspace_volume_name=COALESCE($3,workspace_volume_name), last_error_code=$4, last_error_message=$5,
		started_at=CASE WHEN $1::dev_machine_status='running' THEN COALESCE(started_at,NOW()) ELSE started_at END,
		stopped_at=CASE WHEN $1::dev_machine_status='stopped' THEN NOW() ELSE stopped_at END,
		teardown_at=CASE WHEN $1::dev_machine_status='tearing_down' THEN NOW() ELSE teardown_at END,
		destroyed_at=CASE WHEN $1::dev_machine_status='destroyed' THEN NOW() ELSE destroyed_at END
		WHERE id=$6 AND ($7 OR generation=$8)`, status, networkName, volumeName, errorCode, errorMessage, machineID, allowStale, generation)
	if err != nil {
		return false, err
	}
	rows, _ := result.RowsAffected()
	return rows == 1, nil
}

func (r *DevMachineRepository) ListExpiredMachines(ctx context.Context, limit int) ([]domain.DevMachine, error) {
	var machines []domain.DevMachine
	err := r.db.SelectContext(ctx, &machines, `SELECT m.* FROM dev_machines m WHERE m.expires_at <= NOW()
		AND m.status NOT IN ('tearing_down','destroyed') AND NOT EXISTS (
			SELECT 1 FROM dev_machine_operations o WHERE o.machine_id=m.id AND o.action='teardown' AND o.status IN ('pending','leased')
		) ORDER BY m.expires_at LIMIT $1`, limit)
	return machines, err
}

func (r *DevMachineRepository) ListTimedOutAgentRuns(ctx context.Context, limit int) ([]domain.DevMachineAgentRun, error) {
	var runs []domain.DevMachineAgentRun
	err := r.db.SelectContext(ctx, &runs, `SELECT * FROM dev_machine_agent_runs WHERE status='running'
		AND started_at IS NOT NULL AND started_at + make_interval(secs => max_runtime_seconds) <= NOW()
		ORDER BY started_at LIMIT $1`, limit)
	return runs, err
}

func (r *DevMachineRepository) ListRuntimeMachines(ctx context.Context, limit, offset int) ([]domain.DevMachine, error) {
	var machines []domain.DevMachine
	err := r.db.SelectContext(ctx, &machines, `SELECT * FROM dev_machines
		WHERE (status IN ('spawning','running','paused','stopping','tearing_down','failed')
		OR (desired_status='running' AND status NOT IN ('destroyed','tearing_down','expired')))
		AND NOT EXISTS (SELECT 1 FROM dev_machine_operations o WHERE o.machine_id=dev_machines.id
			AND o.status IN ('pending','leased'))
		ORDER BY updated_at, id LIMIT $1 OFFSET $2`, limit, offset)
	return machines, err
}

func (r *DevMachineRepository) GetPolicy(ctx context.Context, workspaceID uuid.UUID) (*domain.DevMachineWorkspacePolicy, error) {
	var policy domain.DevMachineWorkspacePolicy
	err := r.db.GetContext(ctx, &policy, `SELECT * FROM dev_machine_workspace_policies WHERE workspace_id=$1`, workspaceID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &policy, err
}

func (r *DevMachineRepository) UpsertPolicy(ctx context.Context, policy *domain.DevMachineWorkspacePolicy) error {
	return r.db.QueryRowContext(ctx, `INSERT INTO dev_machine_workspace_policies
		(workspace_id, enabled, max_concurrent_machines, max_machines_per_user, max_daily_agent_runs,
		 max_runtime_minutes, max_disk_gb, allowed_providers, allowed_repositories, allow_custom_providers, idle_pause_minutes)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
		ON CONFLICT (workspace_id) DO UPDATE SET enabled=EXCLUDED.enabled,
		max_concurrent_machines=EXCLUDED.max_concurrent_machines, max_machines_per_user=EXCLUDED.max_machines_per_user,
		max_daily_agent_runs=EXCLUDED.max_daily_agent_runs, max_runtime_minutes=EXCLUDED.max_runtime_minutes,
		max_disk_gb=EXCLUDED.max_disk_gb, allowed_providers=EXCLUDED.allowed_providers,
		allowed_repositories=EXCLUDED.allowed_repositories, allow_custom_providers=EXCLUDED.allow_custom_providers,
		idle_pause_minutes=EXCLUDED.idle_pause_minutes
		RETURNING created_at, updated_at`, policy.WorkspaceID, policy.Enabled, policy.MaxConcurrentMachines,
		policy.MaxMachinesPerUser, policy.MaxDailyAgentRuns, policy.MaxRuntimeMinutes, policy.MaxDiskGB,
		policy.AllowedProviders, policy.AllowedRepositories, policy.AllowCustomProviders, policy.IdlePauseMinutes,
	).Scan(&policy.CreatedAt, &policy.UpdatedAt)
}

func (r *DevMachineRepository) ListServices(ctx context.Context, workspaceID, machineID uuid.UUID) ([]domain.DevMachineService, error) {
	var services []domain.DevMachineService
	err := r.db.SelectContext(ctx, &services, `SELECT s.* FROM dev_machine_services s JOIN dev_machines m ON m.id=s.machine_id
		WHERE m.workspace_id=$1 AND m.id=$2 ORDER BY CASE s.service_type
			WHEN 'egress' THEN 0 WHEN 'collector' THEN 1 WHEN 'ide' THEN 2
			WHEN 'browser' THEN 3 ELSE 4 END, s.service_key`, workspaceID, machineID)
	return services, err
}

func (r *DevMachineRepository) GetService(ctx context.Context, workspaceID, machineID uuid.UUID, serviceKey string) (*domain.DevMachineService, error) {
	var service domain.DevMachineService
	err := r.db.GetContext(ctx, &service, `SELECT s.* FROM dev_machine_services s JOIN dev_machines m ON m.id=s.machine_id
		WHERE m.workspace_id=$1 AND m.id=$2 AND s.service_key=$3`, workspaceID, machineID, serviceKey)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &service, err
}

func (r *DevMachineRepository) GetRoute(ctx context.Context, routingKey, serviceType string) (*domain.DevMachine, *domain.DevMachineService, error) {
	var machine domain.DevMachine
	if err := r.db.GetContext(ctx, &machine, `SELECT m.* FROM dev_machines m
		JOIN dev_machine_workspace_policies p ON p.workspace_id=m.workspace_id AND p.enabled
		WHERE m.routing_key=$1`, routingKey); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil, nil
		}
		return nil, nil, err
	}
	var service domain.DevMachineService
	err := r.db.GetContext(ctx, &service, `SELECT * FROM dev_machine_services WHERE machine_id=$1 AND service_type=$2
		AND agent_run_id IS NULL AND status='running' AND $3::dev_machine_status='running'
		ORDER BY created_at LIMIT 1`, machine.ID, serviceType, machine.DesiredStatus)
	if errors.Is(err, sql.ErrNoRows) {
		return &machine, nil, nil
	}
	return &machine, &service, err
}

func (r *DevMachineRepository) UpdateServiceRuntime(ctx context.Context, serviceID uuid.UUID, containerID, status, healthStatus string, healthMessage *string) error {
	_, err := r.db.ExecContext(ctx, `UPDATE dev_machine_services SET container_id=$1, status=$2::varchar, health_status=$3,
		health_message=$4, started_at=CASE WHEN $2::varchar='running' THEN COALESCE(started_at,NOW()) ELSE started_at END,
		stopped_at=CASE WHEN $2::varchar='stopped' THEN NOW() ELSE stopped_at END WHERE id=$5`,
		containerID, status, healthStatus, healthMessage, serviceID)
	return err
}

func (r *DevMachineRepository) CreateRuntimeService(ctx context.Context, service *domain.DevMachineService) error {
	return r.db.QueryRowContext(ctx, `INSERT INTO dev_machine_services
		(id, machine_id, agent_run_id, service_type, service_key, container_name, image_ref, internal_host,
		 internal_port, status, health_status)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
		ON CONFLICT (machine_id, service_key) DO UPDATE SET image_ref=EXCLUDED.image_ref
		RETURNING id, created_at, updated_at`,
		service.ID, service.MachineID, service.AgentRunID, service.ServiceType, service.ServiceKey,
		service.ContainerName, service.ImageRef, service.InternalHost, service.InternalPort,
		service.Status, service.HealthStatus,
	).Scan(&service.ID, &service.CreatedAt, &service.UpdatedAt)
}

func (r *DevMachineRepository) GetProvider(ctx context.Context, workspaceID, machineID uuid.UUID, providerID string) (*domain.DevMachineAgentProvider, error) {
	var provider domain.DevMachineAgentProvider
	err := r.db.GetContext(ctx, &provider, `SELECT p.* FROM dev_machine_agent_providers p JOIN dev_machines m ON m.id=p.machine_id
		WHERE m.workspace_id=$1 AND m.id=$2 AND p.provider_id=$3 AND p.enabled`, workspaceID, machineID, providerID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &provider, err
}

func (r *DevMachineRepository) ListProviders(ctx context.Context, workspaceID, machineID uuid.UUID) ([]domain.DevMachineAgentProvider, error) {
	var providers []domain.DevMachineAgentProvider
	err := r.db.SelectContext(ctx, &providers, `SELECT p.* FROM dev_machine_agent_providers p JOIN dev_machines m ON m.id=p.machine_id
		WHERE m.workspace_id=$1 AND m.id=$2 ORDER BY p.provider_id`, workspaceID, machineID)
	return providers, err
}

func (r *DevMachineRepository) ListEnvVarsInternal(ctx context.Context, machineID uuid.UUID, providerID *string, target string) ([]domain.DevMachineEnvVar, error) {
	var envVars []domain.DevMachineEnvVar
	err := r.db.SelectContext(ctx, &envVars, `SELECT * FROM dev_machine_env_vars WHERE machine_id=$1 AND ($2='' OR target_service=$2)
		AND revoked_at IS NULL AND (expires_at IS NULL OR expires_at>NOW()) AND ($3::text IS NULL OR provider_id IS NULL OR provider_id=$3)
		ORDER BY name`, machineID, target, providerID)
	return envVars, err
}

func (r *DevMachineRepository) UpsertRuntimeCredential(ctx context.Context, credential *domain.DevMachineRuntimeCredential) error {
	if credential.ID == uuid.Nil {
		credential.ID = uuid.New()
	}
	if credential.Scope == "" {
		credential.Scope = domain.DevMachineRuntimeCredentialScopeMachine
	}
	if credential.CredentialType == "" {
		credential.CredentialType = domain.DevMachineRuntimeCredentialTypeGitHubToken
	}
	if credential.EncryptionKeyVersion == 0 {
		credential.EncryptionKeyVersion = 1
	}
	return r.db.QueryRowContext(ctx, `INSERT INTO dev_machine_runtime_credentials
		(id, machine_id, scope, credential_type, fingerprint_sha256, encrypted_value, encryption_key_version, expires_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
		ON CONFLICT (machine_id, fingerprint_sha256) DO UPDATE SET
			scope=EXCLUDED.scope,
			credential_type=EXCLUDED.credential_type,
			encrypted_value=EXCLUDED.encrypted_value,
			encryption_key_version=EXCLUDED.encryption_key_version,
			expires_at=EXCLUDED.expires_at,
			updated_at=NOW()
		RETURNING id, created_at, updated_at`, credential.ID, credential.MachineID, credential.Scope,
		credential.CredentialType, credential.FingerprintSHA256, credential.EncryptedValue,
		credential.EncryptionKeyVersion, credential.ExpiresAt.UTC(),
	).Scan(&credential.ID, &credential.CreatedAt, &credential.UpdatedAt)
}

func (r *DevMachineRepository) ListRuntimeCredentials(ctx context.Context, machineID uuid.UUID) ([]domain.DevMachineRuntimeCredential, error) {
	var credentials []domain.DevMachineRuntimeCredential
	err := r.db.SelectContext(ctx, &credentials, `SELECT * FROM dev_machine_runtime_credentials
		WHERE machine_id=$1 AND expires_at>NOW() ORDER BY expires_at ASC, created_at ASC`, machineID)
	if credentials == nil {
		credentials = []domain.DevMachineRuntimeCredential{}
	}
	return credentials, err
}

func (r *DevMachineRepository) PurgeExpiredRuntimeCredentials(ctx context.Context, now time.Time) (int, error) {
	result, err := r.db.ExecContext(ctx, `DELETE FROM dev_machine_runtime_credentials WHERE expires_at<=$1`, now.UTC())
	if err != nil {
		return 0, err
	}
	rows, _ := result.RowsAffected()
	return int(rows), nil
}

func (r *DevMachineRepository) CreateAgentRun(ctx context.Context, run *domain.DevMachineAgentRun, operation *domain.DevMachineOperation) error {
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1::text, 0))`, run.WorkspaceID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1::text, 0))`, run.MachineID); err != nil {
		return err
	}
	var status, desiredStatus domain.DevMachineStatus
	var generation int64
	if err := tx.QueryRowContext(ctx, `SELECT status, desired_status, generation FROM dev_machines
		WHERE workspace_id=$1 AND id=$2 FOR UPDATE`, run.WorkspaceID, run.MachineID).
		Scan(&status, &desiredStatus, &generation); errors.Is(err, sql.ErrNoRows) {
		return ErrMachineStateConflict
	} else if err != nil {
		return err
	}
	if status != domain.DevMachineStatusRunning || desiredStatus != domain.DevMachineStatusRunning || generation != operation.Generation {
		return ErrMachineStateConflict
	}
	var maxDailyRuns int
	if err := tx.GetContext(ctx, &maxDailyRuns, `SELECT max_daily_agent_runs FROM dev_machine_workspace_policies
		WHERE workspace_id=$1 AND enabled`, run.WorkspaceID); err != nil {
		return err
	}
	var dailyRuns int
	if err := tx.GetContext(ctx, &dailyRuns, `SELECT COUNT(*) FROM dev_machine_agent_runs
		WHERE workspace_id=$1 AND created_at >= $2`, run.WorkspaceID, time.Now().UTC().Truncate(24*time.Hour)); err != nil {
		return err
	}
	if dailyRuns >= maxDailyRuns {
		return fmt.Errorf("dev machine quota exceeded: daily agent run limit reached")
	}
	var active bool
	if err := tx.GetContext(ctx, &active, `SELECT EXISTS(SELECT 1 FROM dev_machine_agent_runs
		WHERE machine_id=$1 AND status IN ('queued','starting','running','waiting_input'))`, run.MachineID); err != nil {
		return err
	}
	if active {
		return ErrActiveAgentRun
	}
	if err := tx.QueryRowContext(ctx, `INSERT INTO dev_machine_agent_runs
		(id, machine_id, workspace_id, issue_id, checkout_id, requested_by_user_id, provider_id, mode, status, prompt,
		 acceptance_criteria, allowed_commands, forbidden_paths, allowed_secrets, test_command, command_argv,
		 max_runtime_seconds, push_branch, open_pull_request)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19) RETURNING created_at`,
		run.ID, run.MachineID, run.WorkspaceID, run.IssueID, run.CheckoutID, run.RequestedByUserID, run.ProviderID,
		run.Mode, run.Status, run.Prompt, run.AcceptanceCriteria, run.AllowedCommands, run.ForbiddenPaths,
		run.AllowedSecrets, nullJSONPointer(run.TestCommand), run.CommandArgv, run.MaxRuntimeSeconds, run.PushBranch, run.OpenPullRequest,
	).Scan(&run.CreatedAt); err != nil {
		return err
	}
	operation.AgentRunID = &run.ID
	if err := enqueueOperation(ctx, tx, operation); err != nil {
		return err
	}
	return tx.Commit()
}

func nullJSON(raw []byte) any {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	return raw
}

func nullJSONPointer(raw *json.RawMessage) any {
	if raw == nil {
		return nil
	}
	return nullJSON(*raw)
}

func (r *DevMachineRepository) GetAgentRun(ctx context.Context, workspaceID, runID uuid.UUID) (*domain.DevMachineAgentRun, error) {
	var run domain.DevMachineAgentRun
	err := r.db.GetContext(ctx, &run, `SELECT * FROM dev_machine_agent_runs WHERE workspace_id=$1 AND id=$2`, workspaceID, runID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &run, err
}

func (r *DevMachineRepository) GetAgentRunForUser(ctx context.Context, workspaceID, runID, userID uuid.UUID) (*domain.DevMachineAgentRun, error) {
	var run domain.DevMachineAgentRun
	err := r.db.GetContext(ctx, &run, `SELECT r.* FROM dev_machine_agent_runs r
		JOIN dev_machines m ON m.id=r.machine_id
		WHERE r.workspace_id=$1 AND m.workspace_id=$1 AND r.id=$2 AND m.created_by_user_id=$3`, workspaceID, runID, userID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &run, err
}

func (r *DevMachineRepository) GetAgentRunInternal(ctx context.Context, runID uuid.UUID) (*domain.DevMachineAgentRun, error) {
	var run domain.DevMachineAgentRun
	err := r.db.GetContext(ctx, &run, `SELECT * FROM dev_machine_agent_runs WHERE id=$1`, runID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &run, err
}

func (r *DevMachineRepository) ListAgentRuns(ctx context.Context, workspaceID uuid.UUID, machineID *uuid.UUID, limit, offset int) ([]domain.DevMachineAgentRun, int, error) {
	where := `workspace_id=$1 AND ($2::uuid IS NULL OR machine_id=$2)`
	var total int
	if err := r.db.GetContext(ctx, &total, `SELECT COUNT(*) FROM dev_machine_agent_runs WHERE `+where, workspaceID, machineID); err != nil {
		return nil, 0, err
	}
	var runs []domain.DevMachineAgentRun
	err := r.db.SelectContext(ctx, &runs, `SELECT * FROM dev_machine_agent_runs WHERE `+where+` ORDER BY created_at DESC, id DESC LIMIT $3 OFFSET $4`, workspaceID, machineID, limit, offset)
	return runs, total, err
}

func (r *DevMachineRepository) ListAgentRunsForUser(ctx context.Context, workspaceID, userID uuid.UUID, machineID *uuid.UUID, limit, offset int) ([]domain.DevMachineAgentRun, int, error) {
	where := `r.workspace_id=$1 AND m.workspace_id=$1 AND m.created_by_user_id=$2 AND ($3::uuid IS NULL OR r.machine_id=$3)`
	var total int
	if err := r.db.GetContext(ctx, &total, `SELECT COUNT(*) FROM dev_machine_agent_runs r JOIN dev_machines m ON m.id=r.machine_id WHERE `+where, workspaceID, userID, machineID); err != nil {
		return nil, 0, err
	}
	var runs []domain.DevMachineAgentRun
	err := r.db.SelectContext(ctx, &runs, `SELECT r.* FROM dev_machine_agent_runs r JOIN dev_machines m ON m.id=r.machine_id WHERE `+where+` ORDER BY r.created_at DESC, r.id DESC LIMIT $4 OFFSET $5`, workspaceID, userID, machineID, limit, offset)
	if runs == nil {
		runs = []domain.DevMachineAgentRun{}
	}
	return runs, total, err
}

func (r *DevMachineRepository) CountAgentRunsSince(ctx context.Context, workspaceID uuid.UUID, since time.Time) (int, error) {
	var count int
	err := r.db.GetContext(ctx, &count, `SELECT COUNT(*) FROM dev_machine_agent_runs WHERE workspace_id=$1 AND created_at >= $2`, workspaceID, since)
	return count, err
}

func (r *DevMachineRepository) HasActiveAgentRun(ctx context.Context, machineID uuid.UUID) (bool, error) {
	var active bool
	err := r.db.GetContext(ctx, &active, `SELECT EXISTS(SELECT 1 FROM dev_machine_agent_runs
		WHERE machine_id=$1 AND status IN ('queued','starting','running','waiting_input'))`, machineID)
	return active, err
}

func (r *DevMachineRepository) CancelAgentRun(ctx context.Context, workspaceID, runID uuid.UUID, operation *domain.DevMachineOperation) error {
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE dev_machine_agent_runs SET status='cancelled', cancelled_at=NOW(), completed_at=NOW()
		WHERE workspace_id=$1 AND id=$2 AND status IN ('queued','starting','running','waiting_input')`, workspaceID, runID)
	if err != nil {
		return err
	}
	if rows, _ := result.RowsAffected(); rows != 1 {
		var status domain.DevMachineAgentRunStatus
		if err := tx.GetContext(ctx, &status, `SELECT status FROM dev_machine_agent_runs WHERE workspace_id=$1 AND id=$2`, workspaceID, runID); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return sql.ErrNoRows
			}
			return err
		}
		if status == domain.DevMachineAgentRunStatusSucceeded || status == domain.DevMachineAgentRunStatusFailed || status == domain.DevMachineAgentRunStatusCancelled || status == domain.DevMachineAgentRunStatusTimeout {
			return tx.Commit()
		}
		return sql.ErrNoRows
	}
	operation.AgentRunID = &runID
	if err := enqueueOperation(ctx, tx, operation); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *DevMachineRepository) UpdateAgentRunStarted(ctx context.Context, runID uuid.UUID) error {
	result, err := r.db.ExecContext(ctx, `UPDATE dev_machine_agent_runs SET status='running', started_at=COALESCE(started_at,NOW())
		WHERE id=$1 AND status IN ('queued','starting')`, runID)
	if err != nil {
		return err
	}
	if rows, _ := result.RowsAffected(); rows != 1 {
		return sql.ErrNoRows
	}
	return nil
}

func (r *DevMachineRepository) CompleteAgentRun(ctx context.Context, run *domain.DevMachineAgentRun) error {
	_, err := r.db.ExecContext(ctx, `UPDATE dev_machine_agent_runs SET status=$1, result=$2, summary=$3,
		changed_files=$4, commits=$5, branch=$6, pull_request_url=$7, tests_run=$8, test_status=$9,
		risk_notes=$10, exit_code=$11, error_message=$12, completed_at=NOW()
		WHERE id=$13 AND (status NOT IN ('succeeded','failed','cancelled','timeout') OR status=$1)`,
		run.Status, nullJSONPointer(run.Result), run.Summary, run.ChangedFiles, run.Commits, run.Branch,
		run.PullRequestURL, run.TestsRun, run.TestStatus, run.RiskNotes, run.ExitCode, run.ErrorMessage, run.ID)
	return err
}

func (r *DevMachineRepository) CreateEvent(ctx context.Context, event *domain.DevMachineEvent) error {
	if len(event.Payload) == 0 {
		event.Payload = []byte(`{}`)
	}
	if event.OccurredAt.IsZero() {
		event.OccurredAt = time.Now().UTC()
	}
	return r.db.QueryRowContext(ctx, `INSERT INTO dev_machine_events
		(workspace_id, machine_id, agent_run_id, actor_user_id, source, event_type, payload, occurred_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8) RETURNING id, created_at`, event.WorkspaceID, event.MachineID,
		event.AgentRunID, event.ActorUserID, event.Source, event.EventType, event.Payload, event.OccurredAt,
	).Scan(&event.ID, &event.CreatedAt)
}

func (r *DevMachineRepository) AuthenticateMachineToken(ctx context.Context, tokenHash, scope string) (*domain.DevMachineToken, *domain.DevMachine, error) {
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return nil, nil, err
	}
	defer tx.Rollback()
	var token domain.DevMachineToken
	err = tx.GetContext(ctx, &token, `UPDATE dev_machine_tokens SET last_used_at=NOW()
		WHERE token_hash=$1 AND revoked_at IS NULL AND expires_at>NOW() AND scopes ? $2 RETURNING *`, tokenHash, scope)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	var machine domain.DevMachine
	if err := tx.GetContext(ctx, &machine, `SELECT * FROM dev_machines WHERE id=$1 AND status='running' AND expires_at>NOW()`, token.MachineID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil, nil
		}
		return nil, nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, nil, err
	}
	return &token, &machine, nil
}

func (r *DevMachineRepository) ListEvents(ctx context.Context, workspaceID, machineID uuid.UUID, afterID int64, limit int) ([]domain.DevMachineEvent, error) {
	var events []domain.DevMachineEvent
	err := r.db.SelectContext(ctx, &events, `SELECT e.* FROM dev_machine_events e JOIN dev_machines m ON m.id=e.machine_id
		WHERE m.workspace_id=$1 AND m.id=$2 AND ($3=0 OR e.id>$3) ORDER BY e.id ASC LIMIT $4`, workspaceID, machineID, afterID, limit)
	return events, err
}

func (r *DevMachineRepository) CreateLogChunk(ctx context.Context, chunk *domain.DevMachineLogChunk) error {
	return r.db.QueryRowContext(ctx, `INSERT INTO dev_machine_log_chunks
		(workspace_id, machine_id, agent_run_id, service_id, stream, sequence, content, truncated)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
		ON CONFLICT (machine_id, agent_run_id, service_id, stream, sequence) DO UPDATE SET id=dev_machine_log_chunks.id RETURNING id, created_at`,
		chunk.WorkspaceID, chunk.MachineID, chunk.AgentRunID, chunk.ServiceID, chunk.Stream,
		chunk.Sequence, chunk.Content, chunk.Truncated,
	).Scan(&chunk.ID, &chunk.CreatedAt)
}

func (r *DevMachineRepository) ListLogs(ctx context.Context, workspaceID, machineID uuid.UUID, runID *uuid.UUID, afterID int64, limit int) ([]domain.DevMachineLogChunk, error) {
	var chunks []domain.DevMachineLogChunk
	err := r.db.SelectContext(ctx, &chunks, `SELECT l.* FROM dev_machine_log_chunks l JOIN dev_machines m ON m.id=l.machine_id
		WHERE m.workspace_id=$1 AND m.id=$2 AND ($3::uuid IS NULL OR l.agent_run_id=$3)
		AND ($4=0 OR l.id>$4) ORDER BY l.id ASC LIMIT $5`, workspaceID, machineID, runID, afterID, limit)
	return chunks, err
}

func (r *DevMachineRepository) CreateAccessTicket(ctx context.Context, ticket *domain.DevMachineAccessTicket) error {
	return r.db.QueryRowContext(ctx, `INSERT INTO dev_machine_access_tickets
		(id, workspace_id, machine_id, service_id, user_id, token_hash, status, bound_host, expires_at, terminal_session_id)
		SELECT $1, m.workspace_id, m.id, s.id, $5, $6, $7, $8, $9, $10
		FROM dev_machines m
		JOIN dev_machine_services s ON s.id=$4 AND s.machine_id=m.id
		JOIN workspace_members wm ON wm.workspace_id=m.workspace_id AND wm.user_id=$5 AND wm.role IN ('owner','admin','member')
		WHERE m.workspace_id=$2 AND m.id=$3 AND m.created_by_user_id=$5
		AND m.status='running' AND m.desired_status='running' AND m.expires_at>NOW()
		AND s.status='running' AND $9>NOW() AND $9<=m.expires_at
		AND ($10::uuid IS NULL OR (s.service_type='terminal' AND EXISTS (
			SELECT 1 FROM dev_machine_terminal_sessions ts
			WHERE ts.id=$10 AND ts.workspace_id=m.workspace_id AND ts.machine_id=m.id
			AND ts.user_id=$5 AND ts.status='active'
		)))
		RETURNING created_at`, ticket.ID, ticket.WorkspaceID,
		ticket.MachineID, ticket.ServiceID, ticket.UserID, ticket.TokenHash, ticket.Status, ticket.BoundHost, ticket.ExpiresAt, ticket.TerminalSessionID,
	).Scan(&ticket.CreatedAt)
}

func (r *DevMachineRepository) ConsumeAccessTicket(ctx context.Context, tokenHash, host string) (*domain.DevMachineAccessTicket, error) {
	var ticket domain.DevMachineAccessTicket
	err := r.db.GetContext(ctx, &ticket, `UPDATE dev_machine_access_tickets t SET status='used', used_at=NOW()
		FROM dev_machines m, dev_machine_services s, workspace_members wm
		WHERE t.token_hash=$1 AND t.bound_host=$2 AND t.status='active' AND t.expires_at>NOW()
		AND m.id=t.machine_id AND t.workspace_id=m.workspace_id AND m.created_by_user_id=t.user_id
		AND m.status='running' AND m.desired_status='running' AND m.expires_at>NOW()
		AND s.id=t.service_id AND s.machine_id=m.id AND s.status='running'
		AND wm.workspace_id=m.workspace_id AND wm.user_id=t.user_id AND wm.role IN ('owner','admin','member')
		AND (t.terminal_session_id IS NULL OR EXISTS (
			SELECT 1 FROM dev_machine_terminal_sessions ts
			WHERE ts.id=t.terminal_session_id AND ts.workspace_id=t.workspace_id
			AND ts.machine_id=t.machine_id AND ts.user_id=t.user_id AND ts.status='active'
		))
		RETURNING t.*`, tokenHash, host)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &ticket, err
}

func (r *DevMachineRepository) CreateAccessSession(ctx context.Context, session *domain.DevMachineAccessSession) error {
	return r.db.QueryRowContext(ctx, `INSERT INTO dev_machine_access_sessions
		(id, workspace_id, machine_id, service_id, user_id, token_hash, bound_host, expires_at)
		SELECT $1, m.workspace_id, m.id, s.id, $5, $6, $7, $8
		FROM dev_machines m
		JOIN dev_machine_services s ON s.id=$4 AND s.machine_id=m.id
		JOIN workspace_members wm ON wm.workspace_id=m.workspace_id AND wm.user_id=$5 AND wm.role IN ('owner','admin','member')
		WHERE m.workspace_id=$2 AND m.id=$3 AND m.created_by_user_id=$5
		AND m.status='running' AND m.desired_status='running' AND m.expires_at>NOW()
		AND s.status='running' AND $8>NOW() AND $8<=m.expires_at
		RETURNING created_at, last_seen_at`, session.ID, session.WorkspaceID,
		session.MachineID, session.ServiceID, session.UserID, session.TokenHash, session.BoundHost, session.ExpiresAt,
	).Scan(&session.CreatedAt, &session.LastSeenAt)
}

func (r *DevMachineRepository) GetAccessSession(ctx context.Context, tokenHash, host string) (*domain.DevMachineAccessSession, error) {
	var session domain.DevMachineAccessSession
	err := r.db.GetContext(ctx, &session, `UPDATE dev_machine_access_sessions a SET last_seen_at=NOW()
		FROM dev_machines m, dev_machine_services s, workspace_members wm
		WHERE a.token_hash=$1 AND a.bound_host=$2 AND a.revoked_at IS NULL AND a.expires_at>NOW()
		AND m.id=a.machine_id AND a.workspace_id=m.workspace_id AND m.created_by_user_id=a.user_id
		AND m.status='running' AND m.desired_status='running' AND m.expires_at>NOW()
		AND s.id=a.service_id AND s.machine_id=m.id AND s.status='running'
		AND wm.workspace_id=m.workspace_id AND wm.user_id=a.user_id AND wm.role IN ('owner','admin','member')
		RETURNING a.*`, tokenHash, host)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &session, err
}

func (r *DevMachineRepository) RevokeMachineAccess(ctx context.Context, machineID uuid.UUID) error {
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE dev_machine_access_tickets SET status='revoked', revoked_at=NOW()
		WHERE machine_id=$1 AND status='active'`, machineID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE dev_machine_access_sessions SET revoked_at=NOW()
		WHERE machine_id=$1 AND revoked_at IS NULL`, machineID); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *DevMachineRepository) CreateAccessLog(ctx context.Context, accessLog *domain.DevMachineAccessLog) error {
	return r.db.QueryRowContext(ctx, `INSERT INTO dev_machine_access_logs
		(workspace_id, machine_id, service_id, user_id, decision, reason, method, path, response_status, remote_ip, user_agent)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11) RETURNING id, created_at`, accessLog.WorkspaceID,
		accessLog.MachineID, accessLog.ServiceID, accessLog.UserID, accessLog.Decision, accessLog.Reason,
		accessLog.Method, accessLog.Path, accessLog.ResponseStatus, accessLog.RemoteIP, accessLog.UserAgent,
	).Scan(&accessLog.ID, &accessLog.CreatedAt)
}

func (r *DevMachineRepository) CreateResourceSample(ctx context.Context, sample *domain.DevMachineResourceSample) error {
	return r.db.QueryRowContext(ctx, `INSERT INTO dev_machine_resource_samples
		(machine_id, cpu_percent, memory_bytes, disk_bytes, pids, network_rx_bytes, network_tx_bytes)
		VALUES ($1,$2,$3,$4,$5,$6,$7) RETURNING id, created_at`, sample.MachineID, sample.CPUPercent,
		sample.MemoryBytes, sample.DiskBytes, sample.Pids, sample.NetworkRxBytes, sample.NetworkTxBytes,
	).Scan(&sample.ID, &sample.CreatedAt)
}

func (r *DevMachineRepository) UpdateVolumeUsage(ctx context.Context, machineID uuid.UUID, size int64) error {
	_, err := r.db.ExecContext(ctx, `UPDATE dev_machine_volumes SET current_size_bytes=$1
		WHERE machine_id=$2 AND volume_type='workspace' AND deleted_at IS NULL`, size, machineID)
	return err
}

func (r *DevMachineRepository) CreateGitRef(ctx context.Context, ref *domain.DevMachineGitRef) error {
	return r.db.QueryRowContext(ctx, `INSERT INTO dev_machine_git_refs
		(workspace_id, machine_id, agent_run_id, issue_id, ref_type, repository_full_name, ref_name,
		 commit_sha, pull_request_number, url)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) RETURNING id, created_at`, ref.WorkspaceID,
		ref.MachineID, ref.AgentRunID, ref.IssueID, ref.RefType, ref.RepositoryFullName, ref.RefName,
		ref.CommitSHA, ref.PullRequestNumber, ref.URL,
	).Scan(&ref.ID, &ref.CreatedAt)
}

func (r *DevMachineRepository) GetGitHubInstallationID(ctx context.Context, workspaceID uuid.UUID, fullName string) (int64, error) {
	var installationID int64
	err := r.db.GetContext(ctx, &installationID, `SELECT gi.installation_id FROM github_installations gi
		JOIN github_repos gr ON gr.installation_id=gi.id
		WHERE gi.workspace_id=$1 AND LOWER(gr.full_name)=LOWER($2) AND gr.is_active`, workspaceID, fullName)
	return installationID, err
}

func (r *DevMachineRepository) GetGitHubAppConfig(ctx context.Context, workspaceID uuid.UUID) (*domain.GitHubAppConfig, error) {
	var config domain.GitHubAppConfig
	err := r.db.GetContext(ctx, &config, `SELECT * FROM github_app_configs WHERE workspace_id=$1`, workspaceID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &config, err
}

func (r *DevMachineRepository) ListResourceSamples(ctx context.Context, workspaceID, machineID uuid.UUID, limit int) ([]domain.DevMachineResourceSample, error) {
	var samples []domain.DevMachineResourceSample
	err := r.db.SelectContext(ctx, &samples, `SELECT s.* FROM dev_machine_resource_samples s JOIN dev_machines m ON m.id=s.machine_id
		WHERE m.workspace_id=$1 AND m.id=$2 ORDER BY s.created_at DESC LIMIT $3`, workspaceID, machineID, limit)
	return samples, err
}

func (r *DevMachineRepository) MachineNameExists(ctx context.Context, workspaceID uuid.UUID, name string) (bool, error) {
	var exists bool
	err := r.db.GetContext(ctx, &exists, `SELECT EXISTS(
		SELECT 1 FROM dev_machines WHERE workspace_id=$1 AND LOWER(name)=LOWER($2)
	)`, workspaceID, name)
	return exists, err
}

func (r *DevMachineRepository) MachineNameExistsForUser(ctx context.Context, workspaceID, userID uuid.UUID, name string) (bool, error) {
	var exists bool
	err := r.db.GetContext(ctx, &exists, `SELECT EXISTS(
		SELECT 1 FROM dev_machines WHERE workspace_id=$1 AND created_by_user_id=$2 AND LOWER(name)=LOWER($3)
	)`, workspaceID, userID, name)
	return exists, err
}

func (r *DevMachineRepository) UpdateMachinePreferences(ctx context.Context, workspaceID, machineID uuid.UUID, keepRunning *bool) (*domain.DevMachine, error) {
	return r.updateMachinePreferences(ctx, workspaceID, machineID, nil, keepRunning)
}

func (r *DevMachineRepository) UpdateMachinePreferencesForUser(ctx context.Context, workspaceID, machineID, userID uuid.UUID, keepRunning *bool) (*domain.DevMachine, error) {
	return r.updateMachinePreferences(ctx, workspaceID, machineID, &userID, keepRunning)
}

func (r *DevMachineRepository) updateMachinePreferences(ctx context.Context, workspaceID, machineID uuid.UUID, userID *uuid.UUID, keepRunning *bool) (*domain.DevMachine, error) {
	var machine domain.DevMachine
	var err error
	if keepRunning == nil {
		err = r.db.GetContext(ctx, &machine, `SELECT * FROM dev_machines
			WHERE workspace_id=$1 AND id=$2 AND ($3::uuid IS NULL OR created_by_user_id=$3)
			AND delete_requested_at IS NULL
			AND status NOT IN ('tearing_down','destroyed','expired')
			AND desired_status NOT IN ('tearing_down','destroyed','expired')`, workspaceID, machineID, userID)
	} else {
		err = r.db.GetContext(ctx, &machine, `UPDATE dev_machines SET keep_running=$1
			WHERE workspace_id=$2 AND id=$3 AND ($4::uuid IS NULL OR created_by_user_id=$4)
			AND delete_requested_at IS NULL
			AND status NOT IN ('tearing_down','destroyed','expired')
			AND desired_status NOT IN ('tearing_down','destroyed','expired')
			RETURNING *`, *keepRunning, workspaceID, machineID, userID)
	}
	if err == nil {
		return &machine, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	var existing *domain.DevMachine
	if userID == nil {
		existing, err = r.GetMachine(ctx, workspaceID, machineID)
	} else {
		existing, err = r.GetMachineForUser(ctx, workspaceID, machineID, *userID)
	}
	if err != nil || existing == nil {
		return existing, err
	}
	return nil, ErrMachineStateConflict
}

func (r *DevMachineRepository) TouchMachineActivity(ctx context.Context, machineID uuid.UUID, at time.Time) error {
	_, err := r.db.ExecContext(ctx, `UPDATE dev_machines SET last_activity_at=GREATEST(COALESCE(last_activity_at,$2),$2) WHERE id=$1`, machineID, at.UTC())
	return err
}

func (r *DevMachineRepository) RequestPermanentDelete(ctx context.Context, workspaceID, machineID uuid.UUID, requestedByUserID *uuid.UUID) (*domain.DevMachineOperation, error) {
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1::text, 0))`, workspaceID); err != nil {
		return nil, err
	}
	var machine domain.DevMachine
	if err := tx.GetContext(ctx, &machine, `SELECT * FROM dev_machines WHERE workspace_id=$1 AND id=$2 FOR UPDATE`, workspaceID, machineID); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE dev_machines
		SET delete_requested_at=COALESCE(delete_requested_at,NOW())
		WHERE workspace_id=$1 AND id=$2`, workspaceID, machineID); err != nil {
		return nil, err
	}
	var active domain.DevMachineOperation
	err = tx.GetContext(ctx, &active, `SELECT * FROM dev_machine_operations
		WHERE workspace_id=$1 AND machine_id=$2 AND action='teardown' AND status IN ('pending','leased')
		ORDER BY generation DESC, created_at LIMIT 1`, workspaceID, machineID)
	if err == nil {
		if machine.DesiredStatus != domain.DevMachineStatusDestroyed {
			if _, err := tx.ExecContext(ctx, `UPDATE dev_machines
				SET desired_status='destroyed', generation=GREATEST(generation,$3)
				WHERE workspace_id=$1 AND id=$2`, workspaceID, machineID, active.Generation); err != nil {
				return nil, err
			}
		}
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		return &active, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}

	var hasActiveOperation bool
	if err := tx.GetContext(ctx, &hasActiveOperation, `SELECT EXISTS(SELECT 1 FROM dev_machine_operations
		WHERE workspace_id=$1 AND machine_id=$2 AND status IN ('pending','leased'))`, workspaceID, machineID); err != nil {
		return nil, err
	}
	if domain.DevMachineSafelyPurgeable(&machine) && !hasActiveOperation {
		return nil, tx.Commit()
	}

	generation := machine.Generation + 1
	operation := &domain.DevMachineOperation{
		ID: uuid.New(), MachineID: machine.ID, WorkspaceID: workspaceID,
		Action: domain.DevMachineOpTeardown, Status: domain.DevMachineOpStatusPending,
		Generation: generation, IdempotencyKey: fmt.Sprintf("permanent-delete:%d", generation),
		RequestedByUserID: requestedByUserID, MaxAttempts: 10,
	}

	var existing domain.DevMachineOperation
	err = tx.GetContext(ctx, &existing, `SELECT * FROM dev_machine_operations WHERE workspace_id=$1 AND machine_id=$2 AND idempotency_key=$3`, workspaceID, machineID, operation.IdempotencyKey)
	if err == nil {
		if existing.Action != operation.Action || !sameOptionalUUID(existing.RequestedByUserID, operation.RequestedByUserID) {
			return nil, ErrIdempotencyKeyConflict
		}
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		return &existing, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}

	result, err := tx.ExecContext(ctx, `UPDATE dev_machines SET desired_status=$1, generation=$2
		WHERE workspace_id=$3 AND id=$4 AND generation<$2`, domain.DevMachineStatusDestroyed, operation.Generation, workspaceID, machineID)
	if err != nil {
		return nil, err
	}
	if rows, _ := result.RowsAffected(); rows != 1 {
		return nil, sql.ErrNoRows
	}
	if err := enqueueOperation(ctx, tx, operation); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return operation, nil
}

func (r *DevMachineRepository) PurgePermanentDeleteRequest(ctx context.Context, workspaceID, machineID uuid.UUID) error {
	result, err := r.db.ExecContext(ctx, `DELETE FROM dev_machines m
		WHERE m.workspace_id=$1 AND m.id=$2
		AND (
			m.status='destroyed'
			OR (m.status IN ('expired','failed') AND m.docker_network_name IS NULL AND m.workspace_volume_name IS NULL)
		)
		AND NOT EXISTS (SELECT 1 FROM dev_machine_operations o WHERE o.machine_id=m.id AND o.status IN ('pending','leased'))`, workspaceID, machineID)
	if err != nil {
		return err
	}
	if rows, _ := result.RowsAffected(); rows != 1 {
		return sql.ErrNoRows
	}
	return nil
}

func (r *DevMachineRepository) ListPermanentDeleteRequests(ctx context.Context, limit int) ([]domain.DevMachine, error) {
	var machines []domain.DevMachine
	err := r.db.SelectContext(ctx, &machines, `SELECT * FROM dev_machines
		WHERE delete_requested_at IS NOT NULL
		ORDER BY delete_requested_at, updated_at LIMIT $1`, limit)
	return machines, err
}

func (r *DevMachineRepository) BulkPurgeMachines(ctx context.Context, workspaceID uuid.UUID, machineIDs []uuid.UUID, olderThan time.Time, includeFailed, includeExpired bool) (int, error) {
	statuses := []string{"destroyed"}
	if includeExpired {
		statuses = append(statuses, "expired")
	}
	if includeFailed {
		statuses = append(statuses, "failed")
	}
	query := `DELETE FROM dev_machines m WHERE m.workspace_id=? AND m.status IN (?)
		AND COALESCE(m.destroyed_at, m.delete_requested_at, m.updated_at, m.created_at) < ?
		AND NOT EXISTS (SELECT 1 FROM dev_machine_operations o WHERE o.machine_id=m.id AND o.status IN ('pending','leased'))
		AND (m.status='destroyed' OR (m.docker_network_name IS NULL AND m.workspace_volume_name IS NULL))`
	args := []any{workspaceID, statuses, olderThan.UTC()}
	if len(machineIDs) > 0 {
		query += ` AND m.id IN (?)`
		args = append(args, machineIDs)
	}
	query, args, err := sqlx.In(query, args...)
	if err != nil {
		return 0, err
	}
	result, err := r.db.ExecContext(ctx, r.db.Rebind(query), args...)
	if err != nil {
		return 0, err
	}
	rows, _ := result.RowsAffected()
	return int(rows), nil
}

func (r *DevMachineRepository) ListIdleMachines(ctx context.Context, limit int) ([]domain.DevMachine, error) {
	var machines []domain.DevMachine
	err := r.db.SelectContext(ctx, &machines, `SELECT m.* FROM dev_machines m
		JOIN dev_machine_workspace_policies p ON p.workspace_id=m.workspace_id AND p.enabled
		WHERE m.status='running' AND m.desired_status='running' AND NOT m.keep_running
		AND COALESCE(m.last_activity_at,m.started_at,m.created_at) + make_interval(mins => p.idle_pause_minutes) <= NOW()
		AND NOT EXISTS (SELECT 1 FROM dev_machine_agent_runs r WHERE r.machine_id=m.id
			AND r.status IN ('queued','starting','running','waiting_input'))
		AND NOT EXISTS (SELECT 1 FROM dev_machine_operations o WHERE o.machine_id=m.id
			AND o.status IN ('pending','leased'))
		ORDER BY COALESCE(m.last_activity_at,m.started_at,m.created_at) LIMIT $1`, limit)
	return machines, err
}

func (r *DevMachineRepository) ListScopeSettings(ctx context.Context, workspaceID uuid.UUID) ([]domain.DevMachineScopeSetting, error) {
	var settings []domain.DevMachineScopeSetting
	err := r.db.SelectContext(ctx, &settings, `SELECT * FROM dev_machine_scope_settings WHERE workspace_id=$1
		ORDER BY CASE
			WHEN issue_id IS NOT NULL THEN 0
			WHEN project_id IS NOT NULL THEN 1
			WHEN team_id IS NOT NULL THEN 2
			ELSE 3
		END, created_at DESC`, workspaceID)
	if settings == nil {
		settings = []domain.DevMachineScopeSetting{}
	}
	return settings, err
}

func (r *DevMachineRepository) GetScopeSetting(ctx context.Context, workspaceID uuid.UUID, teamID, projectID, issueID *uuid.UUID) (*domain.DevMachineScopeSetting, error) {
	var setting domain.DevMachineScopeSetting
	err := r.db.GetContext(ctx, &setting, `SELECT * FROM dev_machine_scope_settings
		WHERE workspace_id=$1 AND team_id IS NOT DISTINCT FROM $2 AND project_id IS NOT DISTINCT FROM $3
		AND issue_id IS NOT DISTINCT FROM $4`, workspaceID, teamID, projectID, issueID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &setting, err
}

func (r *DevMachineRepository) UpsertScopeSetting(ctx context.Context, setting *domain.DevMachineScopeSetting) error {
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1::text, 0))`, setting.WorkspaceID); err != nil {
		return err
	}
	if setting.EnvironmentID != nil {
		var ready bool
		if err := tx.GetContext(ctx, &ready, `SELECT EXISTS(SELECT 1 FROM dev_machine_environments
			WHERE workspace_id=$1 AND id=$2 AND status='ready')`, setting.WorkspaceID, setting.EnvironmentID); err != nil {
			return err
		}
		if !ready {
			return ErrEnvironmentUnavailable
		}
	}
	if setting.ID == uuid.Nil {
		setting.ID = uuid.New()
	}
	conflictTarget := ""
	switch {
	case setting.TeamID == nil && setting.ProjectID == nil && setting.IssueID == nil:
		conflictTarget = `(workspace_id) WHERE team_id IS NULL AND project_id IS NULL AND issue_id IS NULL`
	case setting.TeamID != nil && setting.ProjectID == nil && setting.IssueID == nil:
		conflictTarget = `(team_id) WHERE team_id IS NOT NULL`
	case setting.TeamID == nil && setting.ProjectID != nil && setting.IssueID == nil:
		conflictTarget = `(project_id) WHERE project_id IS NOT NULL`
	case setting.TeamID == nil && setting.ProjectID == nil && setting.IssueID != nil:
		conflictTarget = `(issue_id) WHERE issue_id IS NOT NULL`
	default:
		return errors.New("invalid development setting scope")
	}
	query := `INSERT INTO dev_machine_scope_settings
		(id,workspace_id,team_id,project_id,issue_id,github_repo_id,base_branch,environment_id)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
		ON CONFLICT ` + conflictTarget + ` DO UPDATE SET
			github_repo_id=EXCLUDED.github_repo_id,base_branch=EXCLUDED.base_branch,
			environment_id=EXCLUDED.environment_id,updated_at=NOW()
		WHERE dev_machine_scope_settings.workspace_id=EXCLUDED.workspace_id
		RETURNING id,created_at,updated_at`
	if err := tx.QueryRowContext(ctx, query, setting.ID, setting.WorkspaceID,
		setting.TeamID, setting.ProjectID, setting.IssueID, setting.GitHubRepoID, setting.BaseBranch, setting.EnvironmentID,
	).Scan(&setting.ID, &setting.CreatedAt, &setting.UpdatedAt); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *DevMachineRepository) DeleteScopeSetting(ctx context.Context, workspaceID uuid.UUID, teamID, projectID, issueID *uuid.UUID) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM dev_machine_scope_settings
		WHERE workspace_id=$1 AND team_id IS NOT DISTINCT FROM $2 AND project_id IS NOT DISTINCT FROM $3
		AND issue_id IS NOT DISTINCT FROM $4`, workspaceID, teamID, projectID, issueID)
	if err != nil {
		return err
	}
	return nil
}

func (r *DevMachineRepository) ScopeResourceExists(ctx context.Context, workspaceID uuid.UUID, scopeType string, scopeID *uuid.UUID) (bool, error) {
	if scopeType == "workspace" {
		return scopeID == nil, nil
	}
	if scopeID == nil {
		return false, nil
	}
	table := map[string]string{"team": "teams", "project": "projects", "issue": "issues"}[scopeType]
	if table == "" {
		return false, nil
	}
	var exists bool
	err := r.db.GetContext(ctx, &exists, `SELECT EXISTS(SELECT 1 FROM `+table+` WHERE id=$1 AND workspace_id=$2)`, *scopeID, workspaceID)
	return exists, err
}

func (r *DevMachineRepository) GetLinkedRepository(ctx context.Context, workspaceID, repositoryID uuid.UUID) (*domain.GitHubRepoModel, error) {
	var repo domain.GitHubRepoModel
	err := r.db.GetContext(ctx, &repo, `SELECT * FROM github_repos WHERE workspace_id=$1 AND id=$2 AND is_active`, workspaceID, repositoryID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &repo, err
}

func (r *DevMachineRepository) GetLinkedRepositoryByFullName(ctx context.Context, workspaceID uuid.UUID, fullName string) (*domain.GitHubRepoModel, error) {
	var repo domain.GitHubRepoModel
	err := r.db.GetContext(ctx, &repo, `SELECT * FROM github_repos WHERE workspace_id=$1 AND LOWER(full_name)=LOWER($2) AND is_active`, workspaceID, fullName)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &repo, err
}

func (r *DevMachineRepository) GetIssueDevelopmentContext(ctx context.Context, workspaceID, issueID uuid.UUID) (*domain.Issue, error) {
	var issue domain.Issue
	err := r.db.GetContext(ctx, &issue, `SELECT * FROM issues WHERE workspace_id=$1 AND id=$2`, workspaceID, issueID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &issue, err
}

func (r *DevMachineRepository) GetProjectDevelopmentContext(ctx context.Context, workspaceID, projectID uuid.UUID) (*domain.Project, error) {
	var project domain.Project
	err := r.db.GetContext(ctx, &project, `SELECT * FROM projects WHERE workspace_id=$1 AND id=$2`, workspaceID, projectID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &project, err
}

func (r *DevMachineRepository) ListEnvironments(ctx context.Context, workspaceID uuid.UUID) ([]domain.DevMachineEnvironment, error) {
	var environments []domain.DevMachineEnvironment
	err := r.db.SelectContext(ctx, &environments, `SELECT * FROM dev_machine_environments WHERE workspace_id=$1 AND status <> 'delete_requested' ORDER BY created_at DESC`, workspaceID)
	if environments == nil {
		environments = []domain.DevMachineEnvironment{}
	}
	return environments, err
}

func (r *DevMachineRepository) GetEnvironment(ctx context.Context, workspaceID, environmentID uuid.UUID) (*domain.DevMachineEnvironment, error) {
	var environment domain.DevMachineEnvironment
	err := r.db.GetContext(ctx, &environment, `SELECT * FROM dev_machine_environments WHERE workspace_id=$1 AND id=$2`, workspaceID, environmentID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &environment, err
}

func (r *DevMachineRepository) CreateEnvironment(ctx context.Context, environment *domain.DevMachineEnvironment, operation *domain.DevMachineOperation) error {
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1::text, 0))`, environment.WorkspaceID); err != nil {
		return err
	}
	if environment.SourceMachineID == nil || operation.MachineID != *environment.SourceMachineID || operation.WorkspaceID != environment.WorkspaceID ||
		operation.Action != domain.DevMachineOpSnapshotEnvironment || operation.EnvironmentID == nil || *operation.EnvironmentID != environment.ID {
		return ErrMachineStateConflict
	}
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1::text, 0))`, environment.SourceMachineID); err != nil {
		return err
	}
	var status, desiredStatus domain.DevMachineStatus
	var generation int64
	var environmentBuilder, lifecycleActive bool
	if err := tx.QueryRowContext(ctx, `SELECT status,desired_status,generation,environment_builder FROM dev_machines
		WHERE workspace_id=$1 AND id=$2 FOR UPDATE`, environment.WorkspaceID, environment.SourceMachineID).
		Scan(&status, &desiredStatus, &generation, &environmentBuilder); errors.Is(err, sql.ErrNoRows) {
		return ErrMachineStateConflict
	} else if err != nil {
		return err
	}
	stable := status == desiredStatus && (status == domain.DevMachineStatusPaused || status == domain.DevMachineStatusStopped)
	if !environmentBuilder || !stable || generation != operation.Generation {
		return ErrMachineStateConflict
	}
	if err := tx.GetContext(ctx, &lifecycleActive, `SELECT EXISTS(SELECT 1 FROM dev_machine_operations
		WHERE machine_id=$1 AND status IN ('pending','leased')
		AND action IN ('spawn','start','stop','pause','teardown','reconcile'))`, environment.SourceMachineID); err != nil {
		return err
	}
	if lifecycleActive {
		return ErrMachineStateConflict
	}
	if err := tx.QueryRowContext(ctx, `INSERT INTO dev_machine_environments
		(id,workspace_id,name,image_ref,status,source_machine_id,created_by_user_id)
		VALUES ($1,$2,$3,$4,$5,$6,$7) RETURNING created_at,updated_at`, environment.ID, environment.WorkspaceID,
		environment.Name, environment.ImageRef, environment.Status, environment.SourceMachineID, environment.CreatedByUserID,
	).Scan(&environment.CreatedAt, &environment.UpdatedAt); err != nil {
		return err
	}
	if err := enqueueOperation(ctx, tx, operation); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *DevMachineRepository) RequestEnvironmentDeletion(ctx context.Context, workspaceID, environmentID uuid.UUID) error {
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1::text, 0))`, workspaceID); err != nil {
		return err
	}
	var status string
	if err := tx.GetContext(ctx, &status, `SELECT status FROM dev_machine_environments
		WHERE workspace_id=$1 AND id=$2 FOR UPDATE`, workspaceID, environmentID); err != nil {
		return err
	}
	if status == "delete_requested" {
		return tx.Commit()
	}
	if status != "pending" && status != "building" && status != "ready" && status != "failed" {
		return ErrEnvironmentInvalidState
	}
	var inUse bool
	if err := tx.GetContext(ctx, &inUse, `SELECT
		EXISTS(SELECT 1 FROM dev_machine_scope_settings WHERE environment_id=$1)
		OR EXISTS(SELECT 1 FROM dev_machines m WHERE m.environment_id=$1 AND (
			m.status<>'destroyed' OR m.desired_status='running' OR EXISTS(
				SELECT 1 FROM dev_machine_operations o WHERE o.machine_id=m.id
				AND o.status IN ('pending','leased') AND o.action IN ('spawn','start','reconcile','snapshot_environment')
			)
		))`, environmentID); err != nil {
		return err
	}
	if inUse {
		return ErrEnvironmentInUse
	}
	var activeBuild bool
	if err := tx.GetContext(ctx, &activeBuild, `SELECT EXISTS(SELECT 1 FROM dev_machine_operations
		WHERE environment_id=$1 AND action='snapshot_environment' AND status='leased'
		AND (lease_expires_at IS NULL OR lease_expires_at>=NOW()))`, environmentID); err != nil {
		return err
	}
	if activeBuild {
		return ErrEnvironmentDeletionConflict
	}
	if _, err := tx.ExecContext(ctx, `UPDATE dev_machine_operations SET status='cancelled',
		error_code='environment_deletion_requested',error_message='snapshot cancelled because environment deletion was requested',
		lease_owner=NULL,lease_expires_at=NULL,completed_at=NOW()
		WHERE environment_id=$1 AND action='snapshot_environment'
		AND (status='pending' OR (status='leased' AND lease_expires_at<NOW()))`, environmentID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE dev_machine_environments
		SET status='delete_requested',delete_requested_at=COALESCE(delete_requested_at,NOW())
		WHERE workspace_id=$1 AND id=$2`, workspaceID, environmentID); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *DevMachineRepository) ListDeleteRequestedEnvironments(ctx context.Context, limit int) ([]domain.DevMachineEnvironment, error) {
	var environments []domain.DevMachineEnvironment
	err := r.db.SelectContext(ctx, &environments, `SELECT * FROM dev_machine_environments
		WHERE status='delete_requested' ORDER BY delete_requested_at NULLS LAST, updated_at LIMIT $1`, limit)
	if environments == nil {
		environments = []domain.DevMachineEnvironment{}
	}
	return environments, err
}

func (r *DevMachineRepository) DeleteEnvironment(ctx context.Context, workspaceID, environmentID uuid.UUID) error {
	result, err := r.db.ExecContext(ctx, `DELETE FROM dev_machine_environments
		WHERE workspace_id=$1 AND id=$2 AND status='delete_requested'`, workspaceID, environmentID)
	if err != nil {
		return err
	}
	if rows, _ := result.RowsAffected(); rows != 1 {
		return sql.ErrNoRows
	}
	return nil
}

func (r *DevMachineRepository) UpdateEnvironmentState(ctx context.Context, environmentID uuid.UUID, status, imageRef string, digest *string) error {
	result, err := r.db.ExecContext(ctx, `UPDATE dev_machine_environments SET status=$2,
		image_ref=CASE WHEN $3='' THEN image_ref ELSE $3 END, image_digest=COALESCE($4,image_digest)
		WHERE id=$1 AND status<>'delete_requested'`, environmentID, status, imageRef, digest)
	if err != nil {
		return err
	}
	if rows, _ := result.RowsAffected(); rows != 1 {
		return ErrEnvironmentDeletionConflict
	}
	return nil
}

func (r *DevMachineRepository) ReconcileOrphanedEnvironments(ctx context.Context, limit int) (int, error) {
	if limit <= 0 {
		limit = 100
	}
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var environmentIDs []uuid.UUID
	if err := tx.SelectContext(ctx, &environmentIDs, `SELECT id FROM dev_machine_environments
		WHERE status IN ('pending','building') ORDER BY updated_at,id LIMIT $1 FOR UPDATE SKIP LOCKED`, limit); err != nil {
		return 0, err
	}
	reconciled := 0
	for _, environmentID := range environmentIDs {
		var operations []struct {
			Status            domain.DevMachineOperationStatus `db:"status"`
			Generation        int64                            `db:"generation"`
			MachineGeneration int64                            `db:"machine_generation"`
			MachineStatus     domain.DevMachineStatus          `db:"machine_status"`
			DesiredStatus     domain.DevMachineStatus          `db:"desired_status"`
			LeaseActive       bool                             `db:"lease_active"`
		}
		if err := tx.SelectContext(ctx, &operations, `SELECT o.status,o.generation,
			m.generation AS machine_generation,m.status AS machine_status,m.desired_status,
			COALESCE(o.status='leased' AND o.lease_expires_at>=NOW(),FALSE) AS lease_active
			FROM dev_machine_operations o JOIN dev_machines m ON m.id=o.machine_id
			WHERE o.environment_id=$1 AND o.action='snapshot_environment' FOR UPDATE OF o`, environmentID); err != nil {
			return 0, err
		}
		viable := false
		for _, operation := range operations {
			if operation.LeaseActive {
				viable = true
				break
			}
			stable := operation.MachineStatus == operation.DesiredStatus &&
				(operation.MachineStatus == domain.DevMachineStatusPaused || operation.MachineStatus == domain.DevMachineStatusStopped)
			if operation.Status == domain.DevMachineOpStatusPending && operation.Generation == operation.MachineGeneration && stable {
				viable = true
				break
			}
		}
		if viable {
			continue
		}
		if _, err := tx.ExecContext(ctx, `UPDATE dev_machine_operations SET status='cancelled',
			error_code='environment_snapshot_orphaned',error_message='snapshot operation is no longer runnable',
			lease_owner=NULL,lease_expires_at=NULL,completed_at=NOW()
			WHERE environment_id=$1 AND action='snapshot_environment'
			AND (status='pending' OR (status='leased' AND (lease_expires_at IS NULL OR lease_expires_at<NOW())))`, environmentID); err != nil {
			return 0, err
		}
		result, err := tx.ExecContext(ctx, `UPDATE dev_machine_environments SET status='failed'
			WHERE id=$1 AND status IN ('pending','building')`, environmentID)
		if err != nil {
			return 0, err
		}
		if rows, _ := result.RowsAffected(); rows == 1 {
			reconciled++
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return reconciled, nil
}

func (r *DevMachineRepository) ReconcileOrphanedCheckouts(ctx context.Context, limit int) (int, error) {
	if limit <= 0 {
		limit = 100
	}
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var checkoutIDs []uuid.UUID
	if err := tx.SelectContext(ctx, &checkoutIDs, `SELECT id FROM dev_machine_checkouts
		WHERE status IN ('queued','preparing') ORDER BY updated_at,id LIMIT $1 FOR UPDATE SKIP LOCKED`, limit); err != nil {
		return 0, err
	}
	reconciled := 0
	for _, checkoutID := range checkoutIDs {
		var operations []struct {
			Status            domain.DevMachineOperationStatus `db:"status"`
			Generation        int64                            `db:"generation"`
			MachineGeneration int64                            `db:"machine_generation"`
			MachineStatus     domain.DevMachineStatus          `db:"machine_status"`
			DesiredStatus     domain.DevMachineStatus          `db:"desired_status"`
			LeaseActive       bool                             `db:"lease_active"`
		}
		if err := tx.SelectContext(ctx, &operations, `SELECT o.status,o.generation,
			m.generation AS machine_generation,m.status AS machine_status,m.desired_status,
			COALESCE(o.status='leased' AND o.lease_expires_at>=NOW(),FALSE) AS lease_active
			FROM dev_machine_operations o JOIN dev_machines m ON m.id=o.machine_id
			WHERE o.checkout_id=$1 AND o.action='checkout_issue' FOR UPDATE OF o`, checkoutID); err != nil {
			return 0, err
		}
		viable := false
		for _, operation := range operations {
			if operation.LeaseActive {
				viable = true
				break
			}
			if operation.Status == domain.DevMachineOpStatusPending && operation.Generation == operation.MachineGeneration &&
				operation.MachineStatus == domain.DevMachineStatusRunning && operation.DesiredStatus == domain.DevMachineStatusRunning {
				viable = true
				break
			}
		}
		if viable {
			continue
		}
		if _, err := tx.ExecContext(ctx, `UPDATE dev_machine_operations SET status='cancelled',
			error_code='checkout_preparation_orphaned',error_message='checkout preparation is no longer runnable',
			lease_owner=NULL,lease_expires_at=NULL,completed_at=NOW()
			WHERE checkout_id=$1 AND action='checkout_issue'
			AND (status='pending' OR (status='leased' AND (lease_expires_at IS NULL OR lease_expires_at<NOW())))`, checkoutID); err != nil {
			return 0, err
		}
		result, err := tx.ExecContext(ctx, `UPDATE dev_machine_checkouts SET status='failed',
			last_error='checkout preparation was interrupted; try again'
			WHERE id=$1 AND status IN ('queued','preparing')`, checkoutID)
		if err != nil {
			return 0, err
		}
		if rows, _ := result.RowsAffected(); rows == 1 {
			reconciled++
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return reconciled, nil
}

func (r *DevMachineRepository) GetCheckout(ctx context.Context, workspaceID, machineID, checkoutID uuid.UUID) (*domain.DevMachineCheckout, error) {
	var checkout domain.DevMachineCheckout
	err := r.db.GetContext(ctx, &checkout, `SELECT * FROM dev_machine_checkouts WHERE workspace_id=$1 AND machine_id=$2 AND id=$3`, workspaceID, machineID, checkoutID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &checkout, err
}

func (r *DevMachineRepository) GetCheckoutInternal(ctx context.Context, checkoutID uuid.UUID) (*domain.DevMachineCheckout, error) {
	var checkout domain.DevMachineCheckout
	err := r.db.GetContext(ctx, &checkout, `SELECT * FROM dev_machine_checkouts WHERE id=$1`, checkoutID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &checkout, err
}

func (r *DevMachineRepository) ListCheckouts(ctx context.Context, workspaceID, machineID uuid.UUID) ([]domain.DevMachineCheckout, error) {
	var checkouts []domain.DevMachineCheckout
	err := r.db.SelectContext(ctx, &checkouts, `SELECT * FROM dev_machine_checkouts WHERE workspace_id=$1 AND machine_id=$2 ORDER BY created_at DESC`, workspaceID, machineID)
	if checkouts == nil {
		checkouts = []domain.DevMachineCheckout{}
	}
	return checkouts, err
}

func (r *DevMachineRepository) CreateCheckout(ctx context.Context, checkout *domain.DevMachineCheckout, operation *domain.DevMachineOperation) error {
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE dev_machines SET repository_affinity_id=$1, issue_id=$2,
		repo_url=$3,repo_provider='github',repo_owner=split_part($4,'/',1),repo_name=split_part($4,'/',2),
		base_branch=$5,working_branch=$6,last_activity_at=NOW()
		WHERE workspace_id=$7 AND id=$8 AND (repository_affinity_id IS NULL OR repository_affinity_id=$1)
		AND status='running' AND desired_status='running'`, checkout.GitHubRepoID, checkout.IssueID,
		"https://github.com/"+checkout.RepositoryFullName, checkout.RepositoryFullName, checkout.BaseBranch,
		checkout.WorkingBranch, checkout.WorkspaceID, checkout.MachineID)
	if err != nil {
		return err
	}
	if rows, _ := result.RowsAffected(); rows != 1 {
		return ErrCheckoutMachineConflict
	}
	if err := tx.QueryRowContext(ctx, `INSERT INTO dev_machine_checkouts
		(id,workspace_id,machine_id,issue_id,github_repo_id,repository_full_name,base_branch,working_branch,workspace_path,status)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
		ON CONFLICT (machine_id,issue_id) DO UPDATE SET status=EXCLUDED.status,last_error=NULL,last_activity_at=NOW()
		RETURNING id,created_at,updated_at`, checkout.ID, checkout.WorkspaceID, checkout.MachineID, checkout.IssueID,
		checkout.GitHubRepoID, checkout.RepositoryFullName, checkout.BaseBranch, checkout.WorkingBranch,
		checkout.WorkspacePath, checkout.Status,
	).Scan(&checkout.ID, &checkout.CreatedAt, &checkout.UpdatedAt); err != nil {
		return err
	}
	operation.CheckoutID = &checkout.ID
	if err := enqueueOperation(ctx, tx, operation); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *DevMachineRepository) UpdateCheckoutState(ctx context.Context, checkoutID uuid.UUID, status string, lastError *string) error {
	_, err := r.db.ExecContext(ctx, `UPDATE dev_machine_checkouts SET status=$2::varchar(32),last_error=$3,
		last_activity_at=CASE WHEN $2::text='ready' THEN NOW() ELSE last_activity_at END WHERE id=$1`, checkoutID, status, lastError)
	return err
}

func (r *DevMachineRepository) CreateTerminalSession(ctx context.Context, session *domain.DevMachineTerminalSession) error {
	return r.db.QueryRowContext(ctx, `INSERT INTO dev_machine_terminal_sessions
		(id,workspace_id,machine_id,checkout_id,user_id,name,runtime_session_name,status)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
		RETURNING created_at,last_activity_at`, session.ID, session.WorkspaceID, session.MachineID,
		session.CheckoutID, session.UserID, session.Name, session.RuntimeSessionName, session.Status,
	).Scan(&session.CreatedAt, &session.LastActivityAt)
}

func (r *DevMachineRepository) ListTerminalSessions(ctx context.Context, workspaceID, machineID, userID uuid.UUID) ([]domain.DevMachineTerminalSession, error) {
	var sessions []domain.DevMachineTerminalSession
	err := r.db.SelectContext(ctx, &sessions, `SELECT * FROM dev_machine_terminal_sessions
		WHERE workspace_id=$1 AND machine_id=$2 AND user_id=$3 ORDER BY created_at DESC`, workspaceID, machineID, userID)
	if sessions == nil {
		sessions = []domain.DevMachineTerminalSession{}
	}
	return sessions, err
}

func (r *DevMachineRepository) RequestTerminalSessionClose(ctx context.Context, workspaceID, machineID, userID, sessionID uuid.UUID, operation *domain.DevMachineOperation) (*domain.DevMachineTerminalSession, error) {
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var session domain.DevMachineTerminalSession
	err = tx.GetContext(ctx, &session, `SELECT * FROM dev_machine_terminal_sessions
		WHERE workspace_id=$1 AND machine_id=$2 AND user_id=$3 AND id=$4 FOR UPDATE`, workspaceID, machineID, userID, sessionID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if session.Status == "closed" {
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		return &session, nil
	}

	if err := tx.GetContext(ctx, &operation.Generation, `SELECT generation FROM dev_machines
		WHERE workspace_id=$1 AND id=$2`, workspaceID, machineID); err != nil {
		return nil, err
	}
	session.Status = "closing"
	if _, err := tx.ExecContext(ctx, `UPDATE dev_machine_terminal_sessions SET status='closing',closed_at=NULL WHERE id=$1`, session.ID); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE dev_machine_access_tickets SET status='revoked',revoked_at=COALESCE(revoked_at,NOW())
		WHERE terminal_session_id=$1 AND status IN ('active','used')`, session.ID); err != nil {
		return nil, err
	}

	operation.TerminalSessionID = &session.ID
	if operation.ID == uuid.Nil {
		operation.ID = uuid.New()
	}
	if operation.MaxAttempts == 0 {
		operation.MaxAttempts = 5
	}
	err = tx.QueryRowContext(ctx, `INSERT INTO dev_machine_operations
		(id,machine_id,terminal_session_id,workspace_id,action,status,generation,idempotency_key,requested_by_user_id,max_attempts)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
		ON CONFLICT (machine_id,idempotency_key) DO UPDATE SET
			status=CASE WHEN dev_machine_operations.status IN ('completed','failed','cancelled') THEN 'pending'::dev_machine_operation_status ELSE dev_machine_operations.status END,
			attempts=CASE WHEN dev_machine_operations.status IN ('completed','failed','cancelled') THEN 0 ELSE dev_machine_operations.attempts END,
			lease_owner=CASE WHEN dev_machine_operations.status IN ('completed','failed','cancelled') THEN NULL ELSE dev_machine_operations.lease_owner END,
			lease_expires_at=CASE WHEN dev_machine_operations.status IN ('completed','failed','cancelled') THEN NULL ELSE dev_machine_operations.lease_expires_at END,
			available_at=CASE WHEN dev_machine_operations.status IN ('completed','failed','cancelled') THEN NOW() ELSE dev_machine_operations.available_at END,
			completed_at=CASE WHEN dev_machine_operations.status IN ('completed','failed','cancelled') THEN NULL ELSE dev_machine_operations.completed_at END,
			error_code=CASE WHEN dev_machine_operations.status IN ('completed','failed','cancelled') THEN NULL ELSE dev_machine_operations.error_code END,
			error_message=CASE WHEN dev_machine_operations.status IN ('completed','failed','cancelled') THEN NULL ELSE dev_machine_operations.error_message END
		RETURNING id,status,attempts,available_at,created_at,updated_at`,
		operation.ID, operation.MachineID, operation.TerminalSessionID, operation.WorkspaceID, operation.Action,
		operation.Status, operation.Generation, operation.IdempotencyKey, operation.RequestedByUserID, operation.MaxAttempts,
	).Scan(&operation.ID, &operation.Status, &operation.Attempts, &operation.AvailableAt, &operation.CreatedAt, &operation.UpdatedAt)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &session, nil
}

func (r *DevMachineRepository) GetTerminalSessionInternal(ctx context.Context, sessionID uuid.UUID) (*domain.DevMachineTerminalSession, error) {
	var session domain.DevMachineTerminalSession
	err := r.db.GetContext(ctx, &session, `SELECT * FROM dev_machine_terminal_sessions WHERE id=$1`, sessionID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &session, err
}

func (r *DevMachineRepository) CompleteTerminalSessionClose(ctx context.Context, sessionID uuid.UUID) error {
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE dev_machine_access_tickets SET status='revoked',revoked_at=COALESCE(revoked_at,NOW())
		WHERE terminal_session_id=$1 AND status IN ('active','used')`, sessionID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE dev_machine_terminal_sessions SET status='closed',closed_at=COALESCE(closed_at,NOW())
		WHERE id=$1 AND status<>'closed'`, sessionID); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *DevMachineRepository) FailTerminalSessionClose(ctx context.Context, sessionID uuid.UUID) error {
	_, err := r.db.ExecContext(ctx, `UPDATE dev_machine_terminal_sessions SET status='close_failed'
		WHERE id=$1 AND status='closing'`, sessionID)
	return err
}

func (r *DevMachineRepository) ListAgentRunSteps(ctx context.Context, runID uuid.UUID) ([]domain.DevMachineAgentRunStep, error) {
	var steps []domain.DevMachineAgentRunStep
	err := r.db.SelectContext(ctx, &steps, `SELECT * FROM dev_machine_agent_run_steps WHERE agent_run_id=$1 ORDER BY sequence`, runID)
	if steps == nil {
		steps = []domain.DevMachineAgentRunStep{}
	}
	return steps, err
}

func (r *DevMachineRepository) ListAgentRunEvents(ctx context.Context, runID uuid.UUID, afterID int64, limit int) ([]domain.DevMachineEvent, error) {
	var events []domain.DevMachineEvent
	err := r.db.SelectContext(ctx, &events, `SELECT * FROM dev_machine_events WHERE agent_run_id=$1 AND ($2=0 OR id>$2) ORDER BY id ASC LIMIT $3`, runID, afterID, limit)
	if events == nil {
		events = []domain.DevMachineEvent{}
	}
	return events, err
}

func (r *DevMachineRepository) ListAgentRunLogs(ctx context.Context, runID uuid.UUID, afterID int64, limit int) ([]domain.DevMachineLogChunk, error) {
	var chunks []domain.DevMachineLogChunk
	err := r.db.SelectContext(ctx, &chunks, `SELECT * FROM dev_machine_log_chunks WHERE agent_run_id=$1 AND ($2=0 OR id>$2) ORDER BY id ASC LIMIT $3`, runID, afterID, limit)
	if chunks == nil {
		chunks = []domain.DevMachineLogChunk{}
	}
	return chunks, err
}
