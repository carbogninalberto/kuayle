package repository

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
)

// declareValidation finishes the attempt with what its validation file declared.
func declareValidation(t *testing.T, store *DevMachineRepository, run *domain.DevMachineAgentRun, declared *domain.AgentDeclaredValidation) {
	t.Helper()
	run.Status = domain.DevMachineAgentRunStatusSucceeded
	if declared != nil {
		data, err := json.Marshal(declared)
		require.NoError(t, err)
		raw := json.RawMessage(data)
		run.DeclaredValidation = &raw
	}
	require.NoError(t, store.CompleteAgentRun(t.Context(), run))
}

func TestAgentChosenTestsRunWhenNoCheckIsConfigured(t *testing.T) {
	db, repo, ctx, task, fixture := taskRecoveryFixture(t)
	claimed := prepareTaskStart(t, db, repo, ctx, task, fixture)
	_, err := repo.Start(ctx, *claimed)
	require.NoError(t, err)
	store := NewDevMachineRepository(db)
	run, err := store.GetAgentRunInternal(ctx, task.RunID)
	require.NoError(t, err)
	declareValidation(t, store, run, &domain.AgentDeclaredValidation{Commands: [][]string{{"sh", "-c", "cd BE && go test ./internal/service/..."}, {"npm", "run", "check"}}, Summary: "Service tests; the UI was not run."})
	require.NoError(t, repo.Transition(ctx, *claimed, "validating", "Native attempt finished"))

	step, err := repo.EnsureValidation(ctx, *claimed)
	require.NoError(t, err)
	require.Equal(t, domain.DevMachineAgentRunStepStatusQueued, step.Status)
	require.Equal(t, domain.CheckSourceAgent, *step.CommandSource)
	require.Equal(t, [][]string{{"sh", "-c", "cd BE && go test ./internal/service/..."}, {"npm", "run", "check"}}, step.CheckCommands())
	require.JSONEq(t, `["sh","-c","cd BE && go test ./internal/service/..."]`, string(step.CommandArgv), "the first command, so the step is never mistaken for an unchecked one")
	require.Equal(t, "Service tests; the UI was not run.", *step.AgentSummary)

	ops, err := store.LeaseOperations(ctx, "validation-fixture", 1, time.Minute)
	require.NoError(t, err)
	require.Len(t, ops, 1)
	op := ops[0]
	require.NoError(t, store.CreateRuntimeService(ctx, &domain.DevMachineService{ID: step.ID, WorkspaceID: task.WorkspaceID, MachineID: op.MachineID, AgentRunID: &task.RunID, ServiceType: "agent", ServiceKey: "validation", ContainerName: "validation", ImageRef: "fixture", InternalHost: "validation", InternalPort: 8080, Status: "pending", HealthStatus: "unknown"}))
	require.NoError(t, store.StartValidation(ctx, op, "container"))
	require.ErrorIs(t, store.CompleteValidation(ctx, op, 0, []domain.CheckCommandResult{{ExitCode: 0}}), ErrAgentTaskConflict, "every command needs a result")
	require.ErrorIs(t, store.CompleteValidation(ctx, op, 0, []domain.CheckCommandResult{{ExitCode: 0}, {ExitCode: 2}}), ErrAgentTaskConflict, "the overall exit code must agree with the commands")
	require.NoError(t, store.CompleteValidation(ctx, op, 0, []domain.CheckCommandResult{{ExitCode: 0}, {ExitCode: 0}}))
	require.NoError(t, store.CompleteOperation(ctx, op.ID, *op.LeaseOwner))
	require.NoError(t, repo.Transition(ctx, *claimed, "publishing", "Independent validation passed"))

	result, err := repo.Validation(ctx, task.WorkspaceID, task.ID, task.RunID)
	require.NoError(t, err)
	require.JSONEq(t, `[{"exit_code":0},{"exit_code":0}]`, string(result.CommandResults))
	_, err = repo.EnsurePublication(ctx, *claimed)
	require.NoError(t, err)
	publication, err := store.GetPublication(ctx, leasePublication(t, store, ctx))
	require.NoError(t, err)
	require.Equal(t, domain.CheckSourceAgent, publication.CheckSource)
	require.Equal(t, "Service tests; the UI was not run.", publication.AgentSummary)
	require.Equal(t, []domain.CheckCommandRun{{Argv: []string{"sh", "-c", "cd BE && go test ./internal/service/..."}, ExitCode: 0}, {Argv: []string{"npm", "run", "check"}, ExitCode: 0}}, publication.CheckCommands)
}

