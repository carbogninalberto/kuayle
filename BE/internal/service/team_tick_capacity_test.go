package service

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/stretchr/testify/require"
)

// Capacity of the pipeline tick (KUA-298): a member's turn takes a task slot
// on its machine like any task (TECHNICAL.md, "Task machine reuse"), so the
// machine's own limit, or the workspace's, bounds the tasks and the members'
// turns working there together. A turn that finds no free slot waits, and
// the turns waiting for one machine start oldest unread first, across tasks.

// twoReviewers adds a security role whose verdict the review phase requires
// too, so two members are due at once.
func twoReviewers(p *domain.Pipeline) {
	p.Roster = append(p.Roster, domain.PipelineMember{Role: "security", ProfileID: p.Roster[1].ProfileID})
	p.Phases[2].Checks = append(p.Phases[2].Checks, domain.PipelineCheck{ID: "security", Fact: domain.PipelineFactVerdict, Role: "security", Required: true})
}

// slots sets the machine's own task limit (nil follows the workspace's) and
// the workspace's.
func (w *tickWorld) slots(machine *int, workspace int) {
	w.t.Helper()
	w.exec(`UPDATE dev_machines SET max_tasks=$2 WHERE id=$1`, w.machine, machine)
	w.exec(`UPDATE dev_machine_workspace_policies SET task_machine_max_tasks=$2 WHERE workspace_id=$1`, w.f.ws, workspace)
}

// occupy starts n tasks of other work on the machine, each holding a task
// slot; the worker leaves them alone (another worker holds their lease).
func (w *tickWorld) occupy(n int) []uuid.UUID {
	w.t.Helper()
	ids := make([]uuid.UUID, 0, n)
	for range n {
		var id uuid.UUID
		require.NoError(w.t, w.f.db.Get(&id, `INSERT INTO agent_tasks(workspace_id,conversation_id,creator_id,issue_id,team_id,request_id,prompt,provider,state,machine_id,lease_token,lease_expires_at)
 SELECT workspace_id,conversation_id,creator_id,issue_id,team_id,gen_random_uuid(),'Other work on the machine',provider,'running',$2,gen_random_uuid(),now()+interval '1 day' FROM agent_tasks WHERE id=$1 RETURNING id`, w.root, w.machine))
		ids = append(ids, id)
	}
	return ids
}

// release ends the other work: its slots free, and nothing wakes anyone.
func (w *tickWorld) release(ids []uuid.UUID) {
	w.t.Helper()
	for _, id := range ids {
		w.exec(`UPDATE agent_tasks SET state='completed',lease_token=NULL,lease_expires_at=NULL WHERE id=$1`, id)
	}
}

// inUse counts the tasks holding a task slot on the machine, as placement
// counts them.
func (w *tickWorld) inUse() int {
	w.t.Helper()
	var n int
	require.NoError(w.t, w.f.db.Get(&n, `SELECT count(*) FROM agent_tasks WHERE machine_id=$1 AND state NOT IN ('completed','cancelled','failed','blocked','awaiting_review') AND workflow_paused_at IS NULL`, w.machine))
	return n
}

// queued are the roles a task's tick left waiting for a slot.
func (w *tickWorld) queued(root uuid.UUID) []string {
	w.t.Helper()
	var raw []byte
	require.NoError(w.t, w.f.db.Get(&raw, `SELECT COALESCE(pipeline_state->'queue','[]') FROM agent_tasks WHERE id=$1`, root))
	var roles []string
	require.NoError(w.t, json.Unmarshal(raw, &roles))
	if len(roles) == 0 {
		return nil
	}
	return roles
}

// heldFor is the run of a role's turn that the test holds for a root task.
func (w *tickWorld) heldFor(root uuid.UUID, role string) (uuid.UUID, bool) {
	w.t.Helper()
	for run, held := range w.runtime.held {
		var of uuid.UUID
		require.NoError(w.t, w.f.db.Get(&of, `SELECT COALESCE((SELECT task_id FROM task_members WHERE member_task_id=r.task_id),r.task_id) FROM dev_machine_agent_runs r WHERE r.id=$1`, run))
		if held == role && of == root {
			return run, true
		}
	}
	return uuid.Nil, false
}

// another starts a second task of the same creator on the same pipeline, for
// another issue, on the same machine.
func (w *tickWorld) another() uuid.UUID {
	w.t.Helper()
	issue := uuid.New()
	w.exec(`INSERT INTO issues(id,workspace_id,team_id,number,identifier_text,title,creator_id,status_id) SELECT $2,workspace_id,team_id,2,'ENG-2','Light mode',creator_id,status_id FROM issues WHERE id=$1`, w.f.issue, issue)
	var workflow string
	require.NoError(w.t, w.f.db.Get(&workflow, `SELECT v.workflow_id::text FROM workflow_versions v JOIN agent_tasks t ON t.workflow_version_id=v.id WHERE t.id=$1`, w.root))
	chat, err := repository.NewConversationRepository(w.f.db).OpenIssue(w.f.ctx, w.f.ws, issue)
	require.NoError(w.t, err)
	task, err := w.f.tasks.Create(w.f.ctx, w.f.ws, chat.ID, domain.AgentTaskRequest{RequestID: uuid.New(), Prompt: "Add a light mode toggle", Provider: "codex", WorkflowID: &workflow})
	require.NoError(w.t, err)
	return task.ID
}

