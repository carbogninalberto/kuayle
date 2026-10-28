package domain

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
)

// Agent message channels: internal messages reach only the recipient agent's
// next prompt; comment messages are also posted on the recipient issue.
const (
	AgentMessageInternal = "internal"
	AgentMessageComment  = "comment"

	// AgentMessageMaxRunes bounds one message body.
	AgentMessageMaxRunes = 4000
	// AgentOutboxFileMaxBytes bounds one outbox file as read from a worktree.
	AgentOutboxFileMaxBytes = 16384
	// AgentMessagesPerHour bounds what one task may send (refusals included).
	AgentMessagesPerHour = 20
)

var agentMessageRecipient = regexp.MustCompile(`^[A-Z][A-Z0-9]{0,9}-[0-9]{1,9}$`)

// AgentOutboxFile is one file an agent left in its worktree's
// .kuayle/outbox/. Data is untrusted and may be cut at the size limit.
type AgentOutboxFile struct {
	Worktree string
	Name     string
	Data     []byte
	Oversize bool
}

// AgentOutboxMessage is a parsed outbox file.
type AgentOutboxMessage struct {
	To      string
	Channel string
	Body    string
}

// ParseAgentOutboxMessage reads {"to","channel","body"} strictly: one JSON
// object, no other fields, a team-style identifier, a known channel and a
// bounded body without control characters other than tabs and newlines.
func ParseAgentOutboxMessage(data []byte) (AgentOutboxMessage, error) {
	if len(data) > AgentOutboxFileMaxBytes {
		return AgentOutboxMessage{}, errors.New("message file is too large")
	}
	if !utf8.Valid(data) {
		return AgentOutboxMessage{}, errors.New("message is not valid UTF-8")
	}
	var raw struct {
		To      *string `json:"to"`
		Channel *string `json:"channel"`
		Body    *string `json:"body"`
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&raw); err != nil || raw.To == nil || raw.Body == nil {
		return AgentOutboxMessage{}, errors.New("invalid JSON message")
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return AgentOutboxMessage{}, errors.New("invalid JSON message")
	}
	message := AgentOutboxMessage{To: strings.ToUpper(strings.TrimSpace(*raw.To)), Channel: AgentMessageInternal}
	if raw.Channel != nil {
		message.Channel = strings.TrimSpace(*raw.Channel)
	}
	if message.Channel != AgentMessageInternal && message.Channel != AgentMessageComment {
		return AgentOutboxMessage{}, errors.New("unknown channel")
	}
	if !agentMessageRecipient.MatchString(message.To) {
		return AgentOutboxMessage{}, errors.New("invalid recipient")
	}
	message.Body = cleanAgentMessage(*raw.Body)
	if message.Body == "" {
		return AgentOutboxMessage{}, errors.New("message is empty")
	}
	if utf8.RuneCountInString(message.Body) > AgentMessageMaxRunes {
		return AgentOutboxMessage{}, errors.New("message is too long")
	}
	return message, nil
}

func cleanAgentMessage(body string) string {
	body = strings.ReplaceAll(body, "\r\n", "\n")
	body = strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' {
			return r
		}
		if unicode.IsControl(r) || r == utf8.RuneError {
			return -1
		}
		return r
	}, body)
	return strings.TrimSpace(body)
}

// ValidAgentMessageIssue reports whether ref looks like an issue identifier.
func ValidAgentMessageIssue(ref string) bool { return agentMessageRecipient.MatchString(ref) }

// AgentMessageRef is what a refused file named as its recipient, bounded,
// for the registry.
func AgentMessageRef(data []byte) string {
	var raw struct {
		To string `json:"to"`
	}
	if json.Unmarshal(data, &raw) != nil {
		return "?"
	}
	ref := strings.ToUpper(strings.Join(strings.Fields(cleanAgentMessage(raw.To)), " "))
	if ref == "" {
		return "?"
	}
	if runes := []rune(ref); len(runes) > 64 {
		ref = string(runes[:64])
	}
	return ref
}

