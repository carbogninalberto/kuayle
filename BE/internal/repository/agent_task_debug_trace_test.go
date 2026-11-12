package repository

import (
	"testing"

	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
)

// A failed task keeps the class and the real cause of the run, not the
// generic "Execution failed; runtime cleanup confirmed".
func TestTaskFailureKeepsItsClassAndCause(t *testing.T) {
	_, repo, ctx, task, _ := taskRecoveryFixture(t)
	claimed, err := repo.Claim(ctx)
	require.NoError(t, err)
	require.NotNil(t, claimed)
	require.NoError(t, repo.Transition(ctx, *claimed, "preparing", "Preparing"))
	cause := "The agent container reached its limit of 1024 processes and threads (PID limit), so the harness could not start another and crashed (exit code 139, SIGSEGV)"
	require.NoError(t, repo.FailWithClass(ctx, *claimed, domain.FailurePlatform, cause))
	require.NoError(t, repo.FinishCancellation(ctx, *claimed, "Runtime cancellation confirmed"))

	failed, err := repo.Get(ctx, task.WorkspaceID, task.ID)
	require.NoError(t, err)
	require.Equal(t, "failed", failed.State)
	require.NotNil(t, failed.FailureClass)
	require.Equal(t, string(domain.FailurePlatform), *failed.FailureClass)
	require.Equal(t, cause, failed.Reason)
}

// Without a class the existing specific reasons still decide the final text.
func TestGenericFailureWithoutACauseStaysGeneric(t *testing.T) {
	_, repo, ctx, task, _ := taskRecoveryFixture(t)
	claimed, err := repo.Claim(ctx)
	require.NoError(t, err)
	require.NotNil(t, claimed)
	require.NoError(t, repo.Transition(ctx, *claimed, "preparing", "Preparing"))
	require.NoError(t, repo.Fail(ctx, *claimed, "Native coding iteration failed; stopping execution before retry"))
	require.NoError(t, repo.FinishCancellation(ctx, *claimed, "Runtime cancellation confirmed"))

	failed, err := repo.Get(ctx, task.WorkspaceID, task.ID)
	require.NoError(t, err)
	require.Nil(t, failed.FailureClass)
	require.Equal(t, "Execution failed; runtime cleanup confirmed", failed.Reason)
}