// waitingRoom is a world whose tasks publish nothing and wait for a person
// between turns (the pull request phase is a person's), so a role's turn
// starts only when the owner writes to it; the roles keep working until the
// test ends their turns.
func waitingRoom(t *testing.T) *tickWorld {
	w := newTickWorld(t, func(p *domain.Pipeline) { p.Phases[1].Owner = domain.PipelineOwnerHuman })
	w.agent = func(role string, _ int, _ domain.AgentTask) tickTurn {
		if role == "implementer" {
			return tickTurn{final: "Nothing to change yet."}
		}
		return tickTurn{hold: true}
	}
	return w
}

func (w *tickWorld) ask(root uuid.UUID, role string) {
	w.t.Helper()
	_, err := w.f.tasks.Repo.PostRoomMessage(w.f.ctx, w.f.ws, root, domain.TaskRoomPost{Kind: domain.RoomAsk, Body: "Is the toggle accessible?", ToRoles: []string{role}})
	require.NoError(w.t, err)
}

// The tick starts a member's turn only while the machine has a free task
// slot: the machine's own limit, or the workspace's when it has none,
// counted together with the other tasks working there.
func TestTickMemberTurnsTakeTheMachinesTaskSlots(t *testing.T) {
	one, two := 1, 2
	for _, c := range []struct {
		name      string
		machine   *int
		workspace int
		others    int
		atOnce    int
	}{
		{"the workspace's limit", nil, 1, 0, 1},
		{"the machine's own limit", &one, 10, 0, 1},
		{"the machine's own limit over the workspace's", &two, 1, 0, 2},
		{"counted with the machine's other tasks", nil, 2, 1, 1},
	} {
		t.Run(c.name, func(t *testing.T) {
			w := newTickWorld(t, twoReviewers)
			w.slots(c.machine, c.workspace)
			limit := c.workspace
			if c.machine != nil {
				limit = *c.machine
			}
			w.occupy(c.others)
			w.agent = func(role string, turn int, _ domain.AgentTask) tickTurn {
				if role == "implementer" {
					return tickTurn{commit: sha(turn), final: "Added the toggle."}
				}
				return tickTurn{hold: true}
			}
			peak := 0
			w.afterStep = func(domain.AgentTask) {
				require.LessOrEqual(t, w.inUse(), limit, "the machine runs no more tasks than its limit")
				peak = max(peak, len(w.runtime.held))
			}
			w.drain()
			require.Len(t, w.runtime.held, c.atOnce)
			if c.atOnce == 1 {
				require.Equal(t, []string{"security"}, w.queued(w.root), "the second role waits for a slot")
				waiting := w.task()
				require.Equal(t, "awaiting_review", waiting.State)
				require.NotNil(t, waiting.WorkflowWakeAt, "the tick polls for the slot")
				require.False(t, waiting.WorkflowWakeAt.After(time.Now().Add(tickPoll)), "within a poll")
			}
			for _, role := range []string{"reviewer", "security"} {
				if _, held := w.heldFor(w.root, role); !held {
					w.later()
				}
				w.finish(role, tickTurn{final: "Verdict: approve\nLooks right."})
				w.drain()
			}
			require.Equal(t, c.atOnce, peak)
			require.Equal(t, "integrate", w.phase(), "both roles approved")
			require.Empty(t, w.queued(w.root))
		})
	}
}

// A machine whose slots other work holds never wedges the task: the role's
// turn waits, the tick polls without stalling or pausing, and once a slot
// frees the turn starts and the task runs to its merge.
func TestTickAFullMachineNeverWedgesTheTask(t *testing.T) {
	w := newTickWorld(t, nil)
	w.agent = w.writesAndApproves
	w.slots(nil, 1)
	others := w.occupy(1)
	w.drain()
	for range tickStallLimit + 2 {
		waiting := w.task()
		require.Equal(t, "awaiting_review", waiting.State)
		require.Equal(t, domain.WorkflowStatusWaiting, *waiting.WorkflowStatus, "nothing pauses for a person")
		require.Nil(t, waiting.WorkflowHeldSince)
		require.NotNil(t, waiting.WorkflowWakeAt)
		require.Contains(t, waiting.Reason, "Waiting to start: @reviewer (a free task slot", "the task says what it waits on")
		require.Zero(t, w.runtime.turns["reviewer"], "no slot, no turn")
		require.Equal(t, []string{"reviewer"}, w.queued(w.root))
		w.later()
	}
	w.release(others)
	w.later() // the tick's next poll
	require.Equal(t, 1, w.runtime.turns["reviewer"])
	require.Equal(t, "integrate", w.phase())
	w.github.ci("passed", "")
	w.later()
	require.NoError(t, w.decide(WorkflowDecisionApprove))
	w.drain()
	require.Equal(t, "completed", w.task().State)
	require.Equal(t, []string{sha(1)}, w.github.merged)
}

