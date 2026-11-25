package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/stretchr/testify/require"
)

// The lead (Phase 5): a member that manages. KUA-300 runs its turns as a
// member that reads.

// withLead gives the team pipeline a lead played by the reviewer's profile.
func withLead(p *domain.Pipeline) {
	p.Lead = &domain.PipelineLead{ProfileID: p.Roster[1].ProfileID}
}

// The owner addresses the lead: it takes a turn on a member task of its
// own, with the prompt of the role that manages, reading the published work.
func TestTickTheLeadTakesATurnWhenAddressed(t *testing.T) {
	w := newTickWorld(t, withLead)
	w.agent = func(role string, turn int, task domain.AgentTask) tickTurn {
		switch {
		case role == "reviewer":
			return tickTurn{hold: true} // a role works: nothing stalls
		case role != domain.PipelineLeadRole:
			return w.writesAndApproves(role, turn, task)
		case turn == 1:
			return leadMove("review", "The toggle is published.")
		case strings.Contains(task.Prompt, "Make sure the toggle is persisted."):
			return tickTurn{final: "Asked the implementer to persist the toggle.", posts: []string{`{"to":"@implementer","kind":"ask","body":"Is the toggle persisted?"}`}}
		}
		return tickTurn{final: "Noted."}
	}
	w.drain()
	require.Equal(t, 1, w.runtime.turns[domain.PipelineLeadRole])
	_, err := w.f.tasks.Repo.PostRoomMessage(w.f.ctx, w.f.ws, w.root, domain.TaskRoomPost{Kind: domain.RoomSay, ToRoles: []string{domain.PipelineLeadRole}, Body: "Make sure the toggle is persisted."})
	require.NoError(t, err)
	w.drain()

	require.GreaterOrEqual(t, w.runtime.turns[domain.PipelineLeadRole], 2, "and again for the writer's turn that answered it (KUA-302)")
	lead := w.member(domain.PipelineLeadRole)
	require.NotNil(t, lead.MemberTaskID)
	require.False(t, lead.VerdictAsked)
	require.NotNil(t, lead.PinnedCommit)
	var kind string
	require.NoError(t, w.f.db.Get(&kind, `SELECT kind FROM agent_tasks WHERE id=$1`, *lead.MemberTaskID))
	require.Equal(t, domain.AgentTaskKindReview, kind)
	prompt := w.prompt(domain.PipelineLeadRole, 2)
	require.Contains(t, prompt, "You are the @lead")
	require.Contains(t, prompt, "Make sure the toggle is persisted.")
	require.Contains(t, prompt, "git checkout --detach "+sha(1), "it reads the published work")
	require.GreaterOrEqual(t, w.runtime.turns["implementer"], 2, "the lead's question reached the writer")
	require.Contains(t, w.prompt("implementer", 2), "Is the toggle persisted?")
}

// The lead runs under the workspace policy as every role does: a task does
// not start when the policy does not allow the lead's harness.
func TestTickALeadTheWorkspaceDoesNotAllowStartsNoTask(t *testing.T) {
	f := profileFixtureOn(t, tickDatabase(t))
	f.tasks.SetPipelines(true)
	implementer, reviewer, err := f.workflows.builtinProfileIDs(f.ctx, f.ws)
	require.NoError(t, err)
	qwen, err := f.repo.CreateProfile(f.ctx, f.ws, f.user, domain.AgentProfileInput{Name: "Lead", Role: "Lead", Behaviour: domain.AgentBehaviourReviews, Harness: "qwen-code"})
	require.NoError(t, err)
	p := domain.TeamPipeline(implementer, reviewer)
	p.Lead = &domain.PipelineLead{ProfileID: qwen.ID}
	raw, err := json.Marshal(p)
	require.NoError(t, err)
	created, err := f.workflows.Create(f.ctx, f.ws, WorkflowRequest{Name: "Led", Graph: raw})
	require.NoError(t, err)
	chat, err := repository.NewConversationRepository(f.db).OpenIssue(f.ctx, f.ws, f.issue)
	require.NoError(t, err)
	f.machines.policy.AllowedProviders = json.RawMessage(`["codex","claude-code"]`)
	_, err = f.tasks.Create(f.ctx, f.ws, chat.ID, domain.AgentTaskRequest{RequestID: uuid.New(), Prompt: "Add a dark mode toggle", Provider: "codex", WorkflowID: &created.ID})
	require.ErrorIs(t, err, ErrProviderNotAllowed)
}

