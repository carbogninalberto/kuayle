package service

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/realtime"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/stretchr/testify/require"
)

type automationNotifierFake struct{ titles []string }

func (f *automationNotifierFake) Create(_ context.Context, _, _ uuid.UUID, _ *uuid.UUID, _, title string, _ ...uuid.UUID) error {
	f.titles = append(f.titles, title)
	return nil
}

// automationRun creates a rule and one run on a fresh ENG issue.
func automationRun(t *testing.T, f toolFixture, status string, actor *uuid.UUID) (*AgentAutomationService, *repository.AgentAutomationRepository, *approvalTaskFake, *automationNotifierFake, uuid.UUID) {
	t.Helper()
	var team, statusID uuid.UUID
	require.NoError(t, f.db.Get(&team, `SELECT id FROM teams WHERE workspace_id=$1`, f.ws))
	require.NoError(t, f.db.Get(&statusID, `SELECT id FROM team_statuses WHERE team_id=$1 LIMIT 1`, team))
	issue := uuid.New()
	_, err := f.db.Exec(`INSERT INTO issues(id,workspace_id,team_id,number,identifier_text,title,creator_id,status_id) VALUES($1,$2,$3,1,'ENG-1','Flaky test',$4,$5)`, issue, f.ws, team, f.user, statusID)
	require.NoError(t, err)
	repo := repository.NewAgentAutomationRepository(f.db)
	automation := &domain.AgentAutomation{WorkspaceID: f.ws, TeamID: team, OwnerID: f.user, Name: "Backlog fixer", StatusID: &statusID, Instructions: "Fix the flaky test", MaxActive: 1}
	require.NoError(t, repo.Create(f.ctx, automation))
	var run uuid.UUID
	require.NoError(t, f.db.Get(&run, `INSERT INTO agent_automation_runs(automation_id,workspace_id,issue_id,status,actor_id) VALUES($1,$2,$3,$4,$5) RETURNING id`, automation.ID, f.ws, issue, status, actor))
	tasks := &approvalTaskFake{repo: repository.NewAgentTaskRepository(f.db)}
	notifier := &automationNotifierFake{}
	return NewAgentAutomationService(repo, f.repo, tasks, notifier, realtime.NewHub()), repo, tasks, notifier, run
}

func TestAutomationRunStartsOneTaskForItsActorAndReplaysAfterACrash(t *testing.T) {
	f := newToolFixture(t)
	svc, repo, tasks, _, id := automationRun(t, f, "starting", &f.user)
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
	_, err = f.db.Exec(`UPDATE agent_automation_runs SET status='starting',task_id=NULL WHERE id=$1`, id)
	require.NoError(t, err)
	svc.start(context.Background(), *run)
	replayed, err := repo.Run(f.ctx, f.ws, id)
	require.NoError(t, err)
	require.Equal(t, *started.TaskID, *replayed.TaskID, "the run's request ID replays the same task")
	var count int
	require.NoError(t, f.db.Get(&count, `SELECT count(*) FROM agent_tasks WHERE workspace_id=$1`, f.ws))
	require.Equal(t, 1, count)
	require.Equal(t, 2, tasks.creates)
}

func TestAutomationRunFailsPermanentlyOnlyForLostAccess(t *testing.T) {
	f := newToolFixture(t)
	svc, repo, tasks, _, id := automationRun(t, f, "starting", &f.user)
	run, err := repo.Run(f.ctx, f.ws, id)
	require.NoError(t, err)
	tasks.optionsErr = errors.New("database unavailable")
	svc.start(context.Background(), *run)
	pending, err := repo.Run(f.ctx, f.ws, id)
	require.NoError(t, err)
	require.Equal(t, "starting", pending.Status, "a transient failure is retried by the worker")

	tasks.optionsErr = nil
	_, err = f.db.Exec(`DELETE FROM workspace_members WHERE workspace_id=$1 AND user_id=$2`, f.ws, f.user)
	require.NoError(t, err)
	svc.start(context.Background(), *run)
	failed, err := repo.Run(f.ctx, f.ws, id)
	require.NoError(t, err)
	require.Equal(t, "failed", failed.Status, "a rule grants nothing once its actor loses access")
	require.Equal(t, "The issue is no longer available to you", *failed.Error)
	require.Zero(t, tasks.creates)
}

func TestApprovingAPendingRunStartsItAsTheApprover(t *testing.T) {
	f := newToolFixture(t)
	svc, _, _, _, id := automationRun(t, f, "pending_approval", nil)
	run, err := svc.Decide(f.ctx, f.ws, id, f.user, false, true)
	require.NoError(t, err)
	require.Equal(t, "started", run.Status)
	require.Equal(t, f.user, *run.ActorID)
	_, err = svc.Decide(f.ctx, f.ws, id, f.user, false, false)
	require.ErrorIs(t, err, repository.ErrAutomationNotFound)
}

func TestNewPendingRunsNotifyTheRuleOwner(t *testing.T) {
	f := newToolFixture(t)
	svc, _, _, notifier, id := automationRun(t, f, "pending_approval", nil)
	run, err := svc.repo.Run(f.ctx, f.ws, id)
	require.NoError(t, err)
	svc.notifyApproval(f.ctx, *run)
	require.Equal(t, []string{"Backlog fixer wants to start a coding task on ENG-1"}, notifier.titles)
}
