package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/stretchr/testify/require"
)

func traceFixture() *repository.TaskTraceRows {
	now := time.Date(2026, 11, 12, 10, 0, 0, 0, time.UTC)
	taskID, ws, issueID, teamID, creator := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	runID, stepID, operationID := uuid.New(), uuid.New(), uuid.New()
	messageID, reviewID, signalID, retryID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	agentMessageID := uuid.New()
	later := now.Add(5 * time.Minute)
	done := "succeeded"
	cause := "The provider rejected the request for s3cr3t-value"
	className := string(domain.FailureProvider)
	summary := "the run ended with s3cr3t-value"
	agentSummary := "I tested s3cr3t-value"
	stepDetail := "the step used s3cr3t-value"
	plan := "plan with ghp_012345678901234567890123456789012345"
	findings := "review found s3cr3t-value"
	signalSummary := "CI says ghp_012345678901234567890123456789012345"
	operationError := "operation failed for s3cr3t-value"
	note := "an instruction with s3cr3t-value"
	closingSeconds := 90
	return &repository.TaskTraceRows{
		Task: domain.AgentTask{ID: taskID, WorkspaceID: ws, CreatorID: creator, IssueID: issueID, TeamID: teamID, State: "failed",
			Reason: "Task failed: s3cr3t-value", Provider: "codex", Model: "gpt", Generation: 2, Kind: domain.AgentTaskKindImplement,
			FailureClass: &className, CreatedAt: now, UpdatedAt: later},
		Identifier:  "KUA-1",
		Title:       "A task",
		Transitions: []domain.AgentTaskTransition{{Sequence: 1, TaskID: taskID, FromState: "", ToState: "queued", Reason: "Delegated with s3cr3t-value", CreatedAt: now}},
		Steps: []domain.WorkflowStep{{TaskID: taskID, Sequence: 1, NodeID: "implement", NodeType: "agent", Visit: 1, Reason: "Start", Detail: stepDetail, Plan: &plan,
			Outcome: &done, EnteredAt: now, FinishedAt: &later}},
		Runs: []domain.DevMachineAgentRun{{ID: runID, TaskID: &taskID, MachineID: uuid.New(), WorkspaceID: ws, ProviderID: "codex", Status: domain.DevMachineAgentRunStatusFailed,
			FailureClass: &className, FailureCause: &cause, Summary: &summary, ClosingAskedAt: &later, ClosingSeconds: &closingSeconds,
			CreatedAt: now, StartedAt: &now, CompletedAt: &later}},
		Prompts: []repository.TraceRunPrompt{{RunID: runID, Sources: []domain.PromptSource{
			{Source: "task", Channel: "prompt", Text: "Do it with s3cr3t-value"},
			{Source: "contract", Channel: "prompt", Text: "Use ghp_012345678901234567890123456789012345 sparingly"},
		}}},
		RunSteps: []domain.DevMachineAgentRunStep{{ID: stepID, AgentRunID: runID, Sequence: 0, StepType: "validation", Name: "check", Status: domain.DevMachineAgentRunStepStatusFailed,
			Summary: &operationError, AgentSummary: &agentSummary, CreatedAt: now, StartedAt: &now, CompletedAt: &later}},
		Operations: []domain.DevMachineOperation{{ID: operationID, MachineID: uuid.New(), AgentRunID: &runID, AgentTaskID: &taskID, WorkspaceID: ws,
			Action: "run_agent", Status: domain.DevMachineOpStatusFailed, ErrorMessage: &operationError, CreatedAt: now, UpdatedAt: later, CompletedAt: &later}},
		Retries: []repository.TraceRetry{{TaskID: taskID, RequestID: retryID, ExpectedGeneration: 1, Prompt: "retry with s3cr3t-value", CreatedAt: now}},
		LiveMessages: []domain.AgentLiveMessage{{ID: messageID, TaskID: taskID, Direction: "to_agent", Sender: "user", Kind: "message",
			Body: "Please use s3cr3t-value", State: "delivered", CreatedAt: now, DeliveredAt: &later, Deliveries: 1}},
		AgentMessages: []domain.AgentMessage{{ID: agentMessageID, WorkspaceID: ws, FromTaskID: taskID, FromIssueID: issueID, ToRef: "KUA-2",
			Channel: "internal", Body: "A reply with s3cr3t-value", State: "delivered", CreatedAt: now, DeliveredAt: &later}},
		Reviews: []domain.AgentReview{{TaskID: reviewID, WorkspaceID: ws, ReviewedTaskID: taskID, Provider: "claude-code", Findings: &findings,
			Verdict: &done, CreatedAt: now, CompletedAt: &later}},
		Signals: []domain.AgentTaskSignal{{ID: signalID, TaskID: taskID, RunID: runID, Kind: "ci", ExternalID: "check-1", Outcome: "failure",
			Summary: signalSummary, Action: "recorded", CreatedAt: now, UpdatedAt: later}},
		Audits: []repository.TraceAuditEvent{
			{ID: 1, Action: domain.WorkflowPauseActionRequested, Detail: json.RawMessage(`{"sequence":1,"mode":"now","reason":"s3cr3t-value"}`), CreatedAt: now},
			{ID: 2, Action: domain.WorkflowSwitchAction, Detail: json.RawMessage(`{"from_version":1,"to_version":2}`), CreatedAt: later},
		},
		NotebookChanges:  []domain.NotebookChange{{Revision: 1, Author: "agent", Summary: "changed s3cr3t-value", Sections: []string{"done"}, CreatedAt: now}},
		Notebook:         &domain.Notebook{Done: []string{"did it with s3cr3t-value"}, Instructions: []domain.NotebookInstruction{{Source: "request", Text: note, At: now}}},
		NotebookRevision: 1,
	}
}

