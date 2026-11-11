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

// A workspace names a profile's role as it likes and chooses what it does.
// Its behaviour is fixed once a workflow uses it; the label never is.
func TestProfilesTakeAnyRoleLabelAndOneOfThreeBehaviours(t *testing.T) {
	f := newProfileFixture(t)
	request := func(name, role, behaviour string) AgentProfileRequest {
		return AgentProfileRequest{Name: name, Role: role, Behaviour: behaviour, Harness: "codex"}
	}
	list, err := f.workflows.Profiles(f.ctx, f.ws)
	require.NoError(t, err)
	require.Equal(t, []string{"plans", "changes_code", "reviews"}, list.Behaviours)
	builtin := map[string]string{}
	for _, p := range list.Profiles {
		builtin[p.Name] = p.Behaviour + "/" + p.Role
	}
	require.Equal(t, map[string]string{"Implementer": "changes_code/Implementer", "Reviewer": "reviews/Reviewer"}, builtin)

	planner, err := f.workflows.CreateProfile(f.ctx, f.ws, request("Architect", "  Staff engineer · platform ", domain.AgentBehaviourPlans))
	require.NoError(t, err)
	require.Equal(t, "Staff engineer · platform", planner.Role, "the label is stored trimmed")
	require.Equal(t, domain.AgentBehaviourPlans, planner.Behaviour)
	builder, err := f.workflows.CreateProfile(f.ctx, f.ws, request("Builder", "reviewer", domain.AgentBehaviourChangesCode))
	require.NoError(t, err, "a label that was once a role means nothing to the engine")
	unlabelled, err := f.workflows.CreateProfile(f.ctx, f.ws, request("Critic", "", domain.AgentBehaviourReviews))
	require.NoError(t, err)
	require.Empty(t, unlabelled.Role)

	_, err = f.workflows.CreateProfile(f.ctx, f.ws, request("Bad", "x", "implementer"))
	requireProfileRefusal(t, err, "choose what the agent does: plans, changes code or reviews")
	_, err = f.workflows.CreateProfile(f.ctx, f.ws, request("Bad", "x", ""))
	requireProfileRefusal(t, err, "choose what the agent does: plans, changes code or reviews")
	for _, role := range []string{strings.Repeat("r", 41), "two\nlines", "hidden‮override"} {
		_, err = f.workflows.CreateProfile(f.ctx, f.ws, request("Bad", role, domain.AgentBehaviourPlans))
		requireProfileRefusal(t, err, "the role is at most 40 characters on one line")
	}

	// A workflow that plans first: the plan step is the delegated iteration,
	// on the planner's harness, and the attempt learns that it plans.
	graph, err := json.Marshal(domain.WorkflowGraph{
		Nodes: []domain.WorkflowNode{
			{ID: "start", Type: domain.WorkflowNodeStart, Label: "Start"},
			{ID: "plan", Type: domain.WorkflowNodeAgent, Label: "Plan", ProfileID: &planner.ID},
			{ID: "build", Type: domain.WorkflowNodeAgent, Label: "Build", ProfileID: &builder.ID, Prompt: "{{plan}}"},
			{ID: "done", Type: domain.WorkflowNodeEnd, Label: "Done", Outcome: domain.WorkflowEndDone},
			{ID: "failed", Type: domain.WorkflowNodeEnd, Label: "Failed", Outcome: domain.WorkflowEndFailed},
		},
		Edges: []domain.WorkflowEdge{
			{ID: "start-plan", Source: "start", Target: "plan", Condition: domain.WorkflowElse},
			{ID: "plan-build", Source: "plan", Target: "build", Condition: domain.WorkflowAgentFinished},
			{ID: "plan-failed", Source: "plan", Target: "failed", Condition: domain.WorkflowAgentFailed},
			{ID: "build-done", Source: "build", Target: "done", Condition: domain.WorkflowAgentFinished},
			{ID: "build-failed", Source: "build", Target: "failed", Condition: domain.WorkflowAgentFailed},
		},
	})
	require.NoError(t, err)
	issues, err := f.workflows.Validate(f.ctx, f.ws, graph)
	require.NoError(t, err)
	require.Empty(t, issues)
	workflow, err := f.workflows.Create(f.ctx, f.ws, WorkflowRequest{Name: "Plan then build", Graph: graph})
	require.NoError(t, err)
	_, err = f.workflows.SetDefault(f.ctx, f.ws, WorkflowDefaultRequest{Scope: domain.WorkflowScopeWorkspace, WorkflowID: &workflow.ID})
	require.NoError(t, err)
	chat, err := repository.NewConversationRepository(f.db).OpenIssue(f.ctx, f.ws, f.issue)
	require.NoError(t, err)
	task, err := f.tasks.Create(f.ctx, f.ws, chat.ID, domain.AgentTaskRequest{RequestID: uuid.New(), Prompt: "Add dark mode", Provider: "claude-code"})
	require.NoError(t, err)
	require.Equal(t, "plan", *task.WorkflowNode)
	require.Equal(t, "codex", task.Provider)
	bundle, err := repository.NewAgentTaskRepository(f.db).ProfileBundle(f.ctx, *task)
	require.NoError(t, err)
	require.Equal(t, domain.AgentBehaviourPlans, bundle.Behaviour)
	require.Equal(t, "Staff engineer · platform", bundle.Role)

	// In use: what the profile does is fixed, what it is called is not.
	_, err = f.workflows.UpdateProfile(f.ctx, f.ws, planner.ID, request("Architect", "Staff engineer", domain.AgentBehaviourChangesCode))
	require.ErrorIs(t, err, repository.ErrAgentProfileBehaviourInUse)
	relabelled, err := f.workflows.UpdateProfile(f.ctx, f.ws, planner.ID, request("Architect", "Principal", domain.AgentBehaviourPlans))
	require.NoError(t, err)
	require.Equal(t, "Principal", relabelled.Role)
	_, err = f.workflows.UpdateProfile(f.ctx, f.ws, unlabelled.ID, request("Critic", "Sceptic", domain.AgentBehaviourPlans))
	require.NoError(t, err, "an unused profile may change behaviour")

	// A review step cannot follow a plan directly: nothing is published.
	bad := strings.Replace(string(graph), builder.ID.String(), list.Profiles[1].ID.String(), 1)
	issues, err = f.workflows.Validate(f.ctx, f.ws, json.RawMessage(bad))
	require.NoError(t, err)
	codes := []string{}
	for _, issue := range issues {
		codes = append(codes, issue.Code)
	}
	require.Contains(t, codes, "plan_target")
}