// AgentMessage is one registry record.
type AgentMessage struct {
	ID             uuid.UUID  `json:"id" db:"id"`
	WorkspaceID    uuid.UUID  `json:"-" db:"workspace_id"`
	FromTaskID     uuid.UUID  `json:"from_task_id" db:"from_task_id"`
	FromIssueID    uuid.UUID  `json:"from_issue_id" db:"from_issue_id"`
	FromRunID      *uuid.UUID `json:"from_run_id" db:"from_run_id"`
	ToRef          string     `json:"to_ref" db:"to_ref"`
	ToIssueID      *uuid.UUID `json:"to_issue_id" db:"to_issue_id"`
	ToTaskID       *uuid.UUID `json:"to_task_id" db:"to_task_id"`
	Channel        string     `json:"channel" db:"channel"`
	Body           string     `json:"body" db:"body"`
	State          string     `json:"state" db:"state"`
	RejectReason   *string    `json:"reject_reason" db:"reject_reason"`
	DedupeKey      string     `json:"-" db:"dedupe_key"`
	HeldBy         *uuid.UUID `json:"held_by" db:"held_by"`
	HeldAt         *time.Time `json:"held_at" db:"held_at"`
	ReleasedBy     *uuid.UUID `json:"released_by" db:"released_by"`
	ReleasedAt     *time.Time `json:"released_at" db:"released_at"`
	DroppedBy      *uuid.UUID `json:"dropped_by" db:"dropped_by"`
	DroppedAt      *time.Time `json:"dropped_at" db:"dropped_at"`
	DeliveredRunID *uuid.UUID `json:"delivered_run_id" db:"delivered_run_id"`
	DeliveredAt    *time.Time `json:"delivered_at" db:"delivered_at"`
	CommentState   string     `json:"comment_state" db:"comment_state"`
	CommentID      *uuid.UUID `json:"comment_id" db:"comment_id"`
	CreatedAt      time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at" db:"updated_at"`
}

// AgentMessageView is a registry record as people see it.
type AgentMessageView struct {
	AgentMessage
	FromIdentifier string  `json:"from_identifier" db:"from_identifier"`
	FromTitle      string  `json:"from_title" db:"from_title"`
	ToIdentifier   *string `json:"to_identifier" db:"to_identifier"`
	ToTitle        *string `json:"to_title" db:"to_title"`
	HeldByName     *string `json:"held_by_name" db:"held_by_name"`
	ReleasedByName *string `json:"released_by_name" db:"released_by_name"`
	DroppedByName  *string `json:"dropped_by_name" db:"dropped_by_name"`
	// Held reports a held conversation between the two issues.
	ConversationHoldID *uuid.UUID `json:"conversation_hold_id" db:"conversation_hold_id"`
	// CanModerate reports whether the reader may hold, release or drop it.
	CanModerate bool `json:"can_moderate" db:"can_moderate"`
}

// AgentMessagePage is one page of the registry, newest first.
type AgentMessagePage struct {
	Messages   []AgentMessageView `json:"messages"`
	NextBefore *uuid.UUID         `json:"next_before,omitempty"`
}

// AgentMessageFilter narrows the registry. IssueID matches either end.
type AgentMessageFilter struct {
	TaskID  *uuid.UUID
	IssueID *uuid.UUID
	// Issue is an issue identifier matching either visible end.
	Issue   string
	State   string
	Channel string
	Before  *uuid.UUID
}

// AgentMessageAction is one audited human action.
type AgentMessageAction struct {
	ID        int64      `json:"id" db:"id"`
	MessageID *uuid.UUID `json:"message_id" db:"message_id"`
	Action    string     `json:"action" db:"action"`
	ActorID   uuid.UUID  `json:"actor_id" db:"actor_id"`
	ActorName *string    `json:"actor_name" db:"actor_name"`
	CreatedAt time.Time  `json:"created_at" db:"created_at"`
}

// AgentMessageHold is a held conversation between two issues' agents.
type AgentMessageHold struct {
	ID               uuid.UUID  `json:"id" db:"id"`
	IssueA           uuid.UUID  `json:"issue_a_id" db:"issue_a"`
	IssueB           uuid.UUID  `json:"issue_b_id" db:"issue_b"`
	IssueAIdentifier string     `json:"issue_a" db:"issue_a_identifier"`
	IssueBIdentifier string     `json:"issue_b" db:"issue_b_identifier"`
	CreatedBy        *uuid.UUID `json:"created_by" db:"created_by"`
	CreatedByName    *string    `json:"created_by_name" db:"created_by_name"`
	HeldMessages     int        `json:"held_messages" db:"held_messages"`
	CanModerate      bool       `json:"can_moderate" db:"can_moderate"`
	CreatedAt        time.Time  `json:"created_at" db:"created_at"`
}

// AgentMessageDelivery is a message placed in a recipient's native prompt.
type AgentMessageDelivery struct {
	ID             uuid.UUID `db:"id"`
	FromIdentifier string    `db:"from_identifier"`
	FromTaskID     uuid.UUID `db:"from_task_id"`
	Channel        string    `db:"channel"`
	Body           string    `db:"body"`
	CreatedAt      time.Time `db:"created_at"`
}

// AgentMessagePreview is a refused file's text as kept for debugging: valid
// UTF-8 without control characters, at most 1,000 characters.
func AgentMessagePreview(data []byte) string {
	text := cleanAgentMessage(strings.ToValidUTF8(string(data), ""))
	if runes := []rune(text); len(runes) > 1000 {
		text = string(runes[:1000])
	}
	return text
}
