package domain

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Members of a pipeline task (KUA-252). A member is a role of the pipeline
// working on one task, and a member is an agent task: its turns are
// generations of the root (implementing) task for the role that writes, and
// of one review task of the root for every other role. That review task is
// reused across turns, so its session resumes; it keeps a review's
// isolation (read-only, no repository credentials, never published).

// Member states: idle between turns, queued while its turn waits for a
// machine, running while the turn's agent works.
const (
	MemberIdle    = "idle"
	MemberQueued  = "queued"
	MemberRunning = "running"
)

// TaskMember is a role working on a pipeline task.
type TaskMember struct {
	ID          uuid.UUID `json:"id" db:"id"`
	WorkspaceID uuid.UUID `json:"-" db:"workspace_id"`
	// TaskID is the root (implementing) task.
	TaskID uuid.UUID `json:"task_id" db:"task_id"`
	Role   string    `json:"role" db:"role"`
	// ProfileID is pinned when the member is created.
	ProfileID uuid.UUID `json:"profile_id" db:"profile_id"`
	// MemberTaskID runs the member's turns; nil for the role that writes,
	// whose turns are the root task's.
	MemberTaskID *uuid.UUID `json:"member_task_id" db:"member_task_id"`
	// LastReadSeq is the room cursor: posts up to it were delivered.
	LastReadSeq int64  `json:"last_read_seq" db:"last_read_seq"`
	State       string `json:"state" db:"state"`
	Turns       int    `json:"turns" db:"turns"`
	NoProgress  int    `json:"no_progress" db:"no_progress"`
	// PinnedCommit is the published commit the current (or last) turn
	// reads; VerdictAsked says that turn must end with a verdict for it.
	PinnedCommit *string   `json:"pinned_commit" db:"pinned_commit"`
	VerdictAsked bool      `json:"verdict_asked" db:"verdict_asked"`
	CreatedAt    time.Time `json:"created_at" db:"created_at"`
	UpdatedAt    time.Time `json:"updated_at" db:"updated_at"`
}

// TaskVerdict is a member's verdict on one commit of the root task, given
// by the room post Seq.
type TaskVerdict struct {
	ID          uuid.UUID  `json:"id" db:"id"`
	WorkspaceID uuid.UUID  `json:"-" db:"workspace_id"`
	TaskID      uuid.UUID  `json:"task_id" db:"task_id"`
	Role        string     `json:"role" db:"role"`
	CommitSHA   string     `json:"commit_sha" db:"commit_sha"`
	Verdict     string     `json:"verdict" db:"verdict"`
	Findings    string     `json:"findings" db:"findings"`
	RunID       *uuid.UUID `json:"run_id" db:"run_id"`
	Seq         int64      `json:"seq" db:"seq"`
	CreatedAt   time.Time  `json:"created_at" db:"created_at"`
}

// MemberState is the state of a member whose turns run on a task in
// taskState: busy from the moment a turn is queued until its task rests.
func MemberState(taskState string) string {
	switch taskState {
	case "queued", "preparing", "waiting_for_disk":
		return MemberQueued
	case "running", "waiting_input", "validating", "publishing", "cancelling":
		return MemberRunning
	}
	return MemberIdle
}

// Writer is the roster member that writes the code.
func (p Pipeline) Writer() (PipelineMember, bool) {
	for _, member := range p.Roster {
		if member.Writer {
			return member, true
		}
	}
	return PipelineMember{}, false
}

// RosterRole is the roster member that plays a role.
func (p Pipeline) RosterRole(name string) (PipelineMember, bool) { return p.role(name) }

// RoomReviewRequested reports whether a role's inbox (RoomInbox) asks it for
// a review: a request for a review that the room took.
func RoomReviewRequested(inbox []TaskMessage) bool {
	for _, m := range inbox {
		if m.Kind == RoomRequest && m.RejectReason == nil && m.Data.Action == RoomRequestReview {
			return true
		}
	}
	return false
}

