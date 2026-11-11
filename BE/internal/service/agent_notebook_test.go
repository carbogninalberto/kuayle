package service

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/agent"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/stretchr/testify/require"
)

type notebookSourceFake struct{ notebook domain.Notebook }

func (f *notebookSourceFake) AttemptNotebook(context.Context, domain.AgentTask) (*domain.Notebook, error) {
	copied := f.notebook
	return &copied, nil
}

type profileSourceFake struct{ bundle *domain.AgentProfileBundle }

func (f profileSourceFake) ProfileBundle(context.Context, domain.AgentTask) (*domain.AgentProfileBundle, error) {
	if f.bundle == nil {
		return nil, nil
	}
	copied := *f.bundle
	return &copied, nil
}

// notebookAttempt runs one attempt of a task on the harness with the
// notebook and profile given, and returns the run Kuayle created for it.
func notebookAttempt(t *testing.T, provider string, notebook *domain.Notebook, bundle *domain.AgentProfileBundle, change func(*domain.AgentTask)) domain.DevMachineAgentRun {
	t.Helper()
	ws, machine, user, checkout := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	store := &taskPolicyStore{devMachineStoreFake: &devMachineStoreFake{
		policy:        testPolicy(ws),
		machine:       &domain.DevMachine{ID: machine, WorkspaceID: ws, CreatedByUserID: &user, Status: domain.DevMachineStatusRunning, DesiredStatus: domain.DevMachineStatusRunning},
		agentProvider: &domain.DevMachineAgentProvider{ProviderID: provider, Enabled: true, RequiredSecrets: json.RawMessage(`[]`), Config: json.RawMessage(`{}`)},
		checkouts:     []domain.DevMachineCheckout{{ID: checkout, WorkspaceID: ws, MachineID: machine, Status: "ready", WorkspacePath: "/workspace/tasks/check", RepositoryFullName: "example/test", BaseBranch: "main", WorkingBranch: "task/check"}},
	}}
	store.policy.AllowedProviders = json.RawMessage(`["opencode","codex","claude-code","qwen-code"]`)
	machines := newTestDevMachineService(store)
	machines.agents = agent.NewRegistry(agent.NewOpenCodeProvider(""), agent.NewCodexProvider(""), agent.NewClaudeCodeProvider(""), agent.NewQwenCodeProvider(""))
	runtime := &devMachineTaskRuntime{machines: machines, profiles: profileSourceFake{bundle}}
	if notebook != nil {
		runtime.notebooks = &notebookSourceFake{*notebook}
	}
	task := domain.AgentTask{ID: uuid.New(), WorkspaceID: ws, CreatorID: user, MachineID: &machine, CheckoutID: checkout, RunID: uuid.New(), State: "preparing", Provider: provider, Prompt: "Implement dark mode", Kind: domain.AgentTaskKindImplement, Generation: 1}
	if change != nil {
		change(&task)
	}
	_, err := runtime.Attempt(context.Background(), task)
	require.NoError(t, err)
	require.Len(t, store.agentRuns, 1)
	return store.agentRuns[0]
}

func recordedNotebook() domain.Notebook {
	notebook := domain.NewNotebook()
	author := uuid.New()
	notebook.AddInstruction(domain.NotebookInstruction{Source: domain.NotebookInstructionRequest, AuthorID: &author, Text: "Implement dark mode", At: time.Now()})
	notebook.SetPlan("1. Add the theme tokens\n2. Switch the toggle")
	update, _ := domain.ParseNotebookUpdate([]byte(`{"done":["Theme tokens added in UI/src/app.css"],"remaining":["Switch the toggle"]}`), false)
	notebook.ApplyUpdate(update, time.Now())
	return notebook
}

