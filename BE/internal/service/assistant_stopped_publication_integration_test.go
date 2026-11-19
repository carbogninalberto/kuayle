package service

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/stretchr/testify/require"
)

func stoppedPublicationFixture(t *testing.T) (toolFixture, uuid.UUID) {
	t.Helper()
	f, work, taskID := liveFixture(t)
	checkableTask(t, f, taskID)
	_, err := f.db.Exec(`UPDATE agent_tasks SET state='failed' WHERE id=$1`, taskID)
	require.NoError(t, err)
	_, err = f.db.Exec(`UPDATE dev_machine_agent_runs r SET status='failed',checkout_id=t.checkout_id FROM agent_tasks t WHERE r.id=t.run_id AND t.id=$1`, taskID)
	require.NoError(t, err)
	_, err = f.db.Exec(`DELETE FROM dev_machine_git_refs WHERE agent_run_id=(SELECT run_id FROM agent_tasks WHERE id=$1)`, taskID)
	require.NoError(t, err)
	_, err = f.db.Exec(`INSERT INTO dev_machine_operations(workspace_id,machine_id,agent_run_id,action,status,generation,idempotency_key)
 SELECT workspace_id,machine_id,run_id,'cancel_agent','completed',1,'task-cancel:'||id||':'||run_id FROM agent_tasks WHERE id=$1`, taskID)
	require.NoError(t, err)
	f.service.tools.SetAgentWork(work)
	return f, taskID
}

func TestPublishTaskChangesApprovesOneLeasedPublication(t *testing.T) {
	for _, tc := range []struct {
		state          string
		empty          bool
		noNewNativeRun bool
	}{
		{state: "failed"},
		{state: "cancelled", empty: true},
		{state: "blocked", noNewNativeRun: true},
	} {
		t.Run(tc.state, func(t *testing.T) {
			f, taskID := stoppedPublicationFixture(t)
			_, err := f.db.Exec(`UPDATE agent_tasks SET state=$2 WHERE id=$1`, taskID, tc.state)
			require.NoError(t, err)
			var originalRun uuid.UUID
			require.NoError(t, f.db.Get(&originalRun, `SELECT run_id FROM agent_tasks WHERE id=$1`, taskID))
			if tc.noNewNativeRun {
				_, err := f.db.Exec(`UPDATE agent_tasks SET generation=generation+1,run_id=gen_random_uuid() WHERE id=$1`, taskID)
				require.NoError(t, err)
			}
			result, attempt, approvals := liveToolResult(t, f, domain.PublishTaskChangesAction, `{"identifier":"eng-1"}`)
			require.Contains(t, result, "pending_user_approval")
			require.Len(t, approvals, 1)
			approval := approvals[0]
			var operations int
			require.NoError(t, f.db.Get(&operations, `SELECT count(*) FROM dev_machine_operations WHERE id=$1`, approval.RequestID))
			require.Zero(t, operations)

			for range 2 {
				queued, err := f.service.ResolveApproval(f.ctx, f.ws, attempt.Conversation.ID, approval.ID, true)
				require.NoError(t, err)
				require.Equal(t, "approving", queued.Status)
				require.Equal(t, taskID, *queued.TaskID)
			}
			store := repository.NewDevMachineRepository(f.db)
			ops, err := store.LeaseOperations(f.ctx, "publish-test", 1, time.Minute)
			require.NoError(t, err)
			require.Len(t, ops, 1)
			op := ops[0]
			require.Equal(t, domain.DevMachineOpPublishAgent, op.Action)
			require.Equal(t, approval.RequestID, op.ID)
			require.Equal(t, originalRun, *op.AgentRunID)

			expired := op
			expired.Attempts--
			_, err = store.GetPublication(f.ctx, expired)
			require.ErrorIs(t, err, repository.ErrAgentTaskConflict)
			publication, err := store.GetPublication(f.ctx, op)
			require.NoError(t, err)
			require.True(t, publication.FromStoppedTask)

			const url = "https://github.com/acme/app/pull/27"
			outcome := domain.AgentPublicationResult{}
			if !tc.empty {
				require.NoError(t, store.StartPublication(f.ctx, op))
				outcome = domain.AgentPublicationResult{Branch: "kuayle/eng-1", Commit: reviewedCommit, PullRequestNumber: 27, PullRequestURL: url}
			}
			require.NoError(t, store.CompletePublication(f.ctx, op, outcome))
			require.NoError(t, store.CompleteOperation(f.ctx, op.ID, *op.LeaseOwner))
			decisions, err := f.repo.StoppedPublicationDecisions(f.ctx)
			require.NoError(t, err)
			require.Len(t, decisions, 1)
			finished, err := f.repo.FinishStoppedPublication(f.ctx, decisions[0])
			require.NoError(t, err)
			require.Equal(t, "approved", finished.Status)
			if tc.empty {
				require.NotContains(t, string(finished.Payload), "pull_request_url")
				var summary string
				require.NoError(t, f.db.Get(&summary, `SELECT summary FROM dev_machine_agent_run_steps WHERE id=$1`, op.ID))
				require.Contains(t, summary, "Nothing to publish")
			} else {
				require.Contains(t, string(finished.Payload), url)
				view, err := repository.NewAgentTaskRepository(f.db).Publication(f.ctx, f.ws, taskID, originalRun)
				require.NoError(t, err)
				require.Equal(t, url, *view.PullRequestURL)
			}
			var state string
			require.NoError(t, f.db.Get(&state, `SELECT state FROM agent_tasks WHERE id=$1`, taskID))
			require.Equal(t, tc.state, state)
		})
	}
}