// MemberTurn is what the turn of a member that does not write code is
// about: the published work it reads (nil before anything is published),
// its profile's instructions, what Kuayle knows about CI on that commit,
// whether it must end with a verdict, and its unread posts.
type MemberTurn struct {
	Role         string
	Target       *AgentReviewTarget
	Instructions string
	CI           AgentReviewCI
	VerdictAsked bool
	Posts        []TaskMessage
}

// MemberInstructionsBytes bounds the profile's instructions in a turn
// prompt, as a graph workflow bounds a reviewer step's focus.
const MemberInstructionsBytes = 4000

// MemberTurnPrompt is the prompt of a turn of a member that does not write
// code: the standing contract of its role, then its room (RoomTurnPrompt).
// A turn asked for a verdict is a review of the pinned commit
// (AgentReviewPrompt) that may answer in the room instead of its final
// message; any other turn answers what it was asked.
func MemberTurnPrompt(turn MemberTurn) string {
	var b strings.Builder
	fmt.Fprintf(&b, "You are the @%s of the team working on this task. Kuayle continues your session across your turns: what you found before still holds, so build on it. Another role writes the code; you read it and answer in the task room.\n\n", turn.Role)
	instructions := strings.TrimSpace(turn.Instructions)
	if len(instructions) > MemberInstructionsBytes {
		instructions = strings.ToValidUTF8(instructions[:MemberInstructionsBytes], "")
	}
	switch {
	case turn.VerdictAsked && turn.Target != nil:
		b.WriteString(AgentReviewPrompt(*turn.Target, instructions, turn.CI))
		fmt.Fprintf(&b, "You may post that review in the room instead: kuayle-say --to @all --verdict approve|request_changes|comment --commit %s --file findings.md. Either way this turn ends with a verdict for commit %s.\n", turn.Target.CommitSHA, turn.Target.CommitSHA)
	case turn.Target != nil:
		t := turn.Target
		fmt.Fprintf(&b, "Kuayle published commit %s on branch %s (pull request #%d in %s); the base branch is %s. To read it, run `git checkout --detach %s` in this worktree, then `git diff origin/%s...HEAD`. ", t.CommitSHA, t.Branch, t.PullRequestNumber, t.Repository, t.BaseBranch, t.CommitSHA, t.BaseBranch)
		b.WriteString("Do not edit, commit or push anything: you have no repository credentials and Kuayle discards any change.\n")
	default:
		b.WriteString("Nothing is published yet, so there is no commit to review. Do not edit, commit or push anything: you have no repository credentials and Kuayle discards any change.\n")
	}
	if !turn.VerdictAsked || turn.Target == nil {
		if instructions != "" {
			fmt.Fprintf(&b, "\nYour role's instructions:\n%s\n", instructions)
		}
		b.WriteString("\nNobody asked you for a verdict this turn: answer what you were asked in the room, and end with a short final message. Give a verdict only when a review is requested.\n")
	}
	if room := RoomTurnPrompt(turn.Role, turn.Posts); room != "" {
		b.WriteString("\n" + room)
	}
	return b.String()
}

// VerdictClosingAsk is what the closing continuation asks of a member's turn
// that was asked for a verdict of commit and gave none.
func VerdictClosingAsk(commit string) ClosingAsk {
	return ClosingAsk{
		Missing: fmt.Sprintf("you were asked to review commit %s and gave no verdict", commit),
		Do:      fmt.Sprintf("Post it now with kuayle-say --to @all --verdict approve|request_changes|comment --commit %s and your findings as the text or with --file (see \"The task room\" in kuayle-agents)", commit),
	}
}

// NoVerdictNote is what Kuayle posts in the room for a member's turn that
// was asked for a verdict and ended without one.
func NoVerdictNote(role, commit string) string {
	return fmt.Sprintf("The @%s ended its turn without a verdict for commit %s.", role, commit)
}
