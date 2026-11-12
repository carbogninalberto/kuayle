package service

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/kuayle/kuayle-backend/internal/domain"
	"github.com/kuayle/kuayle-backend/internal/repository"
	"github.com/kuayle/kuayle-backend/pkg/sanitize"
)

// The debug trace is bounded twice: every text field is cut, and the whole
// document is kept under a byte budget. What is left out is listed in cuts.
const (
	traceTextBytes       = 64 << 10
	traceDocumentBytes   = 4 << 20
	traceRows            = 500
	traceTransitionRows  = 2000
	traceTruncatedMarker = "…[truncated]"
)

type traceLimits struct {
	Rows          int
	Transitions   int
	TextBytes     int
	DocumentBytes int
}

func defaultTraceLimits() traceLimits {
	return traceLimits{Rows: traceRows, Transitions: traceTransitionRows, TextBytes: traceTextBytes, DocumentBytes: traceDocumentBytes}
}

// DebugTrace returns everything Kuayle recorded about a task in one JSON
// document, redacted with the credentials of the machines its attempts ran
// on. The route is session-only and the repository applies the task's access
// rules, including private teams.
func (s *AgentTaskService) DebugTrace(ctx context.Context, ws, id uuid.UUID) (*domain.TaskTrace, error) {
	limits := defaultTraceLimits()
	rows, err := s.Repo.DebugTrace(ctx, ws, id, repository.TraceLimits{Rows: limits.Rows, Transitions: limits.Transitions})
	if err != nil {
		return nil, err
	}
	secrets := []string{}
	if s.machines != nil {
		for _, machine := range traceMachineIDs(rows) {
			values, err := s.machines.machineSecretValues(ctx, ws, machine)
			if err != nil {
				return nil, err
			}
			secrets = append(secrets, values...)
		}
	}
	return buildTaskTrace(rows, secrets, limits), nil
}

// traceMachineIDs lists the distinct machines the trace mentions: the ones
// its attempts ran on and the ones its operations targeted, which can differ
// when an operation failed before any attempt started.
func traceMachineIDs(rows *repository.TaskTraceRows) []uuid.UUID {
	ids := []uuid.UUID{}
	seen := map[uuid.UUID]bool{}
	add := func(id uuid.UUID) {
		if id == uuid.Nil || seen[id] {
			return
		}
		seen[id] = true
		ids = append(ids, id)
	}
	for _, run := range rows.Runs {
		add(run.MachineID)
	}
	for _, operation := range rows.Operations {
		add(operation.MachineID)
	}
	return ids
}

