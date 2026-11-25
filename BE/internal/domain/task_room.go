package domain

import (
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/lib/pq"
)

// The room of a pipeline task (KUA-252): one ordered message log per task.
// The task's creator, the roles of its pipeline (the lead among them) and
// Kuayle post to it. TaskRoomRefusal says who may post what; a member's
// refused post is kept with the reason for its sender and never delivered.
// Text from agents is untrusted.

// Who posts.
const (
	RoomAuthorHuman  = "human"
	RoomAuthorMember = "member"
	RoomAuthorKuayle = "kuayle"
)

// What a post is.
const (
	RoomSay     = "say"
	RoomAsk     = "ask"
	RoomPhase   = "phase"
	RoomVerdict = "verdict"
	RoomRequest = "request"
	RoomWaiver  = "waiver"
	RoomGrant   = "grant"
	RoomFact    = "fact"
	RoomSummary = "summary"

	RoomRequestReview = "review"
	RoomRequestSplit  = "split"
	RoomActionApprove = "approve"
	RoomActionWaive   = "waive"
)

// RoomKinds lists every kind of post.
var RoomKinds = []string{RoomSay, RoomAsk, RoomPhase, RoomVerdict, RoomRequest, RoomWaiver, RoomGrant, RoomFact, RoomSummary}

// A post's text and structured part are each at most an outbox file; an
// agent's text is at most an agent message.
const (
	RoomMaxBodyBytes  = AgentOutboxFileMaxBytes
	RoomMaxAgentRunes = AgentMessageMaxRunes
)

var (
	roomRequests = []string{RoomRequestReview, "merge", "publish", RoomRequestSplit}
	roomVerdicts = []string{"approve", "request_changes", "comment"}
	roomCommit   = regexp.MustCompile(`^[0-9a-f]{7,64}$`)
)

// TaskMessage is one post of a task's room, ordered by Seq.
type TaskMessage struct {
	ID             uuid.UUID      `json:"id" db:"id"`
	WorkspaceID    uuid.UUID      `json:"-" db:"workspace_id"`
	TaskID         uuid.UUID      `json:"task_id" db:"task_id"`
	Seq            int64          `json:"seq" db:"seq"`
	AuthorKind     string         `json:"author_kind" db:"author_kind"`
	AuthorRole     *string        `json:"author_role" db:"author_role"`
	AuthorUserID   *uuid.UUID     `json:"author_user_id" db:"author_user_id"`
	AuthorMemberID *uuid.UUID     `json:"author_member_id" db:"author_member_id"`
	RunID          *uuid.UUID     `json:"run_id" db:"run_id"`
	ToRoles        pq.StringArray `json:"to_roles" db:"to_roles"`
	Kind           string         `json:"kind" db:"kind"`
	Body           string         `json:"body" db:"body"`
	Data           TaskRoomData   `json:"data" db:"data"`
	ReplyTo        *int64         `json:"reply_to" db:"reply_to"`
	DedupeKey      *string        `json:"-" db:"dedupe_key"`
	RejectReason   *string        `json:"reject_reason" db:"reject_reason"`
	CreatedAt      time.Time      `json:"created_at" db:"created_at"`
}

// IsLeadReport marks an accepted summary from the pipeline's managing member.
func (m TaskMessage) IsLeadReport() bool {
	return m.Kind == RoomSummary && m.AuthorKind == RoomAuthorMember && m.AuthorRole != nil &&
		*m.AuthorRole == PipelineLeadRole && m.RejectReason == nil
}

// TaskRoomData is the structured part of a post; each kind reads its own
// fields.
type TaskRoomData struct {
	Phase    string           `json:"phase,omitempty"`    // phase: the phase moved to
	Verdict  string           `json:"verdict,omitempty"`  // verdict: approve, request_changes or comment
	Commit   string           `json:"commit,omitempty"`   // reviewed, approved or waived head
	Role     string           `json:"role,omitempty"`     // verdict: author's role; grant: recipient
	Check    string           `json:"check,omitempty"`    // waiver, grant: a check of the pipeline
	Standing bool             `json:"standing,omitempty"` // waiver: holds across heads
	Action   string           `json:"action,omitempty"`   // requested or granted action
	Issues   []TaskSplitIssue `json:"issues,omitempty"`   // split: the proposed children
	Fact     string           `json:"fact,omitempty"`     // fact: its name
	Value    json.RawMessage  `json:"value,omitempty"`    // fact: its value
}