// leadMove is a lead's turn that moves the task to a phase, saying why.
func leadMove(phase, why string) tickTurn {
	post, _ := json.Marshal(map[string]any{"to": "@all", "kind": "phase", "body": why, "data": map[string]string{"phase": phase}})
	return tickTurn{final: "Moved the task to " + phase + ".", posts: []string{string(post)}}
}

// leadPhases are the phases the room took from the lead, in order; Kuayle
// posts none when there is a lead.
func (w *tickWorld) leadPhases() []string {
	w.t.Helper()
	var moves []string
	for _, m := range w.posts(domain.RoomPhase) {
		require.Equal(w.t, domain.RoomAuthorMember, m.AuthorKind, "Kuayle moved the phase of a task with a lead")
		if m.RejectReason == nil {
			moves = append(moves, m.Data.Phase)
		}
	}
	return moves
}

// leadSummaries are the texts Kuayle sent the lead about what the current
// phase waits on when nobody works (its stall summaries).
func (w *tickWorld) leadSummaries() []string {
	var out []string
	for _, m := range w.posts(domain.RoomSummary) {
		if m.AuthorKind == domain.RoomAuthorKuayle && slices.Contains(m.ToRoles, domain.PipelineLeadRole) && strings.HasPrefix(m.Body, "Nobody is working") {
			out = append(out, m.Body)
		}
	}
	return out
}

// With a lead the tick does not derive the phase (KUA-301): the task is in
// the phase the lead last moved it to, Kuayle runs what that phase needs,
// and when it holds Kuayle tells the lead, who moves the task on.
func TestTickTheLeadMovesThePhase(t *testing.T) {
	w := newTickWorld(t, withLead)
	moves := []tickTurn{
		leadMove("review", "The toggle is published: time for a review."),
		leadMove("integrate", "The reviewer approved."),
		leadMove("approval", "CI passed on the head."),
		leadMove("merge", "The owner approved."),
	}
	w.agent = func(role string, turn int, task domain.AgentTask) tickTurn {
		switch {
		case role != domain.PipelineLeadRole:
			return w.writesAndApproves(role, turn, task)
		case turn <= len(moves):
			return moves[turn-1]
		}
		return tickTurn{final: "Nothing to move."}
	}
	w.drain()
	require.Equal(t, []string{"review", "integrate"}, w.leadPhases())
	require.Equal(t, "integrate", w.phase())
	require.Equal(t, 1, w.runtime.turns["reviewer"], "Kuayle asked for the verdict the lead's phase needs")
	require.Contains(t, w.prompt(domain.PipelineLeadRole, 1), "- reviewer (Review): unmet.", "the checks reach the lead")

	w.github.ci("passed", "")
	w.later()
	require.Equal(t, "approval", w.phase())
	require.NotNil(t, w.task().WorkflowHeldSince, "the owner's approval is awaited")
	require.NoError(t, w.decide(WorkflowDecisionApprove))
	w.drain()

	done := w.task()
	require.Equal(t, "completed", done.State)
	require.Equal(t, []string{sha(1)}, w.github.merged)
	require.Equal(t, []string{"review", "integrate", "approval", "merge"}, w.leadPhases())
	require.Equal(t, 4, w.runtime.turns[domain.PipelineLeadRole])
	require.Contains(t, w.prompt(domain.PipelineLeadRole, 2), "review (Review), the current phase")
}