// buildTaskTrace assembles the document from raw rows. It has no I/O, so the
// ordering, redaction and size rules can be tested without a database.
func buildTaskTrace(raw *repository.TaskTraceRows, secrets []string, limits traceLimits) *domain.TaskTrace {
	if limits.Rows <= 0 || limits.Transitions <= 0 || limits.TextBytes <= 0 || limits.DocumentBytes <= 0 {
		defaults := defaultTraceLimits()
		if limits.Rows <= 0 {
			limits.Rows = defaults.Rows
		}
		if limits.Transitions <= 0 {
			limits.Transitions = defaults.Transitions
		}
		if limits.TextBytes <= 0 {
			limits.TextBytes = defaults.TextBytes
		}
		if limits.DocumentBytes <= 0 {
			limits.DocumentBytes = defaults.DocumentBytes
		}
	}
	cuts := []domain.TraceCut{}
	entries := []domain.TraceEntry{}
	if raw == nil {
		return &domain.TaskTrace{Format: domain.TaskTraceFormat, GeneratedAt: time.Now().UTC(), Limits: traceLimitMap(limits), Entries: entries, Cuts: cuts, Redacted: true}
	}

	transitions := trimTraceRows(raw.Transitions, limits.Transitions, "transition", &cuts)
	steps := trimTraceRows(raw.Steps, limits.Rows, "workflow_step", &cuts)
	runs := trimTraceRows(raw.Runs, limits.Rows, "run", &cuts)
	prompts := trimTraceRows(raw.Prompts, limits.Rows, "prompt", &cuts)
	runSteps := trimTraceRows(raw.RunSteps, limits.Rows, "run_step", &cuts)
	operations := trimTraceRows(raw.Operations, limits.Rows, "operation", &cuts)
	retries := trimTraceRows(raw.Retries, limits.Rows, "retry", &cuts)
	liveMessages := trimTraceRows(raw.LiveMessages, limits.Rows, "live_message", &cuts)
	agentMessages := trimTraceRows(raw.AgentMessages, limits.Rows, "agent_message", &cuts)
	reviews := trimTraceRows(raw.Reviews, limits.Rows, "review", &cuts)
	signals := trimTraceRows(raw.Signals, limits.Rows, "ci_signal", &cuts)
	audits := trimTraceRows(raw.Audits, limits.Rows, "pause", &cuts)
	notebookChanges := trimTraceRows(raw.NotebookChanges, limits.Rows, "notebook_change", &cuts)

	promptByRun := make(map[uuid.UUID][]domain.PromptSource, len(prompts))
	for _, prompt := range prompts {
		promptByRun[prompt.RunID] = prompt.Sources
	}

	for _, transition := range transitions {
		entry := domain.TraceEntry{Kind: "transition", ID: fmt.Sprintf("transition-%d", transition.Sequence), Start: transition.CreatedAt, Outcome: traceOutcome(transition.ToState), Reason: transition.Reason,
			Data: map[string]any{"sequence": transition.Sequence, "from_state": transition.FromState, "to_state": transition.ToState, "actor_id": transition.ActorID}}
		end := transition.CreatedAt
		entry.End = &end
		entries = append(entries, entry)
	}
	for _, step := range steps {
		outcome := ""
		if step.Outcome != nil {
			outcome = *step.Outcome
		}
		entries = append(entries, domain.TraceEntry{Kind: "workflow_step", ID: fmt.Sprintf("step-%d", step.Sequence), Start: step.EnteredAt, End: step.FinishedAt,
			Outcome: traceOutcome(outcome), Reason: step.Reason, Data: map[string]any{
				"sequence": step.Sequence, "node_id": step.NodeID, "node_type": step.NodeType, "visit": step.Visit,
				"from_node": step.FromNode, "edge_id": step.EdgeID, "condition": step.Condition, "generation": step.Generation,
				"review_task_id": step.ReviewTaskID, "detail": step.Detail, "plan": step.Plan, "decided_by": step.DecidedBy,
				"paused_seconds": step.PausedSeconds,
			}})
	}
	for _, run := range runs {
		start := traceFirstTime(run.StartedAt, run.CreatedAt)
		end := run.CompletedAt
		if end == nil {
			end = run.CancelledAt
		}
		reason := ""
		if run.FailureCause != nil {
			reason = *run.FailureCause
		} else if run.ErrorMessage != nil {
			reason = *run.ErrorMessage
		}
		summary := ""
		if run.Summary != nil {
			summary = *run.Summary
		}
		entries = append(entries, domain.TraceEntry{Kind: "run", ID: run.ID.String(), Start: start, End: end, Outcome: traceOutcome(string(run.Status)), Reason: reason,
			RunID: traceUUID(run.ID), Data: map[string]any{
				"task_id": run.TaskID, "machine_id": run.MachineID, "provider": run.ProviderID, "mode": run.Mode,
				"model": run.Model, "thinking": run.Thinking, "status": string(run.Status), "failure_class": run.FailureClass,
				"failure_rule": run.FailureRule, "environment": traceJSON(run.Environment), "output_cuts": traceJSON(run.OutputCuts),
				"branch": run.Branch, "pull_request_url": run.PullRequestURL, "summary": summary, "exit_code": run.ExitCode,
				"session_id": run.SessionID, "notebook_required": run.NotebookRequired, "notebook_updates": run.NotebookUpdates,
				"notebook_unchanged": run.NotebookUnchanged, "notebook_problem": run.NotebookProblem,
				"worktree_read_only": run.WorktreeReadOnly,
			}})
	}
	for _, run := range runs {
		sources := promptByRun[run.ID]
		recorded := len(sources) > 0
		sourceData := make([]any, 0, len(sources))
		for _, source := range sources {
			sourceData = append(sourceData, map[string]any{"source": source.Source, "channel": source.Channel, "text": source.Text, "truncated": source.Truncated})
		}
		outcome := "not_recorded"
		if recorded {
			outcome = "recorded"
		}
		entries = append(entries, domain.TraceEntry{Kind: "prompt", ID: "prompt-" + run.ID.String(), Start: traceFirstTime(run.StartedAt, run.CreatedAt),
			Outcome: outcome, RunID: traceUUID(run.ID), Data: map[string]any{"recorded": recorded, "sources": sourceData}})
		if run.ClosingAskedAt != nil {
			var end *time.Time
			if run.ClosingSeconds != nil {
				at := run.ClosingAskedAt.Add(time.Duration(*run.ClosingSeconds) * time.Second)
				end = &at
			}
			entries = append(entries, domain.TraceEntry{Kind: "closing_continuation", ID: "closing-" + run.ID.String(), Start: *run.ClosingAskedAt, End: end,
				Outcome: "asked", RunID: traceUUID(run.ID), Data: map[string]any{"closing_seconds": run.ClosingSeconds, "notebook_problem": run.NotebookProblem}})
		}
	}
	for _, step := range runSteps {
		summary := ""
		if step.Summary != nil {
			summary = *step.Summary
		}
		commandSource := ""
		if step.CommandSource != nil {
			commandSource = *step.CommandSource
		}
		agentSummary := ""
		if step.AgentSummary != nil {
			agentSummary = *step.AgentSummary
		}
		entries = append(entries, domain.TraceEntry{Kind: "run_step", ID: step.ID.String(), Start: traceFirstTime(step.StartedAt, step.CreatedAt), End: step.CompletedAt,
			Outcome: traceOutcome(string(step.Status)), Reason: summary, RunID: traceUUID(step.AgentRunID), Data: map[string]any{
				"sequence": step.Sequence, "step_type": step.StepType, "name": step.Name, "exit_code": step.ExitCode,
				"command_argv": traceJSON(step.CommandArgv), "compose_file": step.ComposeFile, "command_source": commandSource,
				"commands": traceJSON(step.Commands), "command_results": traceJSON(step.CommandResults), "agent_summary": agentSummary,
			}})
	}
	for _, operation := range operations {
		reason := ""
		if operation.ErrorMessage != nil {
			reason = *operation.ErrorMessage
		} else if operation.ErrorCode != nil {
			reason = *operation.ErrorCode
		}
		entries = append(entries, domain.TraceEntry{Kind: "operation", ID: operation.ID.String(), Start: operation.CreatedAt, End: operation.CompletedAt,
			Outcome: traceOutcome(string(operation.Status)), Reason: reason, RunID: operation.AgentRunID, Data: map[string]any{
				"action": string(operation.Action), "machine_id": operation.MachineID, "agent_task_id": operation.AgentTaskID,
				"checkout_id": operation.CheckoutID, "environment_id": operation.EnvironmentID, "terminal_session_id": operation.TerminalSessionID,
				"generation": operation.Generation, "attempts": operation.Attempts, "max_attempts": operation.MaxAttempts,
				"error_code": operation.ErrorCode, "error_message": operation.ErrorMessage, "available_at": operation.AvailableAt,
				"updated_at": operation.UpdatedAt,
			}})
	}
	for _, retry := range retries {
		entries = append(entries, domain.TraceEntry{Kind: "retry", ID: retry.RequestID.String(), Start: retry.CreatedAt, Outcome: "requested", Reason: retry.Prompt,
			Data: map[string]any{"request_id": retry.RequestID, "expected_generation": retry.ExpectedGeneration, "model": retry.Model, "model_server_id": retry.ModelServerID}})
	}
	for _, message := range liveMessages {
		entries = append(entries, domain.TraceEntry{Kind: "live_message", ID: message.ID.String(), Start: message.CreatedAt, End: message.DeliveredAt,
			Outcome: traceOutcome(message.State), Reason: message.Body, RunID: message.RunID, Data: map[string]any{
				"direction": message.Direction, "seq": message.Seq, "sender": message.Sender, "author_id": message.AuthorID,
				"kind": message.Kind, "delivered_via": message.DeliveredVia, "reject_reason": message.RejectReason,
				"reply_to": message.ReplyTo, "deliveries": message.Deliveries, "workflow_node": message.WorkflowNode,
			}})
	}
	for _, message := range agentMessages {
		entries = append(entries, domain.TraceEntry{Kind: "agent_message", ID: message.ID.String(), Start: message.CreatedAt, End: message.DeliveredAt,
			Outcome: traceOutcome(message.State), Reason: message.Body, RunID: message.FromRunID, Data: map[string]any{
				"from_task_id": message.FromTaskID, "from_issue_id": message.FromIssueID, "to_ref": message.ToRef,
				"to_issue_id": message.ToIssueID, "to_task_id": message.ToTaskID, "channel": message.Channel,
				"reject_reason": message.RejectReason, "comment_state": message.CommentState,
			}})
	}
	for _, review := range reviews {
		outcome := "requested"
		if review.Verdict != nil {
			outcome = *review.Verdict
		} else if review.CompletedAt != nil {
			outcome = "completed"
		}
		findings := ""
		if review.Findings != nil {
			findings = *review.Findings
		}
		entries = append(entries, domain.TraceEntry{Kind: "review", ID: review.TaskID.String(), Start: review.CreatedAt, End: review.CompletedAt, Outcome: traceOutcome(outcome), Reason: findings,
			Data: map[string]any{
				"task_id": review.TaskID, "reviewed_task_id": review.ReviewedTaskID, "requested_by": review.RequestedBy,
				"provider": review.Provider, "commit_sha": review.CommitSHA, "repository": review.Repository,
				"pull_request_number": review.PullRequestNumber, "pull_request_url": review.PullRequestURL,
				"base_branch": review.BaseBranch, "focus": review.Focus, "attempt": review.Attempt,
				"post_to_pull_request": review.PostToPullRequest, "comment_state": review.CommentState,
			}})
	}
	for _, signal := range signals {
		entries = append(entries, domain.TraceEntry{Kind: "ci_signal", ID: signal.ID.String(), Start: signal.CreatedAt, End: &signal.UpdatedAt,
			Outcome: traceOutcome(signal.Outcome), Reason: signal.Summary, RunID: traceUUID(signal.RunID), Data: map[string]any{
				"kind": signal.Kind, "external_id": signal.ExternalID, "name": signal.Name, "url": signal.URL,
				"action": signal.Action, "failure_class": signal.FailureClass, "failure_evidence": signal.FailureEvidence,
				"follow_up_generation": signal.FollowUpGeneration,
			}})
	}
	for _, audit := range audits {
		kind := "pause"
		if audit.Action == domain.WorkflowSwitchAction {
			kind = "version_switch"
		}
		detail := traceJSON(audit.Detail)
		entries = append(entries, domain.TraceEntry{Kind: kind, ID: fmt.Sprintf("audit-%d", audit.ID), Start: audit.CreatedAt, Outcome: traceOutcome(audit.Action),
			Data: map[string]any{"action": audit.Action, "actor_id": audit.ActorID, "detail": detail}})
	}
	for _, change := range notebookChanges {
		entries = append(entries, domain.TraceEntry{Kind: "notebook_change", ID: fmt.Sprintf("notebook-change-%d", change.Revision), Start: change.CreatedAt,
			Outcome: traceOutcome(change.Author), Reason: change.Summary, Data: map[string]any{
				"revision": change.Revision, "author": change.Author, "user_id": change.UserID, "run_id": change.RunID,
				"generation": change.Generation, "step_sequence": change.StepSequence, "step_label": change.StepLabel,
				"sections": change.Sections,
			}})
	}
	if raw.Notebook != nil {
		outcome := "current"
		if raw.NotebookRevision > 0 {
			outcome = fmt.Sprintf("revision_%d", raw.NotebookRevision)
		}
		entries = append(entries, domain.TraceEntry{Kind: "notebook", ID: "notebook", Start: traceFirstTime(&raw.Task.UpdatedAt, raw.Task.CreatedAt), Outcome: outcome,
			Data: map[string]any{"revision": raw.NotebookRevision, "notebook": traceJSONValue(*raw.Notebook)}})
	}

	task := domain.TaskTraceTask{ID: raw.Task.ID, Identifier: raw.Identifier, Title: raw.Title, Prompt: raw.Task.Prompt, State: raw.Task.State, Reason: raw.Task.Reason,
		FailureClass: raw.Task.FailureClass, Kind: raw.Task.Kind, Provider: raw.Task.Provider, Model: raw.Task.Model, Generation: raw.Task.Generation,
		IssueID: raw.Task.IssueID, TeamID: raw.Task.TeamID, CreatorID: raw.Task.CreatorID, Conversation: raw.Task.ConversationID,
		WorkflowID: raw.Task.WorkflowVersionID, WorkflowNode: raw.Task.WorkflowNode, WorkflowState: raw.Task.WorkflowStatus,
		CreatedAt: raw.Task.CreatedAt, UpdatedAt: raw.Task.UpdatedAt}
	if task.Identifier == "" {
		task.Identifier = task.ID.String()
	}
	if task.Kind == "" {
		task.Kind = domain.AgentTaskKindImplement
	}
	trace := &domain.TaskTrace{Format: domain.TaskTraceFormat, GeneratedAt: time.Now().UTC(), Task: task, Limits: traceLimitMap(limits), Entries: entries, Cuts: cuts, Redacted: true}
	trace.Task.Title = traceRedact(trace.Task.Title, secrets)
	trace.Task.Prompt = traceRedact(trace.Task.Prompt, secrets)
	trace.Task.Reason = traceRedact(trace.Task.Reason, secrets)
	if trace.Task.FailureClass != nil {
		class := traceRedact(*trace.Task.FailureClass, secrets)
		trace.Task.FailureClass = &class
	}

	for i := range trace.Entries {
		entry := &trace.Entries[i]
		entry.Reason = traceRedact(entry.Reason, secrets)
		entry.Data = redactTraceValue(entry.Data, secrets)
		if entry.Start.IsZero() {
			entry.Start = trace.Task.CreatedAt
		}
		entry.Outcome = traceOutcome(entry.Outcome)
		if entry.End != nil && entry.End.Before(entry.Start) {
			end := entry.Start
			entry.End = &end
		}
	}
	trimTraceEntries(trace.Entries, limits.TextBytes, &cuts)
	trace.Task.Title = trimTraceText(trace.Task.Title, limits.TextBytes, "task", "title", &cuts)
	trace.Task.Prompt = trimTraceText(trace.Task.Prompt, limits.TextBytes, "task", "prompt", &cuts)
	trace.Task.Reason = trimTraceText(trace.Task.Reason, limits.TextBytes, "task", "reason", &cuts)
	if trace.Task.FailureClass != nil {
		class := trimTraceText(*trace.Task.FailureClass, limits.TextBytes, "task", "failure_class", &cuts)
		trace.Task.FailureClass = &class
	}
	sort.SliceStable(trace.Entries, func(i, j int) bool {
		left, right := trace.Entries[i], trace.Entries[j]
		if !left.Start.Equal(right.Start) {
			return left.Start.Before(right.Start)
		}
		if traceKindRank(left.Kind) != traceKindRank(right.Kind) {
			return traceKindRank(left.Kind) < traceKindRank(right.Kind)
		}
		return left.ID < right.ID
	})
	trace.Cuts = cuts
	fitTraceDocument(trace, limits.DocumentBytes)
	return trace
}

