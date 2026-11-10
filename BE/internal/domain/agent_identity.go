package domain

import (
	"time"

	"github.com/google/uuid"
)

// AgentIdentity is an agent profile seen as a workspace member: it can be
// assigned issues and mentioned in the teams where administrators allow it.
// The ID is the profile's. An agent holds no permissions of its own: every
// task it runs acts for the person who assigned or mentioned it.
type AgentIdentity struct {
	ID          uuid.UUID  `json:"id" db:"id"`
	WorkspaceID uuid.UUID  `json:"workspace_id" db:"workspace_id"`
	Name        string     `json:"name" db:"name"`
	Role        string     `json:"role" db:"role"`
	Harness     string     `json:"harness" db:"harness"`
	Model       string     `json:"model" db:"model"`
	Effort      string     `json:"effort" db:"effort"`
	AvatarColor string     `json:"avatar_color" db:"avatar_color"`
	BuiltinKey  *string    `json:"builtin_key,omitempty" db:"builtin_key"`
	ArchivedAt  *time.Time `json:"archived_at,omitempty" db:"archived_at"`
	// TeamIDs are the teams where the agent can be assigned and mentioned.
	TeamIDs []uuid.UUID `json:"team_ids" db:"-"`
	// Available reports whether the workspace's machine policy lets the
	// agent's harness and model run now; Unavailable says why not.
	Available   bool   `json:"available" db:"-"`
	Unavailable string `json:"unavailable_reason,omitempty" db:"-"`
}

// IssueAgentAssignee is an agent assigned to an issue with its task's status.
type IssueAgentAssignee struct {
	IssueID     uuid.UUID `json:"issue_id" db:"issue_id"`
	ID          uuid.UUID `json:"id" db:"profile_id"`
	WorkspaceID uuid.UUID `json:"-" db:"workspace_id"`
	Name        string    `json:"name" db:"name"`
	Role        string    `json:"role" db:"role"`
	Harness     string    `json:"harness" db:"harness"`
	Model       string    `json:"model" db:"model"`
	AvatarColor string    `json:"avatar_color" db:"avatar_color"`
	// AssignedBy is the person the agent acts for on this issue.
	AssignedBy     *uuid.UUID `json:"assigned_by" db:"assigned_by"`
	AssignedByName *string    `json:"assigned_by_name" db:"assigned_by_name"`
	RequestID      uuid.UUID  `json:"-" db:"request_id"`
	TaskID         *uuid.UUID `json:"task_id" db:"task_id"`
	TaskState      *string    `json:"task_state" db:"task_state"`
	TaskReason     *string    `json:"task_reason" db:"task_reason"`
	// Error says why no task could be started for the assignment.
	Error     *string   `json:"error,omitempty" db:"error"`
	CreatedAt time.Time `json:"created_at" db:"created_at"`
}

// AgentTaskWorking reports whether a task in state is still doing work that
// unassigning its agent stops. A task awaiting review keeps its published
// pull request, and a finished task has nothing left to stop.
func AgentTaskWorking(state string) bool {
	switch state {
	case "queued", "preparing", "running", "waiting_input", "validating", "publishing", "blocked", "failed":
		return true
	}
	return false
}