// The whole notebook reaches the agent as instructions on the path each
// harness reads them from: its instructions file for Claude Code, Codex and
// OpenCode (after the profile's own instructions), and ahead of the prompt
// for a harness that reads no such file. Every such attempt must update it.
func TestAttemptReceivesTheWholeNotebookOnEachHarnessInstructionPath(t *testing.T) {
	notebook := recordedNotebook()
	rendered := notebook.Render()
	bundle := &domain.AgentProfileBundle{Name: "Builder", Behaviour: domain.AgentBehaviourChangesCode, Instructions: "Write small commits."}
	for provider, file := range map[string]string{
		"claude-code": "/home/kuayle/.claude/CLAUDE.md",
		"codex":       "/home/kuayle/.codex/AGENTS.md",
		"opencode":    "/home/kuayle/.config/opencode/AGENTS.md",
	} {
		t.Run(provider, func(t *testing.T) {
			run := notebookAttempt(t, provider, &notebook, bundle, nil)
			var files []domain.AgentProfileFile
			require.NoError(t, json.Unmarshal(run.ProfileFiles, &files))
			require.Len(t, files, 1)
			require.Equal(t, file, files[0].Path)
			require.Contains(t, files[0].Content, rendered, "the notebook is whole")
			require.Less(t, strings.Index(files[0].Content, "Write small commits."), strings.Index(files[0].Content, domain.NotebookHeading))
			require.NotContains(t, run.Prompt, domain.NotebookHeading, "not repeated in the prompt")
			require.Contains(t, run.Prompt, domain.NotebookUpdateHelp)
			require.True(t, run.NotebookRequired)
			// A second attempt sees what the first recorded as done.
			require.Contains(t, files[0].Content, "Theme tokens added in UI/src/app.css")
		})
	}
	t.Run("a harness without an instructions file", func(t *testing.T) {
		run := notebookAttempt(t, "qwen-code", &notebook, nil, nil)
		require.Contains(t, run.Prompt, rendered)
		require.Contains(t, run.Prompt, domain.NotebookUpdateHelp)
		require.True(t, run.NotebookRequired)
	})
	t.Run("without a profile the file holds the notebook alone", func(t *testing.T) {
		run := notebookAttempt(t, "codex", &notebook, nil, nil)
		var files []domain.AgentProfileFile
		require.NoError(t, json.Unmarshal(run.ProfileFiles, &files))
		require.Len(t, files, 1)
		require.Equal(t, rendered, files[0].Content)
	})
	t.Run("a review neither receives nor updates it", func(t *testing.T) {
		run := notebookAttempt(t, "codex", &notebook, nil, func(task *domain.AgentTask) { task.Kind = domain.AgentTaskKindReview })
		require.False(t, run.NotebookRequired)
		require.NotContains(t, run.Prompt, domain.NotebookUpdateHelp)
		require.NotContains(t, string(run.ProfileFiles), domain.NotebookHeading)
	})
}

// A planning step's worktree is mounted read-only on the harnesses that run
// that way; a step that changes code, an agent identity's own task and the
// other harnesses keep a writable worktree.
func TestOnlyAPlanningStepGetsAReadOnlyWorktree(t *testing.T) {
	notebook := recordedNotebook()
	version := uuid.New()
	workflow := func(task *domain.AgentTask) {
		node := "plan"
		task.WorkflowVersionID, task.WorkflowNode = &version, &node
	}
	planner := &domain.AgentProfileBundle{Name: "Planner", Behaviour: domain.AgentBehaviourPlans}
	builder := &domain.AgentProfileBundle{Name: "Builder", Behaviour: domain.AgentBehaviourChangesCode}
	for _, provider := range []string{"claude-code", "codex", "opencode"} {
		require.True(t, domain.ReadOnlyWorktreeSupported(provider))
		planning := notebookAttempt(t, provider, &notebook, planner, workflow)
		require.True(t, planning.WorktreeReadOnly, provider)
		require.True(t, planning.NotebookRequired, "a planning step updates the notebook too: its outbox stays writable")
		require.Contains(t, planning.Prompt, "The only place you write is Kuayle's own `.kuayle/` directory")
		require.False(t, notebookAttempt(t, provider, &notebook, builder, workflow).WorktreeReadOnly, provider)
		require.False(t, notebookAttempt(t, provider, &notebook, planner, nil).WorktreeReadOnly, "outside a workflow a planning profile runs the built-in flow")
	}
	// The planning frame goes ahead of the task and Kuayle's lines after it;
	// the task itself stays attributed to the task.
	var sources []domain.PromptSource
	require.NoError(t, json.Unmarshal(notebookAttempt(t, "codex", &notebook, planner, workflow).PromptSources, &sources))
	require.Equal(t, domain.PromptSourceContract, sources[0].Source)
	require.True(t, strings.HasPrefix(sources[0].Text, "Planning step:"))
	require.Equal(t, domain.PromptSourceTask, sources[1].Source)
	require.Equal(t, "Implement dark mode", sources[1].Text)
	require.False(t, domain.ReadOnlyWorktreeSupported("crush"))
	require.False(t, notebookAttempt(t, "qwen-code", &notebook, planner, workflow).WorktreeReadOnly, "the check after the run is this harness's only guard")
}