// TaskSplitIssue describes one child proposed by the lead. The task supplies
// its parent, team and workspace when the owner approves the request.
type TaskSplitIssue struct {
	Title       string `json:"title"`
	Description string `json:"description"`
}

// SplitRequestRefusal validates proposed children, including stored requests
// that predate structured split proposals.
func SplitRequestRefusal(issues []TaskSplitIssue) string {
	if len(issues) == 0 || len(issues) > 10 {
		return "a split needs 1 to 10 sub-issues"
	}
	for i, issue := range issues {
		if !validSplitText(issue.Title, 500) {
			return fmt.Sprintf("sub-issue %d needs a title of 1 to 500 characters", i+1)
		}
		if !validSplitText(issue.Description, 1000) {
			return fmt.Sprintf("sub-issue %d needs a description of 1 to 1000 characters", i+1)
		}
	}
	return ""
}

func validSplitText(text string, maxRunes int) bool {
	return strings.TrimSpace(text) != "" && utf8.ValidString(text) && !strings.ContainsRune(text, 0) && utf8.RuneCountInString(text) <= maxRunes
}

func (d *TaskRoomData) Scan(value any) error { return scanJSON(value, d) }

// TaskRoomPost is a post before it is stored. AuthorRole is a member's role
// ("lead" for the lead).
type TaskRoomPost struct {
	AuthorKind     string
	AuthorRole     string
	AuthorUserID   *uuid.UUID
	AuthorMemberID *uuid.UUID
	RunID          *uuid.UUID
	ToRoles        []string
	Kind           string
	Body           string
	Data           TaskRoomData
	ReplyTo        *int64
	DedupeKey      string
	// Refused is set on a member's post that is refused before the room
	// reads it (an outbox file that could not be parsed): it is stored with
	// this reason.
	Refused string
}

// Clean trims valid text and drops its control characters (invalid text is
// left for the refusal), and lists each recipient once.
func (p TaskRoomPost) Clean() TaskRoomPost {
	if utf8.ValidString(p.Body) {
		p.Body = cleanAgentMessage(p.Body)
	}
	p.ToRoles = slices.Compact(slices.Sorted(slices.Values(p.ToRoles)))
	return p
}