func traceLimitMap(limits traceLimits) map[string]int {
	return map[string]int{
		"text_bytes":      limits.TextBytes,
		"document_bytes":  limits.DocumentBytes,
		"rows_per_source": limits.Rows,
		"transition_rows": limits.Transitions,
	}
}

func traceKindRank(kind string) int {
	for i, candidate := range domain.TaskTraceEntryKinds {
		if candidate == kind {
			return i
		}
	}
	return len(domain.TaskTraceEntryKinds)
}

func traceOutcome(outcome string) string {
	if strings.TrimSpace(outcome) == "" {
		return "unknown"
	}
	return outcome
}

func traceFirstTime(first *time.Time, fallback time.Time) time.Time {
	if first != nil && !first.IsZero() {
		return *first
	}
	return fallback
}

func traceUUID(id uuid.UUID) *uuid.UUID {
	if id == uuid.Nil {
		return nil
	}
	return &id
}

func traceRedact(value string, secrets []string) string {
	if value == "" {
		return ""
	}
	return sanitize.RedactCredentials(redactText(value, secrets))
}

func traceJSON(raw []byte) any {
	if len(raw) == 0 {
		return nil
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil
	}
	return value
}

func traceJSONValue(value any) any {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	return traceJSON(encoded)
}

func trimTraceRows[T any](rows []T, limit int, kind string, cuts *[]domain.TraceCut) []T {
	if limit <= 0 || len(rows) <= limit {
		return rows
	}
	*cuts = append(*cuts, domain.TraceCut{Kind: kind, What: "oldest_rows", Kept: limit, Total: len(rows)})
	return rows[len(rows)-limit:]
}

