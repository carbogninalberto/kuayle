package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/stretchr/testify/require"
)

// approvalWorkflows is a delegation awaiting approval in a workspace that has
// a usable custom workflow, an archived one, and another workspace's.
type approvalWorkflows struct {
	toolFixture
	conversation, approval          uuid.UUID
	tasks                           *approvalTaskFake
	loop, review, archived, foreign uuid.UUID
}

func newApprovalWorkflows(t *testing.T) approvalWorkflows {
	t.Helper()
	f := newToolFixture(t)
	a, approval, tasks := requestDelegation(t, f)
	w := approvalWorkflows{toolFixture: f, conversation: a.Conversation.ID, approval: approval.ID, tasks: tasks,
		loop: uuid.New(), review: uuid.New(), archived: uuid.New(), foreign: uuid.New()}
	other, now := uuid.New(), time.Now()
	f.service.approvals.SetWorkflows(delegationWorkflows{
		f.ws: {
			{Workflow: domain.Workflow{ID: w.loop, WorkspaceID: f.ws, Name: "Implement with review loop"}},
			{Workflow: domain.Workflow{ID: w.review, WorkspaceID: f.ws, Name: "Review only"}},
			{Workflow: domain.Workflow{ID: w.archived, WorkspaceID: f.ws, Name: "Old flow", ArchivedAt: &now}},
		},
		other: {{Workflow: domain.Workflow{ID: w.foreign, WorkspaceID: other, Name: "Foreign"}}},
	})
	f.service.approvals.members = prMembers{f.user: domain.RoleMember}
	return w
}

func (w approvalWorkflows) approve(workflow string) (*domain.ConversationApproval, error) {
	return w.service.ResolveApprovalDecision(w.ctx, w.ws, w.conversation, w.approval, true, ModelChoice{}, &ApprovalEdit{WorkflowID: &workflow})
}

func (w approvalWorkflows) stored(t *testing.T) (string, domain.DelegationPayload) {
	t.Helper()
	var row struct {
		Status  string `db:"status"`
		Payload []byte `db:"payload"`
	}
	require.NoError(t, w.db.Get(&row, `SELECT status,payload FROM conversation_approvals WHERE id=$1`, w.approval))
	var payload domain.DelegationPayload
	require.NoError(t, json.Unmarshal(row.Payload, &payload))
	return row.Status, payload
}

func (w approvalWorkflows) taskCount(t *testing.T) int {
	t.Helper()
	var count int
	require.NoError(t, w.db.Get(&count, `SELECT count(*) FROM agent_tasks WHERE workspace_id=$1`, w.ws))
	return count
}

// The workflow the approver picks on the card is the one the task follows,
// whatever the assistant proposed, and the card then shows what ran.
func TestApprovedDelegationRunsTheApproversWorkflow(t *testing.T) {
	proposed := func(w approvalWorkflows) {
		// The assistant proposed a workflow before the approver chose.
		_, err := w.db.Exec(`UPDATE conversation_approvals SET payload=payload||jsonb_build_object('workflow_id',$2::text,'workflow','Review only') WHERE id=$1`, w.approval, w.review.String())
		require.NoError(t, err)
	}
	for name, c := range map[string]struct {
		prepare          func(w approvalWorkflows)
		chosen           func(w approvalWorkflows) string
		wantID, wantName func(w approvalWorkflows) string
	}{
		"standard": {
			chosen: func(approvalWorkflows) string { return "standard" },
			wantID: func(approvalWorkflows) string { return domain.StandardWorkflowID }, wantName: func(approvalWorkflows) string { return "Standard" },
		},
		"custom by ID": {
			chosen: func(w approvalWorkflows) string { return w.loop.String() },
			wantID: func(w approvalWorkflows) string { return w.loop.String() }, wantName: func(approvalWorkflows) string { return "Implement with review loop" },
		},
		"custom by name over a proposal": {
			prepare: proposed,
			chosen:  func(approvalWorkflows) string { return "implement with review loop" },
			wantID:  func(w approvalWorkflows) string { return w.loop.String() }, wantName: func(approvalWorkflows) string { return "Implement with review loop" },
		},
		"issue default over a proposal": {
			prepare: proposed,
			chosen:  func(approvalWorkflows) string { return "" },
			wantID:  func(approvalWorkflows) string { return "" }, wantName: func(approvalWorkflows) string { return "" },
		},
	} {
		t.Run(name, func(t *testing.T) {
			w := newApprovalWorkflows(t)
			if c.prepare != nil {
				c.prepare(w)
			}
			done, err := w.approve(c.chosen(w))
			require.NoError(t, err)
			require.Equal(t, "approved", done.Status, "error: %v", done.Error)
			require.NotNil(t, done.TaskID)
			require.Len(t, w.tasks.requests, 1)
			if c.wantID(w) == "" {
				require.Nil(t, w.tasks.requests[0].WorkflowID, "the issue's default workflow runs")
			} else {
				require.NotNil(t, w.tasks.requests[0].WorkflowID)
				require.Equal(t, c.wantID(w), *w.tasks.requests[0].WorkflowID)
			}
			_, payload := w.stored(t)
			require.Equal(t, c.wantID(w), payload.WorkflowID)
			require.Equal(t, c.wantName(w), payload.Workflow)
			require.Equal(t, "Implement dark mode and add tests", payload.Instructions, "the rest of the proposal is kept")
		})
	}
}

