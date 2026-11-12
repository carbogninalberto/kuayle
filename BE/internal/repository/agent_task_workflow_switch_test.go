package repository

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/stretchr/testify/require"
)

// saveNext stores a new version of the fixture's workflow and returns it.
func (f *pauseFixture) saveNext(change func(*domain.WorkflowGraph)) domain.WorkflowVersion {
	f.t.Helper()
	workflows := NewWorkflowRepository(f.db)
	var workflow domain.Workflow
	require.NoError(f.t, f.db.Get(&workflow, `SELECT w.* FROM workflows w JOIN workflow_versions v ON v.workflow_id=w.id WHERE v.id=$1`, f.task.WorkflowVersionID))
	var latest domain.WorkflowVersion
	require.NoError(f.t, f.db.Get(&latest, `SELECT * FROM workflow_versions WHERE workflow_id=$1 AND version=$2`, workflow.ID, workflow.LatestVersion))
	graph, err := domain.ParseWorkflowGraph(latest.Graph)
	require.NoError(f.t, err)
	change(graph)
	raw, err := json.Marshal(graph)
	require.NoError(f.t, err)
	_, err = workflows.SaveVersion(f.ctx, f.ws, workflow.ID, f.task.CreatorID, workflow.LatestVersion, workflow.Name, "", raw)
	require.NoError(f.t, err)
	var saved domain.WorkflowVersion
	require.NoError(f.t, f.db.Get(&saved, `SELECT * FROM workflow_versions WHERE workflow_id=$1 AND version=$2`, workflow.ID, workflow.LatestVersion+1))
	return saved
}

// rest puts the task where a person's pause leaves it once the interrupted
// attempt is stopped: paused, blocked, with no lease.
func (f *pauseFixture) rest() {
	f.t.Helper()
	f.exec(`UPDATE agent_tasks SET workflow_paused_at=now(),state='blocked',lease_token=NULL,lease_expires_at=NULL WHERE id=$1`, f.task.ID)
}

func relabel(graph *domain.WorkflowGraph) {
	for i := range graph.Nodes {
		if graph.Nodes[i].ID == "implement" {
			graph.Nodes[i].Label, graph.Nodes[i].Prompt, graph.Nodes[i].MaxVisits = "Build it", "Finish the work.\n\n{{review_comments}}", 7
		}
	}
}

// A paused task moves to the newest version of its workflow on the step it
// is on, keeping what it already did, and nothing else moves with it.
func TestWorkflowSwitchMovesAPausedTaskToTheNewestVersion(t *testing.T) {
	f := newPauseFixture(t)
	pinned := *f.task.WorkflowVersionID

	// Nothing newer yet.
	plan, _, err := f.repo.SwitchWorkflow(f.ctx, f.ws, f.task.ID, false)
	require.NoError(t, err)
	require.Equal(t, domain.WorkflowSwitchNoNewer, plan.Refused)

	next := f.saveNext(relabel)
	// Tasks keep the version they started on until a person moves them.
	require.Equal(t, pinned, *f.current().WorkflowVersionID)

	// The agent is working: the task has to be paused first.
	plan, _, err = f.repo.SwitchWorkflow(f.ctx, f.ws, f.task.ID, true)
	require.NoError(t, err)
	require.Equal(t, domain.WorkflowSwitchNotAtRest, plan.Refused)
	require.Equal(t, pinned, *f.current().WorkflowVersionID)

	f.rest()
	f.exec(`UPDATE agent_tasks SET workflow_visits='{"implement":2,"gone":4}',workflow_iterations=3 WHERE id=$1`, f.task.ID)
	before := f.current()

	// Asking what would happen changes nothing.
	plan, _, err = f.repo.SwitchWorkflow(f.ctx, f.ws, f.task.ID, false)
	require.NoError(t, err)
	require.Equal(t, domain.WorkflowSwitch{FromVersion: 1, ToVersion: 2, Node: "implement", NodeLabel: "Build it"}, *plan)
	require.Equal(t, pinned, *f.current().WorkflowVersionID)

	plan, moved, err := f.repo.SwitchWorkflow(f.ctx, f.ws, f.task.ID, true)
	require.NoError(t, err)
	require.Empty(t, plan.Refused)
	require.Equal(t, next.ID, *moved.WorkflowVersionID)
	after := f.current()
	require.Equal(t, next.ID, *after.WorkflowVersionID)
	require.Equal(t, "implement", *after.WorkflowNode)
	// Visits of steps that still exist are kept, so the new limits count
	// them; what was used of the budget is kept too. The task stays paused.
	require.Equal(t, map[string]int{"implement": 2}, after.Cursor().Visits)
	require.Equal(t, 3, after.WorkflowIterations)
	require.Equal(t, before.WorkflowSequence, after.WorkflowSequence)
	require.NotNil(t, after.WorkflowPausedAt)
	require.Equal(t, "blocked", after.State)

	var detail json.RawMessage
	var actor uuid.UUID
	require.NoError(t, f.db.QueryRow(`SELECT detail,actor_id FROM workflow_audit_events WHERE entity='task_workflow' AND entity_id=$1 AND action=$2`, f.task.ID, domain.WorkflowSwitchAction).Scan(&detail, &actor))
	require.Equal(t, f.task.CreatorID, actor)
	require.JSONEq(t, `{"from_version":1,"to_version":2,"node":"implement","sequence":`+jsonInt(int64(after.WorkflowSequence))+`}`, string(detail))
	// The workflow view lists the move with the task's pauses, on the step
	// it happened at, so the task's path shows it there.
	events, err := f.repo.WorkflowPauses(f.ctx, f.ws, f.task.ID)
	require.NoError(t, err)
	require.NotEmpty(t, events)
	move := events[len(events)-1]
	require.Equal(t, domain.WorkflowSwitchAction, move.Action)
	require.Equal(t, []int{1, 2, after.WorkflowSequence}, []int{move.FromVersion, move.ToVersion, move.Sequence})
	require.Equal(t, f.task.CreatorID, *move.ActorID)

	// It is on the newest version now.
	plan, _, err = f.repo.SwitchWorkflow(f.ctx, f.ws, f.task.ID, true)
	require.NoError(t, err)
	require.Equal(t, domain.WorkflowSwitchNoNewer, plan.Refused)
}