// TaskRoomRefusal says why the room of a task on pipeline p refuses a post,
// or "" when it takes it: first who may post what (the plan's matrix), then
// the post's bounds and data. leadGranted says the task's creator granted
// the lead this post's exact action (approve or waive this check). The
// pinned pipeline's autonomy may grant it too. It has no I/O.
func TaskRoomRefusal(p Pipeline, post TaskRoomPost, leadGranted bool) string {
	human, kuayle := post.AuthorKind == RoomAuthorHuman, post.AuthorKind == RoomAuthorKuayle
	var who string
	var writer, expert, lead bool
	switch post.AuthorKind {
	case RoomAuthorHuman:
		who = "a person"
	case RoomAuthorKuayle:
		who = "kuayle"
	case RoomAuthorMember:
		who = "the " + post.AuthorRole
		behaviour := p.Behaviour(post.AuthorRole)
		if behaviour == "" {
			return fmt.Sprintf("%q is not a role of this pipeline", post.AuthorRole)
		}
		writer, expert, lead = behaviour == AgentBehaviourChangesCode, behaviour == AgentBehaviourReviews, behaviour == AgentBehaviourManages
	default:
		return "unknown author"
	}
	var allowed bool
	switch post.Kind {
	case RoomSay, RoomAsk:
		allowed = !kuayle
	case RoomVerdict:
		if expert && post.Data.Role != "" && post.Data.Role != post.AuthorRole {
			return "a verdict speaks only for its author's role"
		}
		allowed = expert
	case RoomPhase:
		if kuayle && p.Lead != nil {
			return "kuayle moves phases only when there is no lead"
		}
		allowed = lead || kuayle
	case RoomRequest:
		if post.Data.Action == RoomRequestSplit && (!lead || post.AuthorMemberID == nil) {
			return "only the lead member may request a split"
		}
		if post.Data.Action == RoomActionApprove && (!lead || (!leadGranted && p.Autonomy.Approve != PipelineLeadRole)) {
			return "only the lead with an approval grant may approve here"
		}
		if writer && post.Data.Action != RoomRequestReview {
			return who + " may request only a review"
		}
		allowed = human || lead || writer
	case RoomWaiver:
		if lead && !leadGranted && p.Autonomy.Waive[post.Data.Check] != PipelineLeadRole {
			return "the lead waives only the checks granted to it"
		}
		allowed = human || lead
	case RoomGrant:
		allowed = human
	case RoomFact:
		allowed = kuayle
	case RoomSummary:
		allowed = lead || kuayle
	default:
		return fmt.Sprintf("unknown kind %q", post.Kind)
	}
	if !allowed {
		if why := leadRefusals[post.Kind]; lead && why != "" {
			return fmt.Sprintf("%s may not post %s: %s", who, post.Kind, why)
		}
		return fmt.Sprintf("%s may not post %s", who, post.Kind)
	}

	switch {
	case !utf8.ValidString(post.Body):
		return "the text is not valid UTF-8"
	case len(post.Body) > RoomMaxBodyBytes:
		return fmt.Sprintf("the text is at most %d bytes", RoomMaxBodyBytes)
	case post.AuthorKind == RoomAuthorMember && utf8.RuneCountInString(post.Body) > RoomMaxAgentRunes:
		return fmt.Sprintf("an agent's text is at most %d characters", RoomMaxAgentRunes)
	case (post.Kind == RoomSay || post.Kind == RoomAsk) && strings.TrimSpace(post.Body) == "":
		return "the text is empty"
	}
	for _, to := range post.ToRoles {
		if _, ok := p.role(to); !ok && to != PipelineOwnerHuman && (to != PipelineLeadRole || p.Lead == nil) {
			return fmt.Sprintf("%q is not a role of this pipeline", to)
		}
	}
	if raw, err := json.Marshal(post.Data); err != nil || len(raw) > RoomMaxBodyBytes {
		return fmt.Sprintf("the structured part is at most %d bytes", RoomMaxBodyBytes)
	}
	d := post.Data
	if len(d.Issues) > 0 && (post.Kind != RoomRequest || d.Action != RoomRequestSplit) {
		return "sub-issues belong to a split request"
	}
	switch post.Kind {
	case RoomVerdict:
		if !slices.Contains(roomVerdicts, d.Verdict) || !roomCommit.MatchString(d.Commit) {
			return "a verdict is approve, request_changes or comment, for a commit"
		}
	case RoomPhase:
		ids := make([]string, 0, len(p.Phases))
		for _, phase := range p.Phases {
			ids = append(ids, phase.ID)
		}
		if !slices.Contains(ids, d.Phase) {
			return fmt.Sprintf("%q is not a phase of this pipeline (%s)", d.Phase, strings.Join(ids, ", "))
		}
		// The lead moves the task (KUA-301) and says why.
		if lead && strings.TrimSpace(post.Body) == "" {
			return fmt.Sprintf("a phase move says why: kuayle-say --to @all --phase %s \"the reason\"", d.Phase)
		}
	case RoomRequest:
		switch {
		case d.Action == RoomRequestSplit:
			return SplitRequestRefusal(d.Issues)
		case d.Action == RoomActionApprove:
			if (len(d.Commit) != 40 && len(d.Commit) != 64) || !roomCommit.MatchString(d.Commit) {
				return "approval names the full head commit"
			}
		case !slices.Contains(roomRequests, d.Action):
			return fmt.Sprintf("%q is not a request (%s)", d.Action, strings.Join(roomRequests, ", "))
		}
	case RoomGrant:
		if p.Lead == nil || (d.Role != "" && d.Role != PipelineLeadRole) {
			return "a grant names the lead of this pipeline"
		}
		switch d.Action {
		case RoomActionApprove, RoomRequestSplit:
			if d.Check != "" {
				return "an approval or split grant cannot name a check"
			}
			return ""
		case "", RoomActionWaive: // A check-only grant delegates waiving that check.
		default:
			return "a grant delegates approval, a split or waiving a named check"
		}
		fallthrough
	case RoomWaiver:
		if !slices.ContainsFunc(p.Phases, func(phase PipelinePhase) bool {
			return slices.ContainsFunc(phase.Checks, func(c PipelineCheck) bool { return c.ID == d.Check })
		}) {
			return fmt.Sprintf("%q is not a check of this pipeline", d.Check)
		}
		if post.Kind == RoomWaiver && strings.TrimSpace(post.Body) == "" {
			return "a waiver needs a reason"
		}
	case RoomFact:
		if d.Fact == "" {
			return "a fact names the fact"
		}
	}
	return ""
}

// role is the roster member that plays a role.
func (p Pipeline) role(name string) (PipelineMember, bool) {
	i := slices.IndexFunc(p.Roster, func(m PipelineMember) bool { return m.Role == name })
	if i < 0 {
		return PipelineMember{}, false
	}
	return p.Roster[i], true
}
