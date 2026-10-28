package repository

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestAgentTaskCannotPublishFromNativeSuccessAlone(t *testing.T) {
	db, repo, ctx, task, fixture := taskRecoveryFixture(t)
	claimed := prepareTaskStart(t, db, repo, ctx, task, fixture)
	_, err := repo.Start(ctx, *claimed)
	require.NoError(t, err)
	_, err = db.Exec(`UPDATE dev_machine_agent_runs SET status='succeeded',completed_at=now(),exit_code=0 WHERE id=$1`, task.RunID)
	require.NoError(t, err)
	require.NoError(t, repo.Transition(ctx, *claimed, "validating", "Native attempt finished"))
	require.ErrorIs(t, repo.Transition(ctx, *claimed, "publishing", "Agent claimed tests passed"), ErrAgentTaskConflict)
}

func TestIndependentValidationPersistsEvidenceAndFencesStaleWriters(t *testing.T) {
	db, repo, ctx, task, fixture := taskRecoveryFixture(t)
	claimed := prepareTaskStart(t, db, repo, ctx, task, fixture)
	_, err := repo.Start(ctx, *claimed)
	require.NoError(t, err)
	_, err = db.Exec(`UPDATE dev_machine_agent_runs SET status='succeeded',completed_at=now(),exit_code=0 WHERE id=$1`, task.RunID)
	require.NoError(t, err)
	require.NoError(t, repo.Transition(ctx, *claimed, "validating", "Native attempt finished"))
	_, err = db.Exec(`UPDATE agent_tasks SET validation_command='["sh","-c","exit 0"]' WHERE id=$1`, task.ID)
	require.NoError(t, err)
	step, err := repo.EnsureValidation(ctx, *claimed)
	require.NoError(t, err)
	require.Equal(t, domain.DevMachineAgentRunStepStatusQueued, step.Status)
	again, err := repo.EnsureValidation(ctx, *claimed)
	require.NoError(t, err)
	require.Equal(t, step.ID, again.ID)
	store := NewDevMachineRepository(db)
	ops, err := store.LeaseOperations(ctx, "validation-fixture", 1, time.Minute)
	require.NoError(t, err)
	require.Len(t, ops, 1)
	op := ops[0]
	require.Equal(t, domain.DevMachineOpValidateAgent, op.Action)
	service := &domain.DevMachineService{ID: step.ID, WorkspaceID: task.WorkspaceID, MachineID: op.MachineID, AgentRunID: &task.RunID, ServiceType: "agent", ServiceKey: "validation-" + task.RunID.String(), ContainerName: "validation", ImageRef: "fixture", InternalHost: "validation", InternalPort: 8080, Status: "pending", HealthStatus: "unknown"}
	require.NoError(t, store.CreateRuntimeService(ctx, service))
	require.ErrorIs(t, store.CompleteValidation(ctx, op, 0), ErrAgentTaskConflict, "a result needs a real start")
	require.NoError(t, store.StartValidation(ctx, op, "container"))
	stale := op
	stale.Attempts++
	require.ErrorIs(t, store.CompleteValidation(ctx, stale, 0), ErrAgentTaskConflict)
	chunk := &domain.DevMachineLogChunk{WorkspaceID: task.WorkspaceID, MachineID: op.MachineID, AgentRunID: &task.RunID, ServiceID: &step.ID, Stream: "stdout", Sequence: 1, Content: "actual check output", SourceKey: "source"}
	require.NoError(t, store.CreateValidationOutput(ctx, op, chunk))
	require.NoError(t, store.CreateValidationOutput(ctx, op, chunk))
	logs, err := repo.Logs(ctx, task.WorkspaceID, task.ID, task.RunID, 0)
	require.NoError(t, err)
	require.Len(t, logs.Logs, 1)
	require.NoError(t, store.CompleteValidation(ctx, op, 0))
	require.ErrorIs(t, repo.Transition(ctx, *claimed, "publishing", "Checks passed"), ErrAgentTaskConflict, "operation completion must settle first")
	require.NoError(t, store.CompleteOperation(ctx, op.ID, *op.LeaseOwner))
	require.NoError(t, repo.Transition(ctx, *claimed, "publishing", "Checks passed"))
	result, err := repo.Validation(ctx, task.WorkspaceID, task.ID, task.RunID)
	require.NoError(t, err)
	require.Equal(t, domain.DevMachineAgentRunStepStatusSucceeded, result.Status)
	require.Equal(t, 0, *result.ExitCode)
	require.NotNil(t, result.StartedAt)
	require.NotNil(t, result.CompletedAt)
	require.Error(t, store.CreateValidationOutput(ctx, op, chunk), "finished validation cannot append output")
	stranger := domain.WithAccess(ctx, domain.Access{WorkspaceID: task.WorkspaceID, UserID: uuid.New()})
	_, err = repo.Validation(stranger, task.WorkspaceID, task.ID, task.RunID)
	require.ErrorIs(t, err, ErrConversationNotFound)
	_, err = repo.Validation(ctx, task.WorkspaceID, task.ID, uuid.New())
	require.ErrorIs(t, err, ErrConversationNotFound)
}

