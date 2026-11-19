package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/agent"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/stretchr/testify/require"
)

// automationRun creates a rule and one run on a fresh ENG issue.
func automationRun(t *testing.T, f toolFixture, status string, actor *uuid.UUID) (*AgentAutomationService, *repository.AgentAutomationRepository, *approvalTaskFake, uuid.UUID) {
	t.Helper()
	var team, statusID uuid.UUID
	require.NoError(t, f.db.Get(&team, `SELECT id FROM teams WHERE workspace_id=$1`, f.ws))
	require.NoError(t, f.db.Get(&statusID, `SELECT id FROM team_statuses WHERE team_id=$1 LIMIT 1`, team))
	issue := uuid.New()
	_, err := f.db.Exec(`INSERT INTO issues(id,workspace_id,team_id,number,identifier_text,title,creator_id,status_id) VALUES($1,$2,$3,1,'ENG-1','Flaky test',$4,$5)`, issue, f.ws, team, f.user, statusID)
	require.NoError(t, err)
	repo := repository.NewAgentAutomationRepository(f.db)
	automation := &domain.AgentAutomation{WorkspaceID: f.ws, TeamID: team, OwnerID: f.user, Name: "Backlog fixer", StatusID: &statusID, Instructions: "Fix the flaky test", MaxActive: 1}
	require.NoError(t, repo.Create(f.ctx, automation, false))
	var run uuid.UUID
	require.NoError(t, f.db.Get(&run, `INSERT INTO agent_automation_runs(automation_id,workspace_id,issue_id,status,actor_id) VALUES($1,$2,$3,$4,$5) RETURNING id`, automation.ID, f.ws, issue, status, actor))
	tasks := &approvalTaskFake{repo: repository.NewAgentTaskRepository(f.db)}
	return NewAgentAutomationService(repo, f.repo, tasks), repo, tasks, run
}

func TestAutomationRunStartsOneTaskForItsActorAndReplaysAfterACrash(t *testing.T) {
	f := newToolFixture(t)
	svc, repo, tasks, id := automationRun(t, f, "starting", &f.user)
	run, err := repo.Run(f.ctx, f.ws, id)
	require.NoError(t, err)
	svc.start(context.Background(), *run)
	started, err := repo.Run(f.ctx, f.ws, id)
	require.NoError(t, err)
	require.Equal(t, "started", started.Status)
	require.NotNil(t, started.TaskID)
	var task domain.AgentTask
	require.NoError(t, f.db.Get(&task, `SELECT * FROM agent_tasks WHERE id=$1`, *started.TaskID))
	require.Equal(t, f.user, task.CreatorID, "the task acts for the run's actor")
	require.Equal(t, "Fix the flaky test", task.Prompt)
	require.Equal(t, "codex", task.Provider)

	// A crash after the task was accepted but before the run was settled.
	_, err = f.db.Exec(`UPDATE agent_automation_runs SET status='starting',task_id=NULL,available_at=now() WHERE id=$1`, id)
	require.NoError(t, err)
	svc.start(context.Background(), *run)
	replayed, err := repo.Run(f.ctx, f.ws, id)
	require.NoError(t, err)
	require.Equal(t, *started.TaskID, *replayed.TaskID, "the run's request ID replays the same task")
	var count int
	require.NoError(t, f.db.Get(&count, `SELECT count(*) FROM agent_tasks WHERE workspace_id=$1`, f.ws))
	require.Equal(t, 1, count)
	require.Equal(t, 1, tasks.creates, "the worker recovers the committed task receipt before delegation")
}

func TestAutomationRunFailsPermanentlyOnlyForLostAccess(t *testing.T) {
	f := newToolFixture(t)
	svc, repo, tasks, id := automationRun(t, f, "starting", &f.user)
	run, err := repo.Run(f.ctx, f.ws, id)
	require.NoError(t, err)
	tasks.optionsErr = errors.New("database unavailable")
	svc.start(context.Background(), *run)
	pending, err := repo.Run(f.ctx, f.ws, id)
	require.NoError(t, err)
	require.Equal(t, "starting", pending.Status, "a transient failure is retried by the worker")

	tasks.optionsErr = nil
	_, err = f.db.Exec(`UPDATE agent_automation_runs SET available_at=now() WHERE id=$1`, id)
	require.NoError(t, err)
	_, err = f.db.Exec(`DELETE FROM workspace_members WHERE workspace_id=$1 AND user_id=$2`, f.ws, f.user)
	require.NoError(t, err)
	svc.start(context.Background(), *run)
	_, err = repo.Run(f.ctx, f.ws, id)
	require.ErrorIs(t, err, repository.ErrAutomationNotFound, "the former member cannot read the run")
	var failed domain.AgentAutomationRun
	require.NoError(t, f.db.Get(&failed, `SELECT * FROM agent_automation_runs WHERE id=$1`, id))
	require.Equal(t, "failed", failed.Status, "a rule grants nothing once its actor loses access")
	require.Equal(t, "The issue is no longer available to you", *failed.Error)
	require.Zero(t, tasks.creates)
}

func TestApprovingAPendingRunStartsItAsTheApprover(t *testing.T) {
	f := newToolFixture(t)
	svc, _, _, id := automationRun(t, f, "pending_approval", nil)
	run, err := svc.Decide(f.ctx, f.ws, id, f.user, false, true)
	require.NoError(t, err)
	require.Equal(t, "started", run.Status)
	require.Equal(t, f.user, *run.ActorID)
	_, err = svc.Decide(f.ctx, f.ws, id, f.user, false, false)
	require.ErrorIs(t, err, repository.ErrAutomationNotFound)
}