func TestBuildTaskTraceRedactsSecretsAndSortsEntries(t *testing.T) {
	raw := traceFixture()
	trace := buildTaskTrace(raw, []string{"s3cr3t-value"}, defaultTraceLimits())
	encoded, err := json.Marshal(trace)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "s3cr3t-value")
	require.NotContains(t, string(encoded), "ghp_012345678901234567890123456789012345")
	require.Contains(t, string(encoded), "[REDACTED]")
	require.NotNil(t, trace.Cuts)
	require.Empty(t, trace.Cuts)
	require.Contains(t, string(encoded), `"cuts":[]`)
	require.True(t, trace.Redacted)
	require.Equal(t, domain.TaskTraceFormat, trace.Format)

	previous := time.Time{}
	for _, entry := range trace.Entries {
		require.False(t, entry.Start.IsZero(), "entry %s has no start", entry.ID)
		require.NotEmpty(t, entry.Outcome, "entry %s has no outcome", entry.ID)
		require.False(t, entry.Start.Before(previous), "entries are in time order")
		previous = entry.Start
	}
	// Every source made it into the document.
	kinds := map[string]bool{}
	for _, entry := range trace.Entries {
		kinds[entry.Kind] = true
	}
	require.True(t, kinds["transition"])
	require.True(t, kinds["workflow_step"])
	require.True(t, kinds["run"])
	require.True(t, kinds["prompt"])
	require.True(t, kinds["run_step"])
	require.True(t, kinds["operation"])
	require.True(t, kinds["retry"])
	require.True(t, kinds["live_message"])
	require.True(t, kinds["agent_message"])
	require.True(t, kinds["review"])
	require.True(t, kinds["ci_signal"])
	require.True(t, kinds["pause"])
	require.True(t, kinds["version_switch"])
	require.True(t, kinds["closing_continuation"])
	require.True(t, kinds["notebook_change"])
	require.True(t, kinds["notebook"])
}

func TestBuildTaskTraceCutsLargeTextAndBoundsTheDocument(t *testing.T) {
	raw := traceFixture()
	huge := strings.Repeat("a", 100<<10)
	raw.Prompts[0].Sources = []domain.PromptSource{{Source: "task", Channel: "prompt", Text: huge}}
	raw.Task.Reason = strings.Repeat("r", 100<<10)
	raw.Title = strings.Repeat("t", 100<<10)
	limits := traceLimits{Rows: 10, Transitions: 10, TextBytes: 1024, DocumentBytes: 8192}
	trace := buildTaskTrace(raw, nil, limits)
	encoded, err := json.Marshal(trace)
	require.NoError(t, err)
	require.LessOrEqual(t, len(encoded), limits.DocumentBytes)
	require.NotEmpty(t, trace.Cuts)
	require.Equal(t, limits.TextBytes, trace.Limits["text_bytes"])
	require.Equal(t, limits.DocumentBytes, trace.Limits["document_bytes"])
	trimmedPrompt, trimmedTask := false, false
	for _, cut := range trace.Cuts {
		if cut.Kind == "prompt" && cut.Total > cut.Kept {
			trimmedPrompt = true
		}
		if cut.Kind == "task" {
			trimmedTask = true
		}
	}
	require.True(t, trimmedPrompt, "the 100 KB prompt text is recorded as cut")
	require.True(t, trimmedTask, "the 100 KB task title and reason are recorded as cut")
	require.False(t, bytes.Contains(encoded, []byte(huge)))
}

func traceCut(cuts []domain.TraceCut, kind, what string) *domain.TraceCut {
	for i := range cuts {
		if cuts[i].Kind == kind && cuts[i].What == what {
			return &cuts[i]
		}
	}
	return nil
}