func TestMissingValidationCommandIsUnavailableNotPassed(t *testing.T) {
	db, repo, ctx, task, fixture := taskRecoveryFixture(t)
	claimed := prepareTaskStart(t, db, repo, ctx, task, fixture)
	_, err := repo.Start(ctx, *claimed)
	require.NoError(t, err)
	_, err = db.Exec(`UPDATE dev_machine_agent_runs SET status='succeeded',completed_at=now() WHERE id=$1`, task.RunID)
	require.NoError(t, err)
	require.NoError(t, repo.Transition(ctx, *claimed, "validating", "Native attempt finished"))
	step, err := repo.EnsureValidation(ctx, *claimed)
	require.NoError(t, err)
	require.Equal(t, domain.DevMachineAgentRunStepStatusSkipped, step.Status)
	require.Nil(t, step.ExitCode)
	var count int
	require.NoError(t, db.Get(&count, `SELECT count(*) FROM dev_machine_operations WHERE agent_run_id=$1 AND action='validate_agent'`, task.RunID))
	require.Zero(t, count)
	require.ErrorIs(t, repo.Transition(ctx, *claimed, "publishing", "Unavailable is not passed"), ErrAgentTaskConflict)
}

func TestValidationRejectsRevokedAuthorityAndExpiredOperationLeases(t *testing.T) {
	for _, scenario := range []string{"cancelled", "membership", "policy", "machine_generation", "expired_lease", "superseded_attempt"} {
		t.Run(scenario, func(t *testing.T) {
			db, repo, ctx, task, fixture := taskRecoveryFixture(t)
			claimed := prepareTaskStart(t, db, repo, ctx, task, fixture)
			_, err := repo.Start(ctx, *claimed)
			require.NoError(t, err)
			_, err = db.Exec(`UPDATE dev_machine_agent_runs SET status='succeeded',completed_at=now() WHERE id=$1`, task.RunID)
			require.NoError(t, err)
			require.NoError(t, repo.Transition(ctx, *claimed, "validating", "Native attempt finished"))
			_, err = db.Exec(`UPDATE agent_tasks SET validation_command='["true"]' WHERE id=$1`, task.ID)
			require.NoError(t, err)
			step, err := repo.EnsureValidation(ctx, *claimed)
			require.NoError(t, err)
			store := NewDevMachineRepository(db)
			ops, err := store.LeaseOperations(ctx, "validation-fixture", 1, time.Minute)
			require.NoError(t, err)
			require.Len(t, ops, 1)
			op := ops[0]
			require.NoError(t, store.CreateRuntimeService(ctx, &domain.DevMachineService{ID: step.ID, WorkspaceID: task.WorkspaceID, MachineID: op.MachineID, AgentRunID: &task.RunID, ServiceType: "agent", ServiceKey: "validation", ContainerName: "validation", ImageRef: "fixture", InternalHost: "validation", InternalPort: 8080, Status: "pending", HealthStatus: "unknown"}))
			require.NoError(t, store.StartValidation(ctx, op, "container"))
			// A second native writer cannot use this machine during independent checks.
			competing := &domain.DevMachineAgentRun{ID: uuid.New(), WorkspaceID: task.WorkspaceID, MachineID: op.MachineID}
			require.ErrorIs(t, store.CreateAgentRun(context.Background(), competing, &domain.DevMachineOperation{Generation: op.Generation}), ErrActiveAgentRun)
			switch scenario {
			case "cancelled":
				err = repo.RequestCancel(ctx, task.WorkspaceID, task.ID)
			case "membership":
				_, err = db.Exec(`DELETE FROM workspace_members WHERE workspace_id=$1 AND user_id=$2`, task.WorkspaceID, task.CreatorID)
			case "policy":
				_, err = db.Exec(`UPDATE dev_machine_workspace_policies SET enabled=false WHERE workspace_id=$1`, task.WorkspaceID)
			case "machine_generation":
				_, err = db.Exec(`UPDATE dev_machines SET generation=generation+1 WHERE id=$1`, op.MachineID)
			case "expired_lease":
				_, err = db.Exec(`UPDATE dev_machine_operations SET lease_expires_at=now()-interval '1 second' WHERE id=$1`, op.ID)
			case "superseded_attempt":
				_, err = db.Exec(`UPDATE agent_tasks SET run_id=gen_random_uuid() WHERE id=$1`, task.ID)
			}
			require.NoError(t, err)
			_, err = store.GetValidation(ctx, op)
			require.Error(t, err)
			require.Error(t, store.CompleteValidation(ctx, op, 0))
			require.Error(t, store.CreateValidationOutput(ctx, op, &domain.DevMachineLogChunk{WorkspaceID: task.WorkspaceID, MachineID: op.MachineID, AgentRunID: &task.RunID, ServiceID: &step.ID, Stream: "stdout", Sequence: 1, Content: "must not persist"}))
			var succeeded bool
			require.NoError(t, db.Get(&succeeded, `SELECT status='succeeded' FROM dev_machine_agent_run_steps WHERE id=$1`, step.ID))
			require.False(t, succeeded)
			var count int
			require.NoError(t, db.Get(&count, `SELECT count(*) FROM dev_machine_log_chunks WHERE service_id=$1`, step.ID))
			require.Zero(t, count)
		})
	}
}

