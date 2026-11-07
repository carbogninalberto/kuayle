package domain

import (
	"encoding/json"
	"github.com/google/uuid"
	"time"
)

type Conversation struct {
	ID          uuid.UUID  `json:"id" db:"id"`
	WorkspaceID uuid.UUID  `json:"workspace_id" db:"workspace_id"`
	CreatorID   uuid.UUID  `json:"creator_id" db:"creator_id"`
	IssueID     *uuid.UUID `json:"issue_id" db:"issue_id"`
	TeamID      *uuid.UUID `json:"team_id" db:"team_id"`
	Title       string     `json:"title" db:"title"`
	CreatedAt   time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at" db:"updated_at"`
	FolderID    *uuid.UUID `json:"folder_id,omitempty" db:"folder_id"`
	// ProjectID is the effective project: a free chat's own tag, or an issue
	// chat's current issue project. It is null unless the project is visible to
	// the owner; Project carries its summary.
	ProjectID *uuid.UUID           `json:"project_id" db:"project_id"`
	Project   *ConversationProject `json:"project" db:"-"`
}

// ConversationProject is the visible project a conversation is tagged with.
type ConversationProject struct {
	ID     uuid.UUID  `json:"id" db:"id"`
	Name   string     `json:"name" db:"name"`
	TeamID *uuid.UUID `json:"team_id" db:"team_id"`
}

// ConversationFilter narrows the owner's list. ProjectID selects chats of one
// visible project; NoProject selects chats without a visible project.
type ConversationFilter struct {
	ProjectID *uuid.UUID
	NoProject bool
}

// ConversationFolder groups its owner's conversations; it is never shared.
type ConversationFolder struct {
	ID          uuid.UUID `json:"id" db:"id"`
	WorkspaceID uuid.UUID `json:"workspace_id" db:"workspace_id"`
	OwnerID     uuid.UUID `json:"-" db:"owner_id"`
	Name        string    `json:"name" db:"name"`
	CreatedAt   time.Time `json:"created_at" db:"created_at"`
	UpdatedAt   time.Time `json:"updated_at" db:"updated_at"`
}
type ConversationMessage struct {
	ID             uuid.UUID  `json:"id" db:"id"`
	ConversationID uuid.UUID  `json:"conversation_id" db:"conversation_id"`
	Sequence       int64      `json:"sequence" db:"sequence"`
	RequestID      uuid.UUID  `json:"request_id" db:"request_id"`
	ParentID       *uuid.UUID `json:"parent_id" db:"parent_id"`
	Role           string     `json:"role" db:"role"`
	Content        string     `json:"content" db:"content"`
	Status         string     `json:"status" db:"status"`
	Provider       string     `json:"provider" db:"provider"`
	Model          string     `json:"model" db:"model"`
	LeaseToken     *uuid.UUID `json:"-" db:"lease_token"`
	LeaseExpiresAt *time.Time `json:"-" db:"lease_expires_at"`
	CreatedAt      time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at" db:"updated_at"`
	// Attachments are the images of a user message, in order.
	Attachments []ConversationAttachment `json:"attachments,omitempty" db:"-"`
}
type ConversationEvent struct {
	Cursor         int64           `json:"cursor" db:"cursor"`
	ID             uuid.UUID       `json:"id" db:"id"`
	ConversationID uuid.UUID       `json:"conversation_id" db:"conversation_id"`
	Type           string          `json:"type" db:"type"`
	Payload        json.RawMessage `json:"payload" db:"payload"`
	CreatedAt      time.Time       `json:"created_at" db:"created_at"`
}
type ConversationHistory struct {
	Conversation Conversation           `json:"conversation"`
	Issue        *ConversationIssue     `json:"issue,omitempty"`
	Messages     []ConversationMessage  `json:"messages"`
	Tools        []ConversationToolCall `json:"tools"`
	Approvals    []ConversationApproval `json:"approvals"`
	// AgentMessages are what the conversation's coding agents sent it through
	// their live channel during the page's span: untrusted text to show.
	AgentMessages []AgentLiveChatMessage `json:"agent_messages"`
	Cursor        int64                  `json:"cursor"`
	HasMore       bool                   `json:"has_more"`
}
type ConversationAttempt struct {
	Conversation Conversation
	Message      ConversationMessage
}

// ConversationIssue is a bounded, freshly authorized snapshot for the assistant.
type ConversationIssue struct {
	ID          uuid.UUID `json:"id" db:"id"`
	Identifier  string    `json:"identifier" db:"identifier_text"`
	Title       string    `json:"title" db:"title"`
	Description string    `json:"description" db:"description"`
	Status      string    `json:"status" db:"status"`
}