// The prompt an attempt received is recorded split by source, and the parts
// of the prompt channel, in order, are the prompt exactly as it was sent.
func TestAttemptRecordsItsPromptBySource(t *testing.T) {
	notebook := recordedNotebook()
	plan := "1. Add the theme tokens\n2. Switch the toggle"
	version := uuid.New()
	bundle := &domain.AgentProfileBundle{Name: "Builder", Behaviour: domain.AgentBehaviourChangesCode, Instructions: "Write small commits."}
	run := notebookAttempt(t, "codex", &notebook, bundle, func(task *domain.AgentTask) {
		node := "implement"
		task.WorkflowVersionID, task.WorkflowNode = &version, &node
		task.Prompt = "Implement dark mode" + workflowStepMarker + "Implement (visit 1):\nBuild it.\n\n" + domain.WorkflowPlanHeading + "\n" + plan
		task.AgentInstructions = "Use pnpm."
	})
	var sources []domain.PromptSource
	require.NoError(t, json.Unmarshal(run.PromptSources, &sources))
	prompt := ""
	by := map[string]string{}
	for _, source := range sources {
		if source.Channel == domain.PromptChannelPrompt {
			prompt += source.Text
		}
		by[source.Source+"/"+source.Channel] += source.Text
	}
	require.Equal(t, run.Prompt, prompt, "the prompt parts are the prompt")
	require.Equal(t, "Implement dark mode", by["task/prompt"])
	require.Contains(t, by["workflow_step/prompt"], "Build it.")
	require.Equal(t, domain.WorkflowPlanHeading+"\n"+plan, by["plan/prompt"])
	require.Contains(t, by["project/prompt"], "Use pnpm.")
	require.Contains(t, by["contract/prompt"], domain.NotebookUpdateHelp)
	require.Contains(t, by["contract/prompt"], "Kuayle execution contract")
	require.Contains(t, by["profile/instructions_file"], "Write small commits.")
	require.Equal(t, notebook.Render(), by["notebook/instructions_file"])
	// The run's public JSON (a list of runs) never carries it.
	encoded, err := json.Marshal(run)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "prompt_sources")
	require.NotContains(t, string(encoded), "Write small commits.")
}

func TestPromptAssemblyAttributesEveryStep(t *testing.T) {
	a := newPromptAssembly("Fix the bug"+followUpMarker+"GitHub:\nCI failed", "")
	prompt := a.text()
	prompt = a.apply(domain.PromptSourceIssue, "Issue ENG-1: Bug\n\n---\n"+prompt)
	prompt = a.apply(domain.PromptSourceContract, prompt+"\n\nPublishing: Kuayle commits.")
	// A step that cuts the end of the prompt and appends its own section.
	prompt = a.apply(domain.PromptSourceHandoff, strings.TrimSuffix(prompt, " commits.")+handoffMarker+" facts")
	// The first iteration of a workflow step: project, profile and step.
	instructions := "Use pnpm.\n\n" + workflowProfileTitle + "Builder:\nSmall commits.\n\n" + workflowStepTitle + "Implement:\nBuild it."
	prompt = a.applyInstructions(withInstructions(prompt, instructions))
	require.Equal(t, prompt, a.text())
	by := map[string]string{}
	for _, part := range a.sources() {
		by[part.Source] += part.Text
	}
	require.Equal(t, "Fix the bug", by[domain.PromptSourceTask])
	require.Equal(t, "\n\nPublishing: Kuayle", by[domain.PromptSourceContract])
	require.Equal(t, followUpMarker+"GitHub:\nCI failed", by[domain.PromptSourceFollowUp])
	require.Equal(t, "Issue ENG-1: Bug\n\n---\n", by[domain.PromptSourceIssue])
	require.Equal(t, handoffMarker+" facts", by[domain.PromptSourceHandoff])
	require.Equal(t, workflowProfileTitle+"Builder:\nSmall commits.\n\n", by[domain.PromptSourceProfile])
	require.Equal(t, workflowStepTitle+"Implement:\nBuild it.", by[domain.PromptSourceWorkflowStep])
	require.Equal(t, projectInstructionsHeader+"Use pnpm.\n\n"+projectInstructionsSeparator, by[domain.PromptSourceProject])
}

