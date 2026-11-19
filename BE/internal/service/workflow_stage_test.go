package service

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/stretchr/testify/require"
)

// Use the real task, pinned version, leases, run results and step journal.
// Only the machine's execution and the independent test result are simulated.
func TestStageAssistantRunsAndRoutesInTheTaskWorktree(t *testing.T) {
	f := newProfileFixture(t)
	profile := f.storedProfile(t, "Stage owner", "codex", "", "")
	graph := domain.WorkflowGraph{
		Nodes: []domain.WorkflowNode{
			{ID: "start", Type: "start", Label: "Triage", ProfileID: &profile.ID},
			{ID: "test", Type: "check", Check: "tests", Label: "Verify", ProfileID: &profile.ID},
			{ID: "publish", Type: "publish", Label: "Publish", ProfileID: &profile.ID},
			{ID: "merge", Type: "merge", Label: "Merge", ProfileID: &profile.ID},
			{ID: "gate", Type: "approval", Label: "Approve the result", ProfileID: &profile.ID},
			{ID: "done", Type: "end", Outcome: "done", Label: "Finish", ProfileID: &profile.ID},
		},
		Edges: []domain.WorkflowEdge{
			{ID: "default", Source: "start", Target: "gate", Condition: "else"},
			{ID: "verify", Source: "start", Target: "test", Condition: "assistant"},
			{ID: "passed", Source: "test", Target: "publish", Condition: "checks_passed"},
			{ID: "published", Source: "publish", Target: "gate", Condition: "published"},
			{ID: "approved", Source: "gate", Target: "merge", Condition: "approval_approved"},
			{ID: "merged", Source: "merge", Target: "done", Condition: "merged"},
		},
	}
	raw, err := json.Marshal(graph)
	require.NoError(t, err)
	workflow, err := f.workflows.Create(f.ctx, f.ws, WorkflowRequest{Name: "Supervised delivery", Graph: raw})
	require.NoError(t, err)
	chat, err := repository.NewConversationRepository(f.db).OpenIssue(f.ctx, f.ws, f.issue)
	require.NoError(t, err)
	task, err := f.tasks.Create(f.ctx, f.ws, chat.ID, domain.AgentTaskRequest{RequestID: uuid.New(), Prompt: "Deliver the change", WorkflowID: &workflow.ID, Provider: "claude-code"})
	require.NoError(t, err)
	require.Equal(t, "codex", task.Provider)
	require.Equal(t, 1, task.WorkflowSequence)
	require.Equal(t, "start", *task.WorkflowNode)
	machine := uuid.New()
	_, err = f.db.Exec(`INSERT INTO dev_machines(id,workspace_id,created_by_user_id,routing_key,name,machine_size,cpu_millis,memory_mb,disk_gb,status) VALUES($1,$2,$3,$4,'Test','small',1000,1024,20,'running')`, machine, f.ws, f.user, strings.ReplaceAll(machine.String(), "-", "")[:16])
	require.NoError(t, err)
	refresh := func() {
		task, err = f.tasks.Repo.Get(f.ctx, f.ws, task.ID)
		require.NoError(t, err)
	}
	run := func(stage, answer string, changed bool) {
		t.Helper()
		require.Equal(t, stage, *task.WorkflowNode)
		bundle, err := f.tasks.Repo.ProfileBundle(f.ctx, *task)
		require.NoError(t, err)
		require.Contains(t, bundle.StageInstructions, stage)
		if stage == "start" {
			require.Contains(t, bundle.StageInstructions, "[selectable] assistant verify -> Verify")
			require.NotContains(t, bundle.StageInstructions, "passed ->")
		}
		// Mark this visit's native machine run as finished under the worker lease.
		_, err = f.db.Exec(`UPDATE agent_tasks SET state='running',machine_id=$2,lease_token=$3,lease_expires_at=now()+interval '30 seconds' WHERE id=$1`, task.ID, machine, uuid.New())
		require.NoError(t, err)
		_, err = f.db.Exec(`INSERT INTO dev_machine_agent_runs(id,workspace_id,machine_id,task_id,requested_by_user_id,provider_id,mode,prompt,command_argv,max_runtime_seconds,status,summary,result,worktree_changed)
 VALUES ($1,$2,$3,$4,$5,'codex','autonomous','Stage work','["codex"]',60,'succeeded',$6,'{"final_message":true}',$7)`, task.RunID, f.ws, machine, task.ID, f.user, answer, changed)
		require.NoError(t, err)
		refresh()
		require.NoError(t, f.tasks.Workflow().AgentFinished(f.ctx, *task))
		refresh()
	}
	run("start", "Ready for testing.\nKUAYLE_STAGE_ROUTE verify", false)
	testsRun := task.RunID
	run("test", "Fixed the failing fixture.\nKUAYLE_STAGE_CONTINUE", true)
	require.Equal(t, "validating", task.State)
	require.Equal(t, testsRun, task.RunID)
	require.NoError(t, f.tasks.Workflow().Outcome(f.ctx, *task, domain.WorkflowChecksPassed, "Independent tests passed"))
	refresh()
	run("publish", "Prepared the publication.", false)
	require.Equal(t, "publishing", task.State)
	require.Equal(t, testsRun, task.RunID)
	_, err = f.db.Exec(`INSERT INTO dev_machine_git_refs(workspace_id,machine_id,agent_run_id,ref_type,repository_full_name,ref_name,pull_request_number,url,source) VALUES($1,$2,$3,'pull_request','example/project','work',1,'https://example.test/pr/1','published')`, f.ws, machine, task.RunID)
	require.NoError(t, err)
	require.NoError(t, f.tasks.Workflow().Outcome(f.ctx, *task, domain.WorkflowPublished, "Opened pull request"))
	refresh()
	run("gate", "Prepared a handoff.\nKUAYLE_STAGE_CONTINUE", false)
	require.Equal(t, "awaiting_review", task.State)
	require.Equal(t, testsRun, task.RunID, "the unchanged stage keeps the attempt whose tests passed")
	_, err = f.tasks.Workflow().Decide(f.ctx, f.ws, task.ID, task.WorkflowSequence, WorkflowDecisionApprove)
	require.NoError(t, err)
	refresh()
	run("merge", "Checked the merge handoff.", false)
	require.Equal(t, "awaiting_review", task.State)
	_, err = f.tasks.Repo.CompleteMergedPullRequest(f.ctx, f.ws, "example/project", 1)
	require.NoError(t, err)
	refresh()
	require.Equal(t, "awaiting_review", task.State, "a merge wakes the workflow so its terminal assistant can run")
	require.NoError(t, f.tasks.Workflow().Evaluate(f.ctx, *task))
	refresh()
	run("done", "Updated the notebook.", false)
	require.Equal(t, "completed", task.State)
	steps, err := f.tasks.Repo.WorkflowSteps(f.ctx, f.ws, task.ID)
	require.NoError(t, err)
	require.Len(t, steps, 6)
	require.Equal(t, "verify", *steps[1].EdgeID)
	require.Equal(t, "assistant:verify", *steps[0].Outcome)
	require.Equal(t, f.user, *steps[3].DecidedBy)
	require.Equal(t, "done", *steps[5].Outcome)
}

func TestStageAssistantCannotChooseANativeApproval(t *testing.T) {
	h := newWorkflowHarness(t, domain.WorkflowBudget{})
	var graph domain.WorkflowGraph
	require.NoError(t, json.Unmarshal(h.w.version.Graph, &graph))
	ship, _ := graph.Node("ship")
	ship.ProfileID = graph.FirstRun().ProfileID
	for i := range graph.Nodes {
		if graph.Nodes[i].ID == ship.ID {
			graph.Nodes[i] = ship
		}
	}
	h.w.version.Graph = *rawJSON(t, graph)
	h.restart()
	h.w.task.WorkflowNode = &ship.ID
	h.w.steps[h.w.task.WorkflowSequence].NodeID = ship.ID
	h.w.runs[h.w.task.RunID] = &repository.WorkflowRun{FinalMessage: true, Summary: "KUAYLE_STAGE_ROUTE ship-approved"}
	require.NoError(t, h.runner.AgentFinished(t.Context(), h.w.task))
	require.Equal(t, "cancelling", h.w.task.State)
	require.Contains(t, h.w.task.Reason, "unavailable transition")
}