// Turns waiting for one machine start oldest unread first, across tasks:
// when a slot frees, the task whose role has waited on its room longer takes
// it, whichever tick runs first.
func TestTickQueuedTurnsStartOldestUnreadFirst(t *testing.T) {
	w := waitingRoom(t)
	one := 1
	w.slots(&one, 10)
	w.drain()
	older := w.root
	younger := w.another()
	w.drain()
	others := w.occupy(1)
	w.ask(younger, "reviewer")
	w.ask(older, "reviewer")
	w.drain()
	require.Empty(t, w.runtime.held, "the slot is taken")
	require.Equal(t, []string{"reviewer"}, w.queued(older))
	require.Equal(t, []string{"reviewer"}, w.queued(younger))

	w.release(others)
	w.exec(`UPDATE agent_tasks SET workflow_wake_at=now() WHERE id=$1`, younger)
	w.drain()
	require.Empty(t, w.runtime.held, "the younger task's reviewer waits for the older one's")
	require.Equal(t, []string{"reviewer"}, w.queued(younger))

	w.exec(`UPDATE agent_tasks SET workflow_wake_at=now() WHERE id=$1`, older)
	w.drain()
	run, held := w.heldFor(older, "reviewer")
	require.True(t, held, "the older task's reviewer takes the slot")
	require.Len(t, w.runtime.held, 1)
	require.Empty(t, w.queued(older))

	w.finishRun(run, tickTurn{final: "It is."})
	w.drain()
	w.later()
	_, held = w.heldFor(younger, "reviewer")
	require.True(t, held, "then the younger one's")
	require.Len(t, w.runtime.held, 1)
}

// Two ticks that run at once for the last slot of a machine start one turn:
// the other waits, and starts once the slot frees.
func TestTickTwoTicksRaceForTheLastSlot(t *testing.T) {
	w := waitingRoom(t)
	one := 1
	w.slots(&one, 10)
	w.drain()
	first := w.root
	second := w.another()
	w.drain()
	w.ask(first, "reviewer")
	w.ask(second, "reviewer")

	// Both ticks are claimed, then run side by side.
	w.exec(`UPDATE agent_tasks SET next_poll_at=now()+interval '1 hour' WHERE id NOT IN ($1,$2)`, first, second)
	var claimed []domain.AgentTask
	for range 2 {
		task, err := w.f.tasks.Repo.Claim(context.Background())
		require.NoError(t, err)
		require.NotNil(t, task)
		claimed = append(claimed, *task)
	}
	require.ElementsMatch(t, []uuid.UUID{first, second}, []uuid.UUID{claimed[0].ID, claimed[1].ID})
	// Holding the workspace's row stops a tick that would insert its turn's
	// task (its foreign key needs the row), so both ticks get as far as they
	// can before either starts a turn: without the slot's lock, both counted
	// the free slot by then.
	barrier, err := w.f.db.Beginx()
	require.NoError(t, err)
	defer barrier.Rollback()
	_, err = barrier.Exec(`SELECT 1 FROM workspaces WHERE id=$1 FOR UPDATE`, w.f.ws)
	require.NoError(t, err)
	var wg sync.WaitGroup
	errs := make([]error, len(claimed))
	for i, task := range claimed {
		wg.Go(func() {
			ctx, cancel := context.WithTimeout(context.Background(), w.step)
			defer cancel()
			errs[i] = w.f.tasks.step(ctx, task)
			_ = w.f.tasks.store.Release(ctx, task)
		})
	}
	require.Eventually(t, func() bool {
		var waiting int
		require.NoError(t, w.f.db.Get(&waiting, `SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock'`))
		return waiting == len(claimed)
	}, 10*time.Second, 20*time.Millisecond, "both ticks wait")
	require.NoError(t, barrier.Rollback())
	wg.Wait()
	for _, err := range errs {
		if err != nil && !errors.Is(err, repository.ErrAgentTaskConflict) {
			require.NoError(t, err)
		}
	}
	require.Equal(t, 1, w.inUse(), "one turn took the slot")
	waits := [][]string{w.queued(first), w.queued(second)}
	require.ElementsMatch(t, [][]string{nil, {"reviewer"}}, waits, "the other waits for it")

	w.drain()
	require.Len(t, w.runtime.held, 1)
	for run := range w.runtime.held {
		w.finishRun(run, tickTurn{final: "It is."})
	}
	w.drain()
	w.later()
	require.Len(t, w.runtime.held, 1, "the other turn starts once the slot frees")
	_, a := w.heldFor(first, "reviewer")
	_, b := w.heldFor(second, "reviewer")
	require.True(t, a != b)
	require.Equal(t, 2, w.runtime.turns["reviewer"])
}