func TestValidationArtifactsAreSavedUnderTheLeaseAndReadWithTaskPermissions(t *testing.T) {
	db, repo, ctx, task, fixture := taskRecoveryFixture(t)
	claimed := prepareTaskStart(t, db, repo, ctx, task, fixture)
	_, err := repo.Start(ctx, *claimed)
	require.NoError(t, err)
	_, err = db.Exec(`UPDATE dev_machine_agent_runs SET status='succeeded',completed_at=now() WHERE id=$1`, task.RunID)
	require.NoError(t, err)
	require.NoError(t, repo.Transition(ctx, *claimed, "validating", "Native attempt finished"))
	_, err = db.Exec(`UPDATE agent_tasks SET validation_command='["true"]' WHERE id=$1`, task.ID)
	require.NoError(t, err)
	step, err := repo.EnsureValidation(ctx, *claimed)
	require.NoError(t, err)
	store := NewDevMachineRepository(db)
	ops, err := store.LeaseOperations(ctx, "validation-fixture", 1, time.Minute)
	require.NoError(t, err)
	op := ops[0]
	require.NoError(t, store.CreateRuntimeService(ctx, &domain.DevMachineService{ID: step.ID, WorkspaceID: task.WorkspaceID, MachineID: op.MachineID, AgentRunID: &task.RunID, ServiceType: "agent", ServiceKey: "validation", ContainerName: "validation", ImageRef: "fixture", InternalHost: "validation", InternalPort: 8080, Status: "pending", HealthStatus: "unknown"}))
	artifacts := []domain.ValidationArtifact{{Path: "playwright-report/index.html", ContentType: "text/html", Data: []byte("<html></html>")}, {Path: "test-results/shot.png", ContentType: "image/png", Data: []byte("PNG")}}
	require.ErrorIs(t, store.SaveValidationArtifacts(ctx, op, artifacts), ErrAgentTaskConflict, "only a running check may attach artifacts")
	require.NoError(t, store.StartValidation(ctx, op, "container"))
	require.NoError(t, store.SaveValidationArtifacts(ctx, op, artifacts))
	require.NoError(t, store.SaveValidationArtifacts(ctx, op, artifacts), "a replay keeps the first copy")
	stale := op
	stale.Attempts++
	require.Error(t, store.SaveValidationArtifacts(ctx, stale, artifacts))
	_, err = db.Exec(`INSERT INTO agent_validation_artifacts(step_id,path,content_type,size,data) VALUES($1,'../escape.txt','text/plain',1,'x')`, step.ID)
	require.Error(t, err, "the schema rejects traversal paths")
	list, err := repo.ValidationArtifacts(ctx, task.WorkspaceID, task.ID, task.RunID)
	require.NoError(t, err)
	require.Len(t, list, 2)
	require.Nil(t, list[0].Data, "listings never carry content")
	one, err := repo.ValidationArtifact(ctx, task.WorkspaceID, task.ID, task.RunID, list[1].ID)
	require.NoError(t, err)
	require.Equal(t, []byte("PNG"), one.Data)
	stranger := domain.WithAccess(context.Background(), domain.Access{WorkspaceID: task.WorkspaceID, UserID: uuid.New()})
	_, err = repo.ValidationArtifact(stranger, task.WorkspaceID, task.ID, task.RunID, list[1].ID)
	require.ErrorIs(t, err, ErrConversationNotFound)
	_, err = repo.ValidationArtifact(ctx, task.WorkspaceID, task.ID, uuid.New(), list[1].ID)
	require.ErrorIs(t, err, ErrConversationNotFound, "an artifact is bound to its attempt")
}