// Without a workflow in the decision the assistant's proposal still runs.
func TestApprovalWithoutAWorkflowKeepsTheProposal(t *testing.T) {
	w := newApprovalWorkflows(t)
	_, err := w.db.Exec(`UPDATE conversation_approvals SET payload=payload||jsonb_build_object('workflow_id',$2::text,'workflow','Review only') WHERE id=$1`, w.approval, w.review.String())
	require.NoError(t, err)
	done, err := w.service.ResolveApprovalDecision(w.ctx, w.ws, w.conversation, w.approval, true, ModelChoice{}, &ApprovalEdit{})
	require.NoError(t, err)
	require.Equal(t, "approved", done.Status, "error: %v", done.Error)
	require.Len(t, w.tasks.requests, 1)
	require.NotNil(t, w.tasks.requests[0].WorkflowID)
	require.Equal(t, w.review.String(), *w.tasks.requests[0].WorkflowID)
}

// A workflow the assistant could not have chosen is refused, with a message
// the approver can act on, and nothing starts: the approval stays pending.
func TestApprovalRefusesAForbiddenWorkflow(t *testing.T) {
	for name, chosen := range map[string]func(w approvalWorkflows) string{
		"archived":          func(w approvalWorkflows) string { return w.archived.String() },
		"another workspace": func(w approvalWorkflows) string { return w.foreign.String() },
		"missing":           func(approvalWorkflows) string { return uuid.NewString() },
		"unknown name":      func(approvalWorkflows) string { return "no such flow" },
	} {
		t.Run(name, func(t *testing.T) {
			w := newApprovalWorkflows(t)
			_, err := w.approve(chosen(w))
			var invalid *ApprovalEditError
			require.True(t, errors.As(err, &invalid), "got %v", err)
			require.Contains(t, invalid.Message, "Workflow not found")
			status, payload := w.stored(t)
			require.Equal(t, "pending", status)
			require.Empty(t, payload.WorkflowID)
			require.Zero(t, w.tasks.creates)
			require.Zero(t, w.taskCount(t))

			// The approver can still pick a valid workflow afterwards.
			done, err := w.approve(w.loop.String())
			require.NoError(t, err)
			require.Equal(t, "approved", done.Status, "error: %v", done.Error)
		})
	}

	t.Run("guest", func(t *testing.T) {
		w := newApprovalWorkflows(t)
		w.service.approvals.members = prMembers{w.user: domain.RoleGuest}
		_, err := w.approve("standard")
		var invalid *ApprovalEditError
		require.True(t, errors.As(err, &invalid), "got %v", err)
		require.Equal(t, "You no longer have access to this workspace", invalid.Message)
		status, _ := w.stored(t)
		require.Equal(t, "pending", status)
		require.Zero(t, w.tasks.creates)
	})

	t.Run("custom workflows not wired", func(t *testing.T) {
		w := newApprovalWorkflows(t)
		w.service.approvals.workflows = nil
		_, err := w.approve(w.loop.String())
		var invalid *ApprovalEditError
		require.True(t, errors.As(err, &invalid), "got %v", err)
		status, _ := w.stored(t)
		require.Equal(t, "pending", status)
		done, err := w.approve("standard")
		require.NoError(t, err, "Standard needs no workflow list")
		require.Equal(t, "approved", done.Status, "error: %v", done.Error)
	})

	t.Run("lister failure is not a refusal", func(t *testing.T) {
		w := newApprovalWorkflows(t)
		w.service.approvals.SetWorkflows(failingWorkflows{})
		_, err := w.approve(w.loop.String())
		require.Error(t, err)
		var invalid *ApprovalEditError
		require.False(t, errors.As(err, &invalid), "a database failure is retried, not shown as an invalid choice")
		status, _ := w.stored(t)
		require.Equal(t, "pending", status)
	})
}