func TestPublishTaskChangesRefusesOthersAndRechecksAdmins(t *testing.T) {
	f, _ := stoppedPublicationFixture(t)
	other := uuid.New()
	_, err := f.db.Exec(`INSERT INTO users(id,email,name,password_hash) VALUES($1,$2,'Other','hash')`, other, other.String()+"@test.invalid")
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = f.db.Exec(`DELETE FROM users WHERE id=$1`, other) })
	_, err = f.db.Exec(`INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,'guest')`, f.ws, other)
	require.NoError(t, err)
	otherUser := f
	otherUser.user = other
	otherUser.ctx = domain.WithAccess(context.Background(), domain.Access{WorkspaceID: f.ws, UserID: other})
	chat := domain.Conversation{WorkspaceID: f.ws, CreatorID: other}
	_, _, err = f.service.prepareStoppedPublication(otherUser.ctx, chat, `{"identifier":"ENG-1"}`)
	require.Contains(t, toolErrorText(err), "Ask one of them")
	_, err = f.db.Exec(`UPDATE workspace_members SET role='member' WHERE workspace_id=$1 AND user_id=$2`, f.ws, other)
	require.NoError(t, err)
	result, _, approvals := liveToolResult(t, otherUser, domain.PublishTaskChangesAction, `{"identifier":"ENG-1"}`)
	require.Contains(t, result, "Ask one of them")
	require.Empty(t, approvals)

	_, err = f.db.Exec(`UPDATE workspace_members SET role='admin' WHERE workspace_id=$1 AND user_id=$2`, f.ws, other)
	require.NoError(t, err)
	_, attempt, approvals := liveToolResult(t, otherUser, domain.PublishTaskChangesAction, `{"identifier":"ENG-1"}`)
	require.Len(t, approvals, 1, "an admin can request publication in their own conversation")
	_, err = f.db.Exec(`UPDATE workspace_members SET role='member' WHERE workspace_id=$1 AND user_id=$2`, f.ws, other)
	require.NoError(t, err)
	refused, err := f.service.ResolveApproval(otherUser.ctx, f.ws, attempt.Conversation.ID, approvals[0].ID, true)
	require.NoError(t, err)
	require.Equal(t, "failed", refused.Status)
	require.Contains(t, *refused.Error, "Ask one of them")
	var operations int
	require.NoError(t, f.db.Get(&operations, `SELECT count(*) FROM dev_machine_operations WHERE id=$1`, approvals[0].RequestID))
	require.Zero(t, operations)

	_, err = f.db.Exec(`UPDATE workspace_members SET role='admin' WHERE workspace_id=$1 AND user_id=$2`, f.ws, other)
	require.NoError(t, err)
	_, attempt, approvals = liveToolResult(t, otherUser, domain.PublishTaskChangesAction, `{"identifier":"ENG-1"}`)
	require.Len(t, approvals, 1)
	approval := approvals[0]
	queued, err := f.service.ResolveApproval(otherUser.ctx, f.ws, attempt.Conversation.ID, approval.ID, true)
	require.NoError(t, err)
	require.Equal(t, "approving", queued.Status)
	store := repository.NewDevMachineRepository(f.db)
	ops, err := store.LeaseOperations(f.ctx, "publish-admin-test", 1, time.Minute)
	require.NoError(t, err)
	require.Len(t, ops, 1)
	_, err = store.GetPublication(f.ctx, ops[0])
	require.NoError(t, err, "an admin can publish with their own approval")
	_, err = f.db.Exec(`UPDATE workspace_members SET role='member' WHERE workspace_id=$1 AND user_id=$2`, f.ws, other)
	require.NoError(t, err)
	require.ErrorIs(t, store.StartPublication(f.ctx, ops[0]), repository.ErrStoppedPublicationForbidden)
}

func TestPublishTaskChangesRefusesMissingCheckout(t *testing.T) {
	f, taskID := stoppedPublicationFixture(t)
	_, attempt, approvals := liveToolResult(t, f, domain.PublishTaskChangesAction, `{"identifier":"ENG-1"}`)
	require.Len(t, approvals, 1)
	_, err := f.db.Exec(`UPDATE dev_machine_checkouts SET status='failed' WHERE id=(SELECT checkout_id FROM agent_tasks WHERE id=$1)`, taskID)
	require.NoError(t, err)
	result, err := f.service.ResolveApproval(f.ctx, f.ws, attempt.Conversation.ID, approvals[0].ID, true)
	require.NoError(t, err)
	require.Equal(t, "failed", result.Status)
	require.Contains(t, *result.Error, "Start its Dev Machine")
	var operations int
	require.NoError(t, f.db.Get(&operations, `SELECT count(*) FROM dev_machine_operations WHERE id=$1`, approvals[0].RequestID))
	require.Zero(t, operations)
}