// ConversationToolCall is one assistant tool invocation. Input and output are
// bounded, sanitized JSON; output is untrusted data, never instructions.
type ConversationToolCall struct {
	ID             uuid.UUID        `json:"id" db:"id"`
	ConversationID uuid.UUID        `json:"conversation_id" db:"conversation_id"`
	MessageID      uuid.UUID        `json:"message_id" db:"message_id"`
	Sequence       int              `json:"sequence" db:"sequence"`
	CallID         string           `json:"call_id" db:"call_id"`
	Name           string           `json:"name" db:"name"`
	Input          json.RawMessage  `json:"input" db:"input"`
	Status         string           `json:"status" db:"status"`
	Output         *json.RawMessage `json:"output,omitempty" db:"output"`
	Error          *string          `json:"error,omitempty" db:"error"`
	IssueID        *uuid.UUID       `json:"issue_id,omitempty" db:"issue_id"`
	CreatedAt      time.Time        `json:"created_at" db:"created_at"`
	CompletedAt    *time.Time       `json:"completed_at,omitempty" db:"completed_at"`
}

// ConversationApproval is a human decision requested by an assistant tool. The
// backend binds it to one actor, one action and one target; UI state is never
// the authority.
type ConversationApproval struct {
	ID             uuid.UUID       `json:"id" db:"id"`
	ConversationID uuid.UUID       `json:"conversation_id" db:"conversation_id"`
	ToolCallID     uuid.UUID       `json:"tool_call_id" db:"tool_call_id"`
	Action         string          `json:"action" db:"action"`
	ActorID        uuid.UUID       `json:"actor_id" db:"actor_id"`
	IssueID        uuid.UUID       `json:"issue_id" db:"issue_id"`
	Payload        json.RawMessage `json:"payload" db:"payload"`
	Status         string          `json:"status" db:"status"`
	RequestID      uuid.UUID       `json:"-" db:"request_id"`
	TaskID         *uuid.UUID      `json:"task_id,omitempty" db:"task_id"`
	Error          *string         `json:"error,omitempty" db:"error"`
	ExpiresAt      time.Time       `json:"expires_at" db:"expires_at"`
	ResolvedAt     *time.Time      `json:"resolved_at,omitempty" db:"resolved_at"`
	CreatedAt      time.Time       `json:"created_at" db:"created_at"`
	UpdatedAt      time.Time       `json:"updated_at" db:"updated_at"`
}

// DelegationPayload is the immutable request an approval authorizes.
type DelegationPayload struct {
	Identifier   string `json:"identifier"`
	Title        string `json:"title"`
	Instructions string `json:"instructions"`
	// Harness and Model are the coding agent the assistant proposed; empty
	// means the workspace, project or team default. The approver can change them.
	Harness string `json:"harness,omitempty"`
	Model   string `json:"model,omitempty"`
	// WorkflowID is the workflow the assistant chose ("standard" or a
	// workflow ID of the workspace) and Workflow its name for the card; empty
	// means the issue's default workflow. Creating the task checks it again.
	WorkflowID string `json:"workflow_id,omitempty"`
	Workflow   string `json:"workflow,omitempty"`
	// Repository, BaseBranch and RepositorySource show the development
	// repository resolved when approval was requested (issue → project → team
	// → workspace). The task resolves it again when it runs.
	Repository       string `json:"repository,omitempty"`
	BaseBranch       string `json:"base_branch,omitempty"`
	RepositorySource string `json:"repository_source,omitempty"`
}

// MergePayload binds a merge approval to the pull request and the commit shown
// to the user, which is the head GitHub reported when approval was requested; a
// later push invalidates the approval. PublishedCommit is the commit Kuayle
// published, which differs from Commit when someone else pushed to the branch.
type MergePayload struct {
	Identifier      string    `json:"identifier"`
	Title           string    `json:"title"`
	TaskID          uuid.UUID `json:"task_id"`
	Repository      string    `json:"repository"`
	PullRequest     int       `json:"pull_request"`
	PullRequestURL  string    `json:"pull_request_url"`
	Commit          string    `json:"commit"`
	PublishedCommit string    `json:"published_commit,omitempty"`
	// MergeMethod is merge, squash or rebase, from the development settings or
	// the first the repository allows; approvals created before it merged by squash.
	MergeMethod string                 `json:"merge_method,omitempty"`
	HeadChange  *PullRequestHeadChange `json:"head_change,omitempty"`
}

