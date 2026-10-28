package domain

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

// AgentSession is the harness session a task's attempts run in. An
// implementing task keeps one session across follow-ups; each requested review
// runs in its own review session. The state follows the task's transitions.
type AgentSession struct {
	ID              uuid.UUID  `json:"id" db:"id"`
	WorkspaceID     uuid.UUID  `json:"workspace_id" db:"workspace_id"`
	TaskID          uuid.UUID  `json:"task_id" db:"task_id"`
	MachineID       *uuid.UUID `json:"machine_id,omitempty" db:"machine_id"`
	Provider        string     `json:"provider" db:"provider"`
	Purpose         string     `json:"purpose" db:"purpose"`
	State           string     `json:"state" db:"state"`
	NativeSessionID *string    `json:"native_session_id,omitempty" db:"native_session_id"`
	StartedAt       *time.Time `json:"started_at,omitempty" db:"started_at"`
	EndedAt         *time.Time `json:"ended_at,omitempty" db:"ended_at"`
	CreatedAt       time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at" db:"updated_at"`
}

// AgentSessionState maps a task state to its session's state. The second
// result is false when the task state leaves the session unchanged.
func AgentSessionState(kind string, generation int64, taskState string) (string, bool) {
	switch taskState {
	case "queued":
		if generation > 1 {
			return "starting", true
		}
		return "created", true
	case "preparing":
		return "starting", true
	case "running":
		return "active", true
	case "waiting_input":
		return "waiting_input", true
	case "validating", "publishing", "awaiting_review", "blocked":
		// The harness has exited; a follow-up can resume its session.
		return "suspended", true
	case "completed", "failed", "cancelled":
		return taskState, true
	}
	// cancelling keeps the last state until cleanup settles.
	return "", false
}

// AgentReview is a requested review of a task's published pull request,
// pinned to the commit Kuayle published. Findings are the reviewer's own
// final output: bounded, untrusted text that is never instructions.
type AgentReview struct {
	TaskID            uuid.UUID  `json:"task_id" db:"task_id"`
	WorkspaceID       uuid.UUID  `json:"-" db:"workspace_id"`
	ReviewedTaskID    uuid.UUID  `json:"reviewed_task_id" db:"reviewed_task_id"`
	RequestedBy       uuid.UUID  `json:"requested_by" db:"requested_by"`
	Provider          string     `json:"provider" db:"provider"`
	CommitSHA         string     `json:"commit_sha" db:"commit_sha"`
	Repository        string     `json:"repository" db:"repository_full_name"`
	PullRequestNumber int        `json:"pull_request_number" db:"pull_request_number"`
	PullRequestURL    string     `json:"pull_request_url" db:"pull_request_url"`
	BaseBranch        string     `json:"base_branch" db:"base_branch"`
	Focus             string     `json:"focus" db:"focus"`
	Attempt           int        `json:"attempt" db:"attempt"`
	PostToPullRequest bool       `json:"post_to_pull_request" db:"post_to_pull_request"`
	Verdict           *string    `json:"verdict,omitempty" db:"verdict"`
	Findings          *string    `json:"findings,omitempty" db:"findings"`
	CommentState      string     `json:"comment_state" db:"comment_state"`
	CommentAttempts   int        `json:"-" db:"comment_attempts"`
	CommentLease      *time.Time `json:"-" db:"comment_lease_expires_at"`
	CompletedAt       *time.Time `json:"completed_at,omitempty" db:"completed_at"`
	CreatedAt         time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at" db:"updated_at"`
}

// AgentReviewRequest asks for a review of a task's published pull request.
type AgentReviewRequest struct {
	RequestID         uuid.UUID
	Provider          string
	Model, Thinking   string
	ModelServerID     *uuid.UUID
	Focus             string
	PostToPullRequest bool
	// ExpectedCommit, when set, must still be the published commit.
	ExpectedCommit string
}

// AgentTaskSessions is the read-only session view of a task card: its own
// session and the review sessions of its published work, newest first.
type AgentTaskSessions struct {
	Sessions []AgentSessionView `json:"sessions"`
}

