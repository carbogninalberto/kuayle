package service

import (
	"encoding/json"
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
		if role == domain.PipelineLeadRole {
			return tickTurn{final: "Asked the implementer to persist the toggle.", posts: []string{`{"to":"@implementer","kind":"ask","body":"Is the toggle persisted?"}`}}
		}
		return w.writesAndApproves(role, turn, task)
	}
	w.drain()
	require.Zero(t, w.runtime.turns[domain.PipelineLeadRole], "nobody called the lead")
	_, err := w.f.tasks.Repo.PostRoomMessage(w.f.ctx, w.f.ws, w.root, domain.TaskRoomPost{Kind: domain.RoomSay, ToRoles: []string{domain.PipelineLeadRole}, Body: "Make sure the toggle is persisted."})
	require.NoError(t, err)
	w.drain()

	require.Equal(t, 1, w.runtime.turns[domain.PipelineLeadRole])
	lead := w.member(domain.PipelineLeadRole)
	require.NotNil(t, lead.MemberTaskID)
	require.False(t, lead.VerdictAsked)
	require.Equal(t, sha(1), *lead.PinnedCommit)
	var kind string
	require.NoError(t, w.f.db.Get(&kind, `SELECT kind FROM agent_tasks WHERE id=$1`, *lead.MemberTaskID))
	require.Equal(t, domain.AgentTaskKindReview, kind)
	prompt := w.prompt(domain.PipelineLeadRole, 1)
	require.Contains(t, prompt, "You are the @lead")
	require.Contains(t, prompt, "Make sure the toggle is persisted.")
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

func TestLeadSummaryPublishesOneTaskNotification(t *testing.T) {
	w := newTickWorld(t, withLead)
	events := &userEventsFake{}
	w.f.tasks.SetNotifications(w.f.tasks.Repo, &notificationsFake{}, events)
	w.agent = func(role string, turn int, task domain.AgentTask) tickTurn {
		if role == domain.PipelineLeadRole {
			return tickTurn{final: "Reported progress.", posts: []string{`{"to":"@human","kind":"summary","body":"The tests passed."}`}}
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