// PullRequestHeadChange describes what others added to a task's pull request
// since Kuayle published it. Kind is "merge" when every extra commit has several
// parents (usually the base branch merged in; the branch is not verified),
// "rewritten" when the published commit is no longer in the head's history, and
// otherwise "pushed". TreeChanged is false only when the head's tree is known to
// equal the published commit's.
type PullRequestHeadChange struct {
	Kind        string                  `json:"kind"`
	TreeChanged bool                    `json:"tree_changed"`
	Commits     []PullRequestHeadCommit `json:"commits"`
	Truncated   bool                    `json:"truncated,omitempty"`
	// TotalCommits is how many commits GitHub counts since publication.
	TotalCommits int `json:"total_commits,omitempty"`
}

// PullRequestHeadCommit is one commit added after publication.
type PullRequestHeadCommit struct {
	SHA     string `json:"sha"`
	Author  string `json:"author"`
	Subject string `json:"subject"`
	Merge   bool   `json:"merge,omitempty"`
}

// PullRequestHead is the pull request head read from GitHub. Change is nil
// while the head is the published commit.
type PullRequestHead struct {
	SHA    string
	Change *PullRequestHeadChange
}

// AcceptPayload binds an acceptance approval to the task attempt and the pull
// request shown to the user. Accepting completes the task in Kuayle and never
// merges; a newer attempt invalidates the approval.
type AcceptPayload struct {
	Identifier     string    `json:"identifier"`
	Title          string    `json:"title"`
	TaskID         uuid.UUID `json:"task_id"`
	Generation     int64     `json:"generation"`
	PullRequest    int       `json:"pull_request"`
	PullRequestURL string    `json:"pull_request_url"`
	Commit         string    `json:"commit"`
}

// ReviewPayload binds a review approval to the task, its published pull
// request and commit, the harness and whether to comment on the pull request.
type ReviewPayload struct {
	Identifier        string    `json:"identifier"`
	Title             string    `json:"title"`
	TaskID            uuid.UUID `json:"task_id"`
	Harness           string    `json:"harness"`
	Model             string    `json:"model,omitempty"`
	Focus             string    `json:"focus,omitempty"`
	PostToPullRequest bool      `json:"post_to_pull_request"`
	PullRequest       int       `json:"pull_request"`
	PullRequestURL    string    `json:"pull_request_url"`
	Commit            string    `json:"commit"`
}

// FollowUpPayload binds a follow-up approval to the task attempt the user saw
// and the exact instructions shown to them.
type FollowUpPayload struct {
	Identifier   string    `json:"identifier"`
	Title        string    `json:"title"`
	TaskID       uuid.UUID `json:"task_id"`
	Generation   int64     `json:"generation"`
	Instructions string    `json:"instructions"`
	// Model switches the task's model for this attempt; the harness stays.
	Model string `json:"model,omitempty"`
	// CurrentModel is the model the task ran with, shown on the card.
	CurrentModel string `json:"current_model,omitempty"`
	// Harness is the task's coding agent, shown on the card.
	Harness string `json:"harness,omitempty"`
	// ModelServerID is the task's model server (nil: the workspace default), so
	// the card opens on the server that runs CurrentModel.
	ModelServerID *uuid.UUID `json:"model_server_id,omitempty"`
}

// AppPortAudienceOwner means only the machine's owner, signed in to Kuayle,
// can open an app port link: the gateway admits nobody else.
const AppPortAudienceOwner = "owner"

// PreviewPayload binds a start_preview approval to the issue's task, its
// machine and the preview command shown to the user.
type PreviewPayload struct {
	Identifier string    `json:"identifier"`
	Title      string    `json:"title"`
	TaskID     uuid.UUID `json:"task_id"`
	MachineID  uuid.UUID `json:"machine_id"`
	Machine    string    `json:"machine"`
	Command    []string  `json:"command"`
	// Port is the preferred preview port; the machine picks a free one when
	// it is taken.
	Port     *int   `json:"port,omitempty"`
	Audience string `json:"audience"`
}

// ExposePortPayload binds an expose_port approval to the task's machine, the
// port and the gateway host shown to the user. Existing reports that the
// machine already exposed the port when approval was requested.
type ExposePortPayload struct {
	Identifier string    `json:"identifier"`
	Title      string    `json:"title"`
	TaskID     uuid.UUID `json:"task_id"`
	MachineID  uuid.UUID `json:"machine_id"`
	Machine    string    `json:"machine"`
	Port       int       `json:"port"`
	Label      *string   `json:"label,omitempty"`
	Host       string    `json:"host"`
	Existing   bool      `json:"existing"`
	Audience   string    `json:"audience"`
}