// A phase move to a phase the pipeline does not have, or without a reason,
// is refused: the phase stays, and the lead reads why at its next turn.
func TestTickALeadMoveToAnUnknownPhaseOrWithoutAReasonIsRefused(t *testing.T) {
	w := newTickWorld(t, withLead)
	w.agent = func(role string, turn int, task domain.AgentTask) tickTurn {
		switch {
		case role != domain.PipelineLeadRole:
			return w.writesAndApproves(role, turn, task)
		case turn == 1:
			ship, _ := json.Marshal(map[string]any{"to": "@all", "kind": "phase", "body": "Ship it.", "data": map[string]string{"phase": "ship"}})
			silent, _ := json.Marshal(map[string]any{"to": "@all", "kind": "phase", "data": map[string]string{"phase": "review"}})
			return tickTurn{final: "Tried.", posts: []string{string(ship), string(silent)}}
		case turn == 2:
			return leadMove("review", "Time for a review.")
		}
		return tickTurn{final: "Waiting for the reviewer."}
	}
	w.drain()
	prompt := w.prompt(domain.PipelineLeadRole, 2)
	require.Contains(t, prompt, "Every required check up to Implement holds. Next step: move the task on (kuayle-say --to @all --phase pr", "nobody worked: Kuayle told the lead it owes a move")
	require.Contains(t, prompt, `was refused: "ship" is not a phase of this pipeline (implement, pr, review, integrate, approval, merge)`)
	require.Contains(t, prompt, `was refused: a phase move says why: kuayle-say --to @all --phase review "the reason"`)
	require.Equal(t, []string{"review"}, w.leadPhases(), "only the move with a reason to a phase of the pipeline")
	require.Equal(t, "review", w.phase())
}

// Required checks still guard the merge (KUA-301): a lead that moves the
// task straight to the guarded phase merges nothing while a required check
// of any phase before it is unmet. Kuayle asks for the verdicts those checks
// need and tells the lead what blocks; once every check holds, it merges.
func TestTickTheLeadCannotMergePastAnUnmetRequiredCheck(t *testing.T) {
	w := newTickWorld(t, withLead)
	w.agent = func(role string, turn int, task domain.AgentTask) tickTurn {
		if role == domain.PipelineLeadRole {
			if turn == 1 {
				return leadMove("merge", "Ship it as soon as it may.")
			}
			return tickTurn{final: "Waiting for the checks."}
		}
		return w.writesAndApproves(role, turn, task)
	}
	w.github.ci("failed", "TestDarkMode failed")
	w.drain()
	require.Equal(t, "merge", w.phase())
	require.Equal(t, 1, w.runtime.turns["reviewer"], "Kuayle asked for the verdict a check before the merge needs")
	require.NotNil(t, w.task().WorkflowHeldSince, "the owner's approval, a check before the merge, is awaited")
	require.NoError(t, w.decide(WorkflowDecisionApprove), "the owner approves where the lead's phase needs it")
	w.drain()
	require.Empty(t, w.github.merged, "CI failed: the merge waits")
	require.NotEqual(t, "completed", w.task().State)
	summaries := w.leadSummaries()
	require.NotEmpty(t, summaries)
	require.Contains(t, summaries[len(summaries)-1], "- ci: 1 check(s) failed")

	w.github.ci("passed", "")
	w.later()
	require.Equal(t, "completed", w.task().State)
	require.Equal(t, []string{sha(1)}, w.github.merged)
	require.Equal(t, []string{"merge"}, w.leadPhases())
}