// A task whose step is gone from the newest version, or does another kind of
// work there, is not moved: it is never left without a valid step.
func TestWorkflowSwitchRefusesAStepTheNewestVersionLost(t *testing.T) {
	f := newPauseFixture(t)
	pinned := *f.task.WorkflowVersionID
	f.saveNext(func(graph *domain.WorkflowGraph) {
		for i := range graph.Nodes {
			if graph.Nodes[i].ID == "implement" {
				graph.Nodes[i].ID = "build"
			}
		}
		for i := range graph.Edges {
			if graph.Edges[i].Source == "implement" {
				graph.Edges[i].Source = "build"
			}
			if graph.Edges[i].Target == "implement" {
				graph.Edges[i].Target = "build"
			}
		}
	})
	f.rest()
	plan, moved, err := f.repo.SwitchWorkflow(f.ctx, f.ws, f.task.ID, true)
	require.NoError(t, err)
	require.Equal(t, domain.WorkflowSwitchStepRemoved, plan.Refused)
	require.Equal(t, 2, plan.ToVersion)
	require.Nil(t, moved)
	require.Equal(t, pinned, *f.current().WorkflowVersionID)
	var audits int
	require.NoError(t, f.db.Get(&audits, `SELECT count(*) FROM workflow_audit_events WHERE entity_id=$1 AND action=$2`, f.task.ID, domain.WorkflowSwitchAction))
	require.Zero(t, audits)
}

// A task waiting on a step (CI, an approval, a merge) can be moved without
// pausing it, and the worker's stale lease no longer applies anything.
func TestWorkflowSwitchMovesAWaitingTaskAndFencesTheWorker(t *testing.T) {
	f := newPauseFixture(t)
	next := f.saveNext(relabel)
	f.exec(`UPDATE agent_tasks SET workflow_status='waiting',state='awaiting_review' WHERE id=$1`, f.task.ID)
	plan, moved, err := f.repo.SwitchWorkflow(f.ctx, f.ws, f.task.ID, true)
	require.NoError(t, err)
	require.Empty(t, plan.Refused)
	require.Equal(t, next.ID, *moved.WorkflowVersionID)
	require.Nil(t, f.current().LeaseToken)
	// What the worker decided on the old version is refused.
	_, err = f.repo.ApplyWorkflow(f.ctx, *f.claimed, f.toPublish(*f.claimed))
	require.ErrorIs(t, err, ErrAgentTaskConflict)
}

func jsonInt(v int64) string {
	raw, _ := json.Marshal(v)
	return string(raw)
}