func redactTraceValue(value any, secrets []string) any {
	switch typed := value.(type) {
	case string:
		return traceRedact(typed, secrets)
	case *string:
		if typed == nil {
			return nil
		}
		return traceRedact(*typed, secrets)
	case []string:
		out := make([]any, len(typed))
		for i, item := range typed {
			out[i] = traceRedact(item, secrets)
		}
		return out
	case []any:
		out := make([]any, len(typed))
		for i, item := range typed {
			out[i] = redactTraceValue(item, secrets)
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, item := range typed {
			out[traceRedact(key, secrets)] = redactTraceValue(item, secrets)
		}
		return out
	default:
		return value
	}
}

func trimTraceEntries(entries []domain.TraceEntry, limit int, cuts *[]domain.TraceCut) {
	for i := range entries {
		entries[i].Reason = trimTraceText(entries[i].Reason, limit, entries[i].Kind, "reason", cuts)
		entries[i].Data = trimTraceValue(entries[i].Data, limit, entries[i].Kind, "data", cuts)
	}
}

func trimTraceText(value string, limit int, kind, what string, cuts *[]domain.TraceCut) string {
	if limit <= 0 || len(value) <= limit {
		return value
	}
	room := limit - len(traceTruncatedMarker)
	if room < 0 {
		room = 0
	}
	cut := truncateUTF8Bytes(value, room) + traceTruncatedMarker
	*cuts = append(*cuts, domain.TraceCut{Kind: kind, What: what, Kept: len(cut), Total: len(value), Bytes: int64(len(value) - len(cut))})
	return cut
}

func trimTraceValue(value any, limit int, kind, path string, cuts *[]domain.TraceCut) any {
	switch typed := value.(type) {
	case string:
		return trimTraceText(typed, limit, kind, path, cuts)
	case *string:
		if typed == nil {
			return nil
		}
		return trimTraceText(*typed, limit, kind, path, cuts)
	case []string:
		out := make([]any, len(typed))
		for i, item := range typed {
			out[i] = trimTraceText(item, limit, kind, fmt.Sprintf("%s[%d]", path, i), cuts)
		}
		return out
	case []any:
		out := make([]any, len(typed))
		for i, item := range typed {
			out[i] = trimTraceValue(item, limit, kind, fmt.Sprintf("%s[%d]", path, i), cuts)
		}
		return out
	case map[string]any:
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		out := make(map[string]any, len(typed))
		for _, key := range keys {
			out[key] = trimTraceValue(typed[key], limit, kind, path+"."+key, cuts)
		}
		return out
	default:
		return value
	}
}

// fitTraceDocument keeps the marshalled document under its byte budget: it
// drops the largest prompt-source texts first, then the oldest entries, and
// records every drop in cuts. Dropping a text is estimated from its encoded
// length, so a document with many texts costs one serialisation per batch of
// drops instead of one per text.
func fitTraceDocument(trace *domain.TaskTrace, limit int) {
	if limit <= 0 {
		return
	}
	encoded, err := json.Marshal(trace)
	if err != nil || len(encoded) <= limit {
		return
	}
	for len(encoded) > limit {
		size := len(encoded)
		dropped := false
		for size > limit {
			entry, sourceIndex := largestPromptText(trace.Entries)
			if entry < 0 {
				break
			}
			sources := trace.Entries[entry].Data.(map[string]any)["sources"].([]any)
			source := sources[sourceIndex].(map[string]any)
			text, _ := source["text"].(string)
			encodedText, err := json.Marshal(text)
			if err != nil {
				return
			}
			source["text"] = ""
			source["truncated"] = true
			cut := domain.TraceCut{Kind: "prompt", What: "source_text", Kept: 0, Total: len(text), Bytes: int64(len(text))}
			trace.Cuts = append(trace.Cuts, cut)
			encodedCut, err := json.Marshal(cut)
			if err != nil {
				return
			}
			size -= len(encodedText) - 2
			size += len(encodedCut) + 1
			dropped = true
		}
		if encoded, err = json.Marshal(trace); err != nil {
			return
		}
		if !dropped {
			break
		}
	}
	for len(encoded) > limit && len(trace.Entries) > 0 {
		over := len(encoded) - limit
		total := len(trace.Entries)
		drop, removed := 0, 0
		for drop < len(trace.Entries) && removed < over {
			size, err := json.Marshal(trace.Entries[drop])
			if err != nil {
				return
			}
			removed += len(size) + 1
			drop++
		}
		if drop == 0 {
			drop = 1
		}
		trace.Entries = trace.Entries[drop:]
		trace.Cuts = append(trace.Cuts, domain.TraceCut{Kind: "entries", What: "oldest", Kept: len(trace.Entries), Total: total, Bytes: int64(removed)})
		if encoded, err = json.Marshal(trace); err != nil {
			return
		}
	}
}

func largestPromptText(entries []domain.TraceEntry) (int, int) {
	bestEntry, bestSource, bestLength := -1, -1, 0
	for entryIndex := range entries {
		if entries[entryIndex].Kind != "prompt" {
			continue
		}
		data, ok := entries[entryIndex].Data.(map[string]any)
		if !ok {
			continue
		}
		sources, ok := data["sources"].([]any)
		if !ok {
			continue
		}
		for sourceIndex, item := range sources {
			source, ok := item.(map[string]any)
			if !ok {
				continue
			}
			text, _ := source["text"].(string)
			if len(text) > bestLength {
				bestEntry, bestSource, bestLength = entryIndex, sourceIndex, len(text)
			}
		}
	}
	return bestEntry, bestSource
}
