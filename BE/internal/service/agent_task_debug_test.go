package service

import (
	"bytes"
	"encoding/json"
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
