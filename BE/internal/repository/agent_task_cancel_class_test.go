package repository

import (
	"testing"

	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
)

// The transaction that stops an attempt records who stopped it, where the
// reason is known: a person's cancel, pause or message, a policy refusal, or
// the cleanup of an attempt that already failed (which keeps its own cause).
func TestCancellingAnAttemptRecordsWhoStoppedIt(t *testing.T) {
	for name, tc := range map[string]struct {
		setup string
		class domain.FailureClass
		cause string
	}{
		"a person's cancel": {
			setup: `UPDATE agent_tasks SET state='cancelling',reason='Cancellation requested' WHERE id=$1`,
			class: domain.FailurePerson, cause: "cancelled by a person",
		},
		"a person's pause": {
			setup: `UPDATE agent_tasks SET state='cancelling',workflow_paused_at=now(),reason='Pausing: stopping the agent' WHERE id=$1`,
			class: domain.FailurePerson, cause: "paused",
		},
		"a message interrupted it": {
			setup: `UPDATE agent_tasks SET state='cancelling',live_resume_generation=generation+1,reason='Interrupting: stopping the agent to deliver a message' WHERE id=$1`,
			class: domain.FailurePerson, cause: "message",
		},
		"the policy no longer allows it": {
			setup: `UPDATE agent_tasks SET state='cancelling',reason='Issue access was revoked' WHERE id=$1`,
			class: domain.FailurePolicy, cause: "policy",
		},
		"the cleanup of a failed attempt keeps its cause": {
			setup: `UPDATE agent_tasks SET state='cancelling',failure_pending=true,failure_class='provider',reason='The provider reported an error' WHERE id=$1`,
			class: domain.FailureProvider, cause: "The provider reported an error",
		},
	} {
		t.Run(name, func(t *testing.T) {
			db, repo, ctx, task, fixture := taskRecoveryFixture(t)
			claimed := prepareTaskStart(t, db, repo, ctx, task, fixture)
			_, err := db.Exec(tc.setup, task.ID)
			require.NoError(t, err)
			if tc.class == domain.FailureProvider {
				_, err = db.Exec(`UPDATE dev_machine_agent_runs SET failure_class='provider',failure_rule='usage_limit',failure_cause='The provider reported an error' WHERE id=$1`, claimed.RunID)
				require.NoError(t, err)
			}
			require.NoError(t, repo.Release(ctx, *claimed))
			_, err = db.Exec(`UPDATE agent_tasks SET next_poll_at=now() WHERE id=$1`, task.ID)
			require.NoError(t, err)
			claimed, err = repo.Claim(ctx)
			require.NoError(t, err)
			require.NotNil(t, claimed)
			require.Equal(t, task.ID, claimed.ID)
			require.Equal(t, "cancelling", claimed.State)

			require.NoError(t, repo.CancelAttempt(ctx, *claimed))
			var class, cause, status string
			require.NoError(t, db.QueryRow(`SELECT COALESCE(failure_class,''),COALESCE(failure_cause,''),status FROM dev_machine_agent_runs WHERE id=$1`, claimed.RunID).Scan(&class, &cause, &status))
			require.Equal(t, string(tc.class), class)
			require.Contains(t, cause, tc.cause)
			require.Equal(t, "cancelled", status)
		})
	}
}