func TestRecordedPromptIsBoundedAndRedacted(t *testing.T) {
	sources := domain.BoundPromptSources([]domain.PromptSource{
		{Source: domain.PromptSourceTask, Channel: domain.PromptChannelPrompt, Text: strings.Repeat("é", domain.PromptSourceBytes)},
		{Source: domain.PromptSourceNotebook, Channel: domain.PromptChannelInstructions, Text: "short"},
	})
	require.LessOrEqual(t, len(sources[0].Text), domain.PromptSourceBytes)
	require.True(t, sources[0].Truncated)
	require.False(t, sources[1].Truncated)
	var many []domain.PromptSource
	for range 40 {
		many = append(many, domain.PromptSource{Source: domain.PromptSourceTask, Text: strings.Repeat("a", domain.PromptSourceBytes)})
	}
	total := 0
	for _, source := range domain.BoundPromptSources(many) {
		total += len(source.Text)
	}
	require.LessOrEqual(t, total, domain.PromptSourcesBytes)

	machine := uuid.New()
	prompt := &repository.AgentRunPrompt{MachineID: machine, Sources: []domain.PromptSource{{Source: domain.PromptSourceTask, Text: "Deploy with token ghp_secret123 now"}}}
	redacted, err := redactRunPrompt(context.Background(), uuid.New(), prompt, func(_ context.Context, _, id uuid.UUID) ([]string, error) {
		require.Equal(t, machine, id)
		return []string{"ghp_secret123"}, nil
	})
	require.NoError(t, err)
	require.NotContains(t, redacted.Sources[0].Text, "ghp_secret123")
	require.Contains(t, redacted.Sources[0].Text, "Deploy with token")
}

// A run that ended without a valid notebook update fails with that reason,
// like a run without a final message, in a workflow and outside one, and the
// next attempt is told why.
func TestStepWithoutAValidNotebookUpdateFailsAndTheRetryIsToldWhy(t *testing.T) {
	message := domain.NotebookUpdateReason(`the update is not a JSON object with the notebook's fields: unknown field "status"`)
	store := &taskStoreFake{task: domain.AgentTask{State: "running", Provider: "opencode"}}
	runtime := &taskRuntimeFake{run: &domain.DevMachineAgentRun{Status: domain.DevMachineAgentRunStatusFailed, ErrorMessage: &message}}
	svc := &AgentTaskService{store: store, runtime: runtime}
	require.NoError(t, svc.step(context.Background(), store.task))
	require.Equal(t, message, store.failReason)

	h := newPlanHarness(t, "{{plan}}")
	h.w.runs[h.w.task.RunID] = &repository.WorkflowRun{Status: "failed"}
	require.NoError(t, h.runner.AgentFailedBecause(context.Background(), h.w.task, message))
	require.Contains(t, h.w.task.Reason, "without a valid update of the task notebook")

	previous := &domain.AgentHandoff{AttemptID: uuid.New(), Status: "failed", NotebookProblem: message}
	prompt := withHandoff("Fix it", previous, false, false)
	require.Contains(t, prompt, "failed only because it left no valid update of the task notebook")
	require.Contains(t, prompt, `unknown field "status"`)
	require.Contains(t, prompt, ".kuayle/outbox/notebook.json")
	require.NotContains(t, withHandoff("Fix it", &domain.AgentHandoff{AttemptID: uuid.New(), Status: "failed"}, false, false), "task notebook")
}

// What a follow-up adds to the notebook: CI and a GitHub review are
// findings, never a person's instruction; a member's comment is one, without
// an author Kuayle cannot name.
func TestFollowUpsAreRecordedAsFindingsOrInstructions(t *testing.T) {
	ci := followUpNote(domain.AgentTaskFollowUp{Signal: domain.AgentTaskSignal{Kind: "ci", Name: "tests", Outcome: "failure", URL: "https://github.com/a/b/pull/1"}})
	require.Nil(t, ci.Instruction)
	require.Equal(t, domain.NotebookFindingCI, ci.FindingSource)
	review := followUpNote(domain.AgentTaskFollowUp{Signal: domain.AgentTaskSignal{Kind: "review", Name: "octocat", Summary: "Rename the flag"}})
	require.Nil(t, review.Instruction)
	require.Equal(t, domain.NotebookFindingReview, review.FindingSource)
	require.Contains(t, review.Finding, "Rename the flag")
	comment := followUpNote(domain.AgentTaskFollowUp{Signal: domain.AgentTaskSignal{Kind: "comment", Name: "Ada", Summary: "Use the blue token"}})
	require.Equal(t, domain.NotebookInstructionComment, comment.Instruction.Source)
	require.Nil(t, comment.Instruction.AuthorID)
	require.False(t, comment.Instruction.Assistant)
}