// Validation warns, without refusing, when a planning profile runs a step
// named or told to implement (KUA-122).
func TestValidationWarnsAboutAPlanningStepToldToImplement(t *testing.T) {
	f := newProfileFixture(t)
	planner, err := f.workflows.CreateProfile(f.ctx, f.ws, AgentProfileRequest{Name: "Orchestrator", Behaviour: domain.AgentBehaviourPlans, Harness: "codex"})
	require.NoError(t, err)
	builder, err := f.workflows.CreateProfile(f.ctx, f.ws, AgentProfileRequest{Name: "Builder", Behaviour: domain.AgentBehaviourChangesCode, Harness: "codex"})
	require.NoError(t, err)
	graph, err := json.Marshal(domain.WorkflowGraph{
		Nodes: []domain.WorkflowNode{
			{ID: "start", Type: domain.WorkflowNodeStart, Label: "Start"},
			{ID: "plan", Type: domain.WorkflowNodeAgent, Label: "Implementer", ProfileID: &planner.ID, Prompt: "List the changes needed, then implement them. Run the relevant tests."},
			{ID: "build", Type: domain.WorkflowNodeAgent, Label: "Build", ProfileID: &builder.ID, Prompt: "{{plan}}"},
			{ID: "done", Type: domain.WorkflowNodeEnd, Label: "Done", Outcome: domain.WorkflowEndDone},
			{ID: "failed", Type: domain.WorkflowNodeEnd, Label: "Failed", Outcome: domain.WorkflowEndFailed},
		},
		Edges: []domain.WorkflowEdge{
			{ID: "start-plan", Source: "start", Target: "plan", Condition: domain.WorkflowElse},
			{ID: "plan-build", Source: "plan", Target: "build", Condition: domain.WorkflowAgentFinished},
			{ID: "plan-failed", Source: "plan", Target: "failed", Condition: domain.WorkflowAgentFailed},
			{ID: "build-done", Source: "build", Target: "done", Condition: domain.WorkflowAgentFinished},
			{ID: "build-failed", Source: "build", Target: "failed", Condition: domain.WorkflowAgentFailed},
		},
	})
	require.NoError(t, err)
	issues, err := f.workflows.Validate(f.ctx, f.ws, graph)
	require.NoError(t, err)
	require.Empty(t, issues, "a warning does not refuse the workflow")
	warnings, err := f.workflows.Warnings(f.ctx, f.ws, graph)
	require.NoError(t, err)
	require.Len(t, warnings, 2)
	for _, warning := range warnings {
		require.Equal(t, "planning_step_implements", warning.Code)
		require.Equal(t, "plan", warning.NodeID)
	}
	_, err = f.workflows.Create(f.ctx, f.ws, WorkflowRequest{Name: "Warned", Graph: graph})
	require.NoError(t, err, "it can still be saved")
}
