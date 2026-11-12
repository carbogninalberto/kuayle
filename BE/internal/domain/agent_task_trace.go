package domain

import (
	"time"

	"github.com/google/uuid"
)

// TaskTraceFormat identifies a task's debug trace document. The document is
// assembled on demand from the tables that already hold the task's history;
// it is meant to be handed to a person or an LLM, not rendered by the app.
const TaskTraceFormat = "kuayle.task-trace/v1"

// TraceEntry is one thing that happened to a task, in time order. Every
// entry names when it started, when it ended (nil while it is still open),
// what came of it and why. Data is specific to Kind: see TaskTrace.
type TraceEntry struct {
	Kind    string     `json:"kind"`
	ID      string     `json:"id"`
	Start   time.Time  `json:"start"`
	End     *time.Time `json:"end"`
	Outcome string     `json:"outcome"`
	Reason  string     `json:"reason"`
	RunID   *uuid.UUID `json:"run_id,omitempty"`
	Data    any        `json:"data,omitempty"`
}

// TaskTraceEntryKinds are the kinds of entry a trace contains, in the order
// entries that started at the same instant are listed.
var TaskTraceEntryKinds = []string{
	"transition",
	"workflow_step",
	"run",
	"prompt",
	"closing_continuation",
	"run_step",
	"operation",
	"retry",
	"live_message",
	"agent_message",
	"review",
	"ci_signal",
	"pause",
	"version_switch",
	"notebook_change",
	"notebook",
}

// TraceCut says what the document left out to stay within its limits: which
// kind of entry, which field or rows, how many it kept, how many there were
// and how many bytes were removed.
type TraceCut struct {
	Kind  string `json:"kind"`
	What  string `json:"what"`
	Kept  int    `json:"kept"`
	Total int    `json:"total"`
	Bytes int64  `json:"bytes"`
}

// TaskTraceTask is the task header: what the task is and where it ended up.
type TaskTraceTask struct {
	ID            uuid.UUID  `json:"id"`
	Identifier    string     `json:"identifier"`
	Title         string     `json:"title"`
	Prompt        string     `json:"prompt"`
	State         string     `json:"state"`
	Reason        string     `json:"reason"`
	FailureClass  *string    `json:"failure_class,omitempty"`
	Kind          string     `json:"kind"`
	Provider      string     `json:"provider"`
	Model         string     `json:"model"`
	Generation    int64      `json:"generation"`
	IssueID       uuid.UUID  `json:"issue_id"`
	TeamID        uuid.UUID  `json:"team_id"`
	CreatorID     uuid.UUID  `json:"creator_id"`
	Conversation  *uuid.UUID `json:"conversation_id,omitempty"`
	WorkflowID    *uuid.UUID `json:"workflow_id,omitempty"`
	WorkflowNode  *string    `json:"workflow_node,omitempty"`
	WorkflowState *string    `json:"workflow_status,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

// TaskTrace is one task's complete debug trace. Entries are in time order;
// Cuts is always an array (empty when nothing was left out).
type TaskTrace struct {
	Format      string         `json:"format"`
	GeneratedAt time.Time      `json:"generated_at"`
	Task        TaskTraceTask  `json:"task"`
	Limits      map[string]int `json:"limits"`
	Entries     []TraceEntry   `json:"entries"`
	Cuts        []TraceCut     `json:"cuts"`
	Redacted    bool           `json:"redacted"`
}
