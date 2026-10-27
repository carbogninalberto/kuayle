package domain

import "github.com/google/uuid"

// IssueBlocker is an issue that must finish before BlockedID can be worked on.
type IssueBlocker struct {
	BlockedID  uuid.UUID `json:"-" db:"blocked_id"`
	BlockerID  uuid.UUID `json:"-" db:"blocker_id"`
	Identifier string    `json:"identifier" db:"identifier"`
	Title      string    `json:"title" db:"title"`
	TeamID     uuid.UUID `json:"-" db:"team_id"`
	Done       bool      `json:"done" db:"done"`
}

// RelatedWork is coordination context for an agent: neighbouring issues and
// the newest agent work on each.
type RelatedWork struct {
	Identifier     string  `db:"identifier"`
	Title          string  `db:"title"`
	Relation       string  `db:"relation"`
	Status         string  `db:"status"`
	TaskState      *string `db:"task_state"`
	Branch         *string `db:"branch"`
	PullRequestURL *string `db:"pull_request_url"`
}