// Kuayle tells the lead the state of the pipeline's checks when it changes:
// the required ones, and the advisory ones, which never hold anything up.
func TestTickChecksReachTheLeadAsFacts(t *testing.T) {
	w := newTickWorld(t, func(p *domain.Pipeline) {
		withLead(p)
		p.Phases[2].Checks = append(p.Phases[2].Checks, domain.PipelineCheck{ID: "ready", Fact: domain.PipelineFactPRDraft})
	})
	w.github.status.Draft = true
	moves := []tickTurn{leadMove("review", "Time for a review."), leadMove("integrate", "The reviewer approved.")}
	w.agent = func(role string, turn int, task domain.AgentTask) tickTurn {
		switch {
		case role != domain.PipelineLeadRole:
			return w.writesAndApproves(role, turn, task)
		case turn <= len(moves):
			return moves[turn-1]
		}
		return tickTurn{final: "CI still runs."}
	}
	w.drain()
	advisory := w.facts("advisory")
	require.NotEmpty(t, advisory)
	first := advisory[0]
	require.Equal(t, []string{domain.PipelineLeadRole}, []string(first.ToRoles))
	require.JSONEq(t, `{"ready":"unmet"}`, string(first.Data.Value))
	require.Contains(t, first.Body, "still a draft")
	required := w.facts("checks")
	require.NotEmpty(t, required)
	require.Equal(t, []string{domain.PipelineLeadRole}, []string(required[0].ToRoles))
	var states map[string]string
	require.NoError(t, json.Unmarshal(required[0].Data.Value, &states))
	require.Equal(t, "unmet", states["reviewer"])
	require.Equal(t, "met", states["pr-exists"])
	require.NotContains(t, states, "ready", "advisory checks are a fact of their own")

	w.github.status.Draft = false
	w.later()
	advisory = w.facts("advisory")
	require.JSONEq(t, `{"ready":"met"}`, string(advisory[len(advisory)-1].Data.Value))
}

// A move read while GitHub cannot be read is kept: the pass that read it
// records it and waits for GitHub, and acts on it once GitHub answers.
func TestTickALeadMoveReadWhileGitHubIsDownIsKept(t *testing.T) {
	w := newTickWorld(t, withLead)
	w.agent = func(role string, turn int, task domain.AgentTask) tickTurn {
		if role == domain.PipelineLeadRole {
			return tickTurn{hold: true}
		}
		return w.writesAndApproves(role, turn, task)
	}
	w.drain()
	require.Equal(t, 1, w.runtime.turns[domain.PipelineLeadRole])
	w.github.readErr = errors.New("GitHub answered 502")
	lead := w.member(domain.PipelineLeadRole)
	_, err := w.f.tasks.Repo.RecordRoomMessage(w.f.ctx, w.root, domain.TaskRoomPost{AuthorKind: domain.RoomAuthorMember, AuthorRole: domain.PipelineLeadRole, AuthorMemberID: &lead.ID,
		Kind: domain.RoomPhase, Body: "Time for a review.", Data: domain.TaskRoomData{Phase: "review"}})
	require.NoError(t, err)
	w.finish(domain.PipelineLeadRole, tickTurn{final: "Moved the task to review."})
	w.drain()
	require.Equal(t, "review", w.phase(), "the move is recorded")
	require.Zero(t, w.runtime.turns["reviewer"], "GitHub is down: Kuayle acts on nothing yet")
	w.github.readErr = nil
	w.later()
	require.Equal(t, "review", w.phase(), "the lead's move was kept")
	require.Equal(t, 1, w.runtime.turns["reviewer"])
}

// When the lead wakes (KUA-302).

// leadAtReview is a team with a lead that moves the task to review at its
// first turn and is quiet after; the reviewer's turns are held until the
// test ends them, so a role works and nothing stalls.
func leadAtReview(t *testing.T, edit func(*domain.Pipeline)) *tickWorld {
	t.Helper()
	w := newTickWorld(t, func(p *domain.Pipeline) {
		withLead(p)
		if edit != nil {
			edit(p)
		}
	})
	w.agent = func(role string, turn int, task domain.AgentTask) tickTurn {
		switch {
		case role == "reviewer":
			return tickTurn{hold: true}
		case role != domain.PipelineLeadRole:
			return w.writesAndApproves(role, turn, task)
		case turn == 1:
			return leadMove("review", "The toggle is published.")
		}
		return tickTurn{final: "Noted."}
	}
	w.drain()
	require.Equal(t, "review", w.phase())
	require.Equal(t, 1, w.runtime.turns["reviewer"])
	require.Equal(t, 1, w.runtime.turns[domain.PipelineLeadRole])
	return w
}

