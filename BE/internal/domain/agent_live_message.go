package domain

import (
	"bytes"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/google/uuid"
)

// The live channel connects a running task agent with the person who owns the
// task and the Kuayle orchestrator acting for them. Messages to the agent are
// delivered while an attempt runs through a read-only inbox in its container,
// otherwise at the start of the next attempt; the agent answers through its
// outbox with {"to":"orchestrator"}. Agent text stays untrusted.
const (
	AgentOutboxOrchestrator = "orchestrator"

	AgentLiveToAgent   = "to_agent"
	AgentLiveFromAgent = "from_agent"

	AgentLiveSenderUser         = "user"
	AgentLiveSenderOrchestrator = "orchestrator"
	AgentLiveSenderAgent        = "agent"

	AgentLiveKindMessage  = "message"
	AgentLiveKindQuestion = "question"

	AgentLiveQueued    = "queued"
	AgentLiveDelivered = "delivered"
	AgentLiveReceived  = "received"
	AgentLiveRejected  = "rejected"

	AgentLiveViaInbox  = "inbox"
	AgentLiveViaPrompt = "prompt"

	// AgentLiveMessagesPerHour bounds each direction of one task's channel.
	AgentLiveMessagesPerHour = 30
	// AgentLiveInboxPath is where the agent container sees its inbox.
	AgentLiveInboxPath = "/run/kuayle/inbox"
)

// AgentLiveMessage is one message of a task's live channel.
type AgentLiveMessage struct {
	ID          uuid.UUID `json:"id" db:"id"`
	WorkspaceID uuid.UUID `json:"-" db:"workspace_id"`
	TaskID      uuid.UUID `json:"task_id" db:"task_id"`
	Direction   string    `json:"direction" db:"direction"`
	// Seq orders messages to the agent and names their inbox files.
	Seq      *int64     `json:"seq" db:"seq"`
	Sender   string     `json:"sender" db:"sender"`
	AuthorID *uuid.UUID `json:"author_id" db:"author_id"`
	Kind     string     `json:"kind" db:"kind"`
	Body     string     `json:"body" db:"body"`
	State    string     `json:"state" db:"state"`
	// DeliveredVia is inbox (during an attempt) or prompt (the next attempt).
	DeliveredVia *string `json:"delivered_via" db:"delivered_via"`
	// RunID is the attempt that received the message, or that sent it.
	RunID        *uuid.UUID `json:"run_id" db:"run_id"`
	RejectReason *string    `json:"reject_reason" db:"reject_reason"`
	DedupeKey    *string    `json:"-" db:"dedupe_key"`
	CreatedAt    time.Time  `json:"created_at" db:"created_at"`
	DeliveredAt  *time.Time `json:"delivered_at" db:"delivered_at"`
}

// AgentLiveInboxFile is one file of an attempt's inbox.
type AgentLiveInboxFile struct {
	Name string
	Data []byte
}

// InboxFile renders a message to the agent as <seq>.json, holding only what
// the agent needs: sequence, sender role, kind, text and time.
func (m AgentLiveMessage) InboxFile() (AgentLiveInboxFile, error) {
	if m.Seq == nil || *m.Seq <= 0 || (m.Sender != AgentLiveSenderUser && m.Sender != AgentLiveSenderOrchestrator) {
		return AgentLiveInboxFile{}, errors.New("not a message for the agent")
	}
	kind := m.Kind
	if kind == "" {
		kind = AgentLiveKindMessage
	}
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	// Plain text stays readable; a bounded body then always fits the limit.
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(struct {
		Seq    int64  `json:"seq"`
		From   string `json:"from"`
		Kind   string `json:"kind"`
		Body   string `json:"body"`
		SentAt string `json:"sent_at"`
	}{*m.Seq, m.Sender, kind, m.Body, m.CreatedAt.UTC().Format(time.RFC3339)}); err != nil {
		return AgentLiveInboxFile{}, err
	}
	data := bytes.TrimRight(buffer.Bytes(), "\n")
	if len(data) > AgentOutboxFileMaxBytes {
		return AgentLiveInboxFile{}, errors.New("message file is too large")
	}
	return AgentLiveInboxFile{Name: strconv.FormatInt(*m.Seq, 10) + ".json", Data: data}, nil
}

// AgentLiveChatMessage is a live message as the conversation that owns the
// task shows it: labelled with the task's issue. Agent text stays untrusted.
type AgentLiveChatMessage struct {
	AgentLiveMessage
	Identifier string `json:"identifier" db:"identifier_text"`
}