// UpgradePayload binds an upgrade_instance approval to the versions the user
// saw. The approval belongs to no issue.
type UpgradePayload struct {
	CurrentVersion string `json:"current_version"`
	LatestVersion  string `json:"latest_version"`
	Changelog      string `json:"changelog,omitempty"`
}

// ReleasePayload is a create_release request: the tag to create at a commit of
// the repository's default branch, and the notes to publish. The notes are the
// assistant's draft until the approver edits them on the card.
type ReleasePayload struct {
	Repository     string   `json:"repository"`
	Tag            string   `json:"tag"`
	Name           string   `json:"name"`
	TargetCommit   string   `json:"target_commit"`
	Branch         string   `json:"branch"`
	PreviousTag    string   `json:"previous_tag,omitempty"`
	Notes          string   `json:"notes"`
	NotesEdited    bool     `json:"notes_edited,omitempty"`
	ChecksPassed   int      `json:"checks_passed"`
	Commits        int      `json:"commits"`
	ScannedFiles   int      `json:"scanned_files"`
	UnscannedFiles []string `json:"unscanned_files,omitempty"`
	CompareURL     string   `json:"compare_url,omitempty"`
	ReleaseURL     string   `json:"release_url"`
}

// GitHubAccessGap is the GitHub App access an action needs but lacks. It rides
// in the tool result and in the approval's payload so the card can show it.
type GitHubAccessGap struct {
	Missing     []GitHubPermissionNeed `json:"missing"`
	SettingsURL string                 `json:"settings_url,omitempty"`
	Message     string                 `json:"message"`
	// Unverified means GitHub could not be asked, not that a permission is missing.
	Unverified bool `json:"unverified,omitempty"`
}

// GitHubPermissionNeed is one GitHub App permission at a minimum level.
type GitHubPermissionNeed struct {
	Name  string `json:"name"`
	Level string `json:"level"`
}

// PullRequestActionPayload binds an approval of reply_to_review_comment,
// resolve_review_thread, update_pull_request_branch or mark_pull_request_ready
// to the task's pull request and to exactly what the user saw. Only the fields
// of the approval's action are set.
type PullRequestActionPayload struct {
	Identifier     string    `json:"identifier"`
	Title          string    `json:"title"`
	TaskID         uuid.UUID `json:"task_id"`
	Repository     string    `json:"repository"`
	PullRequest    int       `json:"pull_request"`
	PullRequestURL string    `json:"pull_request_url"`
	// Replies and resolutions name the review comment or thread, its author
	// and path, an excerpt of it and, for a reply, the exact text to post.
	CommentID int64  `json:"comment_id,omitempty"`
	ThreadID  string `json:"thread_id,omitempty"`
	Author    string `json:"author,omitempty"`
	Bot       bool   `json:"bot,omitempty"`
	Path      string `json:"path,omitempty"`
	Excerpt   string `json:"excerpt,omitempty"`
	Body      string `json:"body,omitempty"`
	// A branch update names the head it was requested at, the base branch it
	// merges in and how far behind it is (-1 when unknown).
	Head   string `json:"head,omitempty"`
	Base   string `json:"base,omitempty"`
	Behind int    `json:"behind,omitempty"`
	// Draft is the state a mark_pull_request_ready approval sets: true puts the
	// pull request back to draft, false makes it ready for review.
	Draft bool `json:"draft"`
}

// ShipPayload is a ship_pull_request request: one approval for a chain that
// merges an issue's pull request, waits for the merge commit's checks on the
// default branch, publishes a release of that commit, waits for the release
// build and, when asked, upgrades this instance. Each step carries the payload
// its own tool would have approved. Steps records the progress; the first
// failed step stops the chain and the steps after it are skipped.
type ShipPayload struct {
	Identifier  string          `json:"identifier"`
	Title       string          `json:"title"`
	Repository  string          `json:"repository"`
	PullRequest int             `json:"pull_request"`
	Merge       MergePayload    `json:"merge"`
	Release     ReleasePayload  `json:"release"`
	Upgrade     *UpgradePayload `json:"upgrade,omitempty"`
	// MergeCommit is the commit the merge created on the default branch, once merged.
	MergeCommit string     `json:"merge_commit,omitempty"`
	Steps       []ShipStep `json:"steps"`
}

// ShipStep is one step of a ship_pull_request chain. Step is merge, tests,
// release, build or upgrade; Status is pending, running, done, failed or
// skipped (never run because an earlier step failed). Detail is what happened.
type ShipStep struct {
	Step   string `json:"step"`
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
	URL    string `json:"url,omitempty"`
}