// holdTheLead keeps every later turn of the lead working, so what happens
// after the turn under test cannot start another.
func holdTheLead(w *tickWorld) func(string, int, domain.AgentTask) tickTurn {
	return func(role string, turn int, task domain.AgentTask) tickTurn {
		if role == domain.PipelineLeadRole {
			return tickTurn{hold: true}
		}
		return w.writesAndApproves(role, turn, task)
	}
}

// A member's turn that ends reaches the lead as a digest (its outcome, its
// verdict and the first lines of its final message, not the transcript),
// which wakes it.
func TestTickATurnThatEndsWakesTheLeadWithADigest(t *testing.T) {
	w := leadAtReview(t, nil)
	first := w.prompt(domain.PipelineLeadRole, 1)
	require.Contains(t, first, "The @implementer's turn ended.", "the writer's first turn")
	require.Contains(t, first, "Added the toggle.")

	w.finish("reviewer", tickTurn{final: "Verdict: request_changes\nPersist the setting.\n- a\n- b\n- c\n- d\n- the eighth line"})
	w.drain()
	require.GreaterOrEqual(t, w.runtime.turns[domain.PipelineLeadRole], 2)
	second := w.prompt(domain.PipelineLeadRole, 2)
	require.Contains(t, second, "The @reviewer's turn ended: request_changes on "+shortSHA(sha(1))+".")
	require.Contains(t, second, "Persist the setting.")
	var digests []string
	require.NoError(t, w.f.db.Select(&digests, `SELECT body FROM task_messages WHERE task_id=$1 AND kind='summary' AND author_kind='kuayle' AND body LIKE 'The @reviewer''s turn ended%'`, w.root))
	require.Len(t, digests, 1, "once per turn")
	require.NotContains(t, digests[0], "the eighth line", "a digest, not the transcript")
}

// A change in a required check wakes the lead; a change in an advisory one
// reaches it at its next turn.
func TestTickARequiredCheckWakesTheLeadAndAnAdvisoryOneWaits(t *testing.T) {
	w := leadAtReview(t, func(p *domain.Pipeline) {
		p.Phases[2].Checks = append(p.Phases[2].Checks, domain.PipelineCheck{ID: "ready", Fact: domain.PipelineFactPRDraft})
	})
	w.github.status.Draft = true
	w.later()
	require.Equal(t, 1, w.runtime.turns[domain.PipelineLeadRole], "an advisory check changed: the lead is not woken")
	require.NotEmpty(t, w.facts(domain.LeadFactAdvisory))

	w.github.ci("passed", "")
	w.later()
	require.Equal(t, 2, w.runtime.turns[domain.PipelineLeadRole], "ci, a required check, holds now")
	prompt := w.prompt(domain.PipelineLeadRole, 2)
	require.Contains(t, prompt, "): checks = {")
	require.Contains(t, prompt, "- ready (Review): unmet.", "the advisory change, read at the next turn")
	require.Empty(t, w.leadSummaries(), "the reviewer works: nothing stalled")
}

// A question addressed to the lead wakes it; a message to it waits for its
// next turn (a question to everyone waits too: TestTheLeadWakesForItsSources).
func TestTickAQuestionToTheLeadWakesIt(t *testing.T) {
	w := leadAtReview(t, nil)
	post := func(role, body string) {
		_, err := w.f.tasks.Repo.RecordRoomMessage(w.f.ctx, w.root, domain.TaskRoomPost{AuthorKind: domain.RoomAuthorMember, AuthorRole: role, Kind: domain.RoomSay, Body: body, ToRoles: []string{domain.PipelineLeadRole}})
		require.NoError(t, err)
	}
	post("reviewer", "FYI: the settings page is next.")
	w.later()
	require.Equal(t, 1, w.runtime.turns[domain.PipelineLeadRole], "a message, not a question")

	_, err := w.f.tasks.Repo.RecordRoomMessage(w.f.ctx, w.root, domain.TaskRoomPost{AuthorKind: domain.RoomAuthorMember, AuthorRole: "reviewer", Kind: domain.RoomAsk, Body: "Shall I review the settings page too?", ToRoles: []string{domain.PipelineLeadRole}})
	require.NoError(t, err)
	w.later()
	require.Equal(t, 2, w.runtime.turns[domain.PipelineLeadRole])
	prompt := w.prompt(domain.PipelineLeadRole, 2)
	require.Contains(t, prompt, "Shall I review the settings page too?")
	require.Contains(t, prompt, "FYI: the settings page is next.", "what waited is read with it")
}