func TestComposeFileIsInheritedSnapshottedAndARejectionExplainsTheFailedCheck(t *testing.T) {
	db, repo, ctx, task, fixture := taskRecoveryFixture(t)
	store := NewDevMachineRepository(db)
	compose := "deploy/compose.yaml"
	previewCommand, previewPort := json.RawMessage(`["npm","run","dev"]`), 5173
	require.NoError(t, store.UpsertScopeSetting(ctx, &domain.DevMachineScopeSetting{ID: uuid.New(), WorkspaceID: task.WorkspaceID, TeamID: &task.TeamID, ComposeFile: &compose, PreviewCommand: &previewCommand, PreviewPort: &previewPort}))
	for _, invalid := range []string{"../compose.yaml", "/etc/compose.yaml", "compose.json", "a/./compose.yml"} {
		_, err := db.Exec(`UPDATE dev_machine_scope_settings SET compose_file=$1 WHERE team_id=$2`, invalid, task.TeamID)
		require.Error(t, err, invalid)
	}
	var existing string
	require.NoError(t, db.Get(&existing, `SELECT compose_file FROM agent_tasks WHERE id=$1`, task.ID))
	require.Empty(t, existing, "a task accepted before the setting keeps its snapshot")
	second, err := repo.Create(ctx, task.WorkspaceID, *task.ConversationID, domain.AgentTaskRequest{RequestID: uuid.New(), Prompt: "Second task", Provider: "codex"})
	require.NoError(t, err)
	require.Equal(t, compose, second.ComposeFile, "new tasks snapshot the nearest compose file")
	require.JSONEq(t, `["npm","run","dev"]`, string(second.PreviewCommand), "and the inherited preview")
	require.Equal(t, 5173, *second.PreviewPort)
	_, err = db.Exec(`UPDATE agent_tasks SET compose_file=$2 WHERE id=$1`, task.ID, compose)
	require.NoError(t, err)
	claimed := prepareTaskStart(t, db, repo, ctx, task, fixture)
	_, err = repo.Start(ctx, *claimed)
	require.NoError(t, err)
	_, err = db.Exec(`UPDATE dev_machine_agent_runs SET status='succeeded',completed_at=now(),exit_code=0 WHERE id=$1`, claimed.RunID)
	require.NoError(t, err)
	require.NoError(t, repo.Transition(ctx, *claimed, "validating", "Native attempt finished"))
	_, err = db.Exec(`UPDATE agent_tasks SET validation_command='["npm","test"]' WHERE id=$1`, task.ID)
	require.NoError(t, err)
	step, err := repo.EnsureValidation(ctx, *claimed)
	require.NoError(t, err)
	require.Equal(t, compose, step.ComposeFile, "the check uses the task's snapshot")

	ops, err := store.LeaseOperations(ctx, "compose-fixture", 1, time.Minute)
	require.NoError(t, err)
	require.Len(t, ops, 1)
	_, err = store.FailOperation(ctx, ops[0].ID, "compose-fixture", "compose_rejected", `compose_rejected: Compose file rejected: service "app": "privileged" is not supported`, false)
	require.NoError(t, err)
	step, err = repo.EnsureValidation(ctx, *claimed)
	require.NoError(t, err)
	require.Equal(t, domain.DevMachineAgentRunStepStatusFailed, step.Status)
	require.Equal(t, `Compose file rejected: service "app": "privileged" is not supported`, *step.Summary)
}
