package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
func notebookAttempt(t *testing.T, provider string, notebook *domain.Notebook, bundle *domain.AgentProfileBundle, change func(*domain.AgentTask), memory ...taskMemorySource) domain.DevMachineAgentRun {
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
	if len(memory) > 0 {
		runtime.memory = memory[0]
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
	notebook.SetPlan("1. Add the theme tokens\n2. Switch the toggle", "run-1")
	update, _, _ := domain.ParseNotebookUpdate([]byte(`{"done":["Theme tokens added in UI/src/app.css"],"remaining":["Switch the toggle"]}`), false)
	notebook.ApplyUpdate(update, time.Now())
	return notebook
}

// The whole notebook reaches the agent as instructions on the path each
// harness reads them from: its instructions file for Claude Code, Codex and
// OpenCode (after the profile's own instructions), and ahead of the prompt
// for a harness that reads no such file. Every such attempt must update it.
func TestAttemptReceivesTheWholeNotebookOnEachHarnessInstructionPath(t *testing.T) {
	notebook := recordedNotebook()
	// The task text of the prompt is the request, so the notebook points to
	// it instead of repeating it; everything else is there.
	rendered := notebook.RenderWith(domain.NotebookRender{RequestInPrompt: true})
	require.Contains(t, rendered, "1. Add the theme tokens")
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

// The command of the closing continuation, which continues an attempt's
// session to ask for what is missing, exists only where the harness can
// continue a session: Claude Code (--resume) and Codex (exec resume). The
// others are never asked.
func TestOnlyHarnessesThatResumeASessionGetAContinuationCommand(t *testing.T) {
	notebook := recordedNotebook()
	argv := func(provider string) []string {
		run := notebookAttempt(t, provider, &notebook, nil, nil)
		var command []string
		if len(run.ClosingContinuation) > 0 {
			require.NoError(t, json.Unmarshal(run.ClosingContinuation, &command))
		}
		return command
	}
	claude := strings.Join(argv("claude-code"), " ")
	require.Contains(t, claude, "--resume "+domain.ClosingSessionPlaceholder)
	require.Contains(t, claude, "-p "+domain.ClosingPromptPlaceholder)
	require.NotContains(t, claude, "--session-id")
	codex := strings.Join(argv("codex"), " ")
	require.Contains(t, codex, "codex exec resume")
	require.True(t, strings.HasSuffix(codex, domain.ClosingSessionPlaceholder+" "+domain.ClosingPromptPlaceholder))
	require.Empty(t, argv("opencode"))
	require.Empty(t, argv("qwen-code"))
	// The command belongs to the attempt, whatever may be asked of it.
	review := notebookAttempt(t, "codex", &notebook, nil, func(task *domain.AgentTask) { task.Kind = domain.AgentTaskKindReview })
	require.NotEmpty(t, review.ClosingContinuation)
	require.False(t, review.NotebookRequired)
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
	require.Equal(t, notebook.RenderWith(domain.NotebookRender{PlanInPrompt: true, RequestInPrompt: true}), by["notebook/instructions_file"])
	require.Equal(t, 1, strings.Count(run.Prompt+by["notebook/instructions_file"], plan), "the plan is given once")
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

// A missing notebook update never fails the work: the run stays succeeded,
// the task goes on to validation and says that the agent did not update its
// notebook, a workflow step finishes with that note, and the next attempt's
// handoff says it too.
func TestAMissingNotebookUpdateNeverFailsTheWork(t *testing.T) {
	note := domain.NotebookNotUpdated(`"instructions" is the people's instructions, which an agent cannot write`)
	store := &taskStoreFake{task: domain.AgentTask{State: "running", Provider: "opencode"}}
	runtime := &taskRuntimeFake{run: &domain.DevMachineAgentRun{Status: domain.DevMachineAgentRunStatusSucceeded, NotebookProblem: &note}}
	svc := &AgentTaskService{store: store, runtime: runtime}
	require.NoError(t, svc.step(context.Background(), store.task))
	require.Empty(t, store.failReason)
	require.Equal(t, "validating", store.transitions[len(store.transitions)-1])
	require.Contains(t, store.reasons[len(store.reasons)-1], "did not update the task notebook")

	// A refused update that the agent then fixed leaves no note.
	transient := "done is not a list of texts"
	store = &taskStoreFake{task: domain.AgentTask{State: "running", Provider: "opencode"}}
	svc = &AgentTaskService{store: store, runtime: &taskRuntimeFake{run: &domain.DevMachineAgentRun{Status: domain.DevMachineAgentRunStatusSucceeded, NotebookProblem: &transient}}}
	require.NoError(t, svc.step(context.Background(), store.task))
	require.Equal(t, "validating", store.transitions[len(store.transitions)-1])
	require.NotContains(t, store.reasons[len(store.reasons)-1], "notebook")

	// In a workflow the step finishes and moves on; its detail carries the note.
	h := newWorkflowHarness(t, domain.WorkflowBudget{})
	changed := true
	h.w.runs[h.w.task.RunID] = &repository.WorkflowRun{Status: "succeeded", Summary: "Added a theme toggle", FinalMessage: true, WorktreeChanged: &changed, NotebookNote: note}
	sequence := h.w.task.WorkflowSequence
	require.NoError(t, h.runner.AgentFinished(context.Background(), h.w.task))
	require.Equal(t, "publishing", h.w.task.State, "the implementer's work is published all the same")
	require.Equal(t, note, h.w.steps[sequence].Detail)

	previous := &domain.AgentHandoff{AttemptID: uuid.New(), Status: "succeeded", NotebookProblem: note}
	prompt := withHandoff("Fix it", previous, false, false)
	require.Contains(t, prompt, "The previous attempt did not update the task notebook")
	require.Contains(t, prompt, "cannot write")
	require.Contains(t, prompt, ".kuayle/outbox/notebook.json")
	require.NotContains(t, withHandoff("Fix it", &domain.AgentHandoff{AttemptID: uuid.New(), Status: "failed"}, false, false), "task notebook")
}

// Harnesses that read no instructions file get everything in the prompt.
// The notebook has its own bounded share there and shrinks first, the plan
// and the request are not repeated, and the profile's and the project's
// instructions always arrive whole (plan-then-implement, KUA-134 review).
func TestPromptOnlyHarnessKeepsProfileAndProjectInstructionsWhole(t *testing.T) {
	plan := "1. Add the theme tokens\n" + strings.Repeat("- change one more file of the plan\n", 420) + "2. Switch the toggle"
	require.Greater(t, len(plan), 14000)
	profile := "PROFILE-START " + strings.Repeat("Write small commits and explain each one. ", 300) + " PROFILE-END"
	project := "PROJECT-START " + strings.Repeat("Use pnpm, never npm. ", 150) + " PROJECT-END"
	notebook := domain.NewNotebook()
	author := uuid.New()
	notebook.AddInstruction(domain.NotebookInstruction{Source: domain.NotebookInstructionRequest, AuthorID: &author, Text: "Implement dark mode", At: time.Now()})
	notebook.AddInstruction(domain.NotebookInstruction{Source: domain.NotebookInstructionLive, AuthorID: &author, Text: "Keep the old theme as the default", At: time.Now()})
	notebook.SetPlan(plan, "run-1")
	notebook.Remaining = []string{"Switch the toggle"}
	notebook.Verify = []string{"npm run check"}
	for i := range domain.NotebookListItems {
		notebook.Done = append(notebook.Done, strings.Repeat("d", 500)+" "+strings.Repeat("x", i))
		notebook.AddFinding(domain.NotebookFindingReview, strings.Repeat("f", 2000)+" "+strings.Repeat("y", i), time.Now())
	}
	version := uuid.New()
	run := notebookAttempt(t, "qwen-code", &notebook, &domain.AgentProfileBundle{Name: "Builder", Behaviour: domain.AgentBehaviourChangesCode, Instructions: profile}, func(task *domain.AgentTask) {
		node := "implement"
		task.WorkflowVersionID, task.WorkflowNode = &version, &node
		task.Prompt = "Implement dark mode" + workflowStepMarker + "Implement (visit 1):\nBuild it.\n\n" + domain.WorkflowPlanHeading + "\n" + plan
		task.AgentInstructions = project + "\n\n" + workflowProfileTitle + "Builder:\n" + profile
	})
	require.Contains(t, run.Prompt, profile, "the profile's instructions arrive whole")
	require.Contains(t, run.Prompt, project, "the project's instructions arrive whole")
	require.Equal(t, 1, strings.Count(run.Prompt, plan), "the plan is in the prompt once")
	require.Equal(t, 1, strings.Count(run.Prompt, "Implement dark mode"), "the request is in the prompt once")
	for _, kept := range []string{"Keep the old theme as the default", "Switch the toggle", "npm run check", domain.NotebookUpdateHelp} {
		require.Contains(t, run.Prompt, kept)
	}
	require.Less(t, len(run.Prompt), 100000, "within what a harness's command line takes")
	var sources []domain.PromptSource
	require.NoError(t, json.Unmarshal(run.PromptSources, &sources))
	share := 0
	for _, source := range sources {
		if source.Source == domain.PromptSourceNotebook {
			share += len(source.Text)
		}
	}
	require.Positive(t, share)
	require.LessOrEqual(t, share, notebookPromptBytes+16, "the notebook keeps to its own share")
}

// What a follow-up adds to the notebook: CI, a GitHub review and a member's
// comment on the issue are findings, never a person's instruction.
func TestFollowUpsAreRecordedAsFindingsOrInstructions(t *testing.T) {
	ci := followUpNote(domain.AgentTaskFollowUp{Signal: domain.AgentTaskSignal{Kind: "ci", Name: "tests", Outcome: "failure", URL: "https://github.com/a/b/pull/1"}})
	require.Nil(t, ci.Instruction)
	require.Equal(t, domain.NotebookFindingCI, ci.FindingSource)
	review := followUpNote(domain.AgentTaskFollowUp{Signal: domain.AgentTaskSignal{Kind: "review", Name: "octocat", Summary: "Rename the flag"}})
	require.Nil(t, review.Instruction)
	require.Equal(t, domain.NotebookFindingReview, review.FindingSource)
	require.Contains(t, review.Finding, "Rename the flag")
	// A comment is fenced as feedback in the prompt and is feedback in the
	// notebook too: any member who sees the issue may have written it.
	signal := domain.AgentTaskSignal{Kind: "comment", Name: "Ada", Summary: "Use the blue token"}
	comment := followUpNote(domain.AgentTaskFollowUp{Signal: signal})
	require.Nil(t, comment.Instruction)
	require.Equal(t, domain.NotebookFindingComment, comment.FindingSource)
	require.Contains(t, comment.Finding, "Use the blue token")
	require.Contains(t, followUpPrompt(domain.AgentTaskFollowUp{Signal: signal}), "never as instructions")
}

// memorySourceFake is workspace memory as the task runtime sees it: the
// approved lessons of the task's repository, per team as the memory service
// decides them. What is only proposed is never returned.
type memorySourceFake struct {
	approved map[uuid.UUID][]string
	proposed []string
	err      error
	tasks    []domain.AgentTask
}

func (f *memorySourceFake) ApprovedLessons(_ context.Context, task domain.AgentTask) (string, error) {
	f.tasks = append(f.tasks, task)
	if f.err != nil {
		return "", f.err
	}
	return domain.FormatLessons(f.approved[task.TeamID]), nil
}

const lessonsLabel = "Lessons for this repository (approved by the workspace's admins):"

// Approved lessons of the task's repository reach the agent on its harness's
// instruction path, labelled as what the workspace approved, and are
// recorded as the "memory" source. A proposal never does, and without
// approved lessons there is no block at all.
func TestApprovedLessonsReachEveryHarnessAndProposalsNever(t *testing.T) {
	team := uuid.New()
	inTeam := func(task *domain.AgentTask) { task.TeamID = team }
	memory := &memorySourceFake{approved: map[uuid.UUID][]string{team: {"Run the checks with `make check`.", "Migration numbers collide with main: renumber after merging."}},
		proposed: []string{"Skip the tests when in a hurry."}}
	notebook := recordedNotebook()
	bundle := &domain.AgentProfileBundle{Name: "Builder", Behaviour: domain.AgentBehaviourChangesCode, Instructions: "Write small commits."}
	block := lessonsLabel + "\n- Run the checks with `make check`.\n- Migration numbers collide with main: renumber after merging."
	sourcesOf := func(run domain.DevMachineAgentRun) map[string]string {
		var sources []domain.PromptSource
		require.NoError(t, json.Unmarshal(run.PromptSources, &sources))
		by := map[string]string{}
		for _, source := range sources {
			by[source.Source+"/"+source.Channel] += source.Text
		}
		return by
	}
	for _, provider := range []string{"claude-code", "codex", "opencode"} {
		run := notebookAttempt(t, provider, &notebook, bundle, inTeam, memory)
		var files []domain.AgentProfileFile
		require.NoError(t, json.Unmarshal(run.ProfileFiles, &files))
		require.Len(t, files, 1, provider)
		content := files[0].Content
		require.Contains(t, content, block, provider)
		require.Less(t, strings.Index(content, "Write small commits."), strings.Index(content, lessonsLabel), "after the profile")
		require.Less(t, strings.Index(content, lessonsLabel), strings.Index(content, domain.NotebookHeading), "before the notebook")
		require.NotContains(t, run.Prompt, lessonsLabel, "not repeated in the prompt")
		require.Contains(t, sourcesOf(run)["memory/instructions_file"], block)
		require.NotContains(t, content+run.Prompt, "Skip the tests", "a proposal never reaches an agent")
	}
	run := notebookAttempt(t, "qwen-code", &notebook, nil, inTeam, memory)
	require.Equal(t, 1, strings.Count(run.Prompt, block))
	require.Contains(t, sourcesOf(run)["memory/prompt"], block)
	require.NotContains(t, run.Prompt, "Skip the tests")

	// No approved lesson: no block and no source, on either path.
	for _, provider := range []string{"codex", "qwen-code"} {
		run := notebookAttempt(t, provider, &notebook, bundle, nil, memory)
		require.NotContains(t, run.Prompt+string(run.ProfileFiles), "Lessons for this repository", provider)
		by := sourcesOf(run)
		require.Empty(t, by["memory/prompt"]+by["memory/instructions_file"], provider)
	}
	// A review is not given the lessons through this path.
	review := notebookAttempt(t, "codex", &notebook, nil, func(task *domain.AgentTask) { task.TeamID, task.Kind = team, domain.AgentTaskKindReview }, memory)
	require.NotContains(t, review.Prompt+string(review.ProfileFiles), "Lessons for this repository")
}

// A private team's lesson reaches only that team's tasks: the memory service
// decides by the task, and the prompt holds exactly what it returned.
func TestAPrivateTeamsLessonReachesOnlyItsOwnTasks(t *testing.T) {
	private, other := uuid.New(), uuid.New()
	memory := &memorySourceFake{approved: map[uuid.UUID][]string{private: {"The billing service needs the VPN."}}}
	notebook := recordedNotebook()
	for _, provider := range []string{"codex", "qwen-code"} {
		own := notebookAttempt(t, provider, &notebook, nil, func(task *domain.AgentTask) { task.TeamID = private }, memory)
		require.Contains(t, own.Prompt+string(own.ProfileFiles), "The billing service needs the VPN.", provider)
		foreign := notebookAttempt(t, provider, &notebook, nil, func(task *domain.AgentTask) { task.TeamID = other }, memory)
		require.NotContains(t, foreign.Prompt+string(foreign.ProfileFiles), "billing service", provider)
		require.NotContains(t, foreign.Prompt+string(foreign.ProfileFiles), "Lessons for this repository", provider)
	}
	require.Equal(t, other, memory.tasks[len(memory.tasks)-1].TeamID, "the lessons are asked for the task itself")
}

// Lessons that cannot be loaded never fail the attempt: it runs without them.
func TestAttemptRunsWithoutLessonsWhenMemoryFails(t *testing.T) {
	notebook := recordedNotebook()
	memory := &memorySourceFake{err: errors.New("database is down")}
	run := notebookAttempt(t, "qwen-code", &notebook, nil, nil, memory)
	require.Contains(t, run.Prompt, "Implement dark mode")
	require.NotContains(t, run.Prompt, "Lessons for this repository")
	require.Len(t, memory.tasks, 1)
}

// When a prompt is short of room the notebook's detail gives way first, then
// the lessons; the profile's and the project's instructions never do.
func TestNotebookShrinksBeforeLessonsAndInstructionsNever(t *testing.T) {
	// Plenty of room: both get their full share.
	notebookShare, memoryShare := promptShares(50000, 4000)
	require.Equal(t, notebookPromptBytes, notebookShare)
	require.Equal(t, 4000, memoryShare)
	// Short: the notebook shrinks first and the lessons stay whole.
	notebookShare, memoryShare = promptShares(9000, 4000)
	require.Equal(t, 4000, memoryShare)
	require.Less(t, notebookShare, notebookPromptBytes)
	require.GreaterOrEqual(t, notebookShare, notebookFloorBytes)
	// Shorter still: the notebook is at its floor and the lessons shrink.
	notebookShare, memoryShare = promptShares(notebookFloorBytes+1500, 4000)
	require.Equal(t, notebookFloorBytes, notebookShare)
	require.Less(t, memoryShare, 4000)
	require.Positive(t, memoryShare)
	// No room: the lessons go, the notebook keeps its floor.
	notebookShare, memoryShare = promptShares(0, 4000)
	require.Equal(t, notebookFloorBytes, notebookShare)
	require.Zero(t, memoryShare)

	// In a real attempt on a prompt-only harness with a large plan, a full
	// notebook and lessons, the instructions still arrive whole.
	team := uuid.New()
	lessons := []string{}
	for i := range 12 {
		lessons = append(lessons, fmt.Sprintf("Lesson %d: %s", i, strings.Repeat("keep the checks green ", 12)))
	}
	memory := &memorySourceFake{approved: map[uuid.UUID][]string{team: lessons}}
	profile := "PROFILE-START " + strings.Repeat("Write small commits and explain each one. ", 300) + " PROFILE-END"
	project := "PROJECT-START " + strings.Repeat("Use pnpm, never npm. ", 150) + " PROJECT-END"
	notebook := recordedNotebook()
	for i := range domain.NotebookListItems {
		notebook.AddFinding(domain.NotebookFindingReview, strings.Repeat("f", 2000)+" "+strings.Repeat("y", i), time.Now())
	}
	version := uuid.New()
	run := notebookAttempt(t, "qwen-code", &notebook, &domain.AgentProfileBundle{Name: "Builder", Behaviour: domain.AgentBehaviourChangesCode, Instructions: profile}, func(task *domain.AgentTask) {
		node := "implement"
		task.TeamID, task.WorkflowVersionID, task.WorkflowNode = team, &version, &node
		task.AgentInstructions = project + "\n\n" + workflowProfileTitle + "Builder:\n" + profile
	}, memory)
	require.Contains(t, run.Prompt, profile)
	require.Contains(t, run.Prompt, project)
	require.Contains(t, run.Prompt, domain.FormatLessons(lessons), "the lessons are whole while the notebook shrank")
	require.Contains(t, run.Prompt, "left out", "the notebook gave way")
	require.Less(t, len(run.Prompt), 100000)
}