// A post from the owner wakes the lead; one the owner addressed to another
// role is not the lead's.
func TestTickTheOwnerWakesTheLead(t *testing.T) {
	w := leadAtReview(t, nil)
	w.agent = holdTheLead(w)
	_, err := w.f.tasks.Repo.PostRoomMessage(w.f.ctx, w.f.ws, w.root, domain.TaskRoomPost{Kind: domain.RoomAsk, ToRoles: []string{"reviewer"}, Body: "Check the contrast too."})
	require.NoError(t, err)
	w.drain()
	require.Equal(t, 1, w.runtime.turns[domain.PipelineLeadRole])
	_, err = w.f.tasks.Repo.PostRoomMessage(w.f.ctx, w.f.ws, w.root, domain.TaskRoomPost{Kind: domain.RoomSay, Body: "Keep the change small."})
	require.NoError(t, err)
	w.drain()
	require.Equal(t, 2, w.runtime.turns[domain.PipelineLeadRole])
	require.Contains(t, w.prompt(domain.PipelineLeadRole, 2), "Keep the change small.")
}

// The budget passing 50, 80 and 100 percent wakes the lead, once each; at
// 100 percent the task pauses for the owner and the lead reads it when it
// continues.
func TestTickTheBudgetWakesTheLeadAtEachMark(t *testing.T) {
	w := leadAtReview(t, func(p *domain.Pipeline) { p.Budget.MaxMinutes = 100 })
	spend := func(minutes int) {
		w.exec(`UPDATE agent_tasks SET workflow_started_at=workflow_started_at-$2::interval,workflow_budget_from=workflow_budget_from-$2::interval WHERE id=$1`, w.root, fmt.Sprintf("%d minutes", minutes))
		w.later()
	}
	marks := func() []int {
		var out []int
		for _, m := range w.facts(domain.LeadFactBudget) {
			require.Equal(t, []string{domain.PipelineLeadRole}, []string(m.ToRoles))
			var value struct {
				Percent int `json:"percent"`
			}
			require.NoError(t, json.Unmarshal(m.Data.Value, &value))
			out = append(out, value.Percent)
		}
		return out
	}
	spend(55)
	require.Equal(t, []int{50}, marks())
	require.Equal(t, 2, w.runtime.turns[domain.PipelineLeadRole])
	require.Contains(t, w.prompt(domain.PipelineLeadRole, 2), "used 50% of its budget")
	spend(1)
	require.Equal(t, []int{50}, marks(), "once per mark")
	spend(26)
	require.Equal(t, []int{50, 80}, marks())
	require.Equal(t, 3, w.runtime.turns[domain.PipelineLeadRole])
	spend(25)
	require.Equal(t, []int{50, 80, 100}, marks())
	paused := w.task()
	require.Equal(t, domain.WorkflowStatusPaused, *paused.WorkflowStatus, "the time budget is used up: the owner decides")
	require.Equal(t, 3, w.runtime.turns[domain.PipelineLeadRole], "no turn starts past the budget")
	require.NoError(t, w.decide(WorkflowDecisionContinue))
	w.drain()
	require.Equal(t, 4, w.runtime.turns[domain.PipelineLeadRole])
	require.Contains(t, w.prompt(domain.PipelineLeadRole, 4), "used its whole budget")
}