func TestConfiguredCheckOverridesAgentChosenTests(t *testing.T) {
	db, repo, ctx, task, fixture := taskRecoveryFixture(t)
	claimed := prepareTaskStart(t, db, repo, ctx, task, fixture)
	_, err := repo.Start(ctx, *claimed)
	require.NoError(t, err)
	store := NewDevMachineRepository(db)
	run, err := store.GetAgentRunInternal(ctx, task.RunID)
	require.NoError(t, err)
	declareValidation(t, store, run, &domain.AgentDeclaredValidation{Commands: [][]string{{"true"}}, Summary: "Nothing to see."})
	_, err = db.Exec(`UPDATE agent_tasks SET validation_command='["make","check"]' WHERE id=$1`, task.ID)
	require.NoError(t, err)
	require.NoError(t, repo.Transition(ctx, *claimed, "validating", "Native attempt finished"))
	step, err := repo.EnsureValidation(ctx, *claimed)
	require.NoError(t, err)
	require.Equal(t, domain.CheckSourceConfigured, *step.CommandSource)
	require.Equal(t, [][]string{{"make", "check"}}, step.CheckCommands())
	require.Nil(t, step.AgentSummary, "an ignored declaration is not shown as the check's rationale")
}

func TestRejectedOrMissingDeclarationLeavesTheWorkUnvalidated(t *testing.T) {
	for name, declared := range map[string]*domain.AgentDeclaredValidation{
		"missing":  nil,
		"rejected": {Error: "the file has content after its JSON object"},
	} {
		t.Run(name, func(t *testing.T) {
			db, repo, ctx, task, fixture := taskRecoveryFixture(t)
			claimed := prepareTaskStart(t, db, repo, ctx, task, fixture)
			_, err := repo.Start(ctx, *claimed)
			require.NoError(t, err)
			store := NewDevMachineRepository(db)
			run, err := store.GetAgentRunInternal(ctx, task.RunID)
			require.NoError(t, err)
			declareValidation(t, store, run, declared)
			require.NoError(t, repo.Transition(ctx, *claimed, "validating", "Native attempt finished"))
			step, err := repo.EnsureValidation(ctx, *claimed)
			require.NoError(t, err)
			require.Equal(t, domain.DevMachineAgentRunStepStatusSkipped, step.Status)
			require.JSONEq(t, `[]`, string(step.CommandArgv))
			require.Nil(t, step.CommandSource)
			if declared != nil {
				require.Contains(t, *step.Summary, "validation.json was rejected: the file has content after its JSON object")
			}
			require.NoError(t, repo.Transition(ctx, *claimed, "publishing", "No check; publishing a draft"))
		})
	}
}

func TestFailedAgentChosenTestReachesTheNextAttemptsHandoff(t *testing.T) {
	db, repo, ctx, task, fixture := taskRecoveryFixture(t)
	claimed := prepareTaskStart(t, db, repo, ctx, task, fixture)
	_, err := repo.Start(ctx, *claimed)
	require.NoError(t, err)
	store := NewDevMachineRepository(db)
	run, err := store.GetAgentRunInternal(ctx, task.RunID)
	require.NoError(t, err)
	declareValidation(t, store, run, &domain.AgentDeclaredValidation{Commands: [][]string{{"go", "vet", "./..."}, {"go", "test", "./internal/service/..."}}})
	require.NoError(t, repo.Transition(ctx, *claimed, "validating", "Native attempt finished"))
	step, err := repo.EnsureValidation(ctx, *claimed)
	require.NoError(t, err)
	ops, err := store.LeaseOperations(ctx, "validation-fixture", 1, time.Minute)
	require.NoError(t, err)
	op := ops[0]
	require.NoError(t, store.CreateRuntimeService(ctx, &domain.DevMachineService{ID: step.ID, WorkspaceID: task.WorkspaceID, MachineID: op.MachineID, AgentRunID: &task.RunID, ServiceType: "agent", ServiceKey: "validation", ContainerName: "validation", ImageRef: "fixture", InternalHost: "validation", InternalPort: 8080, Status: "pending", HealthStatus: "unknown"}))
	require.NoError(t, store.StartValidation(ctx, op, "container"))
	require.NoError(t, store.CompleteValidation(ctx, op, 1, []domain.CheckCommandResult{{ExitCode: 0}, {ExitCode: 1}}))
	failed, err := repo.Validation(ctx, task.WorkspaceID, task.ID, task.RunID)
	require.NoError(t, err)
	require.Equal(t, domain.DevMachineAgentRunStepStatusFailed, failed.Status)
	require.Equal(t, 1, *failed.ExitCode)
	command, exit, ok := failed.FailedCommand()
	require.True(t, ok)
	require.Equal(t, []string{"go", "test", "./internal/service/..."}, command)
	require.Equal(t, 1, exit)

	// The next attempt reads the failing command from the recorded attempt.
	next := *claimed
	next.RunID = uuid.New()
	handoff, err := repo.PreviousAttempt(ctx, next)
	require.NoError(t, err)
	require.Equal(t, "failed", handoff.Validation)
	require.Equal(t, domain.CheckSourceAgent, handoff.ValidationSource)
	require.Equal(t, []string{"go", "test", "./internal/service/..."}, handoff.FailedCommand)
	require.Equal(t, 1, *handoff.FailedCommandExitCode)
}
