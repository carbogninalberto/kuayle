package repository

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
)

// The agent view's timeline needs each run on the workflow step that started
// it: an agent step's own runs, and the runs of the review task a reviewer
// step created. Only who can read the task reads them.
func TestStepRunsPlaceEachRunOnItsWorkflowStep(t *testing.T) {
	db, repo, ctx, legacy, fixture := taskRecoveryFixture(t)
	ws := legacy.WorkspaceID
	machine := fixture.insertMachine(t, domain.DevMachineStatusRunning, domain.DevMachineStatusRunning, false)
	insertRun := func(task uuid.UUID, provider, model, status, summary, result, offset string) uuid.UUID {
		t.Helper()
		var id uuid.UUID
		require.NoError(t, db.Get(&id, `INSERT INTO dev_machine_agent_runs(workspace_id,machine_id,task_id,requested_by_user_id,provider_id,model,mode,prompt,command_argv,max_runtime_seconds,status,summary,result,encrypted_output_secrets,created_at,started_at,completed_at)
 VALUES($1,$2,$3,$4,$5,$6,'autonomous','Secret prompt','["agent"]',60,$7::dev_machine_agent_run_status,NULLIF($8,''),NULLIF($9,'')::jsonb,'ciphertext-never-expose',now()+$10::interval,now()+$10::interval,CASE WHEN $7='running' THEN NULL ELSE now()+$10::interval+interval '90 seconds' END) RETURNING id`,
			ws, machine, task, fixture.userID, provider, model, status, summary, result, offset))
		return id
	}

	// A Standard task has no steps: its runs are returned without one.
	standard := insertRun(legacy.ID, "codex", "", "succeeded", "Done", `{"final_message":true}`, "0 seconds")
	runs, err := repo.StepRuns(ctx, ws, legacy.ID, nil)
	require.NoError(t, err)
	require.Len(t, runs, 1)
	require.Equal(t, standard, runs[0].ID)
	require.Nil(t, runs[0].Step)
	require.Equal(t, "Done", *runs[0].Summary)
	// Only the newest 200 runs of a task are listed.
	_, err = db.Exec(`INSERT INTO dev_machine_agent_runs(workspace_id,machine_id,task_id,provider_id,mode,prompt,command_argv,max_runtime_seconds,status,created_at)
 SELECT $1,$2,$3,'codex','autonomous','Old','["agent"]',60,'failed',now()-make_interval(mins=>n) FROM generate_series(1,205) n`, ws, machine, legacy.ID)
	require.NoError(t, err)
	runs, err = repo.StepRuns(ctx, ws, legacy.ID, nil)
	require.NoError(t, err)
	require.Len(t, runs, 200)
	require.Equal(t, standard, runs[199].ID, "the newest ones, oldest first")
	_, err = db.Exec(`UPDATE agent_tasks SET state='completed' WHERE id=$1`, legacy.ID)
	require.NoError(t, err)

	workflows := NewWorkflowRepository(db)
	implementer, reviewer := builtinIDs(t, workflows, ws)
	raw, err := json.Marshal(domain.ReviewLoopWorkflowGraph(implementer, reviewer))
	require.NoError(t, err)
	created, err := workflows.CreateWorkflow(ctx, ws, fixture.userID, "Review loop", "", raw, "created")
	require.NoError(t, err)
	chosen := created.ID.String()
	version, err := workflows.ResolveWorkflow(ctx, ws, legacy.IssueID, &chosen)
	require.NoError(t, err)
	task, err := repo.Create(ctx, ws, *legacy.ConversationID, domain.AgentTaskRequest{RequestID: uuid.New(), Prompt: "Add dark mode", Provider: "claude-code", Workflow: version})
	require.NoError(t, err)

	// Step 2 is the delegated iteration. A usage-limit resume gives the same
	// step a second run; a later agent step starts the next one.
	first := insertRun(task.ID, "claude-code", "opus", "failed", "limit reached", "", "1 minute")
	resumed := insertRun(task.ID, "claude-code", "opus", "succeeded", "Planned the change", `{"final_message":true}`, "5 minutes")
	var review uuid.UUID
	require.NoError(t, db.Get(&review, `INSERT INTO agent_tasks(workspace_id,conversation_id,creator_id,issue_id,team_id,request_id,prompt,provider,model,kind,review_of_task_id,state)
 VALUES($1,$2,$3,$4,$5,gen_random_uuid(),'Review it','codex','gpt-5','review',$6,'completed') RETURNING id`, ws, task.ConversationID, task.CreatorID, task.IssueID, task.TeamID, task.ID))
	_, err = db.Exec(`INSERT INTO agent_task_workflow_steps(task_id,sequence,node_id,node_type,visit,reason,generation,review_task_id,entered_at) VALUES
 ($1,3,'publish','publish',1,'Agent finished',NULL,NULL,now()+interval '10 minutes'),
 ($1,4,'review','agent',1,'Published',NULL,$2,now()+interval '11 minutes'),
 ($1,5,'implement','agent',2,'Changes requested',2,NULL,now()+interval '20 minutes')`, task.ID, review)
	require.NoError(t, err)
	reviewing := insertRun(review, "codex", "gpt-5", "succeeded", "Two findings", `{"final_message":true}`, "12 minutes")
	second := insertRun(task.ID, "opencode", "kimi", "running", "partial raw output", "", "21 minutes")
	// Another task's runs never appear, even when it reviews this one without
	// a step of this task naming it.
	var stray uuid.UUID
	require.NoError(t, db.Get(&stray, `INSERT INTO agent_tasks(workspace_id,conversation_id,creator_id,issue_id,team_id,request_id,prompt,provider,kind,review_of_task_id,state)
 VALUES($1,$2,$3,$4,$5,gen_random_uuid(),'Requested review','codex','review',$6,'completed') RETURNING id`, ws, task.ConversationID, task.CreatorID, task.IssueID, task.TeamID, task.ID))
	insertRun(stray, "codex", "", "succeeded", "Unrelated", "", "13 minutes")

	runs, err = repo.StepRuns(ctx, ws, task.ID, nil)
	require.NoError(t, err)
	require.Len(t, runs, 4)
	type placed struct {
		id, task uuid.UUID
		step     int
	}
	got := []placed{}
	for _, run := range runs {
		require.NotNil(t, run.Step)
		got = append(got, placed{run.ID, run.TaskID, *run.Step})
	}
	require.Equal(t, []placed{{first, task.ID, 2}, {resumed, task.ID, 2}, {reviewing, review, 4}, {second, task.ID, 5}}, got, "oldest first, each on its step")
	require.Nil(t, runs[0].Summary, "raw output is not a summary")
	require.Equal(t, "Planned the change", *runs[1].Summary)
	require.Equal(t, "opus", runs[1].Model)
	require.Equal(t, "codex", runs[2].Provider)
	require.Equal(t, "running", runs[3].Status)
	require.Nil(t, runs[3].CompletedAt)
	require.Nil(t, runs[3].Summary)
	// A view that already has the runs asks only for the summaries of runs
	// finished since: a finished run's summary never changes.
	since, err := repo.StepRuns(ctx, ws, task.ID, runs[1].CompletedAt)
	require.NoError(t, err)
	require.Len(t, since, 4)
	require.Nil(t, since[1].Summary, "already known to the reader")
	require.Equal(t, "Two findings", *since[2].Summary, "finished after what the reader has")
	encoded, err := json.Marshal(runs)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "ciphertext-never-expose")
	require.NotContains(t, string(encoded), "Secret prompt")

	// Another member of the workspace, a person of another workspace and the
	// creator after losing the issue's team cannot read them.
	member := addWorkflowMember(t, db, ws, "admin")
	_, err = repo.StepRuns(domain.WithAccess(context.Background(), domain.Access{WorkspaceID: ws, UserID: member}), ws, task.ID, nil)
	require.ErrorIs(t, err, ErrConversationNotFound)
	other := uuid.New()
	_, err = db.Exec(`INSERT INTO workspaces(id,name,slug,owner_id) VALUES($1,'Other',$2,$3)`, other, other.String(), member)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,'owner')`, other, member)
	require.NoError(t, err)
	outsider := domain.WithAccess(context.Background(), domain.Access{WorkspaceID: other, UserID: member})
	_, err = repo.StepRuns(outsider, other, task.ID, nil)
	require.ErrorIs(t, err, ErrConversationNotFound)
	_, err = repo.StepRuns(outsider, ws, task.ID, nil)
	require.Error(t, err)
	// The team becomes private and the creator, now an ordinary member, is
	// not in it (the guard against private teams in a workspace with machines
	// is not what is tested here).
	_, err = db.Exec(`ALTER TABLE teams DISABLE TRIGGER USER`)
	require.NoError(t, err)
	_, err = db.Exec(`UPDATE workspace_members SET role='member' WHERE workspace_id=$1 AND user_id=$2`, ws, task.CreatorID)
	require.NoError(t, err)
	_, err = db.Exec(`UPDATE teams SET is_private=true WHERE id=$1`, task.TeamID)
	require.NoError(t, err)
	_, err = repo.StepRuns(ctx, ws, task.ID, nil)
	require.ErrorIs(t, err, ErrConversationNotFound, "a private team's task is hidden from who is not in the team")
	_, err = db.Exec(`INSERT INTO team_members(team_id,user_id) VALUES($1,$2)`, task.TeamID, task.CreatorID)
	require.NoError(t, err)
	runs, err = repo.StepRuns(ctx, ws, task.ID, nil)
	require.NoError(t, err)
	require.Len(t, runs, 4, "and readable again by a member of it")
}