// Sources arrive oldest first with one row more than the limit; the service
// keeps the newest rows, because the final attempt and the failure are what a
// reader needs, and records that older rows were left out.
func TestBuildTaskTraceKeepsTheNewestRowsAndSaysWhatItCut(t *testing.T) {
	raw := traceFixture()
	base := time.Date(2026, 11, 12, 10, 0, 0, 0, time.UTC)
	raw.LiveMessages = []domain.AgentLiveMessage{}
	for i := range 4 {
		seq := int64(i + 1)
		raw.LiveMessages = append(raw.LiveMessages, domain.AgentLiveMessage{ID: uuid.New(), TaskID: raw.Task.ID, Direction: "to_agent",
			Seq: &seq, Sender: "user", Kind: "message", Body: fmt.Sprintf("message-%d", i+1), State: "delivered",
			CreatedAt: base.Add(time.Duration(i) * time.Minute)})
	}
	limits := defaultTraceLimits()
	limits.Rows = 3
	trace := buildTaskTrace(raw, nil, limits)

	bodies := []string{}
	for _, entry := range trace.Entries {
		if entry.Kind == "live_message" {
			bodies = append(bodies, entry.Reason)
		}
	}
	require.Equal(t, []string{"message-2", "message-3", "message-4"}, bodies, "the newest rows remain, oldest first")
	cut := traceCut(trace.Cuts, "live_message", "oldest_rows")
	require.NotNil(t, cut)
	require.Equal(t, 3, cut.Kept)
	require.Equal(t, 4, cut.Total)
}

// The header carries the task's original prompt so the trace reads on its own
// when no run recorded a prompt; it is redacted and bounded like every text.
func TestBuildTaskTraceRedactsAndBoundsTheTaskPrompt(t *testing.T) {
	raw := traceFixture()
	raw.Task.Prompt = "Implement s3cr3t-value with ghp_012345678901234567890123456789012345"
	trace := buildTaskTrace(raw, []string{"s3cr3t-value"}, defaultTraceLimits())
	require.NotEmpty(t, trace.Task.Prompt)
	require.Contains(t, trace.Task.Prompt, "[REDACTED]")
	encoded, err := json.Marshal(trace)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "s3cr3t-value")
	require.NotContains(t, string(encoded), "ghp_012345678901234567890123456789012345")

	raw = traceFixture()
	raw.Task.Prompt = strings.Repeat("p", 4<<10)
	limits := defaultTraceLimits()
	limits.TextBytes = 1024
	trace = buildTaskTrace(raw, nil, limits)
	require.LessOrEqual(t, len(trace.Task.Prompt), limits.TextBytes)
	cut := traceCut(trace.Cuts, "task", "prompt")
	require.NotNil(t, cut)
	require.Equal(t, 4<<10, cut.Total)
	require.Greater(t, cut.Total, cut.Kept)
}

// Fitting the document drops the largest prompt texts first and stops as soon
// as it is under budget: it must not empty every prompt it can find.
func TestFitTraceDocumentDropsOnlyThePromptTextsItMust(t *testing.T) {
	raw := traceFixture()
	raw.Prompts = []repository.TraceRunPrompt{}
	text := strings.Repeat("x", 100<<10)
	base := time.Date(2026, 11, 12, 10, 0, 0, 0, time.UTC)
	for i := range 10 {
		runID := uuid.New()
		at := base.Add(time.Duration(i) * time.Minute)
		raw.Runs = append(raw.Runs, domain.DevMachineAgentRun{ID: runID, TaskID: &raw.Task.ID, MachineID: uuid.New(), WorkspaceID: raw.Task.WorkspaceID,
			ProviderID: "codex", Status: domain.DevMachineAgentRunStatusSucceeded, CreatedAt: at, StartedAt: &at})
		raw.Prompts = append(raw.Prompts, repository.TraceRunPrompt{RunID: runID, Sources: []domain.PromptSource{{Source: "task", Channel: "prompt", Text: text}}})
	}
	limits := traceLimits{Rows: 100, Transitions: 100, TextBytes: 200 << 10, DocumentBytes: 600 << 10}
	trace := buildTaskTrace(raw, nil, limits)
	encoded, err := json.Marshal(trace)
	require.NoError(t, err)
	require.LessOrEqual(t, len(encoded), limits.DocumentBytes)

	nonEmpty := 0
	for _, entry := range trace.Entries {
		if entry.Kind != "prompt" {
			continue
		}
		for _, item := range entry.Data.(map[string]any)["sources"].([]any) {
			if source, ok := item.(map[string]any); ok {
				if body, _ := source["text"].(string); body != "" {
					nonEmpty++
				}
			}
		}
	}
	require.Positive(t, nonEmpty, "at least one prompt text survives the budget")

	cuts := 0
	for _, cut := range trace.Cuts {
		if cut.Kind == "prompt" && cut.What == "source_text" {
			cuts++
		}
	}
	require.Positive(t, cuts)
	require.Less(t, cuts, 10, "only the texts needed to reach the budget are dropped")
}

// Machine secrets come from every machine the trace mentions: an operation
// can target a machine that never recorded a run (a prepare step that failed
// before the attempt started).
func TestTraceMachineIDsCoversRunsAndOperations(t *testing.T) {
	runMachine, operationOnly := uuid.New(), uuid.New()
	rows := &repository.TaskTraceRows{
		Runs:       []domain.DevMachineAgentRun{{MachineID: runMachine}, {MachineID: runMachine}, {MachineID: uuid.Nil}},
		Operations: []domain.DevMachineOperation{{MachineID: operationOnly}, {MachineID: runMachine}, {MachineID: uuid.Nil}},
	}
	require.ElementsMatch(t, []uuid.UUID{runMachine, operationOnly}, traceMachineIDs(rows))
}