// The approver picks a pipeline on the card only while the server has
// pipelines on (KUA-291): off, the choice is refused with the reason and the
// approval stays pending; on, the approved task runs it.
func TestApprovalChoosesAPipelineOnlyWhilePipelinesAreOn(t *testing.T) {
	w := newApprovalWorkflows(t)
	team := uuid.New()
	raw, err := json.Marshal(domain.TeamPipeline(uuid.New(), uuid.New()))
	require.NoError(t, err)
	w.service.approvals.SetWorkflows(delegationWorkflows{w.ws: {{Workflow: domain.Workflow{ID: team, WorkspaceID: w.ws, Name: "Team"}, Graph: raw}}})

	_, err = w.approve("Team")
	var invalid *ApprovalEditError
	require.True(t, errors.As(err, &invalid), "got %v", err)
	require.Equal(t, "Team is a pipeline, and pipeline workflows are turned off on this server; omit workflow to use the issue's default, or name another workflow", invalid.Message)
	status, payload := w.stored(t)
	require.Equal(t, "pending", status)
	require.Empty(t, payload.WorkflowID)
	require.Zero(t, w.tasks.creates)

	w.tasks.pipelines = true
	done, err := w.approve(team.String())
	require.NoError(t, err)
	require.Equal(t, "approved", done.Status, "error: %v", done.Error)
	_, payload = w.stored(t)
	require.Equal(t, team.String(), payload.WorkflowID)
	require.Equal(t, "Team", payload.Workflow)
	require.NotNil(t, w.tasks.requests[0].WorkflowID)
	require.Equal(t, team.String(), *w.tasks.requests[0].WorkflowID)
}

// Only a delegation has a workflow to choose; any other approval refuses it.
func TestApprovalWorkflowIsRefusedOnOtherActions(t *testing.T) {
	w := newApprovalWorkflows(t)
	_, err := w.db.Exec(`UPDATE conversation_approvals SET action='accept_agent_work' WHERE id=$1`, w.approval)
	require.NoError(t, err)
	_, err = w.approve("standard")
	var invalid *ApprovalEditError
	require.True(t, errors.As(err, &invalid), "got %v", err)
	require.Equal(t, "Only a delegation's workflow can be chosen before approving", invalid.Message)
	status, _ := w.stored(t)
	require.Equal(t, "pending", status)
}

// Approving again with another workflow converges on the task the first
// approval started: the claim stored the first choice, and the request ID is
// fixed.
func TestApprovalReplayWithAnotherWorkflowStartsNoSecondTask(t *testing.T) {
	w := newApprovalWorkflows(t)
	first, err := w.approve(w.loop.String())
	require.NoError(t, err)
	require.Equal(t, "approved", first.Status, "error: %v", first.Error)
	require.NotNil(t, first.TaskID)

	for _, other := range []string{w.review.String(), "standard", ""} {
		again, err := w.approve(other)
		require.NoError(t, err)
		require.NotNil(t, again.TaskID)
		require.Equal(t, *first.TaskID, *again.TaskID)
	}
	require.Equal(t, 1, w.taskCount(t))
	_, payload := w.stored(t)
	require.Equal(t, w.loop.String(), payload.WorkflowID, "the approved choice is the one that ran")
	for _, request := range w.tasks.requests {
		require.NotNil(t, request.WorkflowID)
		require.Equal(t, w.loop.String(), *request.WorkflowID)
	}

	// A claim left in `approving` by a failed start resumes with the stored
	// choice, not the later one.
	w = newApprovalWorkflows(t)
	w.tasks.createErr = errors.New("database briefly unavailable")
	_, err = w.approve(w.loop.String())
	require.Error(t, err)
	status, _ := w.stored(t)
	require.Equal(t, "approving", status)
	w.tasks.createErr = nil
	resumed, err := w.approve(w.review.String())
	require.NoError(t, err)
	require.Equal(t, "approved", resumed.Status, "error: %v", resumed.Error)
	require.Equal(t, 1, w.taskCount(t))
	last := w.tasks.requests[len(w.tasks.requests)-1]
	require.NotNil(t, last.WorkflowID)
	require.Equal(t, w.loop.String(), *last.WorkflowID)
}

type failingWorkflows struct{}

func (failingWorkflows) Workflows(_ context.Context, _ uuid.UUID) ([]repository.WorkflowDetail, error) {
	return nil, errors.New("database unavailable")
}