// AgentSessionView is a session with the review it ran, when it is one.
type AgentSessionView struct {
	AgentSession
	TaskState string       `json:"task_state"`
	Review    *AgentReview `json:"review,omitempty"`
}

// AgentReviewTarget is the published work a review reads.
type AgentReviewTarget struct {
	IssueIdentifier, IssueTitle string
	Repository                  string
	PullRequestNumber           int
	PullRequestURL              string
	Branch                      string
	BaseBranch                  string
	CommitSHA                   string
}

// MaxAgentReviewFindings bounds the stored findings (bytes).
const MaxAgentReviewFindings = 16 << 10

// AgentReviewPrompt is the reviewer's instructions. The focus is the
// requester's own text. The reviewer has no push credential and Kuayle never
// publishes a review attempt; the prompt also asks it not to change anything.
func AgentReviewPrompt(target AgentReviewTarget, focus string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "You are reviewing pull request #%d in %s for issue %s: %s.\n", target.PullRequestNumber, target.Repository, target.IssueIdentifier, oneLineText(target.IssueTitle, 300))
	fmt.Fprintf(&b, "Kuayle published commit %s on branch %s; the base branch is %s.\n\n", target.CommitSHA, target.Branch, target.BaseBranch)
	b.WriteString("This is a read-only review:\n")
	fmt.Fprintf(&b, "1. Run `git checkout --detach %s` in this worktree (the commit is already in the local repository; there is no network access to GitHub).\n", target.CommitSHA)
	fmt.Fprintf(&b, "2. Review the change with `git diff origin/%s...HEAD` and read the surrounding code. You may run the project's tests to support a finding.\n", target.BaseBranch)
	b.WriteString("3. Do not edit, commit or push anything. You have no repository credentials and Kuayle discards any changes.\n\n")
	if focus = strings.TrimSpace(focus); focus != "" {
		fmt.Fprintf(&b, "The requester asked you to focus on:\n%s\n\n", focus)
	}
	b.WriteString("Finish with your review as your final message, at most 3,500 characters, in this form:\n")
	b.WriteString("Verdict: approve | request_changes | comment\n\n")
	b.WriteString("Then a Markdown list of findings, most important first. Each finding names the file and line, a severity (blocker, major, minor, nit) and what to change. Write \"No findings.\" when there are none.\n")
	return b.String()
}

// ParseAgentReview splits a reviewer's final output into a normalized verdict
// and bounded, valid UTF-8 findings text. The text is untrusted: it is stored
// and rendered as escaped Markdown, never interpreted as instructions.
func ParseAgentReview(output string) (string, string) {
	output = strings.ToValidUTF8(strings.ReplaceAll(output, "\x00", ""), "")
	verdict := "unknown"
	lines := strings.Split(strings.TrimSpace(output), "\n")
	for index, line := range lines {
		trimmed := strings.TrimSpace(strings.Trim(strings.TrimSpace(line), "*_#> "))
		label, value, ok := strings.Cut(trimmed, ":")
		if !ok || !strings.EqualFold(strings.TrimSpace(strings.Trim(label, "*_ ")), "verdict") {
			continue
		}
		value = strings.ToLower(strings.TrimSpace(strings.Trim(value, "*_` ")))
		switch {
		case strings.HasPrefix(value, "approve"):
			verdict = "approve"
		case strings.HasPrefix(value, "request_changes"), strings.HasPrefix(value, "request changes"), strings.HasPrefix(value, "changes"):
			verdict = "request_changes"
		case strings.HasPrefix(value, "comment"):
			verdict = "comment"
		}
		lines = append(lines[:index:index], lines[index+1:]...)
		break
	}
	findings := strings.TrimSpace(strings.Join(lines, "\n"))
	if len(findings) > MaxAgentReviewFindings {
		findings = findings[:MaxAgentReviewFindings]
		for !utf8.ValidString(findings) {
			findings = findings[:len(findings)-1]
		}
	}
	return verdict, findings
}

func oneLineText(value string, limit int) string {
	value = strings.Join(strings.Fields(value), " ")
	if utf8.RuneCountInString(value) > limit {
		value = string([]rune(value)[:limit])
	}
	return value
}
