package service

import (
	"context"
	"sync"
	"testing"

	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/stretchr/testify/require"
)

// The write lock (KUA-289, plan P4.3): the roles that write code take their
// turns on the root task, one at a time; the lock moves only between turns.
// Every other role reads, in turns of its own that run side by side.

// withSecondWriter adds a frontend role that writes code too.
func withSecondWriter(p *domain.Pipeline) {
	p.Roster = append(p.Roster, domain.PipelineMember{Role: "frontend", ProfileID: p.Roster[0].ProfileID, Writer: true})
}

// holder is the role holding the root task's write lock.
func (w *tickWorld) holder() string {
	return domain.PipelineHolderRole(w.task().PipelineState)
}

// A second writer called while the first one's turn runs does not start:
// it takes the write lock once that turn ended, as the root task's next
// turn, in a session of its own.
func TestTickASecondWriterNeverStartsWhileTheFirstRuns(t *testing.T) {
	w := newTickWorld(t, withSecondWriter)
	w.agent = func(role string, turn int, task domain.AgentTask) tickTurn {
		switch role {
		case "implementer":
			return tickTurn{hold: true}
		case "frontend":
			return tickTurn{commit: sha(2), final: "Styled the toggle."}
		}
		return tickTurn{final: "Verdict: approve\nFine."}
	}
	w.drain()
	require.Equal(t, "implementer", w.holder())
	_, err := w.f.tasks.Repo.PostRoomMessage(w.f.ctx, w.f.ws, w.root, domain.TaskRoomPost{Kind: domain.RoomAsk, ToRoles: []string{"frontend"}, Body: "Can you style the toggle?"})
	require.NoError(t, err)
	w.later()
	require.Zero(t, w.runtime.turns["frontend"], "the frontend waits while the implementer writes")
	require.Equal(t, "running", w.task().State)

	w.finish("implementer", tickTurn{commit: sha(1), final: "Added the toggle."})
	w.drain()
	require.Equal(t, 1, w.runtime.turns["frontend"], "the lock moved once the implementer's turn ended")
	require.Equal(t, "frontend", w.holder())
	require.Contains(t, w.prompt("frontend", 1), "Can you style the toggle?")
	frontend := w.member("frontend")
	require.Nil(t, frontend.MemberTaskID, "a writer's turns are the root task's")
	require.Equal(t, 1, frontend.Turns)

	// The frontend's turn does not continue the implementer's session.
	root := w.task()
	previous, err := w.f.tasks.Repo.PreviousAttempt(context.Background(), root)
	require.NoError(t, err)
	require.NotNil(t, previous)
	require.True(t, previous.OtherProfile, "another writer's session is never resumed")

	// Each of the root task's runs is listed with the writer that ran it.
	runs, err := w.f.tasks.Repo.StepRuns(w.f.ctx, w.f.ws, w.root, nil)
	require.NoError(t, err)
	var writers []string
	for _, run := range runs {
		if run.TaskID == w.root && run.Role != nil {
			writers = append(writers, *run.Role)
		}
	}
	require.Equal(t, []string{"implementer", "frontend"}, writers)
}

// Two reviewers read side by side while the writer is idle.
func TestTickTwoReviewersRunWhileTheWriterIsIdle(t *testing.T) {
	w := newTickWorld(t, func(p *domain.Pipeline) {
		p.Roster = append(p.Roster, domain.PipelineMember{Role: "security", ProfileID: p.Roster[1].ProfileID})
		p.Phases[2].Checks = append(p.Phases[2].Checks, domain.PipelineCheck{ID: "security", Fact: domain.PipelineFactVerdict, Role: "security", Required: true})
	})
	w.agent = func(role string, turn int, task domain.AgentTask) tickTurn {
		if role == "implementer" {
			return tickTurn{commit: sha(1), final: "Added the toggle."}
		}
		return tickTurn{hold: true}
	}
	w.drain()
	var held []string
	for _, role := range w.runtime.held {
		held = append(held, role)
	}
	require.ElementsMatch(t, []string{"reviewer", "security"}, held)
	members, err := w.f.tasks.Repo.MemberTasks(context.Background(), w.task())
	require.NoError(t, err)
	require.Equal(t, "running", members["reviewer"].State)
	require.Equal(t, "running", members["security"].State)
	require.Equal(t, "awaiting_review", w.task().State, "the writer is idle")
	require.Equal(t, domain.MemberIdle, w.member("implementer").State)

	w.finish("reviewer", tickTurn{final: "Verdict: approve\nFine."})
	w.finish("security", tickTurn{final: "Verdict: approve\nNo secrets."})
	w.drain()
	require.Equal(t, "integrate", w.phase())
}

// Two passes that race to start a writer's turn start one: the write lock
// is taken in the root task's fenced transaction, and the loser is refused.
func TestTickTwoPassesRacingForTheWriteLockStartOneTurn(t *testing.T) {
	w := newTickWorld(t, withSecondWriter)
	w.agent = w.writesAndApproves
	w.drain()
	require.Equal(t, "awaiting_review", w.task().State)
	before := w.task()
	w.exec(`UPDATE agent_tasks SET workflow_wake_at=now(),next_poll_at=now() WHERE id=$1`, w.root)
	claimed, err := w.f.tasks.Repo.Claim(context.Background())
	require.NoError(t, err)
	require.NotNil(t, claimed)
	require.Equal(t, w.root, claimed.ID)
	p, _, err := w.f.tasks.Workflow().pipeline(context.Background(), before)
	require.NoError(t, err)
	var turnsBefore int
	require.NoError(t, w.f.db.Get(&turnsBefore, `SELECT COALESCE(sum(turns),0) FROM task_members WHERE task_id=$1 AND member_task_id IS NULL`, w.root))

	// As the worker's step runs a pass: for the task's creator.
	ctx := domain.WithAccess(context.Background(), domain.Access{WorkspaceID: claimed.WorkspaceID, UserID: claimed.CreatorID})
	errs := make([]error, 2)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i, writer := range []domain.PipelineMember{p.Roster[0], p.Roster[2]} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, errs[i] = w.f.tasks.Repo.ApplyPipeline(ctx, *claimed, repository.PipelineApply{Sequence: claimed.WorkflowSequence, Phase: deref(claimed.WorkflowNode),
				State: claimed.PipelineState, Turns: 1, ClaimedWake: claimed.WorkflowWakeAt, Writer: writer,
				Entry: repository.WorkflowEntry{Kind: repository.WorkflowEnterIteration, Reason: domain.WorkflowReasonPrefix + "the @" + writer.Role + "'s turn", Prompt: "Go on.", Provider: "codex"}})
		}()
	}
	close(start)
	wg.Wait()
	won := 0
	for _, err := range errs {
		if err == nil {
			won++
		} else {
			require.ErrorIs(t, err, repository.ErrAgentTaskConflict)
		}
	}
	require.Equal(t, 1, won, "one writer's turn starts")
	after := w.task()
	require.Equal(t, before.Generation+1, after.Generation)
	require.Equal(t, "queued", after.State)
	winner := []string{"implementer", "frontend"}[map[bool]int{true: 0, false: 1}[errs[0] == nil]]
	require.Equal(t, winner, w.holder())
	var turns int
	require.NoError(t, w.f.db.Get(&turns, `SELECT COALESCE(sum(turns),0) FROM task_members WHERE task_id=$1 AND member_task_id IS NULL`, w.root))
	require.Equal(t, turnsBefore+1, turns, "the winner's turn only")
}