// Events are batched: whatever reaches the lead while it works is read in
// its next turn, one turn however many sources call it, and a pass starts
// at most one turn of the lead.
func TestTickTheLeadReadsWhatCameTogetherInOneTurn(t *testing.T) {
	w := leadAtReview(t, nil)
	w.agent = holdTheLead(w)
	_, err := w.f.tasks.Repo.PostRoomMessage(w.f.ctx, w.f.ws, w.root, domain.TaskRoomPost{Kind: domain.RoomAsk, ToRoles: []string{domain.PipelineLeadRole}, Body: "Where are we?"})
	require.NoError(t, err)
	w.drain()
	require.Equal(t, 2, w.runtime.turns[domain.PipelineLeadRole], "the lead works on the owner's question")

	// While it works: the owner writes, a role asks it, CI passes and the
	// reviewer's turn ends.
	_, err = w.f.tasks.Repo.PostRoomMessage(w.f.ctx, w.f.ws, w.root, domain.TaskRoomPost{Kind: domain.RoomSay, ToRoles: []string{domain.PipelineLeadRole}, Body: "Ship it this week."})
	require.NoError(t, err)
	_, err = w.f.tasks.Repo.RecordRoomMessage(w.f.ctx, w.root, domain.TaskRoomPost{AuthorKind: domain.RoomAuthorMember, AuthorRole: "reviewer", Kind: domain.RoomAsk, Body: "Is dark mode the default?", ToRoles: []string{domain.PipelineLeadRole}})
	require.NoError(t, err)
	w.github.ci("passed", "")
	w.finish("reviewer", tickTurn{final: "Verdict: approve\nThe toggle works."})
	w.later()
	require.Equal(t, 2, w.runtime.turns[domain.PipelineLeadRole], "a lead that works is not started again")

	w.finish(domain.PipelineLeadRole, tickTurn{final: "Answered the owner."})
	w.drain()
	require.Equal(t, 3, w.runtime.turns[domain.PipelineLeadRole], "one turn for all of it")
	prompt := w.prompt(domain.PipelineLeadRole, 3)
	for _, want := range []string{"Ship it this week.", "Is dark mode the default?", "The @reviewer's turn ended: approve", "): checks = {"} {
		require.Contains(t, prompt, want)
	}
	var started int
	require.NoError(t, w.f.db.Get(&started, `SELECT count(*) FROM agent_task_transitions tr JOIN task_members m ON m.member_task_id=tr.task_id WHERE m.task_id=$1 AND m.role=$2 AND tr.to_state='queued'`, w.root, domain.PipelineLeadRole))
	require.Equal(t, 3, started, "each of its turns was started once")
}

func TestLeadSummaryPublishesOneTaskNotification(t *testing.T) {
	w := newTickWorld(t, withLead)
	events := &userEventsFake{}
	w.f.tasks.SetNotifications(w.f.tasks.Repo, &notificationsFake{}, events)
	w.agent = func(role string, turn int, task domain.AgentTask) tickTurn {
		// The lead also wakes for each role's turn and for the checks
		// (KUA-302): it reports only when the owner asks.
		if prompts := w.runtime.prompts[role]; role == domain.PipelineLeadRole && len(prompts) > 0 && strings.Contains(prompts[len(prompts)-1], "Please report.") {
			return tickTurn{final: "Reported progress.", posts: []string{`{"to":"@human","kind":"summary","body":"The tests passed."}`}}
		}
		if role == domain.PipelineLeadRole {
			// It moves the phases (KUA-301), so the task does not stall.
			return leadMove("merge", "Everything else is Kuayle's to check.")
		}
		return w.writesAndApproves(role, turn, task)
	}
	w.drain()
	_, err := w.f.tasks.Repo.PostRoomMessage(w.f.ctx, w.f.ws, w.root, domain.TaskRoomPost{Kind: domain.RoomSay, ToRoles: []string{"lead"}, Body: "Please report."})
	require.NoError(t, err)
	w.drain()
	w.later()
	require.Len(t, events.events, 1)
	e := events.events[0]
	require.Equal(t, w.f.user, e.user)
	require.Equal(t, w.f.ws, e.workspace)
	require.Equal(t, "notification.created", e.kind)
	require.Equal(t, w.root.String(), e.payload["task_id"])
	require.Equal(t, w.f.issue.String(), e.payload["issue_id"])
	require.Contains(t, e.payload["title"], "The tests passed.")
}