func TestAutomationDelegatesPinnedOrLatestPublishedVersion(t *testing.T) {
	for _, policy := range []string{"pin", "latest"} {
		t.Run(policy, func(t *testing.T) {
			f := newToolFixture(t)
			_, err := f.db.Exec(`UPDATE workspace_members SET role='admin' WHERE workspace_id=$1 AND user_id=$2`, f.ws, f.user)
			require.NoError(t, err)
			var team, status, issue uuid.UUID
			require.NoError(t, f.db.Get(&team, `SELECT id FROM teams WHERE workspace_id=$1`, f.ws))
			require.NoError(t, f.db.Get(&status, `SELECT id FROM team_statuses WHERE team_id=$1 LIMIT 1`, team))
			require.NoError(t, f.db.Get(&issue, `INSERT INTO issues(workspace_id,team_id,number,identifier_text,title,creator_id,status_id)
			 VALUES($1,$2,1,'ENG-1','Check the change',$3,$4) RETURNING id`, f.ws, team, f.user, status))

			definitions := repository.NewWorkflowRepository(f.db)
			require.NoError(t, definitions.EnsureBuiltinProfiles(f.ctx, f.ws))
			profiles, err := definitions.Profiles(f.ctx, f.ws)
			require.NoError(t, err)
			graph, err := json.Marshal(domain.ReviewLoopWorkflowGraph(profiles[0].ID, profiles[1].ID))
			require.NoError(t, err)
			first, err := definitions.CreateWorkflow(f.ctx, f.ws, f.user, "Review loop", "", graph, "created")
			require.NoError(t, err)

			machines := repository.NewDevMachineRepository(f.db)
			require.NoError(t, machines.UpsertPolicy(f.ctx, &domain.DevMachineWorkspacePolicy{WorkspaceID: f.ws, Enabled: true, MaxConcurrentMachines: 5, MaxMachinesPerUser: 2, MaxDailyAgentRuns: 25, MaxDiskGB: 100, AllowedProviders: json.RawMessage(`["claude-code"]`), AllowedRepositories: json.RawMessage(`[]`)}))
			taskSvc := NewAgentTaskService(repository.NewAgentTaskRepository(f.db), NewDevMachineService(machines, agent.NewRegistry(agent.NewClaudeCodeProvider("test")), true, "", nil, time.Minute, DevMachineImages{}), nil)
			taskSvc.SetWorkflows(definitions)
			repo := repository.NewAgentAutomationRepository(f.db)
			svc := NewAgentAutomationService(repo, f.repo, taskSvc)
			input := AutomationInput{TeamID: team, StatusID: &status, Name: "Triage", Instructions: "Fix the issue", WorkflowID: &first.ID, VersionPolicy: policy}
			rule, err := svc.Create(f.ctx, f.ws, f.user, input)
			require.NoError(t, err)
			second, err := definitions.SaveVersion(f.ctx, f.ws, first.ID, f.user, 1, "Review loop", "Revised", graph)
			require.NoError(t, err)
			var run uuid.UUID
			require.NoError(t, f.db.Get(&run, `INSERT INTO agent_automation_runs(automation_id,workspace_id,issue_id,status) VALUES($1,$2,$3,'pending_approval') RETURNING id`, rule.ID, f.ws, issue))
			started, err := svc.Decide(f.ctx, f.ws, run, f.user, false, true)
			require.NoError(t, err)
			require.Equal(t, "started", started.Status, started.Error)
			var task domain.AgentTask
			require.NoError(t, f.db.Get(&task, `SELECT * FROM agent_tasks WHERE id=$1`, started.TaskID))
			want := second.VersionID
			if policy == "pin" {
				want = first.VersionID
			}
			require.Equal(t, want, task.WorkflowVersionID)
			require.NoError(t, definitions.ArchiveWorkflow(f.ctx, f.ws, first.ID, f.user))
			_, err = svc.Create(f.ctx, f.ws, f.user, input)
			require.ErrorIs(t, err, ErrInvalidAutomation)
		})
	}
}

func TestAutomationStopsRetryingAndCanReplayOnlyAsOriginalActor(t *testing.T) {
	f := newToolFixture(t)
	svc, repo, tasks, id := automationRun(t, f, "starting", &f.user)
	tasks.optionsErr = errors.New("temporary storage failure")
	for attempt := 1; attempt <= 3; attempt++ {
		run, err := repo.Run(f.ctx, f.ws, id)
		require.NoError(t, err)
		svc.start(f.ctx, *run)
		if attempt < 3 {
			_, err = f.db.Exec(`UPDATE agent_automation_runs SET available_at=now() WHERE id=$1`, id)
			require.NoError(t, err)
		}
	}
	failed, err := repo.Run(f.ctx, f.ws, id)
	require.NoError(t, err)
	require.Equal(t, "failed", failed.Status)
	require.NotEmpty(t, failed.Error)
	_, err = svc.Replay(f.ctx, f.ws, id, uuid.New(), true)
	require.ErrorIs(t, err, repository.ErrAutomationNotFound)
	replayed, err := svc.Replay(f.ctx, f.ws, id, f.user, false)
	require.NoError(t, err)
	require.Equal(t, failed.RequestID, replayed.RequestID)
	require.Equal(t, 1, replayed.Replays)
	tasks.optionsErr = nil
	svc.start(f.ctx, *replayed)
	started, err := repo.Run(f.ctx, f.ws, id)
	require.NoError(t, err)
	require.Equal(t, "started", started.Status)
}
